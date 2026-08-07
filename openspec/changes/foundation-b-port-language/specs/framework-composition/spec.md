## MODIFIED Requirements

### Requirement: Typed request ports define mutation composition

Graph-mutation composition SHALL use canonical resolved `nats-request` facts.

Exactly one compatible required graph-mutation provider input SHALL exist. Compatible requester outputs MAY connect to
that provider. Zero compatible providers or multiple compatible providers SHALL fail composition.

Provider cardinality SHALL be determined from resolved component topology and SHALL NOT be inferred from process count,
flat port fields, or hard-coded subjects.

#### Scenario: Canonical requester and provider connect

- **GIVEN** a requester output and provider input that both resolve as compatible required graph-mutation
  `nats-request` ports
- **WHEN** framework composition is built
- **THEN** the requester connects to the provider using their resolved interface and subject facts
- **AND** no consumer reconstructs their protocol from concrete config types

#### Scenario: Missing provider fails composition

- **GIVEN** one or more graph-mutation requester outputs
- **WHEN** no compatible required provider input exists
- **THEN** composition fails before runtime startup
- **AND** no hidden or hard-coded provider is supplied

#### Scenario: Multiple providers fail composition

- **GIVEN** more than one compatible required graph-mutation provider input
- **WHEN** framework composition is built
- **THEN** composition fails with a provider-cardinality error
- **AND** process count does not select a provider

#### Scenario: Legacy declaration cannot satisfy topology

- **GIVEN** a requester or provider expressed through a retired flat field, legacy alias, plain-NATS substitute, or
  otherwise noncanonical declaration
- **WHEN** framework composition is built
- **THEN** that declaration cannot satisfy graph-mutation topology
- **AND** no compatibility shim, interface restoration, or subject fallback is applied
