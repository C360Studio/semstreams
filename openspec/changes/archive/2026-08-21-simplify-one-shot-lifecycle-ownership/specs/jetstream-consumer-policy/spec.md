## MODIFIED Requirements

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

## ADDED Requirements

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
`StopAndDeleteConsumer`, `StopAllConsumers`, or `OutstandingWork`. Client Close SHALL be transport-and-worker-only and
SHALL NOT enumerate, drain, stop, delete, or await owner-held consumers or subscriptions.

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
