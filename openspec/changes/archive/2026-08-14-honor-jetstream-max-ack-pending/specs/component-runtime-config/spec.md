## ADDED Requirements

### Requirement: Canonical port-field constraints govern runtime and generated schemas

The canonical port binding catalog SHALL own numeric minima and allowed directions through `PortFieldInfo`. Runtime port
resolution, runtime discovery, and checked-in schema generation SHALL consume the same metadata. Zero SHALL remain
omission for an `omitempty` numeric field.

#### Scenario: Runtime discovery reports the canonical constraint

- **GIVEN** the JetStream `max_ack_pending` field
- **WHEN** its `PortFieldInfo` is inspected
- **THEN** minimum is `-1`
- **AND** allowed directions contain only input

#### Scenario: Runtime validation consumes the minimum

- **GIVEN** a JetStream input declares `max_ack_pending: -2`
- **WHEN** canonical resolution runs
- **THEN** it fails with port, kind, and field context

#### Scenario: Generated schemas consume direction and minimum

- **GIVEN** a generated component schema contains JetStream ports
- **WHEN** input and output variants are inspected
- **THEN** the input contains `max_ack_pending` with minimum `-1`
- **AND** the output contains `max_ack_pending` constrained to the single value `0`
- **AND** omission remains valid while positive and `-1` output declarations fail schema validation

### Requirement: JetStream outputs reject consumer acknowledgement admission

A JetStream output SHALL reject any nonzero `max_ack_pending` because it creates no consumer. Zero or omission SHALL
remain valid.

#### Scenario: Positive or unlimited output is rejected

- **GIVEN** a JetStream output declares a positive value or `-1`
- **WHEN** canonical direction validation runs
- **THEN** configuration fails before component initialization
- **AND** the error identifies the output port, JetStream kind, and field

#### Scenario: Zero output preserves omission behavior

- **GIVEN** a JetStream output omits the field or supplies zero
- **WHEN** canonical resolution runs
- **THEN** the output remains valid
- **AND** no consumer-policy observation is created
