# Datapan Ecosystem

Datapan should become the public-data layer that developers, agents, analysts,
and later products can trust. The CLI is the first executable surface, but the
durable asset is the normalized registry, provider knowledge, verification
evidence, and command contract that sit underneath it.

The project should grow from specifications first, not from rushed repository
splits. A repository should exist only when it owns a clear contract, release
surface, or trust boundary.

## Strategic Shape

Datapan's long-term job is to make public data feel like a usable local
developer platform:

- discover what exists;
- know which provider owns it;
- know how approval and authentication work;
- know whether an operation is actually callable;
- call it with stable local credentials;
- cache and sync trusted public-data outputs when repeat calls are expensive or
  fragile;
- preserve upstream errors and metadata without hiding them;
- export the result to JSON, CSV, Postman, SDKs, or a future Studio UI;
- expose the same contracts through a future MCP server so AI agents can use
  public data without scraping human-oriented portals;
- keep machine-readable evidence for what is verified, broken, external, or
  unknown.

The project should avoid pretending that every public-data API is easy or
homogeneous. Instead, it should make every gap explicit and progressively turn
those gaps into provider adapters, verification reports, and reusable specs.

Datapan should compete on DX ownership, not raw-data ownership. Developers and
agents should choose Datapan because it is the shortest reliable path from
"there is public data somewhere" to "my code, notebook, service, or agent can
use it with known credentials, known schema, known freshness, and known failure
modes."

## Repository Map

Repository splits should follow contracts, not ambition. The current bias is to
keep implementation and schemas in `datapan-cli` until a split makes a contract
more reliable.

| Repository | Timing | Owns | Must Not Own |
| --- | --- | --- | --- |
| `datapan-cli` | Active now | CLI runtime, command contract, local auth, import/update/diff/audit, verification, first provider adapters | hosted service responsibilities or broad claims about unverified APIs |
| `datapan-data` | Existing research/server-side reference | prior data.go.kr application research, browser/application workflow evidence, server-side experiments worth mining | the canonical user-facing contract or released registry artifacts |
| `datapan-registry` | Active first release | versioned registry snapshots, provider backlog, verification reports, provenance, release notes | CLI behavior or provider call logic |
| `datapan-providers` | Create after multiple external adapters prove the boundary | reusable provider adapters, provider auth/error/approval semantics | catalog release policy or UI state |
| `datapan-spec` | Optional after schemas have consumers outside the CLI | canonical JSON Schemas and codegen inputs | implementation-specific Go internals |
| `datapan-sdk-*` | Deferred until specs and verification are stable | generated or registry-driven clients for application developers | hand-written wrappers for thousands of APIs |
| `datapan-studio` | Future product | DBeaver/Postman-like UI over the same registry, verification, and call engine | a second interpretation of public-data behavior |
| `datapan-mcp` | Future after CLI contracts stabilize | MCP server exposing search, status, schema, verification, and call/export tools to AI agents | direct portal scraping or behavior that disagrees with the CLI |
| `datapan-cloud` | Future business layer | hosted registry, scheduled verification, monitoring, team workflows | required runtime dependency for open-source CLI users |

### datapan-cli

Status: active first repository.

Purpose:

- provide the canonical `datapan` command and optional `dp` alias;
- define the agent-friendly CLI contract;
- import, update, diff, and audit data.go.kr catalog metadata;
- resolve dataset references from IDs, URLs, titles, and queries;
- check local authentication and load local `.env` keys safely;
- help users start or inspect data.go.kr usage applications;
- call approved APIs and export JSON/CSV results;
- expose stable command behavior for future UI, SDK, and automation layers.

This repository should own the first executable implementation of Datapan
Core. Even if other repositories appear later, the CLI should remain the
headless runtime that proves the contracts.

Near-term priorities:

1. Use `catalog providers` to expose external host/provider backlog from the
   imported registry and choose the next adapter targets.
2. Use `catalog verify` to collect bounded, repeatable verification evidence.
3. Harden verification reports into a stable JSON artifact.
4. Improve `get` and `save` around verified operations before broadening API
   claims.
5. Add export surfaces only after registry and verification contracts are
   stable.

Non-goals for this repository:

