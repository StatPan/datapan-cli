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

Before writing an adapter for a missing external host, run a credential-free
transport probe:

```bash
datapan catalog verify --registry .datapan/data-go-kr.registry.json --kind external_endpoint --host openapi.price.go.kr --probe-unadapted --timeout 12s --json
```

`--probe-unadapted` is not a substitute for a provider adapter. It exists to
turn ambiguous skips into evidence: DNS failures, timeouts, HTTP 404s, HTTP
503s, and other transport errors. A host that only returns dead-route evidence
belongs in the external endpoint drift backlog before it becomes adapter work.

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
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider gblib --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider gblib --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider airport --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider airport --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider andong --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider andong --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider geoje --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider geoje --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider humetro --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider humetro --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider itfind --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider itfind --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider jeju --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider jeju --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider korad --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider korad --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider kpx --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider kpx --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider lh-ebid --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider lh-ebid --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider myhome --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider myhome --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider emuseum --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider emuseum --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider naqs --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider naqs --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider pqis --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider pqis --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider seoul-bus --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider seoul-bus --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider sisul --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider sisul --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider uiryeong --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider uiryeong --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider ulsan --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider ulsan --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --kind external_endpoint --provider jeonju --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider jeonju --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status missing --provider tour --json
datapan catalog adapter-targets --registry .datapan/data-go-kr.registry.json --provider tour --json
```

To inspect hosts that already have an observation-stage adapter registered:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider q-net --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider epost --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider ekape --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider forest --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider folk --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider gblib --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider airport --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider andong --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider geoje --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider humetro --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider itfind --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider jeju --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider korad --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider kpx --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider lh-ebid --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider myhome --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider emuseum --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider naqs --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider pqis --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider seoul-bus --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider sisul --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider uiryeong --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider ulsan --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider jeonju --json
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider tour --json
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
The gblib adapter owns `openapi.gblib.or.kr`, covering Gangbuk library and
sports-center APIs. It synthesizes `serviceKey`, supplies safe smoke defaults
for search/date/page fields, classifies `resultCode=99` key-registration
responses, and records the current reading-room endpoint `HTTP 404` as
provider evidence instead of leaving the host as a missing adapter.
The airport adapter owns `openapi.airport.co.kr` and captures Korea Airports
Corporation low-visibility API credential-registration responses as
provider-specific evidence instead of leaving those operations as generic
missing-adapter gaps.
The andong adapter owns `www.andong.go.kr`, a REST XML local-government host.
It synthesizes the live `serviceKey` auth parameter, fills only conservative
paging defaults including the upstream `numOfRowns` spelling, skips opaque
district/detail/file identifiers, and records upstream service-registration
errors as provider evidence instead of a generic missing-adapter gap.
The geoje adapter owns `data.geoje.go.kr`, a high-priority local-government
external host. It uses the normal `serviceKey` credential, proves
`resultCode=00` XML list responses, skips ID-only detail operations rather than
inventing identifiers, and declares call capability for operations whose
parameters are supplied or safely defaultable.
The Humetro adapter owns `data.humetro.busan.kr`, synthesizes `ServiceKey` even
when older metadata omits it, supplies conservative XML/station/date/paging
defaults, and records upstream `SERVICE ACCESS DENIED` or deadline-expired XML
status bodies as provider evidence.
The itfind adapter owns `open.itfind.or.kr`, an ICT research and publication
host with REST XML endpoints. It synthesizes the live `serviceKey` parameter,
fills only conservative paging defaults, skips opaque identifier-only detail
operations, and records `NORMAL SERVICE` XML responses as direct call evidence.
The KORAD adapter owns `www.korad.or.kr`, skips provider WADL metadata endpoints
with `korad_wadl_metadata_only`, synthesizes `serviceKey`, fills conservative
year/month/quarter/date defaults, respects approval-required operations, and
can classify upstream key-registration rejection as provider evidence once the
call is legitimately allowed.
The lh-ebid adapter owns `openapi.ebid.lh.or.kr`, Korea Land and Housing
Corporation's electronic-bidding API host. It supplies conservative date/month
and paging defaults for list-style bid, order plan, pre-price, and opening
result endpoints, while leaving opaque identifiers such as `bidNum` as explicit
missing-parameter skips.
The seoul-bus adapter owns `ws.bus.go.kr`, Seoul's bus-position API host. It
adds the data.go.kr API key as `serviceKey`, supplies conservative route and
stop-order defaults for route-position endpoints, skips vehicle-ID-only calls,
and parses `ServiceResult/msgHeader` so provider key-registration errors remain
structured evidence instead of generic XML success.
The NAQS adapter owns `data.naqs.go.kr`, verifies the no-auth environmental
certification XML endpoint, and deliberately skips `pubc` integration
endpoints with `naqs_mutation_endpoint` because their parameters model
insert/update/delete style data exchange rather than safe catalog reads.
The PQIS adapter owns `openapi.pqis.go.kr`, the Animal and Plant Quarantine
Agency plant quarantine statistics host. The imported registry records the
WADL metadata URL as the endpoint, so the adapter rewrites calls to the live
WADL-declared operation paths: `nationCode`, `plantCode`, `importStats`, and
`exportStats`. It fills conservative code/date/paging defaults and records
upstream `SERVICE KEY IS NOT REGISTERED` responses as provider-specific
evidence.
The sisul adapter owns `data.sisul.or.kr`, Seoul Facilities Corporation's
OpenDB host. Its catalog contains both callable `get...Qry` endpoints and
WADL metadata URLs, so the adapter explicitly skips `_wadl` metadata,
synthesizes the live `serviceKey` auth parameter, fills only conservative
paging defaults, and records upstream auth/timeout behavior as provider
evidence instead of treating metadata as data.
The uiryeong adapter owns `data.uiryeong.go.kr`, the next high-coverage
local-government host. It preserves the upstream `ServiceKey` parameter,
separates list filters from opaque detail/file IDs, and records the current
upstream key-registration rejection as provider evidence instead of a generic
missing-adapter gap.
The ulsan adapter owns `openapi.its.ulsan.kr`, a REST XML traffic provider
whose catalog records omit the authentication parameter even though the live
service requires `serviceKey`. The adapter supplies that credential parameter,
fills only conservative paging defaults, skips unknown route/road/date
identifiers, and records upstream `resultCode=30` key-registration responses as
provider evidence.
The tour adapter owns `openapi.tour.go.kr`, a Korea Culture & Tourism
Institute statistics host whose imported records mix callable `operation_url`
values with older service-root-only records. The adapter routes calls through
the source `operation_url` when present, synthesizes `serviceKey`, fills
conservative year/month/code defaults, and skips service-root-only records with
`tour_service_root_missing_operation_path` instead of inventing operation
paths. It also classifies data.go.kr XMLFault authentication responses as
stable tour provider evidence.
The jeonju adapter owns `openapi.jeonju.go.kr`, currently the largest missing
external host family in the imported registry. It preserves the upstream
credential parameter name (`ServiceKey` or `authApiKey`) and only fills
conservative paging/location/search defaults; any unknown required parameter
remains an explicit `jeonju_missing_required_params` skip instead of a guessed
call. The current upstream REST endpoints return `HTTP 405` to normal GET/POST
checks, so the adapter is verification-capable but does not declare call
capability yet.
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

The epost boundary is deliberately conservative, but now call-capable. It owns
the hosts, participates in provider-index release artifacts, verifies REST XML
operation URLs, and can route `datapan get` through the provider boundary when
required parameters are supplied or have safe pagination defaults such as
`countPerPage=1` and `currentPage=1`. It skips WADL metadata URLs with
`epost_wadl_metadata_only`, SOAP operations with `epost_unsupported_protocol`,
and unknown required parameters with `epost_missing_required_params`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider epost --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider epost --kind external_endpoint --limit 5 --output .datapan/epost-batch-verification.json --json
datapan catalog verify summary --input .datapan/epost-batch-verification.json --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --ref 15075270 --operation "우체국알뜰폰 요금제 조회" --output .datapan/epost-single-verification.json --json
datapan get 15075270 --operation "우체국알뜰폰 요금제 조회" --json
```

