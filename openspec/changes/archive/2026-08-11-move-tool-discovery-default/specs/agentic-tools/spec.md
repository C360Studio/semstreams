<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: Tool discovery has one request/reply address

The agentic-tools component MUST retain the logical input port name `tool.list`. That port MUST have kind
`nats-request` and default subject `discovery.tool.list`.

At startup, the runtime MUST resolve the logical port's request/reply facts and subscribe only to the resulting
subject. It MUST NOT also subscribe to the former default `tool.list`, create an alias, or fall back to a hard-coded
address when port resolution fails.

#### Scenario: Default discovery uses the new address

- **GIVEN** agentic-tools uses its default port configuration
- **WHEN** the component starts
- **THEN** logical port `tool.list` resolves as kind `nats-request`
- **AND** the runtime subscribes to `discovery.tool.list`
- **AND** it does not subscribe to subject `tool.list`

#### Scenario: A same-kind custom subject is authoritative

- **GIVEN** logical port `tool.list` is configured as kind `nats-request` with a custom subject
- **WHEN** the component starts
- **THEN** the runtime subscribes only to the custom subject
- **AND** neither default nor former-default subscription is added

### Requirement: An incompatible discovery-port kind fails startup

An override for logical port `tool.list` MUST retain kind `nats-request`. Kind `nats`, `jetstream`, or any other
incompatible port facts MUST fail component startup with an actionable error. The framework MUST NOT repair,
reinterpret, or silently accept the incompatible declaration.

#### Scenario: A legacy nats override is rejected

- **GIVEN** logical port `tool.list` is explicitly configured with kind `nats`
- **WHEN** agentic-tools starts
- **THEN** startup fails
- **AND** the error names port `tool.list`, expected kind `nats-request`, and the observed incompatible kind
- **AND** no discovery subscription is installed

### Requirement: Discovery subscription is startup-atomic and fail-closed

Before allocating any runtime subscription, agentic-tools MUST resolve and validate the discovery request port and all
JetStream input facts required by that startup attempt. The component MUST set `running=true` only after discovery and
every required local input consumer have started successfully.

A discovery-subscription failure MUST return a transient observable startup error with component, start, and
discovery-subscribe context. The returned error MUST preserve the underlying cause for `errors.Is`. The failed attempt
MUST leave no discovery subscription, active local consumer, or tracked startup resource and MUST leave
`running=false`.

If a later input-consumer setup fails, startup MUST roll back the discovery subscription and every local consumer
started by that attempt, clear the tracked subscription and consumer state, leave `running=false`, and return the
setup error. Rollback MUST NOT delete a durable consumer or its delivery position. After either failure, a subsequent
`Start` MUST begin from clean local state and be able to succeed when its dependencies are healthy.

#### Scenario: Discovery subscription failure leaves no false running state

- **GIVEN** valid discovery and JetStream input facts
- **AND** the discovery request subscription fails with `natsclient.ErrNotConnected`
- **WHEN** agentic-tools starts
- **THEN** startup returns a transient error with `Component`, `Start`, and discovery-subscribe context
- **AND** `errors.Is(err, natsclient.ErrNotConnected)` is true
- **AND** no discovery subscription, local consumer, or tracked startup resource remains
- **AND** `running` remains false
- **AND** a later `Start` can succeed cleanly after the transport recovers

#### Scenario: A later consumer failure rolls back the startup attempt

- **GIVEN** discovery subscription succeeds
- **AND** one or more local input consumers start during the same attempt
- **WHEN** a later required consumer setup fails
- **THEN** startup returns the consumer setup error
- **AND** discovery and every local consumer started by that attempt are stopped
- **AND** no discovery subscription or tracked local consumer remains
- **AND** `running` remains false
- **AND** no durable consumer or durable delivery position is deleted
- **AND** a later `Start` can succeed cleanly

### Requirement: The breaking discovery cutover has two live gates

The breaking cutover MUST NOT integrate until both the crud-tools and agentic E2E paths pass on the current corrected
tree. Crud-tools MUST prove a nonempty effect-bearing catalog at `discovery.tool.list`. Agentic E2E MUST prove live
tool execution and result return with stream coverage limited to `tool.execute.>` and `tool.result.>` for tool traffic.
After a startup-ordering or rollback correction, both E2Es MUST be rerun; earlier logs MUST NOT satisfy the gate.

#### Scenario: Crud-tools proves the discovery address

- **GIVEN** the shipped agentic-tools configuration
- **WHEN** the crud-tools E2E requests `discovery.tool.list`
- **THEN** it receives a nonempty tool catalog
- **AND** the existing effect metadata assertions pass

#### Scenario: Agentic execution survives narrowed streams

- **GIVEN** the AGENT stream covers `tool.execute.>` and `tool.result.>` rather than `tool.>`
- **WHEN** the agentic E2E executes a tool call
- **THEN** the tool request is executed
- **AND** its result returns to the loop
