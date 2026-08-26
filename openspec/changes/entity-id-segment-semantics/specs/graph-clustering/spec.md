## MODIFIED Requirements

### Requirement: Community detection runs over explicit plus EntityID-synthesized edges

Community detection (LPA) MUST run over an edge set that combines the entity's explicit graph edges with optional
*virtual* edges synthesized from the six-position EntityID hierarchy: sibling edges between entities sharing the
five-position type prefix (`pkg/types.EntityID.TypePrefix()`, `org.platform.system.domain.type`) and source-peer
edges between entities sharing the same `system` position read by name from `ParseEntityID`, never by raw index.
The synthesis augments explicit adjacency; it never removes an explicit edge. This requirement is superseded when
the derived-partition change (ADR-099) lands; that change MUST compute level 0 as the four-position prefix, level 1
as the three-position source prefix, and level 2 as the two-position deployment prefix.

#### Scenario: explicit edges are always present in the detection input

- **GIVEN** entities with explicit relationship triples
- **WHEN** community detection runs
- **THEN** the explicit edges are part of the adjacency the detector sees
- **AND** any synthesized virtual edges are added on top, not substituted

#### Scenario: sibling and source-peer synthesis reads positions by name

- **GIVEN** entities `acme.dep1.src.git.commit.a1` and `acme.dep1.src.git.commit.a2`
- **WHEN** virtual edges are synthesized
- **THEN** the pair is a sibling pair by the five-position type prefix
- **AND** both share source `src` by the named `System` field
