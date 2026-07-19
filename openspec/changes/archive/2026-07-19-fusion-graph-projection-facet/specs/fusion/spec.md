# fusion — Delta

## ADDED Requirements

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

### Requirement: The projection carries a view-revision consistency contract

The graph projection MUST report the indexed revision sampled before resolution and re-sampled
after the fetch phase, and MUST mark the projection coherent only when the two are equal, so a
consumer can accept a single-revision view or detect and refresh a response that spans
revisions; a failed re-sample MUST report incoherent rather than guessing.

#### Scenario: a consumer can reject a spanning response

- **GIVEN** the indexed revision advances between resolution and the final graph fetch
- **WHEN** the response is returned
- **THEN** the view revision reports unequal start and end and coherent=false
- **AND** a response built entirely at one revision reports equal bounds and coherent=true
