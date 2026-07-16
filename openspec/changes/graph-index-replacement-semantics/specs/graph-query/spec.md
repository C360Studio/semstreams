## ADDED Requirements

### Requirement: Exact predicate lookup and namespace enumeration have distinct semantics

Graph query MUST treat a complete canonical `domain.category.property` as an exact predicate identity.
Namespace enumeration MUST be an explicit operation over `domain` or `domain.category`; it MUST NOT be
implemented by ambiguous string-prefix matching. Query wildcard syntax MUST be validated separately from stored
predicate syntax. The wire contract MUST remain independent of the physical PREDICATE_INDEX key representation.

#### Scenario: exact lookup excludes a longer or neighboring name

- **GIVEN** entities using two distinct canonical predicates in the same namespace
- **WHEN** a caller requests one complete predicate identity
- **THEN** only memberships for that exact three-part predicate are returned

#### Scenario: namespace enumeration is explicit

- **GIVEN** several predicates under one `domain.category` namespace
- **WHEN** a caller performs namespace enumeration for that two-part namespace
- **THEN** all and only canonical predicate identities in that namespace are returned
- **AND** the two-part namespace is never accepted as a stored predicate identity

### Requirement: Query-visible memberships reflect the complete current projection

Graph-index queries MUST observe the complete current ENTITY_STATES projection after `graph.index.query.status`
reports the authoritative entity revision reached. This applies to exact predicate, predicate-list,
predicate-stats, compound-predicate, by-name, incoming, and traversal queries. Superseded and empty memberships
MUST NOT remain query-visible. The contract does not imply synchronous indexing before that watermark or freshness
of independently scheduled downstream processors such as graph-clustering.

#### Scenario: a replacement retracts the former result

- **GIVEN** an entity is discoverable through membership A
- **WHEN** its authoritative projection changes from A to B and then to empty
- **THEN** queries return only B after the first watermark
- **AND** neither A nor B returns the entity after the empty-projection watermark

#### Scenario: restart and repair preserve replacement truth

- **GIVEN** a membership replacement is interrupted by a required index-operation failure
- **WHEN** readiness is withheld and repair or restart replays the current entity state
- **THEN** the public query surface converges to the complete current projection
- **AND** it never reports a ready partial mixture of old and new memberships

### Requirement: Limited query results are deterministic

Graph-index query handlers MUST deduplicate and sort the complete candidate or result set before applying a limit
or sample. Exact, value-filtered, compound, and stats-sample results use entity ID ascending; predicate-list and
namespace-list use predicate identity ascending; INCOMING retains `(sourceID, predicate)` order; NAME retains its
documented ranking tuple with entity ID as the final tie-breaker. Value-filter hydration MUST consume sorted IDs.
Limits or samples are applied after ordering only on wire surfaces that expose them.

#### Scenario: repeated limited queries return the same entities

- **GIVEN** an unchanged index contains more matches than a request limit
- **WHEN** the same limited exact, value-filtered, compound, stats-sample, or by-name query is repeated
- **THEN** every response contains the same ordered limited result

#### Scenario: predicate listing is ordered without inventing a limit

- **GIVEN** predicate-list or namespace-list returns several current predicates
- **WHEN** the same query is repeated after shuffled replay
- **THEN** every response contains all matching predicates in predicate-identity order

#### Scenario: restart does not reshuffle a limited result

- **GIVEN** an unchanged authoritative entity set and a limited query
- **WHEN** graph-index restarts and rebuilds the selected derived buckets
- **THEN** the result identities and order match the pre-restart response

### Requirement: Predicate catalog reports current materialized membership

While `PREDICATE_CATALOG` exists, predicate-list and namespace-list MUST include only predicates with at least one
current membership. Vocabulary declaration and historical-use discovery MUST remain vocabulary-registry concerns,
not graph-index catalog semantics.

#### Scenario: last member removal retracts the catalog name

- **GIVEN** one entity is the final current member of a predicate
- **WHEN** that entity retracts the predicate and graph-index reaches its revision
- **THEN** predicate-list and namespace-list no longer return the predicate
- **AND** the predicate may remain declared in the vocabulary registry
