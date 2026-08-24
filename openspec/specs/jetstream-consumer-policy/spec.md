# jetstream-consumer-policy Specification

## Purpose
TBD - created by archiving change honor-jetstream-max-ack-pending. Update Purpose after archive.
## Requirements
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

`ConsumeStreamWithConfig` and `ConsumeStreamWithConfigContexts` SHALL require nonempty component and port context,
complete every fallible stream, consumer, policy, and observation setup step before delivery, and then return the exact
native `jetstream.ConsumeContext` created at the delivery commit point. No fallible setup step SHALL follow successful
`Consumer.Consume`. Former error-only signatures and a stateful SemStreams managed-consumer wrapper SHALL NOT remain.

The canonical signatures SHALL be:

```go
func (c *Client) ConsumeStreamWithConfig(
    ctx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) (jetstream.ConsumeContext, error)

func (c *Client) ConsumeStreamWithConfigContexts(
    setupCtx context.Context,
    handlerCtx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) (jetstream.ConsumeContext, error)
```

Temporary `ConsumeStreamWithConfigHandle` and `ConsumeStreamWithConfigContextsHandle` aliases or bridges SHALL NOT
remain after the canonical cutover.

`ConsumeDurable` SHALL NOT exist or have an alias. Retained durable owners use `NewDurableHandler`, pass its returned
handler to the canonical port-backed operation, and retain the exact native handle.

#### Scenario: setup fails before commit
- **GIVEN** any setup or observation step fails
- **WHEN** the operation returns
- **THEN** delivery has not begun and no lifecycle handle is published

#### Scenario: split-context setup returns ownership
- **WHEN** split-context setup succeeds
- **THEN** setup observation used setup context and handlers use handler context
- **AND** the owner receives the exact native handle for Drain and Closed

#### Scenario: Missing owner context fails before I/O
- **WHEN** a port-backed operation receives empty component or port context
- **THEN** it returns typed invalid configuration before consumer creation

#### Scenario: Split-context consumption remains observed
- **GIVEN** setup and handler lifetimes differ
- **WHEN** `ConsumeStreamWithConfigContexts` creates the consumer
- **THEN** setup observation uses setup context and delivered handlers use handler context
- **AND** the owner receives the exact native handle

#### Scenario: Temporary bridge is absent
- **WHEN** the canonical consumption surface is enumerated
- **THEN** neither temporary `*Handle` method exists
- **AND** every SemStreams caller uses the canonical method and retains its result

### Requirement: Non-port consumption is explicit and bounded

Consumers with no `JetStreamPort` contract MAY use `ConsumeInternalStreamWithConfig`; port-backed consumers SHALL NOT.
The operation SHALL complete every fallible setup/observation step before `Consumer.Consume`, return the exact native
`jetstream.ConsumeContext`, and require the caller to retain it through exact Closed. No internal split-context or
durable convenience operation SHALL exist without a new consumer inventory and owner review.

#### Scenario: internal consumption returns exact ownership
- **GIVEN** a non-port framework consumer
- **WHEN** `ConsumeInternalStreamWithConfig` commits delivery
- **THEN** its caller receives and retains the exact native handle through Closed

#### Scenario: Production call-site census remains separated
- **WHEN** production consumer call sites are enumerated
- **THEN** every `GetConsumerConfig` caller avoids the internal operation
- **AND** internal callers equal the named framework census and retain their exact handles

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

The framework SHALL retain the three existing consumer-policy metrics and their labels/source semantics. Refresh
failure SHALL remove stale effective truth and set observation availability to zero while retaining requested truth.
Exact observation MAY retain a concurrency-guarded `Consumer.Info` handle, but SHALL NOT own Drain, Stop, deletion, or
Client child cleanup. Observation records SHALL be removed when the resource owner reports exact native Closed. No
replacement, Stop-by-name, delete-by-name, or Client Close path SHALL masquerade as lifecycle observation cleanup.

#### Scenario: owner closes exact consumer
- **WHEN** the owner observes exact native Closed
- **THEN** all policy series and private observation state for that consumer are removed

#### Scenario: Refresh failure removes stale effective truth
- **GIVEN** a tracked consumer previously reported an effective value
- **WHEN** refresh fails
- **THEN** requested truth remains, effective truth is removed, and observation availability becomes zero

#### Scenario: Lifecycle cleanup removes all policy series
- **WHEN** the exact resource owner reports native Closed
- **THEN** all three series for its private observation record are removed
- **AND** no replacement, name-routed lifecycle, or Client Close path performs that cleanup

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

