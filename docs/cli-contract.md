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

`datapan apply` is a guided helper, not an unattended browser automation system.
It may open the data.go.kr application page, copy reusable purpose text to the
clipboard, print manual steps, and show a bounded post-approval smoke command.
It must not submit applications or store login sessions in the MVP.

The fast path is:

```bash
datapan apply <list-id> --start
```

`--start` is equivalent to opening the application page and copying/showing the
purpose text. JSON output should expose `application_url`, `purpose_text`,
`next_steps`, and `smoke_command` so an agent can guide the user without scraping
human prose.

Browser-backed application automation is an explicit advanced flow:

```bash
datapan apply login --headed --storage-state .datapan/data-go-kr-browser-state.json
datapan apply submit <list-id> --dry-run --storage-state .datapan/data-go-kr-browser-state.json --json
datapan apply submit <list-id> --apply --storage-state .datapan/data-go-kr-browser-state.json --json
```

The implementation should prepare the Playwright Chromium runtime before opening
the browser. It may prefer `uv run --with playwright ...` when `uv` is
available, and may fall back to a local Python with Playwright installed.
`apply login` may save a browser storage state only after the user completes any
CAPTCHA/security gate manually. `apply submit` must default to inspection and
must submit only when `--apply` is present. It must reuse the saved session,
fill visible purpose/usage fields, accept visible checkboxes, and stop with a
machine-readable status if the session is expired or a human gate appears.
