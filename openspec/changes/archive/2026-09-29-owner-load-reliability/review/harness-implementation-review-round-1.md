# Harness implementation review — round 1

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Source SHA-256:

- Integration: 5686655e0462a7fd92815b5c7ed38f8e930e1e8a14e33a88ee026d8131cdd9c4
- Helpers: b8909c6e4ce913b9a5ca16f1b44bd83a634195f54ac44da97a6f7b126cb0afb8
- Unit proofs: 065754807af5f9f131b03ac353ba79acb8cb4a5dadc21560d608d5fbae9a1f64

BLOCKING helpers:142 — Cancellation can become success. Seed workers suppress errors after cancellation; selecting
ready completion instead of ready cancellation can return nil. Concurrent completion at 302–309 has the same race
with canceled churn. Reject phase cancellation explicitly before success, capture its evidence, preserve any prior
primary failure, and add cancellation-at-completion proofs. Admission checks do not cover these exits.

HIGH unit:133 — Failure proofs strand fixtures. Intended omitted-sampler-join Fatal precedes releaseSampler:136.
Seed setup failure strands release:29; terminal setup failure can strand listing release:277. Register idempotent
release/cancel/finite completion recovery for every fixture owner before starting the helper. Keep the intended
assertion before release. The evidence claim that the old mutant's held sampler was released is contradicted by
source: correct it and repeat the selected mutation after recovery is fixed.

HIGH helpers:194 — Diagnostics use parent rather than actual derived operation context. A canceled phase does not
change parent.Cause, making the active proof's CallerCause check tautological. Seed has the same problem. Integration
385 reports an outer fifteen-minute deadline after Info's five-second context expires. Capture actual operation
context before finalization; independently observe the callback context in the ordering proof.

MEDIUM helpers:203 — Integer fixture keys alter FNV dispatcher lane assignment from the original fixture.name and
serial. Preserve the original key to keep the workload comparable; no unmeasured equivalence claim.

Shared helper wiring, sole-submitter lane closure, draining completed results, finite Info context and shared
terminal budget are sound. The old mutation reached its intended assertion, but did not establish safe fixture
recovery. Historical native pass is correctly separated from final-source validation.

CHANGES REQUESTED. No tests/mutations or edits ran in reviewer role. Full push gate remains held pending fixes.

Coordinator evidence retention: evidence/harness-review-round-1.patch reconstructs the three reviewed source files
on commit 0d4d6173. Patch SHA-256: 4e684d8c1893d504ce1195efc79b74e833e2fce97058c43db22cbd184a3ba7a0.
This records the reviewed predecessor, not a passing implementation.
