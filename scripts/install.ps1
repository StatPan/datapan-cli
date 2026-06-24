param(
    [string]$Version = "latest",
    [string]$InstallDir = "$HOME\.datapan\bin",
    [switch]$NoDp,
    [switch]$Help
)

$ErrorActionPreference = "Stop"
$Repo = "StatPan/datapan-cli"

if ($Help) {
    Write-Output "Usage: powershell -ExecutionPolicy Bypass -File scripts/install.ps1 [-Version latest|vX.Y.Z] [-InstallDir DIR] [-NoDp]"
    exit 0
}

function Get-ReleaseTag {
    param([string]$RequestedVersion)
    if ($RequestedVersion -ne "latest") {
        return $RequestedVersion
    }
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "datapan-installer" }
    return $release.tag_name
}

$tag = Get-ReleaseTag $Version
if ([string]::IsNullOrWhiteSpace($tag)) {
    throw "could not resolve Datapan CLI release tag"
}

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" {
        Write-Warning "Windows ARM64 archive is not published yet; installing windows amd64 build."
        "amd64"
    }
    default { throw "unsupported Windows architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$asset = "datapan-cli_${tag}_windows_${arch}.zip"
$baseUrl = "https://github.com/$Repo/releases/download/$tag"
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("datapan-cli-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $workDir | Out-Null

try {
    $assetPath = Join-Path $workDir $asset
    $checksumsPath = Join-Path $workDir "checksums.txt"
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $assetPath -Headers @{ "User-Agent" = "datapan-installer" }
    Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumsPath -Headers @{ "User-Agent" = "datapan-installer" }

    $line = Get-Content $checksumsPath | Where-Object { $_ -match [Regex]::Escape($asset) } | Select-Object -First 1
    if (-not $line) {
        throw "checksum for $asset not found"
    }
    $expected = ($line -split "\s+")[0].ToLowerInvariant()
    $actual = (Get-FileHash $assetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        throw "checksum mismatch for $asset"
    }

    $extractDir = Join-Path $workDir "extract"
    Expand-Archive -LiteralPath $assetPath -DestinationPath $extractDir
    $payloadDir = Join-Path $extractDir ("datapan-cli_${tag}_windows_${arch}")
    $datapan = Join-Path $payloadDir "datapan.exe"
    $dp = Join-Path $payloadDir "dp.exe"
    if (-not (Test-Path $datapan)) {
        throw "datapan.exe not found in $asset"
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -LiteralPath $datapan -Destination (Join-Path $InstallDir "datapan.exe") -Force
    if (-not $NoDp) {
        if (-not (Test-Path $dp)) {
            throw "dp.exe not found in $asset"
        }
        Copy-Item -LiteralPath $dp -Destination (Join-Path $InstallDir "dp.exe") -Force
    }

    & (Join-Path $InstallDir "datapan.exe") version --json
    Write-Output "Installed Datapan CLI $tag to $InstallDir"
    Write-Output "Add $InstallDir to PATH if datapan is not already available."
} finally {
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
