# graph-ingest

> Delta for gh#480 (ADR-072). ADDs keyed-concurrent ingest to the `graph-ingest`
> capability. Verified against `processor/graph-ingest/component.go` +
> `graph/helpers.go`.

## ADDED Requirements

### Requirement: Entity ingest MUST process concurrently while preserving per-entity order

graph-ingest MUST dispatch entity ingest across multiple concurrent lanes so that
throughput is not bound to a single serial `Get`+CAS round-trip chain, while
guaranteeing that all messages for a **given entity ID** are processed serially in
arrival order. Messages MUST be partitioned to lanes by entity ID (same ID → same
lane), so that different entities ingest in parallel but one entity's updates never
apply out of order. This is load-bearing because the Graphable merge is arrival-order
newer-wins (a later-arriving message wins a `(subject,predicate)` conflict), so
out-of-order processing of one entity would let an older update overwrite a newer one.

The number of lanes MUST be operator-configurable via `ingest_lanes`, defaulting to a
value greater than 1 (concurrent by default) with `1` selecting the prior fully-serial
behavior. Because one entity ID is only ever processed on one lane at a time, two
concurrent CAS writes to the same entity key MUST NOT occur, so concurrency does not
introduce revision-mismatch retry storms.

#### Scenario: two entities ingest concurrently

- **GIVEN** `ingest_lanes` > 1 and two messages for two different entity IDs
- **WHEN** both arrive
- **THEN** they are processed on different lanes concurrently

#### Scenario: one entity's updates stay ordered

- **GIVEN** `ingest_lanes` > 1 and two messages for the SAME entity ID, message A
      before message B
- **WHEN** both are ingested
- **THEN** A is fully processed before B (same lane, serial)
- **AND** the stored entity reflects B as the newer write, never A overwriting B

#### Scenario: lanes=1 is the prior serial behavior

- **GIVEN** `ingest_lanes` = 1
- **WHEN** entities ingest
- **THEN** processing is fully serial, identical to the pre-change behavior

### Requirement: Concurrent ingest MUST bound in-flight work and preserve at-least-once ack

graph-ingest MUST bound the number of in-flight (dispatched-but-unacked) messages so
that a producer faster than ingest cannot grow unbounded in-memory queues. In-flight
work MUST be capped by both a bounded per-lane queue and a configurable
`max_ack_pending` on the consumer, and the ingest handler MUST acknowledge each
message with the same at-least-once semantics as before (explicit ack after the merge
completes; a decode/extract failure still acknowledges and is counted rather than
redelivered; only a panic naks for redelivery). Acknowledgement MAY occur out of order
across lanes.

#### Scenario: backpressure caps in-flight work

- **GIVEN** a producer offering entities faster than ingest can process them
- **WHEN** the lane queues and `max_ack_pending` are reached
- **THEN** the consumer stops fetching further messages until capacity frees
- **AND** in-memory queued work does not grow without bound

### Requirement: Ingest MUST expose metrics that separate queue wait from processing time

graph-ingest MUST expose Prometheus metrics sufficient to measure the ingest pipeline
in place: a per-message processing-duration histogram (time spent in the merge + CAS
write), a queue-wait histogram (time a message waits between dispatch and processing),
a queue-depth gauge, an achieved-concurrency (in-flight) gauge, and a CAS-retry
counter. These MUST make it possible to distinguish queue wait from processing time
(the prior gap: end-to-end latency could only be inferred downstream) and to confirm
that same-entity keying eliminated CAS contention (the retry counter stays at or near
zero under correct keying).

#### Scenario: an operator can read processing vs queue time

- **GIVEN** ingest under load
- **WHEN** the operator scrapes metrics
- **THEN** the processing-duration histogram reports merge+CAS time
- **AND** the queue-wait histogram reports time spent waiting for a lane
- **AND** the CAS-retry counter is at or near zero
