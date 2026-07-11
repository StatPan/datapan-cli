# Datapan CLI Goal Completion Audit

Audit date: 2026-07-11

This audit evaluates the product goal against evidence in the current
worktree. A passing unit test or a configured workflow is evidence only for
the behavior it directly exercises. A workflow that has not run on GitHub is
not treated as retained release evidence.

## External evidence snapshot

Snapshot checked: 2026-07-11 Asia/Seoul.

- Local `HEAD` and `origin/main` both point to
  `f8b42e1cc2432e01062d565bb3249ac2038c52d0`, but the goal implementation is
  currently held in 18 modified or untracked worktree entries. The successful
  [CI run 28792825702](https://github.com/StatPan/datapan-cli/actions/runs/28792825702)
  proves the unchanged HEAD, not this worktree.
- GitHub currently registers only `CI`, `Release CLI`, and `Smoke Release` for
  the repository. The new `Registry Journey` workflow is untracked locally and
  therefore has no remote run or retained artifact.
- The latest public Registry remains
  [`v2026.06.25.24`](https://github.com/StatPan/datapan-registry/releases/tag/v2026.06.25.24),
  published 2026-06-25. Registry main has advanced to
  [`6b314e8f1b0f8feaf97f6fee355cf65ce35b33a6`](https://github.com/StatPan/datapan-registry/commit/6b314e8f1b0f8feaf97f6fee355cf65ce35b33a6),
  so main-only consumer contracts still lack public-release evidence.

| Requirement | Current evidence | State |
| --- | --- | --- |
| Safe Registry install and update | Manifest-bound Registry, selected evidence validation, install provenance, atomic three-target replacement, durable interruption journal, rollback and restart recovery tests | Implemented and locally verified |
| Source, integrity, freshness, compatibility, and verification visibility | Registry digest, manifest digest, sustainable coverage policy, per-operation verification age, consumer compatibility, release consumer decision, and manual-review boundaries in JSON plus stderr trust context for human dry-run, get, save, and sync without contaminating data stdout | Implemented and locally verified |
| Search to executable plan | Search, ready, show, try, params, use, dry-run, and kit tests preserve Registry trust, per-operation evidence, upstream approval and auth facts, parameters, source URL, endpoint host, Provider route, and explicit call-ready boundaries in JSON and human output; params files receive transactional SHA-bound provenance without copying values | Implemented and locally verified |
| Call, save, sync, export, and code generation | Fake-provider integration tests cover successful and failed calls, save and call-based row export summaries that preserve exact Registry trust, verification, stale warnings, and human failure actions, whole-generation sync replacement with injected commit rollback, structured local-write failure, operation evidence and SHA-bound cache inventories, fail-closed preview/export cache verification, and a common curl/Postman/OpenAPI/Go/Node/Python evidence contract with human stderr context, starter-kit provenance, and SHA-bound standalone generation sidecars | Implemented and locally verified |
| Latest public Registry local journey | Current source installed public Registry `v2026.06.25.24`, verified 12,060 specs and trusted integrity, selected and rediscovered a ready operation, completed show and try, generated a SHA-bound params file, consumed it in a credential-redacted dry-run, generated a SHA-bound standalone OpenAPI export, and generated nine starter-kit artifacts without leaking the sentinel credential | Implemented and locally verified on Linux; evidence is not yet a retained CI artifact |
| Live verification trust and redaction | Live top-level and catalog verification stop before adapter, probe, or provider HTTP when Registry execution is blocked; offline report inspection remains available; transport evidence redacts credentials | Implemented and locally verified |
| Approval and browser trust boundary | Registry-derived open, inspection, and apply paths stop before browser execution when trust is blocked; purpose diagnostics and fixed provider login remain available; browser JSON preserves Registry trust | Implemented and locally verified |
| Credential and sensitive-output safety | Credential redaction tests cover URLs, transport errors, generated commands, cache metadata, blocked execution before HTTP, and raw or URL-encoded credentials reflected by providers into human output, JSON, provider status, CSV, or sync files | Implemented and locally verified |
| Failure taxonomy and next actions | Authentication, approval, input, adapter, compatibility, stale verification, and external-provider categories; manifest-bound Registry error routing and runtime remediation actions; matching human stderr classification and complete next actions without contaminating provider stdout | Implemented and locally verified |
| Backward-compatible Registry consumption | Legacy releases without freshness or consumer-decision artifacts remain explicitly unevaluated or missing rather than rejected; canonical monolith fallback and optional shards are tested | Implemented and locally verified |
| CLI-owned schema conformance | Actual Registry install provenance plus params, Postman, OpenAPI, Go, Node, Python, and starter-kit provenance payloads validate against published repository schemas; invalid artifact kind and install pin mode negative cases are rejected | Implemented and locally verified |
| Linux, macOS, and Windows source verification | CI and tagged-release verification matrices run tests, vet, and builds on all three operating systems | Workflow configured; no run from this worktree retained yet |
| Pre-publication archive journey | Tagged release archives require both datapan and dp binaries to exist and equal the tag, then run the latest Registry journey on Linux amd64, macOS Intel, and Windows amd64 before publication; evidence includes the journey summary, params and standalone export artifacts with provenance, and kit provenance | Workflow configured; no run from this worktree retained yet |
| Exact-tag public installer journey | After publication, the tag workflow installs exactly `GITHUB_REF_NAME` on Linux, macOS, and Windows, requires both datapan and dp version fields to equal that tag, runs the Registry journey, and retains params, export, and kit evidence | Workflow configured; no run from this worktree retained yet |
| Latest public release drift journey | Scheduled and successful-release-triggered workflow installs the current public checksum-verified latest release on Linux, macOS, and Windows, verifies the dp alias, and retains the journey summary, params and standalone export artifacts with provenance, and kit provenance | Workflow configured; no run from this worktree retained yet |
| Latest Registry development contracts in a public release | CLI consumes sustainable coverage, release consumer decision, error action, and remediation contracts from Registry main | Implementation verified with fixtures; current public Registry release predates these main-branch contracts |
| Credentialed live-provider receipts | Default CI intentionally does not hold user or provider credentials; Registry currently records a manual-review boundary for reviewed credential receipts | External evidence remains unavailable and must not be inferred |

## Current completion decision

The product goal is not yet proven complete. Local implementation and test
coverage are strong, and the current source now completes the latest public
Registry journey on Linux. That run exposed and fixed default-Registry loading
for `kit`, `list`, `ls`, and `info`. However, the new CI, archive, and
public-release journeys have not produced authoritative GitHub workflow
artifacts, and the newest Registry contracts are not yet present in a public
Registry release. Completion requires successful retained runs after the
changes are published, plus a new Registry release or an explicitly accepted
compatibility boundary for the unreleased contracts.

## Evidence commands

```text
go test ./...
go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/datapan
GOOS=darwin GOARCH=arm64 go build ./cmd/datapan
GOOS=linux GOARCH=arm64 go build ./cmd/datapan
sh -n scripts/install.sh
pwsh scripts/smoke-registry-journey.ps1 -Datapan ./datapan -KeepWorkDir
```
