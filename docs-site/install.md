# Install Datapan CLI

`datapan` is the canonical command. The short `dp` command is an optional alias and is not installed unless you explicitly enable it.

## Linux (verified)

The Linux shell installer downloads the selected GitHub Release archive, verifies `checksums.txt`, and installs into `$HOME/.datapan/bin` by default. It is non-interactive: there is no prompt, and the alias is skipped unless `DATAPAN_INSTALL_DP=1` is set.

```bash
curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh | sh
```

Add the install directory to the current shell's `PATH` if needed:

```bash
export PATH="$HOME/.datapan/bin:$PATH"
datapan version --json
```

The installer supports a pinned release and a custom directory:

```bash
DATAPAN_VERSION=v0.1.0 DATAPAN_INSTALL_DIR="$HOME/.local/bin" sh <(curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh)
```

To opt in to `dp`, set the environment variable explicitly:

```bash
DATAPAN_INSTALL_DP=1 sh <(curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh)
dp version --json
```

If another `dp` command already exists on `PATH`, do not overwrite it. The shell installer itself does not currently detect that collision; inspect `command -v dp` before opting in and use `DATAPAN_INSTALL_DIR` to isolate the install if needed.

Remove the optional alias and Datapan binaries with:

```bash
rm -f "$HOME/.datapan/bin/dp" "$HOME/.datapan/bin/datapan"
```

## Windows (not verified on this Linux machine)

PowerShell installer behavior is implemented but not verified here. Download the script, then run it without `-InstallAlias` for the canonical command only:

```powershell
$script = "$env:TEMP\datapan-install.ps1"; iwr https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.ps1 -OutFile $script; powershell -ExecutionPolicy Bypass -File $script
```

Pin a release or opt in to the alias explicitly:

```powershell
powershell -ExecutionPolicy Bypass -File $script -Version v0.1.0 -InstallAlias
```

The PowerShell installer does not prompt. `-InstallAlias` installs `dp.exe`; without it, `dp.exe` is skipped. Use `-NoDp` to explicitly prohibit the alias. If another `dp.exe` is already available, the installer warns and skips the alias. Remove the installed files with `Remove-Item "$HOME\.datapan\bin\datapan.exe", "$HOME\.datapan\bin\dp.exe" -Force -ErrorAction SilentlyContinue`.

## From source

Go 1.26 is required by `go.mod`:

```bash
go install github.com/StatPan/datapan-cli/cmd/datapan@latest
```

The `dp` binary is separate and optional:

```bash
go install github.com/StatPan/datapan-cli/cmd/dp@latest
```

See the [quickstart](quickstart.md) for the first local workflow and [troubleshooting](troubleshooting.md) for common failures. The full command contract is in [`docs/cli-contract.md`](../docs/cli-contract.md).
