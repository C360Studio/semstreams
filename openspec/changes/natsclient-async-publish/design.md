# Design — natsclient async publish (gh#470)

Three semantics the issue asks to pin down, plus the one deliberate divergence
from the sync path.

## 1. Ordering

jetstream-go's async publisher preserves per-subject ordering **per connection**:
`PublishMsgAsync` writes to the wire in call order on a single connection, and the
server stores in receive order. natsclient holds one `*nats.Conn` per `Client`, so
a single caller issuing `PublishToStreamAsync` (or `PublishBatchToStream`) to one
subject gets in-order storage without extra work. We do **not** add per-message
ordering controls — the guarantee is "single caller, single connection, in-order."

**Caveat vs. the sync path (weaker under retry).** This is *slightly weaker* than
the synchronous path, which fully serializes (message N+1 is not sent until N
acks/fails). jetstream-go async retries a `NoResponders` publish after `retryWait`
(≈250ms, up to 2 attempts — `publish.go` `handleAsyncReply`), re-publishing that one
message *after* later messages already reached the wire. So under a leadership blip
async storage order can differ from call order for the retried message. Absent a
`NoResponders` retry, order is preserved. Callers needing strict ordering across a
fault window use the sync path.

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
collected by ranging the batch's **own** futures (each `select`s on `Ok()`,
`Err()`, and `ctx.Done()`). The returned error wraps every failure and the total
count (via `errors.Join`), so a caller gets "3 of 200 failed" without threading
futures. The loop does **not** call `recordFailure` on an ack error — the
connection-level handler already records every async ack failure exactly once
(both fire from jetstream-go's single `doErr`), so recording here too would
double-count and trip the breaker at ~half the configured threshold for batches.

## 3. Batch drain is ctx-bounded — and waits on its OWN futures

`PublishBatchToStream` drains by ranging the batch's **own** futures, each in a
`select` over `Ok()`, `Err()`, and `ctx.Done()`. It deliberately does **not** wait
on the connection-global `PublishAsyncComplete()`: that channel closes only when the
whole connection's pending set hits zero, so a concurrent async producer on the same
`Client` could keep it non-empty and make an otherwise-finished batch over-wait until
its ctx expired. Per-future waiting scopes the drain to this batch alone. Each future
is already in-flight, so waiting in order costs the slowest ack, not the sum.

On ctx cancellation the loop returns `ctx.Err()` wrapped (with an "M of N acked, K
pending" count), without waiting for the remaining acks — the outstanding publishes
stay in jetstream-go's pending set and resolve (or feed the err handler on a
connection fault) in the background. This avoids a batch hanging on a wedged ack path.

Two edge cases (memory: select-race-on-pre-cancelled-ctx):

- **Already-cancelled ctx:** checked up front, before any enqueue, so nothing is
  published.
- **Cancel during drain:** the per-future `select` includes `ctx.Done()`. If both a
  future's `Ok()` and `ctx.Done()` are ready, `select` picks at random — harmless
  here: picking `Ok()` just processes that (genuinely-acked) message and the next
  iteration observes the still-cancelled ctx. A batch that *fully* drained before the
  cancel returns `nil` (all futures resolved via `Ok()`/`Err()`, loop completes) —
  correct, because the work actually finished.

## 4. The async breaker is a CONNECTION-LIVENESS gate — the documented divergence

The sync path resets the breaker **after** `PublishMsg` returns (i.e. after the
ack), giving it *consecutive-failure* semantics (any success re-zeroes the count).
The async path cannot mirror that cheaply: jetstream-go offers a global failure
handler (`WithPublishAsyncErrHandler`) but **no** global success handler, and
attaching a per-future goroutine purely to reset on success would spawn one
goroutine per publish — defeating the throughput win at 100k msg/s.

Decision: **on the async path the breaker gates connection liveness, not
message-level ack success.** Concretely:

- **A successful enqueue resets the breaker.** `PublishMsgAsync` accepting the
  message proves the connection is up and JetStream took it onto the wire — exactly
  the health signal this client's breaker gates on (`recordFailure()` fires across
  the client for connect failures, a nil JetStream context, consumer-creation
  failures, etc., not solely for publish-ack outcomes).
- **A failed async ack records a failure via the handler** — primarily to catch a
  *connection outage* fast: on disconnect jetstream-go's `resetPendingAcksOnReconnect`
  fires the handler for every pending publish (a burst of `recordFailure`), and
  subsequent enqueues also start failing (`js.PublishMsgAsync` errors /
  `ErrNotConnected`). Both push the count to threshold and open the breaker.
- **Message-level nacks on a healthy connection do NOT open the breaker.** If the
  connection stays up but acks fail for a *stream-level* reason (stream deleted,
  subject unbound, `MaxMsgs`/`MaxBytes` hit), the interleaved successful enqueues
  keep re-zeroing `circuitFailures`, so the breaker stays closed. This is
  **intentional**: those are per-message failures, surfaced to the caller via
  `future.Err()` / the batch aggregate, not connection faults. It is the deliberate
  divergence from the sync path (whose consecutive-nack run *would* open). A pure-
  async load generator therefore is not spuriously interrupted by sporadic nacks,
  while a real connection outage still trips the breaker.

Why not the alternatives: (a) *never reset on enqueue* → `circuitFailures`
accumulates cumulatively from sporadic nacks and eventually trips on a healthy
long-lived connection (wrong for a load generator; breakers should trip on
*sustained* failure); (b) *per-future success goroutine* → throughput-killing.
Connection-liveness is the honest middle that this client's breaker already models.

Because ack failures are recorded **only** by the handler, `PublishBatchToStream`
must NOT also record them in its collect loop (both would fire from jetstream-go's
single `doErr` → double-count, tripping at ~half the threshold for batches).

## 5. What we do NOT change

- Sync `PublishToStream` / `PublishToStreamWithMsgID` — untouched.
- Async max-pending window — inherits jetstream-go's default (4000). Not exposed as
  config until a consumer needs a different bound; the default already pipelines
  far past the sync ceiling. (Recorded here so the follow-up is a known, deliberate
  gap, not an oversight.)
- No internal semstreams producer is migrated to async in this change.
