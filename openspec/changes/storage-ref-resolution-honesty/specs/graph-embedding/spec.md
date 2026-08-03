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

The bounded reason classification SHALL be a qualifier of the TERMINAL STATE, valid
on any status, not a failure-only field. An empty reason means the terminal state is
exactly what the status says; a non-empty reason on a successful embedding means that
success is qualified. Every consumer of the reason SHALL determine the status first,
so a qualified success is never collected as a failure, never enters `FailedCount`,
and never labels the failures metric.

An entity whose offloaded body resides in a storage instance this producer cannot
resolve SHALL NOT be recorded as a failed embedding. Re-delivery cannot repair a
deployment wiring gap, so counting it as failed would convert a configuration fact
into a permanent health verdict with no exit. It SHALL instead be reported through
the excluded-content path with its own metric, and the entity SHALL still reach a
terminal outcome from whatever inline text it carries. WHEN the entity carries inline
text, that outcome SHALL be recorded as a QUALIFIED SUCCESS — a stored, servable vector
carrying the bounded qualifier for an unreachable body — so those entities are
enumerable from the index. An entity with NO inline text has nothing to embed and
SHALL reach the ordinary no-text terminal, which stores no record at all; it is
therefore counted by the exclusion metric but NOT enumerable from the index, and only
the metric reports it. A re-queue SHALL be skipped only when the stored vector is
strictly newer, or is at the same revision and already carries the same qualifier the
re-queue would produce — so any entity whose outcome would change, because its body
became reachable or because a failure erased its qualifier and left it looking
complete, re-embeds on its next delivery and recovers without operator intervention. The producer SHALL consult the shared store registry for the
instance the reference names, and SHALL fall back to a store it owns only when that
store is the instance named. Resolution SHALL be re-checked at fetch time rather than
assumed from the earlier gate, because store registration and deregistration are live
during operation.

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

#### Scenario: An unresolvable storage instance is a qualified success, not a failure
- **GIVEN** an entity whose reference names a storage instance that is not in the
  shared registry and is not the instance of any store this producer owns
- **WHEN** its embedding is processed
- **THEN** it is reported through the excluded-content path with its metric
- **AND** it does not enter `FailedCount`, so the producer's readiness is not
  degraded by a deployment wiring gap
- **AND** the stored record is a successful embedding of the entity's inline text
  carrying the bounded qualifier for an unreachable body, so the entity is
  enumerable from the index rather than indistinguishable from a complete one
- **AND** re-delivering the entity produces the same outcome rather than
  accumulating repeated failures

#### Scenario: An unreachable body with no inline text leaves no record
- **GIVEN** an entity whose reference names a storage instance this producer cannot
  resolve, and which carries no inline text to embed
- **WHEN** its embedding is processed
- **THEN** it reaches the ordinary no-text terminal and no embedding record is stored,
  so it is NOT enumerable from the index and the exclusion metric is the only report
  of it
- **AND** it is still not counted as a failure, so readiness is not degraded

#### Scenario: A qualified success never enters failure accounting
- **GIVEN** a stored embedding that is successful but carries the unreachable-body
  qualifier
- **WHEN** the producer seeds its current-failed state from the index at startup
- **THEN** the qualified success is not collected as a failure
- **AND** it contributes nothing to `FailedCount` or the failures metric, so the
  producer does not report degraded because of it

#### Scenario: Wiring the missing store heals the qualified entity
- **GIVEN** an entity whose stored embedding carries the unreachable-body qualifier
- **WHEN** the storage instance becomes resolvable and the entity is re-delivered at
  the same revision, with no change to its authoritative state
- **THEN** the re-queue is not skipped over the qualified vector
- **AND** the entity re-embeds with its body and the qualifier clears, so the
  recovery needs no operator action beyond fixing the wiring

#### Scenario: A complete vector is still protected from a stale re-queue
- **GIVEN** an entity whose stored embedding is successful and unqualified
- **WHEN** a re-queue that would also produce a complete embedding arrives at the same
  or an older source revision
- **THEN** it is skipped, so a stale re-drive cannot downgrade a complete vector to
  pending

#### Scenario: A failure that erases the qualifier does not freeze the entity
- **GIVEN** an entity queued as unreachable-bodied whose embedding then fails for an
  unrelated reason, so the failure's own classification replaces the qualifier
- **WHEN** the failed record is re-processed after a restart and succeeds from the
  entity's inline text, leaving a stored embedding that appears complete
- **THEN** the next re-queue for that entity is NOT skipped, because the outcome it
  would produce differs from what is stored
- **AND** the entity is re-recorded as a qualified success, so it becomes enumerable
  again rather than remaining indistinguishable from a complete embedding

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
