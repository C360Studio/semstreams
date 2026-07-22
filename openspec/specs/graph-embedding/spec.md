# graph-embedding Specification

## Purpose

The `graph.embedding.query.search` semantic-search RPC and its similarity scan
(`processor/graph-embedding`, `graph/embedding`). Seeded lazily by gh#463 (ADR-071);
other graph-embedding behavior is added to this spec when a change first touches it.
## Requirements
### Requirement: Semantic search MAY be scoped to entity-ID prefixes, applied at the source

The `graph.embedding.query.search` request (`SearchRequest`) MUST support an optional
`Scope` — a list of entity-ID prefixes. When non-empty, the search MUST return only
candidates whose entity ID matches at least one prefix (via the shared
`graph.MatchesAnyIDPrefix` matcher), and MUST apply that filter **at the candidate
source in every similarity path** — both the warm in-memory cache path
(`FindSimilarFromCache`) and the cold KV-scan fallback — filtering **before** the
expensive per-candidate operation (cosine similarity / the embedding KV fetch). An
empty/absent `Scope` MUST behave exactly as today. The request MUST decode with
unknown-field tolerance, so a producer sending `Scope` to an un-migrated server
degrades gracefully to an unscoped search rather than erroring.

Applying the filter to only one similarity path (e.g. the cold fallback) is
non-conformant: the warm cache is the steady-state path, so a cache-path omission
makes the scope a silent no-op in production.

#### Scenario: a scoped search returns only in-scope candidates (warm and cold)

- **GIVEN** an index holding entities under two ID prefixes
- **WHEN** `graph.embedding.query.search` runs with `Scope` naming one prefix
- **THEN** only entities under that prefix are returned
- **AND** this holds whether the similarity is served from the warm cache or the cold
      KV scan

#### Scenario: an unscoped search is unchanged

- **GIVEN** a `SearchRequest` with an empty/absent `Scope`
- **WHEN** it runs
- **THEN** results are identical to the pre-scope behavior

#### Scenario: an un-migrated server ignores an unknown scope

- **GIVEN** a server that predates the `Scope` field
- **WHEN** it receives a `SearchRequest` carrying `Scope`
- **THEN** it decodes successfully and runs an unscoped search (graceful degrade)

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

### Requirement: Offloaded entities embed their inline identity text alongside the body

The system MUST embed an offloaded entity's inline identity text — the triples
selected by the configured text suffixes — together with its resolved body,
identity-first, in a single vector, so the text-suffix configuration takes effect
on offloaded entities exactly as it does on inline ones. An offloaded entity is one
whose body is resolved from a storage reference; today only its body is embedded and
its identity triples (title, signature, comment, and the like) are silently
excluded, which also makes the text-suffix configuration inert for it.

The combined text is subject to the same embedding-text cap as any other lane;
because identity is placed first, truncation trims the body and the identity always
survives. The deduplication key is derived over the combined, truncated bytes (the
exact text embedded), so a change to either the identity or the body regenerates the
vector. An offloaded entity that carries no inline identity text embeds its body
alone, unchanged; symmetrically, an offloaded entity whose resolved body is empty
embeds its identity text alone (no trailing separator), so it deduplicates against
an inline entity carrying the same text.

#### Scenario: an offloaded entity embeds identity text ahead of its body

- **GIVEN** an offloaded entity carrying inline identity triples selected by the text suffixes
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the identity text followed by the resolved body, in that order

#### Scenario: a text-suffix setting takes effect on offloaded entities

- **GIVEN** a text-suffix configured to include an identifying predicate (e.g. a code signature)
- **AND** an offloaded entity carrying that predicate and an offloaded body
- **WHEN** the entity is embedded
- **THEN** the predicate's text is present in the embedded text rather than excluded

#### Scenario: identity survives the cap ahead of the body

- **GIVEN** an offloaded entity whose identity-plus-body text exceeds the embedding-text cap
- **WHEN** the combined text is truncated at the cap
- **THEN** the identity text is retained and the body is trimmed from the end

#### Scenario: the deduplication key covers the combined text

- **GIVEN** an offloaded entity embedded from its identity text and body
- **WHEN** either the identity text or the body changes
- **THEN** the deduplication key changes and the vector is regenerated, never served from the prior bytes

#### Scenario: an offloaded entity with no inline identity text is unchanged

- **GIVEN** an offloaded entity carrying no inline text-suffix triples
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the resolved body alone

#### Scenario: an offloaded entity with an empty body embeds its identity alone

- **GIVEN** an offloaded entity whose resolved body is empty and which carries inline identity text
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the identity text alone, with no trailing separator
- **AND** it deduplicates against an inline entity whose text is that same identity

#### Scenario: identity inclusion on the offloaded lane is observable

- **WHEN** an offloaded entity is embedded
- **THEN** whether inline identity text was included alongside its body is reported, so a producer can confirm the text-suffix configuration took effect rather than infer it from silence

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