Expected evidence shape: registered adapter ownership for both epost hosts,
redacted URLs, provider-specific skip reasons for WADL/SOAP/required-parameter
cases, `provider=epost` in verification results, and stable provider error
reasons such as `epost_service_key_not_registered` when the upstream epost
service rejects the shared data.go.kr key. Call evidence should return a
`provider=epost` response envelope with redacted URL, upstream HTTP status,
semantic status, provider status when available, and the raw body.

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
Forest is also a call-capable external adapter: `datapan get` can route
`api.forest.go.kr` operations through the provider boundary, preserve redacted
URLs, classify upstream provider status, and return the raw body without falling
back to generic gateway assumptions.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider forest --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider forest --kind external_endpoint --limit 4 --output .datapan/forest-verification.json --json
datapan catalog verify summary --input .datapan/forest-verification.json --json
datapan get <forest-dataset-id> --operation <forest-operation> --json
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

## Seventh Adapter: jeonju

The jeonju adapter covers Jeonju city APIs hosted at `openapi.jeonju.go.kr`.
The host is high-impact because the current full registry exposes dozens of
Jeonju operations across transport, administration, tourism, safety, health, and
environment categories. These APIs mix `ServiceKey` and `authApiKey`, so the
adapter preserves the exact credential parameter requested by each operation
instead of blindly sending `serviceKey`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider jeonju --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider jeonju --kind external_endpoint --limit 5 --output .datapan/jeonju-verification.json --json
datapan catalog verify summary --input .datapan/jeonju-verification.json --json
```

Expected evidence shape: `provider=jeonju`,
`endpoint_host=openapi.jeonju.go.kr`, redacted URLs with `ServiceKey=REDACTED`
or `authApiKey=REDACTED`, XML/JSON response shapes, provider status when
available, and `jeonju_missing_required_params` for operations whose required
domain filters are not safe to guess. Current upstream method failures are
preserved as stable reasons such as `jeonju_http_405_method_not_allowed`.

## Eighth Adapter: geoje

The geoje adapter covers Geoje city APIs hosted at `data.geoje.go.kr`. The host
is the next high-coverage local-government family after Jeonju in the current
registry. Geoje APIs use the normal `serviceKey` parameter and return XML
`rfcOpenApi` envelopes with `resultCode` / `resultMsg` provider status fields.

The adapter supplies only conservative list defaults such as `startPage=1` and
`pageSize=1`. Search filters are not sent unless the user provides values, and
opaque detail IDs such as `geojemedicalId` remain missing so Datapan records a
`geoje_missing_required_params` skip instead of generating bad provider calls.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider geoje --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider geoje --kind external_endpoint --limit 6 --output .datapan/geoje-verification.json --json
datapan catalog verify summary --input .datapan/geoje-verification.json --json
datapan get <geoje-dataset-id> startPage=1 pageSize=1 --operation <geoje-list-operation> --json
```

