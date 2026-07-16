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