- hosted registry service;
- large UI;
- provider-specific SDK packages for many languages;
- silent automation of provider security gates;
- claims that unverified APIs work.

### datapan-data

Status: existing reference and research repository.

Purpose:

- preserve earlier data.go.kr exploration and server-side experiments;
- keep evidence about application/login/browser workflows that the CLI can
  turn into explicit user commands;
- provide real-world examples for approval, authentication, and provider quirks;
- help identify what should become schema, adapter behavior, or documentation.

`datapan-data` should be treated as research material, not as the canonical
public contract. When useful behavior is discovered there, the behavior should
move into one of these Datapan-owned layers:

- CLI command contract in `docs/cli-contract.md`;
- registry/provider/verification schema under `schemas/`;
- provider adapter behavior under `internal/provider`;
- release artifact policy in `docs/registry-release.md`.

Non-goals for this repository:

- replacing the CLI as the local user-facing runtime;
- publishing registry release artifacts;
- owning SDK or Studio behavior;
- hiding browser automation behind unclear server behavior.

### datapan-registry

Status: active initial release at
`https://github.com/StatPan/datapan-registry/releases/tag/v2026.06.24`.

Purpose:

- publish normalized public-data catalogs;
- publish dependency classifications;
- publish adapter target work queues;
- publish verification reports;
- publish verification plans;
- publish provider coverage status;
- version catalog changes separately from CLI code;
- make Datapan's evidence usable without requiring every user to re-import the
  full catalog locally.

This repository is not just a large JSON dump. It has schemas, release policy,
provenance, verification reports, readiness gates, and downloadable release
assets. The full `data/data-go-kr.registry.json` is stored with Git LFS, and
the first release also publishes a zip asset for consumers that do not want to
depend on LFS.

Likely artifacts:

- `schemas/datapan.specs.v1.schema.json`;
- `schemas/datapan.provider-index.v1.schema.json`;
- `schemas/datapan.catalog-diff.v1.schema.json`;
- `schemas/datapan.error-catalog.v1.schema.json`;
- `schemas/datapan.catalog-audit.v1.schema.json`;
- `schemas/datapan.dependencies.v1.schema.json`;
- `schemas/datapan.adapter-targets.v1.schema.json`;
- `schemas/datapan.route-disposition.v1.schema.json`;
- `schemas/datapan.providers.v1.schema.json`;
- `schemas/datapan.coverage.v1.schema.json`;
- `schemas/datapan.studio-datasets.v1.schema.json`;
- `schemas/datapan.studio-bundle.v1.schema.json`;
- `schemas/datapan.verification.v1.schema.json`;
- `schemas/datapan.verification-plan.v1.schema.json`;
- `schemas/datapan.verification-summary.v1.schema.json`;
- `schemas/datapan.release-manifest.v1.schema.json`;
- `schemas/datapan.release-verification.v1.schema.json`;
- `schemas/datapan.release-readiness.v1.schema.json`;
- `schemas/datapan.schema-index.v1.schema.json`;
- `schemas/index.json`;
- `data/data-go-kr.registry.json`;
- `data/provider-index.json`;
- `reports/catalog-diff.json`;
- `reports/dependencies.json`;
- `reports/adapter-targets.json`;
- `reports/route-disposition.json`;
- `reports/coverage.json`;
- `reports/verification-plan.json`;
- `reports/latest-verification.json`;
- `reports/latest-verification-summary.json`;
- `reports/latest-release-readiness.json`;
- `reports/error-catalog.json`;
- `reports/catalog-audit.json`;
- `manifest.json`.

Creation trigger:

- create this repository when Datapan has a stable registry schema and at
  least one repeatable verification command that can generate evidence.

Release artifact planning currently lives in `docs/registry-release.md`.
That document defines the draft layout, generation commands, and gates for a
future dedicated registry repository.

### datapan-providers

Status: planned after provider taxonomy becomes concrete.

Purpose:

- hold provider adapters that are too specific or heavy for the CLI core;
- model external-host behavior discovered from data.go.kr;
- keep provider auth, endpoint normalization, approval rules, and response
  classification isolated;
- let the CLI and future Studio share provider implementations.

Adapter status:

- `data-go-kr`: gateway APIs, approval metadata, provider error bodies;
- `q-net`: registered observation-stage adapter for qualification/exam APIs;
- `epost`: registered adapter for postal APIs with conservative REST XML
  verification and call behavior;
