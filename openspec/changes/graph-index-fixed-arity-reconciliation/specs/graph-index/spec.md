## ADDED Requirements

### Requirement: Every derived index declares semantic ownership and reconciliation capability

Each derived graph index MUST declare its physical token layout, exact arity when fixed or explicit variable arity,
semantic row owner, literal fixed-arity forward query filter when available or explicit non-filterability,
value-overwrite policy, and update/delete/retirement behavior. It MUST declare either a literal
owner-reconciliation filter or explicit non-filterability with alternate authority deferred to a separate
specification. When a proven bounded owner filter exists, reconciliation MUST enumerate the stored owner rows,
deduplicate them, diff them against the complete desired projection from current ENTITY_STATES, delete stale rows,
and put missing rows.

Owner-filter reconciliation MUST preserve keyed entity ordering, execution-time authoritative reads,
bounded repair, and readiness withholding on any required failure.

#### Scenario: removing the final membership retracts the stored row

- **GIVEN** an entity whose stored owner projection contains one membership
- **WHEN** current ENTITY_STATES yields an empty desired projection for that membership index
- **THEN** reconciliation deletes the stale row
- **AND** queries do not return the former membership

#### Scenario: changing a membership retracts the former row

- **GIVEN** an entity whose stored owner projection contains membership A
- **WHEN** current ENTITY_STATES yields membership B instead
- **THEN** reconciliation deletes A and writes B as one required projection
- **AND** public queries do not return the entity through A after the watermark

#### Scenario: predicate replacement reaches empty

- **GIVEN** an entity uses predicate A
- **WHEN** its current projection changes to predicate B and then contains neither predicate
- **THEN** PREDICATE_INDEX contains only B at the first watermark and neither membership at the second

#### Scenario: name replacement overwrites stable-key metadata

- **GIVEN** an entity is named Alpha through one display-name predicate
- **WHEN** it changes to Beta and then removes the display name
- **THEN** NAME_INDEX retracts Alpha, exposes Beta, and finally contains no membership for the entity
- **AND** a case or priority change whose normalized key stays stable overwrites the stored value

#### Scenario: relationship replacement retracts source-owned incoming rows

- **GIVEN** a source relates to target A
- **WHEN** its relationship changes to target B and then to no target
- **THEN** INCOMING_INDEX contains only the source-owned B row at the first watermark and none at the second

#### Scenario: context replacement reaches empty

- **GIVEN** an entity has context membership A
- **WHEN** its context changes to B and then to no context
- **THEN** the entity-owned CONTEXT_INDEX set contains only B and then becomes empty

#### Scenario: outgoing replacement remains complete

- **GIVEN** an entity points to target A
- **WHEN** its outgoing projection changes to target B and then to empty
- **THEN** OUTGOING_INDEX replaces the complete array at both watermarks without a phantom edge

#### Scenario: duplicate filtered results do not duplicate work or query results

- **GIVEN** filtered enumeration observes the same key more than once during concurrent mutation
- **WHEN** reconciliation computes the stored owner set
- **THEN** it deduplicates by exact key before diffing

### Requirement: Fixed-position owner filtering is proven before production reconciliation is selected

PREDICATE, NAME, source-owned INCOMING, and CONTEXT MUST be tested against real NATS using literal exact-arity
forward and owner filters. The proof MUST cover filter-string construction, malformed longer/shorter keys,
matching correctness, no false positives, concurrent
Put/Delete, duplicate handling, cancellation, clean bucket recreation, realistic hot/fanout load, and bounded
resource cost. Concurrent mutation correctness MUST be evaluated only after mutations advance to a declared final
ENTITY_STATES revision and reconciliation reaches that watermark. A store that fails correctness or the approved
budget MUST defer its alternate cleanup authority to a separate dependent specification rather than claiming
self-cleanup. For every affected query-visible store, such deferral MUST block query-parity acceptance and archive
of this change until the alternate bounded replacement mechanism is approved and implemented. Filter failure MUST
NOT waive the required `[A] -> [B] -> []` result.

Every key/filter newly constructed by this proof or later activation implementation MUST pass the baseline
`nats-kv-keys` literal-token, literal-key, wildcard-filter, arity, and byte-budget contract before NATS I/O.
Graph-index MUST consume that contract's stable classified errors and MUST NOT copy or weaken its syntax. The
prerequisite MUST NOT be treated as authorization to change existing KV wrapper behavior globally.

