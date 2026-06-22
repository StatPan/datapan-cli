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

## Exit Codes

| Code | Meaning |
| ---: | --- |
| 0 | success |
| 1 | usage error |
| 2 | unknown spec/list ID |
| 3 | missing local API key |
| 4 | request or export failure |

## Stdin And Files

Parameter and export flows should accept `-` for stdin where practical:

```bash
datapan call 15084084 --params-file - --json
datapan export --input - --format csv
```

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
