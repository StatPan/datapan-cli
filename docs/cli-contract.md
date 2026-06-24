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
datapan list --limit 10 --json
datapan ready --limit 10 --json
datapan list --callable --limit 10 --json
datapan list --call-ready --limit 10 --json
datapan search "실거래" --org 국토교통부 --json
datapan ls --org 기상청 --json
datapan search --org 기상청 --json
```

## Registry Install

`datapan init` is the normal first-run consumer command. It installs the latest
released Datapan registry into `.datapan/data-go-kr.registry.json` by default,
reports registry and credential readiness, reports registered provider
adapters, and returns next-step commands. It must not print credential values.
Consumer commands that resolve datasets, such as `search`, `show`, `use`,
`get`, `curl`, `save`, `call`, `access`, `export`, and `codegen`, should
automatically load that default registry file from the current project
directory when `DATAPAN_REGISTRY_PATH` is not set. `DATAPAN_REGISTRY_PATH`
remains the explicit override for alternate registry files.
Catalog observation commands with optional `--registry`, including `overview`,
`studio`, `audit`, `errors`, `providers`, `dependencies`, `adapter-targets`,
and `verify`, should also load the default installed registry automatically.
Catalog mutation and release commands such as `import`, `update`, `install`,
`diff`, and `release` keep their explicit path contracts.

```bash
datapan init --json
datapan ready --limit 10 --json
datapan list --limit 10 --json
datapan list --callable --limit 10 --json
datapan list --call-ready --limit 10 --json
datapan search "실거래" --org 국토교통부 --json
```

`datapan catalog install datapan-registry` remains the lower-level install
command when callers only want to download and write the released registry.
`datapan doctor --json` reports the active registry source, default registry
path, spec and operation counts, data.go.kr credential presence, registered
provider adapters, and next-step hints. It should not print credential values.

`datapan search --json`, `datapan list --json`, and `datapan ls --json` must
include per-result `examples` for immediate next steps: `show`, `use`, `kit`,
`params`, `get`, `curl`, `postman`, `openapi`, `codegen_go`, `codegen_node`,
and `codegen_python` when those commands can be generated from the selected
operation. Each result should also expose decision metadata such as `callable`,
`call_ready`, `call_route`, `call_provider`, `endpoint_host`,
`default_operation`, `data_format`, approval state, register status,
`updated_at`, and `application_url` when those upstream values are available.
`callable` means the catalog has at least one operation endpoint. `call_ready`
is stricter: it is true only for routes Datapan currently treats as stable for
`get` automation, such as the data.go.kr gateway or a call-capable provider
adapter. `call_route` should use stable values such as `data_go_kr_gateway`,
`provider_adapter`, `provider_adapter_verification_only`, `generic_external`,
`service_root`, `soap`, `wms`, `malformed_endpoint`, or `not_callable`.
`list` and `ls` accept the same source metadata filters as `search`;
unlike `search`, they may run with no query or filters and should return a
bounded dataset list. `--callable` is also accepted by `search`, `list`, and
`ls`; it filters results to specs with at least one operation endpoint and may
be used without a search query. `--call-ready` is also accepted by `search`,
`list`, and `ls`; it filters to specs with at least one `call_ready` operation.
`--ready` is a short human-friendly alias for `--call-ready`. `datapan ready`
is a top-level shortcut for `datapan list --call-ready` and should accept the
same query and source metadata filters as `list`. Its default ordering should
prefer ready APIs with fewer missing required parameters, fewer request
parameters, and less action-like operation names before falling back to route,
priority, and ID ordering. JSON output must include
`callable_only` and `call_ready_only` so agents can tell which filters were
applied. Human output should include `callable: yes|no`,
`call ready: yes|no (...)`, and at least a
`next: datapan show <id>` line and, when callable, a
`try: datapan get ...` line plus a `kit: datapan kit ... --json` line.
Generated examples must omit auth parameters such as `serviceKey`, `apiKey`,
`authApiKey`, and `authKey`; Datapan supplies credentials from environment
variables. Generated examples should fill safe starter values for common
paging and response-format parameters such as `pageNo=1`, `numOfRows=10`,
`_type=json`, `dataType=json`, and `resultType=json`. Unknown required
parameters should remain `VALUE`.

`datapan kit <ref>` should generate a portable starter kit for one selected
operation under `<dataset-id>-kit` by default. `--output-dir DIR` may override
that location. The kit includes params JSON, a curl script, Postman collection,
OpenAPI document, Go/Node/Python clients, and a README. Generated files must
use environment-variable placeholders for credentials and must not write actual
service-key material to disk. `datapan use <ref> --output-dir DIR` remains a
compatible lower-level path for callers that already build around the planning
command.

## Registry Import

`datapan catalog import data-go-kr` imports the upstream data.go.kr open-data
list into Datapan's normalized registry format. The command must preserve
upstream metadata separately from Datapan-generated search helpers.

```bash
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --all --json
datapan search "실거래" --org 국토교통부 --json
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

