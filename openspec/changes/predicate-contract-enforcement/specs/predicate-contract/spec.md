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
either one exact domain or one exact `domain.category` pair. A vocabulary package, configuration, rule pack,
schema, or generated tool MAY expose a valid undeclared predicate only when that artifact is bound to the matching
delegation. Delegation is an authoring boundary, not a runtime bearer credential. Registration or delegation MUST
NOT make malformed syntax valid, and neither mechanism grants ownership of facts on a particular entity.

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

#### Scenario: an anonymous mutation does not invent runtime namespace authority

- **GIVEN** a mutation envelope has no authenticated producer principal
- **WHEN** the authoritative persistence seam receives its final candidate
- **THEN** the seam enforces canonical predicate syntax
- **AND** it does not infer namespace authority from source, message type, context, subject, or other caller data
- **AND** endpoint authentication and ordinary graph ownership remain separate controls

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

### Requirement: Predicate test fixtures are canonical or exactly classified negatives

The completed bounded production predicate corpus MUST remain distinct from the complementary tracked corpus over
every `*_test.go` file and every structured artifact beneath `testdata`. Both corpora MUST be clean before local
zero-violation evidence is complete. Positive runtime fixtures SHOULD use the grammar-only
`internal/semantictest` predicate builder. The builder MUST accept all three semantic positions explicitly, MUST join
and validate them through `vocabulary.ParsePredicate` without normalization, aliases, or defaults, and MUST return only
the validated string. It MUST NOT construct graph entities, triples, Graphable values, or other behavior-bearing
fixtures. Production Go files MUST NOT import this test helper. Vocabulary grammar-authority tests and literal
constants MAY remain raw source values, but MUST remain in the checked corpus.

Every intentional invalid predicate fixture MUST be classified at one exact occurrence with its contract kind, exact
value, and authoritative stable reason. A commentless structured fixture MUST use a checked manifest entry naming its
file and structural location or record. File-wide or directory-wide invalid allowances MUST NOT satisfy the corpus.
Missing, stale, duplicate, unmatched, broad, or reason-mismatched classifications MUST fail, and every classification
MUST resolve to exactly one candidate.

#### Scenario: the predicate helper does not normalize malformed positions

- **GIVEN** explicit predicate positions containing uppercase, underscore, or invalid hyphen placement
- **WHEN** the test fixture builder joins and validates them
- **THEN** it fails through `vocabulary.ParsePredicate`
- **AND** it does not lowercase, replace, alias, default, or return a repaired predicate

#### Scenario: production code cannot import the predicate fixture helper

- **GIVEN** a non-test Go file imports `internal/semantictest`
- **WHEN** repository contract checks run
- **THEN** the check fails and identifies the production import
- **AND** a graph-entity or triple factory is not introduced to hide the dependency

#### Scenario: predicate negative classifications match one authoritative reason

- **GIVEN** one malformed predicate occurrence classified with its exact value and authoritative reason
- **WHEN** the test-fixture corpus audit resolves the classification
- **THEN** it accepts the exception only when exactly one candidate matches and parsing returns that reason
- **AND** a missing, stale, duplicate, broad, unmatched, or wrong-reason classification fails the audit

### Requirement: Production non-predicate classifications are occurrence-exact

The production predicate auditor MUST distinguish authoritative
`stored-predicate` candidates from heuristic `predicate-shaped` candidates.
Only a heuristic candidate MAY be classified as unrelated to graph predicates.
Vocabulary registration, lifecycle tags, rule and configuration predicates and
substitutions, and recognized `message.Triple.Predicate` fields MUST remain
authoritative and MUST NOT accept an unrelated classification.

A production classification MUST use
`predicate-audit:classify unrelated "<value>" line=<line> column=<column> surface=<surface> <basis>`.
The containing source file, target line, target column, extraction surface,
exact quoted value, and bounded nonblank basis MUST identify exactly one
candidate. Missing, moved, stale, duplicate, ambiguous, wrong-value, or
wrong-surface classifications MUST fail. A same-value candidate at another
occurrence MUST remain independently audited.

`predicate-audit:allow-invalid` MUST NOT be accepted. Any retained broad
allowance MUST fail the production audit even when it currently matches no
candidate. Intentional malformed stored-predicate fixtures MUST remain under
the complementary test-fixture audit's exact `predicate-audit:invalid`
contract.

The production CLI MUST retain text output as its default and MUST support a
deterministic, versioned JSON report. The JSON report MUST include roots,
candidate, classification, and finding counts; each candidate's authority and
status; each accepted unrelated classification's file, line, column, surface,
value, and basis; and findings with stable codes. A clean corpus MUST exit zero,
contract findings MUST exit one, and invocation, I/O, source-parse, or
report-encoding failures MUST exit two.

#### Scenario: one exact non-predicate occurrence is accepted and reported

- **GIVEN** one heuristic `go-assignment:predicate` candidate carries an exact
  unrelated classification
- **WHEN** the production audit runs in JSON mode
- **THEN** that occurrence produces no invalid-predicate finding
- **AND** the report records its exact locator, value, and review basis

#### Scenario: a second same-value occurrence remains independently audited

- **GIVEN** one exact occurrence of a value is classified as unrelated
- **AND** the same file contains a second candidate with the same value
- **WHEN** the production audit runs
- **THEN** the classification resolves only the named occurrence
- **AND** the second occurrence remains valid or becomes its own finding

#### Scenario: stale or ambiguous classification fails closed

- **GIVEN** an unrelated classification has a moved line or column, wrong
  surface or value, duplicate locator, or locator matching multiple candidates
- **WHEN** the production audit resolves classifications
- **THEN** the audit exits with a stable contract finding
- **AND** no candidate is silently disposed

#### Scenario: stored predicates cannot be classified as unrelated

- **GIVEN** a recognized authoritative stored-predicate occurrence
- **WHEN** an unrelated classification targets that occurrence
- **THEN** the production audit rejects the classification
- **AND** predicate grammar validation still applies to the candidate

#### Scenario: a broad legacy allowance is rejected

- **GIVEN** scanned production source contains
  `predicate-audit:allow-invalid`
- **WHEN** the production audit runs
- **THEN** it reports a stable legacy-broad-allowance finding
- **AND** the marker suppresses no candidate

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

Every component that interprets ENTITY_STATES or serves a derived graph view MUST use the shared canonical decoder
independently of component startup order. On any unreadable entity or predicate violation, projection owners MUST
enter sticky reset-required state, MUST NOT advance readiness across the poisoned revision, and MUST return the
typed reset/reingest requirement. Action/evaluation consumers MUST emit no derived output. Predicate, incoming,
outgoing, traversal, clustering, spatial, temporal, and embedding paths MUST NOT serve partial or briefly ready
views while another component's preflight is pending.

#### Scenario: invalid preexisting state never becomes query-ready

- **GIVEN** ENTITY_STATES contains a noncanonical predicate before components start
- **WHEN** graph-index and graph-ingest start independently in either order
- **THEN** graph-index readiness remains false
- **AND** predicate, incoming, outgoing, traversal, and clustering reads return reset/reingest required

#### Scenario: clean replay can become ready

- **GIVEN** every replayed ENTITY_STATES value satisfies the canonical contract
- **WHEN** graph-index reaches the authoritative replay watermark
- **THEN** ordinary readiness rules may permit graph-index/query consumers to serve results