- `ekape`: registered observation-stage adapter for livestock quality
  evaluation APIs, including provider key-registration error evidence;
- `forest`: registered adapter for Korea Forest Service culture information
  APIs with verified XML response evidence and external provider call behavior;
- `folk`: registered observation-stage adapter for National Folk Museum
  multimedia APIs with provider-specific JSON result evidence;
- `gblib`: registered adapter for Gangbuk library and sports-center APIs with
  `serviceKey` synthesis, safe search/date defaults, endpoint-not-found
  evidence, and call behavior;
- `airport`: registered observation-stage adapter for Korea Airports
  Corporation APIs with provider key-registration error evidence;
- `andong`: registered adapter for Andong city APIs with synthesized
  `serviceKey`, conservative `numOfRowns` paging, opaque ID skips, and call
  behavior;
- `itfind`: registered adapter for ICT research/publication APIs with
  synthesized `serviceKey`, conservative paging defaults, verified XML list
  evidence, and call behavior;
- `korad`: registered adapter for Korea Radioactive Waste Agency APIs with WADL
  metadata skips, synthesized `serviceKey`, conservative period defaults,
  approval-aware verification, and call behavior for users whose key is
  registered upstream;
- `lh-ebid`: registered adapter for Korea Land and Housing Corporation
  electronic bidding APIs with conservative date/month defaults, opaque
  identifier skips, and provider key-registration error evidence;
- `seoul-bus`: registered adapter for Seoul bus-position APIs with
  `ServiceResult/msgHeader` parsing, route smoke defaults, vehicle-ID skips,
  and provider key-registration evidence;
- `naqs`: registered adapter for National Agricultural Products Quality
  Management Service APIs with no-auth XML lookup verification and explicit
  skips for mutation-like `pubc` integration endpoints;
- `oneclick-law`: registered adapter for Ministry of Government Legislation
  Easy Law SOAP APIs with SOAP envelope generation, approval-aware skips, and
  transport-state evidence for currently refused upstream endpoints;
- `kpx`: registered adapter for Korea Power Exchange electricity APIs with
  scheme normalization, paging defaults, `resultCode/resultMsg` evidence, and
  call behavior;
- `myhome`: registered adapter for LH MyHome rental housing APIs with
  uppercase `ServiceKey` handling, JSON status parsing despite incorrect
  content type, provider key-registration evidence, and call behavior;
- `emuseum`: registered adapter for National Museum of Korea relic APIs with
  named User-Agent handling, empty-filter omission, XML provider-status
  evidence, and call behavior;
- `jeju`: registered adapter for Jeju province APIs with official action URL
  rewriting for night pharmacy data, stale-endpoint evidence, and call behavior;
- `pqis`: registered adapter for Animal and Plant Quarantine Agency plant
  quarantine statistics APIs with WADL endpoint normalization, conservative
  code/date defaults, provider key-registration evidence, and call behavior;
- `jeonju`: registered adapter for Jeonju city APIs with exact `ServiceKey` /
  `authApiKey` handling and upstream `HTTP 405` evidence;
- `geoje`: registered adapter for Geoje city APIs with `serviceKey` handling,
  verified XML list evidence, ID-only detail skips, and call behavior;
- `humetro`: registered adapter for Busan Transportation Corporation APIs with
  synthesized `ServiceKey`, conservative station/date defaults, and provider
  access-denied evidence;
- `sisul`: registered adapter for Seoul Facilities Corporation OpenDB APIs with
  `_wadl` metadata skips, synthesized `serviceKey` handling, and call behavior;
- `uiryeong`: registered adapter for Uiryeong county APIs with `ServiceKey`
  handling, list-filter/default separation, provider key-registration evidence,
  and call behavior;
- `ulsan`: registered adapter for Ulsan traffic APIs with synthesized
  `serviceKey` handling, route/date identifier skips, and call behavior;
- `tour`: registered adapter for Korea Culture & Tourism Institute tourism
  statistics APIs with source `operation_url` routing, service-root skips, and
  call behavior;
- `open-assembly`: National Assembly APIs and external dependency behavior;
- `mfds`: food and drug data APIs;
- `visitkorea`: tourism APIs;
- local government open APIs with repeated host patterns.

