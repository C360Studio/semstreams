## MODIFIED Requirements

### Requirement: Component ports have one strict canonical grammar

Port configuration SHALL contain only `inputs` and `outputs`.

Each port SHALL contain the common fields `name`, `required`, `description`, and `external`, plus a nested `config`
object discriminated by exactly one of these kinds: `timer`, `network`, `file`, `http-client`, `nats`, `nats-request`,
`jetstream`, `kv-watch`, `kv-read`, `kv-write`, `store-read`, `store-provide`. `external` is an optional boolean on an
input declaring that the port is fed from outside the composition (a UI, a peer process, a rule action) — an operator
statement, not a predicted framework value; composition validation (`composition-validation`) treats it as the one
reason a required stream input may have no in-graph publisher. It travels through the strict envelope codec, port
resolution, the runtime `Port` view, complete named replacement, the admitted declaration and its boot parity check,
and the catalog export unchanged. An output declaring `external` SHALL fail resolution with the port-config error
naming the field; it is never silently ignored.

Unknown kinds, unknown fields, kinds used in a prohibited direction, duplicate or unknown named ports, malformed
durations or network ports, and missing required fields SHALL fail before component initialization. The failure SHALL
identify the component, port, kind, and invalid field when those values are available.

Every `jetstream` port SHALL declare at least one non-empty subject. Every input `jetstream` port SHALL additionally
declare a non-empty `stream_name`. An output `jetstream` port MAY omit `stream_name`; that omission is consumed only by
the canonical generic provisioner and SHALL NOT authorize a consumer-local derivation.

Only network host `0.0.0.0` and request timeout `1s` SHALL receive implicit defaults.

Retired flat port fields, `Config any`, runtime `type`/`data` envelopes, legacy aliases, and top-level KV lanes SHALL
NOT be accepted.

#### Scenario: Canonical definition and runtime views agree

- **GIVEN** a valid canonical port declaration
- **WHEN** it is decoded and exposed through runtime configuration
- **THEN** the definition and runtime views preserve identical direction, kind, required state, external marker,
  description, resource fields, and interface metadata
- **AND** the test that verifies the marker's round trip is `TestPortDefinitionExternalRoundTrip`

#### Scenario: Legacy declaration fails without repair

- **GIVEN** a declaration that uses a retired flat field, legacy alias, runtime `type`/`data` envelope, `Config any`, or
  top-level KV lane
- **WHEN** configuration is decoded
- **THEN** startup fails before component initialization
- **AND** no compatibility repair, alias expansion, or fallback is attempted

#### Scenario: Named merge is complete replacement

- **GIVEN** a valid override naming an existing port
- **WHEN** effective configuration is produced
- **THEN** the override completely replaces that named port
- **AND** omitted kind-specific fields are not inherited from the prior declaration

#### Scenario: Invalid named merge is rejected

- **WHEN** an override names an unknown port, repeats a port name, changes a port's direction, or changes its kind
- **THEN** effective configuration fails with typed component and port context

#### Scenario: JetStream fields survive canonical round-trip

- **GIVEN** a canonical `jetstream` declaration
- **WHEN** it is decoded, merged, exposed through runtime configuration, and projected as normalized facts
- **THEN** its subjects, storage, retention, days, size, replicas, consumer, deliver policy, acknowledgement policy,
  maximum deliveries, acknowledgement wait, heartbeat, maximum pending acknowledgements, and interface metadata are
  unchanged

#### Scenario: Subject-only JetStream input is rejected

- **GIVEN** a canonical input `jetstream` declaration with one or more non-empty subjects and no `stream_name`
- **WHEN** its containing `PortConfig` is decoded for the input lane
- **THEN** decoding resolves the declaration for the input direction and fails before component initialization
- **AND** the typed failure identifies the port, kind `jetstream`, and field `stream_name`
- **AND** no component-local subject derivation or default supplies the missing backing stream
- **AND** no partially decoded input or output lane is assigned

#### Scenario: JetStream input without subjects is rejected

- **GIVEN** a canonical input `jetstream` declaration with a non-empty `stream_name` and no non-empty subjects
- **WHEN** the declaration is resolved
- **THEN** resolution fails before component initialization
- **AND** the typed failure identifies field `subjects`

#### Scenario: Subject-only JetStream output remains valid

- **GIVEN** a canonical output `jetstream` declaration with one or more non-empty subjects and no `stream_name`
- **WHEN** its containing `PortConfig` is decoded for the output lane
- **THEN** resolution succeeds with the declared subjects and an omitted stream name
- **AND** only the canonical generic provisioner may derive the physical stream name

#### Scenario: Retired agentic-model stream default is absent

- **GIVEN** an agentic-model component configuration
- **WHEN** its schema, defaults, documentation, and shipped configurations are inspected
- **THEN** no top-level `stream_name` field is exposed
- **AND** each JetStream input carries its explicit backing `stream_name` on the canonical port declaration
