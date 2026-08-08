# graph-retention — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: The live graph carries no lifecycle retention

Live graph KV buckets MUST NOT use NATS TTL or binding `MaxBytes` as a lifecycle mechanism. This includes current
authority, every derived index, `GRAPH_INGEST_APPLIED_SEQ`, and `GRAPH_STATUS`. The descriptor catalog MUST NOT contain
`OWNER_CLAIMS`, `OWNER_PRESENCE`, or `PENDING_EDGES`. Acquisition
MUST reconcile and verify each declared retention policy without changing unrelated stream configuration; the
owner-absent legacy-drift backstop remains limited to cataloged buckets that exist.

#### Scenario: The live catalog has no ownership retention exception

- **WHEN** a post-cutover deployment enumerates live graph descriptors
- **THEN** no owner-claim, owner-presence, or pending-edge bucket is present
- **AND** every remaining correctness-critical graph bucket has its declared no-eviction policy

### Requirement: Framework-owned buckets reject generic KV writes

Framework graph buckets MUST remain protected from generic KV actions by cataloged physical writer responsibility.
`ENTITY_STATES` writes flow only through graph-ingest's Graphable ingest or typed mutation port. This physical/catalog
ownership MUST NOT be interpreted as semantic predicate authorization and requires no claims, leases, or tokens.

#### Scenario: Physical writer responsibility survives semantic ownership deletion

- **GIVEN** a generic KV action targets `ENTITY_STATES`
- **WHEN** catalog validation runs
- **THEN** the generic write is rejected
- **AND** rejection does not consult `pkg/ownership`

### Requirement: Framework KV buckets are acquired through a declared descriptor catalog

The descriptor catalog MUST retain current authority, derived indexes, readiness, guards, and operational stores while
removing `OWNER_CLAIMS`, `OWNER_PRESENCE`, and the declaration-only `PENDING_EDGES` spelling. Existing live-graph no-TTL
and retention policies remain unchanged.

#### Scenario: Ownership buckets are absent after clean cutover

- **GIVEN** a freshly seeded post-cutover deployment
- **WHEN** framework bucket descriptors are enumerated
- **THEN** no owner-claim, owner-presence, or pending-edge descriptor exists
- **AND** `ENTITY_STATES` and `GRAPH_STATUS` retain their existing policies
