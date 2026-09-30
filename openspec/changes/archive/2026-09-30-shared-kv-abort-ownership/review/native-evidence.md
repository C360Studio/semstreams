# Single native experiment evidence review

Reviewer: independent semstreams-reviewer gh1421_inventory_review.
Verdict: evidence accepted; green test exit is not evidence of successful native cleanup.
Log SHA-256: 5a8c4b3892f742995cc1eca2ca992107688039078475e9fc16884981d7096084.
Ordinary/tagged source identities remain0245261b/98b61595 as recorded in source-final.md.

Control returned the exact5,000-key set. Before natural expiry, distinct goroutines40 and51 were blocked at native
forwarding1451 and watcher1290. Production returned nil keys and typed deadline error; delegated Stop ran once.
Watcher51 remained blocked at jetstream/kv.go1290 after production return and after Close returned nil/CLOSED.
Close took about13ms. Forwarder40 was absent from later snapshots; absence is not a join.

This establishes a concrete shared abort-ownership defect at the observed terminal boundary. It does not establish
indefinite survival, reproduce the15s drain failure or attribute either hosted incident.

## High: terminal finding does not fail the diagnostic

At tagged source188, abortObservePair logs surviving-owner classification and returns nil; the gated case consequently
reports completion. Reviewer explicitly acknowledges this was missed in source approval. The valid classifier mutation
proves string classification sensitivity only, not end-to-end finding propagation. Preserve the exact run; no fabricated
RED or rerun. Any permanent regression must assert its terminal obligation through the actual abort path.

The next repair question is how shared listing cancellation terminates native delivery work when collection stops,
with an observable terminal obligation. That requires a separate bounded inventory/design review before production edits.
