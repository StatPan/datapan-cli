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
- preserve upstream errors and metadata without hiding them;
- export the result to JSON, CSV, Postman, SDKs, or a future Studio UI;
- keep machine-readable evidence for what is verified, broken, external, or
  unknown.

The project should avoid pretending that every public-data API is easy or
homogeneous. Instead, it should make every gap explicit and progressively turn
those gaps into provider adapters, verification reports, and reusable specs.

## Repository Map

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

### datapan-registry

Status: planned when registry data becomes a released artifact.

Purpose:

- publish normalized public-data catalogs;
- publish dependency classifications;
- publish verification reports;
- publish provider coverage status;
- version catalog changes separately from CLI code;
- make Datapan's evidence usable without requiring every user to re-import the
  full catalog locally.

This repository should not be just a large JSON dump. It should have a schema,
release policy, provenance, verification timestamp, and change log.

Likely artifacts:

- `schemas/datapan.specs.v1.schema.json`;
- `schemas/datapan.providers.v1.schema.json`;
- `schemas/datapan.verification.v1.schema.json`;
- `data/data-go-kr.registry.json`;
- `data/provider-index.json`;
- `reports/latest-verification.json`;
- `reports/catalog-audit.json`.

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

Candidate adapters:

- `data-go-kr`: gateway APIs, approval metadata, provider error bodies;
- `open-assembly`: National Assembly APIs and external dependency behavior;
- `q-net`: qualification/exam APIs;
- `epost`: postal APIs;
- `mfds`: food and drug data APIs;
- `visitkorea`: tourism APIs;
- local government open APIs with repeated host patterns.

Creation trigger:

- create this repository when at least two non-data.go.kr provider adapters are
  needed and the `Provider` interface has stabilized inside `datapan-cli`.

Adapter planning currently lives in `docs/provider-adapters.md`. The first
code boundary lives in `internal/provider`; keep it there until multiple real
adapters prove the split.

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
- export calls to CLI commands, curl, Postman collections, or SDK snippets.

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

## Core Contracts

Every repository should reinforce these contracts instead of inventing a new
source of truth.

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
- bounded runtime evidence collection with `catalog verify`;
- schema drafts for registry, provider backlog, and verification reports;
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
- dependency backlog by host, protocol, and approval state;
- stable audit JSON;
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
- conservative eligible set for safe data.go.kr gateway calls;
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
- publish verification reports;
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
- OpenAPI or Datapan schema export;
- Go client prototype;
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