Creation trigger:

- create this repository when at least two non-data.go.kr provider adapters are
  needed, at least two adapters have call behavior, and the `Provider`
  interface has stabilized inside `datapan-cli`.

Adapter planning currently lives in `docs/provider-adapters.md`. The first
code boundary lives in `internal/provider`; andong, emuseum, epost, forest,
geoje, gblib, humetro, itfind, jeju, korad, kpx, lh-ebid, myhome, naqs,
oneclick-law, pqis, seoul-bus, sisul, tour, uiryeong, and ulsan now prove
multiple external call paths,
while jeonju expands high-impact external host ownership. The split should wait
for sustained maintenance pressure.

### datapan-spec

Status: optional, planned only if schemas become independently useful.

Purpose:

- define Datapan's registry, operation, parameter, provider, verification, and
  error schemas;
- support code generation and validation across Go, Node, Python, and future
  tools;
- make third-party contributions possible without depending on CLI internals.

This repository should exist only if schema consumers appear outside the CLI.
Until then, schemas can live in `datapan-cli` or `datapan-registry`.
Schema versioning and compatibility rules currently live in
`docs/spec-governance.md`.

### datapan-sdk

Status: deferred.

Purpose:

- expose generated or registry-driven clients for common languages;
- start with thin clients that call Datapan specs rather than hand-written
  per-API wrappers;
- support agent and application use without requiring users to memorize
  provider parameter names.

Language order should follow demand:

1. Go, because the CLI and provider adapters are Go-first.
2. Node.js, because frontend and agent tooling often starts there.
3. Python, because analysts and data workflows expect it.

Creation trigger:

- create SDK repositories only after the registry schema, provider status
  model, and verification evidence format are stable enough to generate from.

### datapan-studio

Status: future product, not MVP.

Purpose:

- provide a DBeaver/Postman-like UI for public data;
- search public-data catalogs;
- inspect provider metadata, approval requirements, and verification status;
- fill request parameters with forms generated from specs;
- call APIs through the same Datapan Core contracts;
- preview results as table, JSON, and CSV;
- save, transform, filter, join, and export datasets;
- export calls to CLI commands, curl, Postman collections, OpenAPI documents,
  or SDK snippets.

Datapan Studio should not duplicate the CLI's logic. It should sit on top of
the same registry, provider adapters, verification reports, and call engine.

Creation trigger:

- start Studio when CLI verification and provider routing are reliable enough
  that the UI can show trust states instead of hiding uncertainty.

### datapan-cloud

Status: business layer, future.

Purpose:

- host verified registry releases;
- run scheduled verification;
- monitor public-data API reliability;
- provide team sharing and audit logs;
- expose managed API routing or cache layers for organizations;
- support business features without making the open-source CLI dependent on a
  hosted Datapan server.

Creation trigger:

- start after the open registry and CLI have enough usage to justify hosted
  verification, team workflows, or reliability monitoring.

### datapan-mcp

Status: future agent interface.

Purpose:

- expose Datapan search, status, schema, access, verification, call, export, and
  starter-kit workflows to AI agents;
- reuse the same registry, provider, verification, and command contracts as the
  CLI;
- let agents reason about public-data trust states without scraping data.go.kr
  pages or guessing provider behavior;
- provide safe, explicit tools for credential checks, dry-runs, bounded calls,
  cached reads, schema inspection, and generated client artifacts.

Creation trigger:

- start after the CLI has stable JSON contracts for init, status, search, try,
  use/kit, coverage, verification, and provider gaps. The MCP server should be
  a thin agent-facing layer over those contracts, not a second implementation.

## Core Contracts

Every repository should reinforce these contracts instead of inventing a new
source of truth.

### Spec-First Ownership Ladder

Datapan should earn control in layers. Each layer should become a published
contract before the project claims the next one.

1. **Raw catalog preservation**: import data.go.kr metadata without destroying
   upstream names, IDs, parameters, categories, organizations, or raw fields.
2. **Normalized registry**: express public-data datasets and operations in
   Datapan's stable `Spec` shape while keeping raw upstream fields available.
3. **Dependency classification**: classify gateway APIs, external hosts, SOAP,
   WMS, missing endpoints, malformed endpoints, approval requirements, and
   provider families.
