# graph-embedding — delta for embedding-derived-state-convergence

## ADDED Requirements

### Requirement: Derived embedding state converges through a single-writer seam with background repair

Every hop-1 mutation of the derived embedding record MUST be issued through one serialized seam — a
watcher update, a watcher tombstone delete, a debounced (coalesced) flush, and a repair re-drive alike
— and MUST converge on the authoritative `ENTITY_STATES` value read at execution time: absence
deletes the derived record; authoritative presence re-queues through the sole hop-1 record writer.
Debounced processing MUST NOT change the outcome relative to immediate processing — a stale queued
flush cannot clobber a newer write or resurrect a deleted entity. A failed derived write or delete
MUST be re-driven by a background repair loop until it converges, rather than only on the next
incidental re-delivery; the repair set is scoped to derived-write/read failures (embedder-side failure
reasons are excluded, so no permanently-failing content can enter the repair lane). Hop-2 vector
persistence does not take the seam — its revision compare-and-set guards are sufficient and
serializing workers behind hop-1 metadata operations is forbidden.

#### Scenario: a debounced re-queue cannot resurrect a tombstoned entity

- **GIVEN** coalescing enabled and a batch flush that has read an entity's authoritative state at
  revision N
- **WHEN** the entity's tombstone (revision N+1) is processed concurrently with the flush
- **THEN** the seam serializes the two, the derived record converges to absent, and the entity does
  not regain a vector

#### Scenario: authoritative absence deletes the derived record on the coalesced lane

- **GIVEN** an entity queued into the coalescing window and then tombstoned before the flush executes
- **WHEN** the flush's reconcile reads `ENTITY_STATES` and finds the key absent or deleted
- **THEN** the derived embedding record is deleted (not silently skipped) and the watermark drains

#### Scenario: a failed delete is repaired until the key is absent

- **GIVEN** a tombstone whose derived-record delete fails transiently
- **WHEN** the background repair loop re-drives the entity after the failure
- **THEN** the delete is retried until the derived key is absent, and the readiness watermark is
  unchanged by the repair

## RENAMED Requirements

- FROM: `### Requirement: Embedding failures are reason-classified and recover on re-delivery`
- TO: `### Requirement: Embedding failures are reason-classified and recover on re-delivery or repair`

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

### Requirement: Only the newest source revision's vector persists, consistently

For a single entity, the embedding index MUST retain only the newest source
revision's vector regardless of the order in which concurrent generations
complete, and a stored record's content hash and vector MUST always originate from
the same generation. Writes MUST use revision compare-and-set so no committed
vector is silently overwritten by a stale read. The hop-1 create lane is covered
by the single-writer seam: a pending record is only ever created from the
authoritative `ENTITY_STATES` value read at execution time under the seam, so a
create cannot carry a source revision below one a delete has already acted on.

#### Scenario: a newer revision wins out-of-order completion

- **GIVEN** two source revisions N and N+1 of one entity generating concurrently
- **WHEN** revision N's generation completes after revision N+1's
- **THEN** the index retains revision N+1's vector and discards the late revision N write

#### Scenario: concurrent commits do not lose updates

- **GIVEN** two workers committing embeddings for the same entity concurrently
- **WHEN** both read the current record and attempt to write
- **THEN** the write uses revision compare-and-set and a conflicting writer re-reads rather than clobbering a committed vector

#### Scenario: content hash and vector never desynchronize

- **WHEN** a generated record is persisted
- **THEN** its content hash and vector are both taken from the same generation, never mixed across revisions

#### Scenario: a vanished record is not resurrected

- **GIVEN** an entity whose pending record was removed (tombstoned) while its vector was being generated
- **WHEN** the generated vector is ready to save
- **THEN** the write is dropped rather than recreating a vector for a deleted entity

#### Scenario: the create lane cannot resurrect a deleted entity

- **GIVEN** an entity deleted from `ENTITY_STATES` while a stale flush or repair for it is queued
- **WHEN** the queued work executes through the single-writer seam
- **THEN** the reconcile observes authoritative absence and no pending record is created
