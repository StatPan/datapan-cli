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
datapan search "아파트 실거래가" --json
datapan search "실거래" --org 국토교통부 --json
datapan search --org 기상청 --json
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --all --json
datapan catalog diff --old .datapan/previous.registry.json --new .datapan/data-go-kr.registry.json --output .datapan/catalog-diff.json --json
datapan catalog audit --registry .datapan/data-go-kr.registry.json --json
datapan catalog errors --registry .datapan/data-go-kr.registry.json --output .datapan/error-catalog.json --json
datapan catalog dependencies --registry .datapan/data-go-kr.registry.json --kind external_endpoint --status missing --output .datapan/dependencies.json --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --output .datapan/adapter-targets.json --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --output .datapan/provider-backlog.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15084084 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider q-net --kind external_endpoint --limit 5 --json
datapan catalog verify --input .datapan/latest-verification.json --status failed --json
datapan catalog verify summary --input .datapan/qnet-batch-verification.json --json
datapan catalog release draft --registry .datapan/data-go-kr.registry.json --previous-registry .datapan/previous.registry.json --verification .datapan/latest-verification.json --json
datapan catalog release verify --manifest .datapan/release/manifest.json --output .datapan/release/reports/latest-release-verification.json --json
datapan catalog release readiness --manifest .datapan/release/manifest.json --output .datapan/release/reports/latest-release-readiness.json --json
datapan catalog update data-go-kr --registry .datapan/data-go-kr.registry.json --json
datapan show "국토교통부_아파트 매매 실거래가 자료"
datapan auth check --json
datapan access 15126469 --purpose
datapan access 15126469 --open
datapan access 15126469 --start
datapan get "기상청_단기예보 조회서비스" base_date=20260622 base_time=0500 nx=60 ny=127 --json
datapan get 15084084 --dry-run --json
datapan curl 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan save 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --format csv --output forecast.csv
datapan export --format curl 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan export --format postman 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.postman_collection.json
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
Use `datapan catalog dependencies` to list operation-level dependency
classifications. It keeps each operation's dataset ID, operation name,
endpoint/source/guide hosts, dependency class, provider family, approval state,
adapter status, and parameter counts together. This is the artifact to use when
an agent, UI, or SDK generator needs to know which exact operations are
gateway-hosted, externally hosted, service-root-only, unsupported, missing an
adapter, or owned by a registered adapter. With `--output`, it writes a
`datapan.dependencies.v1` report.
Use `datapan catalog adapter-targets` to turn missing external/service-root
operations into a prioritized adapter work queue by host. It ranks target hosts
by operation coverage, includes provider family, kinds, organizations, formats,
approval and missing-parameter counts, and bounded sample operations. With
`--output`, it writes a `datapan.adapter-targets.v1` report for release,
planning, or issue creation.
Use `datapan catalog providers` to turn those dependency classes into a
provider backlog by host. It reports gateway hosts, external endpoint hosts,
external guide hosts, registered adapter hosts, missing adapter hosts,
operations that still need adapters, and sample dataset IDs for each host. This
is the command to run before deciding which external provider adapter should be
built next. With `--output`, it writes a `datapan.providers.v1` report that can
later be published by `datapan-registry`. Use `--status`, `--kind`, and
`--provider` to narrow the adapter backlog; `--status adapter` shows hosts with
registered external adapters such as q-net.
Use `datapan catalog verify` to collect bounded runtime evidence. It attempts
only operations Datapan can call conservatively with known smoke/default/safe
paging parameters or a registered provider adapter, then records `verified`,
`failed`, or `skipped` with provider status, HTTP status, dependency class,
redacted URL, and skip reasons. Q-Net has a narrow verification path for proven
XML endpoints. Use `--provider`, `--host`, and `--kind` to collect bounded
adapter evidence without blindly calling the whole catalog.
Use `datapan catalog verify --input REPORT` to reread an existing verification
artifact and filter results by status without making new provider calls.
Use `datapan catalog verify summary --input REPORT` to turn verification
evidence into status, reason, provider, host, and dependency-class rollups.
Use `datapan catalog release draft` to assemble a local registry release layout
from existing registry, optional previous-registry diff, provider index,
catalog audit, error catalog, dependency inventory, adapter targets, provider
backlog, schema, schema index, verification, verification summary, provenance,
and manifest artifacts without calling upstream APIs.
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
response-field counts, and a copyable `datapan get ...` example where Datapan
can synthesize one from the imported data.go.kr spec.

`datapan get` treats HTTP failures, data.go.kr provider errors such as non-`00`
`resultCode`, and HTML service pages as request failures. JSON output preserves
provider error fields under `provider_status`, including `resultCode/resultMsg`
or `OpenAPI_ServiceResponse` fields such as `returnReasonCode`,
`returnAuthMsg`, and `errMsg` when they appear in the response body.
Use `datapan curl <ref>` when you want a copyable request without making a
provider call. It emits a `curl -fsS ...` command with `serviceKey=${ENV_VAR}`
instead of printing credential values. `datapan export --format curl <ref>` is
the same planner through the export surface.
Use `datapan export --format postman <ref> --output collection.json` to write a
Postman Collection v2.1 file for the same planned request. It stores
`serviceKey` as a Postman variable such as `{{DATA_PORTAL_API_KEY}}`, not as a
credential value.

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

The embedded seed catalog is intentionally small. For broader coverage, import
the data.go.kr open-data list into a local normalized registry and point
`DATAPAN_REGISTRY_PATH` at that file.

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
a future `datapan-registry` repository.

## Non-Goals For The MVP

- No hosted server dependency.
- No UI/TUI.
- No CAPTCHA bypass or hidden provider-security workaround.
- No credential printing or storage.
- No claim that every imported data.go.kr operation is immediately callable
  without per-service approval or further endpoint cleanup.

## License

Apache-2.0
