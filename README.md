# Datapan CLI

Datapan CLI is an open-source, agent-friendly command-line tool for Korean
public data.

The first target is `data.go.kr`: discover useful API specs, check local API key
configuration, open or explain usage-application pages, call approved APIs from
the user's machine, and export machine-readable results without a Datapan
server in the middle.

This repository starts with the `datapan` command. A short `dp` alias can be
installed too, but the durable command contract is `datapan`.

## Why CLI First

Datapan is for public data and agents. A CLI gives both humans and coding agents
a stable surface before any UI exists:

- predictable commands and exit codes;
- `--json` output for automation;
- stdin/stdout-friendly parameter and export flows;
- local API keys owned by the user;
- browser automation only for explicit `datapan access login` and `--apply` workflows.

## Install From Source

```bash
go install github.com/StatPan/datapan-cli/cmd/datapan@latest
```

Optional short alias:

```bash
go install github.com/StatPan/datapan-cli/cmd/dp@latest
```

During local development:

```bash
go run ./cmd/datapan search "아파트" --json
go test ./...
```

See `.env.example` for local key names and `docs/cli-contract.md` for the
agent-facing command contract.

## API Key

Set one of these environment variables:

```bash
DATAPAN_DATA_GO_KR_KEY=...
DATA_PORTAL_API_KEY=...
DATA_GO_KR_SERVICE_KEY=...
```

`DATAPAN_DATA_GO_KR_KEY` is the preferred Datapan-specific name. The other names
are accepted because they already appear in existing public-data workflows.

## MVP Commands

```bash
datapan search "아파트 실거래가" --json
datapan search "실거래" --org 국토교통부 --json
datapan search --org 기상청 --json
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --pages 1 --json
datapan info 15126469
datapan auth check --json
datapan access 15126469 --purpose
datapan access 15126469 --open
datapan access 15126469 --start
datapan call 15084084 --operation getVilageFcst --param base_date=20260622 --param base_time=0500 --param nx=60 --param ny=127 --json
datapan call 15084084 --dry-run --json
datapan export --input response.json --format csv
```

`datapan access <list-id> --start` is the fast path for usage applications: it
opens the data.go.kr application page, copies the standard purpose text to the
clipboard when the OS supports it, prints the manual steps, and shows the smoke
command to run after approval.

Search can be narrowed with source metadata such as `--org`, `--category`,
`--priority`, and `--provider`. `provider` is the upstream platform such as
`data.go.kr`; `org` is the public agency or institution that provides the data.
`category` maps to the upstream source category only when that value is present
in the imported catalog; Datapan should not invent source categories.

To move beyond the embedded seed catalog, import the upstream data.go.kr
open-data list into a normalized Datapan registry:

```bash
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --pages 5 --json
DATAPAN_REGISTRY_PATH=.datapan/data-go-kr.registry.json datapan search "실거래" --org 국토교통부 --json
```

The importer uses data.go.kr's public list lookup API and preserves upstream
metadata such as organization, source category, source keywords, operation
names, request parameters, response parameters, and raw source fields.

For browser-backed application automation, first save an authenticated
data.go.kr browser session. This flow does not bypass CAPTCHA or provider
security controls; complete the login manually in the headed browser.

```bash
datapan access login --headed --profile-dir .datapan/browser-profile
datapan access 15126469 --dry-run --profile-dir .datapan/browser-profile --json
datapan access 15126469 --apply --profile-dir .datapan/browser-profile --json
```

`datapan access login` uses Go-native Chrome automation and a local browser
profile directory. No Python or Playwright install is required. Use `--headed`
for the first login so CAPTCHA or other provider security gates stay under the
user's control.

If Chrome/Chromium is not discoverable on `PATH`, pass `--browser-path` or set
`DATAPAN_BROWSER_PATH` to the browser executable.

Browser-backed access defaults to inspection/dry-run behavior. It submits only
when `--apply` is explicitly present. `datapan apply` and
`datapan access request` remain compatibility aliases for early builds; new docs
and scripts should use `datapan access`.

Exit codes are intentionally small and stable:

| Code | Meaning |
| ---: | --- |
| 0 | success |
| 1 | usage error |
| 2 | unknown spec/list ID |
| 3 | missing local API key |
| 4 | request or export failure |

## Scope

The embedded seed catalog is intentionally small. For broader coverage, import
the data.go.kr open-data list into a local normalized registry and point
`DATAPAN_REGISTRY_PATH` at that file.

## Non-Goals For The MVP

- No hosted server dependency.
- No UI/TUI.
- No CAPTCHA bypass or hidden provider-security workaround.
- No credential printing or storage.
- No claim that every imported data.go.kr operation is immediately callable
  without per-service approval or further endpoint cleanup.

## License

Apache-2.0
