# gated-dag-dispatch Specification

## Purpose
Current-truth for how the gated-DAG executor dispatches units: durable at-least-once over a JetStream stream (idempotent publish, claim rollback on publish failure, the durable-consumer + ack-after-marker contract, and the stranded-unit stall detector). ADR-070 (amends ADR-046).
## Requirements
### Requirement: Dispatch is durable at-least-once

The gated-DAG executor MUST publish each unit dispatch to a JetStream stream via
an ack-confirmed publish (`natsclient.PublishToStreamWithAck`), not a
fire-and-forget core-NATS publish. A dispatch that is ack-confirmed is durably
queued and delivered whenever a consumer (re)subscribes, so a lost dispatch
(consumer not subscribed / restarting) can no longer strand a claimed unit.

#### Scenario: dispatch is persisted before the executor considers it sent

- **GIVEN** a dispatchable unit
- **WHEN** the executor dispatches it
- **THEN** it publishes via an ack-confirmed JetStream publish to the dispatch
  stream
- **AND** treats the dispatch as sent only after the publish ack returns

#### Scenario: a consumer that subscribes later still receives the dispatch

- **GIVEN** a unit was dispatched while no consumer was subscribed
- **WHEN** a durable consumer subscribes afterward
- **THEN** the persisted dispatch is delivered to it

### Requirement: A failed publish rolls the claim back

The executor MUST clear a unit's durable claim when its dispatch publish fails
(the ack did not return), because a non-acked publish is proof the message was not
persisted and will not be delivered. The unit is then re-selected on the next
evaluation instead of being stranded until a manual reset.

#### Scenario: publish-ack failure re-arms the unit

- **GIVEN** the executor committed a unit's claim and then the dispatch publish
  failed to ack
- **WHEN** the next evaluation runs
- **THEN** the unit's claim has been cleared
- **AND** the unit is re-selected for dispatch (not skipped as claimed)

### Requirement: The framework provides a typed durable-consume primitive

natsclient MUST provide a durable at-least-once consume wrapper whose handler is
`func(ctx, []byte) error` — acking on nil, nak-with-delay on error, and holding a
long-running unit past `AckWait` via an `InProgress` heartbeat — so a consumer
never handles a raw `jetstream.Msg` for ack semantics. Envelope decoding remains
above natsclient.

#### Scenario: handler success acks, error naks

- **GIVEN** a durable consumer created via the wrapper
- **WHEN** the handler returns nil for a message
- **THEN** the message is acked
- **AND WHEN** the handler returns an error
- **THEN** the message is nak'd for redelivery

#### Scenario: a long-running handler is not redelivered while alive

- **GIVEN** a handler whose work runs longer than `AckWait`
- **WHEN** it is processing
- **THEN** `InProgress` heartbeats hold the message so it is not redelivered until
  the work finishes or the process crashes

### Requirement: Heartbeat interval is enforced below AckWait

The durable-consume configuration MUST reject a `heartbeat_interval` that is not
safely less than `ack_wait` at load/creation time, because a heartbeat that first
fires after `AckWait` has expired redelivers a still-running unit and causes
duplicate work. Documentation alone is insufficient.

#### Scenario: misconfigured heartbeat fails fast

- **GIVEN** a durable-consume config with `heartbeat_interval >= ack_wait`
- **WHEN** the config is validated
- **THEN** validation fails with an error naming both values

### Requirement: A stranded unit surfaces as a stall alert

The executor MUST surface a unit that is claimed, non-terminal, non-dirtied, and
older than a configured `stranded_after` threshold as a stall alert rather than
suppressing it (as it does today, where any claimed non-terminal unit reads as
healthy in-flight). This is alert-only — never auto-re-dispatch. A zero threshold
disables the check (back-compat).

#### Scenario: a long-stranded unit is alerted, not hidden

- **GIVEN** a unit claimed longer ago than `stranded_after`, with no terminal
  marker and not dirtied
- **WHEN** the executor evaluates stall
- **THEN** the unit is reported as stalled (not suppressed as in-flight)

#### Scenario: a fresh claimed unit is still treated as in-flight

- **GIVEN** a claimed non-terminal unit whose claim is newer than `stranded_after`
- **WHEN** the executor evaluates stall
- **THEN** the unit does not trigger a stall (a healthy in-flight unit is not
  falsely alerted)

