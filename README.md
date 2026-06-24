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

## Binary Releases

Tagged `v*` releases build Linux, macOS, and Windows archives containing both
`datapan` and the optional `dp` alias. Each release also includes
`checksums.txt` so install scripts and agents can verify downloaded binaries.
Release binaries stamp `datapan version --json` with the tag name.

See `.env.example` for local key names, `docs/cli-contract.md` for the
agent-facing command contract, `docs/ecosystem.md` for the spec-first Datapan
repository map, `docs/registry-release.md` for registry artifact release
planning, `docs/spec-governance.md` for schema versioning rules,
`docs/provider-adapters.md` for external-provider adapter boundaries, and
`schemas/` for the first registry/provider/verification/release artifact
schemas.

## API Key

Set one of these environment variables:

```bash
DATAPAN_DATA_GO_KR_KEY=...
DATA_PORTAL_API_KEY=...
DATA_GO_KR_SERVICE_KEY=...
```

`DATAPAN_DATA_GO_KR_KEY` is the preferred Datapan-specific name. The other names
are accepted because they already appear in existing public-data workflows.
Both decoded service keys and URL-encoded keys copied from data.go.kr are
accepted; Datapan avoids double-encoding the key when building request URLs.
When running from a project directory, Datapan also reads `.env` automatically
if the variable is not already present in the process environment.

## MVP Commands

```bash
datapan init --json
datapan ready --limit 10 --json
datapan try "단기예보" base_date=20260622 --org 기상청 --json
datapan list --limit 10 --json
datapan list --callable --limit 10 --json
datapan list --call-ready --limit 10 --json
datapan search "아파트 실거래가" --json
datapan search "실거래" --org 국토교통부 --json
datapan ls --org 기상청 --json
datapan search --org 기상청 --json
datapan catalog install datapan-registry --json
datapan doctor --json
datapan catalog overview --json
datapan catalog coverage --verification .datapan/latest-verification.json --json
datapan catalog studio --output-dir .datapan/studio --limit 200 --json
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --all --json
datapan catalog diff --old .datapan/previous.registry.json --new .datapan/data-go-kr.registry.json --output .datapan/catalog-diff.json --json
datapan catalog audit --registry .datapan/data-go-kr.registry.json --json
datapan catalog errors --registry .datapan/data-go-kr.registry.json --output .datapan/error-catalog.json --json
datapan catalog dependencies --registry .datapan/data-go-kr.registry.json --kind external_endpoint --status missing --output .datapan/dependencies.json --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --output .datapan/adapter-targets.json --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --output .datapan/provider-backlog.json --json
datapan catalog verify plan --registry .datapan/data-go-kr.registry.json --verification .datapan/latest-verification.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15084084 --timeout 10s --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --kind data_go_kr_gateway --exclude-input .datapan/latest-verification.json --limit 20 --timeout 10s --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider airport --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider q-net --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider ekape --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider forest --kind external_endpoint --limit 4 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider folk --kind external_endpoint --limit 3 --json
datapan catalog verify --input .datapan/latest-verification.json --status failed --json
datapan catalog verify summary --input .datapan/qnet-batch-verification.json --json
datapan catalog verify merge --input .datapan/qnet-verification.json --input .datapan/epost-verification.json --input .datapan/ekape-verification.json --input .datapan/forest-verification.json --input .datapan/folk-verification.json --input .datapan/airport-verification.json --output .datapan/latest-verification.json --json
datapan catalog release draft --registry .datapan/data-go-kr.registry.json --previous-registry .datapan/previous.registry.json --verification .datapan/latest-verification.json --json
datapan catalog release verify --manifest .datapan/release/manifest.json --output .datapan/release/reports/latest-release-verification.json --json
datapan catalog release readiness --manifest .datapan/release/manifest.json --output .datapan/release/reports/latest-release-readiness.json --json
datapan catalog update data-go-kr --registry .datapan/data-go-kr.registry.json --json
datapan show "국토교통부_아파트 매매 실거래가 자료"
datapan auth check --json
datapan access 15126469 --purpose
datapan access 15126469 --open
datapan access 15126469 --start
datapan use 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan kit 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json
datapan use 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output-dir forecast-kit --json
datapan params 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.params.json
datapan get "기상청_단기예보 조회서비스" base_date=20260622 base_time=0500 nx=60 ny=127 --json
datapan get 15084084 --params-file forecast.params.json --timeout 5s --dry-run --json
datapan get 15084084 --dry-run --json
datapan curl 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan save 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --format csv --output forecast.csv
datapan export --format curl 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan export --format postman 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.postman_collection.json
datapan export --format openapi 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.openapi.json
datapan codegen go 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --package forecastclient --output forecast_client.go
datapan codegen node 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast_client.js
datapan codegen python 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast_client.py
datapan export --input response.json --format csv
datapan preview --input response.json --limit 10
datapan head --input forecast.csv --format csv --limit 5
```