4. **Verification evidence**: prove what was attempted, what worked, what
   failed, and what was skipped with stable reasons and redacted request
   metadata.
5. **Provider adapters**: turn repeated external-host behavior into reusable
   adapters instead of one-off command fixes.
6. **Call and export contract**: make discovered and verified operations usable
   through JSON/CSV, curl/Postman exports, and later SDK generation.
7. **Studio and cloud surfaces**: build UI and hosted reliability features only
   on top of the same registry, provider, verification, and call contracts.

The rule of thumb: if a fact can be preserved from upstream, preserve it; if a
fact is Datapan's interpretation, mark it as Datapan-owned evidence; if a fact
is not verified, make the uncertainty visible.

### Registry Contract

The registry describes what exists. It should preserve upstream values as much
as possible:

- upstream ID;
- upstream title;
- provider platform;
- providing organization;
- source category;
- source keywords;
- guide URL;
- endpoint URL;
- operations;
- request parameters;
- response parameters;
- raw upstream fields.

Datapan-created fields should be separate from upstream metadata. Search
helpers, normalized dependency classes, and verification evidence are useful,
but they should not overwrite source facts.

### Provider Contract

Providers describe how an API ecosystem behaves:

- how credentials are supplied;
- how usage approval works;
- how endpoints are formed;
- how errors are represented;
- whether an API is gateway-hosted or externally hosted;
- whether SOAP, WMS, file download, or browser-gated behavior is involved;
- how a safe verification request can be made.

Initial Go shape:

```go
type Adapter interface {
    Name() string
    Hosts() []string
    MatchHost(host string) bool
    DependencyClass(spec datago.Spec, op datago.Operation) string
    Verify(ctx context.Context, req provider.VerificationRequest) datago.VerificationResult
    Call(ctx context.Context, req provider.CallRequest) (datago.ResponseEnvelope, error)
}
```

This interface should start inside `datapan-cli`. Move it only after multiple
providers prove the boundary.

### Verification Contract

Verification proves what works now. It should be evidence, not marketing.

A verification result should include:

- dataset ID;
- operation ID or name;
- provider;
- endpoint host;
- dependency class;
- verification status;
- skip or failure reason;
- timestamp;
- HTTP status when available;
- provider status fields when available;
- sample row or body shape information;
- redacted request metadata;
- Datapan version.

Verification statuses should stay conservative:

- `verified`: a bounded call succeeded and returned expected provider success;
- `failed`: a bounded call was attempted and failed;
- `skipped`: Datapan chose not to call because it needed approval, parameters,
  unsupported protocol, or a provider adapter;
- `unknown`: no evidence exists yet.

### CLI Contract

The CLI is the first stable machine interface:

- JSON output is one object on stdout;
- errors use stable exit codes;
- credentials are local and never printed;
- provider error fields are preserved;
- dry-run is the default for risky actions;
- broad operations are bounded by limits, samples, and explicit flags.

### Studio Contract

Studio should be a UI over Datapan Core, not a separate interpretation layer.
Every Studio action should be expressible as a CLI command, registry query, or
provider call. This keeps human UI, agents, scripts, and SDKs aligned.

## Phased Plan

### Phase 0: CLI Kernel

Goal: prove the local command contract.

Done or in progress:

- command structure;
- `.env` loading;
- data.go.kr import/update/diff/audit;
- provider backlog classification with `catalog providers`;
- operation-level dependency inventory with `catalog dependencies`;
- adapter target prioritization with `catalog adapter-targets`;
- route disposition classification with `catalog route-disposition`;
- bounded runtime evidence collection with `catalog verify`;
- schema drafts for registry, provider indexes, catalog audits, provider
  backlog, dependency inventories, adapter targets, verification reports,
  verification summaries, release manifests, release verification, release
  readiness, and schema indexes;
- provider error preservation;
- access helper;
- get/save/export basics.

Completion bar:

- all CLI contracts documented;
- local tests cover command outputs, exit codes, registry operations, and
  provider error classification;
- README shows honest MVP scope and non-goals.

### Phase 1: Catalog Control

Goal: own the catalog layer without pretending it is fully callable.

Required:

