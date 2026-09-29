# Owner listing expiry recurrence

## Why

Post-merge main CI run 36641021599 failed on `41d6236a84a8443b284785372126a092331096f6` after the reviewed #1432
harness repair. The new diagnostics place the failure in measured predicate-forward listing repetition 4 (the fifth attempt), before the
concurrent phase. The caller context was live, the configured framework KV default was five seconds, and the call
returned a typed deadline error after 10.001209581 seconds. The 39-entry graph-query cleanup batch remains preserved
in #1433 / #1434 while this observed required-job failure takes priority under #1417.

## Evidence and scope

Use the accepted inventory and native experiment in the completed owner-load-reliability archive as historical
starting evidence. Refresh only changed facts and the paths implicated by this recurrence. Do not infer that SDK
backpressure, a runner stall, the old fifteen-second drain error or the harness repair caused this event.
The captured stack is after listing return and before harness teardown; it is not an in-operation trace.

The next gate is a bounded source/evidence inventory refresh, independent review and a causal experiment design.
Production changes require measured cause and independent design/implementation review. Preserve the real framework
five-second deadline, workloads, latency assertions and error propagation. No blind repetition, retry-to-green,
timeout increase or weakened success criterion is authorized.

## Status

Initial follow-up claim for existing #1421. No cause or repair is established. The previous one-PR waiver applied
only to merged #1432; it supplies no merge authorization here. Graph-query's draft remains open without code changes.
