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

### Requirement: A redelivered stale message MUST NOT overwrite a newer write

graph-ingest MUST NOT apply a message whose source ordering position (its JetStream
stream sequence) is not newer than the position already applied to that entity **from
the same input stream**, so a delayed redelivery cannot overwrite a newer write through
the arrival-order (full-set-replace) merge. The guard MUST be keyed by
`(entity, input stream)` and MUST NOT compare positions across different input streams:
graph-ingest consumes multiple streams (e.g. `objectstore.stored.entity` +
`sensor.processed.entity`) whose sequence spaces are independent, so a cross-stream
comparison would silently drop a valid message from the lower-sequence stream. All
messages for one entity MUST serialize through a single lane regardless of which stream
they arrive on, so cross-stream writes to one entity apply in arrival order
(last-writer-wins), never concurrently. The guard MUST be updated only AFTER a message's
post-commit side effects complete, so a crash mid-apply re-drives them on redelivery
rather than a marker suppressing them.

The guard MUST survive process restart and in-memory cache eviction: a purely in-process
guard would re-admit a stale redelivery after a crash (empty on restart) or after a
high-cardinality eviction within the redelivery window, re-opening the overwrite. The
guard therefore MUST be backed by durable per-`(entity, input stream)` state in a
graph-ingest-owned store, written AFTER side effects and before ack (NOT inside the
entity's own record — that would commit before side effects and is a cross-repo schema
change). An in-memory tier MAY front it as a cache. This makes correctness independent of
`max_ack_pending`/ack-wait/`max_deliver` sizing (which stay tuning knobs to keep
redeliveries rare). A message that cannot be enqueued MUST be negatively acknowledged for
redelivery, never silently dropped; a durable-guard write failure MUST likewise not be
acknowledged past the unpersisted stamp.

#### Scenario: a late redelivery of an older update is ignored

- **GIVEN** an entity to which a message at stream sequence N from stream S has been applied
- **WHEN** a message for the same entity at stream sequence < N from the same stream S is
      (re)delivered and processed
- **THEN** it is dropped without overwriting the entity
- **AND** the entity still reflects the sequence-N (or newer) write

#### Scenario: a valid message from another stream is not dropped

- **GIVEN** an entity updated from stream A at A's sequence 1000
- **WHEN** a newer message for the same entity arrives from stream B at B's sequence 5
- **THEN** it is applied (not dropped) — sequences are compared only within a stream
- **AND** it serialized on the same lane as the stream-A write (no concurrent apply)

#### Scenario: a redelivery after restart is still ignored

- **GIVEN** an entity to which stream S sequence N was applied and acknowledged, then the
      process restarted (the in-memory guard was lost)
- **WHEN** an older stream-S message (sequence < N) is redelivered after restart
- **THEN** it is dropped without overwriting the entity — the durable guard tier survives
      the restart, so correctness does not depend on the in-memory map

## MODIFIED Requirements

### Requirement: Ingest MUST expose metrics that separate queue wait from processing time

graph-ingest MUST expose Prometheus metrics that make the ingest pipeline measurable at
the component. It MUST expose a per-message processing-duration histogram (time applying
a message — the merge and CAS write) and an ingest-lag histogram (message age when
processing begins — the stream/delivery-buffer wait). Under keyed-concurrent ingest it
MUST additionally expose: a LANE queue-wait histogram (time between submit to a lane and
the start of processing — distinct from the stream-backlog ingest-lag), an
achieved-concurrency (in-flight) gauge, a lane queue-depth gauge, a CAS-retry counter,
and a redeliveries-dropped counter. Together these MUST let an operator distinguish
backlog/queue wait from per-message processing time and observe achieved concurrency;
the throughput counter (`entities_updated_total`) remains the ingest-rate signal. The
CAS-retry counter is a cross-entity **contention-observability** signal — an entity's own
key is never written concurrently under keying, but legitimate cross-entity referential
writes (relationship-target stubs, foreign edges, shared hierarchy containers) still
touch shared keys and may retry — so it MUST NOT be interpreted as a keying-correctness
proof. The redeliveries-dropped counter (applied-sequence guard drops) is disjoint from
the pool's dropped counter (full-lane rejects); operators MUST NOT sum them.

#### Scenario: an operator can read processing vs queue time

- **GIVEN** graph-ingest processing a backlog of messages
- **WHEN** the operator scrapes metrics
- **THEN** the processing-duration histogram reports per-message merge+CAS time
- **AND** the ingest-lag histogram reports stream/delivery-buffer wait
- **AND** the lane queue-wait histogram reports time spent waiting for a lane
- **AND** the achieved-concurrency gauge reports how many lanes are processing
- **AND** the CAS-retry counter reflects cross-entity contention (not necessarily zero)
