# Install

`datapan` is the canonical command. The shorter `dp` command is an optional
alias and is installed only when explicitly requested.

## Windows PowerShell

Install the latest release:

```powershell
$script = "$env:TEMP\datapan-install.ps1"
iwr https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.ps1 -OutFile $script
powershell -ExecutionPolicy Bypass -File $script
```

Install a pinned release:

```powershell
powershell -ExecutionPolicy Bypass -File $script -Version v0.1.34
```

Install the optional `dp` alias:

```powershell
powershell -ExecutionPolicy Bypass -File $script -InstallAlias
```

Non-interactive opt-in also works through the environment:

```powershell
$env:DATAPAN_INSTALL_DP = "1"
powershell -ExecutionPolicy Bypass -File $script
```

## Linux And macOS

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh | sh
```

Install a pinned release:

```bash
curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh | DATAPAN_VERSION=v0.1.34 sh
```

Install the optional `dp` alias:

```bash
curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh | DATAPAN_INSTALL_DP=1 sh
```

## Install Location And PATH

By default, installers place binaries in:

```text
$HOME/.datapan/bin
```

Set a custom install directory when needed:

```powershell
powershell -ExecutionPolicy Bypass -File $script -InstallDir "$HOME\.local\bin"
```

```bash
DATAPAN_INSTALL_DIR="$HOME/.local/bin" sh scripts/install.sh
```

Add the install directory to `PATH` if `datapan version --json` is not found
after installation.

## Checksums And Archives

Installers download the GitHub Release archive for the selected version and
verify it against `checksums.txt` before copying binaries. Release archives
contain both `datapan` and `dp`, but installers copy only `datapan` unless the
optional alias is enabled.

If another `dp` command already exists on `PATH`, installers skip the optional
alias and keep the existing command untouched.

## From Source

```bash
go install github.com/StatPan/datapan-cli/cmd/datapan@latest
go install github.com/StatPan/datapan-cli/cmd/dp@latest
```
