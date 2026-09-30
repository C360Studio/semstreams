# Child proof plumbing and fidelity review

Reviewer: `semstreams-reviewer` (`test_reliability_review`), 2026-09-29.

- test_owner_child_test.go: `21f9d4a195f3a2baf35d59e81ddf388ef9de4e3d313b7d089119e62e91f06b74`
- lifecycle_integration_test.go: `9eabd64f9a22c29898a0cae60618aac3a1c5a6c0b7664b8fa7e6d01b4fd1c308`

**Child plumbing PASS; proof-fidelity correction required before mutation approval.**

Exact argument/mode admission, invalid-request rejection, pre-NATS dispatch, actual m.Run exit propagation and
bounded single-process waiting are sound. Canonical focused integration can proceed.

Two proof limitations need correction:

- Setup/transfer children duplicate the provisional ownership protocol. Mutating that duplicate would test the
  fixture itself. Factor small provisional-finalization/transfer behavior into the existing private owner and use
  it from both real helpers and children, or exercise an actual helper failure path.
- Transfer-exit accepts cleanup witnesses emitted prematurely before transfer. Observe Start still live and no Drain
  immediately after transfer, before triggering Fatal.

The cleanup-error child proves deferred error reporting through its required sentinel. Its unconditional return
means absent NEXT_PHASE is not independently an admission-rejection proof.

The coordinator accepted factoring this existing boolean transfer behavior into the private owner shared by all six
helpers and the child proof, plus the causal post-transfer witness. No exported framework or runtime change is added.
Implementation and new mutation evidence remain pending final review; this record is not their approval.

## Bounded correction verdict

**Bounded correction PASS; focused canonical integration can proceed.**

Reviewer verified owner support `0a9e5ad3e4930d64d970b3d9b95df4a1b7370083e57fb0800a2d07952b43a945`
and child proof `d30f9feb4ff9f8f33d0850b69991d88e785447bbaff3fa66bba20359b2b2564f`.
All six real helpers now use the same provisional-finalization and transfer methods exercised by the child proof.
Ownership precedes fallible setup; transfer follows the final setup operation. Caller finalization remains independent
of the transfer marker. The transfer child now rejects premature Drain or canceled Start before Fatal. The fence-error
case requires the concrete failure witness and rejects next-phase admission, resolving the earlier vacuous check.

Mutation evidence, complete proof review, real integration outcomes and exact guard reconciliation remained pending.
No tests or edits were performed by the reviewer.