`datapan catalog overview --registry PATH --json` emits a compact registry
dashboard for humans, agents, and future Studio consumers. It combines
registry counts, dependency coverage, provider-adapter ownership, top
organizations/categories, top external hosts, top registered-adapter hosts,
top missing-adapter hosts, and suggested next commands. With `--output PATH|-`,
the command writes a pure overview report containing `generated_at`, `provider`,
`registry`, `source`, `limit`, `summary`, `top`, `adapters`, and `next`.
`--json` may wrap that report in a command envelope for agent use and must not
be combined with `--output -`. When no `--registry` is supplied, it follows the
same default installed registry discovery used by consumer commands.

`datapan catalog coverage --registry PATH --json` emits a claim-oriented
coverage and gap report. It combines registry counts, callable operation
counts, data.go.kr gateway coverage, external endpoint coverage,
registered-vs-missing adapter coverage, approval-required and unsupported
operation counts, provider split readiness, and top missing adapter hosts.
Callers may pass `--verification REPORT` to include runtime verification
evidence: total checked operations, verified/failed/skipped/unknown counts,
verification timeout, verified percentage, and the percentage of catalog
operations represented by that verification report. With `--output PATH|-`,
the command writes a pure coverage report containing `generated_at`,
`provider`, `registry`, `source`, `verification`, `summary`, `evidence`,
`gaps`, `adapters`, and `next`. `--json` may wrap that report in a command
envelope for agent use and must not be combined with `--output -`. When no
`--registry` is supplied, it follows the same default installed registry
discovery used by consumer commands.

`datapan catalog studio --output-dir DIR --json` writes a static consumer
bundle for Studio-like tools. It must create `overview.json`, `datasets.json`,
`studio.json`, and `index.html`. `overview.json` reuses the catalog overview
report; `datasets.json` contains bounded dataset cards with operation
summaries, access metadata, and the same `examples` map used by search/show;
`studio.json` wraps the bundle manifest, overview, dataset cards, provider
readiness, and next-step commands; `index.html` is a static local viewer over
that same embedded bundle. With no `--registry`, it follows the default
installed registry discovery used by consumer commands. The command may accept
`--query`, `--org`, `--category`, `--provider`, `--priority`, and `--limit` to
build a focused bundle.

`datapan catalog providers [--registry PATH] --json` converts dependency
classification into a host/provider backlog. When `--registry` is omitted, it
uses the default installed registry. The response includes summary
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

`datapan catalog dependencies [--registry PATH] --json` emits an
operation-level dependency inventory. When `--registry` is omitted, it uses the
default installed registry. The response includes summary counts for gateway,
external endpoint, service-root, missing endpoint, malformed endpoint, SOAP,
WMS, approval-required, registered-adapter, and missing-adapter operations.
Each dependency item includes dataset ID, title, operation name, upstream
provider, endpoint/source/guide hosts when present, dependency class, adapter
status, inferred provider family, upstream API type/data format, approval
metadata, skip reason, parameter counts, and missing required parameters.
`adapter_status` must stay conservative: `builtin` for data.go.kr gateway
operations, `adapter` for external endpoint or service-root operations owned by
a registered adapter, `missing` for external endpoint or service-root
operations that need adapter work, and `not_applicable` for unsupported or
non-callable classes. With `--output PATH|-`, the command writes a pure
`datapan.dependencies.v1` report containing `generated_at`, `provider`,
`registry`, `limit`, `truncated`, `filters`, `filtered_count`, `summary`, and
`dependencies`. `--status`, `--kind`, `--provider`, and `--host` narrow the
operation list before limit is applied. `--json` may wrap that report in a
command envelope for agent use and must not be combined with `--output -`.

