# agentic-dispatch Delta

## ADDED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner, and SHALL NOT settle any of them before its durable effect has
committed. Business handlers SHALL receive only an immutable owner-supplied work view and SHALL return a typed
semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, approval response, and
user-response publication has synchronous JetStream PubAck. Whether an unacknowledged publication retries or
quarantines SHALL be decided by whether its redelivery is effect-free, and that decision SHALL be recorded at the
call site rather than taken by default. A command SHALL NOT be retried when both of two facts hold: the delivery
published a signal, and its target was resolved rather than named by the message. The published fact SHALL be
recorded where the publication happens, never inferred from the command name or the response text, because a
command that published nothing is replayable no matter how its target was chosen. Terminal events SHALL retain
their typed ancestry and deterministic response contract. No void, log-only, or core-NATS publication failure
SHALL become ACK.

The `user.message`, `agent.created`, and `agent.approval_pending` subscriptions SHALL invoke their typed business
handlers using the callback installed by each production setup branch. All delivery-derived work SHALL join before
the private callback passes its decision and cause to `natsclient.SettleDelivery`. JetStream consumer configuration
owns AckWait and redelivery; dispatch SHALL NOT derive a universal work deadline from AckWait. An operation MAY use
an ordinary business timeout.

The first owner-fatal result in an owner family SHALL synchronously latch before the exact handle is drained, and
later fatal results in that family SHALL neither overwrite nor recount it. Existing Health SHALL report
`Healthy=false`. The three lanes this change brings under settlement share one latch whose status is
`delivery ownership lost`; the two terminal lanes keep the separate latches they already had, so their loss alone
keeps the narrower `terminal delivery ownership lost` status — it is the whole truth only while no other lane has
lost ownership. `LastError` SHALL carry every latched cause and the error count SHALL be the number of owner
families that lost ownership, so per-family aggregation is preserved rather than replaced. This adds no metric
family, public state, durable state, or communication path.

#### Scenario: Task publication succeeds but user response fails

- **WHEN** the TaskMessage receives PubAck
- **AND** the required user response does not receive PubAck
- **THEN** the delivery quarantines rather than retrying the UserMessage
- **AND** no second task is published, because a redelivery would mint a new task identity that nothing downstream
  could deduplicate

#### Scenario: A named cancel command's signal is published but its response is not

- **WHEN** `/cancel <loop_id>` publishes its signal and the required user response does not receive PubAck
- **THEN** the delivery retries rather than quarantining, and the classification is recorded at the call site so
  the post-effect response failures in this component are told apart deliberately
- **AND** Retry is conditioned on the redelivery being effect-free: the loop gate reports the settled loop
  terminal and answers without publishing a second signal, and a signal that races the loop's own settlement is
  dropped effect-free by the loop's cancel owner

#### Scenario: A cancel command whose target was resolved rather than named

- **WHEN** a bare `/cancel` resolves its target from the tracker, publishes that loop's signal, and the required
  user response does not receive PubAck
- **THEN** the delivery quarantines, because the message does not carry the identity the delivery acted on
- **AND** the redelivery is not effect-free: this delivery's own effect makes the resolution fall through the now
  terminal loop to the user's next live loop, which would be cancelled without ever having been named

#### Scenario: A command that resolved a target and published nothing

- **WHEN** a command whose target was resolved from the tracker publishes no signal — a read-only command, or a
  cancel that was refused, found no loop, or found one already settled — and its response does not receive PubAck
- **THEN** the delivery retries, because a command that did nothing can be replayed whatever its target was
- **AND** the lane is not latched, so later user messages are still admitted

#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its typed user error receives PubAck before the delivery is acknowledged, and a publication that fails
  is classified rather than swallowed
- **AND** tracker and gauge state remain unchanged
- **AND** the response identity is minted per publication on this lane; the deterministic source-derived identity
  belongs to the terminal lane, and extending it to the rest is L2's (#1328)

#### Scenario: Terminal publication is uncertain

- **WHEN** a deterministic user response does not receive PubAck
- **THEN** its terminal source is not positively acknowledged
- **AND** replay uses the same source-derived response identity

#### Scenario: A malformed non-heartbeat input is terminated, never acknowledged as done

- **WHEN** a production dispatch callback receives an input it cannot decode or correlate
- **THEN** it returns Terminate with a non-nil cause
- **AND** no log-only return becomes ACK

#### Scenario: Dispatch business work reaches its own deadline

- **WHEN** a delivery-owned dispatch operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns
