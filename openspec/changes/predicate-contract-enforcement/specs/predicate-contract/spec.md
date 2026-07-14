## ADDED Requirements

### Requirement: Every stored graph predicate has one canonical three-segment syntax

A predicate MUST parse as exactly three segments, `domain.category.property`. Each segment MUST match
`[a-z][a-z0-9]*(-[a-z0-9]+)*`, each segment MUST be no longer than 64 ASCII bytes, and the complete
predicate MUST be no longer than 194 bytes including the two dots. Uppercase, underscore, wildcard tokens,
whitespace, slash, control characters, empty segments, and values beyond the bounds MUST NOT be valid stored
predicates. One authoritative parser MUST return typed components and a stable failure reason; other
validators MUST delegate to it.

The complete predicate string is the exact semantic identity. Sharing a domain or category creates a query
namespace, not aliasing, equivalence, ownership, or write authority.

#### Scenario: a canonical predicate parses into semantic positions

- **GIVEN** a predicate conforming to the canonical grammar
- **WHEN** the canonical parser reads it
- **THEN** it returns exactly one domain, category, and property
- **AND** re-serializing those components returns the exact predicate identity

#### Scenario: query wildcard syntax is not stored as a predicate

- **WHEN** a writer supplies a predicate containing `*` or `>` as a segment
- **THEN** structural validation rejects it with a typed syntax reason
- **AND** no graph state is mutated

### Requirement: Vocabulary declaration and namespace authority are explicit and separate from syntax

Every declared vocabulary predicate MUST satisfy the canonical syntax. A namespace delegation MUST name
either one exact domain or one exact `domain.category` pair. A valid but undeclared predicate MAY be accepted
only when its producer holds the matching delegation. Registration or delegation MUST NOT make malformed
syntax valid, and neither mechanism grants ownership of facts on a particular entity.

Agent and generated-tool authoring surfaces MUST expose declared predicates or delegated namespaces rather
than accept an unrestricted predicate string.

#### Scenario: registration cannot bless malformed syntax

- **GIVEN** startup registers a predicate outside the canonical grammar
- **WHEN** vocabulary validation runs
- **THEN** startup/configuration fails with the predicate and structural reason

#### Scenario: a product uses its delegated vocabulary namespace

- **GIVEN** a product has declared authority for a namespace
- **WHEN** it writes a syntactically valid predicate in that namespace through an authorized lane
- **THEN** predicate declaration policy accepts the name
- **AND** ordinary graph ownership rules still decide whether the fact mutation is authorized

### Requirement: Canonical predicate enforcement is unconditional

Every declared authoring surface and every final ENTITY_STATES candidate MUST reject predicates outside the
canonical grammar. SemStreams MUST NOT expose a permissive runtime mode, compatibility alias, deprecated
predicate table, dual read/write path, or configuration escape hatch. One structured rejection MUST include
every unique invalid predicate/reason in the candidate; metrics MUST count each unique bounded reason once
without entity or predicate labels.

#### Scenario: every lane rejects the same malformed predicate

- **GIVEN** any Graphable, mutation, rule, inference, direct-adapter, batch, or repair lane
- **WHEN** its final candidate contains a noncanonical predicate
- **THEN** the authoritative gate rejects the candidate before persistence
- **AND** the lane returns the same typed structural reason

#### Scenario: runtime configuration cannot disable enforcement

- **WHEN** a deployment loads graph-ingest configuration
- **THEN** no option exists to accept noncanonical predicates

### Requirement: The beta cutover updates owned producers and resets incompatible state

The breaking release MUST update every SemStreams producer, owned reference design, generated schema/tool
surface, exact query, and participating owned sister repository to the canonical contract. The release
MUST publish an exact source/configuration rename ledger, but that ledger MUST NOT be loaded as a runtime
alias or transformation table.

Existing ENTITY_STATES containing a noncanonical predicate MUST block readiness with an error requiring the
operator to export if needed, clear incompatible graph/index buckets, and reingest from canonical sources.
SemStreams MUST NOT rewrite malformed beta state in place. Queries remain not-ready until clean reingest and
index replay reach the authoritative watermark.

#### Scenario: incompatible beta state requires a clean reset

- **GIVEN** an existing ENTITY_STATES bucket containing a noncanonical predicate
- **WHEN** the breaking SemStreams binary starts
- **THEN** graph readiness is refused with reset/reingest instructions
- **AND** no compatibility reader or in-place transformer accepts the old state

#### Scenario: clean reingest exposes only canonical identities

- **GIVEN** incompatible graph/index buckets have been cleared
- **WHEN** owned canonical sources are reingested and index replay completes
- **THEN** every stored predicate satisfies the canonical grammar
- **AND** query results contain no deprecated predicate identity

### Requirement: Every authoritative replay consumer withholds readiness on incompatible state

Every component that replays ENTITY_STATES or serves a derived graph view MUST validate each replayed entity
with the canonical predicate parser independently of component startup order. On any violation, graph-index
MUST mark the entity failed, MUST NOT advertise ready, and MUST return the typed reset/reingest requirement
from predicate, incoming, outgoing, traversal, and clustering paths. No partial or briefly ready view is
permitted while another component's preflight is pending.

#### Scenario: invalid preexisting state never becomes query-ready

- **GIVEN** ENTITY_STATES contains a noncanonical predicate before components start
- **WHEN** graph-index and graph-ingest start independently in either order
- **THEN** graph-index readiness remains false
- **AND** predicate, incoming, outgoing, traversal, and clustering reads return reset/reingest required

#### Scenario: clean replay can become ready

- **GIVEN** every replayed ENTITY_STATES value satisfies the canonical contract
- **WHEN** graph-index reaches the authoritative replay watermark
- **THEN** ordinary readiness rules may permit graph-index/query consumers to serve results
