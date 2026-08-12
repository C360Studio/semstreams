# nats-streaming Delta

## MODIFIED Requirements

### Requirement: Every publish path preserves trace, dedup, and breaker invariants

All publish paths — synchronous, acknowledged, asynchronous, and batch — MUST preserve the same client invariants:
(1) distributed trace context is injected into message headers; (2) a non-empty msgID is stamped as `Nats-Msg-Id`;
and (3) the circuit breaker is honored on entry.

A typed JetStream API error with error code `10077` and exact description `maximum bytes exceeded`, `maximum messages
exceeded`, or `maximum messages per subject exceeded` is a target-stream admission refusal. It MUST remain visible to
the caller through the existing error, wrapper, future, or batch aggregate and MUST be neutral to circuit accounting:
it neither records a failure nor resets prior failures. The successful asynchronous enqueue continues to perform its
existing liveness reset before any later capacity refusal resolves its future.

Every other publish failure MUST retain existing accounting, including message-too-large or unknown descriptions
under `10077`, other JetStream API codes, `nats.ErrMaxPayload`, and generic transport failures. Existing
metrics and logging MUST remain unchanged.

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
