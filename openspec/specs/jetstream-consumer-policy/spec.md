# jetstream-consumer-policy Specification

## Purpose

The CONSUME SIDE of a JetStream stream: how a consumer is declared, acquired, and observed, and how a delivery is
settled. It covers the acknowledgement-admission disposition every port-backed input must declare, the policy context
an exported consumption operation requires, the bounds on non-port consumption, the identity and staleness rules for
consumer-policy metrics and OTEL observation, who owns the consumer handle and its lifecycle, and the semantic
settlement surface — `ConsumeDeliveryWithHeartbeat` with `HeartbeatDeliveryPolicy`, `DeliveryDecision`, and
`DeliveryResult` — including how lease renewal, semantic retry, cancellation, and control loss differ.
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

`ConsumeDurable` and `NewDurableHandler` SHALL NOT exist or have aliases. A durable heartbeat owner SHALL validate
`HeartbeatDeliveryPolicy` from the exact `StreamConsumerConfig` used for acquisition, pass a handler that invokes
`ConsumeDeliveryWithHeartbeat` to the canonical port-backed operation, inspect the returned `DeliveryResult`, and
retain the exact native handle.

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

#### Scenario: Transitional durable builders are absent

- **WHEN** the durable-consumption surface and production call sites are enumerated
- **THEN** neither `ConsumeDurable` nor `NewDurableHandler` exists or has an alias
- **AND** durable heartbeat owners compose the permanent typed policy and delivery APIs with an owner-held canonical
  consume operation

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

Consumer setup MUST re-observe a stream its guarded lookup reports absent until it becomes visible or a bounded
framework-owned budget is spent, because a clustered node that has not applied the meta assignment reports a stream
that exists as absent. Only the absent classification SHALL be re-observed; every other lookup failure SHALL be
returned on first observation. The budget SHALL be framework-owned, with no operator configuration and no
caller-supplied wait, so no adopter predicts a propagation delay the framework observes. The guarded lookup therefore
reports absence only after re-observing it for the whole budget, and a setup that fails that way carries the budget's
latency; that fact is carried by the not-visible sentinel, NOT by the absent classification, which other seams also
emit. The auto-create pre-check is exempt from the wait: an absent answer there is the trigger to create the stream,
not a failure to report.

A caller deciding anything durable from absence — disabling a subscriber, skipping a component, reporting a deployment
shape — MUST branch on the framework's exported not-visible sentinel rather than on the absent classification alone,
because consumer creation and initial consumer observation can also answer with the absent classification for a stream
that is present.

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

#### Scenario: A spent budget is durable evidence of absence

- **GIVEN** consumer setup re-observed a stream reported absent until the budget was spent
- **WHEN** the failure reaches the caller
- **THEN** the returned error carries the framework's exported not-visible sentinel
- **AND** it carries the absent classification on the same error

#### Scenario: The caller ends the wait before the budget

- **GIVEN** the caller's context ends while setup is still re-observing an absent stream
- **WHEN** the failure reaches the caller
- **THEN** the returned error carries the caller's own cause and the absent classification
- **AND** it does NOT carry the not-visible sentinel, because a wait cut short measured nothing

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

### Requirement: semantic heartbeat settlement has one permanent exported surface

The framework SHALL expose `ConsumeDeliveryWithHeartbeat` with validated `HeartbeatDeliveryPolicy`,
`DeliveryDecision`, and `DeliveryResult`.

`NewDurableHandler` and `ConsumeWithHeartbeat` SHALL NOT exist or have an alias. Every original model, tools,
dispatch, loop, and AgentRun heartbeat binding SHALL use the permanent typed surface with its owner-specific durable
definition of done.

No capability SHALL describe a production legacy allowlist. The caller ratchet SHALL assert zero declarations and
zero references to the removed helper in every package; it is retirement conformance only.

#### Scenario: public surface at this layer

- **WHEN** this change is archived
- **THEN** the permanent typed API exists
- **AND** `NewDurableHandler` and every alias are absent with zero production callers
- **AND** `ConsumeWithHeartbeat` is absent: no declaration, alias, or production caller

#### Scenario: binding migration requires semantic authority

- **WHEN** a durable binding migrates
- **THEN** its decision matrix names the exact durable positive and negative consequences
- **AND** nil/error callback behavior alone does not authorize ACK or Retry

#### Scenario: fast lane lacks an admitted settlement route

- **WHEN** an inventoried fast no-heartbeat lane cannot use an existing owner path
- **THEN** migration stops for a separately reviewed capability delta
- **AND** no raw message settlement or exported no-heartbeat interpreter is introduced

### Requirement: delivery work returns a validated decision/error tuple

Typed work SHALL implement
`DeliveryWork func(context.Context, []byte) (DeliveryDecision, error)`. The exported decisions
SHALL remain Invalid, ACK, Retry, Terminate, and Quarantine using the exact `DeliveryDecision*` constants.

