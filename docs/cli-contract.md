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

Machine-readable failures should also be JSON under `--json`, with `ok:false`
and a stable `error` value such as `ambiguous_ref`, `not_found`,
`missing_auth`, `request_failed`, `open_failed`, or `copy_failed`. Commands
should still use the documented exit code for that failure.

Request envelopes may include `semantic_status` for broad transport/body shape
classification, such as `http_error`, `provider_error`, `html_response`,
`provider_ok`, or `json_response`. Provider-defined error details must be
preserved separately under `provider_status` instead of being remapped into
Datapan-specific error types. When present, `provider_status` should carry
source fields such as `resultCode/resultMsg` or
`OpenAPI_ServiceResponse/cmmMsgHeader` values including `returnReasonCode`,
`returnAuthMsg`, and `errMsg`. A provider failure must set `ok:false` and use
exit code 4.

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
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --all --json
DATAPAN_REGISTRY_PATH=.datapan/data-go-kr.registry.json datapan search "실거래" --org 국토교통부 --json
```

`--output -` writes only the registry JSON array to stdout. It must not be
combined with `--json`, because `--json` reserves stdout for one summary object.
`--all` fetches pages until the upstream `totalCount` has been reached. `--pages
N` remains available for bounded samples and CI smoke tests. `--all` uses a
default `--max-pages 1000` guard so a bad upstream counter cannot loop forever.

The normalized registry format is a JSON array of `Spec` objects. Canonical
source fields include `id`, `title`, `provider`, `organization`,
`source_category`, `source_keywords`, `operations`, and `source.raw`.
`search_terms` is reserved for Datapan-created search helpers and must not be
presented as upstream metadata.

`datapan catalog diff --old OLD --new NEW --json` compares two normalized
registry files by stable data.go.kr list ID. It must not guess renamed datasets.
The JSON response includes `summary`, `added`, `removed`, `changed`, and a
`report` object. `changed` entries include the changed field names and old/new
digests so an agent can decide whether a registry replacement needs review.
With `--output PATH|-`, the command writes a pure `datapan.catalog-diff.v1`
report containing `generated_at`, `provider`, `old`, `new`, `limit`,
`truncated`, `counts`, `summary`, `added`, `removed`, and `changed`. `--json`
may wrap that report in a command envelope for agent use and must not be
combined with `--output -`.

`datapan catalog audit --registry PATH --json` reports catalog quality and
coverage gaps. The response includes counts for specs, operations, callable
operations, specs without operations, specs without callable operations,
operations without endpoints, operations missing request/response parameters,
and missing source metadata. It also includes dependency classification for
data.go.kr gateway operations, external endpoint operations, gateway operations
with external guide documents, service-root-only operations, SOAP/WMS
operations, approval-required operations, and malformed endpoint or guide URLs.
Samples should be included only as bounded summaries and should not repeat the
same dataset ID within a sample bucket, even when multiple operations from that
dataset contribute to the same audit count. With `--output PATH|-`, the
command writes a pure `datapan.catalog-audit.v1` report containing
`generated_at`, `provider`, `registry`, `sample_limit`, and `audit`. `--json`
may wrap that report in a command envelope for agent use and must not be
combined with `--output -`.

`datapan catalog errors --registry PATH --json` inventories provider status and
error fields declared in operation response parameters. The command should
preserve upstream field names and labels while adding only a conservative
Datapan `role`, such as `result_code`, `result_message`, `reason_code`,
`auth_message`, or `error_message`. With `--output PATH|-`, the command writes
a pure `datapan.error-catalog.v1` report containing `generated_at`, `provider`,
`registry`, `limit`, `truncated`, `summary`, `status_fields`, and
`operations`. `--json` may wrap that report in a command envelope for agent use
and must not be combined with `--output -`.

`datapan catalog providers --registry PATH --json` converts dependency
classification into a host/provider backlog. The response includes summary
counts for data.go.kr gateway hosts, external endpoint hosts, external guide
hosts, registered adapter hosts, missing adapter hosts, operations that need
adapters, approval-required operations, unsupported protocol operations,
service-root operations, and malformed source URLs. Each provider item includes
`host`, optional inferred `provider`, `kinds`, `adapter_status`, spec and
operation counts, and bounded sample dataset IDs. `adapter_status` must stay
conservative: `builtin` for hosts Datapan can route through core logic,
`adapter` for external endpoint or service-root hosts with a registered
provider adapter, `missing` for hosts that still need provider work, and
`guide_only` for hosts that appear only as external documentation. With
`--output PATH|-`, the command
writes a pure `datapan.providers.v1` report containing `generated_at`,
`provider`, `registry`, `limit`, `truncated`, `filters`, `filtered_count`,
`summary`, and `providers`. `--status`, `--kind`, and `--provider` narrow the
provider list for adapter planning; the report must preserve those filters so
the artifact remains explainable. `--json` may wrap that report in a command
envelope for agent use.

`datapan catalog verify --registry PATH --json` collects bounded runtime
evidence. It must not blindly call the whole catalog. By default it should
consider a small bounded set of operations; callers may pass `--ref REF`,
`--operation NAME`, `--limit N`, `--provider NAME`, `--host HOST`, `--kind
KIND`, and `--output PATH|-`. Provider, host, and kind filters apply before the
limit, so `--provider q-net --limit 5` means five q-net candidates, not the
first five catalog operations. The command should call only conservative
candidates: data.go.kr gateway operations with concrete endpoints and enough
known parameters from smoke metadata, operation defaults, or safe paging/format
defaults, plus external endpoints owned by registered provider adapters when
the adapter can supply conservative provider-specific defaults. External
endpoints without adapters, service-root-only entries, unsupported protocols,
malformed endpoints, approval-gated entries, and operations missing required
parameters should be returned as `skipped` with a clear reason.

Verification JSON includes a `report` with `generated_at`, `provider`,
`registry`, `ref`, `operation`, `limit`, `truncated`, `filters`,
`filtered_count`, `summary`, and `results`. Each result includes dataset ID,
operation, dependency class, status,
timestamp when a call was attempted, HTTP status, semantic status, provider
status, redacted URL, public parameters, missing parameters, and body shape
where available. Status values are conservative: `verified`, `failed`,
`skipped`, or `unknown`. Failed provider responses must preserve
`provider_status` rather than remapping upstream errors into Datapan-only
types. If eligible calls cannot run because credentials are absent, the command
returns exit code 3 while still emitting a machine-readable report under
`--json`.

`datapan catalog verify --input REPORT --json` reads an existing verification
report without making new provider calls. It may be combined with `--status`
to filter results to `verified`, `failed`, `skipped`, or `unknown`, with
`--limit N` to bound the returned result list, and with `--output PATH|-` to
write the filtered report. Input mode must not be combined with `--registry`,
`--ref`, or `--operation`.

`datapan catalog verify summary --input REPORT --json` reads an existing
verification report and emits a pure `datapan.verification-summary.v1` rollup.
The summary includes the original report summary plus grouped counts by status,
reason, provider, endpoint host, and dependency class. `--limit N` bounds each
group list except status groups, and `--output PATH|-` writes the summary
artifact for release or CI use.

`datapan catalog release draft --registry PATH --json` assembles a local
registry release layout without fetching upstream data or calling provider
APIs. It copies Datapan schema files, writes `schemas/index.json`, writes
`data/data-go-kr.registry.json`, generates `reports/catalog-audit.json`,
generates `reports/provider-backlog.json`, optionally copies a verification
report with `--verification PATH`, writes
`reports/latest-verification-summary.json` from that report, and writes
provenance under `provenance/data-go-kr.md`. It also writes `manifest.json`
with relative artifact paths, byte sizes, and SHA-256 checksums. Use
`--output-dir DIR` to choose the release draft directory and
`--provider-limit N` to bound provider report output.

`datapan catalog release verify --manifest PATH --json` rereads a release
manifest without fetching upstream data or calling provider APIs and emits a
`datapan.release-verification.v1` report. Use `--output PATH|-` to write the
pure report artifact; `--json` wraps that report with command metadata and must
not be combined with `--output -`. The command treats the manifest directory as
the release root, verifies each listed relative artifact path, byte size, and
SHA-256 checksum, and validates schema-bound artifacts against the schema files
published in the same release. It returns exit code 4 when any artifact is
missing, outside the release root, size-mismatched, checksum-mismatched, has an
invalid checksum format, fails schema validation, duplicates another artifact
path, references `manifest.json` itself, uses an unsupported manifest schema
version, or when `artifact_count` does not match the listed artifact count.

`datapan catalog update data-go-kr --registry PATH --json` is the safe update
path. It fetches the full upstream catalog, normalizes it, diffs it against the
existing registry, audits the new registry, and returns the result without
modifying files. The command must replace the registry only when `--apply` is
present. With `--backup`, it should write a timestamped copy of the previous
registry before replacement. Long catalog fetches should retry bounded provider
or transport failures and report retry counts and the failed page when the
import still cannot complete. Diff detail output should be bounded by default;
`--diff-limit 0` may be used when a caller explicitly wants all diff entries.

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

`datapan show <ref> --json` should be the stable handoff from search to use. In
addition to the normalized `spec`, it returns:

- `access`: data.go.kr application URL and known upstream access/status fields.
- `operations`: operation names, endpoints, request parameters, response
  parameter counts, and a generated `datapan get ...` example when callable.
- `examples`: top-level `access` and `get` commands for the selected dataset.

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
`show` may expose provider auth parameters under `auth_params`, but generated
examples must not ask the user to pass `serviceKey`; Datapan supplies that from
the accepted environment variables.

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

When using the real OS environment, Datapan may read a local `.env` file from
the current working directory. Process environment variables take precedence
over `.env` values. Shell-style single or double quotes around values should be
trimmed during parsing.

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
human prose. `smoke_command` may come from curated smoke metadata or be
synthesized from the selected operation in the imported registry. `datapan
apply` is a compatibility alias; `datapan access` is the canonical command.

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
