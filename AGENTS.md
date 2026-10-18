# Agent Instructions

## Purpose and source of truth (Datapan Core)

**Core objective** (statpan_docs `projects/datapan/STRATEGY_CONTROL.md`):
공공데이터 전반을 파악하고, 필요한 데이터를 지속적으로 확보·갱신·검증하여 여러 사업에서 재사용할 수 있게 유지·공급한다.
This repo is one part of the Datapan Core product (Registry, Data, CLI, Health, Web, Infra). The product is a maintainable public-data foundation, not a single business model, procurement product, or sample cohort.

**Read order for agents** (private repo `StatPan/statpan_docs`, read `origin/main`, not a stale local checkout; reviewed at `578aebc`, 2026-10-17):
1. `projects/datapan/README.md` — source precedence.
2. `projects/datapan/REPO_CONTRACTS.md` — this repo's goal and standing feature IDs (see "Repository development goals" and its own section).
3. `projects/datapan/PM_DECISION_POLICY.md` — how requirements become Goals/issues; acceptance and escalation.
4. `projects/datapan/core/OPERATING_MODEL.md` — production vs delivery, reuse/isolation, cost, CTR/NFR/FLEX requirements.
Precedence: accepted strategy > REPO_CONTRACTS > OPERATING_MODEL > business/ research (hypotheses, not decisions) > historical docs. Implementation truth is default-branch code/tests; operational claims need dated runtime evidence; execution state lives in GitHub Goals/issues/PRs.

**Principles that bind every change**
- Production, publication, delivery, consumer receipt and customer acceptance are separate facts. Never report one as another.
- Failed/partial/unknown/not-measured are distinct states; unmeasured cost or coverage is not zero or complete. A failed refresh must not overwrite the last good version.
- Identity, provenance, observation time, and rights stay explicit. Name similarity or model inference alone is not a fact.
- Open/DIY local use stays genuinely useful; do not weaken it to push paid use. Paid value = recurring responsibility (collect, validate, refresh, source-change response, history/recovery, delivery).
- Customer-private/specific work is isolated from shared Core; reuse needs named evidence, rights, and bounded maintenance cost.
- Do not adopt a business model, price, offer, or market from an engineering ticket. Business research is not a decision; a Core release does not prove demand.
- Opening an issue/PR does not mean it is approved or prioritized. Deployment, publication, commercial activation, secrets, and ownership changes need their own explicit approval.
- Record gaps honestly: when code, docs, or runtime disagree, open/refresh an issue rather than silently changing one side.

### CLI purpose and completion boundary

The authoritative CLI contract is `statpan_docs` `projects/datapan/REPO_CONTRACTS.md`, read directly with `git -C /home/statpan/workspace/statpan_docs show origin/main:projects/datapan/REPO_CONTRACTS.md`; that checkout is read-only because its local state is stale and dirty. The CLI is the local execution and consumer-compatibility surface: CLI-01 installs and validates pinned Registry content, CLI-02 provides local discovery and access guidance, CLI-03 executes supported provider calls with user-owned credentials, CLI-04 inspects/caches/exports local results and generates integrations, and CLI-05 provides doctor, verification, and deterministic machine-readable contracts. The repository is complete when supported environments establish install → search → access guidance → call/file path → verification/export, while incompatible contracts, permissions, provider failures, and legitimate empty results remain explicit rather than appearing successful.

These local capabilities must complete without a mandatory Datapan-hosted service: hosted access is optional for the promised local workflows, credentials remain user-controlled, and local artifacts are not automatically published datasets. The CLI consumes Registry contracts but does not own canonical Registry publication, Data's dataset production, or hosted credential custody.

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
Milestone and execution status are maintained in GitHub issues and PRs; verify with `gh issue list --repo StatPan/datapan-cli --state open` rather than treating this document as a live tracker. At the time of this update, #196 tracks the v1 bounded local BYOK call/inspect/provenance-preserving export outcome, while blocked #173 and #165 track the Beta three-OS Registry consumer proof and rollback receipts.
