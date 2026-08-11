# gateway-query-routing Specification

## Purpose
TBD - created by archiving change post-foundation-b-graph-query-contract-closure. Update Purpose after archive.
## Requirements
### Requirement: The GraphQL root advertises exactly the operations the gateway serves

Graph-gateway SHALL expose exactly nineteen root Query fields. Fourteen SHALL route to graph-query:
`entity`, `entitiesByPrefix`, `entityByAlias`, `relationships`, `entityIdHierarchy`, `pathSearch`, `spatialSearch`,
`temporalSearch`, `semanticSearch`, `findSimilar`, `localSearch`, `globalSearch`, `graphSummary`, and `searchGraph`.
Five unrelated served fields SHALL remain: `trajectory`, `entitiesByPredicate`, `predicates`, `predicateStats`, and
`compoundPredicateQuery`.

Every advertised field SHALL have a production route and response-field fixture. `capabilities` SHALL be absent until
a separately owned capability contract is admitted; Registry declaration snapshots SHALL NOT be synthesized into a
deployment-capability response.

`semanticSearch` SHALL be the sole semantic-search root and projected response key. The hidden `similaritySearch`
gateway spelling SHALL be absent. No alias, deprecated field, stub, synthesized capability list, or dual route SHALL
remain.

#### Scenario: introspection contains no phantom field

- **WHEN** a caller introspects the root Query schema
- **THEN** the exact graph-query-backed subset has fourteen fields and the total has nineteen
- **AND** `capabilities` is absent and every remaining field has a production route and response-field fixture

#### Scenario: semantic search uses one canonical field name

- **GIVEN** a caller requests `semanticSearch`
- **WHEN** graph-gateway routes the request and projects the successful reply
- **THEN** it requests `graph.query.semantic` and returns the result under `data.semanticSearch`
- **AND** `similaritySearch` is neither advertised nor accepted as a compatibility alias

#### Scenario: removed capabilities fails at the GraphQL boundary

- **GIVEN** an old caller still requests `capabilities`
- **WHEN** the post-cutover gateway validates or routes the request
- **THEN** the query fails visibly as an unsupported GraphQL field
- **AND** no NATS capabilities request, stub response, or inferred declaration list is produced
