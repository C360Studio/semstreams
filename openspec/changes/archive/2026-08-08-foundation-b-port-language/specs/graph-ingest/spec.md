## MODIFIED Requirements

### Requirement: The mutation API is a declared typed component port

Graph ingest SHALL expose exactly one required canonical `nats-request` provider input for interface
`semstreams.graph.mutation`, interface version `v1`, and subject family `graph.mutation.>`.

The graph mutation operations SHALL derive their runtime subjects from that resolved declaration.

No retired flat declaration, legacy alias, plain-NATS port, builder fallback, or hard-coded subject SHALL provide or
restore the mutation API.

#### Scenario: Canonical mutation provider controls subscriptions

- **GIVEN** one valid required graph-mutation `nats-request` provider input
- **WHEN** graph ingest initializes
- **THEN** mutation subscriptions are derived from the provider's resolved subject and interface facts
- **AND** the declared subject family remains the runtime authority

#### Scenario: An undeclared mutation side channel cannot boot

- **GIVEN** graph-ingest has no compatible mutation provider input port
- **WHEN** the flow is validated
- **THEN** validation fails before mutation subscriptions are installed
- **AND** graph-ingest does not fall back to hardcoded subjects

#### Scenario: Missing provider fails before subscription

- **GIVEN** graph ingest without a graph-mutation provider input
- **WHEN** composition and startup validation run
- **THEN** startup fails before any mutation subscription is created
- **AND** no default `graph.mutation.*` subject is supplied

#### Scenario: Incompatible provider fails before subscription

- **GIVEN** a provider with an incompatible kind, required state, interface type, interface version, or subject family
- **WHEN** composition and startup validation run
- **THEN** startup fails before any mutation subscription is created
- **AND** the failure identifies the incompatible provider facts

#### Scenario: Retired declaration does not create a hidden mutation API

- **GIVEN** graph ingest configured with a retired flat declaration, legacy alias, or plain-NATS substitute
- **WHEN** startup validation runs
- **THEN** startup fails
- **AND** no hidden `graph.mutation.*` subscription is created
- **AND** no compatibility shim or hard-coded fallback is applied
