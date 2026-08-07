## ADDED Requirements

### Requirement: Port-derived stream declarations consume canonical normalized facts

Raw component configuration SHALL be decoded through canonical `PortConfig` and resolved before stream provisioning.

Only canonical `jetstream` output ports with normalized stream facts SHALL contribute generic stream declarations.

Provisioners SHALL NOT infer stream identity or policy from retired flat fields, unresolved configuration, concrete
configuration type switches, or consumer-local defaults.

#### Scenario: Canonical JetStream output contributes exact facts

- **GIVEN** a valid canonical `jetstream` output
- **WHEN** component-derived stream declarations are collected
- **THEN** its stream name, subjects, storage, retention, size, replicas, and consumer policy are taken from its
  normalized stream facts
- **AND** those values are not reconstructed independently by the provisioner

#### Scenario: Non-JetStream output does not contribute a stream

- **GIVEN** a valid output whose canonical kind is not `jetstream`
- **WHEN** component-derived stream declarations are collected
- **THEN** that output contributes no generic stream declaration
- **AND** its concrete configuration type is not inspected for stream-like fields

#### Scenario: Invalid output fails without fallback derivation

- **GIVEN** an output that cannot be decoded or normalized
- **WHEN** stream declarations are collected
- **THEN** collection fails with typed component, port, kind, and field context
- **AND** no stream declaration is inferred from partial or legacy fields

#### Scenario: Gated-DAG specialization remains narrow

- **GIVEN** the approved gated-DAG specialized provisioning path
- **WHEN** its work-queue stream is provisioned
- **THEN** canonical port facts own resource identity, stream name, subjects, storage, and work-queue retention
- **AND** only the local specialized provisioner owns its exact `MaxBytes`, discard-new behavior, `MaxAge`, and
  deduplication policy
- **AND** that exception does not authorize generic provisioners or other consumers to infer port meaning
