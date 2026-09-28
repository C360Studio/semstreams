# Known-debt source review

This is a proposed source-review checkpoint, not a passing baseline, a complete census, or approval of unresolved
ownership. It isolates the stable known-debt population while callback analysis continues. Every exact candidate
identity, evidence fingerprint, receiver, ownership, location and build selection is in `known-debt-candidates.json`.

The candidate source files are unchanged from the claim HEAD; their SHA-256 hashes are retained in the packet.
A changed classification, identity, source or evidence fingerprint must re-enter review before machine consumption.
No `test/testinfra/cleanup_baseline.json` has been written.

Packet SHA-256:
`4244e855858c88cff0a0d91ae06d52eba9b9d2d1724d7cf9b6771c8e01c60f66`

Review each exact candidate against its source and type/ownership evidence. Grouping below helps navigation and is
not a package-wide approval. A shared rationale may describe repeated syntax only after all exact entries are checked.
The expected disposition is existing unbounded lifecycle cleanup debt, retained for separate package repair under
#1064. Reject wrong receiver/ownership, insufficient provenance, duplicate variants, or any unsupported conclusion.

| Package | Proposed exact entries |
|---|---:|
| `examples/processors/document` | 1 |
| `examples/processors/iot_sensor` | 1 |
| `gateway/graph-gateway` | 34 |
| `gateway/lifecycle-gateway` | 1 |
| `input/udp` | 7 |
| `output/file` | 5 |
| `output/httppost` | 5 |
| `output/websocket` | 22 |
| `pkg/dispatch` | 4 |
| `pkg/lifecycle` | 1 |
| `processor/agentic-dispatch` | 9 |
| `processor/agentic-loop` | 18 |
| `processor/agentic-model` | 14 |
| `processor/agentic-tools` | 17 |
| `processor/agentic-tools/executors` | 1 |
| `processor/gated-dag` | 31 |
| `processor/graph-clustering` | 16 |
| `processor/graph-embedding` | 13 |
| `processor/graph-index` | 28 |
| `processor/graph-index-spatial` | 5 |
| `processor/graph-index-temporal` | 5 |
| `processor/graph-ingest` | 76 |
| `processor/graph-query` | 39 |
| `processor/json_filter` | 4 |
| `processor/json_map` | 4 |
| `processor/rule` | 64 |
| `service` | 17 |
| `storage/objectstore` | 11 |

Independent source review is pending. The classifier, census completeness and guarded unknowns remain unapproved.

## Initial review finding

The first source-review pass found that the 453 proposed entries map to 325 physical path/line locations.
Twenty-four locations repeat identical ownership/provenance evidence under caller-labelled enclosing functions.
Those identities are rejected pending remapping to the actual lexical declaration and deduplication of identical
variants. Genuinely differing cleanup/context variants must remain distinct. This packet is retained as proposed
review input; its counts and identities are not a passing baseline.

## Corrected exact-set approval

Independent review accepted all 325 physical sites across 104 unchanged source files. The retained
`known-debt-source-review.json` records the source evidence and rejects the 128 duplicate caller-labelled records.
The corrected `known-debt-current-candidates.json` remains frozen as review input, with SHA-256
`3df0dea4ce389232d6b09803cbe3e8823ab32cfb170bf75ddb3747b0fd0bb5e8`.

A separate reconciliation review approved that exact corrected set for machine baseline entries: all fingerprints
and source hashes stayed unchanged, 324 identities stayed unchanged, and the single ordinal change at
`processor/gated-dag/executor_integration_test.go:222` removes the duplicate helper attribution.
`known-debt-reconciliation.json` retains those checks. The resulting baseline contains only those 325 classified
unbounded debt entries, each with a reason and #1064 owner reference.

This supersedes the initial packet's proposed identities and pending-review status. It does not approve unknown
classifications, census completeness, classifier correctness, or the whole PR. Guarded unknowns still fail admission.
