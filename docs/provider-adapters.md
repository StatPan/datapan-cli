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
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --limit 20 --json
```

`catalog providers` summarizes the backlog by host. `catalog adapter-targets`
turns missing external endpoint and service-root operations into a ranked work
queue with operation/spec counts and sample operations, which is usually the
better starting point for deciding the next adapter implementation.

To inspect one likely provider family:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider q-net --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider q-net --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider epost --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider epost --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider ekape --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider ekape --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider forest --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider forest --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider folk --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider folk --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider airport --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider airport --json
```

To inspect hosts that already have an observation-stage adapter registered:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider q-net --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider epost --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider ekape --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider forest --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider folk --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider airport --json
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
It also registers epost host ownership for `openapi.epost.go.kr` and
`openapi.epost.go.kr:80`, so q-net is no longer the only exercised external
adapter family.
EKAPE host ownership for `data.ekape.or.kr` adds a third adapter family and
captures upstream key-registration failures as provider evidence rather than
leaving those operations as generic missing-adapter skips.
The forest adapter owns `api.forest.go.kr` and verifies a small but real
external provider family with observed `NORMAL SERVICE` XML responses.
The folk adapter owns `folkency.nfm.go.kr` and verifies National Folk Museum
multimedia list APIs with provider-specific JSON `result_code=200` responses.
The airport adapter owns `openapi.airport.co.kr` and captures Korea Airports
Corporation low-visibility API credential-registration responses as
provider-specific evidence instead of leaving those operations as generic
missing-adapter gaps.
Release drafts also publish `data/provider-index.json` using
`schemas/datapan.provider-index.v1.schema.json` so consumers can distinguish
registered adapter ownership from backlog observations.
They also publish `reports/error-catalog.json` using
`schemas/datapan.error-catalog.v1.schema.json` so provider adapters can preserve
upstream status fields instead of translating every provider error into a
Datapan-only shape.

## First Adapter: q-net

The q-net adapter starts narrow, not broad:

1. Pull q-net hosts from `catalog providers` and `catalog adapter-targets`.
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
datapan catalog verify summary --input .datapan/qnet-batch-verification.json --json
```

## Second Adapter: epost

The epost adapter covers postal API hosts that data.go.kr catalogs as external
endpoints:

```text
openapi.epost.go.kr:80   22 operations
openapi.epost.go.kr       6 operations
```

The first epost boundary is deliberately conservative. It owns the hosts,
participates in provider-index release artifacts, and verifies REST XML
operation URLs when required parameters are supplied or have safe pagination
defaults such as `countPerPage=1` and `currentPage=1`. It skips WADL metadata
URLs with `epost_wadl_metadata_only`, SOAP operations with
`epost_unsupported_protocol`, and unknown required parameters with
`epost_missing_required_params`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider epost --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider epost --kind external_endpoint --limit 5 --output .datapan/epost-batch-verification.json --json
datapan catalog verify summary --input .datapan/epost-batch-verification.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15075270 --operation "우체국알뜰폰 요금제 조회" --output .datapan/epost-single-verification.json --json
```

Expected evidence shape: registered adapter ownership for both epost hosts,
redacted URLs, provider-specific skip reasons for WADL/SOAP/required-parameter
cases, `provider=epost` in verification results, and stable provider error
reasons such as `epost_service_key_not_registered` when the upstream epost
service rejects the shared data.go.kr key.

## Third Adapter: ekape

The EKAPE adapter covers livestock quality evaluation APIs hosted at
`data.ekape.or.kr`. These APIs often need domain identifiers such as issue
numbers, lot numbers, and base months. The adapter supplies conservative
read-only verification defaults for those fields so Datapan can reach the
provider and classify its response, while still redacting service keys.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider ekape --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider ekape --kind external_endpoint --limit 5 --output .datapan/ekape-batch-verification.json --json
datapan catalog verify summary --input .datapan/ekape-batch-verification.json --json
```

Expected evidence shape: `provider=ekape`, `endpoint_host=data.ekape.or.kr`,
redacted URLs, stable provider-specific reasons such as
`ekape_service_key_not_registered`, and preserved upstream `resultCode` /
`resultMsg` under `provider_status`. A failed EKAPE verification is still useful
evidence when it proves that the request reached the external provider and the
provider rejected the credential registration state.

## Fourth Adapter: forest

The forest adapter covers Korea Forest Service culture information APIs hosted
at `api.forest.go.kr`. It is intentionally small: the current registry contains
four external operations across two datasets. The value is evidence quality.
Those operations need search terms, so the adapter supplies conservative
read-only defaults such as `searchWrd=소나무`, `searchMtNm=북한산`,
`searchArNm=서울`, `pageNo=1`, and `numOfRows=1`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider forest --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider forest --kind external_endpoint --limit 4 --output .datapan/forest-verification.json --json
datapan catalog verify summary --input .datapan/forest-verification.json --json
```

Expected evidence shape: `provider=forest`, `endpoint_host=api.forest.go.kr`,
redacted URLs, XML response shapes, `semantic_status=provider_ok` for working
operations, and stable provider-specific reasons such as
`forest_service_key_not_registered` when upstream rejects a key.

## Fifth Adapter: folk

The folk adapter covers National Folk Museum encyclopedia multimedia APIs
hosted at `folkency.nfm.go.kr`. It is another small but high-quality adapter:
the registry currently exposes three JSON operations, and the two list
operations return provider-specific `result_code=200` JSON with item counts.
The detail operation needs opaque identifiers such as `tit_idx`, `group_name`,
and `md_idx`, so Datapan skips it until those identifiers come from a prior
list response or a user-supplied parameter set.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider folk --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider folk --kind external_endpoint --limit 3 --output .datapan/folk-verification.json --json
datapan catalog verify summary --input .datapan/folk-verification.json --json
```

Expected evidence shape: `provider=folk`, `endpoint_host=folkency.nfm.go.kr`,
redacted URLs, `semantic_status=provider_ok` for list operations,
`body_shape=json_items`, and `folk_missing_required_params` for detail
operations that lack required identifiers.

## Sixth Adapter: airport

The airport adapter covers Korea Airports Corporation APIs hosted at
`openapi.airport.co.kr`. The current registry exposes low-visibility warning
operations where several endpoints need no domain-specific search parameter and
list endpoints only need safe paging defaults such as `pageNo=1` and
`numOfRows=1`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider airport --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider airport --kind external_endpoint --limit 6 --output .datapan/airport-verification.json --json
datapan catalog verify summary --input .datapan/airport-verification.json --json
```

Expected evidence shape: `provider=airport`,
`endpoint_host=openapi.airport.co.kr`, redacted URLs, XML or JSON provider
status bodies, and stable provider-specific reasons such as
`airport_service_key_not_registered` when the upstream service rejects the
current data.go.kr key registration state. A failed airport verification can
still be useful evidence when it proves that the request reached the external
provider and the provider rejected credential registration.

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
real verification and at least one provider has call behavior. Move to
`datapan-providers` only when the interface has been exercised by multiple
providers and the release boundary is worth maintaining separately.

The provider index now makes that decision explicit under `split_readiness`.
Consumers and maintainers should treat `split_readiness.status=not_ready` as a
deliberate boundary decision, not a failure. The current adapter set has enough
registered verification-capable providers, but no adapter has declared stable
`call` capability yet, so the recommendation remains to keep provider adapters
inside `datapan-cli` while the call surface is proven.
