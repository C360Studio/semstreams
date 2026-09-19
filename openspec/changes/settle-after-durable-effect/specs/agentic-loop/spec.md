# agentic-loop Delta

## ADDED Requirements

### Requirement: Loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners, and SHALL NOT positively acknowledge a delivery on a converted
class before that lane's durable effect has committed. Task intake is classified through the same owner but is not
converted here; the requirement below names it. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or work-owning no-heartbeat
adapter.

Each non-heartbeat physical subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; agentic-loop
SHALL NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout.

Decode, correlation, KV, Store, transition, and required publication failures on a converted class SHALL NOT become
successful callback completion. ACK means the lane-specific durable transition or defined refusal and every required
PubAck completed; Retry means stable identity and reconciliation make re-execution safe; Terminate means permanently
invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure
prevents a safe choice.

A failure that arrives after a handler has already moved its loop in memory SHALL be treated as a partial effect and
quarantined, never retried: the redelivery does not reach the loop the first attempt left, so the handler answers it
from the loop's new terminal state and the result the first attempt built cannot be rebuilt. A loop's terminal
business failure SHALL be positively acknowledged only once its failed loop state, its terminal record and its
failure events have committed.

#### Scenario: Required output publication fails

- **WHEN** a handler computes a transition
- **AND** a required publication does not receive PubAck
- **THEN** the source is not positively acknowledged
- **AND** its disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the deterministic terminal event receives PubAck before source ACK

#### Scenario: A malformed non-heartbeat input is terminated, never acknowledged as done

- **WHEN** a production non-heartbeat callback receives an input it cannot decode or correlate
- **THEN** it returns Terminate with a non-nil cause
- **AND** no warning-only return becomes ACK

#### Scenario: A malformed heartbeat-lane input is terminated, never acknowledged as done

- **WHEN** a production heartbeat-lane callback receives bytes that do not decode, or that decode to a payload type
  the lane does not handle
- **THEN** the failure is classified as a permanent delivery error and the binding terminates the delivery
- **AND** the delivery is neither acknowledged nor retried

#### Scenario: A handler result fails before any of its publications

- **WHEN** a handler has moved its loop in memory and the loop-state, terminal-record or graph write then fails
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the delivery is not retried into a handler whose terminal guard would answer it with an empty result

#### Scenario: A terminal business failure cannot be recorded

- **WHEN** a handler error fails its loop and the failed loop state, terminal record or failure event does not commit
- **THEN** the source is not positively acknowledged
- **AND** a loop that could not be transitioned at all is retried rather than quarantined, because no effect was
  written and the redelivery is settled from the loop record

#### Scenario: A handler result fails after some of its publications have returned PubAck

- **WHEN** a handler result's state has been stamped and its publication phase then fails partway
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the owner latches its health fatal and drains that lane, rather than redelivering a callback that would
  republish results whose PubAcks already returned

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** handler recovery returns a non-nil fatal-classified error
- **AND** the production delivery callback returns Quarantine without persistence or settlement
- **AND** the exact owner stops and drains
- **AND** the panic is never rewritten to nil

#### Scenario: Loop delivery metadata is unavailable

- **WHEN** a loop settlement adapter cannot observe native delivery metadata
- **THEN** it invokes no loop work and makes no heartbeat or settlement call
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** drains the exact consume handle
- **AND** loop health becomes negative with the exact cause and one error-count increment

#### Scenario: The first fatal result latches health before the handle drains

- **WHEN** any loop delivery owner produces its first result requiring owner stop
- **THEN** health synchronously reports `Healthy=false`, status `delivery ownership lost`, the exact cause in
  `LastError`, and exactly one increment of the existing error count, before owner-stop observation drains the
  exact handle
- **AND** a later fatal result in the same or another lane neither overwrites nor recounts that first cause
- **AND** the latch itself adds no metric family, public state, durable state, or communication path

### Requirement: Task intake is the one loop input class this layer does not convert

Task intake SHALL be named as an exemption rather than left to the absence of a scenario, because the rule above
reads as covering it. An undecodable task envelope, a task payload of the wrong type, a `HandleTask` failure, and a
failed first publication or loop-state write SHALL keep their pre-existing log-and-acknowledge settlement. Converting
them is not a classification change: a redelivered task deduplicates against the loop its first delivery already
created and acknowledges without publishing, so the lane needs resumable intake before a Retry can mean anything.
The exemption is tracked as issue #1345 and is not a permanent property of the lane.
The birth-failure and transient-lineage paths of the same lane are NOT exempt — they settle on their durable effect
today.

#### Scenario: A task delivery fails after its loop exists

- **WHEN** task intake fails to decode its envelope, fails in its handler, or cannot publish its first request or
  write its loop state
