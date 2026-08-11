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

### Requirement: Unhydrated seeds are reported, never inferable as absent

The fusion `Response` SHALL report every resolved seed the engine failed to
hydrate, distinct from `Misses`: a miss means resolution found nothing for
the query; an unhydrated entry means resolution produced a seed whose entity
fetch returned nothing or failed. Each entry SHALL carry the seed's handle
and a reason from the closed set `not_found` / `error` / `unknown`.
Reconciliation SHALL use ID-set semantics: the handler's missing report is
authoritative; the client synthesizes `unknown` entries only for requested
IDs in neither the returned set nor the handler's report; exactly one entry
per ID. Hydrated entities SHALL be restored to resolve order before ranking
(ranking is position-based, and the batch handler returns cache-hits-first —
today the resolve-top entity is demoted by cache residency; the
reconciliation fixes and pins this). Nothing in the response — including an
empty unhydrated list — SHALL be interpretable as an authoritative absence
claim in either direction: the list says what was not returned, never what
does not exist, and an entry's `not_found` licenses no deletion.

#### Scenario: A dropped seed is visible

- **GIVEN** resolution ranks an entity first and its authoritative-state
  fetch returns not-found (gh#597's confirmed drop path)
- **WHEN** `Fuse` builds the response
- **THEN** the unhydrated list carries that seed with reason `not_found`, and
  the remaining evidence is still returned in resolve order

#### Scenario: Every seed unhydrated is not a Miss

- **GIVEN** resolution produced seeds and every hydration returns not-found
- **WHEN** `Fuse` builds the response
- **THEN** the response carries unhydrated entries for every seed and
  synthesizes no `Miss` — "resolution found nothing" and "resolution found
  seeds I could not fetch" stay distinguishable at the boundary case

#### Scenario: A mixed-version handler cannot reintroduce silent omission

- **GIVEN** a batch handler that omits IDs without reporting them
- **WHEN** the client receives fewer entities than it requested
- **THEN** it synthesizes `unknown`-reason entries for exactly the IDs in
  neither the returned set nor a handler report — one entry per ID, and a
  handler-reported reason is never overwritten

#### Scenario: The list licenses no absence claim

- **GIVEN** an entity absent from both the nodes and the unhydrated list
- **WHEN** a consumer interprets the response
- **THEN** no deletion or reconciliation may treat that absence as
  authoritative (coherent-view consumers use graph-view-subscription)

### Requirement: A Miss licenses no absence claim

A fusion `Miss` SHALL carry no authoritative-absence meaning: with reads
serving under lag (ADR-084), a just-written, not-yet-indexed entity can
produce a miss with near-match suggestions. A consumer needing to establish
its own write is visible SHALL use the read-your-writes revision check, never
the presence or absence of a miss. Contract documentation that states or
implies "a miss only appears when Ready is true" SHALL be removed.

#### Scenario: A miss under lag is not proof of absence

- **GIVEN** an entity written but not yet indexed, and a healthy index under
  lag
- **WHEN** a `Fuse` query for it returns a miss
- **THEN** no consumer flow may treat the miss as proof the entity does not
  exist (create/dedupe/delete decisions need the revision check)

### Requirement: Resolve scores are observable on request

The engine SHALL carry each seed's resolve rank through assembly, and the
resolve similarity where the resolve mode provides one (semantic resolve
carries a similarity; symbol and prefix resolves do not), joining them to
nodes by entity ID — never by slice position. When the request opts in, each
node SHALL report its resolve rank and, when available, similarity; the
fields SHALL be omitted from the wire when not requested. Lens scoring
internals stay private.

#### Scenario: A ranking surprise is diagnosable on the product surface

- **GIVEN** a consumer investigating an unexpected ranking
- **WHEN** it repeats the request with score observability enabled
- **THEN** each node reports its resolve rank (and similarity for semantic
  resolves), without the consumer bypassing fusion over raw NATS

#### Scenario: The default wire shape is unchanged

- **GIVEN** a request that does not opt in
- **WHEN** the response is serialized
- **THEN** no score or rank field appears on any node

### Requirement: Body hydration failure is reported per node, never silent

The projection MUST report a requested-but-unloadable body as an empty body AND a
bounded reason naming why it is absent, never an empty body carrying no signal. This
applies whenever a node's verbatim body is requested (`WantBody`) but cannot be loaded.
A missing body is a partial result, not an absence: the node exists and ranks, so the
reason rides on the node itself. This is DISTINCT from `Unhydrated`, which reports seeds
that produced no node at all — a body-hydration failure concerns a node that is present.

The reason set is closed and mirrors the seed-hydration vocabulary: `not_found` when the
body reference resolves to no stored object (the object is absent — e.g. expired or
not-yet-written), and `error` for a genuine hydration fault (the body handle could not be
produced, or the stored-object read faulted for a reason other than absence). The reason
field is omitted entirely when the body hydrates, so a fully-hydrated response is
wire-unchanged. A failed body hydration MUST NOT cause the engine to defer or to
synthesize a `Miss`.

An entity that simply has no verbatim body is NOT a failure: it produces no body
reference, and its node MUST ship with an empty body, no reason, and no counter
increment. Only a body that was referenced-or-attempted but could not be loaded is
reported.

#### Scenario: a resolve failure reports a reason on the node

- **GIVEN** a request with `WantBody` and a node whose stored body read faults
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and a body reason of `error`

#### Scenario: a missing body object is distinguished from a fault

- **GIVEN** a request with `WantBody` and a node whose body reference does not resolve
  to a stored object
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and a body reason of `not_found`

#### Scenario: a missing body is a partial result, not a defer or a miss

- **GIVEN** a request with `WantBody` and one or more nodes whose bodies fail to load
- **WHEN** the response is assembled
- **THEN** the affected nodes are present with their body reasons set
- **AND** the engine does not defer and synthesizes no `Miss` for them

#### Scenario: a hydrated body carries no reason and is wire-unchanged

- **GIVEN** a request with `WantBody` and a node whose body loads successfully
- **WHEN** the node is projected
- **THEN** the node carries its body and the body reason field is omitted from the wire

#### Scenario: an entity with no verbatim body reports nothing

- **GIVEN** a request with `WantBody` and a node for an entity that has no verbatim body
  (its lens produces no body reference)
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and no body reason
- **AND** no body-hydration-failure counter is incremented

#### Scenario: body hydration failures are observable

- **GIVEN** one or more nodes whose bodies fail to load during a request
- **WHEN** the response is assembled
- **THEN** a body-hydration-failure counter is incremented, labelled by reason

### Requirement: The operation-specific NATS fusion adapter remains stable

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`, preserving
`New(requester, timeout)`, optional Close, lazy `GRAPH_STATUS/graph-index` readiness, and the six interface methods
`Status`, `Resolve`, `Entity`, `Entities`, `Neighbors`, and `Names`.

The transport SHALL retain six request subjects: by-name, prefix, semantic, entity, batch, and relationships. These
subjects are not a one-to-one restatement of the interface: Status uses KV, Resolve selects among three subjects, and
Names reuses by-name.

Every request/reply success SHALL pass through `graph.UnwrapQueryResponse` exactly once before operation decoding.
Status SHALL remain outside this rule because it reads KV state.

Entity SHALL decode the producer's `graph.ExactEntity`, require a valid matching entity and nonzero KV revision, and
project its ID and triples into the existing `fusion.Entity`. The revision SHALL NOT expand `fusion.Entity` or
`RetrievalClient` without a present consumer.

The fusion library SHALL claim no component ports. This change SHALL NOT invent a component or configuration owner for
the client.

#### Scenario: fusion entity uses the producer representation

- **GIVEN** a valid `graph.ExactEntity` reply
- **WHEN** `fusionnats.Client` reads it
- **THEN** the exact entity and revision are validated
- **AND** the existing fusion entity contains its ID and triples
- **AND** no obsolete bare `EntityState` fixture remains

#### Scenario: request subjects accept one envelope

- **GIVEN** equivalent bare and standard-enveloped fixtures for each request subject
- **WHEN** `fusionnats.Client` decodes them
- **THEN** each pair produces the same existing retrieval result
- **AND** no payload is unwrapped twice

#### Scenario: fusion preservation creates no port owner

- **WHEN** Slice E component and configuration changes are inspected
- **THEN** no fusion-host component or fusion port declaration was added
- **AND** the client constructor and interface remain unchanged

