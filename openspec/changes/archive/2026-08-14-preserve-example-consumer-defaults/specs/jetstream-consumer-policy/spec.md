## ADDED Requirements

### Requirement: Component-specific consumer defaults survive canonical extraction

The document and IoT example processors SHALL retain their established local consumer defaults. Omitted
`deliver_policy` SHALL resolve to `all`, and omitted `ack_policy` SHALL resolve to `explicit`. A zero `max_deliver`,
whether produced by omission or explicit JSON zero, SHALL resolve to `5`; only a positive explicit `max_deliver` SHALL
override `5`. Explicit valid delivery and acknowledgement declarations SHALL win for their own fields.
`max_ack_pending` SHALL remain independent and SHALL forward exactly according to the ordinary-input policy.

#### Scenario: Zero/default preserves replay-safe cold-start behavior

- **GIVEN** a document or IoT JetStream input omits delivery and acknowledgement policy
- **AND** its `max_deliver` resolves to zero from omission or explicit JSON zero
- **WHEN** the component constructs its final consumer configuration
- **THEN** delivery is `all`, acknowledgement is `explicit`, and maximum delivery is `5`
- **AND** retained input published before consumer creation remains eligible for delivery

#### Scenario: Positive maximum delivery overrides the local default

- **GIVEN** a document or IoT JetStream input declares a positive `max_deliver`
- **WHEN** the component constructs its final consumer configuration
- **THEN** the positive value is preserved exactly
- **AND** zero is never treated as an override of the local value `5`

#### Scenario: Explicit delivery and acknowledgement declarations win independently

- **GIVEN** a document or IoT JetStream input declares valid delivery or acknowledgement policy
- **WHEN** the component constructs its final consumer configuration
- **THEN** each explicit value is preserved exactly
- **AND** a zero `max_deliver` still resolves to `5`

#### Scenario: Acknowledgement admission remains orthogonal

- **GIVEN** a document or IoT input declares positive or `-1` `max_ack_pending`
- **WHEN** component-specific empty and zero/default policies are applied
- **THEN** the exact acknowledgement-admission value reaches the final consumer request
- **AND** initial observation and lifecycle metrics remain governed by the existing policy contract
