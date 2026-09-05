## ADDED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.complete`, and `agent.failed` through their owning durable callbacks.
It SHALL NOT consume `agent.created` or `agent.approval_pending` as correctness inputs. Business handlers SHALL
receive only an immutable owner-supplied work view and SHALL return a typed semantic outcome. Native message and
settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, cancel signal, approval response,
and user-response publication has synchronous JetStream PubAck. These ordinary publications are at-least-once.
Terminal events SHALL retain their typed ancestry and read-through contract. No void, log-only, or core-NATS
publication failure SHALL become ACK.

The three durable subscriptions SHALL invoke their typed business handlers using the callback installed by each
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; dispatch SHALL
NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout. A physical
subscription SHALL move to the existing heartbeat owner only after measured legitimate work can exceed its
configured acknowledgement interval.

The first owner-fatal result from any dispatch delivery owner SHALL synchronously latch before the exact handle is
drained. Existing Health SHALL report `Healthy=false`, status `delivery ownership lost`, the exact first cause in
`LastError`, and exactly one owner-loss error count. Later owner-fatal results SHALL neither overwrite nor recount
the first cause. This replaces per-lane fatal aggregation and adds no metric family, public state, durable state, or
communication path.

#### Scenario: Task publication succeeds but user response fails

- **WHEN** the stable `TaskMessage` receives PubAck
- **AND** the required user response does not receive PubAck
- **THEN** dispatch retries the UserMessage
- **AND** the ordinary user response may be published again

#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its typed user error receives PubAck before termination
- **AND** the authority-backed projection remains unchanged

#### Scenario: Terminal publication is uncertain

- **WHEN** a user response does not receive PubAck
- **THEN** its terminal source is not positively acknowledged
- **AND** at-least-once publication may repeat

#### Scenario: Dispatch business work reaches its own deadline

- **WHEN** a delivery-owned dispatch operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

### Requirement: Dispatch is exclusively an edge gateway

Dispatch SHALL only admit external requests and publish task, cancel, and approval work; expose exact LoopID reads
and one caught-up current-state view; and bridge terminal complete/failed events to user responses when validated
authority carries a user route. Agentic-loop SHALL exclusively own loop birth, pending approval, every intermediate
transition, and terminal state.

Dispatch SHALL NOT create, advance, repair, infer, persist, or cache intermediate loop state and SHALL NOT consume
`agent.created` or `agent.approval_pending` as correctness inputs. Those publications remain available to external
subscribers.

A validated terminal for a system-lane loop with no user route SHALL settle without `user.response`. Conflicting or
temporarily unreadable route evidence SHALL not be treated as routeless.

#### Scenario: System-lane terminal has no user route

- **GIVEN** validated loop authority identifies a terminal system-lane loop with no user route
- **WHEN** dispatch receives its complete or failed event
- **THEN** dispatch publishes no `user.response`
- **AND** settles the terminal source after required validation

#### Scenario: AutoContinue observes the loop-birth gap

- **GIVEN** a new task has received PubAck but its first `LoopEntity` is not yet visible
- **WHEN** another route-only message uses the same `(UserID, ChannelType, ChannelID)`
- **THEN** dispatch observes zero current matches and may mint another task and random LoopID
- **AND** it does not invent a route claim from process memory
- **AND** a caller requiring continuity must supply the first minted LoopID

### Requirement: Dispatch uses one authority-backed current-state projection

Dispatch SHALL use one caught-up graph view over `AGENT_LOOPS` for `/activity`, `/loops`, `/debug/state`, and
AutoContinue. `LoopTracker` and pending-approval process caches SHALL NOT exist. `/loops` and `/debug/state` SHALL
preserve the existing immutable `LoopInfo` JSON schema. `/debug/state` SHALL expose the view's caught-up readiness
and current poison diagnostics rather than reporting a false empty state.

Explicit LoopID approval, read, continuation, cancellation, terminal-route, and command-owner operations SHALL
exact-read and validate `AGENT_LOOPS/<LoopID>`. A partial, stale, watcher-lost, or relevant-poisoned projection SHALL
never be treated as empty.

#### Scenario: Approval follows replacement

- **GIVEN** exact current state is awaiting approval
- **WHEN** an authorized approval names its canonical LoopID
- **THEN** dispatch obtains CallID from validated `PendingApproval` state
- **AND** requires no earlier approval-pending event

#### Scenario: Projection endpoint is unavailable

- **WHEN** the shared view is not caught up or has current-loop poison
- **THEN** listing and debug return service unavailable
- **AND** debug diagnostics identify not-caught-up readiness or the current poison condition
- **AND** AutoContinue remains retryable
- **AND** no path assumes zero loops

#### Scenario: Loop DTO shape is preserved

- **WHEN** `/loops` or `/debug/state` reports a valid view-derived loop
- **THEN** it uses the existing immutable `LoopInfo` JSON schema
- **AND** no mutable loop entity, tracker state, or projection internals enter the response

#### Scenario: Exact AutoContinue tuple has one match

- **GIVEN** exactly one nonterminal record matches `(UserID, ChannelType, ChannelID)`
- **WHEN** AutoContinue resolves the message
- **THEN** dispatch continues that LoopID

#### Scenario: Partial route does not match

- **WHEN** only UserID, ChannelType, or ChannelID agrees
- **THEN** the record is not an AutoContinue candidate

#### Scenario: AutoContinue is ambiguous

