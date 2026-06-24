param(
    [string]$Version = "latest",
    [string]$WorkDir = "",
    [switch]$KeepWorkDir,
    [switch]$Help
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSCommandPath)

if ($Help) {
    Write-Output "Usage: powershell -ExecutionPolicy Bypass -File scripts/smoke-first-run.ps1 [-Version latest|vX.Y.Z] [-WorkDir DIR] [-KeepWorkDir]"
    exit 0
}

function Assert-File {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "expected file not found: $Path"
    }
}

function Invoke-DatapanJSON {
    param(
        [string]$Datapan,
        [string[]]$CommandArgs,
        [string]$Cwd
    )
    $output = & $Datapan @CommandArgs
    if ($LASTEXITCODE -ne 0) {
        throw "datapan $($CommandArgs -join ' ') failed with exit code $LASTEXITCODE`n$output"
    }
    $json = $output -join "`n"
    try {
        return ConvertFrom-Json -InputObject $json
    } catch {
        throw "datapan $($CommandArgs -join ' ') did not return JSON: $($_.Exception.Message)`n$json"
    }
}

if ([string]::IsNullOrWhiteSpace($WorkDir)) {
    $WorkDir = Join-Path ([System.IO.Path]::GetTempPath()) ("datapan-first-run-" + [System.Guid]::NewGuid().ToString("N"))
}
$WorkDir = [System.IO.Path]::GetFullPath($WorkDir)
$InstallDir = Join-Path $WorkDir "bin"
$ProjectDir = Join-Path $WorkDir "project"
New-Item -ItemType Directory -Path $InstallDir, $ProjectDir -Force | Out-Null

try {
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $RepoRoot "scripts/install.ps1") -Version $Version -InstallDir $InstallDir
    if ($LASTEXITCODE -ne 0) {
        throw "install.ps1 failed with exit code $LASTEXITCODE"
    }

    $datapan = Join-Path $InstallDir "datapan.exe"
    $dp = Join-Path $InstallDir "dp.exe"
    Assert-File $datapan
    Assert-File $dp

    Push-Location $ProjectDir
    try {
        $versionPayload = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("version", "--json") -Cwd $ProjectDir
        if ([string]::IsNullOrWhiteSpace($versionPayload.version)) {
            throw "version payload did not include version"
        }

        $init = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("init", "--json") -Cwd $ProjectDir
        if (-not $init.ok -or -not $init.ready_for_search) {
            throw "init did not produce a searchable registry"
        }
        Assert-File (Join-Path $ProjectDir ".datapan/data-go-kr.registry.json")

        $status = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("status", "--json") -Cwd $ProjectDir
        if (-not $status.ok -or -not $status.ready_for_search -or $status.registry.specs -lt 10000) {
            throw "status did not report a full registry"
        }

        $ready = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("ready", "--limit", "3", "--json") -Cwd $ProjectDir
        if (-not $ready.ok -or $ready.count -lt 1) {
            throw "ready did not return call-ready APIs"
        }

        $try = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("try", "15084084", "base_date=20260622", "--json") -Cwd $ProjectDir
        if (-not $try.ok -or -not $try.call_ready -or [string]::IsNullOrWhiteSpace($try.commands.get)) {
            throw "try did not select a call-ready operation with commands"
        }

        $kitDataset = "15084084"
        $kitDir = Join-Path $ProjectDir "smoke-kit"
        $kit = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("kit", $kitDataset, "base_date=20260622", "base_time=0500", "nx=60", "ny=127", "--output-dir", $kitDir, "--json") -Cwd $ProjectDir
        if (-not $kit.ok) {
            throw "kit did not complete"
        }
        Assert-File (Join-Path $kitDir "$($kitDataset)_params.json")
        Assert-File (Join-Path $kitDir "$($kitDataset).postman_collection.json")
        Assert-File (Join-Path $kitDir "$($kitDataset).openapi.json")

        $studioDir = Join-Path $ProjectDir "smoke-studio"
        $studio = Invoke-DatapanJSON -Datapan $datapan -CommandArgs @("studio", "--output-dir", $studioDir, "--limit", "5", "--json") -Cwd $ProjectDir
        if (-not $studio.ok) {
            throw "studio did not complete"
        }
        Assert-File (Join-Path $studioDir "studio.json")
        Assert-File (Join-Path $studioDir "index.html")

        $dpPayload = Invoke-DatapanJSON -Datapan $dp -CommandArgs @("version", "--json") -Cwd $ProjectDir
        if ($dpPayload.version -ne $versionPayload.version) {
            throw "dp alias version did not match datapan version"
        }

        [pscustomobject]@{
            ok = $true
            version = $versionPayload.version
            work_dir = $WorkDir
            registry_specs = $status.registry.specs
            ready_count = $ready.count
            try_dataset = $try.dataset
            kit_dir = $kitDir
            studio_dir = $studioDir
        } | ConvertTo-Json -Depth 5
    } finally {
        Pop-Location
    }
} finally {
    if (-not $KeepWorkDir) {
        Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
