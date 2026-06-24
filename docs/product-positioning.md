# Product Positioning

Datapan is a public-data access layer for Korean data. The CLI is the first
stable surface: it lets developers and coding agents discover datasets, inspect
normalized metadata, call approved APIs from the user's machine, and export
machine-readable artifacts without requiring a hosted Datapan server.

The long-term product shape is broader than a command-line tool:

- **CLI** for developers, scripts, and coding agents.
- **Registry releases** for normalized, versioned Korean public-data metadata.
- **Provider adapters** for upstream APIs that do not fit the data.go.kr gateway
  shape.
- **SDK and export surfaces** for tools such as OpenAPI, Postman, generated
  clients, and agent workflows.
- **Studio** for non-developers who need search, preview, filtering, joining,
  export, and AI-assisted use without writing shell commands.
- **International access layer** for teams outside Korea that need Korean public
  data with English documentation, stable schemas, licensing notes, and
  machine-readable delivery formats.

## Product Thesis

Korean public data is valuable, but the practical access cost is high. Data is
spread across portals, agencies, formats, approval flows, endpoint shapes,
Korean-only labels, and inconsistent metadata. Datapan should reduce that cost
by turning fragmented upstream datasets into a predictable, agent-friendly data
package and access layer.

The core value is not resale of raw public data. The core value is the
interface around it:

- dataset discovery;
- normalized schema and metadata;
- stable dataset references;
- provider-specific access handling;
- conservative verification evidence;
- exportable API/client artifacts;
- documented update and release history;
- Korean-to-English explanation where useful;
- AI-ready metadata for agents, RAG, and workflow automation.

A useful one-line positioning statement:

> Datapan is an open data package manager for Korean public data, built to make
> datasets searchable, verifiable, exportable, and usable by humans, developers,
> and AI agents.

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
5. Let Studio consume the same registry, dependency, verification, and export
   artifacts instead of inventing a separate product model.

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
it is a stable command surface with machine-readable output. For Studio, it is
the backend product contract.

## Studio Direction

Studio should not start as a full BI product. It should be a visual layer over
Datapan's existing artifacts:

1. Search Korean public datasets.
2. Preview normalized metadata, parameters, response fields, provider status,
   and access requirements.
3. Explain which datasets are callable, approval-gated, missing adapters, or
   unsupported.
4. Let users export OpenAPI, Postman, CSV, parquet-like data products, or SDK
   starter code.
5. Let users ask AI-assisted questions against metadata and sampled outputs.

The key constraint: Studio should reuse CLI/registry semantics. If the CLI says
a dataset is ambiguous, approval-required, missing an adapter, or verified,
Studio should show the same status rather than hiding the complexity.

## International Service Direction

Selling raw public data is weak because the source is public and licensing can
vary by dataset. A stronger international product is a paid access and
interpretation layer for Korean data:

- English dataset descriptions and field explanations.
- Stable API and file delivery formats.
- Update monitoring and changelogs.
- Licensing and attribution notes.
- Administrative-region normalization.
- Geospatial joins and standard codes.
- Curated bundles for market entry, real estate, mobility, weather, population,
  business districts, and public facilities.
- Support for AI agents that need Korean data but cannot navigate Korean portals
  reliably.

In this framing, Datapan sells reliability, normalization, documentation,
translation, monitoring, and support rather than claiming ownership over public
raw data.

## Near-Term Priorities

1. Keep the CLI command contract boring and stable.
2. Make registry releases easy to install and inspect.
3. Track data quality, provider errors, access gates, missing adapters, and
   verification evidence as first-class artifacts.
4. Pick a small number of high-value Korean domains and make them feel excellent:
   real estate, weather, population, business districts, transport, or public
   facilities.
5. Add Studio only after the artifact model is stable enough that the UI can be
   mostly a visual consumer of the same contracts.

## Non-Goals

- Do not build a hosted dependency into the CLI path.
- Do not bypass provider security or approval flows.
- Do not claim every imported operation is callable.
- Do not sell public raw data as if it were proprietary.
- Do not make Studio a separate semantic model from the CLI and registry.
