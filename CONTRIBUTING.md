# Contributing to Datapan CLI

Thank you for improving the local, agent-friendly public-data workflow. Keep
each change reviewable, bounded, and backed by a reproducible verification
step.

## Contribution license

By intentionally submitting a contribution for inclusion in this repository,
you confirm that you have the right to submit it and license that contribution
under the [Apache License 2.0](LICENSE). This is the repository's
inbound-equals-outbound rule; no separate contributor license agreement is
required.

Do not submit provider data, provider documentation, credentials, browser
profiles, API responses, or material whose source terms do not permit the
proposed use. Read [source rights](docs/source-rights.md) before changing a
provider integration, example, fixture, or generated artifact.

## Before opening a pull request

- State the user outcome, scope, and verification evidence in the linked
  issue and pull request.
- Preserve local-only credential custody. Do not add key collection, key
  centralization, provider proxying, or secret-bearing fixtures.
- Do not use real portal submission as test evidence, and keep browser-backed
  workflows fail-closed until their provider contract is evidenced.
- Run the focused tests for the changed command and `go test ./...`.
- Run `sh scripts/check-governance.sh` when changing governance documents or
  release packaging.
- Do not hand-edit generated artifacts to make a validation check pass.

For sensitive security reports, use the private route in
[SECURITY.md](SECURITY.md), not a public issue.
