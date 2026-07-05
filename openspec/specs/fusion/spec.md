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

