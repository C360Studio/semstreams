# Owner-load harness failure ownership

## Why

Seed errors could deadlock the owner-load harness while publishing to an undrained error channel. Failures in its
concurrent phase could skip worker cleanup, lost results could wait until the package alarm, and consumer-baseline
polling could outlive its five-second window. These test defects waste time and obscure the original failure.

Investigation began with #1421's five-second listing expiry followed by a fifteen-second drain timeout. The bounded
native experiment did not reproduce that drain failure or establish the historical cause. This correction repairs
demonstrated harness defects and improves recurrence evidence; #1421 remains open.

## What Changes

Private helpers shared by the integration harness and fast controlled proofs own seed/concurrent admission,
cancellation and bounded completion. The first failure is retained and diagnosed before harness cancellation.
Consumer polling uses one finite context and rejects results after its expiry. Later worker shapes stop after a
failed phase. The original fixture-name dispatcher keys, workload, five-second KV ceiling and latency budgets stay
unchanged. No listing retry, production listing/drain change or timeout waiver is introduced.

The temporary native experiment is retained as reproducible evidence, not a compiled package test. Exact-set
correctness gaps remain tracked in #1293; this patch does not claim to repair them. #1417 owns broader cleanup debt;
Claude's #1426 remains independent.

## Impact and Verification

Only three graph-index test files change runtime test behavior. The graph-index spec adds explicit harness failure
ownership. The accepted pre-change inventory is preserved with its original hash/base; implementation-map.md records
22 current pins and exact reviewed source hashes. Independent design and implementation reviews passed.

Nine focused race proofs, a meaningful omitted-join mutation with independent fixture recovery, and a controlled
expired-baseline behavioral red/green passed. All required local gate stages passed: check:push completed through
unit race, integration admission refused Claude's active lock, and only the remaining canonical integration stage
was resumed with a bounded wait and passed. evidence/full-verification.md records exact results and limits.
Final archive/spec/evidence review passed. Hosted checks remain tracked on PR #1432; no issue-closure or
merge-readiness claim is made.

## Issue Disposition

PR #1432 references #1421 and leaves it open. The initial claim's closure declaration was removed before
implementation review because the observed harness defects do not establish the original incident's cause.
No new issue is filed by this patch. Any merge waiver for that still-unexplained required-job failure must be
explicit and specific to this PR; the earlier #1429 waiver does not transfer.
