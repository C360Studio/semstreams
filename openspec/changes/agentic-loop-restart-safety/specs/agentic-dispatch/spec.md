## ADDED Requirements

### Requirement: Prior messages accompany an independent chat turn

UserMessage and HTTPMessageRequest SHALL accept optional `prior_messages` using the existing ChatMessage shape.
Missing, null, and empty history SHALL be equivalent. Valid ordered text-only user/assistant history SHALL be copied
unchanged into the durable TaskMessage. Invalid routable history SHALL reach the existing observable error response
before task publication; nonempty history on a command SHALL be refused rather than ignored. Required negative-response
publication failure SHALL NOT acknowledge its durable source.

AutoContinue SHALL default to false in typed configuration and generated schema. An ordinary submission without
ReplyTo SHALL create an independent execution under defaults, even if another loop is active. Explicit ReplyTo and
explicitly configured AutoContinue SHALL retain existing attachment behavior. After attachment admission succeeds,
nonempty prior history SHALL be refused as conflicting intent before task publication or live-loop mutation.
Commands requiring a loop SHALL require an explicit loop_id under defaults, with an error that states that remedy.

Same-source redelivery SHALL recover its retained task before consulting current attachment state. Source comparison
SHALL include ordered prior role/content, with nil and empty equivalent. Different history under the same source
identity SHALL use existing correlation quarantine, without overwriting the task or minting a replacement LoopID.

The adapter supplies its displayed user text and UserResponse.Content; SemStreams SHALL NOT promise automatic hosted
conversation recall. Existing transport limits apply without silent history trimming or new caller-computed limits.

#### Scenario: Displayed history crosses the registered task boundary

- **GIVEN** a follow-up contains prior user text and the delivered assistant UserResponse.Content
- **WHEN** dispatch accepts it through HTTP or a registered UserMessage
- **THEN** the registered durable TaskMessage contains that ordered history unchanged
- **AND** this remains true when the displayed Decision.Reason differs from the provider's raw Result

#### Scenario: Independent work is the default

- **GIVEN** another execution is active on the same user and channel route
- **WHEN** a submission omits ReplyTo and AutoContinue uses its default
- **THEN** dispatch publishes a task with a fresh LoopID rather than attaching to the active execution

#### Scenario: Empty history has no presence semantics

- **WHEN** otherwise identical inputs omit prior_messages, set it to null, or supply an empty array
- **THEN** their history validation and routing behavior are equivalent
- **AND** retained-source correlation treats them as the same history

#### Scenario: History conflicts with admitted attachment

- **GIVEN** ReplyTo or explicitly configured AutoContinue resolves an admitted attachment
- **WHEN** the input also supplies nonempty prior history
- **THEN** the caller receives an error without a task publication or live-loop mutation
- **AND** the same attachment without history retains its existing behavior

#### Scenario: Commands require an explicit default target

- **WHEN** a command requiring a loop omits loop_id under default configuration
- **THEN** dispatch reports that an explicit loop_id is required
- **AND** nonempty history on any command is an input error, not ignored data
- **AND** explicitly configured AutoContinue retains its existing implicit command target behavior

#### Scenario: Redelivery retains the committed conversation input

- **GIVEN** dispatch committed a task and was replaced before settling its source
- **WHEN** the source redelivers
- **THEN** dispatch reuses its committed LoopID and history before resolving current attachment state
- **AND** changed ordered history under the same source identity quarantines rather than overwrites or remints

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
preserve the existing immutable `LoopInfo` JSON schema except for `execution_id` on the existing nested
`PendingApprovalInfo`. That identity SHALL come from observed pending authority and appear in the corresponding
JSON/OpenAPI schema. The existing optional `context_request_id` field SHALL remain empty in the authority-backed
projection, as accepted by the edge-gateway design; no historical notification lookup SHALL reconstruct it.
All other unrelated DTO fields and projection contracts SHALL remain unchanged. This identity
addition SHALL NOT authorize tracker retirement or unrelated projection expansion as part of the approval correction.
`/debug/state` SHALL expose the view's caught-up readiness
and current poison diagnostics rather than reporting a false empty state.

Explicit LoopID approval, read, continuation, cancellation, terminal-route, and command-owner operations SHALL
exact-read and validate `AGENT_LOOPS/<LoopID>`. A partial, stale, watcher-lost, or relevant-poisoned projection SHALL
never be treated as empty.

HTTP `ApprovalRequest` SHALL require `execution_id`, echoing the opaque ExecutionID attached to the prompt the human
reviewed. Dispatch SHALL compare it with exact validated pending authority before publishing `ApprovalResponse`,
which SHALL carry the same echo. Missing identity SHALL return HTTP 400. A different or no-longer-current gate SHALL
return HTTP 409 without approval publication. Dispatch SHALL NOT compute the identity for the client, substitute
current identity for an omitted or outdated echo, or use an old prompt to approve a newly observed gate.