The proof MUST cover worst-case formulas for every current PR #524 key, token, and filter: PREDICATE,
PREDICATE_CATALOG, NAME, CONTEXT, INCOMING, OUTGOING, and ALIAS. Representative corpus success MUST NOT substitute for
a governed maximum for an in-scope store. The canonical entity-ID contract fixes `E = 256` and the unit matrix proves
bounded entity-bearing maxima of PREDICATE 321, NAME/CONTEXT 710, INCOMING 902, OUTGOING 256, and raw PREDICATE 451
bytes. Current-layout activation MUST remain blocked until maximum complete keys/filters and exact match sets pass
pinned real-NATS conformance. ALIAS's representative raw key is not a governed maximum; its result remains owned by
the separate ALIAS change and does not block unrelated stores.

The versioned decision profile MUST include a 5,000-hot-member CI guard with a 3-second operation limit and a
21,000-entity full profile with a full INCOMING hub, one all-entity predicate, and 5,000-member NAME/CONTEXT
hotspots. After five warmups, 30 measured repetitions and a sustained run at configured graph-index worker
concurrency MUST achieve p95 at most 3 seconds, p99 at most 5 seconds, and no operation at the 10-second handler
bound. Client allocation, server CPU, and server RSS delta MUST each remain no more than twice a benchmark-only
owner-manifest baseline. Profiles MUST cover representative one- and four-worker shapes, a 16-worker stress shape,
and a preselected maximum-supported-worker candidate. The approved maximum MUST be enforced by configuration before
production activation. Any false match, omission, stale survivor, ownership violation, unbounded queue/resource
growth, or temporary-consumer leak fails the store. The superseding owner-discovery ADR MUST select between
filtered reconciliation and a separately specified alternative using the complete lifecycle and resource evidence;
passing a per-call latency bound alone does not select the architecture.

#### Scenario: a source entity enumerates only its INCOMING assertions

- **GIVEN** INCOMING rows for multiple targets and sources
- **WHEN** the source-axis fixed-position filter is evaluated for one six-part source ID
- **THEN** every matching row is owned by that source assertion
- **AND** no row owned by another source is returned

### Requirement: Predicate membership representation is selected by a real-NATS decision gate

The final PREDICATE_INDEX representation MUST be selected by comparing the current
`hash(predicate).entityID` plus required catalog against the canonical fixed-nine-token
`domain.category.property.entityID`. Both candidates MUST preserve one membership per key and O(E) writes.
The decision MUST compare exact/namespace query latency, owner reconciliation, storage and resource cost, key
length against maximum predicate and entity-ID budgets, catalog consistency/failure behavior, and operational
inspection. Watch semantics MUST be compared only when a current consumer is identified; this change MUST NOT add
a public watch API solely to favor one representation.

Hash plus catalog MUST remain selected when both candidates satisfy the required contracts. Before measurement,
each eligible public operation MUST receive a numeric or mechanically decidable material-improvement threshold.
Raw keys MAY replace hash plus catalog only when their worst-case key is bounded and they cross one of those
thresholds for a required public query or proven consumer.

The selected result MUST be recorded in a superseding ADR. SemStreams MUST NOT operate a permanent dual-format
predicate index. The pre-v1 cutover MUST announce the selected format, update every owned source/configuration/
fixture, wipe all incompatible authoritative and derived NATS graph state, restart, and reseed canonical sources.
Fresh derived buckets MUST initialize behind typed not-ready responses. No reader may recognize the old key format,
and this change MUST NOT provide export, old-state inspection/preservation, in-place migration, or rollback.

#### Scenario: a key-format cutover never serves mixed partial truth

- **GIVEN** the decision selects a new PREDICATE_INDEX representation
- **WHEN** the clean wipe/reseed and fresh derived initialization perform the cutover
- **THEN** query readiness remains false until the selected representation reaches its authoritative watermark
- **AND** predicate queries never combine partial old and new formats

### Requirement: Predicate key codecs do not weaken the canonical grammar

Hex or hash encoding MAY remain as a storage codec for a declared index axis, but it MUST NOT justify
accepting a predicate that violates the canonical predicate contract. If PREDICATE_CATALOG remains, its raw
keys MUST be valid under the canonical grammar. Catalog membership and name recovery MUST be a required repaired
projection whose failure withholds readiness. The physical catalog MAY remain monotonic, but query-visible catalog
results MUST include only predicates with current memberships. If raw predicate membership keys are selected, the
catalog MUST be retired after cutover.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded into a KV-safe token but violates canonical syntax
- **WHEN** graph state is written in enforcement mode
- **THEN** predicate validation rejects it before graph-index processing

#### Scenario: the final predicate member is removed

- **GIVEN** the hash representation remains active and one entity is a predicate's final member
- **WHEN** that entity retracts the predicate
- **THEN** membership and query-visible catalog results converge without a lost-add race
- **AND** predicate-list does not report a zero-member historical name

### Requirement: INCOMING rows are retracted by their source owner