- `catalog providers`;
- `catalog dependencies`;
- `catalog adapter-targets`;
- `catalog route-disposition`;
- dependency backlog by host, protocol, and approval state;
- stable audit JSON;
- stable diff report schema;
- stable error catalog schema;
- stable audit report schema;
- stable dependency inventory schema;
- stable adapter target schema;
- stable route disposition schema;
- stable registry schema draft;
- update workflow that can be repeated locally and in CI.

Completion bar:

- Datapan can explain how many APIs are gateway, external, SOAP, WMS,
  approval-required, malformed, or unknown.

### Phase 2: Verification Evidence

Goal: prove which operations actually work.

Required:

- `catalog verify`;
- verification report schema;
- verification summary schema and grouped reason/provider/host rollups;
- conservative eligible set for safe data.go.kr gateway calls;
- credential-free `--probe-unadapted` transport evidence for missing external
  hosts, so dead routes are separated from real adapter candidates;
- explicit skipped reasons for APIs that need adapters, approval, required
  parameters, or unsupported protocols;
- redacted request evidence.

Completion bar:

- Datapan can distinguish `verified`, `failed`, `skipped`, and `unknown` with
  machine-readable evidence.

### Phase 3: Provider Adapters

Goal: turn external-host gaps into supported provider families.

Required:

- provider interface stabilized through real adapters;
- provider index artifact for registered adapter ownership;
- at least two external provider adapters;
- adapter-specific verification;
- provider-specific auth and error preservation.

Completion bar:

- external host coverage grows by adapter, not by brittle one-off URL fixes.

### Phase 4: Registry Releases

Goal: make evidence portable.

Required:

- create `datapan-registry`;
- publish schemas and registry snapshots;
- publish provider indexes;
- publish dependency inventories;
- publish adapter target reports;
- publish route disposition reports;
- publish `schemas/index.json` so consumers can discover release schema
  contracts without hard-coded filenames;
- publish catalog diff reports when a previous registry exists;
- publish catalog audit reports;
- publish dependency inventory reports;
- publish adapter target reports;
- publish route disposition reports;
- publish coverage reports;
- publish error catalog reports;
- publish verification reports;
- publish verification summaries;
- publish verification plans;
- publish release manifests with artifact checksums;
- publish release verification reports;
- publish release readiness reports;
- document provenance and update cadence.

Completion bar:

- users can consume a released registry without importing from upstream every
  time.

The release should follow `docs/registry-release.md` rather than inventing a
new artifact layout during repository creation.

### Phase 5: Developer Exports And SDKs

Goal: let Datapan generate useful integration surfaces.

Required:

- Postman collection export;
- OpenAPI export as the first SDK/codegen bridge;
- Datapan schema export;
- Go client prototype through `datapan codegen go`;
- Node/Python clients only after schema stability.

Completion bar:

- an operation can be discovered, verified, called, and exported into another
  developer workflow without losing provider metadata.

### Phase 6: Studio

Goal: make public data explorable and usable by humans.

Required:

- search UI over registry;
- dataset detail view;
- approval and verification state;
- generated parameter forms;
- result preview as table/JSON/CSV;
- export to CLI/curl/Postman/SDK snippets.

Completion bar:

- Studio can be used as a DBeaver/Postman-like public-data workbench while the
  CLI remains the underlying runtime.

## Open-Source Posture

Default posture:

- keep `datapan-cli` open source;
- keep source schemas and contracts open;
- avoid hosted-server dependency for core behavior;
- keep user credentials local by default.

Good commercial boundaries:

- hosted verified registry;
- scheduled verification;
- team sharing;
- reliability monitoring;
- managed public-data gateway;
- enterprise adapters;
- private provider catalogs.

This lets the open-source layer build trust while leaving room for a business
that sells reliability, collaboration, and operational depth.

## Organization Timing

Do not split repositories just to look larger. Start under the existing owner
until boundaries are proven.

Consider creating or moving to a Datapan organization when at least two of
these are true:

- `datapan-registry` needs independent releases;
- provider adapters are growing beyond the CLI core;
- Studio has begun;
- external contributors need clearer ownership;
- package names, docs, and release automation benefit from organization-level
  identity.

Until then, `StatPan/datapan-cli` is a fine place to move fast while keeping the
architecture honest.