`datapan access <list-id> --start` is the fast path for usage applications: it
opens the data.go.kr application page, copies the standard purpose text to the
clipboard when the OS supports it, prints the manual steps, and shows the smoke
command to run after approval.

`datapan kit <ref>` turns one selected API operation into a local starter kit
under `<dataset-id>-kit` by default: reusable params JSON, a curl script,
Postman collection, OpenAPI document, Go/Node/Python clients, and a small
README with next commands. Use `--output-dir DIR` when you want a custom
location. The generated files use environment-variable placeholders and never
write the service key into the kit. `datapan use <ref> --output-dir DIR` remains
available for callers that already build on the broader planning command.

Search can be narrowed with source metadata such as `--org`, `--category`,
`--priority`, and `--provider`. `provider` is the upstream platform such as
`data.go.kr`; `org` is the public agency or institution that provides the data.
`category` maps to the upstream source category only when that value is present
in the imported catalog; Datapan should not invent source categories.
Search and show results include copyable next-step examples for `show`, `use`,
starter-kit generation, `get`, `curl`, Postman export, OpenAPI export, and
Go/Node/Python codegen when an operation has an endpoint.
Generated examples fill common paging and response-format parameters such as
`pageNo`, `numOfRows`, `_type`, `dataType`, and `resultType` with safe starter
values, while unknown required parameters remain `VALUE`.
Search JSON also includes quick decision fields such as `callable`,
`call_ready`, `call_route`, `call_provider`, `endpoint_host`,
`default_operation`, approval metadata, data format, update date, and the
data.go.kr application URL when the registry has those upstream values.
`callable` means at least one endpoint exists; `call_ready` is stricter and
marks routes Datapan currently treats as stable for `get`, such as the
data.go.kr gateway or a call-capable provider adapter.
Credential parameters such as `serviceKey` and `authApiKey` stay out of those
examples because Datapan reads the key from environment variables.

To move beyond the embedded seed catalog, run `datapan init`. This is the
normal consumer path: it downloads the latest released `datapan-registry` zip,
extracts `data/data-go-kr.registry.json`, validates that the registry decodes,
writes it to `.datapan/data-go-kr.registry.json`, reports release manifest /
readiness / release-notes evidence when the zip includes those artifacts,
checks local credential presence, reports registered provider adapters, and
returns next commands.

```bash
datapan init --json
datapan status --json
datapan ready --limit 10 --json
datapan try "단기예보" base_date=20260622 --org 기상청 --json
datapan coverage --json
datapan studio --output-dir .datapan/studio --limit 200 --json
datapan providers --adapters --json
datapan providers --gaps --limit 10 --json
datapan targets --limit 10 --json
datapan ops --host openapi.jeonju.go.kr --limit 10 --json
datapan verify --host openapi.q-net.or.kr --limit 3 --json
datapan list --limit 10 --json
datapan list --callable --limit 10 --json
datapan list --call-ready --limit 10 --json
datapan catalog overview --json
datapan search "실거래" --org 국토교통부 --json
```

