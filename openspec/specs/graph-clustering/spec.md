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

