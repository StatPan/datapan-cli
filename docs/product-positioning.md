# Product Positioning

Datapan is a public-data access layer for Korean data. The CLI is the first
stable surface: it lets developers and coding agents discover datasets, inspect
normalized metadata, call approved APIs from the user's machine, and export
machine-readable artifacts without requiring a hosted Datapan server.

## CLI Goal

Datapan CLI is the local-first execution environment that turns a verified
`datapan-registry` release into usable public-data results. A user should be
able to install a registry, find and understand an operation, determine whether
it is ready to call, execute it with local credentials, and save or export the
result. When that path is blocked, the CLI must preserve the registry's risk
state and explain the next action instead of presenting uncertainty as support.

The repository boundary is deliberate:

- `datapan-registry` owns canonical metadata, source contracts, coverage
  denominators, evidence freshness, release readiness, and consumer
  compatibility;
- `datapan-cli` owns registry installation and compatibility checks, local
  credentials, discovery, request planning and execution, response handling,
  caching, and developer exports;
- registry-production commands under `datapan catalog` remain operator tooling,
  not the center of the end-user product.

The first completion path is:

```text
init -> search -> try -> get -> sync/export
```

That path must remain machine-readable, credential-safe, and honest about
approval requirements, missing parameters, adapter support, stale evidence,
manual-review boundaries, and upstream failures.

The long-term product shape is broader than a command-line tool:

- **CLI** for developers, scripts, and coding agents.
- **Registry releases** for normalized, versioned Korean public-data metadata.
- **Provider adapters** for upstream APIs that do not fit the data.go.kr gateway
  shape.
- **SDK and export surfaces** for tools such as OpenAPI, Postman, generated
  clients, and agent workflows.
- **Future visual and agent surfaces** that consume the same registry,
  verification, and export contracts.

## Product Thesis

Korean public data is valuable, but the practical access cost is high. Data is
spread across portals, agencies, formats, approval flows, endpoint shapes,
Korean-only labels, and inconsistent metadata. Datapan should reduce that cost
by turning fragmented upstream datasets into a predictable, agent-friendly data
package and access layer.

The core value is not resale of raw public data. The core value is the
interface around it:

- dataset discovery;
- usage application and credential-management guidance;
- normalized schema and metadata;
- stable dataset references;
- provider-specific access handling;
- conservative verification evidence;
- response-shape normalization across XML, JSON, CSV, files, and provider quirks;
- exportable API/client artifacts;
- documented update and release history;
- schema and catalog change detection;
- local cache and sync behavior;
- Korean-to-English explanation where useful;
- AI-ready metadata for agents, RAG, MCP servers, and workflow automation.

A useful one-line positioning statement:

> Datapan is an open data package manager for Korean public data, built to make
> datasets searchable, verifiable, exportable, and usable by humans, developers,
> and AI agents.

## Developer Experience Thesis

Datapan should aim to own the developer experience around public data rather
than the public data itself. Most upstream data is free or inexpensive, but the
developer cost is high: application flows are inconsistent, API keys are easy
to leak or misplace, response formats vary, documentation quality is uneven,
change history is unclear, data quality is hard to judge, and combining
multiple agencies requires repeated one-off glue work.

The product moat is the workflow developers and agents choose first:

```bash
datapan init --json
datapan search "부동산 실거래" --json
datapan try "단기예보" base_date=20260622 --org 기상청 --json
datapan sync
datapan mcp serve
```

Some of these commands are future-facing, but the direction is deliberate:
Datapan should feel like npm, Homebrew, Postman, and a local data workbench for
public APIs. The open-source CLI should become the trusted local interface;
registry releases should become the package index; provider adapters should
become the compatibility layer; SDK/codegen, Postman/curl exports, and MCP
should consume the same contracts.

## Why the Current CLI Shape Fits

The current CLI direction is compatible with this product thesis because it
already emphasizes stable command contracts, `--json` output, local API keys,
registry installation, upstream catalog import, audit reports, provider backlog
artifacts, runtime verification, and export/code-generation surfaces.

That means Datapan can grow without changing its center of gravity:

1. Start with the CLI contract and local-first execution.
2. Publish normalized registry artifacts.
3. Add provider adapters for high-value external endpoints.
4. Generate exports and SDKs from the same normalized specs.
5. Keep future surfaces aligned with the same registry, dependency,
   verification, and export artifacts.

## Wedge

The first strong wedge should be:

> Korean public data, installable and usable like a data package.

Example user journeys:

```bash
datapan catalog install datapan-registry --json
datapan search "아파트 실거래가" --org 국토교통부 --json
datapan show 15084084 --json
datapan export --format openapi 15084084 --output apartment.openapi.json
datapan codegen go 15084084 --output apartment_client.go
```

For developers, this feels like a package manager and API planner. For agents,
it is a stable command surface with machine-readable output.

## Coverage And Milestones

The public roadmap should be judged by measurable coverage and contract
maturity rather than broad product claims:

1. Registry install works from a release artifact.
2. Search and show results expose stable source metadata.
3. Callable and call-ready coverage are reported conservatively.
4. Provider adapters increase verified operation coverage without hiding
   unsupported or approval-gated routes.
5. Verification reports distinguish `verified`, `failed`, `skipped`, and
   `unknown`.
6. Export and codegen surfaces reuse the same registry and verification
   contracts.
7. Future visual or agent-facing surfaces reuse the CLI/registry semantics
   instead of inventing a second interpretation.

## Near-Term Priorities

1. Keep the CLI command contract boring and stable.
2. Make registry releases easy to install and inspect.
3. Track data quality, provider errors, access gates, missing adapters, and
   verification evidence as first-class artifacts.
4. Reduce the first-use workflow to a few memorable commands: init, search,
   try, sync/cache, export/codegen, and status.
5. Add MCP only on top of the stable CLI/registry contracts so agents use the
   same semantics as humans.
6. Pick a small number of high-value Korean domains and make them feel excellent:
   real estate, weather, population, business districts, transport, or public
   facilities.
7. Keep future surfaces milestone-gated by registry, verification, and export
   contract stability.

## Non-Goals

- Do not build a hosted dependency into the CLI path.
- Do not bypass provider security or approval flows.
- Do not claim every imported operation is callable.
- Do not sell public raw data as if it were proprietary.
- Do not let future surfaces diverge from the CLI and registry semantics.
