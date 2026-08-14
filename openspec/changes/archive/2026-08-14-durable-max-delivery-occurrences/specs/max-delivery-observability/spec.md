# max-delivery-observability Specification

## ADDED Requirements

### Requirement: MaxDeliver exhaustion occurrences MUST be durably captured before component consumption

SemStreams MUST provision a framework-owned JetStream stream capturing exactly
`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>` before any component consumer starts. The stream MUST use file storage,
LimitsPolicy, DiscardOld, a seven-day MaxAge, a 64 MiB MaxBytes ceiling, and the framework's explicit single-replica
policy. It is a bounded occurrence ledger, not an authoritative current parked-message set.

#### Scenario: An incident occurs while no observer is running

- **GIVEN** the capture stream exists and every SemStreams observer is stopped
- **WHEN** NATS emits a MaxDeliver advisory
- **THEN** the occurrence is retained in `MAX_DELIVERY_EVENTS`
- **AND** the fixed durable observer receives it when a process later binds

#### Scenario: The ledger reaches a retention bound

- **GIVEN** the capture stream reaches MaxAge or MaxBytes
- **WHEN** NATS stores a newer exhaustion advisory
- **THEN** DiscardOld removes oldest evidence as needed
- **AND** the stream does not claim exhaustive history beyond its documented bounds

### Requirement: Replicas MUST share one fixed durable observer

Every SemStreams replica MUST bind the same durable consumer identity with explicit acknowledgements, DeliverAll, and
unlimited MaxDeliver. A single occurrence MUST be delivered to one active binding rather than independently to every
replica. Stopping a process MUST release its local binding without deleting the durable acknowledgement floor.

#### Scenario: Two application replicas are active

- **GIVEN** two SemStreams processes bound to the fixed durable
- **WHEN** one MaxDeliver advisory is captured
- **THEN** exactly one observer binding emits and acknowledges that occurrence

#### Scenario: Observer telemetry fails

- **GIVEN** a valid retained occurrence
- **WHEN** its operator telemetry cannot be emitted
- **THEN** the observer NAKs it for redelivery
- **AND** unlimited observer delivery prevents recursive MaxDeliver exhaustion

### Requirement: Valid occurrences MUST emit bounded telemetry before acknowledgement

The observer MUST validate the typed advisory and, before ACK, increment
`semstreams_nats_max_delivery_exhaustions_total` labelled only by domain, stream, and consumer and emit a structured
ERROR log carrying advisory ID, timestamp, domain, stream, consumer, stream sequence, and delivery count. Advisory ID
and sequence MUST NOT be metric labels. A crash between telemetry and ACK MAY duplicate the signal; advisory ID is the
deduplication key for downstream incident processing.

#### Scenario: A valid advisory is observed

- **GIVEN** a typed max-deliver event whose required fields and subject agree
- **WHEN** the durable observer handles it
- **THEN** the bounded-label counter and structured ERROR are emitted
- **AND** only then is the event acknowledged
- **AND** component readiness, health, retry policy, and message disposition remain unchanged

#### Scenario: A settlement operation fails

- **GIVEN** occurrence telemetry has been emitted or a poison event has been classified
- **WHEN** the observer's ACK or NAK call returns an error
- **THEN** `semstreams_nats_max_delivery_advisory_settlement_errors_total{operation}` increments and ERROR is emitted
- **AND** the observer does not claim that the durable floor advanced

### Requirement: Poison ledger entries MUST be visible and terminal

Malformed JSON, a wrong typed-event discriminator, a missing required field, or disagreement between subject and typed
payload MUST increment `semstreams_nats_max_delivery_advisory_decode_errors_total{reason}`, emit ERROR, and be ACKed.
Such poison entries MUST NOT redeliver forever.

#### Scenario: A malformed ledger entry is delivered

- **GIVEN** an entry that cannot decode as the required typed advisory
- **WHEN** the observer handles it
- **THEN** decoder-error telemetry identifies the bounded reason class
- **AND** the entry is acknowledged after that telemetry
