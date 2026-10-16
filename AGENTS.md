# Agent Instructions

## Purpose
This repository delivers a local CLI for install → search → access guidance → call → export.

## Commands
- `go test ./...` — run the full Go test suite.
- `go vet ./...` — run static checks.
- `go build ./cmd/datapan ./cmd/dp` — build both commands.
- `go run ./cmd/datapan search "아파트" --json` — exercise local search.
- CI runs tests, vet, governance checks, installer checks, and both builds on Linux, macOS, and Windows.

## Delegation and safety
- One issue and one branch per change; state scope and acceptance criteria before delegation.
- Never perform live calls without permission; use offline mocks for provider work.
- Never rotate secrets, expose credentials, publish releases, create release tags, or make destructive infrastructure changes.
- CLI release publication is an operator-approved step.
- Protect `.datapan/` user data, including browser profiles; do not inspect, edit, or delete it.
- Follow the workspace rules in `/home/statpan/workspace/AGENTS.md`.

## Evidence
Record every material decision as `[command / exit code / actual output / interpretation / unknown]`.
Do not abbreviate hashes. Include reproducible command output in the PR.

## Milestones
- C1 / MVP: independent local install, search, explanation, and access guidance; blocked gate: pinned Registry contract.
- C2 / Beta: immutable three-OS consumption and rollback receipts; blocked gate: Registry R2 publication for #173/#165.
- C3 / v1: bounded BYOK call, inspect, and provenance-preserving export; Health transmission is optional.
