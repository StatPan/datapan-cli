param(
    [Parameter(Mandatory = $true)]
    [string]$Datapan,
    [string]$WorkDir = "",
    [switch]$KeepWorkDir,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Output "Usage: pwsh scripts/smoke-registry-journey.ps1 -Datapan PATH [-WorkDir DIR] [-KeepWorkDir]"
    exit 0
}

$Datapan = [System.IO.Path]::GetFullPath($Datapan)
if (-not (Test-Path -LiteralPath $Datapan)) {
    throw "datapan binary not found: $Datapan"
}
if ([string]::IsNullOrWhiteSpace($WorkDir)) {
    $WorkDir = Join-Path ([System.IO.Path]::GetTempPath()) ("datapan-registry-journey-" + [System.Guid]::NewGuid().ToString("N"))
}
$WorkDir = [System.IO.Path]::GetFullPath($WorkDir)
New-Item -ItemType Directory -Path $WorkDir -Force | Out-Null

function Invoke-DatapanJSON {
    param([string[]]$CommandArgs)
    $lines = & $Datapan @CommandArgs 2>&1
    $exitCode = $LASTEXITCODE
    $text = $lines -join "`n"
    if ($text.Contains("smoke-secret-do-not-print")) {
        throw "credential leaked from datapan $($CommandArgs -join ' ')"
    }
    if ($exitCode -ne 0) {
        throw "datapan $($CommandArgs -join ' ') failed with exit code $exitCode`n$text"
    }
    try {
        return ConvertFrom-Json -InputObject $text
    } catch {
        throw "datapan $($CommandArgs -join ' ') did not return JSON: $($_.Exception.Message)`n$text"
    }
}

function Assert-NoCredentialLeak {
    param([string[]]$Paths)
    foreach ($path in $Paths) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "credential leak check target not found: $path"
        }
        $text = [System.IO.File]::ReadAllText([System.IO.Path]::GetFullPath($path))
        if ($text.Contains("smoke-secret-do-not-print")) {
            throw "credential leaked into generated artifact: $path"
        }
    }
}