`datapan catalog adapter-targets [--registry PATH] --json` converts missing
external endpoint and service-root operations into a prioritized adapter work
queue by host. When `--registry` is omitted, it uses the default installed
registry. The response includes summary counts for target hosts, target
operations, external endpoint operations, service-root operations,
approval-required operations, missing-parameter operations, and unsupported
protocol operations. Each target includes rank, host, inferred provider family,
dependency kinds, operation and spec counts, organizations, source categories,
API types, data formats, approval and missing-parameter counts, unsupported
protocol counts, and bounded sample operations. Targets are sorted by operation
coverage, then spec coverage, then approval-required count, then host. With
`--output PATH|-`, the command writes a pure `datapan.adapter-targets.v1`
report containing `generated_at`, `provider`, `registry`, `limit`,
`truncated`, `filters`, `filtered_count`, `summary`, and `targets`.
`--provider`, `--host`, and `--kind` narrow the target list before limit is
applied. `--json` may wrap that report in a command envelope for agent use and
must not be combined with `--output -`.

`datapan catalog verify [--registry PATH] --json` collects bounded runtime
evidence. It must not blindly call the whole catalog. By default it should
consider a small bounded set of operations; when `--registry` is omitted, it
uses the default installed registry. Callers may pass `--ref REF`,
`--operation NAME`, `--limit N`, `--provider NAME`, `--host HOST`, `--kind
KIND`, `--exclude-input REPORT`, `--timeout DURATION`, and `--output PATH|-`.
Provider, host, and kind
filters apply before the limit, so `--provider q-net --limit 5` means five
q-net candidates, not the first five catalog operations. `--timeout` bounds
each eligible provider call; it accepts Go durations such as `500ms` or `10s`,
or bare seconds, and defaults to 30 seconds. `--exclude-input` removes
operations already present in an existing verification report before applying
the limit, so scheduled batches can accumulate evidence without repeating the
same dataset operation. The command should call only
conservative candidates: data.go.kr gateway operations with concrete endpoints
and enough known parameters from smoke metadata, operation defaults, or safe
paging/format defaults, plus external endpoints owned by registered provider
adapters when the adapter can supply conservative provider-specific defaults.
External endpoints without adapters, service-root-only entries, unsupported
protocols, malformed endpoints, approval-gated entries, and operations missing
required parameters should be returned as `skipped` with a clear reason.

Verification JSON includes a `report` with `generated_at`, `provider`,
`registry`, `ref`, `operation`, `limit`, `timeout`, `exclude_input`,
`truncated`, `filters`, `filtered_count`, `summary`, and `results`. Each result includes dataset ID,
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
`--ref`, `--operation`, `--provider`, `--host`, `--kind`, `--exclude-input`,
or `--timeout`.

`datapan catalog verify plan --registry PATH --json` emits a bounded
verification growth plan without calling providers. With `--verification
REPORT`, it computes already-covered dataset operations and emits batch
commands that include `--exclude-input REPORT`. The report includes total
operations, existing evidence count, uncovered gateway candidates, uncovered
registered-adapter candidates, missing adapter hosts, planned batch count,
planned operation count, ready-to-run `catalog verify` commands, top missing
adapter gaps, and next commands for coverage and evidence merging.

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
`reports/error-catalog.json`, `reports/dependencies.json`, and
`reports/adapter-targets.json`, and `reports/provider-backlog.json`,
optionally writes `reports/catalog-diff.json` when `--previous-registry PATH`
is provided,
optionally copies a verification report with `--verification PATH`, writes
`reports/latest-verification-summary.json` from that report, and writes
provenance under `provenance/data-go-kr.md` and human-facing release notes
under `RELEASE_NOTES.md`. It also writes `manifest.json` with relative artifact
paths, byte sizes, and SHA-256 checksums. Use
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

`datapan catalog release readiness --manifest PATH --json` rereads a release
manifest, runs the same manifest verification, and emits a
`datapan.release-readiness.v1` gate report for registry publication decisions.
It must not fetch upstream data or call provider APIs. Required gates include a
verified manifest, a complete schema set for the current CLI, a non-empty
registry, and the presence of `schema_index`, `registry`, `provider_index`,
`catalog_audit`, `error_catalog`, `dependencies`, `adapter_targets`,
`provider_backlog`, `provenance`, and `release_notes` artifacts. Recommended
gates warn when `catalog_diff`, `verification`, or `verification_summary`
artifacts are absent. The command returns exit code 4 when any required gate fails. Warnings do not
make `ready:false`, but they remain visible in `summary` and `gates`.

