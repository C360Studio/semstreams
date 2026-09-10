# predicate-contract Specification

## Purpose

`predicate-contract` governs **what a predicate string may be, who may author one, and what the
system refuses to infer from it**. A predicate is the vocabulary of the graph — every triple carries
one — so its grammar is a cross-cutting contract rather than a component detail.

Three things live here.

**Grammar.** Every registered predicate must be syntactically valid, checked at the earliest surface
that can check it. Validation happens at declaration and configuration time — vocabulary
registration, rule conditions and actions, generated tool schemas — because a malformed predicate
caught at startup is an operator error, while the same predicate caught at runtime is a data
incident.

**The beta cutover.** The canonical grammar was a breaking change. State written under the old one
is not migrated in place: it surfaces as typed `graph_state_reset_required` poison, projection and
replay consumers block whole-view readiness, and the authoritative surface refuses exactly the
poisoned entities while continuing to serve valid state. Repair is the operator delete/reset path,
never an in-process transformer — a compatibility reader would make the old grammar permanent.

**What authority a predicate does NOT confer.** This is the part that is easiest to get backwards.
Authoring delegation is declaration-time governance: it constrains what rules, dispatch, and tools
may *author*. It is **not** a bearer credential, and configuration-time checks are **not** runtime
authorization. The system therefore states two prohibitions rather than an exemption: an agent tool
MUST NOT accept a caller-controlled predicate, and no component may infer authority from
caller-supplied content on the declared `graph.mutation.>` request family.

Those are prohibitions deliberately. Access to the mutation lanes *is* the trust boundary — NATS
authentication is connection-level, no principal concept exists in the graph, and any publisher can
write any triple. Saying "the graph is not required to authenticate writers" would have been
satisfiable while a tool handed a model the power to mint `agent.lineage.*`; an exemption describes
what a component need not do and leaves the dangerous act permitted. Deployments needing separation
between writers enforce it with NATS subject permissions, which is the layer that actually holds
identity.