Existing pending HTTP output SHALL expose the pending ExecutionID after replacement without requiring an earlier
approval-pending event. The loop owner SHALL independently check the echoed identity when applying the decision;
dispatch's check SHALL NOT stand in for that application-time check.

#### Scenario: Approval follows replacement

- **GIVEN** exact current state is awaiting approval
- **WHEN** an authorized approval names its canonical LoopID and echoes the displayed pending ExecutionID
- **THEN** dispatch verifies the echo before obtaining CallID from validated `PendingApproval` state
- **AND** it publishes the same ExecutionID in ApprovalResponse
- **AND** requires no earlier approval-pending event

#### Scenario: Pending output carries observed identity after replacement

- **GIVEN** exact validated current authority contains a pending approval
- **WHEN** the existing pending HTTP projection is read after dispatch replacement
- **THEN** it carries the pending record's ExecutionID as `execution_id`
- **AND** the caller need not compute identity or have received the original event

#### Scenario: An older approval client omits the gate echo

- **WHEN** an HTTP approval omits `execution_id`
- **THEN** dispatch returns 400 without approval publication
- **AND** it does not fill the omission from current pending authority

#### Scenario: A stale displayed prompt is submitted

- **GIVEN** the displayed prompt names execution A
- **AND** exact coherent current authority exposes B or no pending gate
- **WHEN** the client submits A's ExecutionID
- **THEN** dispatch returns 409 without approval publication or authority mutation
- **AND** matching LoopID and provider CallID do not substitute B's identity

#### Scenario: The gate changes after dispatch publication

- **GIVEN** dispatch validated and published the submitted ExecutionID
- **WHEN** another gate becomes current before loop application
- **THEN** the loop's independent exact-authority check prevents wrong-gate application
- **AND** the native decision follows the loop's observable, effect-free inapplicable settlement contract

#### Scenario: Projection endpoint is unavailable

- **WHEN** the shared view is not caught up or has current-loop poison
- **THEN** listing and debug return service unavailable
- **AND** debug diagnostics identify not-caught-up readiness or the current poison condition
- **AND** AutoContinue remains retryable
- **AND** no path assumes zero loops

#### Scenario: Loop DTO shape is preserved

- **WHEN** `/loops` or `/debug/state` reports a valid view-derived loop
- **THEN** it uses the existing immutable `LoopInfo` JSON schema with only the declared nested pending
  `execution_id` addition
- **AND** `context_request_id` retains its optional schema but is empty, as accepted for the authority-backed view
- **AND** JSON/OpenAPI verification preserves every other unrelated field and mapping
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

### Requirement: The shared view separates current authority from activity

Bare canonical LoopID keys SHALL validate as `LoopEntity` with key/ID equality. Invalid values under those keys
SHALL poison authoritative listing and AutoContinue until a greater-revision valid write or tombstone heals them.
Other non-completion keys SHALL be excluded from current-loop authority without optional-producer classification.

Existing ordinary `COMPLETE_` activity SHALL retain its field mappings and canonical suffix/payload identity checks.
Completion records SHALL remain activity-only. Unsupported or malformed completion records SHALL produce observable
activity errors and SHALL NOT fabricate current state or block current-loop authority.

This change SHALL NOT introduce research completion rendering, a payload behavior, or a shared namespace package.
Registered stream terminal decoding and validation SHALL remain unchanged by this activity-scope reduction.
Withdrawal of research rendering SHALL NOT withdraw registered ordinary-terminal support. Both private raw
terminal records and registered ordinary-terminal envelopes SHALL retain their current validation and field mappings.

#### Scenario: Canonical current-loop corruption heals

- **GIVEN** an invalid value under a canonical LoopID has poisoned current-loop authority
- **WHEN** a greater-revision valid value with matching ID or a tombstone lands
- **THEN** the poison clears
- **AND** authoritative listing and AutoContinue may resume after that revision is applied

#### Scenario: Non-authority record is present

- **WHEN** a key is neither a canonical LoopID nor a completion key
- **THEN** it is excluded without interpreting its value or optional producer's namespace
- **AND** it does not become a loop or poison current-loop authority

#### Scenario: Ordinary completion remains activity-only

- **GIVEN** an ordinary raw or registered completion with a canonical key and matching payload LoopID
- **WHEN** the shared view decodes it
- **THEN** it preserves the existing activity fields
- **AND** it does not supply a current-loop record or an AutoContinue candidate

#### Scenario: Unsupported completion does not block current authority

- **GIVEN** an unsupported or malformed completion value
- **WHEN** the shared view observes it
- **THEN** activity reports the existing observable error
- **AND** no result or current-loop state is fabricated
- **AND** current-loop authority remains available if its own records and watcher are healthy

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
