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

