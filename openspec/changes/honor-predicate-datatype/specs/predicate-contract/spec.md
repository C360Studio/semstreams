## ADDED Requirements

### Requirement: A declared predicate datatype comes from one closed pragmatic vocabulary

Every predicate registration MUST either declare its object datatype as one value from a closed, framework-owned
vocabulary or leave it absent, and the registry MUST normalize a recognized legacy spelling to its canonical value at
declaration time while refusing an unrecognized one.

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

Normalization is a declaration-time step, NOT a compatibility alias, a deprecated-value table, a dual read/write path,
or a runtime escape hatch: the registry MUST store only the canonical value, so no reader ever observes a legacy
spelling and no legacy spelling is ever persisted. Normalization MUST be idempotent — normalizing a canonical value
returns that same value — because registration amends rather than replaces, so an already-normalized value is
re-validated on every subsequent registration of the same predicate.

An absent datatype MUST remain valid. Declarations that carry no datatype are a legitimate registration shape, and
refusing them would fail registrations that declare nothing but a predicate name.

Refusal MUST name the offending value and the accepted vocabulary in the same message, at the registration surface,
before any data moves. There is no compile-time surface for this: the declaring option keeps a plain string parameter,
so registration-time refusal is the earliest place the mistake can be caught.

#### Scenario: a canonical spelling registers unchanged

- **GIVEN** a predicate registration declaring a datatype already in the closed vocabulary
- **WHEN** the registry validates the declaration
- **THEN** registration succeeds
- **AND** reading the predicate's metadata returns that exact value

#### Scenario: a recognized legacy spelling normalizes once at declaration

- **GIVEN** a predicate registration declaring a datatype spelled in a recognized legacy form
- **WHEN** the registry validates the declaration
- **THEN** registration succeeds
- **AND** reading the predicate's metadata returns the canonical value, never the legacy spelling
- **AND** no reader anywhere observes the legacy spelling

#### Scenario: an unrecognized spelling is refused at declaration

- **GIVEN** a predicate registration declaring a datatype that is neither canonical nor a recognized legacy spelling
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
- **THEN** registration succeeds
- **AND** the inherited canonical value is retained unchanged

#### Scenario: both registration entry points enforce the same vocabulary

- **GIVEN** a datatype outside the closed vocabulary
- **WHEN** it is declared through the functional-option entry point, and separately through the direct-metadata entry point
- **THEN** both refuse it with the same reason
- **AND** neither path accepts a value the other refuses

#### Scenario: the framework's own declarations are complete

- **GIVEN** every predicate the framework itself registers at its vocabulary composition root
- **WHEN** the repository contract check walks the registry
- **THEN** each one carries a datatype from the closed vocabulary
- **AND** the check is shown capable of failing against a registry seeded with a bare declaration

### Requirement: RDF export honors the declared datatype and never fabricates a value

RDF export MUST classify a triple's object by the most specific declaration available, in exactly this order: the
triple's own datatype hint first, the predicate's declared datatype next, observation of the value's Go type last.
Export MUST NOT emit a lexical form the observed value does not support.

An object carrying the entity-reference datatype — from either the per-triple hint or the predicate declaration — MUST
serialize as an IRI resource, never as a literal carrying a non-IRI datatype. An object whose value is an absolute IRI
MUST serialize as a resource for the same reason: a literal in that position produces invalid RDF for the consumer,
and the two cases are one decision, not two.

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
