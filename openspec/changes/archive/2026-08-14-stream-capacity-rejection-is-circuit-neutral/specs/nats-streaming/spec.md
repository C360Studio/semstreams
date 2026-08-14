# nats-streaming Delta

## MODIFIED Requirements

### Requirement: Every publish path preserves trace, dedup, and breaker invariants

All publish paths — synchronous, acknowledged, asynchronous, and batch — MUST preserve the same three client
invariants: (1) a distributed-trace context is injected into the message headers (auto-generated when absent from
`ctx`); (2) a non-empty msgID is stamped as `Nats-Msg-Id`; and (3) the circuit breaker is honored on entry (an open
circuit rejects the publish).

On the async path the breaker is a connection-liveness gate: a successful enqueue resets it (the connection is up),
and a failed async ack records a breaker failure via the connection-level async error handler. On a connection outage,
jetstream-go fires that handler for every pending publish; once the recorded failures cross the breaker threshold the
breaker MUST open. The `ErrNotConnected` status gate additionally rejects enqueues fast during an outage through
status, not the failure count.

Message-level ack failures on a healthy connection MUST be surfaced to the caller through the future's `Err()`
channel or the batch aggregate error and MUST NOT by themselves open the breaker; interleaved successful enqueues
keep it closed. This is the deliberate divergence from the synchronous path's consecutive-failure semantics.

A typed JetStream API error with error code `10077` and exact description `maximum bytes exceeded`, `maximum messages
exceeded`, or `maximum messages per subject exceeded` is a target-stream admission refusal. It MUST remain visible to
the caller through the existing error, wrapper, future, or batch aggregate and MUST be neutral to circuit accounting:
it neither records a failure nor resets prior failures. The successful asynchronous enqueue continues to perform its
existing liveness reset before any later capacity refusal resolves its future.

Every other publish failure MUST retain existing accounting, including message-too-large or unknown descriptions
under `10077`, other JetStream API codes, `nats.ErrMaxPayload`, and generic transport failures. Existing metrics and
logging MUST remain unchanged.

#### Scenario: a connection outage opens the breaker

- **GIVEN** a connected client publishing via `PublishToStreamAsync`
- **WHEN** the connection is lost and pending acks fail past the breaker threshold
- **THEN** the circuit breaker opens
- **AND** subsequent publishes are rejected with `ErrCircuitOpen` until it resets

#### Scenario: a successful enqueue resets the breaker failure count

- **GIVEN** a connected client that has recorded some failures below the threshold
- **WHEN** a message is enqueued successfully via `PublishToStreamAsync`
- **THEN** the recorded failure count is reset to zero

#### Scenario: a full stream does not block an unrelated stream

- **GIVEN** a connected client with prior circuit failures below its threshold
- **AND** a target stream whose configured `DiscardNew` bytes, messages, or per-subject message ceiling is full
- **WHEN** a publish receives the corresponding typed capacity refusal
- **THEN** the original refusal remains visible to the caller
- **AND** neither circuit failure count changes
- **AND** the client remains connected and can publish to an unrelated writable stream

#### Scenario: similar errors remain connection failures

- **GIVEN** a publish error whose code or exact description is outside the defined capacity-refusal set
- **WHEN** a publish path accounts for that error
- **THEN** it retains the existing circuit-failure behavior