- **THEN** the failure is logged and the delivery is positively acknowledged
- **AND** the exemption is recorded here and tracked as issue #1345, with resumable intake named as its
  precondition

#### Scenario: A loop-execution birth failure is not exempt

- **WHEN** a task's graph birth or lineage write fails
- **THEN** the loop's terminal business failure is established before the delivery is acknowledged
- **AND** a failure that could not be recorded quarantines instead

### Requirement: A loop absent from process memory is settled from its record

A callback that receives an input naming a loop it does not hold in memory SHALL NOT infer from that absence that
the loop is finished. It SHALL classify the loop from the loop record: absent or terminal is stale, non-terminal is
live, and any failed or undecodable read is unknown. A stale loop SHALL be acknowledged and counted as an expected
drop; a live or unknown loop SHALL be retried, because a positive acknowledgement would discard work this process
lost rather than work that completed. The classification SHALL perform no recovery: it reconstructs no state,
re-registers no routing, and reads no retained request. A loop identifier recovered from a structured identifier
SHALL be a framework-minted token, so a provider-authored identifier is never used as a record key.

#### Scenario: A response or tool result arrives for a loop this process does not hold

- **WHEN** a model response or tool result names a loop absent from process memory
- **AND** the loop record shows the loop absent or terminal
- **THEN** the delivery is acknowledged and counted as an expected drop naming a stale identifier

#### Scenario: The named loop is still live

- **WHEN** a model response, tool result, or governance verdict names a loop whose record is non-terminal
- **THEN** the delivery is retried rather than acknowledged
- **AND** no expected-drop count is recorded for it

#### Scenario: The loop record cannot be read

- **WHEN** the loop record read fails, or its value does not decode
- **THEN** the delivery is retried
- **AND** the failure is never reported as a stale loop

#### Scenario: A cancel signal names a loop that cannot be cancelled

- **WHEN** a cancel signal names a loop that is already terminal
- **THEN** the signal is acknowledged effect-free and counted as an already-terminal drop
- **WHEN** a cancel signal names a loop this process does not hold
- **THEN** the signal is settled by the same record classification as any other input

### Requirement: Delivery work joins before settlement

Every goroutine spawned by delivery work SHALL join before its callback returns. A deadline cancels the operation but
SHALL NOT authorize return while work remains live.

#### Scenario: Delivery work exceeds its budget

- **WHEN** bounded work reaches its deadline
- **THEN** the owner cancels and joins before callback return

#### Scenario: Terminal approval rejection reaches a bounded graph write

- **WHEN** an approval rejection produces a terminal result and its bounded graph write reaches cancellation
- **THEN** graph-write work observes the delivery-derived context and joins before the callback returns
- **AND** the approval source is not settled while that work remains live

### Requirement: Long-running loop heartbeat policy is valid before acquisition

Task, response, and tool-result consumers SHALL default to heartbeat 15s against BackOff `[30s,2m]`. They SHALL
validate the exact acquisition config before consumer allocation; heartbeat SHALL be no greater than half the
shortest positive BackOff. MaxDeliver SHALL be at least the number of BackOff entries, so the fixed two-entry BackOff
requires MaxDeliver at least 2. Omitted or zero MaxDeliver SHALL default to 2. An explicit value below 2 SHALL be
refused before consumer allocation; the owner SHALL NOT truncate BackOff or admit a single-delivery posture.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s,2m]`
- **THEN** it returns a typed error naming the values and 15s ceiling
- **AND** allocates no consumer

#### Scenario: Single delivery is refused before allocation

- **WHEN** setup observes MaxDeliver 1 with BackOff `[30s,2m]`
- **THEN** it returns a typed policy error naming observed 1 and required minimum 2
- **AND** allocates no consumer

#### Scenario: Minimum valid delivery count reaches acquisition

- **WHEN** setup observes MaxDeliver 2, heartbeat 15s, and BackOff `[30s,2m]`
- **THEN** heartbeat and delivery-count validation pass
- **AND** setup may allocate the consumer with the unchanged two-entry BackOff

#### Scenario: Shipped fixtures resolve a valid policy

- **WHEN** every shipped loop configuration fixture resolves its consumer config
- **THEN** each one satisfies the heartbeat ceiling and the delivery floor

#### Scenario: The non-heartbeat lanes are held to the same retry floor

- **WHEN** the cancel-signal, approval-response, approved-verdict, or rejected-verdict lane is set up
- **THEN** it acquires a consumer carrying a non-empty BackOff and a MaxDeliver covering it
- **AND** the same validation refuses a configuration below that floor before allocating a consumer

#### Scenario: Retry on a non-heartbeat lane is delayed, not immediate

- **WHEN** a non-heartbeat callback returns Retry
- **THEN** the binding negatively acknowledges with a delay rather than at line rate