### Requirement: Consumer setup tolerates stream-visibility lag

Consumer setup MUST re-observe a stream reported absent until it becomes visible or a bounded framework-owned budget
is spent, because a clustered node that has not applied the meta assignment reports a stream that exists as absent.
Only the absent classification SHALL be re-observed; every other lookup failure SHALL be returned on first
observation. The budget SHALL be framework-owned, with no operator configuration and no caller-supplied wait, so no
adopter predicts a propagation delay the framework observes. A returned absent classification therefore means the
stream was absent continuously for at least the budget, and a setup that fails this way carries the budget's latency.

#### Scenario: The stream becomes visible within the budget

- **GIVEN** consumer setup is answered "stream not found" by the node serving the request
- **WHEN** the stream becomes visible before the budget is spent
- **THEN** setup proceeds and the consumer is created

#### Scenario: The stream never becomes visible

- **GIVEN** consumer setup is answered "stream not found" for the whole budget
- **WHEN** the budget is spent
- **THEN** setup fails with the established transient classification
- **AND** `jetstream.ErrStreamNotFound` remains reachable through `errors.Is` on the returned error

#### Scenario: A lookup fails for a reason other than absence

- **GIVEN** the stream lookup fails with a permission, transport, or cancellation error
- **WHEN** setup observes that failure
- **THEN** it is returned without any further observation of the stream

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

### Requirement: Durable settlement composition is stateless

The framework SHALL expose exactly
`NewDurableHandler(cfg StreamConsumerConfig, heartbeat time.Duration,
work func(context.Context, []byte) error) (func(context.Context, jetstream.Msg), error)`.

The builder SHALL reject nil work and nonpositive heartbeat before acquisition. When BackOff is nonempty, every entry
SHALL be positive and the effective acknowledgement wait SHALL be its minimum entry regardless of order. An invalid
BackOff error SHALL identify its index and value. Without BackOff, positive AckWait SHALL be effective and nonpositive
AckWait SHALL resolve to the 30-second default.

Validation SHALL reject `heartbeat > effectiveAckWait/2`, permit equality, use division rather than multiplication,
and identify the heartbeat and computed ceiling in its error. The returned handler SHALL delegate Ack, Nak, Term,
InProgress, cancellation, heartbeat failure, and work join to `ConsumeWithHeartbeat`. Every nonnil result SHALL emit
a WARN with exact message `ConsumeDurable handler error` and fields `stream`, `consumer`, and `error`; the result SHALL
NOT be suppressed, sampled, or downgraded. The builder SHALL retain no context and SHALL own no consumer, handle,
goroutine, identity, catalog, Stop, deletion, or replay authority.

#### Scenario: Heartbeat equality is valid
- **GIVEN** heartbeat equals half the effective AckWait
- **WHEN** a durable handler is built
- **THEN** validation succeeds without multiplying the heartbeat

#### Scenario: BackOff controls the tightest heartbeat ceiling
- **GIVEN** BackOff contains positive, nonmonotonic intervals
- **WHEN** a durable handler is built
- **THEN** the minimum interval determines the acknowledgement wait regardless of position
- **AND** heartbeat equal to half that minimum is valid

#### Scenario: Invalid BackOff identifies the entry
- **GIVEN** BackOff contains a zero or negative interval
- **WHEN** a durable handler is built
- **THEN** it fails before acquisition and identifies the interval index and value

#### Scenario: Invalid durable handler configuration fails before acquisition
- **GIVEN** nil work, nonpositive heartbeat, or heartbeat greater than half the effective AckWait
- **WHEN** a durable handler is built
- **THEN** it returns an error and no consumer or goroutine is created

#### Scenario: Durable settlement remains exclusive
- **WHEN** the returned handler processes a JetStream message
- **THEN** `ConsumeWithHeartbeat` exclusively controls InProgress and terminal settlement
- **AND** the work callback does not receive settlement authority

#### Scenario: Durable handler failure remains operator-visible
- **WHEN** `ConsumeWithHeartbeat` returns a nonnil result
- **THEN** one WARN uses exact message `ConsumeDurable handler error`
- **AND** it contains `stream`, `consumer`, and `error` fields without sampling or downgrade

### Requirement: Client owns no consumer or subscription children

