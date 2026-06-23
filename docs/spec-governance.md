# Datapan Spec Governance

Datapan should treat schemas as public contracts. CLI commands, registry
releases, provider adapters, SDK generators, and future Studio views should all
share these contracts instead of inventing parallel shapes.

## Scope

This document governs JSON Schemas under `schemas/` and the command outputs
that claim those schemas:

- registry specs;
- provider index reports;
- catalog diff reports;
- error catalog reports;
- catalog audit reports;
- provider backlog reports;
- verification reports and summaries;
- release manifests;
- release verification reports;
- schema indexes.

Until `datapan-spec` exists, `datapan-cli` owns the source schemas and their
tests. When `datapan-registry` begins publishing releases, it should copy schema
files from this repository, generate `schemas/index.json`, and record both the
schemas and the index in `manifest.json`.

## Naming

Schema files must use this filename shape:

```text
datapan.<contract>.vN.schema.json
```

The `$id` must match the filename:

```text
https://schemas.datapan.dev/datapan.<contract>.vN.schema.json
```

Runtime artifacts that carry a `schema_version` field must use:

```text
datapan.<contract>.vN
```

Examples:

- `datapan.release-manifest.v1.schema.json`;
- `$id`: `https://schemas.datapan.dev/datapan.release-manifest.v1.schema.json`;
- `schema_version`: `datapan.release-manifest.v1`.

## Compatibility

Within a major schema version, changes should be additive and conservative:

- adding optional fields is allowed;
- adding enum values is allowed only when consumers can safely treat unknown
  values as not-yet-supported;
- tightening required fields, changing field meaning, renaming fields, removing
  fields, or changing scalar types requires a new major schema version;
- changing a command's exit-code meaning requires a CLI contract update and
  release note.

Datapan-owned interpretation must stay separate from upstream facts. For
example, upstream data.go.kr fields belong in source metadata; Datapan
dependency classes, verification statuses, and provider reason codes are
Datapan-owned evidence.

## Lifecycle

1. Draft a schema in `schemas/` with a `v1` suffix.
2. Wire command output or release artifacts to emit that schema.
3. Add tests that require the schema file and documentation references.
4. Document the command contract in `docs/cli-contract.md` or
   `docs/registry-release.md`.
5. Publish the schema in release drafts via `catalog release draft`.
6. Treat breaking changes as `v2`, leaving `v1` readable until consumers can
   migrate.

Schemas should not move to `datapan-spec` until at least one consumer outside
`datapan-cli` or `datapan-registry` needs them.

## Release Gate

Before a registry snapshot is published:

- schema files must be copied from the current source repository;
- `schemas/index.json` must be generated from the copied schema files;
- `manifest.json` must list every release artifact except itself;
- `catalog release verify --manifest manifest.json --output reports/latest-release-verification.json`
  must pass checksum and schema-bound artifact validation;
- release verification output must be a `datapan.release-verification.v1`
  report;
- credentials and API keys must never appear in schema-bound artifacts.