- **GIVEN** more than one exact nonterminal match
- **WHEN** AutoContinue resolves the message
- **THEN** dispatch refuses with typed ambiguity
- **AND** does not guess

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

### Requirement: Dispatch shutdown closes every owner without retaining context

Dispatch SHALL stop accepting new work, drain its three durable inputs, stop and join the shared view control owner,
and join every delivery callback. Neither dispatch nor graphview SHALL retain context or a closure/provider that
recovers one. A failed view SHALL be replaced only by the dispatch lifecycle-control goroutine using its active
lifecycle context. Exported graphview `Restart` is not part of the contract.

#### Scenario: Shutdown races active dispatch work

- **WHEN** dispatch Stop begins while a durable callback and projection observer are active
- **THEN** no new delivery is admitted, every consume handle drains and closes, and both work paths join
- **AND** Stop returns only after no later ACK, publication, or projection mutation is possible

### Requirement: The shared loop view classifies the mixed bucket

Bare canonical LoopID keys SHALL validate as `LoopEntity` with key/ID equality. `COMPLETE_<canonical LoopID>` SHALL
validate by completion family and remain activity-only. Known research namespaces SHALL be ignored as non-loop
records. Every other key SHALL poison as malformed would-be loop state.

A typed terminal payload's LoopID SHALL equal the suffix. A registered `SearchResult` has no payload LoopID; the
suffix supplies its activity identity. Its aggregate `TokensUsed` SHALL NOT populate directional Loop token fields.

Current-loop and unknown-key poison SHALL disable AutoContinue and authoritative listing until a greater-revision
clean write or tombstone heals it.

#### Scenario: SearchResult completion is projected

- **GIVEN** a valid registered `SearchResult` at `COMPLETE_<canonical LoopID>`
- **WHEN** the view decodes it
- **THEN** the suffix supplies LoopID
- **AND** synthesis, success, complete state, and iterations project through the existing Loop activity shape
- **AND** TokensIn and TokensOut remain zero

#### Scenario: Research intermediate record is present

- **WHEN** a known research namespace is observed
- **THEN** it is excluded without becoming loop poison

#### Scenario: Malformed would-be loop heals

- **GIVEN** an unknown or malformed current-loop key has poisoned the view
- **WHEN** a greater-revision clean value or tombstone lands
- **THEN** the poison clears
- **AND** readiness may return after that revision is applied

### Requirement: Terminal user-response routing is retention-intersection bounded

Dispatch SHALL reconstruct terminal user routing only while both the complete/failed source delivery and exact loop
routing state remain retained. It SHALL validate agreement between them and SHALL NOT claim either full configured
horizon.

Temporary read failure retries. Deleted, purged, expired, or evicted loop state is outside the guarantee and reports
`terminal_route_unavailable`. Process memory SHALL never fabricate the route.

#### Scenario: Loop route expires before terminal source

- **GIVEN** a terminal source delivery remains retained but its exact loop record has expired
- **WHEN** dispatch attempts to bridge the result
- **THEN** it reports `terminal_route_unavailable`
- **AND** does not invent a user route or claim delivery

## MODIFIED Requirements

### Requirement: Loop existence and ownership are merged facts, never process memory alone

The gate MUST decide explicit LoopID existence, ownership, route, pending approval, and recorded state from validated
`AGENT_LOOPS/<LoopID>` authority. No process-local entry may establish or override those facts. "Merged facts" in this
requirement does not authorize a second process-memory authority. The bucket name MUST be observed from the
component's declared KV read port; no reader carries another bucket-name default.

A confirmed absent key is not found. Every other read failure is transient and MUST NOT create or admit another loop
for that token.

Authority facts MUST carry and report the loop's recorded state, not merely terminality. State reporting MUST report
`awaiting_approval`, executing, cancelled, complete, failed, or unknown exactly as validated; it MUST NOT render a
fixed "running" value. A record carrying no state or an invalid state, including removed `paused`, MUST be reported or
refused according to its typed invalid/unknown outcome and never fabricated.

A custom command needing ownership SHALL receive only
`LookupLoopOwner(context.Context, LoopID) (LoopOwner, error)`, where immutable `LoopOwner` contains only LoopID and
UserID. Invalid ID, confirmed absence, missing owner, invalid record, and unavailable storage SHALL be distinct error
classes. No raw `LoopEntity`, KV handle, bucket name, tracker, or generic query surface is exposed.

#### Scenario: a continuation after a process replacement is admitted from the durable record

- **GIVEN** a loop created before dispatch was replaced whose exact `AGENT_LOOPS` record names its owner
- **WHEN** that owner continues it by explicit LoopID
- **THEN** the request continues that loop rather than silently forking it

#### Scenario: an unreadable durable record refuses as transient

- **GIVEN** an `AGENT_LOOPS` read fails with anything other than confirmed key absence
- **WHEN** a request names that loop
- **THEN** the refusal is transient rather than not-found
- **AND** no loop is created for the token

#### Scenario: a status read after a process replacement reports the recorded state

- **GIVEN** an `AGENT_LOOPS` record whose loop is `awaiting_approval`
- **WHEN** its owner asks for that loop's status after dispatch replacement
- **THEN** the answer names `awaiting_approval` rather than a fixed "running"
- **AND** a record carrying no state is reported as unknown, never as a state nobody read

#### Scenario: Custom command checks ownership

- **WHEN** a command supplies a canonical LoopID
- **THEN** `LookupLoopOwner` returns only LoopID and UserID from exact authority
- **AND** absence, missing owner, invalid record, and unavailable storage are classified distinctly