An INCOMING row MUST be treated as evidence owned by its source entity. Source fact replacement and source entity
removal/tombstone MUST retract stale source-axis rows. Logical retirement, removal, or tombstone of the target MUST
NOT delete assertions still owned by live sources merely because the target occupies the physical key prefix. The
target-prefix hard-delete behavior MUST be removed rather than preserved as a compatibility path.

#### Scenario: target lifecycle preserves live source evidence

- **GIVEN** a live source still asserts a relationship to a target
- **WHEN** the target is logically retired, removed, or tombstoned without changing the source fact
- **THEN** the source-owned INCOMING assertion remains available to retention/query policy
- **AND** it is removed only when the source retracts it or an authorized cascade changes the source fact

#### Scenario: source removal retracts reciprocal rows

- **GIVEN** one source owns INCOMING assertions across several targets
- **WHEN** the source entity is removed or tombstoned
- **THEN** every row owned by that source is retracted through the selected bounded source-owned mechanism
- **AND** unrelated source assertions to the same targets remain

### Requirement: Framework activation follows independent decision gates

Current-layout replacement and INCOMING lifecycle behavior MUST remain unchanged until each affected store either
passes the registered owner-filter profile and is selected by the owner-discovery/INCOMING-ownership ADR or has an
approved and implemented dependent bounded replacement mechanism. Each selected mechanism MUST be covered by the
same readiness contract before activation. Every affected in-scope layout and filter MUST pass the `E = 256` unit
budgets plus maximum real-NATS key/filter and exact-match proof. ALIAS remains
frozen current behavior and separately owned; its unresolved bound or codec does not block unrelated stores. Once
those current-layout prerequisites pass, the correctness changes MUST NOT wait for the optional raw-key decision.
Physical PREDICATE key/catalog and bucket cutover behavior MUST remain unchanged until the separate representation
benchmark and ADR select a format. Benchmark helpers and tests MAY exercise fixed-position reconciliation before
either decision.

#### Scenario: unit maxima do not replace real-NATS proof

- **GIVEN** canonical six-part entity IDs are bounded at 256 bytes and every entity-bearing unit maximum fits
- **WHEN** current-layout activation is evaluated
- **THEN** unit arithmetic and representative data do not authorize activation
- **AND** activation waits for pinned real-NATS maximum key/filter exact-match conformance

When current-layout reconciliation activates, the pre-v1 release MUST first wipe incompatible authoritative and
derived NATS graph resources and reseed canonical owned sources. PREDICATE_INDEX, PREDICATE_CATALOG, NAME_INDEX, and
INCOMING_INDEX MUST initialize from the freshly reseeded ENTITY_STATES behind typed not-ready responses. This is a
fresh-state release contract, not an upgrade path for preserved beta state.

Graph-index MUST start with readiness false before canonical reseed and keep affected queries typed not-ready until
initial replay reaches the authoritative watermark. Generation-based maintenance rebuild is outside this pre-v1
cutover and remains owned by `bounded-storage-operability` tasks 5.1-5.4.

#### Scenario: a spike cannot silently activate reconciliation

- **GIVEN** benchmark-only helper code exists in the graph-index package
- **WHEN** that helper's applicable benchmark or ADR gate is still open
- **THEN** production entity updates and deletes retain the documented shipped behavior
- **AND** no configuration flag or implicit default can activate the candidate path

#### Scenario: current-layout activation starts from canonical fresh state

- **GIVEN** owned sources/configurations/fixtures are canonical and incompatible NATS graph resources were wiped
- **WHEN** current-layout reconciliation starts after canonical reseed
- **THEN** the affected derived buckets initialize before readiness
- **AND** no beta authoritative or derived state is read, translated, or preserved

#### Scenario: fresh start has no premature-ready window

- **GIVEN** incompatible NATS state was wiped and graph-index starts before canonical reseed
- **WHEN** initial authoritative replay is incomplete
- **THEN** graph-index readiness remains false
- **AND** affected queries remain not-ready until the initial authoritative watermark is reached

### Requirement: Raw predicate keys fit a complete supported key budget

The raw fixed-nine-token candidate MUST NOT be selected until its worst-case key, using the maximum canonical
predicate and maximum supported six-part entity ID, passes the baseline `nats-kv-keys` literal-token, literal-key,
arity, and byte budgets and its real-NATS boundary proof. The canonical `E = 256` bound proves a 451-byte unit maximum;
selection remains blocked until pinned real-NATS maximum-key conformance passes.

#### Scenario: maximum supported key is measured before selection

- **GIVEN** the 194-byte maximum canonical predicate and maximum supported entity ID
- **WHEN** the raw candidate is evaluated
- **THEN** its complete key is tested against the versioned NATS budget and server
- **AND** an unknown or exceeded bound prevents Candidate B from winning