The framework SHALL validate non-nil work before acquisition. For each admitted delivery it SHALL supply that
delivery's body as read-only invocation-scoped bytes and nothing else. It
SHALL NOT expose `jetstream.Msg`, headers, reply subjects, sequences, delivery counts, consumer identity, or another
settlement-capable interface to work.

ACK SHALL require nil error. Retry, Terminate, and Quarantine SHALL require non-nil error. Invalid, unknown, and every
mismatched tuple SHALL preserve the requested decision, attempt no terminal method, expose an
`InvalidDeliveryDecisionError`, quarantine, and require owner stop. A supplied error SHALL remain reachable through
the typed cause. Recovered panic SHALL synthesize Quarantine with `DeliveryWorkPanicError`.

Existing `TerminateDelivery(error) error` and `PermanentDeliveryError` SHALL retain their exact behavior and SHALL
not be deprecated or removed by this change.

An owner's decision matrix SHALL classify every error it settles positively, terminally, or by retry, and SHALL do so
from a typed cause rather than from the absence of another class. Retry SHALL require a typed result proving that no
external effect began or that the durable consequence is already committed. Owner cancellation surfacing from work
is such a result only where the lane's effect site provably cannot return a bare context error; where it can, a
cancellation is unclassified and SHALL fail closed. An error the matrix does not classify
SHALL return Quarantine: no terminal method, delivery left pending, lane latched, exact handle stopped. No owner
matrix SHALL make Retry its unclassified default.

#### Scenario: unclassified owner error fails closed

- **WHEN** a migrated binding's work observes an error its decision matrix does not classify
- **THEN** it returns Quarantine with that error as the cause
- **AND** no Ack, Nak, delayed Nak, or Term is attempted
- **AND** the exact owner stops the lane for explicit reconstruction

#### Scenario: valid ACK

- **WHEN** work returns `DeliveryDecisionAck, nil`
- **THEN** the framework attempts Ack
- **AND** the result records ACK with nil semantic cause

#### Scenario: setup validates work before acquisition

- **WHEN** DeliveryWork is nil
- **THEN** heartbeat-policy validation fails
- **AND** no consumer is acquired and no message operation occurs

#### Scenario: policy is reused across deliveries

- **WHEN** one validated policy handles two deliveries with different bodies
- **THEN** each invocation receives its own current body
- **AND** no payload is retained in the policy

#### Scenario: settlement authority does not escape

- **WHEN** typed work runs
- **THEN** it receives context and read-only payload bytes only
- **AND** Ack, Nak, Term, InProgress, native message, headers, sequences, and consumer identity remain exclusively
  inside natsclient

#### Scenario: decision requires a cause

- **WHEN** work returns Retry, Terminate, or Quarantine with nil error
- **THEN** the result preserves the requested decision
- **AND** handling quarantines with `InvalidDeliveryDecisionError`
- **AND** no terminal method is attempted

#### Scenario: ACK incorrectly carries an error

- **WHEN** work returns ACK with a non-nil error
- **THEN** the result preserves ACK as the requested decision
- **AND** the invalid-decision cause unwraps the supplied error

#### Scenario: unknown decision

- **WHEN** work returns an enum value outside the declared constants
- **THEN** the result preserves that numeric decision
- **AND** handling quarantines and requires owner stop

#### Scenario: work panics

- **WHEN** work panics before returning a tuple
- **THEN** the result records Quarantine with `DeliveryWorkPanicError`
- **AND** no terminal method is attempted

### Requirement: delivery metadata is validated before work

For each valid typed delivery, natsclient SHALL call `msg.Metadata()` exactly once before Data or work and SHALL
require a positive `NumDelivered`. The framework SHALL NOT expose the delivery count, or any other metadata field,
to work: no production binding reads one, and redelivery is not proof that prior work started or committed.

Metadata error, nil metadata, or zero delivery number SHALL produce Quarantine with typed
`DeliveryMetadataUnavailableError`, require owner stop, and call neither Data, work, heartbeat, nor a terminal
settlement method. An underlying metadata error SHALL remain reachable through the typed cause.

#### Scenario: valid metadata precedes payload and work

- **WHEN** metadata reports a positive `NumDelivered` on a first or later delivery
- **THEN** Metadata is read exactly once, before Data and before work
- **AND** work receives the payload bytes with no delivery-count affordance

#### Scenario: metadata is unavailable

- **WHEN** Metadata errors, returns nil, or reports delivery number zero
- **THEN** the result quarantines with typed `DeliveryMetadataUnavailableError`
- **AND** OwnerStopRequired is true
- **AND** Data, work, heartbeat, Ack, Nak, delayed Nak, and Term are not called

### Requirement: semantic retry and consumer lease policy are distinct

