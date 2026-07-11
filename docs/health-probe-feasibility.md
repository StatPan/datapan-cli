# Health probe feasibility and storage contract

Status: spike baseline for issues #135 and #134.

## Decision

Datapan can provide useful public-data health observations, but the current
runtime verification status is not a health verdict. The health surface must
reuse the existing verification executor and preserve separate confidence
levels for reachability, parsing, provider success, structure, data presence,
and freshness.

The canonical interchange is a versioned, redacted JSON receipt. A hosted
service may normalize receipts into relational tables, but the CLI does not
depend on a database or hosted service.

## Measured public evidence

The analysis used `reports/latest-verification.json` installed from public
Hugging Face Dataset revision
`a804917eba528c62a9f12cefca1e41ccf0840190`. The Registry SHA-256 recorded by
the install provenance is
`eeda72ee8590f458de8d75703662578e80edf3e61282f0e5e67547c4f6e5f644`.

| Observation | Count | Share of 4,774 |
| --- | ---: | ---: |
| verified | 2,841 | 59.5% |
| failed | 389 | 8.1% |
| skipped | 1,544 | 32.3% |
| HTTP status observed | 3,109 | 65.1% |
| structured JSON/XML body shape | 245 | 5.1% |
| structured provider status | 168 | 3.5% |
| provider status explicitly OK | 53 | 1.1% |
| HTML body shape | 2,856 | 59.8% |
| empty body observed | 4 | 0.1% |
| missing authentication | 583 | 12.2% |
| missing required parameters | 679 | 14.2% |
| approval required | 128 | 2.7% |

The evidence contains 4,050 external endpoints, 705 data.go.kr gateway
operations, and 19 service roots. These categories overlap with different
verification semantics; they must not be compared as one availability SLI.

The dominant HTML count demonstrates the principal gap: a reachable portal or
HTML response can be valuable transport evidence, but it is not proof that a
documented data operation returned healthy data.

## Bounded live check

On 2026-07-11, the spike selected Registry operation `3044607`, forest
operation `숲에서 만날 수 있는 식물 이야기 목록 정보 검색`, with safe parameters
`numOfRows=1`, `pageNo=1`, and `searchWrd=소나무`. The existing verification
engine stopped before HTTP with `missing_auth` because no supported data.go.kr
credential environment variable was configured. No credential value was read
or printed.

This confirms that candidate resolution and conservative parameter planning
work for a real published operation. A credentialed Provider success remains a
required manual canary before #134 can be called production-ready.

## Confidence levels

Health evidence is monotonic within one receipt: a higher level implies that
the lower-level observation was made, but does not imply data correctness.

| Level | Meaning | Current evidence source |
| --- | --- | --- |
| L0 | Registry contract resolved and trusted | Registry trust context |
| L1 | DNS/TLS/transport completed | executor or adapter result |
| L2 | HTTP response obtained | `http_status` |
| L3 | response parsed as an expected wire format | `body_shape` plus parser result |
| L4 | Provider-specific success confirmed | structured `provider_status.ok` |
| L5 | expected structural shape confirmed | future versioned probe policy |
| L6 | data presence observed | observation only; empty is not failure |
| L7 | freshness assertion evaluated | explicit Registry policy required |

The current report can measure L0-L4 partially. It cannot defensibly infer L5
through L7 for the catalog as a whole. In particular, `verified` must not be
mapped directly to L4 or to `healthy`.

Using only fields preserved in the published report gives this conservative
funnel for its 4,774-result cohort:

| Maximum defensible level | Count | Interpretation |
| --- | ---: | --- |
| L0 | 4,774 | operation represented in runtime evidence |
| L1 | 3,109 | transport completion proven by an HTTP result |
| L2 | 3,109 | HTTP status preserved |
| L3 | 245 | structured JSON/XML body shape preserved |
| L4 | 53 | structured provider status explicitly reports success |
| L5 | 0 | no versioned structural assertion policy applied |
| L6 | 0 | presence is not yet a first-class report observation |
| L7 | 0 | no explicit freshness assertion policy applied |

These are lower bounds, not catalog coverage claims: the legacy report did not
preserve every observation needed to reconstruct a higher confidence level.

## Objective observations and policy decisions

The receipt stores observations independently of their interpretation.

Objective observations include elapsed time, transport outcome, HTTP status,
parse result, provider code, body shape, and whether a record was observed.
Datapan policy supplies category, retryability, expected shape, and freshness
interpretation. A policy change creates a new policy version or derived view;
it never rewrites historical observations.

An empty successful result is `data_presence=empty`, not automatically
unhealthy. A missing timestamp is `freshness_status=not_observed`, not stale.

## Stable operation identity

For the first schema, `operation_key` is the SHA-256 of the length-delimited
tuple:

```text
provider, dataset_id, operation_name, dependency_class, normalized endpoint host and path
```

Length delimiting avoids concatenation collisions. Credentials, query values,
scheme, and mutable Registry revision are excluded. The human-readable tuple is
stored alongside the hash.

An operation rename or endpoint-path replacement intentionally creates a new
operation key. Continuity across such changes requires an explicit Registry
alias or replacement relation; the health database must not guess.

## Receipt and relational storage

`schemas/datapan.health-probe.v1.schema.json` is the interchange proposal.
`docs/health-probe-storage.sql` is the PostgreSQL reference mapping.

The relational design separates:

- immutable Registry revisions;
- operation versions observed in each revision;
- versioned input policies;
- bounded execution runs;
- append-only operation results.

Every normalized result retains the complete redacted receipt and its SHA-256.
This permits schema evolution and later reclassification without losing the
original evidence.

## Privacy and retention

Receipts and database rows must never contain credentials, authorization
headers, full query URLs, raw response bodies, or returned data rows. Endpoint
host, normalized path, safe parameter names, and policy-approved public sample
values are sufficient for reproducibility.

Detailed receipts may use a shorter retention period than daily aggregates.
Retention is a service policy and is deliberately not fixed by the CLI schema.
Results are append-only. UTC timestamps and integer milliseconds are required.

## Go/no-go

Go for #134 as a receipt and classification feature after these constraints:

1. do not rename current `verified` to `healthy`;
2. implement L0-L4 first and mark L5-L7 not observed without explicit policy;
3. reuse the existing executor and issue #12 execution budgets;
4. complete one credentialed data.go.kr canary and one registered external
   adapter canary with redacted receipts;
5. keep scheduling, database hosting, alerts, and status pages outside the CLI.

The remaining catalog-wide work is to measure maximum confidence level per
operation and add Registry-owned policies only where semantic or freshness
claims are supportable.