Use `datapan catalog install datapan-registry --json` when you want only the
registry download/install step, and `datapan status --json` or `datapan doctor --json` when you want to
recheck which registry is active, how many specs and operations it contains,
whether a data.go.kr API key is present, and which external provider adapters
are registered.
Use `datapan providers --adapters --json` when you want to see external hosts
already owned by registered provider adapters, and `datapan providers --gaps
--json` when you want the missing external endpoint host backlog without
remembering the longer `catalog providers --status missing --kind
external_endpoint` form. The JSON envelope includes `next_commands` so an
agent or human can jump directly from a provider host to `datapan targets`,
`datapan ops`, or bounded `datapan verify` commands.
Use `datapan targets --json` when you want the ranked adapter work queue
directly: target host, operation/spec counts, organizations, categories, formats,
and sample operations for the next external provider adapter decision.
Use `datapan ops --host HOST --json` when you want the exact operation-level
inventory behind a host: dataset ID, operation name, dependency class, adapter
status, approval state, parameter counts, and skip reason.
Use `datapan verify --host HOST --limit N --json` when you want bounded runtime
evidence for a registered adapter or gateway host without remembering the
longer `catalog verify` form.
Use `datapan list` or `datapan ls` when you want a data-CLI-style dataset list
without inventing a search term. They accept the same `--org`, `--category`,
`--priority`, `--provider`, `--callable`, `--call-ready`, `--limit`, and
`--json` options as `search`. `--callable` returns only specs that have at
least one operation endpoint, so it is the quickest path when you want
something Datapan can turn into `get`, `curl`, Postman, OpenAPI, or generated
client code. Check `call_ready` and `call_route` when you need the stronger
"Datapan has a stable call route" signal, or use `--call-ready` directly.
`--ready` is the shorter alias for interactive use, and `datapan ready` is a
top-level shortcut for `datapan list --call-ready`. Its default output is
ranked toward APIs with fewer required parameters and less action-like
operations, so the first screen is closer to "try this now."
Use `datapan try "query" KEY=VALUE --json` when you want Datapan to choose the
best call-ready match and return params, `get`, `save`, `curl`, Postman,
OpenAPI, and Go/Node/Python codegen commands in one response. It treats
`KEY=VALUE` tokens as parameter overrides, uses safe starter values for common
paging/format fields, and keeps auth parameters in environment variables. Add
`--any` only when you intentionally want callable but not-yet-ready routes
included in the selection.
Use `datapan coverage --json` when you want the high-level claim/gap dashboard:
registry size, callable coverage, external adapter coverage, provider split
readiness, and optional runtime evidence from `--verification REPORT`.
Use `datapan catalog overview --json` when you want a compact registry dashboard
for humans, agents, or a future Studio surface: total specs and operations,
organization/category counts, gateway/external/adapter coverage, top
organizations, top external hosts, missing adapter hosts, registered adapter
hosts, and suggested next commands.
Use `datapan coverage --json` or `datapan catalog coverage --json` when you want a claim-oriented coverage
and gap report. It combines registry coverage, callable operations, adapter
coverage, provider split readiness, top missing adapter hosts, and optional
runtime evidence from `--verification REPORT` into one agent-friendly response.
Use `datapan studio --output-dir DIR --json` or `datapan catalog studio
--output-dir DIR --json` when you want a static
consumer bundle for a future Studio or local viewer. It writes `overview.json`,
`datasets.json`, `studio.json`, and `index.html` with the same search examples,
starter-kit commands, provider readiness, and registry summary used by the CLI.

For registry maintainers and bounded upstream checks, import the upstream
data.go.kr open-data list into a normalized Datapan registry:

```bash
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --pages 5 --json
datapan search "실거래" --org 국토교통부 --json
```

The importer uses data.go.kr's public list lookup API and preserves upstream
metadata such as organization, source category, source keywords, operation
names, request parameters, response parameters, and raw source fields.
Use `--all` to continue fetching pages until the upstream `totalCount` has been
reached; use `--pages N` for a bounded sample or smoke test. `--all` has a
default `--max-pages 1000` guard that can be raised for unusually large
catalogs.

