# Datapan CLI

Datapan CLI is an open-source, agent-friendly command-line tool for Korean
public data. The long-term goal is not to resell public data; it is to own the
developer and agent experience around discovering, applying for, calling,
verifying, exporting, caching, and generating clients for public-data APIs.

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
- browser automation only for explicit `datapan access login`, access inspection,
  and `--apply` workflows.

## Coverage Goals

Datapan's open-source coverage target is not just "many catalog rows." The
project tracks whether public APIs can be discovered, routed, called, exported,
and verified from a local machine.

Snapshot counts, layered coverage denominators, evidence freshness, remaining
risks, and consumer compatibility belong to each `datapan-registry` release.
The CLI does not duplicate one release's numbers as permanent product truth.
Install a release and inspect its evidence instead:

```bash
datapan init --json
datapan status --json
datapan coverage --json
```

`status` verifies the installed registry provenance and exposes the release's
CLI compatibility and manual-review boundary. `coverage` combines the active
registry with preserved release evidence while keeping callable, adapter, and
runtime verification coverage distinct.

## Install

Windows PowerShell:

```powershell
$script = "$env:TEMP\datapan-install.ps1"; iwr https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.ps1 -OutFile $script; powershell -ExecutionPolicy Bypass -File $script
```

Linux/macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/StatPan/datapan-cli/main/scripts/install.sh | sh
```

The installers download the latest GitHub Release archive, verify
`checksums.txt`, install `datapan` and the optional `dp` alias into
`$HOME/.datapan/bin`, and print `datapan version --json`.
To pin a specific release, download the script first and pass `-Version v0.1.1`
on PowerShell, or set `DATAPAN_VERSION=v0.1.1` for the shell installer.

From source:

```bash
go install github.com/StatPan/datapan-cli/cmd/datapan@latest
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

