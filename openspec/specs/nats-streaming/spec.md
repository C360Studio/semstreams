# nats-streaming Specification

## Purpose

The PUBLISH PATH onto a JetStream stream: how a message is written, acknowledged, deduplicated and
traced, and what each publish variant costs. It covers the synchronous ack round-trip, the
asynchronous ack future, the batch helper, deterministic message IDs for the server's
duplicate-detection window, and the invariants every one of those paths shares — trace-context
propagation, dedup headers, and circuit-breaker accounting.

It does NOT cover where streams come from. Declaring, creating, bounding and reconciling a stream is
`stream-provisioning`; reporting how much of the account those streams are consuming is
`storage-observability`. The boundary is deliberate and worth keeping: this capability answers "how
does a message get onto a stream", the other two answer "what stream exists, with what limits" and
"how full is it". Filing provisioning here would put a data-plane concern and a
capacity-and-lifecycle concern under one Purpose, which is how a spec stops being able to say no to
anything.

Related: `nats-kv-keys` (the KV plane's key grammar), `graph-retention` (the retention contract for
KV and ObjectStore backing streams, which stream provisioning explicitly refuses to touch).
## Requirements
### Requirement: Synchronous stream publish blocks on the PubAck

`PublishToStream` MUST publish one message to a JetStream stream and block until the
server acknowledges persistence (or errors). It is the at-most-one-in-flight path:
each call pays the full server persist+ack round-trip before returning.

#### Scenario: a synchronous publish returns only after the ack

- **GIVEN** a connected client and a stream bound to the subject
- **WHEN** `PublishToStream(ctx, subject, data)` is called
- **THEN** the call returns nil only after the server has acknowledged the write
- **AND** on the server-ack error path the call returns that error

### Requirement: Deterministic message IDs deduplicate re-published events

`PublishToStreamWithMsgID` MUST stamp the `Nats-Msg-Id` header so the server's
duplicate-detection window collapses re-publishes/redeliveries of the same logical
event to a single stored message. This is the producer half of the ADR-055 T1
at-least-once idempotency contract. An empty msgID MUST behave as `PublishToStream`
(no dedup), so the WithMsgID variant is a safe drop-in. The guarantee holds only
within the stream's configured `Duplicates` window.

#### Scenario: same msgID within the window stores once

- **GIVEN** a stream with a non-zero `Duplicates` window
- **WHEN** two messages carrying the same deterministic `Nats-Msg-Id` are published
      within that window
- **THEN** the stream stores exactly one message

### Requirement: Asynchronous stream publish returns an ack future without blocking

`PublishToStreamAsync` MUST enqueue a message to a JetStream stream and return a
`jetstream.PubAckFuture` immediately, without blocking on the PubAck, so a single
producer goroutine can pipeline many publishes past the synchronous ack-RTT ceiling.
The caller reads the future's `Ok()`/`Err()` channels for the eventual server
acknowledgement. `PublishToStreamAsyncWithMsgID` MUST additionally stamp
`Nats-Msg-Id` with the same dedup semantics as the synchronous WithMsgID variant.

`PublishAsyncComplete` MUST return a channel that closes when every outstanding
async publish has been acknowledged, and `PublishAsyncPending` MUST return the count
of outstanding (enqueued-but-unacked) async publishes, so a producer can bound its
own in-flight window and drain before shutdown.

#### Scenario: async publishes pipeline and drain

- **GIVEN** a connected client and a stream bound to the subject
- **WHEN** N messages are published via `PublishToStreamAsync` and the caller waits
      on `PublishAsyncComplete`
- **THEN** all N messages are stored on the stream
- **AND** each returned future resolves via `Ok()` with no `Err()`
- **AND** `PublishAsyncPending` returns 0 after the complete channel closes

#### Scenario: async publish enqueue is rejected when the circuit is open

- **GIVEN** a client whose circuit breaker is open
- **WHEN** `PublishToStreamAsync` is called
- **THEN** it returns `ErrCircuitOpen` and a nil future
- **AND** no message is enqueued

### Requirement: A batch helper pipelines and returns an aggregate error

`PublishBatchToStream` MUST publish every message in a slice to one subject via the
async path, wait for all acks (bounded by the caller's context), and return a single
aggregate error. Per-subject ordering from the single calling goroutine MUST be
preserved. If the context is cancelled before all acks arrive, it MUST return the
context error rather than hang; the already-enqueued publishes resolve in the
background.

#### Scenario: a batch stores all messages in order and reports no error

- **GIVEN** a connected client and a stream bound to the subject
- **WHEN** `PublishBatchToStream(ctx, subject, msgs)` is called with M messages
- **THEN** all M messages are stored in publish order
- **AND** the returned error is nil

#### Scenario: a batch surfaces failed acks as an aggregate error

- **GIVEN** a batch in which one or more messages fail to be acknowledged
- **WHEN** `PublishBatchToStream` completes
- **THEN** it returns a non-nil error identifying that acks failed and how many

### Requirement: Every publish path preserves trace, dedup, and breaker invariants

All publish paths — synchronous, asynchronous, and batch — MUST preserve the same
three client invariants: (1) a distributed-trace context is injected into the
message headers (auto-generated when absent from `ctx`); (2) a non-empty msgID is
stamped as `Nats-Msg-Id`; (3) the circuit breaker is honored on entry (an open
circuit rejects the publish).

On the async path the breaker is a **connection-liveness** gate: a successful
enqueue resets it (the connection is up), and a failed async ack records a breaker
failure via the connection-level async error handler. On a **connection outage**
jetstream-go fires that handler for every pending publish; once the recorded
failures cross the breaker threshold the breaker MUST open. (The `ErrNotConnected`
status gate additionally rejects enqueues fast during an outage — via status, not
the failure count.) **Message-level ack failures on a healthy connection** (e.g. a
stream-full nack) MUST be surfaced to the caller via the future's `Err()` channel /
the batch aggregate error, and MUST NOT by themselves open the breaker (the
interleaved successful enqueues keep it closed). This is the deliberate divergence
from the synchronous path's consecutive-failure semantics.

#### Scenario: a connection outage opens the breaker

- **GIVEN** a connected client publishing via `PublishToStreamAsync`
- **WHEN** the connection is lost and pending acks fail past the breaker threshold
- **THEN** the circuit breaker opens
- **AND** subsequent publishes are rejected with `ErrCircuitOpen` until it resets

#### Scenario: a successful enqueue resets the breaker failure count

- **GIVEN** a connected client that has recorded some failures below the threshold
- **WHEN** a message is enqueued successfully via `PublishToStreamAsync`
- **THEN** the recorded failure count is reset to zero

### Requirement: Heartbeat consumption SHALL expose settlement failure

`ConsumeWithHeartbeat` SHALL return ACK, delayed NAK, and Term settlement errors to its caller while preserving the
existing heartbeat and shutdown delays. It SHALL not discard a settlement error after work has returned.

#### Scenario: transient work fails and delayed NAK fails

- **WHEN** work returns a transient error
- **AND** `NakWithDelay` also fails
- **THEN** the returned error chain contains both failures

#### Scenario: shutdown NAK fails

- **WHEN** context cancellation owns the delivery outcome
- **AND** the five-second delayed NAK fails
- **THEN** the returned error chain contains context cancellation and the settlement failure