`datapan catalog install datapan-registry --registry PATH --json` is the normal
lower-level install path for a released Datapan registry. It fetches the latest
GitHub release metadata for `StatPan/datapan-registry`, downloads the first
`.zip` asset, extracts `data/data-go-kr.registry.json`, validates that the file
decodes as a Datapan registry, and writes it to `PATH` without calling
data.go.kr. Use `--url URL` to install from an explicit release zip and skip
release metadata lookup. Use `--release-url URL` to point at a different
compatible GitHub release API endpoint. JSON output reports `ok`, `provider`,
`registry`, `url`, `bytes`, `specs`, `installed`, and `release`. `release`
must report whether the downloaded zip included `manifest.json`,
`RELEASE_NOTES.md`, release verification, and release readiness artifacts, plus
parsed readiness/verification summaries when those files are present. `--json`
must not be combined with `--registry -`, because `--registry -` writes the raw
registry JSON to stdout.

`datapan init [--registry PATH] [--url URL] [--release-url URL] --json` wraps
that install path for first-run setup. JSON output must include `install`,
`registry`, `auth`, `providers`, `ready_for_search`, `ready_for_calls`, and
`next_steps`. `ready_for_calls` is true only when a registry was installed and a
data.go.kr key is present. `--registry -` is not allowed for `init`; callers who
want raw registry JSON on stdout should use `catalog install`. `next_steps`
should include `datapan ready --limit 10 --json` after a successful install so
new users can immediately find APIs with stable call routes.

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
datapan use 15084084 base_date=20260622 base_time=0500 --json
datapan params 15084084 base_date=20260622 base_time=0500 --output forecast.params.json
datapan get "기상청_단기예보 조회서비스" base_date=20260622 base_time=0500 --json
datapan get 15084084 --params-file forecast.params.json --timeout 5s --dry-run --json
datapan curl 15084084 base_date=20260622 base_time=0500
datapan save 15084084 base_date=20260622 base_time=0500 --format csv --output forecast.csv
datapan export --format curl 15084084 base_date=20260622 base_time=0500
datapan export --format postman 15084084 base_date=20260622 base_time=0500 --output forecast.postman_collection.json
datapan export --format openapi 15084084 base_date=20260622 base_time=0500 --output forecast.openapi.json
datapan codegen go 15084084 base_date=20260622 base_time=0500 --package forecastclient --output forecast_client.go
datapan codegen node 15084084 base_date=20260622 base_time=0500 --output forecast_client.js
datapan codegen python 15084084 base_date=20260622 base_time=0500 --output forecast_client.py
datapan preview --input response.json --limit 10
```

`datapan show <ref> --json` should be the stable handoff from search to use. In
addition to the normalized `spec`, it returns:

- `access`: data.go.kr application URL and known upstream access/status fields.
- `operations`: operation names, endpoints, request parameters, response
  parameter counts, and a generated `datapan get ...` example when callable.
- `examples`: top-level `access`, `params`, `get`, export, and codegen commands
  for the selected dataset when those commands can be generated.

`datapan use <ref> [KEY=VALUE ...] [--param k=v] [--params-file PATH|-]
--json` is the stable planning handoff from a resolved dataset to concrete
consumer actions. It must not call the provider. It must return `dataset`,
`title`, `operation`, `application_url`, accepted credential env vars, the
merged non-auth `params`, field labels, and a `commands` object containing
copyable `params`, `dry_run`, `get`, `save_csv`, `curl`, `postman`, `openapi`,
`codegen_go`, `codegen_node`, `codegen_python`, and `access` commands when the
selected operation is callable. The merge order is operation defaults, smoke
values, `--params-file`, positional `KEY=VALUE`, and `--param k=v`, with later
sources overriding earlier sources. The command must preserve exact upstream
parameter names and must never include credential values or auth parameters in
the params object or generated commands.

`datapan kit <ref> [KEY=VALUE ...] [--param k=v] [--params-file PATH|-]
[--output-dir DIR] --json` is the shorter human-facing starter-kit command. It
must reuse the same parameter merge, operation resolution, credential redaction,
and generated file set as `datapan use --output-dir`, while defaulting
`--output-dir` to `<dataset-id>-kit` when the caller omits it.

`datapan params <ref> [KEY=VALUE ...] [--param k=v] --output params.json`
writes a JSON object that can be passed directly to
`datapan get/save/call/curl/export/codegen --params-file`. It must use exact
upstream parameter names, omit auth parameters such as `serviceKey`, preserve
operation default or smoke values where known, apply user-supplied overrides,
and use `VALUE` for unknown user-editable fields. With `--json`, it must
require `--output PATH` and return `params`, field labels, `next_get`, and
`next_dry_run` without mixing the raw params object into stdout.

`datapan get` and its calling aliases (`call`, `save`, and CSV/JSON
`export`) accept `--timeout DURATION`. Durations follow Go-style values such as
`5s` and `500ms`; a bare integer is interpreted as seconds. The default is
`30s`. Dry-run JSON must include the selected `timeout`, and actual provider
calls must cancel through the request context when the timeout expires.

## Verification Evidence

`datapan catalog verify merge --input A --input B --output REPORT --json`
combines existing verification reports without making provider calls. It is a
pure evidence-accumulation command: provider failures and skipped results remain
in the merged report, and the command itself succeeds when the input reports
are valid JSON and the output is written. `--json` must not be combined with
`--output -`.

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
datapan params 15084084 base_date=20260622 | datapan get 15084084 --params-file - --dry-run --json
datapan export --input - --format csv
datapan preview --input - --format json --json
```

