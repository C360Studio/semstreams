## ADDED Requirements

### Requirement: Every derived index declares semantic ownership and fixed-position reconciliation

Each derived graph index MUST declare its physical token layout and arity, semantic row owner, exact forward
query filter, owner-reconciliation filter, and update/delete/retirement behavior. When a proven bounded owner
filter exists, reconciliation MUST enumerate the stored owner rows, deduplicate them, diff them against the
complete desired projection from current ENTITY_STATES, delete stale rows, and put missing rows.

Owner-filter reconciliation MUST preserve keyed entity ordering, execution-time authoritative reads,
bounded repair, and readiness withholding on any required failure.

#### Scenario: removing the final membership retracts the stored row

- **GIVEN** an entity whose stored owner projection contains one membership
- **WHEN** current ENTITY_STATES yields an empty desired projection for that membership index
- **THEN** reconciliation deletes the stale row
- **AND** queries do not return the former membership

#### Scenario: duplicate filtered results do not duplicate work or query results

- **GIVEN** filtered enumeration observes the same key more than once during concurrent mutation
- **WHEN** reconciliation computes the stored owner set
- **THEN** it deduplicates by exact key before diffing

### Requirement: Fixed-position owner filtering is proven before it replaces a manifest

PREDICATE, NAME, source-owned INCOMING, and CONTEXT MUST be tested against real NATS using their exact
fixed-position owner filters. The proof MUST cover matching correctness, no false positives, concurrent
Put/Delete, duplicate handling, cancellation, clean bucket recreation, realistic hot/fanout load, and bounded
resource cost. A store that fails correctness or the approved budget MUST use a separately specified owner manifest or
tombstone payload rather than claiming self-cleanup.

The versioned decision profile MUST include a 5,000-hot-member CI guard with a 3-second operation limit and a
21,000-entity full profile with a full INCOMING hub, one all-entity predicate, and 5,000-member NAME/CONTEXT
hotspots. After five warmups, 30 measured repetitions MUST achieve p95 at most 3 seconds, p99 at most 5
seconds, no operation at the 10-second handler bound, and no more than twice the client allocation, server CPU,
or server RSS delta of an owner-manifest baseline. Any false match, omission, stale survivor, or ownership
violation fails the store. A passing store MUST prefer filtered reconciliation to avoid another durable write
structure; a failing store MUST declare its alternate authority.

#### Scenario: a source entity enumerates only its INCOMING assertions

- **GIVEN** INCOMING rows for multiple targets and sources
- **WHEN** the source-axis fixed-position filter is evaluated for one six-part source ID
- **THEN** every matching row is owned by that source assertion
- **AND** no row owned by another source is returned

### Requirement: Predicate membership representation is selected by a real-NATS decision gate

The final PREDICATE_INDEX representation MUST be selected by comparing the current
`hash(predicate).entityID` plus required catalog against the canonical fixed-nine-token
`domain.category.property.entityID`. Both candidates MUST preserve one membership per key and O(E) writes.
The decision MUST compare exact/namespace query latency, owner reconciliation, watch semantics, storage and
resource cost, catalog consistency/failure behavior, and operational inspection.

The selected result MUST be recorded in a superseding ADR. SemStreams MUST NOT operate a permanent
dual-format predicate index. Any cutover MUST delete/recreate the selected bucket and replay freshly
reingested canonical ENTITY_STATES while reads remain not-ready. No reader may recognize the old key format.

#### Scenario: a key-format cutover never serves mixed partial truth

- **GIVEN** the decision selects a new PREDICATE_INDEX representation
- **WHEN** bucket recreation and clean replay perform the cutover
- **THEN** query readiness remains false until the selected representation reaches its authoritative watermark
- **AND** predicate queries never combine partial old and new formats

### Requirement: Predicate key codecs do not weaken the canonical grammar

Hex or hash encoding MAY remain as a storage codec for a declared index axis, but it MUST NOT justify
accepting a predicate that violates the canonical predicate contract. If PREDICATE_CATALOG remains, its raw
keys MUST be valid under the canonical grammar and its update MUST be a required repaired projection whose
failure withholds readiness. If raw predicate membership keys are selected, the catalog MUST be retired after
cutover.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded into a KV-safe token but violates canonical syntax
- **WHEN** graph state is written in enforcement mode
- **THEN** predicate validation rejects it before graph-index processing

### Requirement: INCOMING rows are retracted by their source owner

An INCOMING row MUST be treated as evidence owned by its source entity. Source update or death
MUST retract stale source-axis rows. Logical retirement of the target MUST NOT delete live source assertions
merely because the target occupies the physical key prefix. The target-prefix hard-delete behavior MUST be
removed rather than preserved as a compatibility path.

#### Scenario: target retirement preserves live source evidence

- **GIVEN** a live source still asserts a relationship to a target
- **WHEN** the target is logically retired
- **THEN** the source-owned INCOMING assertion remains available to retention/query policy
- **AND** it is removed only when the source retracts it or an authorized cascade changes the source fact
