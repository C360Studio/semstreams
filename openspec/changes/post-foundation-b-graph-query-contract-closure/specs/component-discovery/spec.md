## ADDED Requirements

### Requirement: Graph query request composition uses one versioned operation family

Graph-query SHALL declare exactly one required input named `graph_queries` with direction `input`, kind
`nats-request`, subject family `graph.query.*`, interface type `graph.query`, and interface version `v1`. Every admitted
graph-query responder SHALL derive its exact one-token operation suffix from that resolved family. A handler SHALL NOT
subscribe through a separate literal that bypasses the declaration.

Graph-gateway SHALL retain exactly its three required query-family outputs. Its existing `graph_queries` output SHALL
use `graph.query.*` and interface `graph.query` version `v1` and SHALL cover the fourteen admitted GraphQL operations
without creating fourteen ports.

Research classify SHALL declare one required `searchGraph` output. Research execute SHALL declare four required
outputs for `batch`, `relationships`, `temporal`, and `searchGraph`. Each SHALL be kind `nats-request`, use its exact
`graph.query.<operation>` subject, and carry interface `graph.query` version `v1`. Agentic-tools SHALL declare no output
for the deleted search or summary wrappers.

Libraries and E2E harnesses SHALL NOT synthesize component ports. `pkg/fusion/fusionnats.Client` owns adapter behavior;
the component that embeds it owns the six exact graph-query outputs and `GRAPH_STATUS` KV-read declaration required by
that composition.

#### Scenario: canonical graph-query provider and gateway compose

- **GIVEN** graph-query's required `graph.query.*` input and graph-gateway's required matching family output
- **WHEN** Registry resolves their normalized facts
- **THEN** direction, kind, subject containment, required state, interface type, and version compose
- **AND** all sixteen provider subjects derive from the one resolved input family

#### Scenario: research declares only the operations it requests

- **GIVEN** research classify and execute are admitted
- **WHEN** their effective output declarations are inspected
- **THEN** classify declares only its required `searchGraph` query dependency
- **AND** execute declares its required `batch`, `relationships`, `temporal`, and `searchGraph` dependencies
- **AND** no general query-client or wildcard dependency is invented

#### Scenario: undeclared or mismatched request fails before execution

- **GIVEN** an embedded component requests an operation without its exact output, or provider and consumer disagree on
  family, kind, required state, interface type, or interface version
- **WHEN** production factory and Registry validation run
- **THEN** admission fails before subscription or request handling
- **AND** no literal bypass, alias, autofill, library port, or compatibility shim repairs it

#### Scenario: shipped configuration census stays mechanically complete

- **WHEN** all twenty-one shipped configurations load through production factories and Registry
- **THEN** the effective set contains eleven graph-query, eight graph-gateway, two research-classify, two
  research-execute, and nine agentic-tools instances with the exact declarations above
- **AND** raw counts are `395/243/54`, effective counts are `571/378/69`, and the raw-to-effective delta remains
  `176/135/15`
