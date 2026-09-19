# gated-dag-dispatch Specification

## Purpose
Current-truth for how the gated-DAG executor dispatches units: durable at-least-once over a JetStream stream (idempotent publish, claim rollback on publish failure, the durable-consumer + ack-after-marker contract, and the stranded-unit stall detector). ADR-070 (amends ADR-046).
## Requirements
### Requirement: Dispatch is durable at-least-once

The gated-DAG executor MUST publish each unit dispatch to its JetStream stream through the synchronous,
ack-confirmed `PublishToStreamWithMsgID` path. The logical unit ID MUST be the deterministic `Nats-Msg-Id`. A returned
PubAck proves persistence; a publish error is ambiguous and MUST NOT be described as proof that persistence did not
occur.

The configured `Duplicates` window MUST be at least the ordinary `BackstopInterval`, so a backstop-driven redispatch
of the same unit remains inside server deduplication. This covers that bounded redispatch interval only. It MUST NOT
be described as unbounded exactly-once delivery.

#### Scenario: dispatch is persisted before the executor considers it sent

- **GIVEN** a dispatchable unit
- **WHEN** the executor dispatches it
- **THEN** it publishes through `PublishToStreamWithMsgID` using the unit ID
- **AND** treats the dispatch as sent only after the PubAck returns

#### Scenario: a consumer that subscribes later still receives the dispatch

- **GIVEN** a unit was dispatched while no consumer was subscribed
- **WHEN** a durable consumer subscribes afterward
- **THEN** the persisted dispatch is delivered to it

### Requirement: A failed publish rolls the claim back

After a synchronous dispatch publish error, the executor MUST attempt to clear the unit's durable claim and local
in-flight hint so ordinary evaluation can select the unit again. It MUST preserve the same unit ID on every attempt.
The error SHALL remain commit-ambiguous: rollback plus deterministic message ID makes redispatch safe only within the
configured `Duplicates` window.

If durable `Unclaim` fails, the executor MUST clear only its local in-flight hint and surface the rollback failure.
The stranded-unit detector owns visibility of the durable claimed unit; automatic redispatch MUST NOT be claimed safe
while that durable claim remains.

#### Scenario: publish-ack failure re-arms the unit

- **GIVEN** the executor committed a unit's claim and then the dispatch publish
  failed to ack
- **WHEN** the next evaluation runs
- **THEN** the unit's claim has been cleared
- **AND** the unit is re-selected for dispatch (not skipped as claimed)

#### Scenario: publish error re-arms the unit inside the dedupe window

- **GIVEN** the executor committed a unit's claim and the synchronous dispatch publish returned an error
- **WHEN** durable Unclaim succeeds and ordinary evaluation selects the unit again within `Duplicates`
- **THEN** the repeated publish uses the same unit ID as `Nats-Msg-Id`
- **AND** server deduplication collapses any already-persisted first attempt inside that window

#### Scenario: dedupe horizon has elapsed

- **GIVEN** redispatch occurs after the configured `Duplicates` window
- **WHEN** the adopter receives the unit again
- **THEN** server message-ID deduplication is no longer claimed
- **AND** the adopter's durable already-complete or idempotent replay contract is authoritative

#### Scenario: durable rollback fails

- **GIVEN** dispatch publish returns an error and durable Unclaim also fails
- **WHEN** the executor releases its local in-flight hint
- **THEN** it surfaces the rollback failure and leaves the durable claim intact
- **AND** the stranded-unit detector, not an unsafe automatic redispatch claim, provides visibility

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

### Requirement: Each adopter owns its durable definition of done and replay

A gated-DAG dispatch adopter SHALL positively settle only after its own reviewed durable consequence is committed.
Before repeating effects after redelivery, including redelivery beyond the server dedupe window, it SHALL apply its
reviewed already-complete or idempotent replay check.

Transient failure SHALL retry. Immutable poison, already-complete work, ambiguous effects, and partial work SHALL
follow the adopter's reviewed ACK, Retry, Terminate, or Quarantine matrix rather than a generic nil/error inference.
Generic settlement, heartbeat, lease validation, and exact native consume-handle ownership belong to
`jetstream-consumer-policy`, not this domain capability.

#### Scenario: durable consequence defines positive settlement

- **GIVEN** an adopter has accepted a gated-DAG dispatch
- **WHEN** its domain consequence is durably committed
- **THEN** the adopter may return its reviewed positive settlement decision
- **AND** callback return shape alone does not define done

#### Scenario: redelivery checks durable domain authority

- **GIVEN** a dispatch is redelivered inside or beyond the server dedupe window
- **WHEN** the adopter's durable authority already records the consequence as complete
- **THEN** the adopter follows its reviewed already-complete decision without repeating the effect
- **AND** the server dedupe window is not treated as the durable completion authority

