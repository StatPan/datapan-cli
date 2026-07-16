param(
    [Parameter(Mandatory = $true)]
    [string]$Datapan,
    [string]$WorkDir = "",
    [switch]$KeepWorkDir,
    [ValidateRange(1, 3)]
    [int]$DistributionAttempts = 1,
    [ValidateRange(0, 30)]
    [int]$DistributionRetryDelaySeconds = 2,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Output "Usage: pwsh scripts/smoke-registry-journey.ps1 -Datapan PATH [-WorkDir DIR] [-KeepWorkDir] [-DistributionAttempts 1..3] [-DistributionRetryDelaySeconds 0..30]"
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
$script:RegistryDistributionAttempts = 0

function Invoke-DatapanJSON {
    param(
        [string[]]$CommandArgs,
        [ValidateRange(1, 3)]
        [int]$MaxDistributionAttempts = 1
    )
    for ($attempt = 1; $attempt -le $MaxDistributionAttempts; $attempt++) {
        $lines = & $Datapan @CommandArgs 2>&1
        $exitCode = $LASTEXITCODE
        $text = $lines -join "`n"
        if ($text.Contains("smoke-secret-do-not-print")) {
            throw "credential leaked from datapan $($CommandArgs -join ' ')"
        }
        if ($exitCode -eq 0) {
            if ($CommandArgs.Count -gt 0 -and $CommandArgs[0] -eq "init") {
                $script:RegistryDistributionAttempts = $attempt
            }
            try {
                return ConvertFrom-Json -InputObject $text
            } catch {
                throw "datapan $($CommandArgs -join ' ') did not return JSON: $($_.Exception.Message)`n$text"
            }
        }

        $failure = $null
        try {
            $failure = ConvertFrom-Json -InputObject $text
        } catch {
            # Non-JSON failures are never retry candidates.
        }
        $retryableDistributionTimeout = (
            $CommandArgs.Count -gt 0 -and
            $CommandArgs[0] -eq "init" -and
            $null -ne $failure -and
            [string]$failure.error -eq "registry_distribution_failed" -and
            [string]$failure.category -eq "distribution_timeout"
        )
        if (-not $retryableDistributionTimeout -or $attempt -ge $MaxDistributionAttempts) {
            throw "datapan $($CommandArgs -join ' ') failed with exit code $exitCode`n$text"
        }

        $delaySeconds = $DistributionRetryDelaySeconds * $attempt
        Write-Warning "Registry distribution timed out on init attempt $attempt/$MaxDistributionAttempts; retrying after ${delaySeconds}s"
        if ($delaySeconds -gt 0) {
            Start-Sleep -Seconds $delaySeconds
        }
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

function Invoke-DatapanHealthReceipt {
    param(
        [string]$Dataset,
        [string]$Operation,
        [string]$OutputPath
    )

    # Healthcheck workers must be able to reproduce the receipt contract
    # without inheriting a developer credential from the release environment.
    foreach ($name in @("DATAPAN_DATA_GO_KR_KEY", "DATA_PORTAL_API_KEY", "DATA_GO_KR_SERVICE_KEY")) {
        Remove-Item "Env:$name" -ErrorAction SilentlyContinue
    }

    $lines = & $Datapan "verify" "--ref" $Dataset "--operation" $Operation "--health" "--output" $OutputPath "--json" 2>&1
    $exitCode = $LASTEXITCODE
    $text = $lines -join "`n"
    if ($text.Contains("smoke-secret-do-not-print")) {
        throw "credential leaked from health receipt invocation"
    }
    if ($exitCode -ne 3) {
        throw "credential-free health receipt should exit 3 (skipped), got $exitCode`n$text"
    }
    if (-not (Test-Path -LiteralPath $OutputPath)) {
        throw "health receipt was not written: $OutputPath"
    }
    try {
        $stdoutReceipt = ConvertFrom-Json -InputObject $text
        $receipt = Get-Content -LiteralPath $OutputPath -Raw | ConvertFrom-Json
    } catch {
        throw "health receipt was not valid JSON: $($_.Exception.Message)`n$text"
    }
    if ($stdoutReceipt.probe_id -ne $receipt.probe_id -or
        $receipt.schema_version -ne "datapan.health-probe.v1" -or
        $receipt.assessment.outcome -ne "skipped" -or
        $receipt.assessment.category -ne "credential_missing" -or
        $receipt.execution.attempted -ne $false -or
        $receipt.observation.data_presence -ne "not_observed" -or
        $receipt.redaction.credentials_removed -ne $true -or
        $receipt.redaction.query_values_removed -ne $true -or
        $receipt.redaction.response_rows_removed -ne $true -or
        [string]::IsNullOrWhiteSpace([string]$receipt.registry.dataset_revision) -or
        [string]$receipt.registry.registry_sha256 -notmatch "^[a-f0-9]{64}$" -or
        [string]$receipt.registry.manifest_sha256 -notmatch "^[a-f0-9]{64}$") {
        throw "health receipt did not preserve the released contract boundary"
    }
    Assert-NoCredentialLeak @($OutputPath)
    return $receipt
}

try {
    Push-Location $WorkDir
    try {
        $init = Invoke-DatapanJSON -CommandArgs @("init", "--json") -MaxDistributionAttempts $DistributionAttempts
        if (-not $init.ok -or -not $init.ready_for_search) {
            throw "init did not install a searchable Registry release"
        }
        $requiredInstallArtifacts = @(".datapan/data-go-kr.registry.json", ".datapan/registry-install.json")
        if ($init.install.distribution -eq "huggingface_dataset") {
            if ($init.install.release.manifest_present) {
                $requiredInstallArtifacts += ".datapan/release/manifest.json"
                $requiredInstallArtifacts += ".datapan/release/registry-shards/registry-shards.json"
            } else {
                $requiredInstallArtifacts += ".datapan/release/registry-shards.json"
            }
            if ([string]::IsNullOrWhiteSpace([string]$init.install.dataset_revision) -or
                [string]$init.install.dataset_revision -notmatch "^[a-f0-9]{40}([a-f0-9]{24})?$") {
                throw "Hugging Face install did not preserve an immutable Dataset revision"
            }
        } else {
            $requiredInstallArtifacts += ".datapan/release/manifest.json"
        }
        foreach ($path in $requiredInstallArtifacts) {
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

        $healthReceiptPath = Join-Path $WorkDir "health-probe-receipt.json"
        $healthReceipt = Invoke-DatapanHealthReceipt -Dataset $dataset -Operation $operation -OutputPath $healthReceiptPath

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
            registry_distribution = $init.install.distribution
            registry_distribution_attempts = $script:RegistryDistributionAttempts
            registry_dataset_revision = $init.install.dataset_revision
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
            health_probe = [pscustomobject]@{
                schema_version = $healthReceipt.schema_version
                outcome = $healthReceipt.assessment.outcome
                category = $healthReceipt.assessment.category
                attempted = $healthReceipt.execution.attempted
                registry_dataset_revision = $healthReceipt.registry.dataset_revision
                registry_sha256 = $healthReceipt.registry.registry_sha256
                manifest_sha256 = $healthReceipt.registry.manifest_sha256
                receipt = $healthReceiptPath
            }
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
