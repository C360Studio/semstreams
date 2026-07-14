## ADDED Requirements

### Requirement: Exact predicate lookup and namespace enumeration have distinct semantics

Graph query MUST treat a complete canonical `domain.category.property` as an exact predicate identity.
Namespace enumeration MUST be an explicit operation over `domain` or `domain.category`; it MUST NOT be
implemented by ambiguous string-prefix matching. Query wildcard syntax MUST be validated separately from
stored predicate syntax and MUST NOT imply semantic equivalence or ownership.

The wire contract MUST remain independent of whether PREDICATE_INDEX uses raw or hashed physical keys.

#### Scenario: exact lookup excludes a longer or neighboring name

- **GIVEN** entities using two distinct canonical predicates in the same namespace
- **WHEN** a caller requests one complete predicate identity
- **THEN** only memberships for that exact three-part predicate are returned

#### Scenario: namespace enumeration is explicit

- **GIVEN** several predicates under one `domain.category` namespace
- **WHEN** a caller performs namespace enumeration for that two-part namespace
- **THEN** all and only canonical predicate identities in that namespace are returned
- **AND** the two-part namespace is never accepted as a stored predicate identity
