# graph-ingest Specification

## Purpose

graph-ingest is the **sole writer to `ENTITY_STATES`**, and this capability governs what that
write means. It owns two ways state arrives and the rules each one follows:

- the **Graphable lane** — JetStream arrivals that resolve by predicate-level replacement, so a
  re-arriving entity refreshes its predicates rather than accumulating them, with per-entity
  ordering preserved under concurrent processing and an applied-sequence guard so a redelivered
  stale message cannot overwrite a newer write;
- the **mutation lane** — the four typed request/reply operations (`entity.create`,
  `entity.reconcile`, `triple.append`, and `entity.delete`), with strict birth, revision-fenced
  reconciliation and deletion, and exact-tuple append deduplication.

It also owns the structural gate that rejects invalid entity IDs and predicates — unconditionally
fail-closed, with no bypass configuration — the create-time indexing profile, and the queue-wait
versus processing-time metric split.

**What it does NOT cover.** Readiness and coverage of the derived indexes belong to
`graph-index-readiness`; retention and deletion policy to `graph-retention`; the shape of the
stored entity itself to `graph-state-contract` and `predicate-contract`; query and retrieval to
`graph-query`. This capability is about the write boundary — who may write, by what rule, and
what the response tells the caller about what happened.
## Requirements
### Requirement: A re-arriving entity's triples merge by predicate-level replacement

graph-ingest MUST merge the incoming triples of a re-arriving (already-existing)
entity by replacing per `(subject, predicate)`, not by appending, when the write
comes through the Graphable (JetStream) ingest lane. A predicate carried by the
incoming entity MUST replace that `(subject, predicate)`'s prior triples, so the
entity does not accumulate duplicate triples across repeated arrivals.

This is NOT the same rule the `triple.append` mutation applies. Append is append-only and
deduplicates by exact six-field tuple, so it preserves multiple distinct values under one
predicate — which is what multi-valued predicates such as hierarchy containment and sibling edges
require. Predicate-level replacement during append would delete them. The two paths converge only
on the outcome that a repeated identical write accumulates nothing.

#### Scenario: republishing the same entity does not accumulate duplicates

- **GIVEN** an entity previously ingested with `flock.position.x = 1`
- **WHEN** the same entity is ingested again with `flock.position.x = 2`
- **THEN** the stored entity has exactly one `flock.position.x` triple
- **AND** its value is `2`

#### Scenario: append preserves multiple values under one predicate

- **GIVEN** an entity carrying two `hierarchy.type.contains` triples with distinct objects
- **WHEN** a third distinct `hierarchy.type.contains` triple is submitted through
  `graph.mutation.triple.append`
- **THEN** the entity carries three `hierarchy.type.contains` triples

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
profile. Missing relationship targets do not create profile-less entities.

#### Scenario: a re-arrival cannot change the create-time profile

- **GIVEN** an entity created with indexing profile `content`
- **WHEN** a later Graphable arrival for that entity declares profile `trace`
- **THEN** the stored indexing profile is still `content`
- **AND** the entity has exactly one indexing-profile triple

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
key is never written concurrently under keying, but legitimate cross-entity hierarchy
container and inverse writes still touch shared keys and may retry — so it MUST NOT be interpreted as a
keying-correctness proof. The redeliveries-dropped counter (applied-sequence guard drops) is disjoint from
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
- **WHEN** a mutation carries a non-3-part predicate on the `triple.append` lane,
  under any component configuration
- **THEN** the gate rejects the mutation with the classified structural code before any KV I/O
- **AND** the `mutation_rejections{reason="structural_invalid"}` metric increments exactly once and a
  log names the token
- **AND** nothing from the mutation is written to `ENTITY_STATES`

### Requirement: Append MUST NOT store a triple already present with an identical tuple

