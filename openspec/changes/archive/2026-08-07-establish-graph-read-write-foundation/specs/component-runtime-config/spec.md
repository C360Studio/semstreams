# component-runtime-config — delta for establish-graph-read-write-foundation

## ADDED Requirements

### Requirement: Request-port interface identity survives effective configuration

A component's effective `nats-request` port configuration MUST preserve subject family, timeout, direction, required
flag, and interface type/version through schema decode, `BuildPortFromDefinition`, runtime GET config, and flow-graph
analysis. Typed `Config.Interface` wins when supplied; otherwise flat `PortDefinition.Interface` constructs the v1
contract. Runtime configuration MUST NOT silently downgrade a request port to plain `nats` or discard its interface.

#### Scenario: JSON-loaded mutation port keeps its contract

- **GIVEN** JSON configuration declares a required `nats-request` mutation output with interface
  `semstreams.graph.mutation` and family `graph.mutation.>`
- **WHEN** the definition is decoded and built into an effective port
- **THEN** the resulting port carries the same interface and family
- **AND** flow validation classifies it as request/reply rather than pub/sub

### Requirement: Retired semantic ownership configuration is rejected

Graph-ingest, projection, rule, and composition schemas MUST contain no `enforce_owner_lease`, owner token, owner
registry, presence, heartbeat, foreign-edge mode, or semantic ownership field. The clean pre-v1 cutover MUST NOT retain
ignored compatibility fields.

#### Scenario: Old lease setting is not silently ignored

- **GIVEN** a configuration still supplies `enforce_owner_lease`
- **WHEN** post-cutover schema validation runs
- **THEN** validation rejects the unknown retired field
- **AND** startup does not pretend enforcement remains active
