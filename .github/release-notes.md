## Health probe receipt contract

Datapan CLI binary archives and checksums are attached to this release. Install
the archive for your platform, then put `datapan` (or the optional `dp` alias)
on `PATH`.

This release includes the one-operation `datapan.health-probe.v1` receipt:

```text
datapan verify --ref REF --operation NAME --health --output receipt.json --json
```

The receipt binds a redacted operation observation to the installed immutable
Registry revision and digests. It never contains credentials, full query URLs,
response bodies, or response rows.

An operation observation is not a provider SLA, a guarantee that every API at
the provider is available, or a data-freshness assertion. Empty data remains an
observation; schema and freshness stay `not_observed` unless a Registry policy
explicitly supports them.

Healthcheck may invoke this local CLI contract with its own operational
credential boundary. Scheduling, credential storage, retries, history, alerts,
and status UI are not part of Datapan CLI.