Expected evidence shape: `provider=geoje`,
`endpoint_host=data.geoje.go.kr`, redacted URLs with `serviceKey=REDACTED`, XML
`rfcOpenApi` response bodies, `provider_status.code=00` for working list
operations, `geoje_missing_required_params` for ID-only detail operations, and
stable provider failures such as `geoje_common_error` or
`geoje_provider_sql_error` when upstream returns those bodies.

## Ninth Adapter: uiryeong

The uiryeong adapter covers Uiryeong county APIs hosted at
`data.uiryeong.go.kr`. The current registry exposes dozens of XML REST
operations across administration, tourism, welfare, traffic, health, safety,
and science categories. These APIs use an upstream `ServiceKey` parameter and
return XML `rfcOpenApi` envelopes with `resultCode` / `resultMsg`.

The adapter is intentionally careful with identifier-looking fields. In list
operations, `EntId`, title, address, type, kind, and similar fields are treated
as optional filters and omitted when no value is supplied. In detail, view,
file, and photo operations, opaque IDs remain required so Datapan records
`uiryeong_missing_required_params` instead of inventing identifiers.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider uiryeong --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider uiryeong --kind external_endpoint --limit 6 --output .datapan/uiryeong-verification.json --json
datapan catalog verify summary --input .datapan/uiryeong-verification.json --json
datapan get 15008883 pageNo=1 numOfRows=1 --operation "도시공원정보 목록" --json
```

Expected evidence shape: `provider=uiryeong`,
`endpoint_host=data.uiryeong.go.kr`, redacted URLs with `ServiceKey=REDACTED`,
XML `rfcOpenApi` status bodies, `provider_status.code=99` with the upstream
Korean key-registration message when the current data.go.kr key is not
registered, and stable provider reasons such as
`uiryeong_service_key_not_registered`. A failed verification is still useful
evidence when it proves the request reached the external provider and the
provider rejected the credential registration state.

## Tenth Adapter: korad

The korad adapter covers Korea Radioactive Waste Agency APIs hosted at
`www.korad.or.kr`. The registry currently contains both WADL metadata URLs and
actual REST XML operation URLs. Datapan deliberately skips `_wadl` URLs as
metadata-only endpoints and routes only the concrete service operation paths.

The adapter synthesizes `serviceKey`, adds conservative smoke defaults such as
`pageNo=1`, `numOfRows=1`, `yyyy=2024`, `yyyymm=202401`, `quart=1`, and
`approvalDate=20240101`, and leaves unknown required identifiers unset. Search
filters such as `nuclide`, `contractNm`, and `subject` are optional and omitted
when no user value is supplied. Approval-required operations remain skipped
until the user's upstream permission state allows a legitimate call.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider korad --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider korad --kind external_endpoint --limit 15 --output .datapan/korad-verification.json --json
datapan catalog verify summary --input .datapan/korad-verification.json --json
datapan get <korad-dataset-id> pageNo=1 numOfRows=1 yyyy=2024 --operation <korad-list-operation> --json
```