`get` and `save` also accept positional `KEY=VALUE` parameters for the common
case where a user or agent has the required parameter names from `show`.
`show` may expose provider auth parameters under `auth_params`, but generated
examples must not ask the user to pass `serviceKey`, `apiKey`, `authApiKey`, or
`authKey`; Datapan supplies those from the accepted environment variables.

`datapan curl <ref>` and `datapan export --format curl <ref>` emit a copyable
`curl -fsS ...` command without making a provider request. The generated URL
must include `serviceKey=${ENV_VAR}` using the selected or preferred credential
environment variable name, and must never include the credential value.
`datapan export --format postman <ref>` writes a Postman Collection v2.1 JSON
document for the same request plan. The generated collection must represent the
service key as a Postman variable such as `{{DATAPAN_DATA_GO_KR_KEY}}` or
`{{DATA_PORTAL_API_KEY}}`, never as the credential value.
`datapan export --format openapi <ref>` writes an OpenAPI 3.1 JSON document for
the same request plan. The generated document must include server, path, query
parameter, response-field, and `serviceKey` apiKey security-scheme metadata. It
must represent the service key as an environment-variable placeholder such as
`${DATAPAN_DATA_GO_KR_KEY}` or `${DATA_PORTAL_API_KEY}`, never as the
credential value.
`datapan preview --input PATH|- [--format auto|json|csv] [--limit N] --json`
and its alias `datapan head` inspect saved data without making provider calls.
Input format `json` accepts data.go.kr response envelopes or Datapan row JSON;
input format `csv` treats the first row as headers; `auto` tries JSON first and
then CSV. JSON output must include `ok`, `input`, detected `format`, total
`count`, requested `limit`, `truncated`, `columns`, and limited `rows`. Human
output should be a compact fixed-width table suitable for quick terminal
inspection.
`datapan codegen go <ref>` writes a small compilable Go client for the same
request plan. The generated file must use a caller-provided service key or
`NewFromEnv`, must expose operation parameters as `map[string]string` so
upstream parameter names remain exact, and must not embed credential values or
shell placeholders.
`datapan codegen node <ref>` writes a dependency-free Node.js client for the
same request plan. The generated file must use built-in `fetch`, expose
operation parameters as a plain object so upstream parameter names remain
exact, provide `DatapanClient.fromEnv()`, and must not embed credential values
or shell placeholders.
`datapan codegen python <ref>` writes a dependency-free Python client for the
same request plan. The generated file must use `urllib`, expose operation
parameters as a mapping so upstream parameter names remain exact, provide
`DatapanClient.from_env()`, and must not embed credential values or shell
placeholders.

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
output must redact `serviceKey`; curl exports must use an environment-variable
placeholder instead of the raw key.

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
