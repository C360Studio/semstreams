# natsclient: async / pipelined JetStream publish (gh#470)

## Why

`natsclient` exposes only **synchronous** JetStream publishes. Both public methods
route to the same shared path, which blocks on the PubAck round-trip:

```go
// natsclient/client.go:834 (publishToStream, shared by PublishToStream + PublishToStreamWithMsgID)
_, err = js.PublishMsg(ctx, msg)
```

One message is in flight per caller goroutine; every publish pays the full
server persist+ack RTT. On localhost that is ~231µs/publish → a single producer
goroutine is RTT-bound at **~4.3k msg/s**, far below what JetStream itself
accepts.

semboids reproduced this running its graph-ingest load dial (200-boid flock
snapshots published serially via `PublishToStream`, gh#470 evidence table):

| Dial | Achieved snapshots/s | Entity publishes/s | Drops (10s) |
|---|---|---|---|
| 1 Hz | 1.0 | 200 | 0 |
| 10 Hz | 10.0 | 2,000 | 0 |
| 30 Hz | ~21.6 | ~4,300 | 83 (~28%) |

Ceiling math: 21.6 snapshots/s → 46.3ms per 200-entity snapshot → **~231µs per
publish — the sync ack RTT, exactly**. The producer's physics loop held 30 fps
throughout, so this is purely publish-side ack-wait, not ingest backpressure (the
ack fires on stream persist, before graph-ingest consumes). jetstream-go's async
publish (`PublishMsgAsync` + a bounded in-flight window) sustains 100k+ msg/s on
the same hardware — the sync-only path leaves ~20–100× on the table.

Impact: any load generator (semboids-as-instrument for the planned graph-ingest
melt-point campaign) **melts first**, before the system under test. Any bursty
producer (lifecycle wave spawns, bulk graph mutations, replays/backfills) pays
N × RTT serially with no in-client alternative.

This is a **framework substrate gap**, not a product concern: the streaming
publish primitive belongs to `natsclient` (SemStreams owns the NATS/KV runtime),
and the fix is to expose the async publish jetstream-go already provides — while
preserving natsclient's three existing publish invariants: distributed-trace
header injection, circuit-breaker accounting, and `Nats-Msg-Id` dedup stamping
(ADR-055 T1).

## What Changes

- **Async publish returning an ack future.** Add `PublishToStreamAsync(ctx,
  subject, data) (jetstream.PubAckFuture, error)` and
  `PublishToStreamAsyncWithMsgID(ctx, subject, data, msgID) (…)` — thin passthroughs
  over `js.PublishMsgAsync` that keep the shared pre-checks (circuit-open gate,
  connected gate), trace injection, and `Nats-Msg-Id` stamping. The synchronous
  enqueue returns immediately; the caller inspects the returned future's
  `Ok()`/`Err()` channels for the eventual ack.

- **Flush + inflight accessors.** `PublishAsyncComplete() <-chan struct{}` (closed
  when all outstanding async publishes are acked) and `PublishAsyncPending() int`
  (outstanding count) — pass-throughs so a producer can bound its own in-flight
  window and drain before shutdown.

- **Pipelined batch helper.** `PublishBatchToStream(ctx, subject, msgs [][]byte)
  error` — enqueues every message via the async path, waits for
  `PublishAsyncComplete` (bounded by `ctx`), and returns an aggregate error
  collected from each future. Preserves per-subject ordering (jetstream-go async
  preserves order per connection). Convenience path for bursty producers that
  don't need per-message futures.

- **Circuit-breaker integration for async acks.** Register a
  `WithPublishAsyncErrHandler` at `jetstream.New` time that calls
  `recordFailure()` (+ a `publish_async` error metric) on every failed async ack,
  so a broken ack path opens the breaker exactly as a failed sync publish does.
  Successful **enqueue** resets the breaker (see `design.md` for why enqueue —
  not ack — is the honest reset signal on the async path, and the one documented
  semantic difference from the sync path).

- **No change to existing sync methods or their callers.** `PublishToStream` /
  `PublishToStreamWithMsgID` are untouched; async is purely additive. No internal
  semstreams producer is retrofitted (per the gateway-first / no-retrofit
  discipline) — the first consumer is semboids-as-load-generator, which owns its
  in-flight window and ack handling.

## Impact

- **Affected specs:** new capability `nats-streaming` (seeded lazily by this
  change; distilled from `natsclient/client.go` and verified against code).
- **Affected code:** `natsclient/client.go` (connection setup gains the async err
  handler; new public methods + one shared internal async path),
  `natsclient/*_test.go` (unit + integration coverage).
- **No breaking change.** Additive API; the `jetstream.New` option is transparent
  to existing sync publishes. Default async max-pending (jetstream-go
  `defaultAsyncPubAckInflight = 4000`) is inherited — sufficient to pipeline well
  past the sync ceiling; not exposed as config in this change (deferred until a
  consumer needs a different window).
