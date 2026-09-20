# agentic-dispatch Delta

## ADDED Requirements

### Requirement: Dispatch task redelivery recovers the committed LoopID

Dispatch SHALL derive stable TaskID from validated `UserMessage` identity. For new work it SHALL mint a
random framework LoopID and retain that LoopID in the committed `TaskMessage`. On source redelivery it SHALL
exact-read the retained task by TaskID, validate the TaskID/source mapping, and recover the retained LoopID. One
TaskID naming two LoopIDs SHALL quarantine.

Cancel, approval-response, refusal, terminal user-response, and other ordinary publications SHALL be at-least-once.
Source ACK SHALL wait for every required PubAck. A stable `Nats-Msg-Id` MAY suppress duplicates inside the configured
server window, but SHALL NOT be treated as exact commitment proof or a guarantee beyond that window. Dispatch SHALL
NOT add exact committed-output lookup for ordinary publications.

#### Scenario: User delivery repeats after task commit

- **WHEN** a `UserMessage` redelivers after its task committed
- **THEN** dispatch reads the retained task by stable TaskID
- **AND** reuses its random minted LoopID rather than deriving or minting another

#### Scenario: Task mapping conflicts

- **WHEN** retained evidence maps one stable TaskID to a different LoopID or source
- **THEN** dispatch quarantines
- **AND** does not select or overwrite either mapping

#### Scenario: Ordinary publication has uncertain PubAck

- **WHEN** a cancel, approval response, refusal, or user response does not receive PubAck
- **THEN** the source remains unsettled and publication may repeat
- **AND** duplicate-window suppression is not treated as durable reconciliation


## MODIFIED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner, and SHALL NOT settle any of them before its durable effect has
committed. Business handlers SHALL receive only an immutable owner-supplied work view and SHALL return a typed
semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, cancel signal, approval response,
and user-response publication has synchronous JetStream PubAck. The cancel signal SHALL travel its stream with
PubAck rather than as a core publication, so the published fact a classification reads names a durable effect
rather than a hope. Whether an unacknowledged publication retries or quarantines SHALL be decided by whether its
redelivery is effect-free, and that decision SHALL be recorded at the call site rather than taken by default. A
command SHALL NOT be retried when its target was resolved rather than named by the message and this component
either published a signal during that delivery or attempted one whose outcome it cannot account for. A failed
publish SHALL count as an unaccounted attempt unless the error PROVES nothing was stored — a refusal the client
returns before the bytes leave the process — and that set SHALL fail closed, so an error it does not recognize is
unaccounted rather than refused. Both the attempt and the published fact SHALL be recorded where this component's
own publication happens, never inferred from the command name or the response text, because a command that
published nothing is replayable no matter how its target was chosen. The recorder is internal to this component,
so a command handler an adopter registers cannot record a publication of its own: its failed response after a
durable publication retries exactly as it did before this change, and exporting the recorder is an addition a
later change owns. Terminal events SHALL retain their typed
ancestry and deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

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
- **AND** no second task is published; a redelivery recovers the committed task identity rather than minting
  another, so what keeps this arm from retrying is the loop tracking and started counting it would re-enter, not
  the identity

#### Scenario: A named cancel command's signal is published but its response is not

- **WHEN** `/cancel <loop_id>` publishes its signal and the required user response does not receive PubAck
- **THEN** the delivery retries rather than quarantining, and the classification is recorded at the call site so
  the post-effect response failures in this component are told apart deliberately
- **AND** Retry is conditioned on the redelivery being effect-free: the loop gate reports the settled loop
  terminal and answers without publishing a second signal, and a signal that races the loop's own settlement is
  dropped effect-free by the loop's cancel owner
- **AND** the signal reached its stream with PubAck before the response was attempted, so the published fact the
  classification reads is durable

#### Scenario: A cancel command whose target was resolved rather than named

- **WHEN** a bare `/cancel` resolves its target from the tracker, publishes that loop's signal, and the required
  user response does not receive PubAck
- **THEN** the delivery quarantines, because the message does not carry the identity the delivery acted on
- **AND** the redelivery is not effect-free: this delivery's own effect makes the resolution fall through the now
  terminal loop to the user's next live loop, which would be cancelled without ever having been named

#### Scenario: A resolved cancel's signal publish fails without proving refusal

- **WHEN** a bare `/cancel` resolves its target from the tracker and its signal publish fails with an error that
  does not prove the broker stored nothing
- **THEN** the delivery quarantines rather than retrying, because the failure describes what the client
  experienced and not whether the signal was stored
- **AND** the hazard is the published one reached through an error: were it retried and the first attempt had
  stored, this delivery's own effect would make the redelivery resolve past the now cancelling loop onto the
  user's next live loop and cancel it unnamed
- **AND** the same failure on a target the message named retries, as does a failure the client proves is a refusal
  it made before publishing, because neither can have changed the world

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
- **AND** the response identity is minted per publication on this lane, and #1328 leaves it that way: which
  refusal a message earns is decided by which check failed, so two deliveries of one source message can carry
  different refusals, and a source-derived identity would give those one name and let a duplicate window suppress
  the second; the deterministic source-derived identity stays with the terminal lane, where one source has exactly
  one answer

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
