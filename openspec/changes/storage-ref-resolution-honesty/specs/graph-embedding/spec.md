## MODIFIED Requirements

### Requirement: Embedding failures are reason-classified and recover on re-delivery or repair

A failed embedding SHALL be recorded with a bounded reason classification
alongside its raw error message, and SHALL count toward the producer's
`FailedCount` so the producer reports `degraded` — never `ready` — while any
embedding remains failed (see graph-index-readiness). A failed record advances
the low-water watermark (so a permanently-failing or no-text entity never stalls
readiness) but is never treated as a usable vector or served from the store. A
failed record SHALL be re-processed when its entity is re-delivered (on restart
via last-per-subject re-delivery, or on a new revision), so a transient dependency
outage recovers without operator action once the dependency returns; the current
failed count SHALL decrease as failures resolve, so `degraded` clears when the
last failure is repaired. Derived-write failures — a failed derived-record
delete, a failed pending write, a failed authoritative source read — SHALL enter
the same failed accounting with their own bounded reasons, so they surface as
`degraded` immediately, and SHALL additionally recover via the background repair
loop (not only on re-delivery). These derived-write reasons are process-local
accounting only: they are never persisted into a stored embedding record.

An entity whose offloaded body resides in a storage instance this producer cannot
resolve SHALL NOT be recorded as a failed embedding. Re-delivery cannot repair a
deployment wiring gap, so counting it as failed would convert a configuration fact
into a permanent health verdict with no exit. It SHALL instead be reported through
the excluded-content path with its own metric, and the entity SHALL still reach a
terminal outcome from whatever inline text it carries. The producer SHALL consult
the shared store registry for the instance the reference names, and SHALL fall back
to a store it owns only when that store is the instance named. Resolution SHALL be
re-checked at fetch time rather than assumed from the earlier gate, because store
registration and deregistration are live during operation.

#### Scenario: A failed embedding keeps the producer degraded
- **GIVEN** the embedding dependency is down at cold start and every entity's
  embedding reaches a failed terminal outcome
- **WHEN** readiness is computed
- **THEN** the producer reports `State = degraded` with `FailedCount` equal to the
  failed entities, never `State = ready`

#### Scenario: Recovery on re-delivery clears degraded
- **GIVEN** entities in a failed embedding state and the dependency has recovered
- **WHEN** the entities are re-delivered (restart or a new revision)
- **THEN** they are re-embedded to a terminal generated outcome, `FailedCount`
  drops to zero, and `State` returns to `ready`

#### Scenario: A failure carries a bounded reason
- **WHEN** an embedding fails
- **THEN** the record carries a reason from the fixed classification enum next to
  the raw error message, and the failures metric increments under that reason

#### Scenario: A failed derived-record delete degrades readiness without pinning the watermark
- **GIVEN** a tombstoned entity whose derived-record delete fails
- **WHEN** readiness is computed before the repair loop has converged the entity
- **THEN** the producer reports `degraded` with the delete failure counted and
  reason-classified, while the readiness watermark has still drained at the
  tombstone's revision (a failed delete never wedges readiness)

#### Scenario: Derived-write reasons never persist into stored records
- **WHEN** a derived-write failure is recorded
- **THEN** its reason exists only in process-local failed accounting — no stored
  embedding record ever carries a derived-write reason

#### Scenario: An unresolvable storage instance excludes rather than fails
- **GIVEN** an entity whose reference names a storage instance that is not in the
  shared registry and is not the instance of any store this producer owns
- **WHEN** its embedding is processed
- **THEN** it is reported through the excluded-content path with its metric
- **AND** it does not enter `FailedCount`, so the producer's readiness is not
  degraded by a deployment wiring gap
- **AND** re-delivering the entity produces the same outcome rather than
  accumulating repeated failures

#### Scenario: A resolved store's read failure still fails and still recovers
- **GIVEN** an entity whose reference names an instance the producer CAN resolve
- **WHEN** reading the referenced content fails
- **THEN** it is recorded as a reason-classified failed embedding that counts
  toward `FailedCount`
- **AND** it is re-processed on re-delivery, so a transient outage recovers
  without operator action

#### Scenario: The owned-store fallback does not answer for a foreign instance
- **GIVEN** a producer that owns a content store for one instance
- **WHEN** it processes an entity whose reference names a different instance that
  the registry cannot resolve
- **THEN** it does not read the referenced key from the store it owns
- **AND** it does not report content as resolved from a store that never held it

#### Scenario: A store deregistered between gate and fetch does not latch a failure
- **GIVEN** an entity whose instance resolves when the producer decides to fetch
- **WHEN** that store is deregistered before the content is read
- **THEN** the outcome is the excluded-content path, not a durable failed record
