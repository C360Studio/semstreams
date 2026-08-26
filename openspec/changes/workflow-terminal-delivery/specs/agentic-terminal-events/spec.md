## ADDED Requirements

### Requirement: Workflow terminal selection SHALL follow the typed decision, not loop position

Dispatch SHALL select the user-facing terminal of a workflow by the succeeded terminal's typed `Decision` and SHALL
NOT select it by route ownership, rule metadata, run phase, or loop position. A terminal whose `Decision.Action` is
a reserved reply action (`respond_direct`, `ask_user`) SHALL be user-facing. A terminal whose `Decision` names any
other action SHALL be a handoff and SHALL publish no `UserResponse` to any channel. A terminal with no `Decision`
SHALL keep the existing route-ownership behaviour.

#### Scenario: routed root handoff publishes nothing

- **GIVEN** a succeeded terminal whose loop owns a complete route
- **AND** its `Decision.Action` is `autoresearch`
- **WHEN** dispatch settles it
- **THEN** it publishes no `UserResponse`
- **AND** records reason `handoff_settled`
- **AND** acknowledges the terminal

#### Scenario: route-less reply decision is delivered to its origin

- **GIVEN** a succeeded terminal whose own reconciled route is empty
- **AND** its `Decision.Action` is `respond_direct`
- **AND** persisted ancestry reaches a loop record with a complete route
- **WHEN** dispatch settles it
- **THEN** it publishes a `result` response carrying `Decision.Reason` to that origin route
- **AND** `InReplyTo` is the deciding loop's ID

#### Scenario: clarification is delivered as a prompt

- **GIVEN** a succeeded terminal whose `Decision.Action` is `ask_user`
- **WHEN** dispatch settles it
- **THEN** it publishes a `prompt` response carrying `Decision.Reason` to the loop's own route when complete,
  otherwise to the resolved origin

#### Scenario: internal phase without a decision stays route-less

- **GIVEN** a succeeded terminal with no `Decision`
- **AND** its own reconciled route is empty
- **WHEN** dispatch settles it
- **THEN** it publishes no `UserResponse`
- **AND** records reason `route_less_settled`
- **AND** does not resolve an origin

### Requirement: Origin route resolution SHALL observe persisted ancestry

When a user-facing decision's own reconciled route is empty, dispatch SHALL resolve the origin by walking persisted
`AGENT_LOOPS` loop records from the terminal loop: the next record is `ParentLoopID` when nonempty, otherwise `RunID`
when nonempty and not the current loop; the first record carrying a complete `ChannelType`/`ChannelID` pair is the
origin. The walk SHALL be bounded at 32 hops with cycle detection, SHALL reuse the existing persisted-loop read and
its transient/permanent classification, and SHALL introduce no new field on `TaskMessage`, no run-entity predicate,
and no second durable authority.

#### Scenario: ancestry walk resolves the HTTP root

- **GIVEN** an empty process-local tracker
- **AND** `AGENT_LOOPS` holds a routed root record and two rule-spawned descendants linked by `ParentLoopID`
- **WHEN** the descendant's reply-decision terminal is settled
- **THEN** the response is published to the root's channel

#### Scenario: run anchor is used when the parent link is absent

- **GIVEN** a terminal loop record with empty `ParentLoopID` and a nonempty `RunID`
- **AND** `AGENT_LOOPS/<RunID>` carries a complete route
- **WHEN** the terminal is settled
- **THEN** the response is published to that route

#### Scenario: missing ancestor settles origin-unresolvable

- **GIVEN** a reply-decision terminal whose next ancestor key is absent from `AGENT_LOOPS`
- **WHEN** the terminal is settled
- **THEN** it publishes no `UserResponse`
- **AND** records reason `origin_unresolvable` with a warning naming the absent loop ID
- **AND** acknowledges the terminal

#### Scenario: transient ancestor read is retried

- **GIVEN** a transient `AGENT_LOOPS` read failure on an ancestor
- **WHEN** the terminal is settled
- **THEN** dispatch delayed-NAKs the terminal
- **AND** does not classify the origin

#### Scenario: malformed ancestor is permanent

- **GIVEN** an ancestor record that is malformed JSON or carries a different loop ID
- **WHEN** the terminal is settled
- **THEN** the delivery is permanently rejected

#### Scenario: walk is bounded

- **GIVEN** ancestry that cycles or exceeds 32 hops without a routed record
- **WHEN** the terminal is settled
- **THEN** it records reason `origin_unresolvable`
- **AND** does not loop indefinitely

## MODIFIED Requirements

### Requirement: Response routing SHALL reconcile fields independently

