## MODIFIED Requirements

### Requirement: Deduplication identity is content-addressed at the embedded bytes

The system MUST derive an embedding's deduplication key from the exact text that
is embedded — the resolved and truncated body — folded with the embedder identity
(type, model, dimensions) and the effective text cap, so a deduplication hit
returns a stored vector only when the current embedder identity and the resolved
content both match. This holds for offloaded content (resolved from ObjectStore
via a storage reference) identically to inline content; the key is never derived
from a storage address. Over-cap content MUST be truncated by a single rune-safe
routine shared by both lanes, so byte-identical content yields byte-identical
embedded bytes — and therefore the same key — regardless of which lane delivered
it.

#### Scenario: offloaded content deduplicates on identical bytes

- **GIVEN** an entity whose text is offloaded and resolved from ObjectStore
- **WHEN** a second entity resolves to byte-identical content under the same embedder identity
- **THEN** the second entity's vector is served from the deduplication store without regeneration

#### Scenario: over-cap content deduplicates across lanes

- **GIVEN** byte-identical over-cap content delivered inline to one entity and via
  a storage reference to another, under the same embedder identity
- **WHEN** both are embedded
- **THEN** both derive the same deduplication key and the second is served from the
  deduplication store without regeneration

#### Scenario: overwriting a stable storage key regenerates

- **GIVEN** an entity whose content lives at a stable ObjectStore key that already has a stored vector
- **WHEN** the body at that key is overwritten with different content
- **THEN** the deduplication key changes and a fresh vector is generated, never the previous body's vector

#### Scenario: embedder identity change does not deduplicate

- **GIVEN** a stored deduplication record produced by one embedder identity
- **WHEN** an entity is embedded under a different embedder type, model, or dimensions
- **THEN** the stored vector is not returned and the entity is regenerated under the current identity

#### Scenario: skipped deduplication is observable

- **WHEN** an entity is embedded on a lane or condition where deduplication is not consulted
- **THEN** a skip is counted so the avoided-reuse cost is visible rather than inferred

## ADDED Requirements

### Requirement: Embedding failures are reason-classified and recover on re-delivery
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
last failure is repaired.

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

### Requirement: Concurrent byte-identical content collapses to one embedder call
The producer SHALL make at most one embedder generation call for byte-identical
content processed concurrently under the same embedder identity, sharing the
result rather than each worker calling the paid embedder independently. This
guarantee is process-local; cross-process collapse is not required (the
deduplication key already collapses sequential cross-process duplicates).

#### Scenario: A burst of identical content makes one embedder call
- **GIVEN** K workers each holding byte-identical content under the same embedder
  identity, arriving in the same window
- **WHEN** they are embedded concurrently
- **THEN** exactly one embedder generation call is made and all K entities are
  stored (each with its own record), never K generation calls
