# graph-ingest Specification

## Purpose
TBD - created by archiving change graphable-merge-semantics. Update Purpose after archive.
## Requirements
### Requirement: A re-arriving entity's triples merge by predicate-level replacement

graph-ingest MUST merge the incoming triples of a re-arriving (already-existing)
entity by replacing per `(subject, predicate)`, not by appending, when the write
comes through the Graphable (JetStream) ingest lane. A predicate carried by the
incoming entity MUST replace that `(subject, predicate)`'s prior triples, so the
entity does not accumulate duplicate triples across repeated arrivals. This matches
the mutation (`AddTriples`) lane's merge semantics.

#### Scenario: republishing the same entity does not accumulate duplicates

- **GIVEN** an entity previously ingested with `flock.position.x = 1`
- **WHEN** the same entity is ingested again with `flock.position.x = 2`
- **THEN** the stored entity has exactly one `flock.position.x` triple
- **AND** its value is `2`

### Requirement: A predicate absent from an arrival is preserved

Merging MUST preserve any existing triple whose `(subject, predicate)` is not
present in the incoming arrival, so a Graphable arrival does not clobber
predicates written by a different writer (e.g. lifecycle-managed triples).

#### Scenario: a non-conflicting predicate survives a later arrival

- **GIVEN** an entity carrying `lifecycle.phase = active` and `sensor.temp = 20`
- **WHEN** a Graphable arrival for that entity carries only `sensor.temp = 21`
- **THEN** the stored entity still has `lifecycle.phase = active`
- **AND** `sensor.temp` is `21`

### Requirement: The create-time indexing profile is not overridden by a re-arrival

MUST preserve the create-time indexing profile across a merge: it is immutable
after create (ADR-054), so even though the merge is otherwise newer-wins, a
re-arrival that declares a different indexing profile MUST NOT change the stored
profile. A profile-less referential-integrity stub is the sole exception — its
first real arrival's declared profile stands as the entity's true birth.

#### Scenario: a re-arrival cannot change the create-time profile

- **GIVEN** an entity created with indexing profile `content`
- **WHEN** a later Graphable arrival for that entity declares profile `trace`
- **THEN** the stored indexing profile is still `content`
- **AND** the entity has exactly one indexing-profile triple

#### Scenario: a profile-less stub's first real arrival sets the profile

- **GIVEN** a profile-less referential-integrity stub for an entity
- **WHEN** the first real Graphable arrival declares indexing profile `content`
- **THEN** the stored indexing profile is `content`

### Requirement: A multi-valued predicate is full-set replaced

On merge, a multi-valued relationship predicate MUST be full-set replaced.
For a predicate a subject may hold several times (such as `flock.neighbor`), an
arrival that carries that predicate replaces the entire prior set for that
`(subject, predicate)` with the arrival's set. Producers therefore own publishing
the complete object set for such a predicate on each arrival; this lane MUST NOT
append individual relationship objects.

#### Scenario: a new neighbor set replaces the prior set

- **GIVEN** an entity whose stored `flock.neighbor` set is `{b, c}`
- **WHEN** a Graphable arrival carries `flock.neighbor` = `{c, d}`
- **THEN** the stored `flock.neighbor` set is exactly `{c, d}`
- **AND** the prior-only member `b` is no longer present

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
message with at-least-once semantics: an explicit ack after the merge completes; a
decode/extract/metadata failure or a stale-redelivery guard-drop acknowledges-and-drops
(counted, not redelivered); and a submit failure, a durable-guard read or write failure,
or a Process panic naks for redelivery. Acknowledgement MAY occur out of order across
lanes.

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

### Requirement: Ingest MUST reject structurally-invalid entity IDs and predicates

graph-ingest — the sole writer to `ENTITY_STATES` — MUST validate every mutation's entity ID and every
triple predicate against the structural-identity contract (entity ID = exactly 6 non-empty parts;
predicate = exactly 3 non-empty parts) before persistence. A mutation carrying any structurally-invalid
token MUST be rejected in full with a classified validation error, MUST NOT be persisted (not the bad
token, not the rest of the mutation), and MUST emit a loud (WARN or ERROR) log naming the offending
token, its kind (entity-id vs predicate), the source (rule/caller/subject), and the reason. Enforcement
is fail-closed: the write boundary is the single choke point, so a non-conforming token cannot enter the
graph regardless of its producer (framework, rule-stamped, product, or agent-authored).

#### Scenario: A mutation with a non-3-part predicate is rejected
- **WHEN** a graph mutation carries a triple whose predicate is `agent.role` (two parts)
- **THEN** the mutation is rejected with a classified validation error
- **AND** nothing from the mutation is written to `ENTITY_STATES`
- **AND** a loud log names the predicate, that it is a predicate, the source, and the reason

#### Scenario: A mutation with a non-6-part entity ID is rejected
- **WHEN** a graph mutation targets entity ID `acme.ops.robotics.gcs.drone` (five parts)
- **THEN** the mutation is rejected with a classified validation error and is not persisted

#### Scenario: A fully-conforming mutation is persisted unchanged
- **WHEN** a graph mutation targets a 6-part entity ID and carries only 3-part predicates
- **THEN** it passes the structural gate and is persisted with existing merge semantics intact

### Requirement: The structural gate MUST be unconditionally fail-closed with no bypass configuration

The handler-level structural gate MUST be unconditionally fail-closed: no bypass configuration exists
that lets a structurally-invalid predicate pass the gate. Every violation is rejected with a classified
validation error and metered exactly once — by the shared mutation-metering wrapper, under the error's
code (`mutation_rejections{reason="structural_invalid"}`) — with a loud log whose detail names the
offending predicate. Behind the gate, the authoritative persistence seam — the entity-state
contract validation every `ENTITY_STATES` write path calls (`graph.MarshalEntityState` /
`ValidateEntityStateContract`) — independently rejects structurally-invalid predicates, so the gate and
the seam are two fail-closed layers and no configuration can weaken either. (An observe-only escape
hatch was prototyped during this change and removed pre-release as provably inert: the seam's
unconditional rejection meant the hatch could only swap the caller-visible error code, never permit
persistence.)

#### Scenario: No configuration can weaken the gate
- **WHEN** a mutation carries a non-3-part predicate on the `triple.add` or `triple.add_batch` lane,
  under any component configuration
- **THEN** the gate rejects the mutation with the classified structural code before any KV I/O
- **AND** the `mutation_rejections{reason="structural_invalid"}` metric increments exactly once and a
  log names the token
- **AND** nothing from the mutation is written to `ENTITY_STATES`

