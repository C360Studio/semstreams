# graph-clustering Specification

## Purpose
TBD - created by archiving change graph-clustering-edge-config. Update Purpose after archive.
## Requirements
### Requirement: Community detection runs over explicit plus EntityID-synthesized edges

Community detection (LPA) MUST run over an edge set that combines the entity's
explicit graph edges with optional *virtual* edges synthesized from the 6-part
EntityID hierarchy: sibling edges between entities sharing the 5-part type prefix
(`org.platform.domain.system.type`) and system-peer edges between entities sharing
the same system. The synthesis augments explicit adjacency; it never removes an
explicit edge.

#### Scenario: explicit edges are always present in the detection input

- **GIVEN** entities with explicit relationship triples
- **WHEN** community detection runs
- **THEN** the explicit edges are part of the adjacency the detector sees
- **AND** any synthesized virtual edges are added on top, not substituted

### Requirement: EntityID virtual-edge synthesis is operator-configurable

The graph-clustering component MUST let an operator enable or disable sibling and
system-peer virtual edges (and tune their weights and per-entity caps) through its
configuration, so that community detection over a homogeneous entity family whose
explicit relationships already encode the topology can run on the explicit edges
alone.

#### Scenario: an operator disables virtual edges for a homogeneous family

- **GIVEN** a family of same-type entities whose explicit edges form two disjoint clusters
- **AND** the component configured with sibling and system-peer synthesis disabled
- **WHEN** community detection runs
- **THEN** the two clusters are detected as distinct communities
- **AND** the synthesized virtual edges do not bridge them into one

### Requirement: Omitting the edge-synthesis config preserves the default behavior

The virtual-edge configuration MUST be tri-state per toggle: unset resolves to the
built-in default, and only an explicit value overrides it. A configuration that
omits the edge-synthesis block MUST behave exactly as the built-in defaults
(sibling and system-peer synthesis enabled), so introducing the field cannot
silently disable synthesis for an existing deployment.

#### Scenario: omitted config resolves to defaults-on

- **GIVEN** a component configuration with no edge-synthesis block
- **WHEN** the component initializes its community detector
- **THEN** sibling and system-peer synthesis are enabled with the built-in default weights and caps

#### Scenario: a partial config leaves unset toggles at their default

- **GIVEN** a component configuration that disables only sibling edges
- **WHEN** the component initializes its community detector
- **THEN** sibling synthesis is disabled
- **AND** system-peer synthesis remains at its default (enabled)

### Requirement: The community index is rebuilt non-destructively

A detection run SHALL NOT empty the community index as a step in rebuilding it.
It SHALL write the new partition over the prior one in place and then remove
only the stored keys that do not belong to the new partition, so a reader
observing the bucket mid-rebuild sees the union of the prior and new partitions
— stale entries, never an absent index. Detectors SHALL NOT clear the store
before a rebuild.

The removal step SHALL derive the keys the new partition owns from the
partition itself, inside the storage layer, so the key format stays private to
storage and cannot drift at a caller. A removal failure SHALL NOT fail the
detection run: every community in the new partition is already persisted at
that point, so the index is correct and merely carries stale extra entries,
which the next cycle removes. Failing the run would discard a valid partition
and surface an error to callers who received valid results.

This requirement exists because readiness no longer gates detection on view
age (ADR-085). Under the previous exact gate, detection effectively never ran
on a continuously-written graph, so a clear-then-rebuild window was almost
unreachable; running every tick makes it permanent. Detection has been measured
from 4.4s to 23.7s against a 30s cycle.

#### Scenario: A reader mid-rebuild never observes an empty index

- **GIVEN** a populated community index and a detection run in progress
- **WHEN** a consumer reads the index at any instant during the run
- **THEN** it observes the prior partition, the new one, or a union of both
- **AND** it never observes an empty index on account of the rebuild

#### Scenario: Stale communities are removed once the run completes

- **GIVEN** a prior partition containing a community absent from the new one
- **WHEN** the detection run finishes
- **THEN** that community and its entity mappings are no longer stored

#### Scenario: A removal failure leaves a correct superset

- **GIVEN** a completed detection run whose removal step fails
- **WHEN** a consumer reads the index
- **THEN** every community of the new partition is present and readable
- **AND** the run does not report an error
- **AND** the next successful cycle removes the leftovers

### Requirement: Projections of the community index agree with it on record identity

Any in-memory projection of the community index SHALL identify a community by
the same identity the store uses — the pair of level and community ID — and
SHALL NOT index communities by ID alone. A deletion SHALL be applied using the
level carried by the deleted key, not the level of whatever record the
projection happens to hold under that ID.