Expected evidence shape: `provider=korad`,
`endpoint_host=www.korad.or.kr`, WADL metadata skips with
`korad_wadl_metadata_only`, approval-gated REST operations reported as
`approval_required`, and redacted URLs with `serviceKey=REDACTED` for callable
operations. If an approved call reaches KORAD but the upstream service rejects
the credential registration state, Datapan classifies that as
`korad_service_key_not_registered` rather than a generic provider failure.

## Eleventh Adapter: naqs

The naqs adapter covers National Agricultural Products Quality Management
Service APIs hosted at `data.naqs.go.kr`. The imported catalog currently mixes
one safe XML lookup endpoint, `naqsenv/envparam`, with several `pubc`
integration endpoints whose parameter contracts include `proc=I/U/D` and
domain identifiers for produce traceability records.

Datapan treats those `pubc` endpoints as mutation-like integration boundaries.
Verification records `naqs_mutation_endpoint` before HTTP instead of guessing
business identifiers or risking write-like calls. The environmental
certification endpoint is no-auth XML and can be verified with an empty
`certno` smoke value; user-provided `certno` values are passed through for
explicit `datapan get` calls.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider naqs --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider naqs --kind external_endpoint --limit 9 --output .datapan/naqs-verification.json --json
datapan catalog verify summary --input .datapan/naqs-verification.json --json
datapan get 15000935 certno=1 --operation "친환경인증정보" --json
```

Expected evidence shape: `provider=naqs`,
`endpoint_host=data.naqs.go.kr`, `xml_env_response` for the safe lookup
endpoint, `provider_status.code=00` with `NORMAL SERVICE.`, and
`naqs_mutation_endpoint` skips for `pubc` integration operations.

## Twelfth Adapter: humetro

The humetro adapter covers Busan Transportation Corporation APIs hosted at
`data.humetro.busan.kr`. These APIs use `.tnn` REST XML endpoints for station,
public facility, event, air quality, noise, and contract information. Several
older catalog records omit the credential parameter, but live provider probes
show that the upstream still expects a `ServiceKey`.

The adapter therefore synthesizes `ServiceKey`, redacts it, and supplies
conservative defaults: `act=xml`, `scode=101`, `year=2024`, `pageNo=1`,
`numOfRows=1`, `kind=1`, `c_page=1`, `c_size=1`, and a bounded 2024 date range
for event queries. Current evidence reaches the provider and records
provider-specific XML status failures such as `humetro_service_access_denied`
and `humetro_deadline_expired`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider humetro --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider humetro --kind external_endpoint --limit 8 --output .datapan/humetro-verification.json --json
datapan catalog verify summary --input .datapan/humetro-verification.json --json
datapan get <humetro-dataset-id> act=xml scode=101 --operation <humetro-operation> --json
```