Append MUST suppress any triple whose six-field identity tuple — subject, predicate,
object, datatype, source, context — already exists on the target entity. Object identity MUST be
decided on the object's PERSISTED encoding, so that a value and the form it takes after a storage
round-trip are one tuple. An object submitted in process and the same object read back from
stored state MUST NOT be treated as two distinct assertions, whatever its Go type — a producer
re-deriving its facts would otherwise append them again on every restart. This covers every way a
round-trip can change an encoding, including field ordering in structured values and numeric width
in values outside the storage format's exact range; a type excluded from normalization MUST be one
whose round-trip is provably the identity, not one assumed to be unaffected. Suppression is
unconditional: it does not depend on `RequestID`, on `Context` carrying a request identifier, on
the caller, or on which entry point was used. It MUST cover every append emitter from a single
implementation, including the `graph.mutation.triple.append` handler, hierarchy inference's
in-process append path, and projection clients. Duplicates appearing more than once within a single
request MUST also collapse, preserving first-input order, so one request commits at most one copy
of a tuple.

#### Scenario: a stored tuple is not appended again

- **GIVEN** an entity already carries a triple with a given subject, predicate, object, datatype,
  source, and context
- **WHEN** an append request submits a triple with that same six-field tuple
- **THEN** the entity still carries exactly one copy of that tuple

#### Scenario: duplicates within one batch collapse to one

- **GIVEN** an entity that carries none of the submitted tuples
- **WHEN** a single batch submits the same six-field tuple three times, interleaved with two
  distinct tuples
- **THEN** the entity gains exactly three triples
- **AND** their relative order matches first appearance in the request

#### Scenario: suppression does not depend on a request identifier

- **GIVEN** an entity already carries a triple stamped with context `inference.hierarchy` and no
  request identifier
- **WHEN** the identical triple is submitted again with no request identifier
- **THEN** it is suppressed
- **AND** the outcome is the same as for a request that carries one

### Requirement: A fully duplicate append MUST advance no ENTITY_STATES revision

A request whose triples are all suppressed as duplicates MUST commit nothing. The check MUST
short-circuit inside the compare-and-swap closure, before the revision-checked write, so no
`ENTITY_STATES` revision advances, no watcher re-fires, and the entity's version and update
timestamp are unchanged. Returning the unchanged entity from the closure is NOT sufficient: that
is an identity rewrite, which bumps the revision and re-fires every watcher — the precise
behavior that makes a restart's replay observable downstream. The request MUST report success,
not an error, and MUST NOT increment the component error counter.

#### Scenario: a duplicate-only write leaves the revision untouched

- **GIVEN** an entity at a known KV revision whose triples include every tuple about to be
  submitted
- **WHEN** the append request is processed
- **THEN** the request succeeds
- **AND** the entity's KV revision is unchanged
- **AND** the entity's version and update timestamp are unchanged

#### Scenario: a restart's replay is invisible to watchers

- **GIVEN** a component that re-submits the identical derived triples for unchanged entities on
  restart
- **WHEN** the component restarts against the same store with no source data changed
- **THEN** no `ENTITY_STATES` watcher observes an update for those entities
- **AND** stored triple cardinality is identical before and after the restart

#### Scenario: a partially-duplicate write commits only the new tuples

- **GIVEN** an entity carrying one of two submitted tuples
- **WHEN** the append request is processed
- **THEN** exactly one triple is appended
- **AND** the KV revision advances exactly once

### Requirement: Deduplication MUST be atomic against concurrent identical requests

Deduplication MUST run inside the same compare-and-swap closure whose write is revision-checked,
so a request that loses the CAS re-evaluates against the winner's committed state and suppresses
on the retry. A read performed outside the CAS loop MUST NOT be used to decide suppression: it
reintroduces the time-of-check-to-time-of-use window in which two concurrent identical requests
both observe the tuple absent and both append.

#### Scenario: two concurrent identical appends store one tuple

- **GIVEN** an entity that does not yet carry the tuple
- **WHEN** two identical append requests are issued concurrently
- **THEN** exactly one copy of the tuple is stored
- **AND** both requests report success

