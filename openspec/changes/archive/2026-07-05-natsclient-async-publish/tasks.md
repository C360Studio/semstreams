# Tasks — natsclient async publish (gh#470)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. Connection setup — async error handler

- [x] 1.1 At `jetstream.New(conn, …)` (`client.go:448`), pass
      `jetstream.WithPublishAsyncErrHandler(m.asyncPublishErrHandler)`. Handler
      calls `m.recordFailure()` and `m.jsMetrics.recordError("publish_async")`
      (nil-guard `jsMetrics`), and logs at Debug with subject + error. This is the
      ack-failure → circuit-breaker bridge (design §4).

## 2. Shared async publish path

- [x] 2.1 Add `publishToStreamAsync(ctx, subject, data, msgID) (jetstream.PubAckFuture, error)`
      mirroring `publishToStream`'s pre-checks: circuit-open gate → `ErrCircuitOpen`;
      connected gate → `ErrNotConnected`; `JetStream()` → record failure on error;
      auto-generate trace context; build `*nats.Msg`; stamp `Nats-Msg-Id` when
      `msgID != ""`; `InjectTrace`.
- [x] 2.2 Call `js.PublishMsgAsync(msg)`. On enqueue error (incl.
      `ErrTooManyStalledMsgs`) → `recordFailure()`, return `nil, err`. On success →
      `resetCircuit()` (design §4: enqueue is the honest reset signal), return the
      future.

## 3. Public methods

- [x] 3.1 `PublishToStreamAsync(ctx, subject, data) (jetstream.PubAckFuture, error)`
      → `publishToStreamAsync(ctx, subject, data, "")`.
- [x] 3.2 `PublishToStreamAsyncWithMsgID(ctx, subject, data, msgID) (…)` →
      `publishToStreamAsync(ctx, subject, data, msgID)`. Doc the ADR-055 T1 dedup
      contract (same as the sync WithMsgID variant): deterministic msgID per logical
      event; dedup holds within the stream's configured `Duplicates` window.
- [x] 3.3 `PublishAsyncComplete() <-chan struct{}` — guard JetStream availability;
      passthrough to `js.PublishAsyncComplete()`. (Return an already-closed channel
      when JetStream is unavailable so a drain loop doesn't block forever.)
- [x] 3.4 `PublishAsyncPending() int` — passthrough to `js.PublishAsyncPending()`;
      `0` when JetStream unavailable.

## 4. Batch helper

- [x] 4.1 `PublishBatchToStream(ctx, subject, msgs [][]byte) error`: circuit/connected
      pre-checks once; enqueue each via `publishToStreamAsync(ctx, subject, m, "")`,
      collecting futures; on an enqueue error, stop enqueuing and remember it (already-
      enqueued messages still drain).
- [x] 4.2 Wait on `PublishAsyncComplete()` or `ctx.Done()`; on ctx cancel, return
      `ctx.Err()` wrapped (design §3). On complete, range futures' `Err()` channels
      (non-blocking select with `Ok()`), `recordFailure()` per ack error, and return
      an aggregate error via `errors.Join` (first enqueue error + all ack errors),
      including a "N of M failed" count. Nil on all-acked.

## 5. Tests (integration — real JetStream via testcontainers)

- [x] 5.1 `PublishToStreamAsync` happy path: publish N, drain via
      `PublishAsyncComplete`, assert stream `LastSeq == N` and every future's `Ok()`
      resolved (no `Err()`).
- [x] 5.2 Ordering: async-publish a monotonic sequence to one subject from one
      goroutine; consume and assert stored order matches publish order.
- [x] 5.3 `PublishToStreamAsyncWithMsgID` dedup: publish the same msgID twice within
      the stream `Duplicates` window → exactly one stored message (mirror the sync
      WithMsgID dedup test).
- [x] 5.4 `PublishBatchToStream` happy path: batch of M → all stored, nil error,
      aggregate reflects zero failures; and a ctx-cancel case returns a ctx error
      without hanging.
- [x] 5.5 Circuit breaker: (a) enqueue on an open circuit → `ErrCircuitOpen`, no
      publish; (b) enqueue success resets `circuitFailures` (assert via a
      recordFailure-then-successful-enqueue sequence). Ack-failure→handler path
      documented; assert the handler is wired (unit: handler calls recordFailure).
- [x] 5.6 Trace + msgID header presence: consume a raw async-published msg and assert
      the trace headers and (for the WithMsgID variant) `Nats-Msg-Id` are stamped —
      same invariants the sync path guarantees.

## 6. Spec + gates + close

- [x] 6.1 `openspec validate --strict`.
- [x] 6.2 Gates: `go test -race ./natsclient/...`,
      `go test -race -tags=integration ./natsclient/...`, `task lint` (revive clean),
      `task schema:generate` + `git diff schemas/ specs/` no-drift, `go vet` with
      `-tags=integration`.
- [x] 6.3 semstreams-reviewer pre-merge — two rounds. R1: HIGH (batch
      double-count) + 3 MEDIUM. R2 (APPROVE-WITH-NITS): fixed the drain
      select-race re-check, reverted un-tidy go.sum churn, doc-precision on the
      breaker mechanism. All findings addressed; invariants/ordering/no-goroutine
      claims verified against jetstream-go source.
- [x] 6.4 Archive → promote `nats-streaming` into `openspec/specs/`.
- [ ] 6.5 e2e:core GREEN (passed=2 failed=0: core-health + core-dataflow, 8
      healthy components, websocket dataflow verified — additive change, no
      regression). PR; tag.
- [ ] 6.6 Confirm back to semboids on gh#470 (async publish available; they own the
      in-flight window + ack handling for the load-dial harness).
