## ADDED Requirements

### Requirement: Predicate membership keys are raw canonical predicates unless an absolute gate fails

PREDICATE_INDEX MUST adopt the fixed-nine-token layout
`domain.category.property.org.platform.domain.system.type.instance`, preserving one membership per key and O(E)
writes, unless a named absolute gate fails. The gates are: the complete worst-case key and every constructed
filter pass the `nats-kv-keys` budgets and pinned real-NATS maximum/exact-match conformance; the replacement
lifecycle fixtures pass on the raw layout; and the ADR-065 CI guard plus one sustained-churn run meet the absolute
latency and resource budgets. A comparative benchmark against hash+catalog MUST be recorded as ADR evidence but
MUST NOT function as a selection threshold. On any gate failure, the ADR MUST record the specific failed gate and
retain hash+catalog as the documented fallback.

#### Scenario: gates pass and raw keys ship

- **GIVEN** the worst-case raw key and all filters pass budgets and real-NATS conformance
- **WHEN** the lifecycle fixtures and churn run pass on the raw layout
- **THEN** the superseding ADR adopts raw keys
- **AND** PREDICATE_CATALOG is retired after cutover

#### Scenario: a failed gate falls back with a recorded reason

- **GIVEN** one named absolute gate fails on the raw layout
- **WHEN** the representation ADR is written
- **THEN** it records the specific failed gate and evidence
- **AND** hash+catalog remains with the catalog's consistency obligations intact

#### Scenario: namespace filters do not over-match

- **GIVEN** predicates `alpha.beta.x` and `alpha.betax.y` have current memberships
- **WHEN** the `alpha.beta` namespace filter is evaluated
- **THEN** only `alpha.beta.x` memberships are returned

### Requirement: A key-format cutover never serves mixed partial truth

The selected representation MUST cut over through the announced pre-v1 wipe/reseed. Fresh raw buckets MUST
initialize behind typed not-ready responses from freshly reseeded canonical ENTITY_STATES, and readiness MUST stay
false until initial replay reaches the authoritative watermark. SemStreams MUST NOT operate a permanent dual-format
predicate index, recognize old-format keys, or provide export, preservation, in-place migration, or rollback. If
the pre-v1 wipe window has closed, this change MUST NOT execute; it converts to an explicit post-v1 migration
proposal.

#### Scenario: cutover holds readiness until the watermark

- **GIVEN** the raw representation is selected and incompatible NATS state was wiped
- **WHEN** canonical reseed and initial replay are incomplete
- **THEN** predicate queries return typed not-ready rather than partial results
- **AND** once ready they match canonical fresh-state fixtures without reading abandoned keys

#### Scenario: a closed window halts the cutover

- **GIVEN** the announced pre-v1 wipe has already executed without the raw buckets
- **WHEN** this change's cutover is evaluated
- **THEN** no second wipe is performed
- **AND** the ADR records the missed window and the change is re-filed as a post-v1 migration proposal

### Requirement: Non-predicate codecs keep their recorded rationale

NAME and CONTEXT MUST keep hashed keys for their open-content axes, and NAME, CONTEXT, and INCOMING MAY keep the
reversible `hex(predicate)` single-token codec; each keep MUST be recorded with its rationale in the
representation ADR and revisited only on a demonstrated query or operational need. No codec MAY be treated as
acceptance authority for a predicate that violates the canonical grammar.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded into a KV-safe token but violates canonical syntax
- **WHEN** graph state is written in enforcement mode
- **THEN** predicate validation rejects it before graph-index processing