Dispatch SHALL reconcile `ChannelType`, `ChannelID`, and `UserID` independently from the process-local tracker,
terminal payload, and persisted `AGENT_LOOPS/<loopID>` `LoopEntity`. Empty fields SHALL contribute no value. Matching
nonempty fields SHALL agree. Conflicting nonempty fields SHALL be permanently rejected.

A publishable route SHALL require nonempty `ChannelType` and `ChannelID`. `UserID` SHALL be optional metadata and an
empty `UserID` SHALL NOT invalidate a complete channel pair. Dispatch SHALL observe persisted state before classifying
a route as partial or route-less. A route-less classification SHALL apply only to terminals that carry no user-facing
`Decision`; a user-facing decision with an empty own route SHALL proceed to origin resolution.

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

#### Scenario: route-less terminal without a user-facing decision

- **GIVEN** persisted-state observation is complete
- **AND** both `ChannelType` and `ChannelID` are empty after reconciliation
- **AND** the terminal carries no user-facing `Decision`
- **WHEN** dispatch settles the terminal
- **THEN** it publishes no `UserResponse`
- **AND** it may acknowledge the terminal after all other required work succeeds

#### Scenario: transient persisted-state lookup failure

- **GIVEN** dispatch cannot yet observe `AGENT_LOOPS/<loopID>` because of a transient lookup failure
- **WHEN** the retained terminal is processed
- **THEN** dispatch delayed-NAKs the terminal
- **AND** does not classify it as route-less or malformed

### Requirement: Dispatch projection SHALL match normalized terminal class

Dispatch SHALL project succeeded terminals as result responses, failed terminals as error responses, and cancelled
terminals as status responses. A succeeded terminal whose `Decision.Action` is `ask_user` SHALL instead be projected
as a prompt response. A succeeded terminal carrying a user-facing `Decision` SHALL carry `Decision.Reason` as its
content; a succeeded terminal without a `Decision` SHALL carry its result content unchanged. Tracker mutation SHALL be
idempotent and SHALL use the validated terminal timestamp.

#### Scenario: successful loop result

- **GIVEN** a valid succeeded terminal carrying result content and no `Decision`
- **WHEN** dispatch projects it
- **THEN** dispatch emits a result response carrying that result

#### Scenario: reply decision carries its reason

- **GIVEN** a valid succeeded terminal whose `Decision.Action` is `respond_direct`
- **WHEN** dispatch projects it
- **THEN** dispatch emits a result response whose content is `Decision.Reason`

#### Scenario: clarification decision is a prompt

- **GIVEN** a valid succeeded terminal whose `Decision.Action` is `ask_user`
- **WHEN** dispatch projects it
- **THEN** dispatch emits a prompt response whose content is `Decision.Reason`

#### Scenario: cancelled loop

- **GIVEN** a valid cancelled terminal
- **WHEN** dispatch projects it
- **THEN** dispatch emits a deterministic cancellation status response

### Requirement: Terminal settlement telemetry SHALL use bounded reasons

The framework SHALL emit exactly one final fixed-reason disposition per terminal attempt. Reasons SHALL distinguish
envelope/type rejection, payload-validation rejection, zero terminal timestamp, identity/category/outcome collision,
routing collision or malformed state, tracker-projection collision, transient routing read, transient response
publication, successful response settlement, route-less settlement, handoff settlement, and origin-unresolvable
settlement. Loop IDs, user IDs, channel IDs, actions, and subjects SHALL NOT be metric labels.

#### Scenario: unsettled terminal is permanently malformed

- **WHEN** dispatch permanently rejects a terminal
- **THEN** it records exactly one fixed reason
- **AND** no unbounded identity appears in metric labels

#### Scenario: handoff and origin-unresolvable are fixed reasons

- **WHEN** dispatch settles a handoff decision or cannot resolve an origin
- **THEN** it records exactly one of `handoff_settled` or `origin_unresolvable`
- **AND** the decision action appears only in the log line, never as a label

### Requirement: Delivery declaration SHALL remain bounded and honest

The framework SHALL describe terminal-derived `UserResponse` publication as at-least-once within bounded AGENT
retention and USER duplicate-detection mechanisms. It SHALL NOT claim exactly-once, indefinite retry, per-message
eviction proof, or post-eviction response delivery. It SHALL additionally document that origin resolution reads
`AGENT_LOOPS`, whose keys expire 24h after their last write, and SHALL NOT claim delivery of a workflow answer whose
routed ancestor record has expired.

#### Scenario: operator inspects the contract

- **WHEN** an operator evaluates recovery guarantees
- **THEN** the documented AGENT age/capacity horizon and visibility gap are explicit
- **AND** the finite-MaxDeliver advisory is not described as an eviction signal for these consumers
- **AND** the `AGENT_LOOPS` 24h origin horizon is explicit