**What it does NOT cover.** Index encoding and key representation (`nats-kv-keys`, `graph-index`).
Entity identity (`entity-id-contract`). Provenance — recording what the system believes about a
triple's origin — belongs to gh#692's lane, and is a different question from authorization. A
principal-bearing mutation envelope is deferred with explicit trigger conditions (gh#802); no
current graph contract depends on predicted per-predicate writer identity.
## Requirements
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
- **AND** the selected mutation operation and observed Create/CAS outcome decide whether it lands

#### Scenario: an anonymous mutation does not invent runtime namespace authority

- **GIVEN** a mutation envelope has no authenticated producer principal
- **WHEN** the authoritative persistence seam receives its final candidate
- **THEN** the seam enforces canonical predicate syntax
- **AND** it does not infer namespace authority from source, message type, context, subject, or other caller data
- **AND** endpoint authentication and operation validation remain separate controls

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

### Requirement: An agent tool MUST NOT accept a caller-controlled predicate
A tool exposed to a model MUST construct any predicate it writes internally, and MUST NOT accept a
predicate, triple, or equivalent grammar-bearing value from the model's tool input.

This is stated as a PROHIBITION on the tool surface, deliberately, and not as an exemption of the
graph seam. "The graph is not required to authenticate writers" would be satisfiable while a tool
happily handed a model the power to mint `agent.lineage.*` — an exemption describes what a component
need not do, and leaves the dangerous act permitted. The enforceable statement is what a tool MUST
NOT do.

The scope of the rule follows from what is actually enforceable. The model is the only
semi-trusted principal in the system: it emits tool calls whose arguments it chooses. Every other
writer of `graph.mutation.>` is infrastructure holding NATS credentials, and is inside the trust
boundary (see the requirement below). So the tool surface is where a real boundary exists, and it is
the surface this rule binds.

Compliance MUST be verified against the tool REGISTRY rather than a maintained list of tool names,
so a tool added later is covered without anyone remembering, and the verification MUST itself be
shown capable of failing.

#### Scenario: a graph-writing tool constructs its own predicate

- **GIVEN** a tool that writes to the graph on the model's behalf
- **WHEN** its input schema is inspected
- **THEN** no property conveys a predicate, triple, or equivalent grammar-bearing value
- **AND** the predicate it writes is determined by the tool implementation

#### Scenario: the verification is shown capable of failing

- **GIVEN** a registry containing a tool that DOES accept a caller-controlled predicate
- **WHEN** the registry audit runs
- **THEN** it reports that tool as a violation
- **AND** a clean result on the real registry is therefore evidence rather than an absence of looking

### Requirement: Mutation-lane access MUST be treated as the trust boundary, not as authenticated identity
A component MUST NOT infer namespace authority, principal identity, or write privilege from
caller-supplied triple content, message fields, or subject naming on `graph.mutation.>`.

The graph seam authenticates no principal, and no principal concept exists to authenticate: NATS
authentication is connection-level, and the mutation lanes accept any triple from any publisher.
Access to those lanes is therefore itself the trust boundary. Deployments requiring separation
between writers MUST enforce it with NATS subject permissions, which is the layer that actually
holds identity.

Deployment guidance: restrict publish permissions on the canonical mutation family —
`graph.mutation.entity.create`, `graph.mutation.entity.reconcile`, `graph.mutation.triple.append`, and
`graph.mutation.entity.delete` — to identities trusted with graph writes. A subscriber-only consumer
needs none of them. NATS permissions identify who may publish; the operation schema validates what
may land. Neither is semantic predicate ownership, and an authenticated caller still observes real
Create/CAS conflicts.

Stating this is what prevents an implied guarantee. A component that behaved as though
configuration-time authoring checks were runtime authorization would be relying on a property the
system does not have — the checks constrain what rules and dispatch may AUTHOR, not what a
credential holder may WRITE.

#### Scenario: authority is not inferred from message content

- **GIVEN** a mutation request whose triples name an `agent.*` predicate
- **WHEN** it is applied
- **THEN** no component treats the predicate namespace as evidence of the caller's authority
- **AND** the write is accepted or refused on grammar and contract grounds alone

### Requirement: The beta cutover updates owned producers and resets incompatible state

The breaking release MUST update every SemStreams producer, owned reference design, generated schema/tool
surface, exact query, and participating owned sister repository to the canonical contract. The release
MUST publish an exact source/configuration rename ledger, but that ledger MUST NOT be loaded as a runtime
alias or transformation table.

Existing ENTITY_STATES containing a noncanonical predicate MUST surface as typed
`graph_state_reset_required` poison per the graph-state-contract reader classes: projection and replay
consumers MUST block whole-view readiness until clean reingest and index replay reach the authoritative
watermark, while the authoritative graph-ingest surface refuses exactly the poisoned entities and keeps
serving valid state. SemStreams MUST NOT rewrite malformed beta state in place; repair is the operator
delete/reset path.

#### Scenario: incompatible beta state requires a clean reset

- **GIVEN** an existing ENTITY_STATES bucket containing a noncanonical predicate
- **WHEN** the breaking SemStreams binary starts
- **THEN** projection and replay consumers refuse whole-view readiness with reset/reingest instructions
- **AND** no compatibility reader or in-place transformer accepts the old state

#### Scenario: the authoritative surface keeps serving unaffected entities during the incident

- **GIVEN** an ENTITY_STATES bucket in which SOME entities carry a noncanonical predicate
- **WHEN** a caller reads a valid entity from the authoritative graph-ingest surface
- **THEN** that entity is served normally
- **AND** only the poisoned entities are refused, as typed `graph_state_reset_required`

#### Scenario: clean reingest exposes only canonical identities

- **GIVEN** incompatible graph/index buckets have been cleared
- **WHEN** owned canonical sources are reingested and index replay completes
- **THEN** every stored predicate satisfies the canonical grammar
- **AND** query results contain no deprecated predicate identity

### Requirement: Every authoritative replay consumer withholds readiness on incompatible state

Every component that interprets ENTITY_STATES or serves a derived graph view MUST use the shared canonical decoder
independently of component startup order. On any unreadable entity or predicate violation, derived-view components MUST
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

### Requirement: A declared predicate datatype comes from one closed pragmatic vocabulary

Every predicate registration MUST either declare its object datatype as one value from a closed, framework-owned
vocabulary or leave it absent, and the registry MUST refuse any other value at declaration time rather than
translating it.

The vocabulary names the **pragmatic** type of the object, never the Go type of the value. The Go type is something
the serializer observes directly; asking a declaring author to predict it produces a value the framework already holds
and cannot trust, because a value read back through the authoritative JSON round trip no longer carries the Go type
the author predicted. The vocabulary therefore carries only what observation cannot recover: that an object is an
entity reference, that a number is semantically an integer after a JSON round trip, that a string holds a structured
document.

Every value in the closed vocabulary MUST be a spelling the declaring authors already use for that exact fact, and
MUST NOT be a semantic-web term. Semantic-web vocabulary — XSD local names, JSON-LD keywords, RDF datatype IRIs —
belongs at the export boundary, where interoperability is the job; it MUST NOT appear in the declaration surface an
adopter writes against. The mapping from a declared value to its RDF datatype IRI therefore lives with the serializer,
and the registry MUST NOT be required to know it. A declaring author states that an object is an entity reference; the
exporter, and only the exporter, decides that this means an IRI node rather than a literal.

The registry MUST store exactly the value that was declared. It MUST NOT normalize, alias, or otherwise translate a
non-canonical spelling into a canonical one: no compatibility alias table, no deprecated-value map, no dual
read/write path, no runtime escape hatch. Two reasons, and the second is the durable one. A translation layer on a
frozen package is an undated permanent bridge that nothing ever removes. And translation makes the declaration
surface lie — a reader observes a value no author wrote, so the registry can no longer be read as a record of what
adopters declared. An adopter whose declarations use a retired spelling MUST migrate them; the framework's obligation
is to refuse early, name the accepted vocabulary, and publish the migration, not to guess an intent.

Because nothing is rewritten, validation MUST be stable under re-registration: registration amends rather than
replaces, so a stored value is re-validated on every subsequent registration of the same predicate and MUST still be
accepted.

An absent datatype MUST remain valid. Declarations that carry no datatype are a legitimate registration shape, and
refusing them would fail registrations that declare nothing but a predicate name.

Refusal MUST name the offending value and the accepted vocabulary in the same message, at the registration surface,
before any data moves. There is no compile-time surface for this: the declaring option keeps a plain string parameter,
so registration-time refusal is the earliest place the mistake can be caught.

#### Scenario: a canonical spelling registers unchanged

- **GIVEN** a predicate registration declaring a datatype already in the closed vocabulary
- **WHEN** the registry validates the declaration
- **THEN** registration succeeds
- **AND** reading the predicate's metadata returns that exact value, never a substituted one

#### Scenario: a retired legacy spelling is refused at declaration

- **GIVEN** a predicate registration declaring a datatype in a spelling the framework once translated
- **WHEN** the registry validates the declaration
- **THEN** registration fails, exactly as for any other value outside the closed vocabulary
- **AND** the failure names both the offending value and the accepted vocabulary
- **AND** nothing is stored under that predicate

#### Scenario: an unrecognized spelling is refused at declaration

- **GIVEN** a predicate registration declaring a datatype that is not in the closed vocabulary
- **WHEN** the registry validates the declaration
- **THEN** registration fails
- **AND** the failure names both the offending value and the accepted vocabulary

#### Scenario: an absent datatype remains a valid declaration

- **GIVEN** a predicate registration that declares no datatype at all
- **WHEN** the registry validates the declaration
- **THEN** registration succeeds
- **AND** the predicate's metadata reports an absent datatype rather than a substituted default

#### Scenario: an amending re-registration keeps its inherited canonical value

- **GIVEN** a predicate already registered with a canonical datatype
- **WHEN** the same predicate is registered again with options that do not mention the datatype
- **THEN** registration succeeds, because the inherited value is re-validated and still accepted
- **AND** the inherited canonical value is retained unchanged

#### Scenario: both registration entry points enforce the same vocabulary

- **GIVEN** a datatype outside the closed vocabulary
- **WHEN** it is declared through the functional-option entry point, and separately through the direct-metadata entry point
- **THEN** both refuse it with the same reason
- **AND** neither path accepts a value the other refuses

#### Scenario: the set of framework predicates declaring no datatype can only shrink

- **GIVEN** every predicate the framework registers — its package `init()` registrations and its explicit
  composition root — walked in the order a running binary applies them, the composition root amending `init()`
- **WHEN** the repository contract check walks the registry
- **THEN** every predicate that declares a datatype declares one from the closed vocabulary
- **AND** every predicate declaring none is named in a recorded exemption set, so that set can only shrink
- **AND** every recorded exemption names a predicate that is registered and does declare nothing
- **AND** the check is shown capable of failing, and fails on a walk that examined nothing

### Requirement: RDF export honors the declared datatype and never fabricates a value

RDF export MUST classify a triple's object by the most specific declaration available, in exactly this order: the
triple's own datatype hint first, the predicate's declared datatype next, observation of the value's Go type last.
Export MUST NOT emit a lexical form the observed value does not support.

The `string` declaration is the one exception to that order, and it is a confirmation rather than an override: its
RDF rendering is `xsd:string`, which is the default and is omitted from output, so a `string`-declared predicate MUST
fall through to observation. An entity-ID-shaped or absolute-IRI object under a `string` declaration therefore still
serializes as a resource. Forcing a plain literal instead would turn framework predicates that declare `string` and
carry entity IDs from IRI objects into string literals.

An object carrying the entity-reference datatype — from either the per-triple hint or the predicate declaration — MUST
serialize as an IRI resource, never as a literal carrying a non-IRI datatype. An object whose value carries a scheme
the serializer recognizes as absolute — `http://`, `https://` and `urn:` — MUST serialize as a resource for the same
reason: a literal in that position produces invalid RDF for the consumer, and the two cases are one decision, not
two. Schemes outside that recognized set are not detected and serialize as literals; widening the set is a
deliberate change, not an accident of the implementation.

Where a declared datatype cannot represent the observed value, observation MUST win for that triple and the
declaration MUST be ignored rather than applied. Export is a serializer: it renders what is there. A declaration is a
statement about the predicate, not a licence to rewrite a value, and a serializer that emitted a predicted type over a
contradicting observation would be lying about data it can see.

An absent declaration MUST leave observation-based classification exactly as it is, so predicates that declare nothing
serialize today's way.

#### Scenario: a declared integer survives the authoritative JSON round trip

- **GIVEN** a predicate declared with the integer datatype and a triple whose object is a whole number
- **WHEN** the triple is written to and read back from the authoritative entity store, then serialized to RDF
- **THEN** the object serializes with the integer datatype
- **AND** it does not serialize with the floating-point datatype the round trip left in the Go value

#### Scenario: a per-triple datatype still beats the predicate declaration

- **GIVEN** a predicate declared with one datatype and a triple carrying a different explicit datatype
- **WHEN** the triple is serialized
- **THEN** the triple's own datatype is used
- **AND** the predicate's declaration is not consulted for that triple

#### Scenario: an entity reference serializes as a resource

- **GIVEN** a triple whose object carries the entity-reference datatype and whose value is a canonical entity ID
- **WHEN** the triple is serialized
- **THEN** the object is emitted as an IRI resource
- **AND** it is not emitted as a literal carrying the entity-reference marker as its datatype

#### Scenario: an absolute-IRI object serializes as a resource

- **GIVEN** a triple whose object is an absolute IRI
- **WHEN** the triple is serialized
- **THEN** the object is emitted as an IRI resource
- **AND** the output is valid RDF for a type-bearing predicate

#### Scenario: a declaration the value contradicts is ignored, not applied

- **GIVEN** a predicate declared with the integer datatype and a triple whose object has a fractional part
- **WHEN** the triple is serialized
- **THEN** the object serializes with the datatype its observed value supports
- **AND** no rounded, truncated, or otherwise altered lexical form is emitted

#### Scenario: an undeclared predicate serializes by observation

- **GIVEN** a predicate that declares no datatype
- **WHEN** a triple using it is serialized
- **THEN** the object is classified from the observed Go type
- **AND** the output is unchanged from the behavior before declarations were honored

### Requirement: Declared units and value ranges are documentation until a named consumer exists

Declared measurement units and value ranges MUST be treated as human-readable documentation carried on the
declaration, and MUST NOT be validated, normalized, interpreted, or honored by any framework path while no consumer
reads them.

This is stated rather than left implicit because the alternative failure is silent: a metadata field that looks typed,
sits beside a field that IS honored, and is frozen into a released surface reads to every adopter as a promise the
system does not keep. Saying "documentation" is a smaller promise than the one the field's presence currently implies,
and it is one the framework actually keeps.

A closed vocabulary MUST NOT be invented for either field ahead of its consumer. Inventing one would freeze a grammar
chosen with no reader to constrain it, which is the same mistake being corrected for the datatype field one field
over. When a consumer is named, it arrives with its own change and its own requirement.

#### Scenario: units and range round-trip exactly as declared

- **GIVEN** a predicate registration declaring measurement units and a value range in any form
- **WHEN** the registry validates the declaration
- **THEN** registration succeeds regardless of the form of either value
- **AND** reading the predicate's metadata returns both values byte-for-byte as declared

#### Scenario: no framework path interprets units or range

- **GIVEN** a predicate declaring measurement units and a value range
- **WHEN** its triples are validated, stored, fused, or serialized
- **THEN** no behavior differs from the same predicate declaring neither

