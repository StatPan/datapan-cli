# Quickstart

After installing, run this offline-safe first-run flow from a new project directory. These commands were verified on Linux with the source-built CLI.

```bash
mkdir datapan-demo && cd datapan-demo
datapan init --json
datapan status --json
datapan ready --limit 10 --json
datapan try "단기예보" --json
datapan kit 15084084 --json
```

`init` installs and verifies the public registry. `status` reports registry, credential, and provider state. `ready` lists call-ready APIs. `try` selects a matching API and prints the next steps. `kit` writes a starter kit for a selected operation; it may require a reference and parameters for a specific API.

For a credential-backed call, set the preferred local variable before using the generated parameters. Never commit keys:

```bash
export DATAPAN_DATA_GO_KR_KEY='your-data-go-kr-service-key'
datapan auth check --json
datapan get 15084084 --dry-run --json
```

The dry-run path does not contact the provider. A live call requires a valid user-owned key and network access; it was not used as installation evidence here.

For discovery without a query:

```bash
datapan search --limit 10 --json
datapan search "아파트 실거래가" --json
```

For the full stable surface, see the [command reference](command-reference.md).