Maintainers can smoke-test the public Windows first-run path from a release archive:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/smoke-first-run.ps1 -Version latest
```

The scheduled `Registry Journey` workflow builds the current source on Linux,
macOS, and Windows, installs the latest public `datapan-registry` release, and
checks the offline-safe path from init and integrity status through ready,
search, show, try, SHA-bound params generation, params-file redacted dry-run,
SHA-bound standalone OpenAPI export, and starter-kit generation. It checks
generated files for credential leakage and uploads a compact journey result,
params and export artifacts, and generated provenance as CI evidence. The same
check can be run against a local binary with PowerShell 7:

```powershell
pwsh scripts/smoke-registry-journey.ps1 -Datapan ./datapan -KeepWorkDir
```

Tagged releases are published only after tests, vet, and command builds pass on
Linux, macOS, and Windows. After publication, the `Smoke Release` workflow uses
the public installer on all three operating systems, verifies both `datapan`
and `dp`, runs the latest Registry journey against the installed release, and
retains the journey summary and generated provenance for 30 days. The Unix
installer accepts both `asset` and `./asset` checksum filename formats emitted
by common `sha256sum` workflows.
The tag workflow itself also runs an exact-version public smoke after publish:
each operating system installs `GITHUB_REF_NAME`, requires both binary version
fields to equal that tag, runs the Registry journey, and retains exact-tag
evidence. The separately scheduled Smoke Release workflow remains a latest
release drift check rather than the authoritative tag binding.
Before publication, each Linux amd64, macOS Intel, and Windows amd64 archive
smoke requires both packaged executables to exist and requires `datapan version`
and `dp version` to equal the tag before starting the Registry journey.

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
datapan catalog coverage --verification .datapan/latest-verification.json --route-disposition .datapan/route-disposition.json --json
datapan catalog studio --output-dir .datapan/studio --limit 200 --json
datapan catalog import data-go-kr --output .datapan/data-go-kr.registry.json --all --json
datapan catalog diff --old .datapan/previous.registry.json --new .datapan/data-go-kr.registry.json --output .datapan/catalog-diff.json --json
datapan catalog audit --registry .datapan/data-go-kr.registry.json --json
datapan catalog errors --registry .datapan/data-go-kr.registry.json --output .datapan/error-catalog.json --json
datapan catalog dependencies --registry .datapan/data-go-kr.registry.json --kind external_endpoint --status missing --output .datapan/dependencies.json --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --output .datapan/adapter-targets.json --json
datapan catalog route-disposition --registry .datapan/data-go-kr.registry.json --probe .datapan/unadapted-external-probe.json --output .datapan/route-disposition.json --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --output .datapan/provider-backlog.json --json
datapan catalog verify plan --registry .datapan/data-go-kr.registry.json --verification .datapan/latest-verification.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15084084 --timeout 10s --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --kind data_go_kr_gateway --exclude-input .datapan/latest-verification.json --limit 20 --timeout 10s --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --kind external_endpoint --probe-unadapted --timeout 12s --output .datapan/unadapted-external-probe.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider airport --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider andong --kind external_endpoint --limit 15 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider q-net --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider ekape --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider forest --kind external_endpoint --limit 4 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider folk --kind external_endpoint --limit 3 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider gblib --kind external_endpoint --limit 3 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider jeonju --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider geoje --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider humetro --kind external_endpoint --limit 8 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider itfind --kind external_endpoint --limit 13 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider jeju --kind external_endpoint --limit 4 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider korad --kind external_endpoint --limit 15 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider kpx --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider lh-ebid --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider myhome --kind external_endpoint --limit 1 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider emuseum --kind external_endpoint --limit 3 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider naqs --kind external_endpoint --limit 9 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider oneclick-law --kind external_endpoint --limit 30 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider pqis --kind external_endpoint --limit 4 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider seoul-bus --kind external_endpoint --limit 5 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider sisul --kind external_endpoint --limit 20 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider tour --limit 26 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider uiryeong --kind external_endpoint --limit 6 --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider ulsan --kind external_endpoint --limit 6 --json
datapan catalog verify --input .datapan/latest-verification.json --status failed --json
datapan catalog verify summary --input .datapan/qnet-batch-verification.json --json
datapan catalog verify merge --input .datapan/qnet-verification.json --input .datapan/epost-verification.json --input .datapan/ekape-verification.json --input .datapan/emuseum-verification.json --input .datapan/forest-verification.json --input .datapan/folk-verification.json --input .datapan/gblib-verification.json --input .datapan/airport-verification.json --input .datapan/andong-verification.json --input .datapan/jeju-verification.json --input .datapan/jeonju-verification.json --input .datapan/geoje-verification.json --input .datapan/humetro-verification.json --input .datapan/itfind-verification.json --input .datapan/korad-verification.json --input .datapan/kpx-verification.json --input .datapan/lh-ebid-verification.json --input .datapan/myhome-verification.json --input .datapan/naqs-verification.json --input .datapan/oneclick-law-verification.json --input .datapan/pqis-verification.json --input .datapan/seoul-bus-verification.json --input .datapan/sisul-verification.json --input .datapan/tour-verification.json --input .datapan/uiryeong-verification.json --input .datapan/ulsan-verification.json --output .datapan/latest-verification.json --json
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
datapan sync 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json
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
`datapan try` and `datapan use` also return ordered `next_steps`, so agents and
humans can move from params to dry-run, real call, CSV save, exports, codegen,
starter kit, status, and coverage without reconstructing the workflow.

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
normal consumer path: it resolves the latest public
`StatPan/datapan-registry` Hugging Face Dataset commit, downloads the Registry
distribution pointer, follows its immutable payload revision, downloads the
Registry and required release contracts only from that revision, verifies
every distribution byte count and SHA-256 plus the canonical release manifest,
and validates that the registry decodes,
writes it to `.datapan/data-go-kr.registry.json`, reports release manifest /
readiness / route-disposition / release-notes evidence when the zip includes those artifacts,
stores the key release evidence files under `.datapan/release` for follow-up commands,
checks local credential presence, reports registered provider adapters, and
returns next commands.

```bash
datapan init --json
datapan status --json
datapan ready --limit 10 --json
datapan try "단기예보" base_date=20260622 --org 기상청 --json
datapan coverage --json
datapan studio --output-dir .datapan/studio --limit 200 --json
datapan providers --split --json
datapan providers --adapters --json
datapan providers --gaps --limit 10 --json
datapan targets --limit 10 --json
datapan ops --host www.andong.go.kr --limit 10 --json
datapan ops --host openapi.jeonju.go.kr --limit 10 --json
datapan ops --host data.geoje.go.kr --limit 10 --json
datapan ops --host data.humetro.busan.kr --limit 10 --json
datapan ops --host open.itfind.or.kr --limit 10 --json
datapan ops --host openapi.gblib.or.kr --limit 10 --json
datapan ops --host www.korad.or.kr --limit 10 --json
datapan ops --host openapi.ebid.lh.or.kr --limit 10 --json
datapan ops --host data.naqs.go.kr --limit 10 --json
datapan ops --host oneclick.law.go.kr --limit 10 --json
datapan ops --host openapi.pqis.go.kr --limit 10 --json
datapan ops --host ws.bus.go.kr --limit 10 --json
datapan ops --host data.sisul.or.kr --limit 10 --json
datapan ops --host openapi.tour.go.kr --limit 10 --json
datapan ops --host data.uiryeong.go.kr --limit 10 --json
datapan verify --host openapi.q-net.or.kr --limit 3 --json
datapan verify --host www.andong.go.kr --limit 15 --json
datapan verify --host data.geoje.go.kr --limit 6 --json
datapan verify --host data.humetro.busan.kr --limit 8 --json
datapan verify --host open.itfind.or.kr --limit 13 --json
datapan verify --host openapi.gblib.or.kr --limit 3 --json
datapan verify --host www.korad.or.kr --limit 15 --json
datapan verify --host openapi.ebid.lh.or.kr --limit 6 --json
datapan verify --host data.naqs.go.kr --limit 9 --json
datapan verify --host oneclick.law.go.kr --limit 30 --json
datapan verify --host openapi.pqis.go.kr --limit 4 --json
datapan verify --host ws.bus.go.kr --limit 5 --json
datapan verify --host data.sisul.or.kr --limit 20 --json
datapan verify --host openapi.tour.go.kr --limit 26 --json
datapan verify --host data.uiryeong.go.kr --limit 6 --json
datapan list --limit 10 --json
datapan list --callable --limit 10 --json
datapan list --call-ready --limit 10 --json
datapan catalog overview --json
datapan search "실거래" --org 국토교통부 --json
```

Use `datapan catalog install datapan-registry --json` when you want only the
registry download/install step. It also preserves key release evidence files
under `.datapan/release` when installing to a file, so follow-up coverage
commands can reuse the published verification and route-disposition evidence.
It writes `.datapan/registry-install.json` with the Dataset ID and immutable
revision, source URLs, Registry and manifest SHA-256 values, install mode,
shard evidence, and CLI consumer compatibility
known at install time. Registry data, release evidence, and provenance are
staged and replaced as one recoverable install transaction. A commit failure
restores the previous local state, and a release without evidence clears stale
evidence from an older installation. Before replacing any target, the CLI
syncs `.datapan/registry-install.transaction.json`. If the process or machine
stops before the journal is removed at the commit point, the next non-help CLI
command restores the previous complete Registry, evidence, and provenance set
before loading Registry data. Invalid or unrecoverable journals stop execution
with a structured recovery error instead of guessing which partial state is
safe. Status reports when that invocation recovered an interrupted install.
Use `datapan status --json` or `datapan doctor --json` when you want to
recheck which registry is active, how many specs and operations it contains,
whether a data.go.kr API key is present, and which external provider adapters
are registered. After `datapan init`, status also reports the installed
release evidence files and the key verification, route-disposition, and
coverage numbers they contain. The `registry_release` block verifies that the
active registry path and digest still match the install provenance, checks the
latest Dataset metadata without replacing local files, and preserves CLI
compatibility and runtime manual-review boundaries. A failed online freshness
check does not make an intact installed registry unusable.
New Registry releases may include the manifest-bound sustainable coverage
policy and report. The CLI preserves them and classifies each verification
record against the policy's 30/90-day-style thresholds using
`manifest.generated_at`, not the current machine clock. Stale or expired
evidence is surfaced as a `stale_verification` warning with next actions; it is
not silently treated as current and does not block a new provider request
unless a future Registry consumer contract explicitly requires that behavior.
The CLI also preserves `release-consumer-decision.json`. A decision that tells
`datapan-cli` to consume the canonical Registry remains executable even when
release adoption still requires manual review; the manual-review boundary and
Registry-provided reason remain visible. A blocked decision, unsupported CLI
action, or locally modified decision artifact stops provider HTTP as a
`compatibility` failure.
Manifest-bound Registry error-action catalogs also refine failed calls. JSON
preserves the matched rule, Registry classification, severity, scoped actions,
action reasons, impact categories, and the source runtime manual-review
boundary under `failure.registry_routing`. The CLI uses only verified rules and
does not apply a locally modified catalog; an integrity error is reported while
the built-in failure classification remains intact.
Use `datapan providers --adapters --json` when you want to see external hosts
already owned by registered provider adapters, and `datapan providers --gaps
--json` when you want the missing external endpoint host backlog without
remembering the longer `catalog providers --status missing --kind
external_endpoint` form. The JSON envelope includes `next_commands` so an
agent or human can jump directly from a provider host to `datapan targets`,
`datapan ops`, or bounded `datapan verify` commands.
Use `datapan providers --split --json` when you want the narrower split
decision: registered adapter counts, verification/call-capable adapter
readiness, external adapter coverage, optional verification evidence, and the
next provider commands. It does not call upstream providers.
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
OpenAPI, Go/Node/Python codegen commands, and ordered `next_steps` in one response. It treats
`KEY=VALUE` tokens as parameter overrides, uses safe starter values for common
paging/format fields, and keeps auth parameters in environment variables. Add
`--any` only when you intentionally want callable but not-yet-ready routes
included in the selection.
Use `datapan coverage --json` when you want the high-level claim/gap dashboard:
registry size, callable coverage, external adapter coverage, provider split
readiness, optional runtime evidence from `--verification REPORT`, and optional
route evidence from `--route-disposition REPORT`. After `datapan init` or a
file install, it automatically reads
`.datapan/release/reports/latest-verification.json` and
`.datapan/release/reports/route-disposition.json` when those files are present.
Use `datapan catalog overview --json` when you want a compact registry dashboard
for humans, agents, or a future Studio surface: total specs and operations,
organization/category counts, gateway/external/adapter coverage, top
organizations, top external hosts, missing adapter hosts, registered adapter
hosts, and suggested next commands.
Use `datapan coverage --json` or `datapan catalog coverage --json` when you want a claim-oriented coverage
and gap report. It combines registry coverage, callable operations, adapter
coverage, provider split readiness, top missing adapter hosts, optional runtime
evidence from `--verification REPORT`, and evidence-adjusted missing route
counts from `--route-disposition REPORT` into one agent-friendly response. The
default installed release evidence under `.datapan/release/reports` is loaded
automatically when explicit report paths are omitted.
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
Add `--enrich-link-details` when refreshing LINK-style rows that have no
operation metadata in the list API. The importer then fetches the corresponding
data.go.kr detail pages and materializes external 활용 links as operations;
use `--enrich-limit N` for bounded batches while expanding coverage.
When a current registry snapshot already exists and a data.go.kr list API key is
not available, use `datapan catalog enrich link-details --registry PATH
--output PATH --json` to apply the same detail-page operation enrichment without
re-importing the upstream list API.

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
Use `datapan catalog route-disposition` after an unadapted probe when you need
to separate stale routes, transient transport failures, parameter-blocked
routes, and real adapter candidates. With `--output`, it writes a
`datapan.route-disposition.v1` report that keeps each missing route tied to
dataset ID, operation, host, probe evidence, and recommended next action.
Use `datapan catalog providers` to turn those dependency classes into a
provider backlog by host. It reports gateway hosts, external endpoint hosts,
external guide hosts, registered adapter hosts, missing adapter hosts,
operations that still need adapters, and sample dataset IDs for each host. This
is the command to run before deciding which external provider adapter should be
built next. With `--output`, it writes a `datapan.providers.v1` report that can
be published by `datapan-registry`. Use `--status`, `--kind`, and
`--provider` to narrow the adapter backlog; `--status adapter` shows hosts with
registered external adapters such as airport, q-net, epost, ekape, emuseum,
forest, folk, andong, gblib, humetro, itfind, jeju, jeonju, geoje, korad, kpx,
lh-ebid, myhome, naqs, oneclick-law, pqis,
seoul-bus, sisul, tour, uiryeong, and ulsan.
Andong, EPost, eMuseum, forest, gblib, geoje, Humetro, itfind, Jeju, KORAD, KPX, lh-ebid, MyHome, NAQS, oneclick-law, PQIS,
seoul-bus, sisul, tour, uiryeong, and ulsan are call-capable external adapters, so `datapan get` can route
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
bounded adapter evidence without blindly calling the whole catalog. Add
`--probe-unadapted` when auditing external endpoints without a registered
adapter; Datapan performs a credential-free GET probe and records DNS, timeout,
HTTP 404, HTTP 503, and other transport failures as explicit verification
evidence instead of leaving them as unknown skips.
Live verification uses the same Registry trust gate as get and sync, and stops
before adapter, probe, or provider HTTP when installed integrity, readiness, or
consumer compatibility is blocked. JSON includes `registry_trust`. Offline
report filtering, summary, merge, and planning remain available while blocked.
Transport evidence redacts both raw and URL-encoded credential values.
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
backlog, route disposition, schema, schema index, verification, verification
summary, optional unadapted external probe evidence, provenance, release notes,
and manifest artifacts without calling upstream APIs.
Use `datapan catalog release verify --manifest PATH --output REPORT` to recheck
the manifest's artifact paths, byte sizes, SHA-256 checksums, and schema-bound
artifact shapes before publishing and preserve a `datapan.release-verification.v1`
report.
Use `datapan catalog release readiness --manifest PATH --output REPORT` after
manifest verification to produce a `datapan.release-readiness.v1` gate report.
It checks whether the release contains the required registry, schema index,
provider index, catalog audit, error catalog, dependency inventory, adapter
target, route disposition, provider backlog, and provenance artifacts, while
treating catalog diff and runtime verification evidence as recommended gates.
When coverage still has missing external adapter operations, readiness requires
`reports/unadapted-external-probe.json` and its summary so those endpoints are
documented with bounded probe evidence instead of being silent unknowns.
Use `datapan catalog update data-go-kr` for the safer update path. It imports
the full upstream catalog with bounded retries, diffs it against the current
registry, audits the new registry, and stays in dry-run mode unless `--apply`
is present. Add `--backup` with `--apply` to keep a timestamped copy of the
previous registry. Add `--enrich-link-details` to include LINK detail-page
operation enrichment in the refreshed registry. Diff output is bounded by
default; use `--diff-limit 0` when a full machine-readable diff is needed.

`datapan show <ref> --json` is the bridge from discovery to use. It keeps the
normalized spec, and also returns access metadata, operation parameter names,
response-field counts, and copyable next-step examples where Datapan can
synthesize them from the imported data.go.kr spec.

Human `datapan show <ref>` presents the same approval, authentication,
parameter, source, and response-field facts. Each operation separately reports
its endpoint host, call-ready decision, gateway or adapter route, provider
owner, and verification freshness, so an unadapted external URL is not mistaken
for an executable operation.

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
File output also creates `<output>.datapan-provenance.json`, binding the params
file SHA256 to the dataset, operation, Registry trust, and verification without
copying parameter names, values, or credentials. Use `--provenance-output` for
a custom sidecar. Raw stdout stays a pure params object and sends evidence
diagnostics to stderr.

`datapan get` treats HTTP failures, data.go.kr provider errors such as non-`00`
`resultCode`, and HTML service pages as request failures. JSON output preserves
provider error fields under `provider_status`, including `resultCode/resultMsg`
or `OpenAPI_ServiceResponse` fields such as `returnReasonCode`,
`returnAuthMsg`, and `errMsg` when they appear in the response body.
Failed call and sync JSON also includes `failure.category`, a stable reason,
whether retry is appropriate, and concrete next steps. This separates
credential rejection, missing approval, invalid input, adapter/response-shape
problems, and external provider outages without hiding the underlying provider
status. Credentials are redacted from transport error text.
Without `--json`, failures print the same category, reason, and next actions to
stderr so provider data on stdout remains pipeable. Human dry-run uses stderr
for Registry trust and verification freshness while leaving only the redacted
GET request on stdout. Successful human get and sync use the same stderr trust
channel, leaving the provider body or sync summary on stdout unchanged.
If a provider reflects the active credential in a response body, message, or
provider-status field, the CLI replaces both the raw and URL-encoded value with
`REDACTED` before human or JSON output, CSV saving, row extraction, or sync cache
writes. Public response data otherwise remains unchanged.
Use `--timeout 5s` or `--timeout 500ms` on `get`, `call`, `save`, and
CSV/JSON `export` when an external provider is slow or unstable. A bare integer
such as `--timeout 5` is interpreted as seconds.
`save --json` preserves the selected dataset and operation, Registry trust,
verification status, and stale-evidence warning from the underlying call.
Human save keeps those diagnostics on stderr so raw CSV or JSON stdout remains
pipeable; failed saves retain the same failure category and next actions as
`get`.
Call-based CSV and JSON export preserves the same dataset, operation, Registry
trust, verification, and stale warning in JSON summaries. Human export leaves
rows on stdout, sends evidence to stderr, and renders provider failure
classification and next actions instead of failing silently.
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
Postman, OpenAPI, Go, Node, and Python outputs written to files also create a
`<output>.datapan-provenance.json` sidecar by default. The sidecar binds the
artifact path, byte count, SHA-256, dataset, operation, Registry trust, and
verification evidence without copying credentials. Use
`--provenance-output PATH` to choose another sidecar path. Raw stdout output
keeps its existing artifact-only contract and does not create a sidecar. File
and sidecar replacement is transactional for command failures, so an update
does not leave a new artifact paired with old provenance.
JSON summaries for curl and every standalone generator include the same
Registry trust, operation verification, and stale-evidence warning. Human
generation keeps the copyable command or artifact alone on stdout and writes
that evidence context to stderr.
Use `datapan sync <ref> --json` when you want one approved API call cached as
local files under `.datapan/cache`: request params without credentials,
`response.json`, extracted `rows.json`/`rows.csv` when possible, and a
`manifest.json` with status and provenance. This is the first local cache/sync
surface; it keeps repeatable public-data work in the project directory instead
of forcing every script or agent to call the upstream provider again. The
manifest also records `integrity`: extracted row count, upstream
`currentCount`/`totalCount`-style counters when present, and warnings such as
`row_count_exceeds_total_count` when provider metadata and actual rows disagree.
It preserves the operation verification and stale-evidence warning used for
the call, and records byte counts and SHA256 values for params, response, and
extracted row artifacts so cached bytes can be checked against their generation
manifest.
One sync is staged as a complete cache generation before replacing its output
directory. Reusing a directory removes stale files from the prior generation;
if the commit fails during the command, the previous directory is restored and
temporary staging and backup directories are removed. JSON reports such local
failures as `sync_cache_write_failed` with trust, verification, and recovery
steps.
Use `datapan preview --input response.json` or `datapan head --input rows.csv`
to inspect saved data without leaving the CLI. It accepts data.go.kr response
JSON, Datapan row JSON, or CSV, prints a compact table by default, and returns
`columns`, `count`, limited `rows`, and `truncated` under `--json`.
When the input belongs to a sync directory, preview and input-based export first
verify its byte count and SHA256 against the sibling manifest. Mismatched,
unlisted, or invalid-manifest cache files fail as `cache_integrity_failed`
before rows or CSV are emitted. Ordinary files and stdin remain usable without
a sync manifest.

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

Opening a Registry-provided application URL and both browser-backed inspection
and submission require an execution-allowed local Registry. They stop before
opening or starting a browser when the Registry trust decision blocks execution,
including provenance, integrity, readiness, or consumer-compatibility failures.
Their JSON results include `registry_trust`. Purpose-text display and copying
remain available for diagnosis, and `access login` remains available because it
uses the provider's fixed login page rather than a Registry-provided dataset
URL.

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
longer-term shape is a public-data DX layer made of the CLI runtime, normalized
registry releases, provider adapters, verification evidence, developer exports,
SDK/codegen surfaces, MCP, cache/sync behavior, and eventually a Studio UI. See
`docs/ecosystem.md` and `docs/product-positioning.md`.

The first schema drafts live in `schemas/`:

- `datapan.specs.v1.schema.json` for normalized registry files;
- `datapan.dependencies.v1.schema.json` for operation-level dependency inventories;
- `datapan.adapter-targets.v1.schema.json` for adapter work queue reports;
- `datapan.route-disposition.v1.schema.json` for missing external route disposition reports;
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
See `docs/goal-completion-audit.md` for the requirement-by-requirement evidence
boundary and the remaining conditions before this product goal can be marked
complete.

## Non-Goals For The MVP

- No hosted server dependency.
- No UI/TUI.
- No CAPTCHA bypass or hidden provider-security workaround.
- No credential printing or storage.
- No claim that every imported data.go.kr operation is immediately callable
  without per-service approval or further endpoint cleanup.

## License

Apache-2.0