#### Scenario: a late commit followed by an identical retry stores one tuple

- **GIVEN** an append request whose response is lost after the write commits
- **WHEN** the caller retries with identical provenance
- **THEN** exactly one copy of the tuple is stored
- **AND** the retry reports success

### Requirement: Append responses report one discriminated result per subject

Append MUST return one result for each distinct submitted subject. Its outcome MUST be `applied`,
`unchanged`, `entity_not_found`, or `failed`. `applied` and `unchanged` MUST carry a nonzero KV
revision and no error. `entity_not_found` MUST carry neither a revision nor an error. `failed`
MUST carry a typed `{class, code}` error and no revision. The response MUST NOT contain aggregate
written, suppressed, or failed-subject counts, a degraded flag, or a single revision that implies
a cross-subject transaction.

#### Scenario: a mixed append is fully accounted by subject

- **GIVEN** one append request targets existing A, unchanged B, absent C, and invalid D
- **WHEN** the request is processed
- **THEN** A reports `applied` with its committing revision
- **AND** B reports `unchanged` with its observed live revision
- **AND** C reports `entity_not_found` without a revision or error
- **AND** D reports `failed` with a typed error and no revision

### Requirement: A no-op mutation MUST report that it committed nothing

A mutation that commits nothing MUST report `unchanged`. For append this means every submitted
canonical tuple is already present. For reconcile it means the selected predicates already equal
the requested desired state. The reported KV revision is observed live state, not evidence of a
commit by this caller, and no KV revision, logical entity version, or update timestamp may advance.

#### Scenario: an unchanged append is not attributed to the caller

- **GIVEN** a rule that previously wrote a triple, and a later unrelated write by another
  component that advanced the entity's revision
- **WHEN** that rule re-asserts the identical triple and it is suppressed
- **THEN** the response reports `unchanged` with the live revision
- **AND** the rule does not record the reported revision as its own
- **AND** the rule still evaluates when its watcher delivers the other component's change

### Requirement: Suppressed duplicates MUST be observable

Suppressed duplicates MUST be counted on a metric labeled by the lane that submitted them, so an
operator can tell a silently-skipped write from absent traffic, and can attribute a sustained
suppression rate to the component producing it. A suppression MUST NOT be logged per occurrence:
replay traffic makes that unbounded.

#### Scenario: suppression is attributable to its lane

- **GIVEN** hierarchy inference re-submits derived triples on restart
- **WHEN** those triples are suppressed
- **THEN** the suppressed-duplicate counter rises with a label identifying that lane

### Requirement: Duplicate suppression preserves required Graphable side effects

Suppression MUST cover the KV write only. The applied-sequence redelivery guard remains the sole
gate on whether a redelivered message is re-applied, and required Graphable-lane index and hierarchy
side effects MUST still run when their lane runs, so a crash mid-apply is re-driven on redelivery
rather than being masked by a suppression that looks like a completed write.

#### Scenario: a redelivered message still re-drives its post-commit work

- **GIVEN** a message whose triples are all already stored
- **WHEN** it is redelivered and its triples are suppressed
- **THEN** the lane's post-commit side effects still execute

### Requirement: Stored duplicate triples MUST remain readable and MUST NOT be removed

Entities that already accumulated duplicate triples before this contract MUST continue to read
and serve normally. No backfill, sweep, or migration removes them, and the stored-state
structural contract MUST NOT gain a duplicate-rejection rule — such a rule would reclassify every
already-affected entity as invalid, converting a cosmetic accumulation into a service outage.

#### Scenario: a pre-existing duplicate does not poison its entity

- **GIVEN** an entity whose stored state carries two identical triples written before this
  contract
- **WHEN** the entity is read
- **THEN** it serves normally with both triples present

#### Scenario: a new add against a duplicated entity still suppresses

- **GIVEN** an entity carrying two identical copies of a tuple
- **WHEN** that tuple is submitted again
- **THEN** nothing is appended and the revision is unchanged

