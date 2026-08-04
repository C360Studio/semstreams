## ADDED Requirements

### Requirement: Structural analysis is an ephemeral anomaly input, not a durable view

Graph-clustering MUST NOT create, open, write, read, advertise, or publish
readiness for a `STRUCTURAL_INDEX` bucket. When anomaly detection is enabled and
successfully initialized, graph-clustering MUST compute the required K-core and
pivot inputs from the current cycle's graph and pass them directly to the anomaly
detectors in memory. Structural computation MUST use the explicit plus
EntityID-derived provider and MUST NOT include semantic virtual edges.

The component MUST expose no `enable_structural`, `pivot_count`,
`max_hop_distance`, or `structural_index` output-port contract. Anomaly enablement
owns these internal prerequisites and uses the framework's internal default pivot
count. Retired configuration and an explicit `STRUCTURAL_INDEX` output MUST fail
startup with deletion guidance rather than being silently ignored.

#### Scenario: anomaly detection receives fresh structural inputs

- **GIVEN** anomaly detection is enabled and initialized
- **WHEN** a community-detection cycle completes
- **THEN** K-core and pivot inputs are computed from that cycle's structural provider
- **AND** they are passed directly to anomaly detection in memory
- **AND** no `STRUCTURAL_INDEX` bucket is created

#### Scenario: disabled anomaly detection performs no structural computation

- **GIVEN** anomaly detection is disabled
- **WHEN** community-detection cycles run
- **THEN** graph-clustering does not compute K-core or pivot inputs
- **AND** no structural storage or output is created

#### Scenario: persistence cannot disable anomaly initialization

- **GIVEN** anomaly detection is enabled
- **WHEN** graph-clustering starts on a clean account
- **THEN** anomaly initialization does not depend on a structural bucket or storage adapter
- **AND** the component uses its internal default pivot count without adopter configuration

#### Scenario: semantic virtual edges remain isolated from structural analysis

- **GIVEN** a pair connected only by a semantic virtual edge
- **WHEN** anomaly prerequisites are computed for a cycle
- **THEN** the pair remains disconnected in the K-core and pivot inputs
- **AND** semantic similarity may affect community detection without changing structural anomaly inputs

#### Scenario: stale structural configuration fails loudly

- **GIVEN** configuration containing `enable_structural`, `pivot_count`,
  `max_hop_distance`, or an explicit `STRUCTURAL_INDEX` output
- **WHEN** graph-clustering loads it
- **THEN** startup rejects the retired surface with deletion guidance
- **AND** no field or port is silently ignored
