## ADDED Requirements

### Requirement: Predicate membership keys are raw canonical predicates

PREDICATE_INDEX MUST adopt the fixed-nine-token layout
`domain.category.property.org.platform.domain.system.type.instance`, preserving one membership per key and O(E)
writes. PREDICATE_CATALOG MUST be retired. The complete worst-case key and every constructed filter MUST pass the
`nats-kv-keys` budgets and pinned real-NATS maximum/exact-match conformance. Production activation MUST additionally
pass the replacement lifecycle, CI, sustained-churn, restart, and resource gates. A server, SDK, predicate grammar,
entity-ID bound, or layout change MUST rerun the affected evidence before release. Hash-plus-catalog measurements
MAY be retained as comparative evidence but MUST NOT function as a selection threshold or activation fallback.

#### Scenario: raw keys ship after activation gates pass

- **GIVEN** the worst-case raw key and all filters pass budgets and real-NATS conformance
- **WHEN** the lifecycle fixtures and churn run pass on the raw layout
- **THEN** PREDICATE_INDEX is created with raw nine-token membership keys
- **AND** PREDICATE_CATALOG is absent after cutover

#### Scenario: a changed evidence pin cannot inherit the decision run

- **GIVEN** the selected NATS server digest, `nats.go` version, grammar bound, or physical layout changes
- **WHEN** release evidence is evaluated
- **THEN** the affected maximum, correctness, latency, churn, and resource gates are rerun
- **AND** prior measurements alone cannot activate the changed implementation

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
