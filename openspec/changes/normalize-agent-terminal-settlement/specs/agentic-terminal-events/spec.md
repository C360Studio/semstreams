## ADDED Requirements

### Requirement: Terminal decoding SHALL fail closed

The framework SHALL decode terminal events through a registry-bound `message.Decoder` and SHALL permanently reject
an event unless its `BaseMessage.ID` is nonempty, its message type and metadata are valid, its concrete payload
validates, its loop and task IDs are nonempty, its applicable terminal timestamp is nonzero, and its category/outcome
pair is accepted.

#### Scenario: valid completion

- **GIVEN** a production `BaseMessage` containing a valid `LoopCompletedEvent`
- **AND** its outcome is `success`
- **AND** `CompletedAt` is nonzero
- **WHEN** a framework terminal consumer decodes it
- **THEN** it receives a succeeded normalized terminal projection

#### Scenario: missing source identity

- **GIVEN** an otherwise valid terminal `BaseMessage` with an empty source message ID
- **WHEN** it is normalized
- **THEN** the delivery is permanently rejected

#### Scenario: invalid payload identity

- **GIVEN** a terminal payload with an empty loop ID or task ID
- **WHEN** it is normalized
- **THEN** concrete payload validation fails
- **AND** the delivery is permanently rejected

#### Scenario: missing terminal timestamp

- **GIVEN** a completed, failed, or cancelled payload whose applicable terminal timestamp is zero
- **WHEN** it is normalized
- **THEN** the delivery is permanently rejected

#### Scenario: flat envelope

- **GIVEN** a flat envelope without the production `type` discriminator
- **WHEN** it is normalized
- **THEN** the delivery is permanently rejected
- **AND** it is not silently acknowledged

### Requirement: Terminal category and outcome agreement SHALL be closed

The framework SHALL recognize exactly `loop_completed + success`, `loop_failed + failed`, and
`loop_cancelled + cancelled`. Every other category/outcome pairing SHALL be permanently rejected. Subject name SHALL
NOT be semantic category authority.

#### Scenario: cancellation on completion subject

- **GIVEN** a valid `LoopCancelledEvent` with outcome `cancelled`
- **AND** it is delivered on `agent.complete.<loopID>`
- **WHEN** a terminal consumer processes it
- **THEN** it is projected as cancellation

#### Scenario: invalid category and outcome collision

- **GIVEN** a terminal payload whose category and outcome are not one accepted pair
- **WHEN** it is normalized
- **THEN** the delivery is permanently rejected

### Requirement: Truncation producer behavior SHALL remain unchanged

This change SHALL NOT introduce `loop_failed + truncated` as a terminal wire pair. A persisted
`LoopEntity.Outcome="truncated"` and an emitted `LoopFailedEvent.Outcome="failed"` SHALL remain distinct existing
producer behavior.

#### Scenario: truncation failure reaches terminal wire

- **GIVEN** a truncation failure whose persisted loop outcome is `truncated`
- **WHEN** its terminal failure event is emitted
- **THEN** the event outcome remains `failed`
- **AND** the normalizer treats it as the existing `loop_failed + failed` pair

### Requirement: Dispatch projection SHALL match normalized terminal class

Dispatch SHALL project succeeded terminals as result responses, failed terminals as error responses, and cancelled
terminals as status responses. Tracker mutation SHALL be idempotent and SHALL use the validated terminal timestamp.

#### Scenario: successful loop result

- **GIVEN** a valid succeeded terminal carrying result content
- **WHEN** dispatch projects it
- **THEN** dispatch emits a result response carrying that result

#### Scenario: cancelled loop

- **GIVEN** a valid cancelled terminal
- **WHEN** dispatch projects it
- **THEN** dispatch emits a deterministic cancellation status response

### Requirement: Response routing SHALL reconcile fields independently

Dispatch SHALL reconcile `ChannelType`, `ChannelID`, and `UserID` independently from the process-local tracker,
terminal payload, and persisted `AGENT_LOOPS/<loopID>` `LoopEntity`. Empty fields SHALL contribute no value. Matching
nonempty fields SHALL agree. Conflicting nonempty fields SHALL be permanently rejected.

A publishable route SHALL require nonempty `ChannelType` and `ChannelID`. `UserID` SHALL be optional metadata and an
empty `UserID` SHALL NOT invalidate a complete channel pair. Dispatch SHALL observe persisted state before classifying
a route as partial or route-less.

#### Scenario: complete route with empty UserID

- **GIVEN** reconciled `ChannelType` and `ChannelID` are nonempty
- **AND** reconciled `UserID` is empty
- **WHEN** dispatch settles the retained terminal
- **THEN** it publishes a `UserResponse` to the resolved channel

#### Scenario: fields compose independently

- **GIVEN** `ChannelType` is present only in one compatible source
- **AND** `ChannelID` is present only in another compatible source
- **AND** no nonempty routing fields conflict
- **WHEN** dispatch reconciles the terminal route
- **THEN** it combines the fields into one publishable channel pair

#### Scenario: persisted route after restart

- **GIVEN** no process-local loop information
- **AND** the terminal payload lacks one or more channel fields
- **AND** the persisted loop entity supplies a complete compatible channel pair
- **WHEN** a retained terminal is redelivered
- **THEN** dispatch publishes to that channel

#### Scenario: conflicting optional UserID

- **GIVEN** two sources contain different nonempty `UserID` values
- **WHEN** dispatch reconciles the route
- **THEN** it permanently rejects the terminal as an identity/routing collision

#### Scenario: empty UserID does not conflict

