# Provider Adapters

Provider adapters are the bridge from catalog knowledge to actual runtime
coverage. `data.go.kr` can list many APIs, but not every endpoint is served by
the data.go.kr gateway. Some operations point to external hosts, service roots,
SOAP/WMS protocols, provider-specific approval flows, or separate error
formats. Datapan should make those gaps explicit and then close them adapter by
adapter.

## Current Boundary

The first code boundary lives in `internal/provider`.

An adapter owns:

- provider name;
- host matching;
- dependency classification;
- verification behavior;
- call behavior;
- provider-specific credential and error handling.

The CLI still owns:

- command contracts;
- registry loading;
- common JSON/CSV output;
- exit codes;
- `.env` loading;
- redaction and local credential policy.

This keeps provider-specific behavior from leaking into every command while
letting the CLI remain the headless runtime for future Studio and SDK layers.

## Minimal Interface

The current minimum shape is:

```go
type Adapter interface {
    Name() string
    Hosts() []string
    MatchHost(host string) bool
    DependencyClass(spec datago.Spec, op datago.Operation) string
    Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult
    Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error)
}
```

Adapters use Datapan's normalized `Spec`, `Operation`, `VerificationResult`,
and `ResponseEnvelope` types. That is intentional: the registry, CLI, Studio,
and SDK generators should all see one Datapan-shaped contract instead of a
different shape per provider.

## Adapter Registry

`internal/provider.Registry` is the host-to-adapter lookup layer. It should stay
small:

- register adapters;
- reject duplicate host ownership;
- match a normalized host to one adapter;
- expose registered hosts for diagnostics.

The registry is deliberately separate from the public-data catalog registry.
The catalog registry describes datasets. The adapter registry describes runtime
ownership for hosts.

## Adapter Selection

Use `catalog providers` to choose adapter work by evidence:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --limit 20 --json
```

To inspect one likely provider family:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider q-net --json
```

To inspect hosts that already have an observation-stage adapter registered:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider q-net --json
```

The current imported registry shows q-net as a strong early adapter family:

```text
openapi.q-net.or.kr   104 operations
c.q-net.or.kr          42 operations
open.api.q-net.or.kr    1 operation
```

Those numbers are not a guarantee that the APIs work. They are adapter backlog
evidence: enough surface exists to justify tracking q-net as a provider family.
Datapan now registers q-net host ownership so `catalog providers --status
adapter --provider q-net` can separate q-net from hosts that still have no
adapter at all.

## First Adapter: q-net

The q-net adapter starts narrow, not broad:

1. Pull q-net hosts from `catalog providers`.
2. Inspect sample dataset IDs from the provider backlog report.
3. Identify credential requirements and approval behavior.
4. Identify response success and error envelopes.
5. Add safe verification for a narrow operation subset.
6. Expand coverage only when each endpoint family has evidence.
7. Only then add call support.

The adapter must not bypass provider security gates or pretend approval is
present when it is not. If a human login, CAPTCHA, manual approval, or separate
key is required, the adapter should return a conservative skipped or failed
status with provider-specific evidence.

The q-net adapter owns `openapi.q-net.or.kr`, `c.q-net.or.kr`, and
`open.api.q-net.or.kr`. It can verify a narrow XML `openapi.q-net.or.kr`
subset when required parameters are either supplied or have conservative
provider-specific defaults such as `baseYY=2023`, `pageNo=1`, and
`numOfRows=1`. It still skips operations with unknown required parameters such
as `jmCd` using `qnet_missing_required_params`, and it does not claim call
support yet.

Q-Net endpoint families are intentionally separated:

- `_wadl` endpoints are metadata-only and return `qnet_wadl_metadata_only`
  instead of being counted as data verification.
- `c.q-net.or.kr` is skipped with `qnet_separate_service_key_required` until
  separate credential or registration evidence exists.
- JSON `{message: "...ERROR"}` responses are provider errors, not successful
  JSON data responses.
- Known provider failures use stable Datapan reasons such as
  `qnet_connection_validation_failed` and `qnet_service_key_not_registered`,
  while preserving the original upstream message under `provider_status`.

Observed smoke evidence:

```bash
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15025329 --operation "연도별 등급별 실기 합격률 조회" --json
```

Expected evidence shape: `provider=q-net`, `dependency_class=external_endpoint`,
`status=verified`, `semantic_status=provider_ok`, `provider_status.code=00`,
and `body_shape=xml_items`.

For bounded batch evidence, filter verification before the limit:

```bash
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider q-net --kind external_endpoint --limit 5 --output .datapan/qnet-batch-verification.json --json
```

## Adapter Readiness Bar

A provider adapter is not ready just because it can build a URL. It needs:

- host matching tests;
- response classification tests;
- at least one bounded verification path;
- redacted request evidence;
- provider-specific skip reasons;
- documentation for credentials and approval;
- no credential printing;
- no silent fallback to guessed parameters.

## Split Trigger

Keep adapters inside `datapan-cli` until at least two external providers have
real verification and call behavior. Move to `datapan-providers` only when the
interface has been exercised by multiple providers and the release boundary is
worth maintaining separately.