Expected evidence shape: `provider=humetro`,
`endpoint_host=data.humetro.busan.kr`, redacted URLs with
`ServiceKey=REDACTED`, XML status bodies, and stable provider reasons such as
`humetro_service_access_denied`. Failed verification remains useful because it
proves Datapan reached the external provider and classified the credential or
provider state.

## Thirteenth Adapter: oneclick-law

The oneclick-law adapter covers Ministry of Government Legislation Easy Law
SOAP APIs hosted at `oneclick.law.go.kr` and `oneclick.law.go.kr:80`. These
catalog records expose SOAP service endpoints, operation names in
`operation_url`, and request parameter names in `request_param_nm_en`.

The adapter builds SOAP 1.1 POST envelopes from that metadata, inserts
`ServiceKey` only when the operation declares it, supplies conservative smoke
defaults for request IDs, page fields, section/query fields, and common Easy
Law identifiers, and skips approval-required operations before HTTP. Current
live probes show the upstream endpoint refusing connections, so Datapan records
`oneclick_connection_refused` instead of treating these operations as unknown
or silently unsupported.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider oneclick-law --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider oneclick-law --kind external_endpoint --limit 30 --output .datapan/oneclick-law-verification.json --json
datapan catalog verify summary --input .datapan/oneclick-law-verification.json --json
datapan get <oneclick-dataset-id> txtQuery=법 nowPageNo=1 pageMg=1 --operation <oneclick-soap-operation> --json
```

Expected evidence shape: `provider=oneclick-law`,
`endpoint_host=oneclick.law.go.kr` or `oneclick.law.go.kr:80`, SOAP POST
requests, `approval_required` skips for 심의승인 operations, and stable
transport reasons such as `oneclick_connection_refused` when the upstream host
refuses the connection.

## Fourteenth Adapter: tour

The tour adapter covers Korea Culture & Tourism Institute tourism statistics
APIs hosted at `openapi.tour.go.kr`. The imported catalog has two different
shapes for this host: newer operations include full `operation_url` values,
while older records only expose the shared
`http://openapi.tour.go.kr/openapi/service` service root.

The adapter uses the source `operation_url` as the call endpoint when present,
adds the data.go.kr API key as `serviceKey`, and fills only conservative
verification defaults such as `YY=2024`, `YM=202401`, `numOfRows=1`, and
`pageNo=1`. Service-root-only records are skipped with
`tour_service_root_missing_operation_path` because Datapan should preserve the
upstream gap instead of inventing paths for a public catalog with thousands of
operations.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider tour --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider tour --limit 26 --output .datapan/tour-verification.json --json
datapan catalog verify summary --input .datapan/tour-verification.json --json
datapan get <tour-dataset-id> YM=202401 --operation getTourismBalcList --json
```

Expected evidence shape: `provider=tour`,
`endpoint_host=openapi.tour.go.kr`, redacted URLs with `serviceKey=REDACTED`,
`tour_service_root_missing_operation_path` skips for root-only records, and
stable provider reasons such as `tour_missing_auth` for XMLFault authentication
responses.

## Fifteenth Adapter: lh-ebid

The lh-ebid adapter covers Korea Land and Housing Corporation electronic bidding
APIs hosted at `openapi.ebid.lh.or.kr`. The current registry exposes six REST
XML operations across bid notices, contracts, order plans, prior-specification
notices, planned prices, and bid-opening results.

The adapter adds the data.go.kr API key as `serviceKey`, fills only conservative
read windows such as `20240101..20240131`, `202401..202412`, `pageNo=1`, and
`numOfRows=1`, and skips opaque identifiers such as `bidNum` instead of
inventing values. Live probes reach the provider and currently classify the
shared key's unregistered state as `lh_ebid_service_key_not_registered`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider lh-ebid --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider lh-ebid --kind external_endpoint --limit 6 --output .datapan/lh-ebid-verification.json --json
datapan catalog verify summary --input .datapan/lh-ebid-verification.json --json
datapan get <lh-ebid-dataset-id> tndrbidRegDtStart=20240101 tndrbidRegDtEnd=20240131 --operation "입찰정보 조회" --json
```

