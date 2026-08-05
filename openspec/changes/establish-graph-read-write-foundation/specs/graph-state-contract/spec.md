# graph-state-contract — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Generic KV actions cannot bypass graph ownership

Generic KV actions MUST NOT bypass graph-ingest's physical/catalog responsibility for `ENTITY_STATES` or a derived
component's responsibility for its own bucket. “Ownership” in this requirement names storage topology and
poison/reset
responsibility only; it MUST NOT authorize semantic predicates or require claims, leases, tokens, or presence.

#### Scenario: Catalog responsibility remains after claim deletion

- **GIVEN** a generic action attempts to write a graph-owned bucket
- **WHEN** the graph-state guard validates the target
- **THEN** it rejects the bypass using the catalog
- **AND** it performs no semantic owner lookup

### Requirement: Poison response scope is defined per reader class

The authoritative exact entity read MUST validate the returned `ENTITY_STATES` entry and carry the same-entry revision.
Poison remains scoped to the poisoned entity at the authority surface; unrelated reads and writes proceed. Semantic
ownership deletion MUST NOT broaden poison into a global halt or change `GRAPH_STATUS`.

#### Scenario: Missing relationship target is not poison

- **GIVEN** a valid source entity references an absent object
- **WHEN** the source is read and its object is dereferenced
- **THEN** the source remains valid
- **AND** dereference returns typed not-found rather than graph-state poison
