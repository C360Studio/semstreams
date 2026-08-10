## ADDED Requirements

### Requirement: The operation-specific NATS fusion adapter remains stable

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`, preserving
`New(requester, timeout)`, optional Close, lazy `GRAPH_STATUS/graph-index` readiness, and the six interface methods
`Status`, `Resolve`, `Entity`, `Entities`, `Neighbors`, and `Names`.

The transport SHALL retain six request subjects: by-name, prefix, semantic, entity, batch, and relationships. These
subjects are not a one-to-one restatement of the interface: Status uses KV, Resolve selects among three subjects, and
Names reuses by-name.

Every request/reply success SHALL pass through `graph.UnwrapQueryResponse` exactly once before operation decoding.
Status SHALL remain outside this rule because it reads KV state.

Entity SHALL decode the producer's `graph.ExactEntity`, require a valid matching entity and nonzero KV revision, and
project its ID and triples into the existing `fusion.Entity`. The revision SHALL NOT expand `fusion.Entity` or
`RetrievalClient` without a present consumer.

The fusion library SHALL claim no component ports. This change SHALL NOT invent a component or configuration owner for
the client.

#### Scenario: fusion entity uses the producer representation

- **GIVEN** a valid `graph.ExactEntity` reply
- **WHEN** `fusionnats.Client` reads it
- **THEN** the exact entity and revision are validated
- **AND** the existing fusion entity contains its ID and triples
- **AND** no obsolete bare `EntityState` fixture remains

#### Scenario: request subjects accept one envelope

- **GIVEN** equivalent bare and standard-enveloped fixtures for each request subject
- **WHEN** `fusionnats.Client` decodes them
- **THEN** each pair produces the same existing retrieval result
- **AND** no payload is unwrapped twice

#### Scenario: fusion preservation creates no port owner

- **WHEN** Slice E component and configuration changes are inspected
- **THEN** no fusion-host component or fusion port declaration was added
- **AND** the client constructor and interface remain unchanged