Expected evidence shape: `provider=lh-ebid`,
`endpoint_host=openapi.ebid.lh.or.kr`, redacted URLs with
`serviceKey=REDACTED`, `lh_ebid_missing_required_params` skips for opaque
identifier operations, XML status bodies, and stable provider reasons such as
`lh_ebid_service_key_not_registered`.

## Sixteenth Adapter: seoul-bus

The seoul-bus adapter covers Seoul bus-position APIs hosted at `ws.bus.go.kr`.
The current registry exposes route-position, low-floor route-position, and
vehicle-position REST XML operations.

The adapter adds the data.go.kr API key as `serviceKey`, fills safe smoke
defaults such as `busRouteId=100100118`, `startOrd=1`, and `endOrd=5`, and
does not invent `vehId` values. Live probes reach the provider and currently
classify the shared key's unregistered state as
`seoul_bus_service_key_not_registered`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider seoul-bus --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider seoul-bus --kind external_endpoint --limit 5 --output .datapan/seoul-bus-verification.json --json
datapan catalog verify summary --input .datapan/seoul-bus-verification.json --json
datapan get 15000332 busRouteId=100100118 --operation getBusPosByRtidList --json
```

Expected evidence shape: `provider=seoul-bus`, `endpoint_host=ws.bus.go.kr`,
redacted URLs with `serviceKey=REDACTED`, `ServiceResult/msgHeader`
`provider_status`, `seoul_bus_missing_required_params` skips for vehicle-ID
operations, and stable provider reasons such as
`seoul_bus_service_key_not_registered`.

## Seventeenth Adapter: gblib

The gblib adapter covers Gangbuk Urban Management Corporation library and
sports-center APIs hosted at `openapi.gblib.or.kr`. The current registry has
three REST XML operations: sports-center usage, library book search, and
reading-room status.

The adapter adds the data.go.kr API key as `serviceKey`, fills conservative
smoke defaults such as `keyword=공공`, `pub=도서`, `lib=MA`, `org=1`,
`date=20260625`, `pageNo=1`, and `numOfRows=1`, and classifies provider
`resultCode/resultMsg` XML statuses. Live probes currently return
`gblib_service_key_not_registered` for callable search/sports endpoints and
`gblib_endpoint_not_found` for the reading-room endpoint.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider gblib --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider gblib --kind external_endpoint --limit 3 --output .datapan/gblib-verification.json --json
datapan catalog verify summary --input .datapan/gblib-verification.json --json
datapan get 3075291 keyword=공공 pub=도서 lib=MA --operation "도서자료검색" --json
```

Expected evidence shape: `provider=gblib`,
`endpoint_host=openapi.gblib.or.kr`, redacted URLs with
`serviceKey=REDACTED`, `resultCode/resultMsg` `provider_status`,
`gblib_service_key_not_registered` for unregistered credentials, and
`gblib_endpoint_not_found` for the current reading-room endpoint.

## Eighteenth Adapter: kpx

