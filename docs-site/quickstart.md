# Quickstart

Start with `datapan`. Use `dp` only if you explicitly installed the optional
alias.

## Initialize

Install the latest registry and write first-run evidence locally:

```bash
datapan init --json
```

Check the local setup:

```bash
datapan status --json
```

## Find A Callable API

List APIs that have enough defaults or smoke metadata for a conservative call:

```bash
datapan ready --limit 10 --json
```

Inspect a specific dataset and generate copy-paste commands:

```bash
datapan try 15084084 base_date=20260622 --json
```

## Create A Starter Kit

Generate params, OpenAPI, Postman, curl, and client examples for a dataset:

```bash
datapan kit 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json
```

## Verify Runtime Evidence

Collect a bounded verification batch:

```bash
datapan verify --limit 10 --timeout 10s --workers 1 --json
```

Use filters before increasing limits:

```bash
datapan verify --provider q-net --kind external_endpoint --limit 5 --timeout 10s --json
```

## Save Data

Once parameters are known, call or save the API:

```bash
datapan get 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json
```

```bash
datapan save 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --format csv --output forecast.csv --json
```
