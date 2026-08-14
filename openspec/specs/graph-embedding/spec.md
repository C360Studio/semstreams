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

### Requirement: The similarity query classifies not-ready distinctly from a genuine empty result

The `graph.embedding.query.similar` handler MUST return the classified transient `ErrorCodeIndexNotReady`
when the embedding index's bootstrap/health gate has not cleared (the initial ENTITY_STATES bootstrap is
still validating, or its watcher is unavailable), and MUST NOT return that transient — or any error — for an
entity that is simply found to have no close neighbors above the caller's threshold. A caller MUST be able to
distinguish "could not ask" (the transient) from "asked, got nothing" (an ordinary empty result) without
matching on error message text.

This is seeded now, verified against existing code, because a second consumer — community detection's
semantic-edge synthesis (`graph-clustering`), not only anomaly detection — depends on the distinction to
implement its embedding-readiness structural-floor fallback correctly.

#### Scenario: A query during embedding bootstrap returns the classified transient

- **GIVEN** the embedding index's initial bootstrap has not yet completed
- **WHEN** `graph.embedding.query.similar` is queried
- **THEN** the handler returns the classified `ErrorCodeIndexNotReady` transient

#### Scenario: A query against a ready index returns an ordinary empty result when there are no close neighbors

- **GIVEN** the embedding index is ready and a queried entity has no neighbor above the caller's similarity
  threshold
- **WHEN** `graph.embedding.query.similar` is queried for that entity
- **THEN** the handler returns a successful response with an empty similarity list, not an error

#### Scenario: The transient is programmatically detectable

- **GIVEN** a caller of `graph.embedding.query.similar`
- **WHEN** it receives the not-ready transient
- **THEN** it can classify it via the shared transient-error check (`errs.IsTransient` plus the
  `ErrorCodeIndexNotReady` code) without matching on error message text

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

#### Scenario: an obsolete in-flight terminal cannot clear a repair obligation

- **GIVEN** an entity stranded by a failed derived write or delete at revision N
- **WHEN** a hop-2 terminal already in flight for an OLDER revision (below N) completes after the
  stranding — hop-2 persistence runs outside the seam, so this ordering is reachable
- **THEN** the failed accounting retains the stranding (degraded persists and repair keeps
  targeting the entity) until a terminal at or above the stranding revision, or an explicit
  reconcile convergence, clears it

#### Scenario: repair cannot downgrade a generated record

- **GIVEN** a stranded entity whose embedding is generated — and whose stranding is causally
  cleared — between a repair snapshot and that snapshot's re-drive reaching the entity
- **WHEN** the stale re-drive re-queues the entity through the single hop-1 writer
- **THEN** the pending write is skipped because a generated record at a same-or-newer source
  revision exists, and the stored vector is unchanged

### Requirement: Offloaded-body resolution is instance-exact

A `StorageReference` SHALL be resolved only through the live store registered under its exact non-empty
`StorageInstance`. A registry miss SHALL NOT be served from a default, configured bucket, owned fallback, or any store
registered under another name.

An unresolved instance is an explicit content exclusion, not a resolved-store failure. The unresolved body SHALL be
counted through the existing content-unresolved observable. Existing inline identity text MAY continue through
embedding; if no usable inline text remains, the entity SHALL reach the existing skipped/no-text outcome. The miss
alone SHALL NOT create a failed embedding, increment a failure reason, or make embedding readiness degraded.

Resolution SHALL remain lazy per fetch. If an instance deregisters after the entity is admitted but before the worker
fetches, the worker SHALL apply the same unresolved/excluded behavior.

Once the exact instance resolves, an Open or Read failure from that store remains a real content failure and SHALL
retain existing failed/degraded accounting.

#### Scenario: The exact registered instance serves the body

- **GIVEN** a reference naming instance A and live stores registered as A and B
- **WHEN** graph-embedding resolves the reference
- **THEN** it reads only store A
- **AND** store B is never consulted

#### Scenario: A foreign instance never falls back

- **GIVEN** a reference naming unregistered instance B while another store A is available
- **WHEN** graph-embedding processes the entity
- **THEN** store A is never opened for the reference
- **AND** the body is counted as unresolved/excluded
- **AND** the miss does not enter failed/degraded accounting

#### Scenario: Inline identity continues after an unresolved body

- **GIVEN** an entity with an unresolved storage reference and inline identity text
- **WHEN** graph-embedding processes it
- **THEN** the inline identity text remains eligible for embedding
- **AND** no body bytes are guessed from another store

#### Scenario: An unresolved body with no inline text skips

- **GIVEN** an entity with an unresolved storage reference and no usable inline text
- **WHEN** graph-embedding processes it
- **THEN** it reaches the existing skipped/no-text terminal outcome
- **AND** any stale vector is removed through the existing no-text behavior
- **AND** the entity is not recorded as failed

#### Scenario: Deregistration between admission and fetch remains non-degrading

- **GIVEN** the exact instance exists when the component admits the entity
- **AND** that instance deregisters before the worker fetches
- **WHEN** the worker resolves lazily
- **THEN** it applies unresolved/excluded behavior
- **AND** it does not record a content failure solely because the name is now absent

#### Scenario: A resolved-store read failure remains a failure

- **GIVEN** the exact named instance resolves successfully
- **WHEN** that store fails to Open or Read the referenced key
- **THEN** graph-embedding records the existing bounded content-failure outcome
- **AND** failed/degraded observability remains intact
