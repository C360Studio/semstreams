## ADDED Requirements

### Requirement: Direct graph-read tools classify an empty result and show the vocabulary they observed

A direct `query_*` tool that succeeds with zero rows SHALL set `ResultHint` to `empty` and SHALL carry, in its
content, the facts it observed while answering: for `query_relationships`, every predicate present on the entity with
its kind (relationship or property) and this process's registry metadata when registered, plus whether the
`relationship_type` filter is registered. A relationship SHALL be a triple for which `message.Triple.IsRelationship()`
holds. A filter that is not a canonical `domain.category.property` SHALL be refused as `invalid_args` before any
scan. `relationship_type` is a read filter: this requirement does not extend the predicate-contract prohibition,
which binds what a tool writes; these tools write nothing.

#### Scenario: registered predicate absent on the entity

- **GIVEN** an entity whose triples carry no `agent.lineage.parent` and a registry that registers it
- **WHEN** `query_relationships` filters on that predicate
- **THEN** `count` is 0, `ResultHint` is `empty`, `filter_registered` is true
- **AND** `predicates_present` lists the entity's predicates with their kinds
- **AND** the test that verifies this is `TestQueryRelationships_FilteredAbsenceIsClassified`

#### Scenario: unregistered predicate

- **GIVEN** a filter this process's registry does not know
- **WHEN** the tool answers
- **THEN** `filter_registered` is false and the result is otherwise the same empty classification

#### Scenario: predicate present only as a property

- **GIVEN** an entity carrying the filtered predicate with a literal object
- **WHEN** the tool answers
- **THEN** `count` is 0 and `predicates_present` shows that predicate with kind `property`
- **AND** the test that verifies this is `TestQueryRelationships_LiteralObjectsAreNotRelationships`

#### Scenario: malformed filter

- **GIVEN** a `relationship_type` that is not three dot-separated segments
- **WHEN** the tool is called
- **THEN** the result is `invalid_args` and no entity is read

### Requirement: query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing

`query_by_type` SHALL build a six-position pattern from `entity_type` — `*.*.*.*.<type>.*` for one token or
`*.*.*.<domain>.<type>.*` for two — validate it as an entity-ID pattern, and list matching `ENTITY_STATES` keys through
the catalog reader's filtered key listing. It SHALL return sorted identities up to `limit`, the pattern used, and the
matched total; SHALL set `too_large` when the match exceeds `limit` and `empty` when nothing matched; and SHALL create
no index or bucket. A binding without key listing SHALL fail the call with a classified internal error.

#### Scenario: one-token type

- **GIVEN** three entities of type `temperature` under two domains
- **WHEN** `entity_type` is `temperature`
- **THEN** all three identities are returned in sorted order with `pattern` `*.*.*.*.temperature.*`
- **AND** the test that verifies this is `TestQueryByType_ListsIDsByTypeSegment`

#### Scenario: truncated listing

- **GIVEN** more matches than `limit`
- **WHEN** the tool answers
- **THEN** `truncated` is true, `matched` is the observed total, `ResultHint` is `too_large`

#### Scenario: wildcard injection refused

- **GIVEN** an `entity_type` that contains `*`, `>`, an empty segment, or more than two tokens
- **WHEN** the tool is called
- **THEN** the result is `invalid_args` and the key lister is not invoked
- **AND** the test that verifies this is `TestQueryByType_RejectsNonSegmentTokens`

#### Scenario: binding without key listing is loud

- **GIVEN** an executor whose KV binding does not implement key listing
- **WHEN** `query_by_type` is called
- **THEN** the result is a classified internal error naming the binding, never an empty listing
- **AND** the test that verifies this is `TestQueryByType_WithoutKeyListerIsLoud`

### Requirement: query_neighbors observes a content budget and reports unresolved targets

`query_neighbors` SHALL expand only through relationship triples, SHALL stop expanding when the next record would
exceed the executor's content budget, and SHALL report `truncated`, `frontier_remaining`, and `too_large` when it
does. A target absent from `ENTITY_STATES` SHALL be listed in `unresolved`, never omitted; a transient read failure
SHALL fail the call as a network error. `filter_type` SHALL match the identity's type segment with the same grammar as
`query_by_type`.

#### Scenario: budget reached

- **GIVEN** a start entity whose neighbors exceed the budget
- **WHEN** the tool answers
- **THEN** the returned records fit the budget, `truncated` is true, `ResultHint` is `too_large`
- **AND** the test that verifies this is `TestQueryNeighbors_BudgetTruncatesWithHint`

#### Scenario: missing target is reported

- **GIVEN** a relationship whose object is not resident
- **WHEN** the tool answers
- **THEN** that identity appears in `unresolved` and the source edge is still counted
- **AND** the test that verifies this is `TestQueryNeighbors_UnresolvedTargetsAreReported`

#### Scenario: filter_type reads the identity

- **GIVEN** neighbors of two types
- **WHEN** `filter_type` names one
- **THEN** only identities whose type segment matches are returned
- **AND** the test that verifies this is `TestQueryNeighbors_FilterTypeReadsIDSegment`