Community IDs are seed entity IDs and every level derives its partition from
the same entity set, so the same ID recurs across levels by construction. A
projection keyed by ID alone therefore lets one level shadow another, and a
deletion for one level evicts another level's live record. This is not
hypothetical: it silently truncated the level-0 index that global community
search reads without a fallback.

#### Scenario: Levels sharing a community ID do not shadow each other

- **GIVEN** a community ID present at more than one level
- **WHEN** the projection has applied the records for every level
- **THEN** each level's record is independently retrievable
- **AND** each level's index lists its own communities completely

#### Scenario: A deletion removes only the level it names

- **GIVEN** a projection holding records for one community ID at several levels
- **WHEN** a deletion arrives for that ID at one level
- **THEN** only that level's record is removed
- **AND** the other levels' records and level indexes are unchanged

### Requirement: Community detection synthesizes semantic co-location virtual edges via mutual-kNN

Community detection MUST support an operator-configurable semantic-similarity virtual-edge tier that adds
ephemeral mutual-kNN edges to the detection edge set alongside the explicit and EntityID-derived edges, so
entities that are thematically related but structurally heterogeneous (different type, different system) can
still be voted into the same community. A semantic edge between two entities MUST be synthesized only when
each appears in the other's top-`k` similarity result (via `graph.embedding.query.similar`) at or above a
configured similarity threshold — a one-directional match MUST NOT synthesize an edge. Edge-weight resolution
MUST treat an explicit edge as strictly dominant over every virtual-edge tier, and MUST resolve a pair that
qualifies under more than one virtual-edge tier (sibling, system-peer, semantic) to the MAXIMUM of the
qualifying tiers' weights, never their sum. Omitting the semantic-edge configuration MUST reproduce today's
sibling and system-peer edge weights and caps exactly — enabling the tier MUST NOT silently change behavior
for a deployment that has not opted in.

#### Scenario: A mutual match synthesizes a semantic edge

- **GIVEN** two entities where each appears in the other's top-k similarity result at or above the
  configured threshold
- **WHEN** community detection builds its edge set
- **THEN** a semantic virtual edge is synthesized between them

#### Scenario: A one-directional match does not synthesize an edge

- **GIVEN** two entities where only one appears in the other's top-k similarity result (not mutual)
- **WHEN** community detection builds its edge set
- **THEN** no semantic virtual edge is synthesized between them

#### Scenario: Explicit edges dominate every virtual-edge tier

- **GIVEN** a pair of entities connected by an explicit relationship edge and also qualifying as a mutual-kNN
  semantic match
- **WHEN** the edge weight between them is resolved
- **THEN** the explicit edge's weight is used, not the semantic weight and not a combination of the two

#### Scenario: A dual-qualifying pair resolves to the max weight, not a sum

- **GIVEN** a pair of entities with no explicit edge that qualifies as both siblings (EntityID type-prefix
  match) and a mutual-kNN semantic match
- **WHEN** the edge weight between them is resolved
- **THEN** the resolved weight is the maximum of the sibling and semantic weights
- **AND** it is never the sum of the two

#### Scenario: Omitting the configuration preserves today's edge weights and caps

- **GIVEN** a component configuration that does not enable the semantic-edge tier
- **WHEN** community detection builds its edge set
- **THEN** the sibling and system-peer virtual-edge weights and per-entity caps are exactly what they were
  before the semantic-edge tier existed

### Requirement: Explicit-edge membership for weight resolution is bidirectional, per-cycle, and fails closed

Community detection MUST decide explicit-edge membership for weight resolution from a real graph edge in
EITHER direction (outgoing or incoming) between the pair, and MUST resolve a pair to the strictly-dominant
explicit weight only when such an edge exists. A pair with NO explicit edge in either direction MUST fall
through to the maximum of its qualifying virtual-edge tiers (sibling, system-peer, semantic) and MUST NOT
resolve to the explicit weight — every virtual-edge pair therefore votes at its tier weight, never a flat
explicit weight. Explicit-edge membership MUST reflect the current detection cycle's graph topology, so an
explicit edge removed between cycles MUST NOT count as explicit in the following cycle. A failed edge-weight
or topology read during resolution MUST abort the detection cycle rather than fabricate an explicit-dominant
weight, so a transient read error can never silently partition every pair as explicitly connected.

#### Scenario: An explicit edge in either direction makes the pair explicit

- **GIVEN** a pair of entities connected by a real explicit graph edge in exactly one stored direction
  (present in the outgoing index for the source, equivalently in the incoming index for the target)
