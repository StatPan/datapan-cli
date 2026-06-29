# Troubleshooting

## `datapan` Is Not Found

Installers place binaries in `$HOME/.datapan/bin` by default. Add that
directory to `PATH`, then check:

```bash
datapan version --json
```

On Windows, restart the terminal after changing `PATH`.

## Missing data.go.kr Key

Some commands can search and inspect the registry without credentials. Provider
calls require a public-data key:

```bash
DATAPAN_DATA_GO_KR_KEY=...
```

Compatibility names such as `DATA_PORTAL_API_KEY` are also accepted, but new
docs should prefer `DATAPAN_DATA_GO_KR_KEY`.

## Registry Install Fails

Installers and `datapan init` read GitHub Releases. If GitHub rate limits the
request, set one of:

```bash
GITHUB_TOKEN=...
GH_TOKEN=...
```

Then retry:

```bash
datapan catalog install datapan-registry --json
```

## Checksum Mismatch

A checksum mismatch means the downloaded archive did not match the release
manifest. Delete the temporary installer download if it remains, retry once, and
avoid manually copying binaries from an unverified archive.

## Optional `dp` Alias Was Not Installed

`dp` is opt-in. Install it explicitly:

```powershell
powershell -ExecutionPolicy Bypass -File $script -InstallAlias
```

```bash
DATAPAN_INSTALL_DP=1 sh scripts/install.sh
```

If another `dp` command already exists on `PATH`, the installer skips Datapan's
optional alias and leaves the existing command untouched. Use `datapan` as the
stable command.

## Provider Calls Time Out

Use smaller batches first:

```bash
datapan verify --limit 5 --workers 1 --timeout 10s --json
```

Increase `--workers` only after a provider is known to tolerate concurrent
requests.
