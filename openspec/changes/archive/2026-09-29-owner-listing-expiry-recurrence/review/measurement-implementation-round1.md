# Measurement implementation review: round 1

Independent reviewer: **CHANGES REQUESTED** before native execution.

Temporary source: `natsclient/kv_lifecycle_diagnostic_integration_test.go`, 987-line baseline, SHA-256
`a51fb72d6803c0b6b84e7706c768f5e2f1ce0b158d0182ba3449a831fe3e92fb`.

## HIGH — lines 393 and 628: context-completion callback join is unbounded

The new `joinCompletion` waits unconditionally for the `context.AfterFunc` callback. The callback takes the reporter
mutex and writes stdout before closing its completion signal. The finalizer invokes the join outside a bounded select,
so this new test-owned task can exceed the shared terminal allowance.

Pass the existing terminal context into the join, report unresolved completion on its deadline, and preserve subsequent
connection and child containment. Native execution remains held until corrected and checked.

## HIGH — line 864: early failure discards SDK traces

The trace snapshot is emitted only after the successful assertions, cleanup and stack checks. Acquisition/control errors,
unmet gates, unexpected operation errors and cleanup failures return first, discarding the new evidence and bypassing
its overflow check.

Install a checked lexical finalizer immediately after trace allocation. Run it after owned cleanup where possible, retain
records on failure, and combine its error with the original via `errors.Join`. Explicitly mark observation incomplete
when activity remains unresolved; a snapshot does not itself prove native completion.

## Verdict

No additional blocking finding in the measurement diff. Final approval requires the corrected frozen source, focused
execution evidence and bounded native observations. Root held the native launch on receiving these findings.
