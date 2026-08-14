## MODIFIED Requirements

### Requirement: Activation is gated and starts from canonical fresh state

Production replacement and INCOMING lifecycle behavior MUST remain the documented shipped behavior until the
owner-filter proof passes and the owner-discovery/INCOMING-ownership ADR approves each store's mechanism. When
reconciliation activates for the stable release, every downstream adoption MUST start on newly provisioned NATS
storage. Owned sources, configurations, schemas, fixtures, and queries MUST already use the selected layout.

Fresh raw PREDICATE_INDEX, NAME_INDEX, and INCOMING_INDEX buckets MUST initialize from current ENTITY_STATES behind
typed not-ready responses. Readiness MUST remain false until cold replay reaches the authoritative watermark. Retired
PREDICATE_CATALOG and CONTEXT_INDEX buckets MUST be absent. No reader SHALL recognize old keys, and no dual format,
compatibility alias, persistence migration, preservation, online conversion, or rollback path SHALL be provided.
Discovery of retained deployed NATS state MUST stop that adoption and require a separate owner-reviewed migration or
recovery design.

Typed poison recovery and projection rebuild from current authoritative ENTITY_STATES remain unchanged; this release
premise does not convert recovery into a release operation.

#### Scenario: a spike cannot silently activate reconciliation

- **GIVEN** benchmark-only helper code exists in the graph-index package
- **WHEN** its applicable proof or ADR gate is still open
- **THEN** production entity updates and deletes retain the documented shipped behavior
- **AND** no configuration flag or implicit default can activate the candidate path

#### Scenario: selected-layout activation starts from canonical fresh state

- **GIVEN** owned sources, configurations, schemas, fixtures, and queries use the selected layout
- **AND** the downstream provisions new NATS storage with no retired graph buckets
- **WHEN** graph-index starts and cold replay begins
- **THEN** affected queries return typed not-ready until replay reaches the authoritative watermark
- **AND** ready projections contain only current raw-layout truth
- **AND** no old key or compatibility path is read

#### Scenario: fresh start has no premature-ready window

- **GIVEN** newly provisioned NATS storage and graph-index starts before canonical source replay completes
- **WHEN** initial authoritative replay is incomplete
- **THEN** graph-index readiness remains false
- **AND** affected queries remain not-ready until the initial authoritative watermark is reached

#### Scenario: Retained state stops adoption

- **GIVEN** a downstream intends to adopt the stable release
- **WHEN** retained deployed NATS graph state is discovered
- **THEN** adoption stops before the new release is started against that state
- **AND** a separate owner-reviewed migration or recovery design is required
- **AND** the release does not wipe, reseed, translate, preserve, or roll back that state

### Requirement: A key-format cutover never serves mixed partial truth

The selected representation MUST start on newly provisioned NATS storage. Fresh raw buckets MUST initialize behind
typed not-ready responses from canonical ENTITY_STATES, and readiness MUST stay false until initial replay reaches
the authoritative watermark. SemStreams MUST NOT operate a permanent dual-format predicate index, recognize
old-format keys, or provide export, preservation, in-place migration, or rollback. Discovery of retained deployed
state MUST stop the affected adoption for separate owner review.

#### Scenario: cutover holds readiness until the watermark

- **GIVEN** the raw representation starts with newly provisioned NATS storage
- **WHEN** canonical source replay is incomplete
- **THEN** predicate queries return typed not-ready rather than partial results
- **AND** once ready they match canonical fresh-state fixtures without reading abandoned keys

#### Scenario: a closed window halts the cutover

- **GIVEN** retained deployed graph state is present
- **WHEN** adoption is evaluated
- **THEN** the adoption stops
- **AND** no second format reader, automatic deletion, translation, or migration is activated

### Requirement: Context provenance remains authoritative without a durable context index

Graph-index MUST NOT create, open, write, reconcile, or publish readiness for a CONTEXT_INDEX bucket. Triple
provenance MUST remain stored on each authoritative triple through `Triple.Context` in ENTITY_STATES; retiring the
derived bucket MUST NOT erase or rewrite that fact.

Stable-release adoption MUST use newly provisioned NATS storage on which CONTEXT_INDEX is absent. No legacy bucket
reader, alias, translation, dual write, or online migration is provided. Retained deployed state stops adoption for
separate owner review; typed poison recovery remains separately scoped.

#### Scenario: fresh graph-index startup creates no context bucket

- **GIVEN** newly provisioned NATS storage with no CONTEXT_INDEX
- **WHEN** graph-index starts and reaches readiness
- **THEN** CONTEXT_INDEX remains absent
- **AND** the surviving query indexes initialize normally

#### Scenario: hierarchy provenance remains on authoritative triples

- **GIVEN** a hierarchy triple with canonical context provenance
- **WHEN** the authoritative entity state is stored and replayed
- **THEN** the triple retains its `Triple.Context`
- **AND** no durable context index is needed to preserve provenance