- **WHEN** the edge weight between them is resolved
- **THEN** the pair is treated as explicit and resolves to the strictly-dominant explicit weight
- **AND** the stored direction of the edge does not change the result

#### Scenario: A non-explicit pair resolves to its virtual tier, not the explicit weight

- **GIVEN** a pair of entities with NO explicit graph edge in either direction that qualifies as siblings
  (sibling weight 0.7) and qualifies under no higher virtual tier
- **WHEN** the edge weight between them is resolved
- **THEN** the resolved weight is the sibling weight (0.7), not the explicit weight (1.0)
- **AND** it is never the flat explicit weight that an "every pair is explicit" resolution would return

#### Scenario: An explicit edge removed between cycles stops counting as explicit

- **GIVEN** a pair treated as explicit in one detection cycle because a real explicit edge connected them
- **AND** that explicit edge is removed from the graph before the next detection cycle
- **WHEN** the edge weight between them is resolved in the next cycle
- **THEN** the pair is no longer treated as explicit
- **AND** it resolves to the maximum of its qualifying virtual-edge tiers instead

#### Scenario: A failed topology read aborts the cycle rather than defaulting to explicit

- **GIVEN** the weight-resolution path performs an explicit-edge or topology read that fails
- **WHEN** a detection tick resolves edge weights
- **THEN** the detection cycle aborts and surfaces the read error
- **AND** no partition is committed that treats the unread pairs as explicitly connected

### Requirement: Community detection gates semantic edges on embedding readiness with a structural-floor guarantee

Community detection MUST gate semantic-edge synthesis on a dedicated embedding-readiness signal, distinct
from the existing graph-index readiness gate that governs whether the cycle runs at all, and MUST fall back
to a structural-only partition — never an empty or failed detection cycle — when the embedding index is not
ready. When the graph-index readiness gate defers, the whole detection cycle MUST defer exactly as it does
today, unaffected by embedding readiness. When the graph-index gate proceeds but the embedding index is not
ready and the semantic-edge tier is enabled, detection MUST run using explicit and EntityID-derived edges
only and MUST record that the cycle ran without semantic edges. When both gates are satisfied, detection MUST
run the full edge set and MUST record that semantic edges were applied. A read of the embedding similarity
service that fails because the embedding index is not ready (the classified `ErrorCodeIndexNotReady`
transient) MUST be treated as "could not ask this tick," never conflated with a genuine "no similar
neighbors" result.

#### Scenario: A not-ready graph-index defers the whole cycle unaffected by embedding readiness

- **GIVEN** the graph-index readiness gate is not satisfied
- **WHEN** a detection tick fires
- **THEN** the entire cycle defers, regardless of the embedding index's readiness state

#### Scenario: A cold embedding index degrades to structural-only, never fails

- **GIVEN** the graph-index readiness gate is satisfied, the semantic-edge tier is enabled, and the embedding
  index is not ready
- **WHEN** a detection tick fires
- **THEN** detection runs and commits a complete partition built from explicit and EntityID-derived edges
  only
- **AND** the cycle is recorded as having run without semantic edges applied
- **AND** the cycle neither fails nor produces an empty partition

#### Scenario: A ready embedding index runs the full cycle and reports applied

- **GIVEN** both the graph-index and embedding readiness gates are satisfied and the semantic-edge tier is
  enabled
- **WHEN** a detection tick fires
- **THEN** detection runs using the full edge set including semantic virtual edges
- **AND** the cycle is recorded as having applied semantic edges

#### Scenario: The not-ready transient is distinguished from a genuine empty result

- **GIVEN** the semantic-edge synthesis path queries the embedding similarity service
- **WHEN** the service returns the classified `ErrorCodeIndexNotReady` transient
- **THEN** the tick is treated as "could not ask" (degrading per the scenario above), not as evidence the
  entity has no semantic neighbors

### Requirement: Community detection is reproducible given a fixed edge set

Community detection SHALL produce the same partition across repeated runs over an identical, unchanged edge
set: the label-propagation shuffle order SHALL be drawn from a seeded random source scoped to that detection
run rather than an unseeded global default, and a vote-total tie between two candidate labels SHALL resolve
deterministically (the lexicographically smallest label wins) rather than via unordered map iteration order.

#### Scenario: Repeated runs over a fixed edge set converge to the same partition

- **GIVEN** an unchanged set of entities and edges
- **WHEN** community detection runs twice in succession
- **THEN** both runs produce an identical partition (same communities, same membership)

#### Scenario: A vote tie resolves deterministically

- **GIVEN** an entity whose neighbor votes produce an exact tie between two candidate labels
- **WHEN** community detection resolves the new label
- **THEN** the lexicographically smallest of the tied labels is chosen, every time the tie recurs