Commands that operate on one dataset accept a `<ref>`. A ref can be a
data.go.kr list ID, a data.go.kr detail URL, an exact title, or a search query.
If a query matches multiple datasets, Datapan stops and returns candidates
instead of guessing.

Use `datapan catalog diff` after a fresh import to inspect upstream catalog
changes before replacing an existing registry. It reports added, removed, and
changed specs by stable data.go.kr list ID and includes changed field names
under `--json`. With `--output`, it writes a `datapan.catalog-diff.v1`
report for update review, CI, or release-note generation.

Use `datapan catalog audit` to make registry gaps visible: total specs,
operations, callable operations, specs without operations, specs without
callable endpoints, dependency classes, and missing source metadata. Dependency
classes distinguish data.go.kr gateway operations, external endpoint operations,
gateway operations with external guide documents, service-root-only operations,
SOAP/WMS operations, approval-required operations, and malformed source URLs.
Audit counts remain operation/spec scoped, while sample buckets are bounded and
dataset-deduplicated so one multi-operation dataset does not hide other
examples. With `--output`, it writes a `datapan.catalog-audit.v1` report for
release or CI use.
Use `datapan catalog errors` to inventory provider status fields declared in
response parameters, such as `resultCode`, `resultMsg`, `returnReasonCode`,
`returnAuthMsg`, and `errMsg`. With `--output`, it writes a
`datapan.error-catalog.v1` report so verification, SDK, and Studio layers can
preserve upstream error/status semantics instead of inventing a separate error
taxonomy.
Use `datapan ops` or `datapan catalog dependencies` to list operation-level dependency
classifications. It keeps each operation's dataset ID, operation name,
endpoint/source/guide hosts, dependency class, provider family, approval state,
adapter status, and parameter counts together. This is the artifact to use when
an agent, UI, or SDK generator needs to know which exact operations are
gateway-hosted, externally hosted, service-root-only, unsupported, missing an
adapter, or owned by a registered adapter. With `catalog dependencies
--output`, it writes a `datapan.dependencies.v1` report.
Use `datapan targets` or `datapan catalog adapter-targets` to turn missing external/service-root
operations into a prioritized adapter work queue by host. It ranks target hosts
by operation coverage, includes provider family, kinds, organizations, formats,
approval and missing-parameter counts, and bounded sample operations. With
`catalog adapter-targets --output`, it writes a `datapan.adapter-targets.v1`
report for release, planning, or issue creation.
Use `datapan catalog providers` to turn those dependency classes into a
provider backlog by host. It reports gateway hosts, external endpoint hosts,
external guide hosts, registered adapter hosts, missing adapter hosts,
operations that still need adapters, and sample dataset IDs for each host. This
is the command to run before deciding which external provider adapter should be
built next. With `--output`, it writes a `datapan.providers.v1` report that can
be published by `datapan-registry`. Use `--status`, `--kind`, and
`--provider` to narrow the adapter backlog; `--status adapter` shows hosts with
registered external adapters such as airport, q-net, epost, ekape, forest, and
folk.
EPost and forest are call-capable external adapters, so `datapan get` can route
their matching operations through the provider boundary instead of treating them
as generic data.go.kr gateway calls.
After `datapan init`, catalog observation commands such as `catalog providers`,
`catalog dependencies`, `catalog adapter-targets`, and `catalog verify`
automatically use `.datapan/data-go-kr.registry.json` when `--registry` is not
provided.
Use `datapan verify` or `datapan catalog verify` to collect bounded runtime evidence. It attempts
only operations Datapan can call conservatively with known smoke/default/safe
paging parameters or a registered provider adapter, then records `verified`,
`failed`, or `skipped` with provider status, HTTP status, dependency class,
redacted URL, skip reasons, and the per-call timeout used for the run. Q-Net has
a narrow verification path for proven XML endpoints. Use `--timeout` to bound
each provider call, and use `--provider`, `--host`, and `--kind` to collect
bounded adapter evidence without blindly calling the whole catalog.
Use `datapan catalog verify plan --verification REPORT` to generate the next
bounded verification batches. The plan emits ready-to-run commands and includes
`--exclude-input REPORT` so repeated runs grow evidence instead of rechecking
operations already present in the current report.
Use `datapan catalog verify --input REPORT` to reread an existing verification
artifact and filter results by status without making new provider calls.
Use `datapan catalog verify summary --input REPORT` to turn verification
evidence into status, reason, provider, host, and dependency-class rollups.
Use `datapan catalog verify merge --input A --input B --output REPORT` to
combine bounded provider-specific verification runs into one release evidence
artifact without calling providers again. Failed and skipped results are kept;
they are evidence, not noise.
Use `datapan catalog release draft` to assemble a local registry release layout
from existing registry, optional previous-registry diff, provider index,
catalog audit, error catalog, dependency inventory, adapter targets, provider
backlog, schema, schema index, verification, verification summary, provenance,
release notes, and manifest artifacts without calling upstream APIs.
Use `datapan catalog release verify --manifest PATH --output REPORT` to recheck
the manifest's artifact paths, byte sizes, SHA-256 checksums, and schema-bound
artifact shapes before publishing and preserve a `datapan.release-verification.v1`
report.
Use `datapan catalog release readiness --manifest PATH --output REPORT` after
manifest verification to produce a `datapan.release-readiness.v1` gate report.
It checks whether the release contains the required registry, schema index,
provider index, catalog audit, error catalog, dependency inventory, adapter
target, provider backlog, and provenance artifacts, while treating catalog diff
and runtime verification evidence as recommended gates.
Use `datapan catalog update data-go-kr` for the safer update path. It imports
the full upstream catalog with bounded retries, diffs it against the current
registry, audits the new registry, and stays in dry-run mode unless `--apply`
is present. Add `--backup` with `--apply` to keep a timestamped copy of the
previous registry. Diff output is bounded by default; use `--diff-limit 0` when
a full machine-readable diff is needed.

