# Diagnostic consumer handoff

Datapan CLI exposes an additive `diagnostic.consumer_handoff` on bounded local
call results. It lets Datapan Web reuse the reviewed Registry vocabulary without
treating Registry draft fixtures as runtime facts.

## Contract identity

- schema SHA-256: `da254b40947462347fcda90fdd7686b6632c76943b438f2046a28f079f33e403`
- mapping SHA-256: `da55d52d2ee1f197969ac63a1d5ab5b98e3b88fd65f90d6a48800d2e3c522d33`
- status: `reviewed_draft_dependency_gated`
- `runtime_authority`: always `false`

The embedded test fixtures are byte-exact compatibility inputs only. CLI
runtime results come from current CLI request validation, provider response,
validation, or data-quality evidence.

## Consumer rules

Use `subject` as the join key. The CLI emits it only for an exact eight-digit
data.go.kr Registry dataset and derives `operation_id` with the same stable
operation-key algorithm used by Health catalogs.

Render `result.recommended` and `result.avoid` exactly. Do not infer a more
specific cause from HTTP 401/403. A response-only provider outage has no
`reissue_credential` avoid action; that action requires a qualifying Health
observation or provider notice.

`capabilities.dataset_application` is a navigation entry, not a submission URL.
`local_reproduction` never carries credentials, query values, response bodies,
or a shell command. `reusable_export` becomes available only after a successful
local reusable result and names JSON and CSV formats without embedding rows or
private artifact locations.

An available export with `evidence_level: parseable_transport_result` and
`semantic_validation: not_proven` means only that JSON/CSV can be reused. It
does not mean the Registry `ready` cause or semantic/data-quality success.

`metrics.time_to_diagnosis_ms` and `metrics.time_to_first_success_ms` require an
explicit journey start, diagnosis time, and first-success time. A single
`get`/`sync` attempt does not establish that cross-action clock and therefore
omits these product metrics. Metric inputs never contain credentials, hashes,
URLs, response bodies, or user identity.

`capabilities.public_health` remains:

```json
{"status":"unavailable","reason":"health_identity_dependency_unavailable"}
```

Web must preserve this unavailable state until the accepted Datapan Health #19
receipt supplies the matching operation identity. Registry release #568 is the
separate gate for promoting the reviewed draft contract to a published
production contract.

## Exact Web field policy

The handoff is an additive object below `diagnostic.consumer_handoff`. Datapan
Web consumes only the following fields; absence, wrong type, or an unlisted enum
value fails that capability closed:

| Object | Consumed fields |
| --- | --- |
| `contract` | `status`, `schema_sha256`, `mapping_sha256`, `runtime_authority` |
| `subject` | `source_id`, `provider_id`, `dataset_id`, `operation_id` |
| `result` | `code`, `determination`, `layer`, `accountable_party`, and each `recommended[]` / `avoid[]` item's `action_id`, `actor`, `rationale_id` |
| `capabilities.dataset_application` | `route_kind`, `url`, `direct_submission_url` |
| `capabilities.local_reproduction` | `status`, `mode`, `credential_handling`, `requires_credential` |
| `capabilities.reusable_export` | `status`, `formats`, `reason`, `evidence_level`, `semantic_validation` |
| `capabilities.public_health` | `status`, `reason` |
| `metrics` | `time_to_diagnosis_ms`, `time_to_first_success_ms` |

The outer CLI-only `diagnostic` fields (`code`, `responsible_party`,
`evidence_state`, `evidence_authority`, `observed_at`, `scope`, `evidence`,
`recommended_actions`, `prohibited_actions`, and attempt-level `timing`) are
ignored by Web as authority inputs. Provider `body`, `message`, `url`,
`provider_status`, Registry routing internals, cache paths, rows, query values,
credentials, headers, and user identity are unsupported handoff inputs and must
never be copied into the handoff.

Unknown additive fields inside `consumer_handoff` are ignored for forward
compatibility, but unknown values in the consumed identity, result, capability,
or contract fields are not interpreted. They make the affected projection
unavailable. In particular, Web must not infer a cause, service identity, or
reusable state from ignored fields.

The command boundary accepts `--journey-started-at RFC3339` on a failed
`get`/`sync` to emit `time_to_diagnosis_ms`. A failed command always uses its
own runtime `diagnosis_computed_at`; a caller-supplied `--journey-diagnosed-at`
cannot replace or extend that metric. A successful retry or call-based JSON/CSV
export accepts the prior `--journey-diagnosed-at RFC3339`; it must not precede
the journey start. The first-success timestamp is captured at the successful
provider-call boundary, not supplied by the caller. Without these explicit
cross-command timestamps, the metrics remain omitted.
