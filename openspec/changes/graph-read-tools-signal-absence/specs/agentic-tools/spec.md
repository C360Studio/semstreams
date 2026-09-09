## ADDED Requirements

### Requirement: Direct graph-read tools classify an empty result and show the vocabulary they observed

A direct `query_*` tool that succeeds with zero rows SHALL set `ResultHint` to `empty` and SHALL carry, in its
content, the facts it observed while answering: for `query_relationships`, every predicate present on the entity with
its kind (relationship or property) and this process's registry metadata when registered, plus whether the
`relationship_type` filter is registered. A relationship SHALL be a triple for which `message.Triple.IsRelationship()`
holds. A filter that is not a canonical `domain.category.property` SHALL be refused as `invalid_args` before any
scan. `relationship_type` is a read filter: this requirement does not extend the predicate-contract prohibition,
which binds what a tool writes; these tools write nothing.

`filter_registered` reports whether this process's vocabulary registry declares the filtered predicate. It SHALL NOT
be read as an authorization or existence verdict: an unregistered predicate can be legitimately minted under a
namespace delegation, and a registered one can be absent from every entity.

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
- **AND** the test that verifies this is the `present only as a property` case of
  `TestQueryRelationships_FilteredAbsenceIsClassified`

#### Scenario: malformed filter

- **GIVEN** a `relationship_type` that is not three dot-separated segments
- **WHEN** the tool is called
- **THEN** the result is `invalid_args` and no entity is read

### Requirement: query_relationships serves the direction it can read and names the owner of the one it cannot

`query_relationships` reads relationships from the entity's own record, which holds own-subject triples only, and
SHALL therefore serve outgoing relationships. An omitted `direction` SHALL be served as `outgoing` and echoed in the
result. An explicit `direction` of `incoming` or `both` SHALL be refused as `invalid_args` with a message naming the
incoming owner — the `graph.query.relationships` operation over the incoming index — and the advertised enum SHALL
list only the direction served.

#### Scenario: omitted direction

- **GIVEN** a call with no `direction` argument
- **WHEN** the tool answers
- **THEN** outgoing relationships are returned and `direction` in the result is `outgoing`

#### Scenario: a direction this tool cannot read

- **GIVEN** a call with `direction` of `incoming` or `both`
- **WHEN** the tool is called
- **THEN** the result is `invalid_args`, the message names the incoming owner, and no entity is read
- **AND** the test that verifies this is `TestQueryRelationships_UnservedDirectionIsRefused`

### Requirement: query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing

`query_by_type` SHALL build a six-position pattern by right-anchoring one to three canonical segments of
`entity_type` — `*.*.*.*.<type>.*`, `*.*.*.<domain>.<type>.*`, or `*.*.<system>.<domain>.<type>.*` — validate it as an
entity-ID pattern, and list matching `ENTITY_STATES` keys through the catalog reader's filtered key listing. It SHALL
sort the matched keys before returning or paging them, and SHALL return identities up to `limit`, the pattern used,
and the matched total; SHALL set `empty` when nothing matched and `too_large` when the match exceeds the page; and
SHALL create no index or bucket. A binding without key listing SHALL fail the call with a classified internal error.

`query_by_type` SHALL declare itself paginated and continue through the framework's pagination contract rather than a
result-body truncation flag: every successful call SHALL set `has_more` in the result metadata, a call with matches
beyond the returned page SHALL also set `next_cursor` to an opaque token the caller passes back verbatim as `cursor`,
and the result SHALL carry no separate truncation field. The token SHALL use the same encoding as the graph
prefix-listing cursor, so one cursor format serves keyset continuation over `ENTITY_STATES`, and a `cursor` that does
not decode SHALL be refused as `invalid_args`.

#### Scenario: one-token type

- **GIVEN** three entities of type `temperature` under two domains
- **WHEN** `entity_type` is `temperature`
- **THEN** all three identities are returned in sorted order with `pattern` `*.*.*.*.temperature.*`
- **AND** the test that verifies this is `TestQueryByType_ListsIDsByTypeSegment`

#### Scenario: three right-anchored tokens

- **GIVEN** an `entity_type` of `<system>.<domain>.<type>`
- **WHEN** the tool answers
- **THEN** `pattern` is `*.*.<system>.<domain>.<type>.*` and only identities matching all three segments are returned
- **AND** the test that verifies this is `TestQueryByType_ListsIDsByTypeSegment`

#### Scenario: more matches than the page

- **GIVEN** more matches than `limit`
- **WHEN** the tool answers
- **THEN** `matched` is the observed total, `has_more` is true, and `next_cursor` is an opaque continuation token
- **AND** `ResultHint` is `too_large`, so the model is told both to narrow and that it may continue
- **AND** the test that verifies this is `TestQueryByType_PageOneSetsHasMoreAndCursor`

#### Scenario: continuation returns the next page

- **GIVEN** the `next_cursor` from a previous call
- **WHEN** `query_by_type` is called again with the same `entity_type` and that `cursor`
- **THEN** every identity returned sorts after the cursor position and no identity repeats a previous page
- **AND** `has_more` is false on the last page and no `next_cursor` is set
- **AND** the test that verifies this is `TestQueryByType_CursorContinuesWithoutRepeats`

#### Scenario: unusable cursor refused

- **GIVEN** a `cursor` that is not a token this tool issued
- **WHEN** the tool is called
- **THEN** the result is `invalid_args` and the key lister is not invoked
- **AND** the test that verifies this is `TestQueryByType_RejectsUndecodableCursor`

#### Scenario: wildcard injection refused

- **GIVEN** an `entity_type` that contains `*`, `>`, an empty segment, or more than three tokens
- **WHEN** the tool is called
- **THEN** the result is `invalid_args` and the key lister is not invoked
- **AND** the test that verifies this is `TestQueryByType_RejectsNonSegmentTokens`

#### Scenario: binding without key listing is loud

- **GIVEN** an executor whose KV binding does not implement key listing
- **WHEN** `query_by_type` is called
- **THEN** the result is a classified internal error naming the binding, never an empty listing
- **AND** the test that verifies this is `TestQueryByType_WithoutKeyListerIsLoud`

### Requirement: query_neighbors bounds its content by a model-facing budget and reports unresolved targets

`query_neighbors` SHALL expand only through relationship triples, SHALL stop expanding when the next record would
exceed the executor's model-facing content budget — a fixed executor cap in the class of the bash and HTTP output
caps, distinct from the transport bound the component observes — and SHALL report `truncated`, `frontier_remaining`,
and `too_large` when it does. A target absent from `ENTITY_STATES` SHALL be listed in `unresolved`, never omitted; a transient read failure
SHALL fail the call as a network error. `filter_type` SHALL match the identity's type segment with the same grammar as
`query_by_type`.

A traversal frontier is not a stable continuation position, so `query_neighbors` SHALL NOT declare itself paginated
and SHALL NOT set `has_more`: it reports that more exists through `truncated` and `frontier_remaining`, and the caller
narrows with `depth` or `filter_type`. No result SHALL announce more results without a token the caller can pass back.

#### Scenario: budget reached

- **GIVEN** a start entity whose neighbors exceed the budget
- **WHEN** the tool answers
- **THEN** the returned records fit the budget, `truncated` is true, `ResultHint` is `too_large`
- **AND** `frontier_remaining` is the count of identities not expanded
- **AND** no continuation flag is set, because the traversal has no resumable position
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
