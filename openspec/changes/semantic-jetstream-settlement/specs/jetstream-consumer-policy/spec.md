# jetstream-consumer-policy Delta

## ADDED Requirements

### Requirement: typed semantic heartbeat settlement is additive during held migration

The framework SHALL expose `ConsumeDeliveryWithHeartbeat` as the permanent typed entry point. Stage A tools and
dispatch SHALL use it. Legacy `ConsumeWithHeartbeat` SHALL remain source- and behavior-compatible only for the exact
held model, loop, and AgentRun allowlist. New production legacy callers SHALL fail repository conformance.

#### Scenario: Stage A compiles with held callers

- **WHEN** tools and dispatch migrate to the typed entry point
- **THEN** binding-local wrappers accept `DeliveryAttempt` and delegate unchanged bytes to transport-agnostic domain
  handlers
- **AND** held model, loop, and AgentRun continue to compile against legacy
- **AND** their configuration and runtime behavior remain unchanged

#### Scenario: final legacy removal

- **WHEN** every held binding has accepted addenda and replay proof and the allowlist is zero
- **THEN** legacy is removed without alias after separate owner approval
- **AND** the permanent typed API remains unchanged

### Requirement: delivery work returns a validated decision/error tuple

Typed work SHALL implement
`DeliveryWork func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error)`. The exported decisions
SHALL remain Invalid, ACK, Retry, Terminate, and Quarantine using the exact `DeliveryDecision*` constants.

The framework SHALL validate non-nil work before acquisition. For each admitted delivery it SHALL supply that
delivery's immutable settlement-authority-free `DeliveryAttempt` and body as read-only invocation-scoped bytes. It
SHALL NOT expose `jetstream.Msg`, headers, reply subjects, sequences, consumer identity, or another
settlement-capable interface to work.

ACK SHALL require nil error. Retry, Terminate, and Quarantine SHALL require non-nil error. Invalid, unknown, and every
mismatched tuple SHALL preserve the requested decision, attempt no terminal method, expose an
`InvalidDeliveryDecisionError`, quarantine, and require owner stop. A supplied error SHALL remain reachable through
the typed cause. Recovered panic SHALL synthesize Quarantine with `DeliveryWorkPanicError`.

Existing `TerminateDelivery(error) error` and `PermanentDeliveryError` SHALL retain their exact behavior and SHALL
not be deprecated or removed by this change.

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
- **THEN** it receives context, immutable `DeliveryAttempt`, and read-only payload bytes only
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

### Requirement: delivery attempt is observed before work

For each valid typed delivery, natsclient SHALL call `msg.Metadata()` exactly once before Data or work. It SHALL
derive `DeliveryAttempt.Number` from positive `NumDelivered`; `MetadataAvailable` SHALL be true and `IsRedelivery`
SHALL be true only when Number is greater than one. The zero value SHALL report Number zero, metadata unavailable,
and not redelivered. Typed work SHALL never receive that zero value.

Metadata error, nil metadata, or zero delivery number SHALL produce Quarantine with typed
`DeliveryMetadataUnavailableError`, require owner stop, and call neither Data, work, heartbeat, nor a terminal
settlement method. An underlying metadata error SHALL remain reachable through the typed cause.

`DeliveryAttempt` SHALL expose no native message, header, reply, stream or consumer sequence, stream or consumer
identity, settlement method, setter, or mutable state. Redelivery SHALL be an observation and SHALL NOT prove that
prior work started or committed.

#### Scenario: first delivery

- **WHEN** metadata reports `NumDelivered == 1`
- **THEN** work receives Number 1 with MetadataAvailable true
- **AND** IsRedelivery is false

#### Scenario: second delivery

- **WHEN** metadata reports `NumDelivered == 2`
- **THEN** work receives Number 2 with MetadataAvailable true
- **AND** IsRedelivery is true

#### Scenario: prior process stopped before work

- **WHEN** a first delivery was lost before work invocation and JetStream delivers it again
- **THEN** the next work invocation observes redelivery
- **AND** the framework does not claim the prior invocation or effect occurred

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
attempt/success/failure, server confirmation, quarantine, owner-stop requirement, and aggregate error. Plain terminal
methods SHALL never report server confirmation. A method error SHALL mean unknown/not-confirmed and SHALL NOT prove
redelivery.

#### Scenario: clean local Ack

- **WHEN** ACK is selected and Ack returns nil
- **THEN** local method success is true and `Err` is nil
- **AND** server confirmation is false

#### Scenario: terminal method errors

- **WHEN** Ack, Nak, delayed Nak, or Term returns an error without control loss or quarantine
- **THEN** the method failure remains observable and server confirmation is false
- **AND** OwnerStopRequired is false

### Requirement: cancellation joins semantic work

Owner cancellation SHALL cancel work, join it, interpret the exact decision/error tuple, and then apply settlement.
Context cancellation SHALL NOT overwrite the joined semantic result. InProgress failure SHALL cancel and join work,
preserve decision/cause, record control error, attempt no later terminal method, and require owner stop.

These semantics apply after a valid `DeliveryAttempt` has been observed. Panic, owner cancellation, and heartbeat
control loss SHALL preserve that observation without changing the existing cancel, join, interpret, and
OwnerStopRequired decisions.

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

#### Scenario: control loss precedes handle return

- **WHEN** a callback reports OwnerStopRequired before acquisition returns
- **THEN** the result remains buffered until the exact handle is committed
- **AND** owner-side shutdown occurs outside callback

### Requirement: current crash redelivery declarations are preserved

Tools SHALL retain BackOff 15s/60s and use heartbeat 5s. Each loop binding SHALL retain BackOff 30s/120s and use
heartbeat 15s only when its hold lifts. BackOff SHALL remain missing-settlement policy, not semantic retry timing.

#### Scenario: process stops without settlement

- **WHEN** heartbeat and settlement stop
- **THEN** tools redelivery follows its 15s/60s BackOff classes
- **AND** an admitted loop follows its 30s/120s classes rather than AckWait

### Requirement: unsafe direct bindings remain non-authorizing

Model, each loop binding, and each AgentRun binding SHALL require a then-current line-addressable addendum, independent
inventory/design reviews, and named owner acceptance before migration. Their current done rows SHALL NOT authorize
durable state, rehydration, handler ledgers, or decision mapping.

#### Scenario: an implementer reaches a held binding

- **WHEN** no named accepted addendum exists for that physical binding
- **THEN** its production legacy call and behavior remain unchanged
- **AND** implementation does not infer a settlement policy

### Requirement: shared settlement remains stateless and heartbeat-specific

The typed and legacy paths MAY share only a private terminal-method executor. The no-heartbeat interpreter SHALL remain
private. #759 SHALL add no exported pull settlement operation and SHALL not modify OTEL production settlement.

#### Scenario: terminal execution is shared privately

- **WHEN** typed and legacy heartbeat paths attempt a terminal JetStream method
- **THEN** they may call the same private terminal-method executor
- **AND** no shared helper owns admission, a native handle, health, shutdown, or restart

### Requirement: the zero-adopter builder retires after Stage A proof

`NewDurableHandler` SHALL be removed without alias only after the permanent typed API, direct validation/integration
tests, Stage A migration, owner review, and #1155 Stage A proof exist. Held callers SHALL NOT depend on the builder.

#### Scenario: Stage A replacement is proven

- **WHEN** the permanent API and Stage A tools/dispatch paths pass the required owner and replacement gates
- **THEN** the zero-adopter builder is removed without alias
- **AND** held callers continue through the separately contained legacy helper
