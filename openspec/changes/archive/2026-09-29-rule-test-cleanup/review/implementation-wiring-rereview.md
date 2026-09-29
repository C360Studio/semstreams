# Wiring correction re-review — #1428 / #1429

Mode: implementation review, bounded correction addendum. Read-only; no tests, Docker, repository edits or commits.

Reviewed HEAD `0d888375e332ac915ba55af50668cd6c0cad699b` with uncommitted correction in `processor/rule/rule_integration_test.go`, SHA256 `bd1555af8fa008af9c08da1ceb86502f4e30f0079d955c4338b5011770efab4e`. Hash remained identical before and after inspection. Accepted design SHA256 `c2206a3471e0fc5c3f21f966422856b4ee1adc7fa9b02b0352f361ac9cb82e78` remains the review basis. This addendum supersedes only the two HIGH findings in `/private/tmp/gh1428-wiring-review.md`.

## Finding dispositions

Both wiring findings are closed in the reviewed bytes.

1. Actual accepted Start authority is now observed. B18/B16/B20 declare local terminal authority initially equal to the bounded setup/I/O context, then assign the narrower Start parent after Initialize and before Start (156–159, 588–591, 910–913). Their early deferred closures read that variable at invocation (139, 573, 895). Consequently expiry of the narrower context reaches the existing owner's Err check even if the wider I/O authority remains live. The closure captures a lexical variable, not a retained production context or hidden getter. Short declarations of `testCtx, cancelStartScope` occur in the same function block as the existing cancel variable, so they assign it rather than shadowing it.

2. Existing execution-clock boundaries are restored. All six B15–B20 cases now create the original 10/15-second phase context after successful Initialize (156, 273, 484, 588, 745, 910), rather than before NATS startup. Previously unbounded setup remains bounded by the separate 30-second ancestor; that ancestor still limits total operation authority. This is not a budget increase or a replacement authority after expiry. B15/B19/B17 reassign the function-local ctx with ordinary assignment, so their deferred closures observe the actual execution context. B16/B18/B20 retain separate outer KV/I/O scope.

## Ordering and failure-path check

The cleanup closure is installed immediately after nonnil Processor acquisition and before constructor-error/Initialize assertions. Before the execution phase exists it finalizes with setup authority and has no phase cancel to invoke. After phase creation it finalizes with the accepted phase parent, then calls the phase cancel. The earlier outer cancel defer executes after this closure. Thus native Stop and the owner's private Start cancellation complete before phase cancellation, and lexical owner cleanup still precedes subscription/substrate testing callbacks. No fallback retry or new explicit Stop is introduced.

These six corrected cases do not contain a restart/explicit-Stop phase. The separate cron restart cases already reviewed in the preliminary slice retain their distinct operation authority and owner attempt fence; this one-file correction does not alter them. Owner/helper/proof files were not read during the developer's exclusive mutation window.

The reported seven-case external integration pass (7.94 seconds) was not independently inspected in this bounded re-review and is not promoted to final evidence. Full native proof, mutation/restoration evidence, baseline/classification reconciliation and immutable manifest review remain pending as planned. The #1421 merge hold is unchanged.

APPROVE the two wiring corrections only. Full implementation approval remains withheld pending the final immutable snapshot and complete evidence packet.
