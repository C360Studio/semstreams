# fusion Specification

## Purpose
TBD - created by archiving change fusion-per-facet-edges. Update Purpose after archive.
## Requirements
### Requirement: A Lens declares its relationship edges as EdgeSpecs

A `Lens` MUST declare the relationship predicates it walks via `Edges() []EdgeSpec`.
Each `EdgeSpec` names a `Predicate` and the role labels for its forward
(`OutgoingRole`) and optional reverse (`IncomingRole`) directions; an empty
`IncomingRole` skips the reverse direction. The engine consumes these specs for
three facets: the per-node `relations` map (forward + reverse roles), the outgoing
`paths` walk, and the incoming `impact` walk.

#### Scenario: a declared edge feeds the relations map

- **GIVEN** a Lens whose `Edges()` includes an `EdgeSpec` with a forward and reverse role
- **WHEN** the engine builds the `relations` map for a result node
- **THEN** neighbors reachable over that predicate appear under the declared roles

### Requirement: An EdgeSpec participates only in its selected facets

`EdgeSpec` MUST support an optional `Facets` selector naming the facets the edge
feeds — `relations`, `paths`, and/or `impact`. An `EdgeSpec` whose `Facets` is
empty MUST participate in **all three** facets (the backward-compatible default, so
a lens that declares no selector is unchanged). A non-empty `Facets` MUST restrict
the edge to exactly the named facets: the engine includes the edge's predicate in a
facet's walk iff the edge's `Facets` is empty or contains that facet.

This lets a lens declare a containment edge (e.g. file→symbol) that populates the
`relations` map without polluting the `impact` walk, whose incoming traversal would
otherwise pull structural containment ancestry into the reverse-dependency closure.

#### Scenario: an edge excluded from impact still populates relations

- **GIVEN** a Lens with an `EdgeSpec` whose `Facets` is `{relations}`
- **WHEN** the engine builds a result node's `relations` map AND computes `impact`
- **THEN** the edge's neighbors appear in the `relations` map
- **AND** the edge is NOT traversed by the `impact` walk

#### Scenario: an edge with no facet selector participates everywhere

- **GIVEN** a Lens with an `EdgeSpec` whose `Facets` is empty
- **WHEN** the engine computes `relations`, `paths`, and `impact`
- **THEN** the edge's predicate is walked for all three facets

### Requirement: A request MAY scope NL retrieval to entity-ID prefixes

`fusion.Request` MUST support an optional `Scope` — a list of dot-delimited entity-ID
prefixes. When non-empty, the engine MUST constrain NL seed resolution to entities
whose ID matches at least one prefix (OR-matched), so a lens instance over a shared
embedding index retrieves only its domain and is not diluted by a larger co-resident
domain. An empty/absent `Scope` MUST behave exactly as today (no filter). Matching is
by leading prefix on a dot boundary, not glob, and the scope MUST be applied at the
candidate source (before ranking), not as a post-retrieval trim, so a small domain
is never crowded out of the ranked window.

The scope MUST be threaded to the retrieval client via a struct parameter
(`ResolveQuery{Query, Mode, Scope, Limit}`) rather than a positional argument, so the
NL-only scope does not force symbol/prefix callers to pass an ignored value.

#### Scenario: a scoped NL query retrieves only the in-scope domain

- **GIVEN** a shared embedding index holding a large `code` domain and a small `docs`
      domain
- **WHEN** `Fuse` runs an NL request whose `Scope` names the docs ID prefix
- **THEN** the resolved seeds are docs entities only
- **AND** the small domain is not out-ranked by the larger one

#### Scenario: an empty scope is a no-op

- **GIVEN** an NL request with an empty/absent `Scope`
- **WHEN** `Fuse` resolves it
- **THEN** retrieval is identical to the unscoped behavior (byte-identical request)

### Requirement: The graph facet is additive and opt-in

A request without the `graph` want MUST produce a byte-identical v1 response shape, and the
default want-set MUST NOT include the graph facet — a fusion request MAY opt in by naming the
`graph` want, and only then does the response carry the optional graph projection alongside
the untouched v1 fields.

#### Scenario: v1 requests are unaffected

