# framework-composition — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Framework packages have an explicit ownership basis

Every framework package MUST have a product-neutral responsibility and present consumers. “Ownership basis” means
the
package's maintained capability, storage resource, or lifecycle responsibility; it MUST NOT imply a semantic predicate
claim. The semantic ownership package and service are removed because explicit operation contracts, CAS, and component
ports own their surviving responsibilities with less adopter knowledge.

#### Scenario: Removing an unused authority substrate reduces composition

- **GIVEN** no accepted target contract requires semantic owner claims or leases
- **WHEN** both composition roots are built after the cutover
- **THEN** neither constructs an ownership registry or service
- **AND** the graph-state guard and catalog check remain independently wired

### Requirement: Composition roots register explicit capability sets

Both framework binaries MUST register the same graph mutation protocol, exact-read adapter, graph-state guard, and local
projection dependencies. Neither binary MAY retain ownership buckets, services, shutdown joins, tokens, or legacy
mutation subjects. A grep of all binaries that import migrated packages MUST prove explicit parity.

#### Scenario: Sister binaries cannot half-migrate

- **GIVEN** the breaking mutation contract is implemented
- **WHEN** both production composition roots start
- **THEN** each exposes the same four-operation provider and exact-read dependencies
- **AND** neither imports or wires `pkg/ownership`

## ADDED Requirements

### Requirement: Typed request ports define mutation composition

`NATSRequestPort.Interface` MUST survive flat definition construction, typed config construction, JSON round trips, and
flow-graph extraction. A validated flow MUST provide exactly one input port for interface `semstreams.graph.mutation`
v1 and family `graph.mutation.>`, and MAY provide many compatible outputs. Zero providers is unresolved; multiple
providers are ambiguous. This rule observes declared topology only and MUST NOT claim account-wide process cardinality.

#### Scenario: Multiple requesters share one provider

- **GIVEN** three components declare compatible mutation output ports and graph-ingest declares one provider input
- **WHEN** the flow graph is validated
- **THEN** all three requesters connect to the provider
- **AND** validation creates no stream, consumer, leader, or lease