### Requirement: Graph-ingest holds no steady-state self-watch on ENTITY_STATES

After the boot snapshot sweep completes, graph-ingest MUST hold no live watcher on ENTITY_STATES:
the sweep validates the full resident snapshot synchronously during Start, stops its watcher, and
then MUST continue consuming the watcher's update channel until it closes, discarding
post-marker entries — the deliberate stop and its channel closure MUST NOT be classified as
watch loss or transport failure. A genuine transport failure during the snapshot drain keeps the
existing recovery contract: ingest boots, entity queries stay not-ready, and no poison is
recorded from the failure itself.

#### Scenario: a steady-state write is not re-delivered to its writer

- **GIVEN** graph-ingest completed Start with a successful snapshot sweep
- **WHEN** an entity write commits to ENTITY_STATES
- **THEN** no graph-ingest-owned watcher receives that write
- **AND** the ENTITY_STATES stream carries no graph-ingest guard consumer at steady state

#### Scenario: deliberate stop drains to channel close without misclassification

- **GIVEN** the snapshot sweep reached the end-of-snapshot marker while concurrent writers keep
  publishing
- **WHEN** graph-ingest stops the sweep watcher
- **THEN** the update channel is consumed until it closes and pending entries are discarded
- **AND** entity queries are ready and Health is not degraded by the stopped watcher

#### Scenario: snapshot transport failure keeps the boot recovery contract

- **GIVEN** the snapshot drain fails with a transport error before completing
- **WHEN** Start continues
- **THEN** ingest writers boot and operate
- **AND** entity queries return the transient not-ready classification
- **AND** no poison is recorded from the transport failure

### Requirement: The boot snapshot sweep validates every resident entity, last revision wins

The boot snapshot sweep MUST validate every resident ENTITY_STATES value with the canonical
decoder before steady-state operation, recording poisoned entities in the poison inventory
(structured ERROR + metric) instead of failing startup, and MUST resolve multiple deliveries of
the same key during the drain to the last-delivered revision — a key whose poisoned revision is
superseded by a valid pre-marker revision ends with no inventory entry. Snapshot completeness
assumes ENTITY_STATES keeps history depth 1; raising history invalidates this contract.

#### Scenario: resident poison from before this boot is inventoried

- **GIVEN** an ENTITY_STATES value that fails the canonical decode is resident at boot
- **WHEN** the snapshot sweep processes it
- **THEN** the entity is recorded in the poison inventory with its bounded reason and revision
- **AND** a structured ERROR names the entity once
- **AND** ingest still boots

#### Scenario: a key repaired mid-drain is not inventoried

- **GIVEN** the drain delivers a poisoned revision of entity A and later a valid revision of A
  before the end-of-snapshot marker
- **WHEN** the sweep completes
- **THEN** A has no poison inventory entry

### Requirement: Poison refusal is scoped to the poisoned entity

A poisoned entity MUST refuse per-entity across every lane: reads of the poisoned entity return
the typed `graph_state_reset_required` classification with its bounded reason, mutations whose
resident read or RMW cycle encounters the poison fail with the same typed fatal classification
(never a retryable or caller-blaming class), and reads, ingest, and mutations of every other
entity proceed. On poison detection the entity's query-cache entry MUST be invalidated so cached
responses cannot outlive detection.

#### Scenario: one poisoned entity does not take down the query surface

- **GIVEN** entity A's resident state fails the canonical decode and entity B's is valid
- **WHEN** a caller queries A and then queries B
- **THEN** the read of A fails with `graph_state_reset_required`
- **AND** the read of B returns B's state

#### Scenario: ingest of healthy entities continues during a poison incident

- **GIVEN** entity A is poisoned
- **WHEN** a Graphable arrival for entity B is processed
- **THEN** B's merge commits normally

#### Scenario: mutation read seams return the typed classification