Work SHALL return Retry without timing. The owner SHALL supply an opaque immediate or fixed-delay retry policy at setup.
Consumer AckWait/BackOff SHALL govern server lease and missing-settlement redelivery and SHALL NOT supply semantic retry
timing.

#### Scenario: delayed semantic retry

- **WHEN** work returns Retry under a 30-second fixed-delay policy
- **THEN** the framework calls `NakWithDelay(30s)`
- **AND** preserves the retry cause whether that method succeeds or fails

#### Scenario: invalid retry policy

- **WHEN** policy is zero or its delay is nonpositive
- **THEN** setup fails before acquisition or message I/O

### Requirement: heartbeat policy validates the actual consumer lease

The framework SHALL validate heartbeat from the same `StreamConsumerConfig` passed to acquisition. Heartbeat SHALL be
positive and no greater than half the effective interval. Effective interval SHALL be shortest positive BackOff when
present, otherwise positive AckWait, otherwise 30 seconds. Invalid AckWait/BackOff SHALL fail setup.

#### Scenario: Stage A target configurations

- **WHEN** tools validates BackOff 15s/60s with heartbeat 5s
- **AND** dispatch validates effective AckWait 30s with heartbeat 10s
- **THEN** both configurations are admitted before acquisition

#### Scenario: operator BackOff shortens the lease

- **WHEN** an operator supplies BackOff
- **THEN** validation observes its shortest entry
- **AND** rejects heartbeat above half that entry without clamping

#### Scenario: invalid runtime policy touches no message data

- **WHEN** the runtime entry point receives a zero or invalid heartbeat policy
- **THEN** it calls neither Metadata, Data, work, heartbeat, nor terminal settlement
- **AND** it returns the invalid quarantined owner-stop result

### Requirement: delivery results preserve semantic and transport evidence

`DeliveryResult` SHALL expose requested decision/cause, control error, settlement error, local method
attempt/success/failure, quarantine, owner-stop requirement, and aggregate error. It SHALL expose no
server-confirmation accessor: plain terminal methods never provide that confirmation, so a reader that always
answers the same way is a false affordance rather than evidence. A method error SHALL mean unknown/not-confirmed
and SHALL NOT prove redelivery.

#### Scenario: clean local Ack

- **WHEN** ACK is selected and Ack returns nil
- **THEN** local method success is true and `Err` is nil
- **AND** no result value reports server confirmation

#### Scenario: terminal method errors

- **WHEN** Ack, Nak, delayed Nak, or Term returns an error without control loss or quarantine
- **THEN** the method failure remains observable and no result value claims the server settled
- **AND** OwnerStopRequired is false

### Requirement: cancellation joins semantic work

Owner cancellation SHALL cancel work, join it, interpret the exact decision/error tuple, and then apply settlement.
Context cancellation SHALL NOT overwrite the joined semantic result. InProgress failure SHALL cancel and join work,
preserve decision/cause, record control error, attempt no later terminal method, and require owner stop.

These semantics apply after valid delivery metadata has been observed. Panic, owner cancellation, and heartbeat
control loss SHALL NOT change the existing cancel, join, interpret, and OwnerStopRequired decisions.

#### Scenario: heartbeat fails after joined ACK

- **WHEN** InProgress fails and work joins with ACK
- **THEN** Decision remains ACK and ControlError identifies heartbeat failure
- **AND** no terminal method follows
- **AND** OwnerStopRequired is true

### Requirement: control loss shuts down through the existing exact owner

Each migrated physical binding SHALL create private admission before acquisition and buffer its first
OwnerStopRequired result. Closed admission SHALL perform no work, heartbeat, or terminal method. Already-admitted work
MAY finish. The callback SHALL NOT drain or wait on its handle; the existing owner SHALL stop the exact committed
handle outside callback and join the observer during Stop.

Refusal by closed admission is a declared event, not a silent drop. Each refused delivery SHALL emit a substrate log
line and increment a lane-labelled counter naming the refusal and why continuing is safe (ADR-098). The buffered
deliveries a drained handle flushes reach this path, so the first fatal result alone SHALL NOT be the only signal.

Within this module the owner-side reaction has exactly one home: one shared package that provides the per-lane
admission latch, the drain-once binding around the exact committed handle, and the observer that runs the owner's
reaction and drains that handle on the first buffered fatal. That package SHALL own no lifecycle authority — no
Stop, no restart, no reconstruction, no registry of lanes: the owner constructs the admission before acquisition,
constructs and retains the binding after it, decides Stop, awaits the exact handle's Closed, and joins the observer
through the binding. The owner's health writer SHALL run synchronously inside the latch before the result is
buffered. A closed lane SHALL read nothing from a refused delivery except its subject, and that only to declare the
refusal. No migrated binding SHALL declare its own admission latch.

