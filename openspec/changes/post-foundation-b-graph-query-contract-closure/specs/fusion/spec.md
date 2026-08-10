## ADDED Requirements

### Requirement: The operation-specific NATS fusion adapter remains stable

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`, preserving
`New(requester, timeout)`, optional Close, lazy `GRAPH_STATUS` graph-index readiness, downstream role, and exactly six
operations: by-name, prefix, semantic, entity, batch, and relationships.

Every operation SHALL pass a successful reply through `graph.UnwrapQueryResponse` exactly once before decoding its
production payload. Entity replies SHALL decode as `graph.ExactEntity`, preserving canonical entity and same-entry KV
revision. Fixtures SHALL preserve ranking and order, optional similarity, batch missing reasons and request order,
relationship direction, and raw readiness behavior. A library SHALL claim no component ports; the component embedding
the adapter SHALL own its six request outputs and readiness KV-read declaration.

#### Scenario: fusion entity uses the producer representation

- **GIVEN** the graph-query entity producer returns a bare or enveloped `graph.ExactEntity`
- **WHEN** `fusionnats.Client` reads it
- **THEN** the adapter preserves the exact entity and revision
- **AND** no fixture relies on an obsolete bare `EntityState` response

#### Scenario: all six operations accept the admitted envelope rule

- **GIVEN** equivalent bare and enveloped production-shape replies for each of the six operations
- **WHEN** `fusionnats.Client` decodes them
- **THEN** each pair yields the same typed retrieval result
- **AND** every response is unwrapped at most once

#### Scenario: preserving fusion adds no library ports

- **WHEN** fusion package exports and component declarations are inspected
- **THEN** the client constructor and six-operation interface are unchanged
- **AND** all component port ownership remains with the actual embedding component
