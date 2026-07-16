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

### Requirement: Post-v1 retained-state upgrades are versioned and operator approved

Every post-v1 breaking storage or enforcement upgrade MUST begin with a versioned report-only manifest naming the
source and target binary/configuration versions, authoritative retained-resource inventory and expected data shapes,
backup/export and restore-validation scope, ordered migration/rebuild steps, readiness and data/query validation
gates, the last safe compatible binary/configuration rollback point, and the owner/removal deadline for any temporary
migration-only compatibility mechanism.

The storage doctor MUST compare live inventory, configuration, and data shape with that manifest without mutation.
An operator MUST approve the exact maintenance plan before resource mutation. Destructive migration or stricter
enforcement MUST remain disabled until backup/restore, supported upgrade, readiness, resource, and query-result proof
passes. Rollback MUST return only to the last manifest-declared compatible binary/configuration while its retained
state is proven readable; after an irreversible boundary, the plan MUST require forward recovery.

Temporary migration compatibility MUST NOT relax canonical validation, become a permissive dual contract, or remain
past its declared removal deadline. A release MUST NOT retain an indefinite legacy reader or dual writer.

#### Scenario: report-only preflight cannot mutate retained resources

- **GIVEN** a post-v1 source/target manifest and retained resources with configuration or data-shape drift
- **WHEN** the storage doctor runs in report-only preflight mode
- **THEN** it reports the exact drift, affected resource, required order, and blocking proof
- **AND** it does not reconfigure, delete, rewrite, rebuild, or enforce a stricter admission rule

#### Scenario: destructive enforcement waits for proven recovery

- **GIVEN** an upgrade would delete, reformat, rebuild, or newly reject retained production state
- **WHEN** backup/restore validation, operator approval, or a required readiness/query gate is missing
- **THEN** the destructive or stricter step remains blocked
- **AND** the diagnostic identifies the missing manifest gate

#### Scenario: rollback uses only the last compatible state

- **GIVEN** the supported real-NATS upgrade has not crossed its manifest-declared irreversible boundary
- **WHEN** validation fails and the operator selects rollback
- **THEN** the retained resources, binary, and configuration return to the last proven compatible point
- **AND** no permissive reader, dual writer, or relaxed validator is enabled

#### Scenario: temporary compatibility expires

- **GIVEN** a migration-only bridge has an owner and removal deadline in the versioned manifest
- **WHEN** the deadline is reached or the target validation gate passes
- **THEN** release remains blocked until the bridge is removed
- **AND** the bridge cannot become an indefinite legacy contract

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
