## ADDED Requirements

### Requirement: Every port-backed JetStream input has an explicit acknowledgement-admission disposition

Ordinary inputs SHALL forward positive and `-1` `max_ack_pending` values exactly and SHALL leave zero unset. The final
effective policy SHALL be observed before delivery. Non-port consumers SHALL NOT claim this contract.

#### Scenario: Ordinary input forwards a declared value

- **GIVEN** an ordinary JetStream input declares a positive value or `-1`
- **WHEN** its consumer is created or updated
- **THEN** the final request carries that exact value
- **AND** delivery begins only after the effective value is observed

#### Scenario: Zero leaves policy to NATS

- **GIVEN** an ordinary input omits `max_ack_pending` or declares zero
- **WHEN** startup observes the consumer
- **THEN** requested policy is zero
- **AND** any successfully observed inherited, default, or capped value is accepted

### Requirement: Agentic acknowledgement-admission policies remain component-owned

Agentic-loop SHALL retain values 1 for task/response/tool-result and 10 for its advisory input. Agentic-model SHALL
retain 1 and agentic-tools SHALL retain 3. Each SHALL reject every nonzero port declaration before consumer creation.

#### Scenario: Component-owned declaration is rejected

- **GIVEN** a component-owned agentic input declares any nonzero value
- **WHEN** consumer setup runs
- **THEN** startup fails with typed invalid configuration naming component, port, field, and fixed value
- **AND** no consumer starts

### Requirement: Every exported port-backed consumption operation requires policy context

`ConsumeStreamWithConfig`, `ConsumeStreamWithConfigContexts`, and `ConsumeDurable` SHALL require nonempty component and
port context and SHALL observe policy before delivery. Their former signatures SHALL NOT remain as compatibility paths.

#### Scenario: Missing owner context fails before I/O

- **WHEN** a port-backed operation receives empty component or port context
- **THEN** it returns typed invalid configuration before consumer creation

#### Scenario: Split-context consumption remains observed

- **GIVEN** setup and handler lifetimes differ
- **WHEN** `ConsumeStreamWithConfigContexts` creates the consumer
- **THEN** setup observation uses the setup context
- **AND** delivered handlers use the declared handler context

### Requirement: Non-port consumption is explicit and bounded

Consumers with no `JetStreamPort` contract MAY use `ConsumeInternalStreamWithConfig`. Port-backed consumers SHALL NOT
use it. No internal split-context or durable convenience operation SHALL exist without a new consumer inventory.

#### Scenario: Production call-site census remains separated

- **WHEN** production consumer call sites are enumerated
- **THEN** every `GetConsumerConfig` caller avoids the internal operation
- **AND** internal callers equal the named framework census

### Requirement: The legacy unclassified stream consumer is retired

The exported `Client.ConsumeStream(ctx, streamName, subject, handler)` operation SHALL NOT exist, nor SHALL an equivalent
stream/subject-only alias create consumers outside the classified operations.

#### Scenario: Exported API has no ambiguous creator

- **WHEN** natsclient consumer-creation methods are enumerated
- **THEN** `ConsumeStream` and equivalent convenience aliases are absent

### Requirement: Direct OTEL observation derives policy from creation artifacts

OTEL SHALL pass the exact final nats.go config and returned consumer handle to natsclient before fetch begins. Requested
policy SHALL derive from that config and identity/effective policy from `ConsumerInfo`. Cleanup SHALL be opaque.

#### Scenario: Initial Info failure prevents fetch

- **GIVEN** direct consumer creation succeeds but initial `Info` fails
- **WHEN** OTEL registers observation
- **THEN** startup fails transiently
- **AND** no metric record or fetch goroutine starts

### Requirement: Consumer policy metrics never retain stale effective truth

The framework SHALL expose exactly `semstreams_jetstream_consumer_max_ack_pending_requested`,
`semstreams_jetstream_consumer_max_ack_pending_effective`, and
`semstreams_jetstream_consumer_max_ack_pending_observation_available`, each labeled by `component`, `port`, `stream`,
`consumer`, and `policy_source`. Source SHALL be `port`, `component`, or `server`.

#### Scenario: Refresh failure removes stale effective truth

- **GIVEN** a tracked consumer previously reported an effective value
- **WHEN** refresh fails
- **THEN** requested remains
- **AND** effective is removed
- **AND** observation availability becomes zero

#### Scenario: Lifecycle cleanup removes all policy series

- **WHEN** a managed consumer is replaced, stopped, deleted, or closed, or a direct OTEL consumer stops
- **THEN** all three series for its private record are removed

### Requirement: Successful initial observation emits one identity-complete record

Initial success SHALL emit exactly one INFO record with message `JetStream consumer acknowledgement policy applied` and
fields `component`, `port`, `stream`, `consumer`, `policy_source`, `requested_max_ack_pending`, and
`effective_max_ack_pending`. Refresh SHALL NOT repeat it.

#### Scenario: Server-owned zero is recorded honestly

- **GIVEN** the final request is zero
- **WHEN** observation succeeds
- **THEN** source is `server`, requested is zero, and effective is the observed value

### Requirement: Consumer policy failures have stable classifications

NATS API errors 10121 and 10082 SHALL be invalid configuration while preserving the original API error and code.
Transport/unavailable and initial `Info` failures SHALL remain transient. Unequal nonzero requested/effective values
SHALL be invalid configuration.

#### Scenario: Policy rejection is not retryable transport failure

- **WHEN** create/update returns API error 10121 or 10082
- **THEN** startup returns typed invalid configuration
- **AND** delivery and policy tracking do not start

### Requirement: Metric registration returns one canonical collector

Compatible repeated GaugeVec registration SHALL return the identical registered collector. Incompatible collector type
or descriptor collisions SHALL fail fatally.

#### Scenario: Two clients share policy collectors

- **GIVEN** two clients use one metrics registry
- **WHEN** both initialize policy metrics
- **THEN** both retain the same registered GaugeVec instances

### Requirement: Policy updates preserve durable state

Declaration changes SHALL use `CreateOrUpdateConsumer` and SHALL NOT delete and recreate the durable merely to change
`MaxAckPending`.

#### Scenario: Changed policy updates in place

- **GIVEN** an existing durable consumer
- **WHEN** component replacement changes an honored value
- **THEN** the consumer is updated without discarding durable position

