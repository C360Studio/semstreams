## ADDED Requirements

### Requirement: Operators can inventory every account storage resource

SemStreams MUST enumerate ordinary JetStream streams, `KV_*` backing streams, and `OBJ_*` backing
streams from the authoritative account and expose a normalized inventory. Each resource MUST report
its kind, logical name, storage type, configured and live capacity/retention limits, current bytes,
messages or objects, headroom, configuration drift, sampled growth rate, and time to configured
pressure thresholds when sufficient samples exist. Inventory MUST NOT depend on the resource having
been created or previously accessed through one client instance.

#### Scenario: KV and ObjectStore resources appear without client registration

- **GIVEN** a KV bucket and ObjectStore created by another process in the authorized account
- **WHEN** an operator requests the SemStreams storage inventory
- **THEN** both backing streams appear with their logical resource kinds and current usage
- **AND** neither depends on prior registration in the JetStream metrics client

#### Scenario: Forecast is honest when history is insufficient

- **GIVEN** a newly discovered resource with fewer samples than the forecast requires
- **WHEN** storage status is reported
- **THEN** current usage and headroom are present
- **AND** time-to-threshold is reported as unavailable rather than a fabricated estimate

### Requirement: Storage pressure changes admission before hard exhaustion

SemStreams MUST derive configurable `normal`, `warning`, `restrict`, and `critical` pressure states
for each resource and the account. Admission MUST apply the most severe relevant state and preserve a
reserve sized for bounded in-flight commits and recovery. Under restriction, new graph identities,
append-shaped graph growth, and new content uploads MUST be rejected before compact replacement,
retraction, reads, diagnostics, and exact-owner object release. Pressure MUST NOT trigger blind
deletion of live graph state or durable-referenced content.

#### Scenario: Graph births stop before replacement updates

- **GIVEN** the graph resource is in `restrict` pressure with reserve remaining
- **WHEN** one request births a new identity and another compactly replaces an existing entity facet
- **THEN** the birth is rejected with a capacity error
- **AND** the bounded replacement remains admissible while reserve permits

#### Scenario: Object pressure preserves recovery operations

- **GIVEN** an object backend is above its upload restriction threshold
- **WHEN** clients request a new upload, a streaming read, and release of an exact superseded object
- **THEN** the new upload is rejected
- **AND** the read and release remain available

### Requirement: Capacity state and enforcement are operationally observable

SemStreams MUST expose bounded-cardinality metrics and operator diagnostics for resource bytes,
limits, headroom, pressure state, growth rate, forecast, rejected admissions, configuration drift,
and inventory/scrub freshness. Alerts MUST distinguish approaching soft thresholds from a backend hard
rejection. Entity IDs, object keys, and other unbounded values MUST NOT be metric labels.

#### Scenario: An operator can identify the constraining resource

- **GIVEN** uploads are rejected because one retained-content store is in `restrict` pressure
- **WHEN** the operator inspects metrics and storage status
- **THEN** both identify the retained store, threshold, live bytes, configured limit, and rejection
  class
- **AND** no object key is emitted as a Prometheus label

### Requirement: Maintenance rebuild reclaims only derived index state

SemStreams MUST provide a readiness-gated maintenance operation that clears configured rebuildable
graph indexes, replays current `ENTITY_STATES`, and withholds affected query surfaces until rebuild
readiness is established. The operation MUST NOT delete or rewrite authoritative entity state and
MUST NOT decide whether a semantic identity is dead.

#### Scenario: Queries do not observe a partial rebuild

- **GIVEN** an operator starts a derived-index maintenance rebuild
- **WHEN** the index has been cleared and replay is incomplete
- **THEN** affected queries return not-ready rather than partial results
- **AND** queries resume only after readiness confirms the replay completed successfully