- **GIVEN** a fusion request without the `graph` want
- **WHEN** the engine responds
- **THEN** the response carries no graph projection field
- **AND** all v1 fields behave exactly as before

### Requirement: Graph classification is declaration-driven, never value-shape-driven

The graph projection MUST classify a triple as a directed edge only when its predicate is
lens-declared as a relationship or the triple carries the explicit entity-reference datatype;
a literal value that merely resembles a six-part entity ID MUST remain a typed property fact.

#### Scenario: an ID-shaped literal stays a property

- **GIVEN** a seed entity carrying a triple whose string value has valid six-part entity-ID
  shape, whose predicate is not lens-declared, and whose datatype is empty
- **WHEN** the graph projection is built
- **THEN** the triple appears as a property fact with its verbatim predicate and value
- **AND** no edge is projected from it

### Requirement: Distinct directed facts stay distinct

The graph projection MUST preserve parallel predicates between the same node pair as separate
edges, opposite-direction facts between the same pair as separate edges with true
subject-to-object source/target orientation, and multiple evidence contributions for the same
semantic edge as separate inspectable evidence entries on one edge.

#### Scenario: parallel predicates are two edges

- **GIVEN** two lens-declared predicates each linking node A to node B
- **WHEN** the graph projection is built
- **THEN** two edges appear, each with its verbatim predicate, both with source A and target B

#### Scenario: opposite directions are distinct

- **GIVEN** a fact from A to B and a fact from B to A under lens-declared predicates
- **WHEN** the graph projection is built
- **THEN** both edges appear with swapped source and target handles
- **AND** neither collapses into the other

#### Scenario: evidence contributions do not collapse

- **GIVEN** two triples asserting the same source, predicate, and target with different
  evidence (source or timestamp or confidence)
- **WHEN** the graph projection is built
- **THEN** one edge appears carrying both evidence entries, each inspectable

### Requirement: Evidence is projected verbatim and never fabricated

Per-fact and per-edge evidence MUST carry the underlying triple's source, timestamp,
confidence, and context exactly as stored, with absent values omitted from the wire — the
projection MUST NOT default, infer, or synthesize any evidence value.

#### Scenario: missing evidence stays absent

- **GIVEN** a stored triple with no confidence value and no context
- **WHEN** its fact or edge is projected
- **THEN** the evidence entry omits confidence and context rather than emitting zero values

### Requirement: Graph truncation is observable and independent

The graph projection MUST bound facts per node and edges per projection with explicit
truncation metadata (per-node truncation flags and dropped counts, and a projection-level
truncated flag) that is independent of the v1 node/body budget truncation and of the
relations facet's per-role cap.

#### Scenario: fact truncation is visible without touching v1 truncation

- **GIVEN** a seed entity whose fact count exceeds the per-node fact cap
- **WHEN** the graph projection is built
- **THEN** the node reports facts-truncated with a dropped count
- **AND** the projection-level truncated flag is set
- **AND** the v1 top-level truncated field is unaffected by graph-facet truncation

### Requirement: The projection reports view-revision observations, never a coherence claim

The graph projection SHALL report the indexed revision sampled before resolution
(start) and re-sampled after the fetch phase (end) as plain observations, and
SHALL NOT carry any field claiming the projection reflects a single indexed
revision — such a claim is not provable from samples of a heartbeat-published
status feed (ADR-083). A failed re-sample SHALL report end=0 rather than a
guessed revision. A consumer that needs a genuinely coherent single-revision
view uses the graph-view-subscription capability (ADR-081); the fusion
projection is best-effort ranked evidence.

#### Scenario: the observed span is reported verbatim

- **GIVEN** the sampled indexed revision differs between the pre-resolution
  sample and the post-fetch re-sample
- **WHEN** the response is returned
- **THEN** the view revision reports the unequal start and end bounds verbatim

#### Scenario: the wire carries no coherence claim

- **GIVEN** a response built entirely at one observed revision
- **WHEN** the graph projection is serialized
- **THEN** the view revision reports equal start and end bounds
- **AND** no coherent field exists on the wire

#### Scenario: a failed re-sample degrades honestly

- **GIVEN** the post-fetch status re-sample fails
- **WHEN** the response is returned
- **THEN** the view revision reports end=0, never a guessed revision

