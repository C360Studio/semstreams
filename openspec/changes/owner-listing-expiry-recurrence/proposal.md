# Owner listing failure-phase evidence

## Why

Post-merge main CI run 36641021599 failed on `41d6236a84a8443b284785372126a092331096f6` after the reviewed #1432
harness repair. The new diagnostics place the failure in measured predicate-forward listing repetition 4 (the fifth attempt), before the
concurrent phase. The caller context was live, the configured framework KV default was five seconds, and the call
returned a typed deadline error after 10.001209581 seconds. The 39-entry graph-query cleanup batch remains preserved
in #1433 / #1434 while this observed required-job failure takes priority under #1417.

## Evidence and scope

This change delivers persistent, bounded failure diagnostics for the actual repeated predicate-forward owner-load
workload. Historical cause attribution and production repair remain open on #1421; completing this diagnostic change
does not resolve that issue. See `scope-reconciliation.md` for the current behavioral scope.

Use the accepted inventory and native experiment in the completed owner-load-reliability archive as historical
starting evidence. Refresh only changed facts and the paths implicated by this recurrence. Do not infer that SDK
backpressure, a runner stall, the old fifteen-second drain error or the harness repair caused this event.
The captured stack is after listing return and before harness teardown; it is not an in-operation trace.

The bounded inventory refresh passed independent review at SHA-256
`6f3479feb76d24d9768fe05abd815df2187b9baf6c89ea01e0fa15d1d2ecfc5a`.
The reduced two-case measurement design passed independent review at SHA-256
`5e1e18c10f1d0aacda7d6b95e1042e44cc0c73d97862ce772997e2b891afd1f4`; root accepted the bounded diagnostic slice.
The focused checks, selected attribution mutation and single native measurement pass are complete and independently
reviewed (review/measurement-evidence-final.md). The pass did not reproduce CI's additional interval.
The actual-workload observation design passed independent review at SHA-256
`77a7dbc61d54a536ab5056f0ee9f0a01a03957746dde37c23f47a4db09cde81e`. The next slice adds private phase/Stop
evidence to the five initial CI predicate-forward calls, preserving their native channel, workload and failure
semantics. It includes focused proof and one real-workload pass. Additional synthetic fault machinery and production
repair remain unsupported by the current observations.
Production changes require measured cause and independent design/implementation review. Preserve the real framework
five-second deadline, workloads, latency assertions and error propagation. No blind repetition, retry-to-green,
timeout increase or weakened success criterion is authorized.

## Status

Follow-up diagnostic change for existing #1421. No cause or production repair is established. The previous one-PR waiver applied
only to merged #1432; it supplies no merge authorization here. Graph-query's draft remains open without code changes.
