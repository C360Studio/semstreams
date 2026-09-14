# Review: first operational loop-state implementation slice

## Verdict and scope

Independent semstreams-reviewer verdict: **APPROVE — first-R6 slice, no remaining findings.**

This implements the first slice of the owner-accepted operational contract, not all of R6 or R2. It covers the five
states, private reuse of lifecycle.Transitions, local coherence and public methods, manager cancellation cleanup,
candidate validation at UpdateLoop, and required caller/fixture adaptations. Public method signatures are unchanged.
No terminal persistence, selected-outcome arbitration, ACK ordering, revision ledger or new runtime is introduced.

The review used the exact pre-R6 WIP-relative patch, rather than attributing earlier R2 work to this slice.
Final patch SHA-256: `7e4473b98f7e4bce7369d22d61421dfd4b9a5692d0e9b50c850d03b84c431dcb`.
Final 39-file manifest SHA-256: `697b17f1c0d26c646498c955acc77408c94d5b2aa5073bde59aa684fb802ab2a`.
The reviewer independently verified all 39 fingerprints and the complete slice.

## Review correction

One non-blocking MEDIUM finding affected the watcher-replacement test: both old and refreshed snapshots now use
running, so its state assertion no longer distinguished them. The correction retains that assertion and requires
Iterations == 5; the stale snapshot has 1. The reviewer confirmed this sole post-review assertion change at
`processor/agentic-dispatch/http_activity_test.go:652`. No runtime source changed after initial review.

The reviewer also verified that malformed terminal-plus-gate fixtures require quarantine and unchanged authority,
while separate valid unresolved-approval controls retain retry and no mutation. Startup adds no retained-stream
lookup, and delivery-owner correlation checks remain intact. The reviewer ran no tests or source mutations.

## TDD and regression evidence

All commands ran on the dirty claim worktree based at `5e0e2259aa7392f7f3255d7f01533869862d8174` on 2026-09-13.
The first two REDs preceded the source correction; their failure was behavioral, not missing-symbol compilation.

| Command | Result | Log |
|---|---|---|
| `go test -race ./agentic -run '^TestOperational' -count=1 -v` | RED: retired/contradictory states, duplicate Begin, old birth/Resolve values and retired field | state-contract-red.log |
| `go test -race ./processor/agentic-loop -run '^TestOperationalManager' -count=1 -v` | RED: invalid installation and cancellation retaining its gate | manager-contract-red.log |
| `go test -race ./agentic ./processor/agentic-loop -run '^TestOperational' -count=1 -v` | PASS: 1.477s / 1.490s | state-manager-contract-green.log |
| `go test -race ./agentic ./processor/agentic-loop ./processor/agentic-dispatch ./frameworkcapabilities/graphresearch -count=1` | Agentic, dispatch and graphresearch PASS; loop FAIL only on the three pre-existing terminal-selection rows | affected-packages-second.log |
| `go test -race ./processor/agentic-loop -skip '^TestTerminalSelectionPreservesSavedOutcome$' -count=1` | PASS: 3.273s; explicit exclusion, not full-loop green | loop-unit-excluding-known-terminal-red.log |
| `go test -race ./processor/agentic-dispatch -count=1` | PASS after reviewer assertion correction: 2.173s | dispatch-after-review-green.log |

The focused watcher-loss test also passed after the assertion correction (1.538s). No native cancellation-overwrite
test, Docker integration, full push gate or E2E tier was rerun for this slice. All test processes completed; no
containers were started. Full loop remains RED on saved-success/failure/cancellation selection. The existing native
cancellation-overwrite assertion is preserved, not proven fixed.

## Reproduction and retained boundary

Local evidence and pre-edit source copies are in `/private/tmp/gh1146-loop-state.vULMuF`. Root preserved a reboot-safe
copy under `/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r6-state-contract.ZYhKkH`.
Its `final-r6-evidence` directory contains the final corrected patch, 39-file manifest, logs and pre-R6 source
copies. The initial review-input archive and patch remain separately preserved. These are local artifacts,
not hosted CI or published branch content.

The owner-approved spec and independent promotion review remain this change's active authority. First-R6 approval
permits the already-sequenced terminal correction to continue, not completion of R2/R6, acceptance of unchanged V4,
revocation of R3, merge, archive or issue closure. No commit or push is claimed.
