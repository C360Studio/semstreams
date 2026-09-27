# Tasks: a deferred turn whose record write fails is retried until it lands

Tasks record work when it happens. No task asserts a post-merge fact. Claimed by PR #1403.

## 1. Implementation

- [x] 1.1 `settleDeferredContinuation`: non-CAS, non-size write failure remembers the result and returns an
      `errs.WrapTransient`-outermost Retry with a Warn line; CAS and success unchanged; size refusal acknowledged.
- [x] 1.2 `resumeDeferredContinuation` before `HandleTask`: re-write only while this turn is still the loop's
      uncarried turn; otherwise acknowledge.
- [x] 1.3 `releaseLoopTransientState` clears a released loop's pending results.
- [x] 1.4 `TestADeferredTurnWhoseWriteFailsIsRetriedUntilItLands` (integration, production entry path, real NATS):
      same-process redelivery, second-turn-in-between, no-observed-revision, release clears; controls success, CAS,
      size refusal.
- [x] 1.5 Migration note: `docs/operations/migration-beta162-to-beta163.md` § Task intake settles by class.

## 2. Review

- [ ] 2.1 SemStreams implementation review recorded on PR #1403.
