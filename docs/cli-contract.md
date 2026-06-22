# CLI Contract

This contract is the first stable surface for humans, scripts, and coding
agents. The implementation can grow, but these expectations should stay boring.

## Command Name

The canonical command is:

```bash
datapan
```

Installers may add `dp` as a convenience alias. Documentation and agent
instructions should prefer `datapan`.

## Output

Commands that support `--json` must produce one JSON object on stdout and no
human prose on stdout. Diagnostic errors go to stderr.

`--json` may appear before or after the subcommand:

```bash
datapan --json search "미세먼지"
datapan search "미세먼지" --json
```

Search may be narrowed by source metadata. `provider` is the upstream platform,
such as `data.go.kr`; `org` is the agency or institution that provides the
dataset. `category` maps to the upstream source category only when that value
is present in the imported catalog.

```bash
datapan search "실거래" --org 국토교통부 --json
datapan search --org 기상청 --json
```

## Registry Import

`datapan catalog import data-go-kr` imports the upstream data.go.kr open-data
list into Datapan's normalized registry format. The command must preserve
upstream metadata separately from Datapan-generated search helpers.

```bash
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --pages 5 --json
DATAPAN_REGISTRY_PATH=.datapan/data-go-kr.registry.json datapan search "실거래" --org 국토교통부 --json
```

`--output -` writes only the registry JSON array to stdout. It must not be
combined with `--json`, because `--json` reserves stdout for one summary object.

The normalized registry format is a JSON array of `Spec` objects. Canonical
source fields include `id`, `title`, `provider`, `organization`,
`source_category`, `source_keywords`, `operations`, and `source.raw`.
`search_terms` is reserved for Datapan-created search helpers and must not be
presented as upstream metadata.

## Dataset Refs

Commands that operate on one dataset accept a `<ref>`. A ref may be a data.go.kr
list ID, a data.go.kr detail URL, an exact title, or a query string. Exact ID,
URL, and title matches resolve directly. Query matches must resolve to exactly
one dataset before a command can call, save, or request access. Ambiguous refs
must fail with exit code 5 and return candidate summaries under `--json`.

```bash
datapan show "국토교통부_아파트 매매 실거래가 자료" --json
datapan get "기상청_단기예보 조회서비스" base_date=20260622 base_time=0500 --json
datapan save 15084084 base_date=20260622 base_time=0500 --format csv --output forecast.csv
```

## Exit Codes

| Code | Meaning |
| ---: | --- |
| 0 | success |
| 1 | usage error |
| 2 | unknown spec/list ID |
| 3 | missing local API key |
| 4 | request or export failure |
| 5 | ambiguous dataset ref |

## Stdin And Files

Parameter and export flows should accept `-` for stdin where practical:

```bash
datapan call 15084084 --params-file - --json
datapan export --input - --format csv
```

`get` and `save` also accept positional `KEY=VALUE` parameters for the common
case where a user or agent has the required parameter names from `show`.

## Credentials

The preferred key is:

```bash
DATAPAN_DATA_GO_KR_KEY
```

Compatibility names are also accepted:

```bash
DATA_PORTAL_API_KEY
DATA_GO_KR_SERVICE_KEY
```

Credential values must never be printed. Request URLs shown in dry-run and JSON
output must redact `serviceKey`.

## Application Help

`datapan access` is the guided data.go.kr access helper. It may open the
application page, copy reusable purpose text to the clipboard, print manual
steps, show a bounded post-approval smoke command, and run explicit
browser-backed access workflows only when the user asks for them.

The fast path is:

```bash
datapan access <list-id> --start
```

`--start` is equivalent to opening the application page and copying/showing the
purpose text. JSON output should expose `application_url`, `purpose_text`,
`next_steps`, and `smoke_command` so an agent can guide the user without scraping
human prose. `datapan apply` is a compatibility alias; `datapan access` is the
canonical command.

Browser-backed application automation is an explicit advanced flow:

```bash
datapan access login --headed --profile-dir .datapan/browser-profile
datapan access <list-id> --dry-run --profile-dir .datapan/browser-profile --json
datapan access <list-id> --apply --profile-dir .datapan/browser-profile --json
```

The implementation should use Go-native browser automation and a local browser
profile directory, without requiring Python or Playwright. `access login` may
persist a browser profile only after the user completes any CAPTCHA/security
gate manually. Browser-backed `access <list-id>` must default to inspection and
must submit only when `--apply` is present. It must reuse the saved profile,
fill visible purpose/usage fields, accept visible checkboxes, and stop with a
machine-readable status if the session is expired or a human gate appears.
When Chrome/Chromium is not discoverable, the user may provide `--browser-path`
or `DATAPAN_BROWSER_PATH`.