- **GIVEN** entity A's resident state is poisoned
- **WHEN** a caller issues `entity.create`, `entity.reconcile`, `triple.append`, or `entity.delete`
  against A
- **THEN** the reply carries the fatal `graph_state_reset_required` classification
- **AND** no reply invites the caller to retry the same request

#### Scenario: detection invalidates the cached entry

- **GIVEN** entity A's state is cached by the query cache and A's stored bytes are poisoned
  out-of-band
- **WHEN** any lane detects A's poison and records it in the inventory
- **THEN** A's query-cache entry is invalidated in the same detection

#### Scenario: suffix resolution does not serve entity state

- **GIVEN** entity A is poisoned
- **WHEN** a suffix query resolves A's ID
- **THEN** the resolution may return the ID without decoding A's bytes
- **AND** any subsequent read of A's state fails with the typed classification

### Requirement: An aggregate read encountering poison fails naming every poisoned entity

A multi-entity read that encounters poisoned entities MUST fail as a whole with the typed
`graph_state_reset_required` error identifying every poisoned entity encountered in that attempt
as a bounded list, MUST record all of them in the poison inventory in that same attempt, and
MUST NOT silently omit any entity from a successful response.

#### Scenario: batch fetch fails loudly and names all poisoned entities

- **GIVEN** a batch read spanning entities A and C (both poisoned) and B (valid)
- **WHEN** the aggregate read executes
- **THEN** the whole read fails with `graph_state_reset_required` identifying both A and C
- **AND** A and C are both recorded in the poison inventory by that single attempt
- **AND** no response is returned that contains B but silently omits A or C

### Requirement: The poison inventory is observability-only, revision-stamped, and self-healing

The per-entity poison inventory MUST NOT gate any read or write decision — refusal derives
solely from decoding the bytes actually stored — and each entry MUST carry the KV revision
whose decode failed. An entry MUST clear when the entity is deleted, when a write successfully
commits a newer revision to its key, or when any read of the key successfully validates its
current bytes, so Health and metrics recover without a process restart in both in-band and
out-of-band repair directions. Steady-state cost with an empty inventory MUST be a single
atomic check on the commit path.

#### Scenario: inventory drives Health, gauge, and enumeration while poison is present

- **GIVEN** the poison inventory is non-empty
- **WHEN** Health is reported
- **THEN** the component is unhealthy with status `degraded`, the poisoned-entity count, and a
  bounded sample of IDs
- **AND** the poisoned-entities gauge equals the inventory size
- **AND** the full inventory is enumerable through the component debug surface

#### Scenario: operator repair recovers Health without restart

- **GIVEN** entity A is the only inventoried poisoned entity
- **WHEN** an operator deletes A through the canonical `graph.mutation.entity.delete` verb
- **THEN** A's inventory entry clears and the gauge reads zero
- **AND** Health recovers
- **AND** a subsequent canonical create of A serves normally

#### Scenario: an out-of-band repair clears on the next successful read

- **GIVEN** entity A is inventoried and A's stored bytes are subsequently replaced with valid
  bytes outside the mutation API
- **WHEN** any read of A validates its current bytes
- **THEN** A's inventory entry clears without a restart

#### Scenario: a concurrent repair commit is not erased by a stale record

- **GIVEN** a mutation lane classifies A's resident poison at revision R while another lane
  commits valid bytes to A at revision R+1
- **WHEN** both the record and the clear complete in either order
- **THEN** the inventory ends with no entry for A

#### Scenario: re-poisoned entity is re-inventoried and re-logged

- **GIVEN** entity A was inventoried, repaired, and cleared
- **WHEN** A's stored bytes fail the canonical decode again on any detection path
- **THEN** A is re-recorded in the inventory
- **AND** a structured ERROR names A again

#### Scenario: a stale inventory entry cannot refuse a repaired entity

- **GIVEN** entity A's stored bytes were repaired
- **WHEN** a caller reads A before any inventory bookkeeping runs
- **THEN** the read validates A's current bytes and serves