- **GIVEN** one source contains a nonempty `UserID`
- **AND** another source contains an empty `UserID`
- **AND** the channel pair is complete and compatible
- **WHEN** dispatch reconciles the route
- **THEN** it retains the nonempty `UserID` as response metadata

#### Scenario: partial channel pair

- **GIVEN** persisted-state observation is complete
- **AND** exactly one of `ChannelType` or `ChannelID` is nonempty after reconciliation
- **WHEN** dispatch classifies the route
- **THEN** it permanently rejects the terminal as a malformed partial route

#### Scenario: intentionally route-less loop

- **GIVEN** persisted-state observation is complete
- **AND** both `ChannelType` and `ChannelID` are empty after reconciliation
- **WHEN** dispatch settles the terminal
- **THEN** it publishes no `UserResponse`
- **AND** it may acknowledge the terminal after all other required work succeeds

#### Scenario: transient persisted-state lookup failure

- **GIVEN** dispatch cannot yet observe `AGENT_LOOPS/<loopID>` because of a transient lookup failure
- **WHEN** the retained terminal is processed
- **THEN** dispatch delayed-NAKs the terminal
- **AND** does not classify it as route-less or malformed

### Requirement: Terminal-derived response identity SHALL be stable

Dispatch SHALL derive both `UserResponse.ResponseID` and JetStream `Nats-Msg-Id` as
`terminal-user-response:<source BaseMessage ID>`. It SHALL derive the response timestamp from the validated terminal
timestamp and SHALL NOT generate a fresh identity on redelivery.

#### Scenario: same terminal is redelivered

- **WHEN** dispatch processes the same retained terminal again
- **THEN** it uses the same `ResponseID`, `Nats-Msg-Id`, timestamp, and resolved route

### Requirement: Terminal response settlement SHALL require PubAck

Dispatch SHALL acknowledge a retained terminal requiring a `UserResponse` only after synchronous JetStream PubAck.
Permanent validation, identity, category/outcome, and routing failures SHALL Term. Transient persisted-state or
response-publication failures SHALL delayed-NAK. Shutdown SHALL short delayed-NAK.

#### Scenario: transient publication failure

- **WHEN** required response publication fails transiently
- **THEN** dispatch delayed-NAKs the terminal
- **AND** does not ACK it

#### Scenario: successful publication

- **WHEN** required response publication receives synchronous PubAck
- **THEN** dispatch may ACK the terminal

#### Scenario: permanent invalid terminal

- **WHEN** terminal validation fails permanently
- **THEN** dispatch Terms the delivery
- **AND** records fixed-reason failure telemetry

### Requirement: Delivery attempts SHALL be unlimited only within AGENT retention

Dispatch terminal consumers SHALL use `MaxDeliver=0` and SHALL NOT expose a retry-count configuration knob.
`MaxDeliver=0` SHALL mean unlimited delivery attempts while the terminal remains retained, not indefinite storage.

The checked AGENT posture SHALL be documented as 24h MaxAge, 256MiB MaxBytes, and DiscardOld. The framework SHALL NOT
claim a response-publication guarantee after source eviction and SHALL NOT add an outbox or second durable response
authority in this change.

#### Scenario: transient failure while source remains retained

- **GIVEN** a terminal remains retained in AGENT
- **WHEN** routing lookup or response publication fails transiently more than three times
- **THEN** `MaxDeliver=0` continues redelivery

#### Scenario: source is evicted by age

- **GIVEN** an unsettled terminal
- **WHEN** AGENT MaxAge evicts it
- **THEN** no further response-publication attempt is guaranteed

#### Scenario: source is evicted by capacity

- **GIVEN** an unsettled terminal
- **WHEN** DiscardOld capacity pressure evicts it
- **THEN** `MaxDeliver=0` does not preserve the evicted terminal
- **AND** the framework does not claim successful response delivery

### Requirement: Framework terminal consumers SHALL share one interpretation

Dispatch, AgentRun, and OTel SHALL consume the repo-internal normalized terminal projection. AgentRun SHALL retain its
existing public callback type and SHALL invoke it for valid success, failure, and cancellation production envelopes.
No new exported normalized terminal type SHALL be introduced.

#### Scenario: AgentRun receives production terminal envelopes

- **GIVEN** valid production success, failure, and cancellation `BaseMessage` envelopes
- **WHEN** AgentRun consumes them
- **THEN** its existing milestone callback receives all three normalized terminal classes

### Requirement: Terminal settlement telemetry SHALL use bounded reasons

The framework SHALL emit exactly one final fixed-reason disposition per terminal attempt. Reasons SHALL distinguish
envelope/type rejection, payload-validation rejection, zero terminal timestamp, identity/category/outcome collision,
routing collision or malformed state, tracker-projection collision, transient routing read, transient response
publication, successful response settlement, and route-less settlement. Loop IDs, user IDs, channel IDs, and
subjects SHALL NOT be metric labels.

#### Scenario: unsettled terminal is permanently malformed

- **WHEN** dispatch permanently rejects a terminal
- **THEN** it records exactly one fixed reason
- **AND** no unbounded identity appears in metric labels

### Requirement: Delivery declaration SHALL remain bounded and honest

The framework SHALL describe terminal-derived `UserResponse` publication as at-least-once within bounded AGENT
retention and USER duplicate-detection mechanisms. It SHALL NOT claim exactly-once, indefinite retry, per-message
eviction proof, or post-eviction response delivery.

#### Scenario: operator inspects the contract

- **WHEN** an operator evaluates recovery guarantees
- **THEN** the documented AGENT age/capacity horizon and visibility gap are explicit
- **AND** the finite-MaxDeliver advisory is not described as an eviction signal for these consumers
