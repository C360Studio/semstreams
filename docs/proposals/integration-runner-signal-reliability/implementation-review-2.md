# Second implementation review

Mode: implementation/merge re-review. Reviewer: `semstreams-reviewer`, configured `gpt-6-astra`.
Reviewed on 2026-09-28. Source MD5: `95c3ecb58d7e099b014c3772420a8301`.
Unchanged production runner MD5: `ef0dde707bec1ab6a39d5ad7293896fe`.

The reviewer checked the corrected evidence and all shared-waiter callers. Previous EOF behavior and unsafe
`ProcessState` reads are corrected. Cleanup now attempts helper completion rather than only reporting initial
liveness. No tests, mutations or broad gates were run during this review.

## HIGH: Regression controls recreate a guessed scheduling threshold

`test/testinfra/integration_runner_contract_test.go:486` and 527 race observer completion against
`time.After(time.Second)`, despite an existing five-second observer context. Healthy scheduling delay can fail this
new one-second assertion.

Attempted refutation: holding the runner or writer proves the terminal event preceded release. It does not justify
another scheduling limit. The measured 0.03-second pass supplies no loaded-host bound.

Correction: use one declared containment boundary per control. Assert the terminal result occurred while the resource
remained held and without exhausting that boundary. Remove the independent one-second performance assertion.

Required evidence: preserve RED against the previous observer and GREEN against the correction. The tests must
continue to distinguish waiting until context expiry from acting on EOF or owner exit.

## HIGH: Cleanup treats a stale PID as authority to kill

`test/testinfra/integration_runner_contract_test.go:1352` and 1362 read a PID file, poll numeric PID existence,
create a fresh `os.Process` and call `Kill`. After the original helper exits and is reaped, its PID can be recycled.
This can target the replacement process: `kill -0` establishes existence, not fixture ownership.

Attempted refutation: the fixture originally wrote that PID, but its parent may already have exited and the child may
have been reaped. Opening a process handle at cleanup time does not identify the original helper.

Contrary local evidence: `TestIntegrationRunner_ImagePullOwnershipComesFromBashJobTable` at 83–92 rejects numeric PID
existence as ownership authority. The production runner documents the same recycled-PID hazard at
`scripts/run-integration-tests.sh:151–154`.

Correction: retain destructive cleanup under an established fixture owner or identity-bearing process authority.
When ownership cannot be established, report unresolved cleanup rather than signaling an unproven process. Keep the
solution specific to this fixture.

Required evidence: owned-helper cleanup still completes before return; stale or mismatched identity cannot signal an
unrelated controlled process. Use a controlled identity seam, not real host PID recycling.

## Verdict

**CHANGES REQUESTED:** resolve both findings before the full gate.

The updated examples and semantic mutation improve coverage. The explicit-`wait` survivor remains appropriately
limited. Eight seconds describes sequential waiting allowances, not safe process identity.

## Separate local gate result

Root started `GOCACHE=/private/tmp/semstreams-gh1397-gocache task check:push` against this frozen code while re-review
ran. Build passed, then lint failed before unit or integration execution. Revive reported function-length (84
statements, maximum 80) on the main termination test, and context-as-argument on `readRunnerSignal`.
The gate returned exit 201; log: `/private/tmp/semstreams-gh1397-check-push-1.log`. These are required corrections,
not waived warnings. No long integration run was spent on this candidate.
