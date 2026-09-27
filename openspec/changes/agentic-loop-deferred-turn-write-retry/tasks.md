# Tasks: a deferred turn whose record write fails is retried

Tasks record work when it happens. No task asserts a post-merge fact. Claimed by PR #1403.

## 1. Implementation

- [x] 1.1 `settleDeferredContinuation`: non-CAS, non-size write failure remembers the result and returns an
      `errs.WrapTransient`-outermost Retry with a Warn line; CAS and success unchanged; size refusal acknowledged.
- [x] 1.2 `resumeDeferredContinuation` before `HandleTask`: re-write only while this turn is still the loop's
      uncarried turn; otherwise acknowledge.
- [x] 1.3 `releaseLoopTransientState` clears a released loop's pending results.
- [x] 1.4 `TestADeferredTurnWhoseWriteFailsIsRetried` (integration, production entry path, real NATS):
      same-process redelivery, second-turn-in-between, carried-by-the-outstanding-response, no-observed-revision,
      release clears; controls success, CAS, size refusal.
- [x] 1.5 Migration note: `docs/operations/migration-beta162-to-beta163.md` § Task intake settles by class.

## 2. Review

- [x] 2.1 SemStreams implementation review at `1c572823`: PASS WITH AMENDMENTS (one MEDIUM, two LOW), relayed by the
      coordinator; amendments applied in the next commit.
- [x] 2.2 Re-review: round 1 PASS WITH AMENDMENTS at `1c572823` (PR #1403 issuecomment-5856771283); amendments at
      `89ce0f54`, rebased `d7e27de2`. The coordinator accepts the fix pass on the reported mutation evidence and CI's
      Test job; no second reviewer round.
- [x] 2.3 BREAKING gate: `task e2e:agentic` at revision `d7e27de2` (coordinator run) — `msg="Scenario completed
      successfully" duration=5m35.582566875s ... assertions_run=20`, `task_exit=0`, compose stacks 0 before and after.
