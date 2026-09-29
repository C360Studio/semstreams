# Early core-owner implementation review

Reviewer: `semstreams-reviewer` (`test_reliability_review`), 2026-09-29.
Base: `c9870a71`; source was uncommitted and frozen for this bounded review.
Root verified these hashes directly against source before recording this verdict:

- `processor/graph-ingest/test_owner_support_test.go`: `8a147f0b7311c4620796f7415a76def4c5fd0de698bd98a89d31b607b8b09261`
- `processor/graph-ingest/test_owner_test.go`: `ac744ebd63982b21c8f3db4ae4049eb822d272b6591b49ae9b3d8ef56bfa44db`

**Bounded core-owner PASS; no early implementation blocker found.**

- Attempt bookkeeping precedes Stop; deferred Start cancellation follows the synchronous call, including panic unwinding.
- Terminal authority is fresh, detached and finite. Concrete errors remain discoverable through errors.Join;
  operation authority stays available for subsequent assertions.
- finish reports failures without Fatal and suppresses implicit retry.
- The failed-attempt test is meaningful: cleanupPending retains the subscription after failure, so an erroneous
  second Stop would increment its Drain count. Production running-terminal behavior cannot conceal that retry.
- The controlled test observes the real Component.Stop boundary, finite authority, cancellation order and
  post-Stop operation usability.

Evidence limit: the initial RED establishes missing finite authority. The second test's nil-cancel panic does not
establish a causal retry-oracle failure; retain that distinction until the planned mutation evidence.

This is not approval of caller migration, assertion-exit ownership, joined-work proof or the complete PR.
No tests or changes performed by the reviewer. Later implementation changes require further review.