`datapan show <ref> --json` is the bridge from discovery to use. It keeps the
normalized spec, and also returns access metadata, operation parameter names,
response-field counts, and copyable next-step examples where Datapan can
synthesize them from the imported data.go.kr spec.

`datapan use <ref> KEY=VALUE --json` is the shortest non-calling handoff from a
dataset to action. It resolves the dataset, selects an operation, merges
defaults, smoke values, `--params-file`, positional `KEY=VALUE`, and
`--param k=v` overrides, then returns the exact params plus copyable commands
for params-file creation, dry-run, get, CSV save, curl, Postman, OpenAPI, and
Go/Node/Python codegen. It does not call the provider or print credential
values.

`datapan params <ref> KEY=VALUE --output params.json` writes a reusable JSON
object for `--params-file`. The template keeps exact upstream parameter names,
omits auth parameters, fills known defaults, smoke values, and supplied
overrides, and leaves unknown values as `VALUE` so humans and agents can edit
one small file instead of memorizing long command lines.

`datapan get` treats HTTP failures, data.go.kr provider errors such as non-`00`
`resultCode`, and HTML service pages as request failures. JSON output preserves
provider error fields under `provider_status`, including `resultCode/resultMsg`
or `OpenAPI_ServiceResponse` fields such as `returnReasonCode`,
`returnAuthMsg`, and `errMsg` when they appear in the response body.
Use `--timeout 5s` or `--timeout 500ms` on `get`, `call`, `save`, and
CSV/JSON `export` when an external provider is slow or unstable. A bare integer
such as `--timeout 5` is interpreted as seconds.
Use `datapan curl <ref>` when you want a copyable request without making a
provider call. It emits a `curl -fsS ...` command with `serviceKey=${ENV_VAR}`
instead of printing credential values. `datapan export --format curl <ref>` is
the same planner through the export surface.
Use `datapan export --format postman <ref> --output collection.json` to write a
Postman Collection v2.1 file for the same planned request. It stores
`serviceKey` as a Postman variable such as `{{DATA_PORTAL_API_KEY}}`, not as a
credential value.
Use `datapan export --format openapi <ref> --output openapi.json` to write an
OpenAPI 3.1 document for the same planned request. It exposes the provider
endpoint, query parameters, response fields, and a `serviceKey` apiKey security
scheme with an environment-variable placeholder such as
`${DATA_PORTAL_API_KEY}`. This is the first bridge toward SDK generation and
Studio-style tooling without hand-writing one wrapper per API.
Use `datapan codegen go <ref> --output client.go` to generate a small
compilable Go client for one operation. The generated client keeps public-data
parameter names as `map[string]string`, reads the service key from the planned
environment variable via `NewFromEnv`, and does not embed credential values.
Use `datapan codegen node <ref> --output client.js` for a dependency-free
Node.js client using the built-in `fetch` available in Node.js 18+. It keeps
upstream parameter names exact, reads the service key through
`DatapanClient.fromEnv()`, and does not embed credential values.
Use `datapan codegen python <ref> --output client.py` for a dependency-free
Python client using `urllib`. It keeps upstream parameter names exact, reads
the service key through `DatapanClient.from_env()`, and does not embed
credential values.
Use `datapan preview --input response.json` or `datapan head --input rows.csv`
to inspect saved data without leaving the CLI. It accepts data.go.kr response
JSON, Datapan row JSON, or CSV, prints a compact table by default, and returns
`columns`, `count`, limited `rows`, and `truncated` under `--json`.

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
| 5 | ambiguous dataset ref |

