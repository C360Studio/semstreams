## ADDED Requirements

### Requirement: Every derived index declares semantic ownership and reconciliation capability

Each derived graph index MUST declare its physical token layout, exact arity when fixed or explicit variable arity,
semantic row owner, literal fixed-arity forward query filter when available or explicit non-filterability,
value-overwrite policy, and update/delete/retirement behavior. It MUST declare either a literal
owner-reconciliation filter or explicit non-filterability with alternate authority deferred to a separate
specification. When a proven bounded owner filter exists, reconciliation MUST enumerate the stored owner rows,
deduplicate them by exact key, diff them against the complete desired projection from current ENTITY_STATES,
delete stale rows, and put missing rows.

Owner-filter reconciliation MUST preserve keyed entity ordering, execution-time authoritative reads, bounded
repair, and readiness withholding on any required failure.

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

### Requirement: Fixed-position owner filtering is proven before production reconciliation activates

PREDICATE, NAME, source-owned INCOMING, and CONTEXT MUST be tested against real NATS using literal exact-arity
forward and owner filters constructed through the `nats-kv-keys` contract. The proof MUST cover filter-string
construction, malformed longer/shorter keys, matching correctness with no false positives, neighboring-owner and
reversed-axis controls, concurrent Put/Delete with exact-key deduplication, cancellation, empty buckets, restart,
and clean bucket recreation. Concurrent-mutation correctness MUST be evaluated only after mutations advance to a
declared final ENTITY_STATES revision and reconciliation reaches that watermark, with zero false matches,
omissions, stale survivors, or ownership violations.

Performance MUST be gated by absolute budgets, not comparison: the ADR-065 CI guard (5,000 hot members, each
operation under 3 seconds) and one sustained-churn run on the 21,000-entity profile at the configured worker shape
and one stress shape, achieving p95 at most 3 seconds, p99 at most 5 seconds, no operation at the 10-second
handler bound, temporary consumers returning to baseline, and no unbounded queue growth. The selected worker
maximum MUST be enforced in validated configuration before activation.

A store that fails correctness or budget MUST defer its cleanup authority to a separately specified bounded
replacement mechanism; that mechanism becomes a completion dependency of this change, and deferral MUST NOT waive
the required `[A] -> [B] -> []` result for any query-visible store.

#### Scenario: a source entity enumerates only its INCOMING assertions

- **GIVEN** INCOMING rows for multiple targets and sources
- **WHEN** the source-axis fixed-position filter is evaluated for one six-part source ID
- **THEN** every matching row is owned by that source assertion
- **AND** no row owned by another source is returned

#### Scenario: unit maxima do not replace real-NATS proof

- **GIVEN** canonical six-part entity IDs are bounded and every entity-bearing unit maximum fits shared budgets
- **WHEN** production activation is evaluated
- **THEN** unit arithmetic and representative data do not authorize activation
- **AND** activation waits for pinned real-NATS maximum key/filter exact-match conformance

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

### Requirement: Activation is gated and starts from canonical fresh state

Production replacement and INCOMING lifecycle behavior MUST remain the documented shipped behavior until the
owner-filter proof passes and the owner-discovery/INCOMING-ownership ADR approves each store's mechanism. When
reconciliation activates, the pre-v1 release MUST first wipe incompatible authoritative and derived NATS graph
resources and reseed canonical owned sources; PREDICATE_INDEX, PREDICATE_CATALOG, NAME_INDEX, and INCOMING_INDEX
MUST initialize from the freshly reseeded ENTITY_STATES behind typed not-ready responses, and readiness MUST stay
false until initial replay reaches the authoritative watermark. This is a fresh-state release contract, not an
upgrade path; no reader recognizes old keys and no export, preservation, or rollback is provided.

#### Scenario: a spike cannot silently activate reconciliation

- **GIVEN** benchmark-only helper code exists in the graph-index package
- **WHEN** its applicable proof or ADR gate is still open
- **THEN** production entity updates and deletes retain the documented shipped behavior
- **AND** no configuration flag or implicit default can activate the candidate path

#### Scenario: current-layout activation starts from canonical fresh state

- **GIVEN** owned sources/configurations/fixtures are canonical and incompatible NATS graph resources were wiped
- **WHEN** reconciliation starts after canonical reseed
- **THEN** the affected derived buckets initialize before readiness
- **AND** no beta authoritative or derived state is read, translated, or preserved

#### Scenario: fresh start has no premature-ready window

- **GIVEN** incompatible NATS state was wiped and graph-index starts before canonical reseed
- **WHEN** initial authoritative replay is incomplete
- **THEN** graph-index readiness remains false
- **AND** affected queries remain not-ready until the initial authoritative watermark is reached