The kpx adapter covers Korea Power Exchange electricity market APIs hosted at
`openapi.kpx.or.kr`. The registry contains a small family of operation-specific
paths such as `forecast1dMaxBaseDate/getForecast1dMaxBaseDate`,
`sukub5mToday/getSukub5mToday`, `smp1hToday/getSmp1hToday`, and
`sumperfuel5m/getSumperfuel5m`; a few registry endpoint values omit the URL
scheme, so the adapter normalizes them to `https://`.

The adapter adds `serviceKey`, fills conservative paging defaults
`pageNo=1` and `numOfRows=1`, preserves operation-specific paths, and
classifies `resultCode/resultMsg` XML statuses. The current registry marks the
KPX operations as production approval-required, so Datapan records bounded
verification as `approval_required` instead of making live calls with
unapproved credentials.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider kpx --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider kpx --kind external_endpoint --limit 6 --output .datapan/kpx-verification.json --json
datapan catalog verify summary --input .datapan/kpx-verification.json --json
datapan get 15043670 --operation "현재전력수급현황조회" --json
```

Expected evidence shape: `provider=kpx`,
`endpoint_host=openapi.kpx.or.kr`, redacted URLs with
`serviceKey=REDACTED`, normalized `https://` endpoints,
`approval_required` skips for the current registry metadata, and
`resultCode/resultMsg` `provider_status` when a credential is approved for the
operation.

## Nineteenth Adapter: pqis

The pqis adapter covers the Animal and Plant Quarantine Agency plant
quarantine statistics API hosted at `openapi.pqis.go.kr`. The data.go.kr
registry stores the WADL metadata endpoint
`/openapi/service/plntQrantStats?_wadl&type=xml`, while the live WADL declares
the callable paths `nationCode`, `plantCode`, `importStats`, and
`exportStats`.