Client SHALL NOT retain consumer or subscription child catalogs and SHALL NOT expose `StopConsumer`,
`StopAndDeleteConsumer`, `StopAllConsumers`, or `OutstandingWork`. Client Close SHALL own only native connection
transport drain completion: it MUST initiate and await `nats.Conn` drain completion, but SHALL NOT enumerate,
name-route, or retain lifecycle authority over owner-held consumer or subscription handles, nor directly invoke their
lifecycle methods. Native connection drain waiting for its own subscription callbacks does not grant Client child
lifecycle ownership.

`Subscription.Drain(context.Context)` behavior remains unchanged by this capability.

Client-scoped internal identity claims SHALL remain handle-free and release on precommit failure or exact Closed,
never Client Close. Consumer-policy observation and metrics, `ObserveDirectPortConsumerPolicy`, OTEL process claims,
internal consumer creation, graph-ingest readiness, and agentic-loop inflight observation SHALL remain independently
owned. Removing child lifecycle authority SHALL NOT merge or delete those mechanisms. Unknown observation SHALL NOT
be reported as zero, and current cross-Client metric-label collision debt SHALL NOT be reclassified as lifecycle.

#### Scenario: Client Close does not stop owner children
- **GIVEN** composition has owner-held consumers or subscriptions
- **WHEN** Client Close begins after owner Stops
- **THEN** it performs no child enumeration or name-routed lifecycle action
- **AND** it awaits native connection drain completion before returning

#### Scenario: Name-routed lifecycle APIs are absent
- **WHEN** the Client lifecycle surface is enumerated
- **THEN** Client has no consumer Stop, delete, stop-all, or outstanding-work lookup by name

#### Scenario: Independent observation survives child-catalog removal
- **WHEN** the current observation surface is enumerated
- **THEN** policy metrics, direct-port observation, graph readiness, and agent-loop inflight remain available
- **AND** none gains Stop, deletion, replacement, or Client Close authority

### Requirement: Lifecycle deletion configuration is absent without a replacement mechanism

The five production `DeleteConsumerOnStop` fields and corresponding generated-schema properties for OTEL exporter,
agentic dispatch, agentic loop, agentic model, and agentic tools SHALL NOT exist. No production configuration, exported
Client deletion method, wildcard cleanup, discovered-name cleanup, or private fixture helper SHALL replace them. No
current SemStreams fixture requires consumer deletion; future fixture cleanup SHALL remain local and exact-identity
scoped if a concrete need arises.

#### Scenario: Discovery advertises no lifecycle deletion knob
- **WHEN** the published component schemas are generated
- **THEN** none advertises `delete_consumer_on_stop`
- **AND** the regression covers OTEL exporter, agentic dispatch, agentic loop, agentic model, and agentic tools

#### Scenario: Existing decoder behavior is preserved
- **GIVEN** stale configuration contains `delete_consumer_on_stop`
- **WHEN** a component decodes it
- **THEN** OTEL rejects it through its existing `DisallowUnknownFields` behavior
- **AND** agentic dispatch, agentic loop, agentic model, and agentic tools ignore it through existing lenient decoding
- **AND** the removal adds no decoder strictness or universal fail-fast guarantee

#### Scenario: Contract scanner scope is honest
- **WHEN** the published-schema regression passes
- **THEN** it proves the five schemas emitted by `registerPublishedComposition` omit the property
- **AND** it does not claim to validate sister-repository copies or runtime unknown-field behavior

### Requirement: Duplicate local durable identity fails rather than replaces

The existing Client-local internal claim SHALL retain its current behavior. For a nonempty durable name, acquisition
SHALL reserve `(stream,durable)` with an opaque pointer token, reject a second live local claim without stopping,
draining, deleting, or replacing the incumbent, roll back every precommit failure, and release only after the exact
native consume handle closes. The claim SHALL NOT store an owner label or become a child-handle catalog.

The Client does not provide sealed pre-Start identity validation or require the duplicate error to name both owners.
This local claim does not assert complete ADR-095 conformance.

#### Scenario: duplicate identity is rejected
- **GIVEN** two local owners resolve to one stream and durable identity
- **WHEN** the second acquisition reaches the Client-local internal claim
- **THEN** it fails with the existing duplicate-local-durable-identity error without replacing the incumbent

#### Scenario: claim lifecycle remains handle-free
- **WHEN** acquisition fails before commit or the committed exact native handle closes
- **THEN** the opaque claim is released by its existing path
- **AND** no owner label, lifecycle handle, or additional lifecycle boundary is added