### Requirement: LLM community summaries live in a worker-exclusive, content-addressed store

LLM-generated community summaries SHALL be stored in a `COMMUNITY_SUMMARIES` KV bucket keyed by
`{level}.{membership_hash}`, written ONLY by the enhancement worker; the detector SHALL NOT write
this bucket, and the enhancement worker SHALL NOT write the partition bucket (`COMMUNITY_INDEX`).
The partition bucket remains detector-exclusive (partition, keywords, and the statistical summary);
LLM prose lives only in the summary store.

Because the summary key is derived from the content the worker summarized (the sorted member set),
a write is correct for that membership whether or not the membership is still current — so a
lagging or slow worker CANNOT overwrite a fresher partition (there is no shared key) and CANNOT
resurrect a `Prune`-deleted community (the read path joins by the *current* community's membership
hash, so an orphaned summary is served only when a current community has that exact member set).
The write therefore requires no revision CAS, no membership-similarity transfer, and no archive
step. A same-membership double-write by two workers SHALL be idempotent, not an error.

#### Scenario: A stale-snapshot worker write cannot corrupt the partition

- **GIVEN** a detection cycle has replaced community `X`'s membership since a worker read its snapshot
- **WHEN** that worker finishes summarizing the stale snapshot and writes its result
- **THEN** the write lands in `COMMUNITY_SUMMARIES` keyed by the stale membership's hash
- **AND** `COMMUNITY_INDEX` and its entity mappings are unchanged
- **AND** no `Prune`-deleted community is resurrected in the partition bucket

#### Scenario: An unchanged membership is a cache hit, not a re-summarization

- **GIVEN** a community whose membership hash already has an `llm-enhanced` summary record
- **WHEN** the enhancement worker is triggered for that community again
- **THEN** it serves the stored summary and performs no LLM call
- **AND** a `summary_cache_hits_total` observation is recorded

#### Scenario: A failed summary is retried only after a backoff

- **GIVEN** a membership hash whose summary record has status `llm-failed`
- **WHEN** the enhancement worker is triggered for it before the retry backoff elapses
- **THEN** it does not perform an LLM call
- **AND** after the backoff elapses a subsequent trigger does retry

### Requirement: The community membership hash has a single shared definition

The membership hash that keys `COMMUNITY_SUMMARIES` SHALL be produced by one shared exported helper
(`clustering.MembershipHash`) computing sha256 over the newline-joined, lexically-sorted member IDs,
hex-encoded. Every producer and consumer of the key — the enhancement worker, the graph-query
read-join, and the B0 thematic eval — SHALL derive the hash through that one helper so the
definition cannot drift into two subtly different hashes that never join.

#### Scenario: The store and the eval agree on the hash

- **GIVEN** a fixed member set
- **WHEN** the enhancement worker and the B0 eval each compute its membership hash
- **THEN** both obtain the identical value from `clustering.MembershipHash`

### Requirement: Community-summary volume is observable

The number of stored community summaries SHALL be exposed as a gauge, so the operational question
"is the content-addressed summary store accumulating unboundedly?" is answered by a metric read
rather than an estimate. (The store is a reuse cache with no summaries GC in this increment; the
gauge is the trigger for a future bounded-GC decision.)

#### Scenario: The summary-store size is a metric

- **GIVEN** a running graph-clustering component with a populated `COMMUNITY_SUMMARIES` bucket
- **WHEN** an operator scrapes metrics
- **THEN** a gauge reports the current number of stored summary records

### Requirement: Contract validation rides the polled input path, with no ENTITY_STATES watcher

Graph-clustering MUST validate ENTITY_STATES values at its consuming read seam (the polled
entity-state queries that drive detection and enhancement) and MUST hold no ENTITY_STATES
watcher at all — its input path is timer-driven polled reads, not a watch. A validating-decode
failure at the consuming seam MUST drive the sticky whole-view projection reset-required latch;
because each detection cycle's corpus read decodes the resident entity set, resident poison
latches within one detection interval of appearing.

#### Scenario: consumed poison latches the sticky projection reset

- **GIVEN** a poisoned ENTITY_STATES value resident in the bucket
- **WHEN** a detection or enhancement cycle reads and fails to decode it
- **THEN** clustering enters its sticky reset-required projection state
- **AND** the latch survives a later valid overwrite of the same key until process restart

#### Scenario: steady-state writes cost clustering nothing

- **GIVEN** graph-clustering is running at steady state
- **WHEN** an entity write commits to ENTITY_STATES
- **THEN** clustering receives zero deliveries of that write
- **AND** clustering holds zero ENTITY_STATES watchers

