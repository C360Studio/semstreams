## ADDED Requirements

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