#### Scenario: control loss precedes handle return

- **WHEN** a callback reports OwnerStopRequired before acquisition returns
- **THEN** the result remains buffered until the exact handle is committed
- **AND** owner-side shutdown occurs outside callback

#### Scenario: closed admission refuses a buffered delivery

- **WHEN** a delivery reaches a binding whose admission is already closed
- **THEN** the binding performs no work, heartbeat, or terminal method
- **AND** it emits a log line and increments its lane-labelled refusal counter

#### Scenario: the owner-side reaction has one home

- **WHEN** a durable-consumer owner in this module reacts to OwnerStopRequired
- **THEN** it consumes each delivery through the shared lane package under an admission it constructed before
  acquisition, and retains the shared binding it constructed after acquisition
- **AND** the observer runs on the owner's Start-derived context, runs the owner's reaction, drains the exact handle
  once, and is joined by the owner's Stop through the binding, which is joinable whether or not an observer ran
- **AND** the shared package holds no registry of bindings, stops nothing on its own authority, and exposes no
  status, metric, or durable state

### Requirement: current crash redelivery declarations are preserved

Tools SHALL retain BackOff 15s/60s and use heartbeat 5s. Dispatch SHALL retain its accepted 10-second heartbeat and
30-second effective acknowledgement interval. Every later staged binding SHALL preserve or deliberately replace its
crash schedule under its owner-reviewed migration contract. BackOff SHALL remain missing-settlement policy, not
semantic retry timing.

#### Scenario: process stops without settlement

- **WHEN** heartbeat and settlement stop
- **THEN** tools redelivery follows its 15s/60s BackOff classes
- **AND** it does not silently fall back to AckWait or semantic retry timing

#### Scenario: a staged binding keeps an explicit crash schedule

- **WHEN** model, loop, or AgentRun migrates on the integration branch
- **THEN** its accepted contract names the resulting AckWait, BackOff, and heartbeat behavior
- **AND** semantic retry timing remains independent of that crash schedule

### Requirement: shared settlement remains stateless and heartbeat-specific

The typed path SHALL use only a private terminal-method executor. The no-heartbeat interpreter SHALL remain private:
the decision interpreter and the terminal-method executor SHALL NOT be exported. #759 SHALL add no exported pull
settlement operation. #1327 SHALL add exactly one exported settlement operation — settlement of one already-joined
outcome, with no work half — reachable through two entry points that differ only in the retry policy they apply:
`SettleDelivery` fixes it at immediate and `SettleDeliveryWithRetry` takes it from the caller. Neither entry point
SHALL invoke work, read payload or metadata, create a context, derive a deadline from consumer policy, send a
heartbeat, or own consumer lifecycle, and neither SHALL modify OTEL production settlement.

#### Scenario: terminal execution is shared privately

- **WHEN** typed heartbeat settlement attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared settlement helper owns admission, a native handle, health, shutdown, or restart; the owner-side
  reaction lives in the one shared lane package, which owns no lifecycle authority

#### Scenario: the exported settlement operation carries no work half

- **WHEN** a caller reaches either exported settlement entry point
- **THEN** that caller has already invoked and joined its own delivery work
- **AND** the entry point attempts at most one terminal method and returns
- **AND** the interpreter it delegates to remains unexported

### Requirement: settlement-only delivery decisions use one shared interpreter

`SettleDelivery` SHALL validate the existing closed decision/error tuple and attempt at most one local terminal
method. Ack with nil cause SHALL call Ack; Retry with non-nil cause SHALL call Nak under the operation's retry
policy, immediate for `SettleDelivery` and delayed by the caller's interval for `SettleDeliveryWithRetry`;
Terminate with non-nil cause SHALL call Term. Quarantine with non-nil cause, invalid tuples, and nil message
SHALL attempt no terminal method and SHALL return quarantined owner-stop evidence. Terminal-method errors SHALL
remain local, unconfirmed evidence.

The function SHALL NOT invoke work, read payload or metadata, create context, derive a deadline from consumer policy,
send heartbeat, or own consumer lifecycle. Its caller SHALL invoke and join delivery work before settlement and SHALL
react to `OwnerStopRequired` through the existing exact owner.

#### Scenario: terminal execution is shared without work ownership

- **WHEN** typed heartbeat or settlement-only handling attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared settlement helper owns admission, a native handle, health, shutdown, or restart; the owner-side
  reaction lives in the one shared lane package, which owns no lifecycle authority

#### Scenario: an invalid tuple or absent message settles nothing

- **WHEN** `SettleDelivery` observes Quarantine, a decision/cause combination outside the closed set, or a nil message
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true

#### Scenario: an unset retry policy settles nothing

- **WHEN** `SettleDeliveryWithRetry` observes a retry policy that is not valid
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true, rather than silently settling as immediate

