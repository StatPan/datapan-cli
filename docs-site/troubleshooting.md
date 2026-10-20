# Troubleshooting

## `datapan: command not found`

The installer prints its default directory. Add it to `PATH`, then start a new shell if your shell caches command lookup:

```bash
export PATH="$HOME/.datapan/bin:$PATH"
datapan version --json
```

## Registry install fails

Run the diagnostic commands and inspect the JSON error without deleting `.datapan`:

```bash
datapan status --json
datapan doctor --json
datapan catalog install datapan-registry --json
```

The installer and registry commands need network access. A failed refresh must not be treated as a successful or complete registry installation.

## Missing data.go.kr key

Set one accepted environment variable. `DATAPAN_DATA_GO_KR_KEY` is preferred:

```bash
export DATAPAN_DATA_GO_KR_KEY='your-data-go-kr-service-key'
datapan auth check --json
```

A key is not required for local registry discovery or dry-run planning, but live provider calls can fail without a valid user-owned key.

## GitHub download or rate-limit failure

The release installer resolves `latest` through GitHub and downloads the archive plus `checksums.txt`. Retry with a pinned release after confirming it exists, or download the script first and run it locally. The PowerShell installer accepts `GITHUB_TOKEN` or `GH_TOKEN` for GitHub API requests; do not paste tokens into shell history or docs.

```bash
DATAPAN_VERSION=v0.1.0 sh scripts/install.sh
```

This Linux command was syntax-checked, but a live GitHub release download was not performed during this documentation change.

## `dp` alias was skipped

`datapan` is always the supported command. On Linux, the alias is opt-in:

```bash
DATAPAN_INSTALL_DP=1 sh scripts/install.sh
```

The shell installer does not detect an existing `dp` command, so check before enabling it:

```bash
command -v dp || true
```

On PowerShell, use `-InstallAlias`; an existing `dp.exe` causes a warning and the alias is skipped. Remove an alias you no longer want with `rm -f "$HOME/.datapan/bin/dp"` on Linux or `Remove-Item "$HOME\.datapan\bin\dp.exe" -Force` on Windows.
