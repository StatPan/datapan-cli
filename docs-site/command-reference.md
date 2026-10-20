# Command reference

Run `datapan --help` for the complete current surface. Every command below supports the documented `--json` form where shown; JSON is intended for scripts and agents, while non-JSON output is human-oriented.

## Install and inspect

```bash
datapan init --json
datapan status --json
datapan doctor --json
datapan version --json
```

## Discover registry APIs

```bash
datapan search "아파트" --json
datapan ready --limit 10 --json
datapan show 15084084 --json
datapan coverage --json
```

## Registry and evidence

```bash
datapan catalog install datapan-registry --json
datapan catalog overview --json
datapan catalog audit --registry .datapan/data-go-kr.registry.json --json
datapan catalog diff --old OLD.json --new NEW.json --json
datapan catalog release verify --manifest MANIFEST.json --json
datapan catalog release readiness --manifest MANIFEST.json --json
```

These commands inspect or produce local registry and release evidence. They do not publish a registry release.

## Call planning, calls, and exports

```bash
datapan try "단기예보" --json
datapan params 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.params.json
datapan get 15084084 --params-file forecast.params.json --dry-run --json
datapan curl 15084084 base_date=20260622 base_time=0500 nx=60 ny=127
datapan save 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --format csv --output forecast.csv
datapan export --format openapi 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --output forecast.openapi.json
datapan codegen go 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --package forecastclient --output forecast_client.go
```

`get`, `curl`, `save`, and `sync` can make provider calls. `--dry-run` is the safe planning mode where supported. `export` and `codegen` operate on local inputs and preserve provenance in generated artifacts when the command supports it.

## Access and credentials

```bash
datapan auth check --json
datapan access 15126469 --purpose --json
datapan kit 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json
```

Browser-backed access commands are local workflows and may require a user profile. Credentials remain user-controlled.
