# First implementation review

Mode: implementation/merge review. Reviewer: `semstreams-reviewer`, configured `gpt-6-astra`.
Reviewed on 2026-09-28, claim HEAD `234200f3` plus working implementation.
Source MD5: `ac1fad4080fab595290da701d9e37b0f`.
Runner script MD5: `ef0dde707bec1ab6a39d5ad7293896fe` (restored).

The reviewer read the complete diff against `origin/main`, retained decision records and implementation evidence.
Retained inventory/design documents match the reviewed originals. `gopls references` succeeded for `killAndWait`;
its existing callers were included. No tests or mutations were run during review.

## HIGH: Known signal failure waits for the containment deadline

`test/testinfra/integration_runner_contract_test.go:1058`: after EOF, `observeRunnerSignal` waits for
`waiter.done` or `ctx.Done`. EOF already proves the expected signal cannot arrive. A runner that closes the descriptor
but remains alive delays the diagnostic until the fixture deadline. At 1034–1044, the initial readiness call passes
`ownedPID == 0`; an exited runner with a descendant retaining the signal writer similarly causes a full-deadline wait.

Attempted refutation: buffered final acknowledgements justify draining an already-written byte. They do not justify
waiting after EOF or treating unexpected early-phase runner exit as continuing progress.

Correction: return a phase-specific failure immediately on EOF. Distinguish the final acknowledgement's legitimate
exit race from phases where parent exit is terminal. Resolve the pending reader without inheriting the remaining
fixture window.

Required evidence: EOF with a still-live runner; early parent exit with a retained signal writer; buffered final
acknowledgement coincident with exit. The first two must report failure while the deliberately held process or
file descriptor remains held; then release and join cleanup.

## HIGH: Existing caller assumes bounded cleanup always joined

`test/testinfra/integration_runner_contract_test.go:617`: `killAndWait` can now return timeout before
`waiter.done` closes. This caller accepts any nonnil error, then reads `command.ProcessState` at 620 concurrently
with `Cmd.Wait`. Cleanup callers at 578, 611 and 729 discard the newly meaningful incomplete-cleanup error.

Attempted refutation: killing `/bin/sleep` normally finishes promptly, but the new timeout expressly admits unfinished
`Wait`; elapsed time supplies no synchronization.

Correction: require observed waiter completion before mutable command-state inspection and report incomplete cleanup
in every affected caller. Distinguish expected killed-process errors from unresolved ownership.

Required evidence: a controlled incomplete-join path through updated caller behavior, plus focused race checks.

## HIGH: Cleanup observes a surviving helper without waiting for exit

`test/testinfra/integration_runner_contract_test.go:307`: when the runner already exited, its wait returns immediately.
Cleanup performs one helper-liveness check and returns. Releasing the helper gate does not prove exit; no bounded
observation or escalation exists for that identified helper. Duplicate cleanup at 452–456 has the same behavior.

Attempted refutation: reporting the live PID avoids false success, but does not meet the accepted obligation to attempt
termination and establish completion before return. The mutation record's later external `kill -0` proves eventual
absence, not cleanup completion.

Correction: release, observe completion and apply appropriate owned-helper termination under an independent bound.
Report unresolved ownership only after that sequence expires. No generic process-tree supervisor is required.

Required evidence: parent exit with a controlled helper still alive; cleanup must establish helper disappearance before
return or expressly exhaust the independent cleanup bound. Record the result inside the test.

## Verdict

**CHANGES REQUESTED:** resolve all three findings before the full gate.

The 35-second whole-fixture bound has a documented basis and differs materially from four restarted scheduling
assertions. Named schedule examples remain an appropriate PBT choice; add the missing cases above. The surviving
explicit-`wait` mutation is honestly recorded and must not become proof of equivalent reaping behavior. The reported
package pass does not resolve these source-visible failure paths.