try {
    Push-Location $WorkDir
    try {
        $init = Invoke-DatapanJSON @("init", "--json")
        if (-not $init.ok -or -not $init.ready_for_search) {
            throw "init did not install a searchable Registry release"
        }
        foreach ($path in @(".datapan/data-go-kr.registry.json", ".datapan/registry-install.json", ".datapan/release/manifest.json")) {
            if (-not (Test-Path -LiteralPath $path)) {
                throw "init did not preserve required release artifact: $path"
            }
        }

        $status = Invoke-DatapanJSON @("status", "--json")
        if (-not $status.ok -or -not $status.ready_for_search -or $status.registry.specs -lt 1) {
            throw "status did not report an installed Registry"
        }
        if (-not $status.registry_release.provenance_present -or $status.registry_release.registry_digest_matches -ne $true) {
            throw "status did not verify Registry provenance and integrity"
        }

        $ready = Invoke-DatapanJSON @("ready", "--limit", "1", "--json")
        if (-not $ready.ok -or $ready.count -ne 1) {
            throw "ready did not return a call-ready operation"
        }
        $dataset = [string]$ready.results[0].id
        $operation = [string]$ready.results[0].default_operation
        if ([string]::IsNullOrWhiteSpace($dataset) -or [string]::IsNullOrWhiteSpace($operation)) {
            throw "ready result did not preserve dataset and operation identity"
        }

        $search = Invoke-DatapanJSON @("search", $dataset, "--limit", "10", "--json")
        $searchIDs = @($search.results | ForEach-Object { [string]$_.id })
        if (-not $search.ok -or $search.registry_trust.integrity -ne "verified" -or $dataset -notin $searchIDs) {
            throw "search did not rediscover the selected dataset with trusted Registry context"
        }

        $show = Invoke-DatapanJSON @("show", $dataset, "--json")
        if (-not $show.ok -or $show.registry_trust.integrity -ne "verified") {
            throw "show did not preserve trusted Registry context"
        }
        $try = Invoke-DatapanJSON @("try", $dataset, "--operation", $operation, "--json")
        if (-not $try.ok -or -not $try.call_ready -or [string]::IsNullOrWhiteSpace($try.commands.get)) {
            throw "try did not create a call-ready execution plan"
        }

        $paramsPath = Join-Path $WorkDir "selected.params.json"
        $params = Invoke-DatapanJSON @("params", $dataset, "--operation", $operation, "--output", $paramsPath, "--json")
        $paramsProvenancePath = "$paramsPath.datapan-provenance.json"
        if (-not $params.ok -or $params.registry_trust.integrity -ne "verified" -or $params.provenance -ne $paramsProvenancePath) {
            throw "params did not preserve trusted Registry provenance"
        }
        foreach ($path in @($paramsPath, $paramsProvenancePath)) {
            if (-not (Test-Path -LiteralPath $path)) {
                throw "params artifact not found: $path"
            }
        }
        $paramsProvenance = Get-Content -LiteralPath $paramsProvenancePath -Raw | ConvertFrom-Json
        $paramsSHA256 = (Get-FileHash -LiteralPath $paramsPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($paramsProvenance.schema_version -ne "datapan.generated-artifact-provenance.v1" -or
            $paramsProvenance.artifact.kind -ne "params" -or
            $paramsProvenance.artifact.sha256 -ne $paramsSHA256 -or
            $paramsProvenance.registry_trust.integrity -ne "verified") {
            throw "params provenance does not bind the generated artifact and Registry trust"
        }
        Assert-NoCredentialLeak @($paramsPath, $paramsProvenancePath)

        $env:DATAPAN_DATA_GO_KR_KEY = "smoke-secret-do-not-print"
        $dryRun = Invoke-DatapanJSON @("get", $dataset, "--operation", $operation, "--params-file", $paramsPath, "--dry-run", "--json")
        if (-not $dryRun.ok -or -not $dryRun.dry_run -or $dryRun.url -notmatch "REDACTED") {
            throw "dry-run did not produce a redacted execution plan"
        }

        $exportPath = Join-Path $WorkDir "selected.openapi.json"
        $export = Invoke-DatapanJSON @("export", "--format", "openapi", $dataset, "--operation", $operation, "--output", $exportPath, "--json")
        $exportProvenancePath = "$exportPath.datapan-provenance.json"
        if (-not $export.ok -or $export.registry_trust.integrity -ne "verified" -or $export.provenance -ne $exportProvenancePath) {
            throw "standalone export did not preserve trusted Registry provenance"
        }
        foreach ($path in @($exportPath, $exportProvenancePath)) {
            if (-not (Test-Path -LiteralPath $path)) {
                throw "standalone export artifact not found: $path"
            }
        }
        $exportProvenance = Get-Content -LiteralPath $exportProvenancePath -Raw | ConvertFrom-Json
        $exportSHA256 = (Get-FileHash -LiteralPath $exportPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($exportProvenance.schema_version -ne "datapan.generated-artifact-provenance.v1" -or
            $exportProvenance.artifact.sha256 -ne $exportSHA256 -or
            $exportProvenance.registry_trust.integrity -ne "verified") {
            throw "standalone export provenance does not bind the generated artifact and Registry trust"
        }
        Assert-NoCredentialLeak @($exportPath, $exportProvenancePath)

        $kitDir = Join-Path $WorkDir "kit"
        $kit = Invoke-DatapanJSON @("kit", $dataset, "--operation", $operation, "--output-dir", $kitDir, "--json")
        if (-not $kit.ok) {
            throw "kit generation failed"
        }
        foreach ($path in @("datapan-provenance.json", "$dataset.openapi.json", "$dataset.postman_collection.json")) {
            if (-not (Test-Path -LiteralPath (Join-Path $kitDir $path))) {
                throw "kit artifact not found: $path"
            }
        }
        $kitFiles = @($kit.files | ForEach-Object { [string]$_.path })
        Assert-NoCredentialLeak $kitFiles

        $evidence = [pscustomobject]@{
            ok = $true
            datapan_version = (Invoke-DatapanJSON @("version", "--json")).version
            registry_release = $status.registry_release.installed.release_tag
            registry_specs = $status.registry.specs
            trust_status = $show.registry_trust.status
            dataset = $dataset
            operation = $operation
            search_count = $search.count
            params_sha256 = $paramsSHA256
            params_provenance = $paramsProvenancePath
            dry_run_redacted = $true
            export_sha256 = $exportSHA256
            export_provenance = $exportProvenancePath
            kit_files = $kitFiles.Count
            credential_leak = $false
            work_dir = $WorkDir
        }
        $evidenceJSON = $evidence | ConvertTo-Json -Depth 5
        Set-Content -LiteralPath (Join-Path $WorkDir "journey-evidence.json") -Value $evidenceJSON -Encoding utf8
        Write-Output $evidenceJSON
    } finally {
        Remove-Item Env:DATAPAN_DATA_GO_KR_KEY -ErrorAction SilentlyContinue
        Pop-Location
    }
} finally {
    if (-not $KeepWorkDir) {
        Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
