# agentic-dispatch Delta

## ADDED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner, and SHALL NOT settle any of them before its durable effect has
committed. Business handlers SHALL receive only an immutable owner-supplied work view and SHALL return a typed
semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, approval response, and
user-response publication has synchronous JetStream PubAck. Terminal events SHALL retain their typed ancestry
and deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

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

#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its deterministic typed user error receives PubAck before termination
- **AND** tracker and gauge state remain unchanged

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
