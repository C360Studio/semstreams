# Design: a deferred turn whose record write fails is retried

## 0. Ruling (verbatim)

Owner ruling on #1146, issuecomment-5854830449, Q3:

> **Q3 — ruled: the deferred turn Retries on any write error** at `component.go:1591` (today only a lost
> compare-and-swap propagates). Placement: the owner did not choose between "same PR" and "its own"; default taken =
> **its own small beta.163 PR** (different lane, different exit clause, separately reviewable). Owner may override in
> one word.

Size refusal (owner, #1400 issuecomment-5856565994, via the coordinator): "your ruling is fine" — a `nats.ErrMaxPayload`
refusal of the marker write stays acknowledged and documented as a permanent limit.

## 1. Measurements (base `fe9482b7`)

Measured with an integration probe on the production task-lane entry path (`deliverTask` → `taskInputHandler` →
`newLoopHeartbeatDeliveryPolicy`), real NATS, a loops-bucket wrapper whose `Update` fails; the probe became
`TestADeferredTurnWhoseWriteFailsIsRetried`.

**1.1 Error → decision.** The task lane's policy (`component.go`, `newLoopHeartbeatDeliveryPolicy`) maps `nil` → Ack,
`errs.IsFatal` → Quarantine, `*natsclient.PermanentDeliveryError` → Terminate, anything else → Retry (30 s delay).
`taskHandlerErrorDisposition` is not on the deferred branch. `errs.IsFatal` takes the first `ClassifiedError` by
`errors.As`, and without one falls back to a text match ("fatal", "panic", "corrupted", "disk full", …) and the
`ErrStorageFull` sentinel. A bare bucket error would therefore quarantine the lane when its text says "disk full",
and the no-observed-revision refusal is `errs.WrapFatal` at its source. Every Retry here is `errs.WrapTransient`
outermost, which `errors.As` finds first.

**1.2 Idempotency.** The turn is appended to the context before the write (`handlers.go`, `HandleTask`, the
`RegionRecentHistory` add precedes the deferral return). A redelivery into the process holding the loop hits
`HandleTask`'s task-id dedup (`attachContinuation` rebound the loop's `TaskID`) and the "Task deduplicated" arm
acknowledges it: turn in context once, marker never re-written (probe: `decision=Ack marker=false`). After a second
turn deferred behind the same request the dedup misses (the single-scalar residual documented on
`attachContinuation`) and the first turn is attached again (probe: turn in context twice, record text flipped from
the second turn to the first). Hence resume-before-`HandleTask`, gated on "this turn is still the loop's uncarried
turn".

**1.3 Cross-process redelivery.** Only a lost compare-and-swap releases the loop; the redelivery then meets the cold
fork (`classifyRedeliveredTask`): a record the winning carrier wrote names this task and settles on its applied or
republish arm, a record naming another task settles `taskContinuationUnheld` (acknowledged without effect, the
documented re-send recovery). Unchanged. Every other failure keeps the loop held, so the redelivery resumes the
remembered result in this process. A loop released by any other path during the retry delay (terminal, cancel,
another CAS loss) clears its pending results at `releaseLoopTransientState`, so its redelivery meets the cold fork
too, never a resume against a loop this process no longer holds.

## 2. Shape

`settleDeferredContinuation` owns the four settlements (written → Ack; CAS → Retry, loop released; size → Ack;
other → remember + Warn + Retry). `resumeDeferredContinuation` runs before `HandleTask` for a remembered deferred
result: loop held, `TaskID` this task's, marker set with no carrier, text this turn's → write again; otherwise Ack
with an Info line. No new map, no exported surface; the existing `pendingTaskResults` owner is extended.

## 3. Declared cost and bounds

- Each Retry delays all task intake for the lane's retry delay (30 s default, MaxAckPending 1).
- Exhaustion of `max_deliver` (default 2) takes the max-delivery exhaustion path and is counted by
  `semstreams_nats_max_delivery_exhaustions_total`; the turn then stays in process memory only, as before this change.
- The no-observed-revision refusal is an invariant breach nothing in production reaches with the loop held; it takes
  the same Retry and is bounded by `max_deliver`.
- A turn replaced by a later deferred turn during the delay is not re-written: the record holds one uncarried turn
  (#1365's single string), and the later write already carries the later turn.
- The write is retried while `max_deliver` allows — once at the default of 2 (`config.go` `DefaultConfig`
  `MaxDeliver: 2`). After exhaustion, or once the process no longer holds the loop, the turn is in process memory
  only and not durable, as before this change. "Never silently dropped" is about the delivery, not the turn.

## 7. Residuals

- **No in-place re-attempt (reviewer suggestion, declined).** One immediate re-run of the write before returning the
  Retry would absorb a momentary bucket blip without parking the lane for 30 s. Not taken: after a commit-unknown
  failure (a timeout whose Update did land) the re-run meets its own commit as a lost compare-and-swap and releases
  a healthy loop, a second write path whose ordering needs its own review. Record it here, revisit if the 30 s
  intake stall is observed.
- **Commit-unknown write, then the redelivery.** The same shape reaches the resume: the remembered revision predates
  the landed write, the re-run loses its compare-and-swap, and the loop is released and retried. The record already
  carries the marker and text, so a later cold rebuild replays the turn; the redelivered task itself settles on the
  cold fork (a record naming the previous task → `taskContinuationUnheld`, acknowledged without effect). Not
  observed by a test.
