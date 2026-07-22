## ADDED Requirements

### Requirement: Deduplication identity is content-addressed at the embedded bytes

The system MUST derive an embedding's deduplication key from the exact text that
is embedded — the resolved and truncated body — folded with the embedder identity
(type, model, dimensions) and the effective text cap, so a deduplication hit
returns a stored vector only when the current embedder identity and the resolved
content both match. This holds for offloaded content (resolved from ObjectStore
via a storage reference) identically to inline content; the key is never derived
from a storage address.

#### Scenario: offloaded content deduplicates on identical bytes

- **GIVEN** an entity whose text is offloaded and resolved from ObjectStore
- **WHEN** a second entity resolves to byte-identical content under the same embedder identity
- **THEN** the second entity's vector is served from the deduplication store without regeneration

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

### Requirement: Embedding text cap is an observable, identity-bearing contract

The embedding text cap MUST be operator-configurable, truncation MUST be
observable rather than silent, and the effective cap MUST participate in a
vector's deduplication identity so that changing the cap cannot serve a vector
built from a different byte range.

#### Scenario: operator sets the cap

- **GIVEN** an operator-supplied embedding text cap
- **WHEN** an entity's text exceeds the cap
- **THEN** the text is truncated at a word boundary to the configured cap before embedding

#### Scenario: truncation emits a signal

- **WHEN** an entity's text is truncated to the cap
- **THEN** a truncation signal is emitted so the bytes actually embedded are discoverable

#### Scenario: a cap change changes identity

- **GIVEN** a stored vector generated at one cap
- **WHEN** the cap changes such that the embedded byte range differs
- **THEN** the deduplication key differs and no vector built from the old byte range is served

### Requirement: Only the newest source revision's vector persists, consistently

For a single entity, the embedding index MUST retain only the newest source
revision's vector regardless of the order in which concurrent generations
complete, and a stored record's content hash and vector MUST always originate from
the same generation. Writes MUST use revision compare-and-set so no committed
vector is silently overwritten by a stale read.

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
