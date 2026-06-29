# Command Reference

Commands that support `--json` write one JSON object to stdout. Prefer JSON for
scripts, agents, and release workflows.

## First Run

```bash
datapan init --json
datapan status --json
datapan ready --limit 10 --json
datapan try <ref> [KEY=VALUE ...] --json
datapan kit <ref> [KEY=VALUE ...] --json
```

## Search And Inspect

```bash
datapan search [query] [--org NAME] [--category NAME] [--priority P0] [--provider NAME] [--callable] [--call-ready] --json
datapan list [query] --limit 10 --json
datapan show <ref> --json
datapan params <ref> [--operation NAME] --json
```

## Call And Export

```bash
datapan get <ref> [KEY=VALUE ...] [--operation NAME] [--timeout 30s] --json
datapan call <ref> [KEY=VALUE ...] [--dry-run] --json
datapan save <ref> [KEY=VALUE ...] [--format csv|json] [--output PATH|-] --json
datapan export --format curl|openapi|postman|python|go|node <ref> [KEY=VALUE ...]
```

## Registry

```bash
datapan catalog install datapan-registry --json
datapan catalog overview --json
datapan catalog coverage --json
datapan catalog providers --limit 20 --json
datapan catalog dependencies --limit 20 --json
datapan catalog adapter-targets --limit 20 --json
```

## Verification Evidence

```bash
datapan verify --limit 10 --timeout 10s --workers 1 --json
datapan catalog verify --registry PATH --limit 100 --timeout 10s --workers 1 --output verification.json --json
datapan catalog verify --input verification.json --status failed --json
datapan catalog verify plan --registry PATH --verification verification.json --json
datapan catalog verify summary --input verification.json --json
datapan catalog verify merge --input A.json --input B.json --output merged.json --json
```

`--limit` selects the maximum number of candidates, `--workers` bounds
concurrent upstream requests, and `--timeout` bounds each request.

## Release Maintenance

```bash
datapan catalog diff --old OLD --new NEW --json
datapan catalog audit --registry PATH --json
datapan catalog errors --registry PATH --json
datapan catalog route-disposition --registry PATH --probe REPORT --json
datapan catalog release draft --registry PATH --previous-registry PATH --json
datapan catalog release verify --manifest PATH --json
datapan catalog release readiness --manifest PATH --json
```

## Local Files

```bash
datapan preview --input response.json --limit 10 --json
datapan head --input response.csv --limit 10
```