The adapter removes the metadata query, rewrites the request to the proper
operation path based on the Korean operation name, adds `serviceKey`, fills
safe smoke defaults such as `nationName=한국`, `plantName=사과`,
`fromYYYYMM=202501`, `toYYYYMM=202501`, `nationCode=CN`, `plantCode=1000`,
`pageNo=1`, and `numOfRows=1`, and classifies `resultCode/resultMsg` XML
statuses. Live probes currently return `pqis_service_key_not_registered` for
all four operations with the local data.go.kr key, which proves the endpoint
shape while preserving the provider-specific access failure.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider pqis --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider pqis --kind external_endpoint --limit 4 --output .datapan/pqis-verification.json --json
datapan catalog verify summary --input .datapan/pqis-verification.json --json
datapan get 3055528 nationName=한국 --operation "국가코드" --json
```

Expected evidence shape: `provider=pqis`,
`endpoint_host=openapi.pqis.go.kr`, redacted URLs with
`serviceKey=REDACTED`, rewritten paths such as `/nationCode` and
`/importStats`, `resultCode/resultMsg` `provider_status`, and
`pqis_service_key_not_registered` for unregistered credentials.

## Twentieth Adapter: myhome

The myhome adapter covers the LH MyHome public rental housing complex API
hosted at `data.myhome.go.kr:443`. The operation uses an uppercase
`ServiceKey` parameter and returns JSON status bodies even when the HTTP
`Content-Type` is `text/html`, so a generic caller can misclassify a structured
provider error as a plain HTML response.

The adapter preserves `ServiceKey`, fills `pageNo=1` and `numOfRows=1`, treats
regional filters such as `brtcCode` and `signguCode` as optional for smoke
calls, and classifies `{"code":"30","msg":"SERVICE KEY IS NOT REGISTERED
ERROR."}` as `myhome_service_key_not_registered`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider myhome --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider myhome --kind external_endpoint --limit 1 --output .datapan/myhome-verification.json --json
datapan catalog verify summary --input .datapan/myhome-verification.json --json
datapan get 15058476 --operation "임대주택목록 조회" --json
```

Expected evidence shape: `provider=myhome`,
`endpoint_host=data.myhome.go.kr:443`, redacted URLs with
`ServiceKey=REDACTED`, `code/msg` `provider_status`, and
`myhome_service_key_not_registered` for unregistered credentials.

## Twenty-First Adapter: emuseum

The emuseum adapter covers National Museum of Korea relic APIs hosted at
`www.emuseum.go.kr`. The registry exposes list, detail, and code operations
under `/openapi/relic/list`, `/openapi/relic/detail`, and `/openapi/code`.
The list operation accepts many optional search filters, so the adapter omits
empty filter parameters instead of sending a noisy query string.

The upstream blocks Go's default `Go-http-client/1.1` user agent with an HTML
WAF page, while a named `datapan-cli` user agent returns the expected XML
provider status. The adapter therefore sends a stable Datapan User-Agent,
adds `serviceKey`, fills `pageNo=1` and `numOfRows=1`, and classifies
`resultCode/resultMsg` statuses such as `4030 / SERVICE KEY IS NOT REGISTERED
ERROR.` as `emuseum_service_key_not_registered`.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider emuseum --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider emuseum --kind external_endpoint --limit 3 --output .datapan/emuseum-verification.json --json
datapan catalog verify summary --input .datapan/emuseum-verification.json --json
datapan get 3036708 --operation "소장품 목록 조회" --json
```

Expected evidence shape: `provider=emuseum`,
`endpoint_host=www.emuseum.go.kr`, redacted URLs with
`serviceKey=REDACTED`, `resultCode/resultMsg` `provider_status`, omitted empty
search filters, `application/xml` responses with a named User-Agent, and
`emuseum_service_key_not_registered` for unregistered credentials.

## Twenty-Second Adapter: jeju

The jeju adapter covers Jeju Special Self-Governing Province APIs hosted at
`data.jeju.go.kr`. The imported registry preserves the upstream metadata, but
some older Jeju records expose only a service root such as
`/rest/nightpharmacy` or include a stray space in the endpoint path. The adapter
normalizes the path for calls and rewrites the night pharmacy list operation to
the official action URL, `/rest/nightpharmacy/getNightPharmacyList`.

Jeju's night pharmacy list endpoint is currently useful without inventing
extra parameters: the adapter omits empty `dataTitle`, fills `pageSize=1` and
`startPage=1`, sends a named Datapan User-Agent, and classifies the XML
`resultCode/resultMsg` response as provider status. The older Gyorae recreation
forest endpoint still returns `HTTP 405`; Datapan records that as
`jeju_method_not_allowed` instead of hiding the upstream mismatch.

Observed evidence commands:

```bash
datapan catalog providers --registry .datapan/data-go-kr.registry.json --status adapter --provider jeju --json
datapan catalog verify --registry .datapan/data-go-kr.registry.json --provider jeju --kind external_endpoint --limit 4 --output .datapan/jeju-verification.json --json
datapan catalog verify summary --input .datapan/jeju-verification.json --json
datapan get 15043696 --operation "심야약국 리스트 조회" --json
```

Expected evidence shape: `provider=jeju`,
`endpoint_host=data.jeju.go.kr`, redacted URLs with `serviceKey=REDACTED` when
a key is configured, `resultCode/resultMsg` `provider_status`,
`xml_rfcopenapi_list` for the verified night pharmacy list call,
`jeju_method_not_allowed` for the stale recreation-forest endpoint, and
`jeju_missing_required_params` for detail/file operations that require
`dataSid`.

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
real verification and at least two providers have call behavior. Move to
`datapan-providers` only when the interface has been exercised by multiple
providers and the release boundary is worth maintaining separately.

The provider index now makes that decision explicit under `split_readiness`.
Consumers and maintainers should treat `split_readiness` as a release signal,
not a mandate to split immediately. The current adapter set has enough
registered verification-capable providers, and epost, emuseum, forest, geoje,
humetro, gblib, itfind, jeju, korad, kpx, lh-ebid, myhome, naqs, oneclick-law, pqis,
seoul-bus, sisul, tour, andong, uiryeong, and ulsan declare stable `call`
capability, so the boundary is ready
to consider.
Keep adapters inside `datapan-cli` until release cadence or maintenance cost
makes a separate `datapan-providers` repository clearly worth it.
