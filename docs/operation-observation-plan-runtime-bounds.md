# Operation observation plan runtime bounds

The operation-plan child reads a small, selected projection from the installed
Registry release. It does not open or re-hash the canonical raw Registry
snapshot on each probe. Installer provenance binds the snapshot digest, the
release manifest binds the plan index and selected artifacts, and the index
binds the selected source scope and shard.

The current per-invocation local-file byte ceilings are:

| File | Maximum bytes |
| --- | ---: |
| Installed release manifest | 4 MiB |
| Install provenance | 64 KiB |
| Pinned plan, policy, assertion, and evidence schemas (5 files) | 5 MiB |
| Plan index | 8 MiB |
| Selected plan shard | 16 MiB |
| Selected operation-document evidence (up to 4 files) | 4 MiB |
| Selected reviewed policy artifact | 8 MiB |
| Selected response assertion artifact | 1 MiB |
| Private credential-binding file, when credentials are required | 256 KiB |

The selected artifacts are read with a `maximum+1` limiter, so the maximum
aggregate file input is 46 MiB plus 320 KiB plus 15 bytes, including the
one-byte overflow checks. Index parsing has a separate 500,000-token limit and
32,000 artifact-reference limit; the policy artifact has its own 500,000-token
and 32,000-reference limits. At most 8 policy rows are decoded, and only the
selected assertion and document sidecars are opened. These are consumer
resource ceilings, not claims about Registry population size.

On Linux, the child hashes the opened `/proc/self/exe` descriptor to attest the
running image, with a 128 MiB maximum and one-byte overflow check. Combining
that maximum with selected release files gives a bounded local read ceiling of
174 MiB plus 320 KiB plus 16 bytes. Actual binary size is normally much lower;
the fixed maximum prevents an unbounded hash operation.

The network request budget remains one. The request body and response body are
each limited to 1 MiB; a response over its declared or consumer byte limit is
indeterminate. The complete request deadline remains bounded by the selected
plan, up to 30 seconds. A 2xx response under observation-only semantics is
indeterminate until a reviewed response contract exists; a non-2xx status is
reported as an HTTP failure. No response rows, body, full URL, or credential
material enter the receipt.
