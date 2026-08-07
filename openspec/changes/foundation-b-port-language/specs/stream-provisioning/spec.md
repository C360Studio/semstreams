## ADDED Requirements

### Requirement: Port-derived stream declarations consume canonical normalized facts

Raw component configuration SHALL be decoded through canonical `PortConfig` and resolved before stream provisioning.

Only canonical `jetstream` output ports with normalized stream facts SHALL contribute generic stream declarations.

When a canonical JetStream output omits `stream_name`, only the canonical generic provisioner MAY derive the physical
stream name from its declared subjects. No input consumer, component-local helper, or specialized non-provisioning
path may use that derivation.

Provisioners SHALL NOT infer stream identity or policy from retired flat fields, unresolved configuration, concrete
configuration type switches, or consumer-local defaults.

#### Scenario: Named canonical JetStream output contributes exact facts

- **GIVEN** a valid canonical `jetstream` output with an explicit `stream_name`
- **WHEN** component-derived stream declarations are collected
- **THEN** its stream name, subjects, storage, retention, size, replicas, and consumer policy are taken from its
  normalized stream facts
- **AND** those values are not reconstructed independently by the provisioner

#### Scenario: Generic provisioner derives omitted output stream name

- **GIVEN** a valid canonical JetStream output with non-empty subjects and no `stream_name`
- **WHEN** the canonical generic provisioner collects component-derived stream declarations
- **THEN** it derives the physical stream name from the output subjects
- **AND** no other consumer or component-local helper performs that derivation

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
