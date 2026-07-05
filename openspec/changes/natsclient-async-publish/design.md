# Design — natsclient async publish (gh#470)

Three semantics the issue asks to pin down, plus the one deliberate divergence
from the sync path.

## 1. Ordering

jetstream-go's async publisher preserves per-subject ordering **per connection**:
`PublishMsgAsync` writes to the wire in call order on a single connection, and the
server stores in receive order. natsclient holds one `*nats.Conn` per `Client`, so
a single caller issuing `PublishToStreamAsync` (or `PublishBatchToStream`) to one
subject gets in-order storage without extra work. We do **not** add per-message
ordering controls — the guarantee is "single caller, single connection, in-order,"
matching what the sync path already gives.

Cross-goroutine ordering is the caller's responsibility (same as sync). We
document this rather than serialize internally: a global publish mutex would
re-introduce the exact RTT bottleneck this change removes.

## 2. Failed-ack surfacing — two channels, deliberately

An async publish can fail at two distinct points, and we surface each where it is
actionable:

- **Enqueue-time** (synchronous): circuit open → `ErrCircuitOpen`; not connected →
  `ErrNotConnected`; JetStream unavailable → error; in-flight window full past the
  stall wait → jetstream-go's `ErrTooManyStalledMsgs`. These return from the
  `PublishToStreamAsync*` call itself, so the caller learns immediately and the
  future is `nil`.
- **Ack-time** (asynchronous): the server nacks or the ack times out. This is
  delivered on the returned future's `Err()` channel **and** to the
  connection-level `WithPublishAsyncErrHandler`. The caller reads `Err()` for
  per-message handling; the handler feeds the circuit breaker (§4). Both see the
  same failure — the future is for the caller's business logic, the handler is for
  client health accounting.

`PublishBatchToStream` collapses both into one aggregate `error`: enqueue errors
short-circuit the batch (already-enqueued messages still drain); ack errors are
collected by ranging the futures after `PublishAsyncComplete`. The returned error
wraps the first failure and the total count (via `errors.Join`), so a caller gets
"3 of 200 failed" without threading futures.

## 3. Batch drain is ctx-bounded

`PublishBatchToStream` waits on `PublishAsyncComplete()` **or** `ctx.Done()`,
whichever fires first. On ctx cancellation it returns `ctx.Err()` wrapped, without
waiting for the remaining acks — the outstanding publishes are still in
jetstream-go's pending set and will resolve (or feed the err handler) in the
background. This mirrors the sync path honoring `ctx` and avoids a batch hanging
forever on a wedged ack path.

Two edge cases, handled explicitly (memory: select-race-on-pre-cancelled-ctx):

- **Already-cancelled ctx:** checked up front, before any enqueue, so nothing is
  published and the drain select never races a pre-satisfied `ctx.Done()`.
- **Cancel during drain:** `ctx.Done()` and `PublishAsyncComplete()` can become
  ready in the same instant; `select` then picks at random. On the `ctx.Done()`
  branch we re-check `PublishAsyncComplete()` non-blocking — a batch that actually
  finished draining is reported as success, not spuriously cancelled.

## 4. Circuit-breaker reset is on ENQUEUE, not ACK — the one documented divergence

The sync path resets the breaker **after** `PublishMsg` returns (i.e. after the
ack). The async path cannot mirror this cheaply: jetstream-go offers a global
failure handler (`WithPublishAsyncErrHandler`) but **no** global success handler,
and attaching a per-future goroutine purely to reset the breaker on success would
spawn one goroutine per publish — defeating the throughput win at 100k msg/s.

Decision: **a successful async enqueue resets the breaker; a failed async ack
records a failure (via the err handler).** This is honest because this client's
breaker is fundamentally a *connection-health* gate — `recordFailure()` is called
across the client for connect failures, a nil JetStream context, consumer-creation
failures, etc., not solely for publish-ack outcomes. A successful `PublishMsgAsync`
enqueue proves the connection is up and JetStream accepted the message onto the
wire, which is exactly the health signal the breaker gates on.

Consequences, stated plainly so the reviewer can check them:

- A pure-async producer (only `PublishToStreamAsync`, never sync/batch) still has a
  reset path — every enqueue clears `circuitFailures` — so a transient blip that
  recorded a few ack failures does not leave the breaker permanently degraded.
  (Without an enqueue reset it would rely solely on the `testCircuit` backoff timer
  to move open→disconnected, and `circuitFailures` would never re-zero.)
- Enqueue-reset can, in principle, race with a slightly-later ack failure arriving
  via the handler and mask a *single* failure count. This is acceptable: (a) the
  same reset-races-record window already exists on the sync path under concurrent
  callers; (b) in a genuine outage the connection drops and enqueue itself starts
  failing (`ErrTooManyStalledMsgs` / `ErrNotConnected`), which records failures and
  trips the breaker regardless of ack timing. The breaker still opens under real
  failure; it just won't over-trip on a lone late nack amidst healthy traffic.

`PublishBatchToStream` additionally records a failure per collected ack error, so a
batch with failing acks feeds the breaker even though its enqueues succeeded.

## 5. What we do NOT change

- Sync `PublishToStream` / `PublishToStreamWithMsgID` — untouched.
- Async max-pending window — inherits jetstream-go's default (4000). Not exposed as
  config until a consumer needs a different bound; the default already pipelines
  far past the sync ceiling. (Recorded here so the follow-up is a known, deliberate
  gap, not an oversight.)
- No internal semstreams producer is migrated to async in this change.