### Requirement: Resident-poison arrivals are redelivered, not destroyed

An ingest arrival that fails because the target entity's RESIDENT state is poisoned MUST be
negatively acknowledged for redelivery (bounded by the consumer's delivery cap) so valid data
survives the repair window, while an arrival whose own candidate is structurally invalid remains
terminally rejected.

#### Scenario: valid arrival survives a poison window

- **GIVEN** entity A's resident state is poisoned and a valid Graphable arrival for A is
  delivered
- **WHEN** the ingest lane classifies the resident poison
- **THEN** the message is negatively acknowledged, not terminated
- **AND** after an operator repairs A, a redelivery of the same message applies successfully

#### Scenario: structurally invalid candidate is still terminal

- **GIVEN** an arrival whose own projection fails the structural contract
- **WHEN** the ingest lane rejects it
- **THEN** the message is terminated and never redelivered

### Requirement: Every ENTITY_STATES commit validates the complete final candidate

graph-ingest MUST apply the canonical predicate contract at one authoritative persistence seam used by every
ENTITY_STATES create, reconcile, append, delete, CAS, Graphable, hierarchy, and rule path.
Validation MUST inspect the complete candidate after normalization, merging, routing, and
framework triple injection, and before any state or required projection side effect commits.

Handler-level validation MAY return earlier classified errors but MUST NOT be the correctness boundary.

#### Scenario: a malformed foreign triple cannot bypass Graphable validation

- **GIVEN** one Graphable arrival containing valid own triples and an invalid foreign-subject predicate
- **WHEN** normalization and foreign routing construct their final candidates
- **THEN** the invalid foreign candidate reaches the same authoritative structural gate
- **AND** graph-ingest commits neither malformed state nor a partial derived projection

#### Scenario: a canonical RPC mutation cannot bypass the gate

- **WHEN** a create, reconcile, or append handler constructs an invalid final candidate
- **THEN** the final persistence seam returns the canonical typed structural rejection

### Requirement: Replacement validates before destructive mutation

An operation that replaces a predicate value MUST validate its intended complete final candidate before
removing the existing fact. If validation or persistence of the replacement fails, the prior authoritative
value MUST remain unchanged.

#### Scenario: a rejected replacement does not lose the old value

- **GIVEN** an entity carrying valid state and an update request that would produce an invalid final candidate
- **WHEN** replacement validation fails
- **THEN** the original triple remains in ENTITY_STATES
- **AND** no remove-then-fail partial update is visible

### Requirement: The mutation API is a declared typed component port

Graph-ingest MUST declare one required input `nats-request` port with interface `semstreams.graph.mutation` version
`v1` and family `graph.mutation.>`. Handler setup MUST resolve the four admitted leaves from that declaration and MUST
NOT subscribe through hidden constants or fallback subjects. A validated flow MUST contain exactly one compatible
provider input and MAY contain many compatible requester outputs.

#### Scenario: An undeclared mutation side channel cannot boot

- **GIVEN** graph-ingest has no compatible mutation provider input port
- **WHEN** the flow is validated
- **THEN** validation fails before mutation subscriptions are installed
- **AND** graph-ingest does not fall back to hardcoded subjects

### Requirement: Authority writes share one atomic Create and CAS discipline

A genuine `ENTITY_STATES` birth MUST use atomic KV `Create`. Every write to an existing key, whether Graphable merge,
RPC mutation, or hierarchy inverse, MUST commit by CAS against state read at a specific revision. No production path
MAY retain unconditional Put-as-upsert semantics. The keyed ingest pool MAY reduce local contention but MUST NOT be a
correctness precondition or coordinate RPC handlers.

#### Scenario: RPC reconcile survives a racing Graphable merge

- **GIVEN** ingest and reconcile both read entity A at revision R
- **WHEN** reconcile commits R to R+1 before ingest attempts its write
- **THEN** ingest cannot overwrite R+1 from its stale candidate
- **AND** it re-evaluates its retry-safe merge against R+1 or returns a classified failure

### Requirement: Four explicit mutation operations define the wire surface

The admitted operations MUST be `entity.create`, `entity.reconcile`, `triple.append`, and `entity.delete`. Create MUST
strictly birth one absent entity. Reconcile MUST require a nonzero expected revision and replace the complete desired
set for named predicates. Append MUST deduplicate canonical exact tuples and report one result per subject, with no
cross-subject transaction. Delete MUST require a nonzero expected revision and conditionally delete one entity. Every
absent non-create target MUST return typed `entity_not_found`.

#### Scenario: A stale reconcile does not silently retry

- **GIVEN** a reconcile request names revision R and the entity is now at R+1
- **WHEN** graph-ingest evaluates the request
- **THEN** it returns typed `revision_mismatch`
- **AND** it does not overwrite or retry automatically

#### Scenario: Append uses an explicit discriminated result

- **GIVEN** one append request targets existing A and absent B
- **WHEN** graph-ingest evaluates both subjects
- **THEN** A reports `applied` or `unchanged` with its KV revision
- **AND** B reports `entity_not_found` without a revision or fabricated error payload

### Requirement: Exact authority read returns value and same-entry revision

The exact entity read MUST return one validated entity and the nonzero KV revision from the same KV entry. Absence MUST
return typed `entity_not_found`; poison MUST retain the graph-state classification. Logical `EntityState.Version` MUST
NOT be accepted as a KV revision. The read MUST NOT mutate, repair, create a stub, or change `GRAPH_STATUS`.

#### Scenario: A reconciler receives usable CAS evidence

- **GIVEN** entity A is resident and valid
- **WHEN** the exact read succeeds
- **THEN** its entity and revision come from one KV entry
- **AND** the returned revision is nonzero

### Requirement: Relationship target absence creates no entity

Mutation MUST validate relationship syntax without requiring the object entity to exist. Graph-ingest MUST NOT create
a target stub, pending record, inverse repair, or delayed drain because an object is absent. A later real birth makes
future dereference resolve; no source-edge replay is required.

#### Scenario: A relationship may precede its object

- **GIVEN** entity A contains a valid relationship to absent entity B
- **WHEN** A commits
- **THEN** A's edge remains current authority
- **AND** no `ENTITY_STATES` key for B is created

### Requirement: Hierarchy inference is Graphable-lane-only and uses Create/CAS

Opt-in hierarchy inference MUST run only for Graphable ingest. RPC create MUST produce no hierarchy side effects.
Hierarchy containers are real inferred entities; their birth MUST use atomic `Create`, and container or sibling inverse
edges MUST update must-exist targets through CAS. A failed companion write MAY leave a dangling relationship, which is
valid eventual graph state and MUST NOT trigger rollback or repair machinery.

#### Scenario: RPC create does not manufacture hierarchy

- **GIVEN** hierarchy is enabled
- **WHEN** a caller creates an entity through request/reply
- **THEN** only the caller-supplied entity birth commits
- **AND** no container or inverse hierarchy write is attempted

### Requirement: Mutation outcomes are bounded and honest about lost replies

Server replies MUST classify applied, unchanged, not-found, exists, revision-mismatch, and invalid outcomes. The typed
client MUST classify no responder as `unavailable` and a context already done before send as `deadline`; both prove
non-delivery. It MUST classify a post-send timeout or disconnect, malformed reply, or semantically invalid success
reply as `commit_unknown`. `graph_mutation_outcomes_total{operation,outcome}` is the bounded command counter. Entity ID
MUST NOT be a metric label.

#### Scenario: An ambiguous reply is not automatically retried

- **GIVEN** a request was sent and may have reached graph-ingest
- **WHEN** its reply times out, disconnects, or cannot be validated
- **THEN** it returns `commit_unknown`
- **AND** it does not automatically retry or claim exactly-once behavior
