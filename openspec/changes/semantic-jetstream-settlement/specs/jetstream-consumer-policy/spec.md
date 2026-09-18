# jetstream-consumer-policy Delta

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

## ADDED Requirements

### Requirement: semantic heartbeat settlement has one permanent exported surface

The framework SHALL expose `ConsumeDeliveryWithHeartbeat` with validated `HeartbeatDeliveryPolicy`,
`DeliveryDecision`, and `DeliveryResult`.

`NewDurableHandler` SHALL NOT exist or have an alias. `ConsumeWithHeartbeat` SHALL have no alias and no new
production caller, and SHALL be deleted by the PR that migrates its last one (#1249). Every original model, tools,
dispatch, loop, and AgentRun heartbeat binding SHALL use the permanent typed surface with its owner-specific durable
definition of done.

No capability SHALL describe a production legacy allowlist. The exact caller list is ratchet conformance only: it
never widens, and it reaches zero in the PR that removes the helper.

#### Scenario: public surface at this layer

- **WHEN** this change is archived
- **THEN** the permanent typed API exists
- **AND** `NewDurableHandler` and every alias are absent with zero production callers
- **AND** `ConsumeWithHeartbeat` carries only its ratcheted remaining callers and no alias

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

#### Scenario: control loss precedes handle return

- **WHEN** a callback reports OwnerStopRequired before acquisition returns
- **THEN** the result remains buffered until the exact handle is committed
- **AND** owner-side shutdown occurs outside callback

#### Scenario: closed admission refuses a buffered delivery

- **WHEN** a delivery reaches a binding whose admission is already closed
- **THEN** the binding performs no work, heartbeat, or terminal method
- **AND** it emits a log line and increments its lane-labelled refusal counter

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

The typed path SHALL use only a private terminal-method executor. The no-heartbeat interpreter SHALL remain private.
#759 SHALL add no exported pull settlement operation and SHALL not modify OTEL production settlement.

#### Scenario: terminal execution is shared privately

- **WHEN** typed heartbeat settlement attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared helper owns admission, a native handle, health, shutdown, or restart

## REMOVED Requirements

### Requirement: Durable settlement composition is stateless

**Reason**: `NewDurableHandler` was a zero-adopter transitional wrapper around the legacy nil/error settlement
contract. The permanent typed policy and delivery APIs now own validation and per-delivery settlement, and the
accepted Stage A removal gate has been satisfied.

**Migration**: No measured `NewDurableHandler` adopter requires source migration. Durable owners validate
`HeartbeatDeliveryPolicy` from the exact acquisition configuration, invoke `ConsumeDeliveryWithHeartbeat` inside the
canonical owner-held consumer callback, inspect `DeliveryResult`, and stop the exact owner outside the callback when
`OwnerStopRequired` is true. `docs/operations/migration-restart-safe-nats-client.md` carries the complete composition.