## Scope

The embedded seed catalog is intentionally small. For broader coverage, install
the released `datapan-registry` snapshot into `.datapan/data-go-kr.registry.json`.
Consumer commands such as `search`, `show`, `get`, and `export` automatically
load that default file from the current project directory. Set
`DATAPAN_REGISTRY_PATH` only when you want to use a different registry file.

Datapan CLI is the first repository, not the whole planned ecosystem. The
longer-term shape is a public-data layer made of the CLI runtime, normalized
registry releases, provider adapters, verification evidence, developer exports,
and eventually a Studio UI. See `docs/ecosystem.md`.

The first schema drafts live in `schemas/`:

- `datapan.specs.v1.schema.json` for normalized registry files;
- `datapan.dependencies.v1.schema.json` for operation-level dependency inventories;
- `datapan.adapter-targets.v1.schema.json` for adapter work queue reports;
- `datapan.provider-index.v1.schema.json` for registered provider adapter indexes;
- `datapan.catalog-diff.v1.schema.json` for registry update diff reports;
- `datapan.error-catalog.v1.schema.json` for upstream provider status field inventories;
- `datapan.catalog-audit.v1.schema.json` for registry gap audit reports;
- `datapan.providers.v1.schema.json` for provider backlog reports;
- `datapan.verification.v1.schema.json` for runtime evidence reports;
- `datapan.verification-summary.v1.schema.json` for verification rollups;
- `datapan.release-manifest.v1.schema.json` for release artifact manifests;
- `datapan.release-verification.v1.schema.json` for release verification reports;
- `datapan.release-readiness.v1.schema.json` for registry publication gate reports;
- `datapan.schema-index.v1.schema.json` for the release schema index at
  `schemas/index.json`.

See `docs/registry-release.md` for the local draft layout and release gates for
the published `datapan-registry` repository:
`https://github.com/StatPan/datapan-registry`.

## Non-Goals For The MVP

- No hosted server dependency.
- No UI/TUI.
- No CAPTCHA bypass or hidden provider-security workaround.
- No credential printing or storage.
- No claim that every imported data.go.kr operation is immediately callable
  without per-service approval or further endpoint cleanup.

## License

Apache-2.0
