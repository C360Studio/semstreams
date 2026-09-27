# Change: a deferred turn whose record write fails is retried

## Why

A continuation admitted while its loop's model request is outstanding defers: the turn goes into the loop's context
and its durable effect is one compare-and-swap that writes the pending-continuation marker and the turn's text onto
the loop record. Only a lost compare-and-swap reached the task lane's disposition; every other write failure was
swallowed, the delivery was acknowledged, and a process replacement lost the turn. A failed KV write is not a
retention limit (#1400). The owner ruled on #1146 (issuecomment-5854830449, Q3): the deferred turn Retries on any
write error, as its own beta.163 PR.

A bare Retry at the site does not do that: the redelivery into the process that holds the loop meets `HandleTask`'s
task-id dedup and is acknowledged without re-running the write, and once a later turn has rebound the loop's task id
it attaches the turn a second time (design § 1, measured).

## What changes

- A non-CAS failure of the deferred turn's record write remembers the result in the existing `pendingTaskResults`
  map and returns an `errs.WrapTransient`-outermost error, so the lane retries it and never quarantines on it.
- The redelivery resumes that result BEFORE `HandleTask`, and re-runs the write only while the loop still shows this
  turn uncarried; a turn a later request carried, or a later turn replaced, is acknowledged without writing.
- `releaseLoopTransientState` clears a released loop's pending results (the deferred ones and the existing lineage
  ones), so a redelivery for a loop nobody here holds meets the cold fork.
- Unchanged: a lost compare-and-swap (release and Retry); a write that lands (Ack); a size refusal (Ack, now
  documented as a permanent limit — owner, #1400 issuecomment-5856565994).

## Impact

- Spec: `agentic-loop` — requirement "Loop input classes settle after owner-specific durable done", two scenarios
  reworded (titles unchanged).
- Code: `processor/agentic-loop/component.go` (the settle and resume); `trajectory_handler_wiring.go` (one line: the
  release seam `releaseLoopTransientState` clears the released loop's pending results). No exported surface change.
- Operations: a Retry on the task lane parks all task intake for the retry delay (30 s at MaxAckPending 1);
  recorded in `docs/operations/migration-beta162-to-beta163.md`.
