## ADDED Requirements

### Requirement: Prior messages accompany an independent chat turn

UserMessage and HTTPMessageRequest SHALL accept optional `prior_messages` using the existing ChatMessage shape.
Missing, null, and empty history SHALL be equivalent. Valid ordered text-only user/assistant history SHALL be copied
unchanged into the durable TaskMessage. Invalid routable history SHALL reach the existing observable error response
before task publication; nonempty history on a command SHALL be refused rather than ignored. Required negative-response
publication failure SHALL NOT acknowledge its durable source.

Every new work submission SHALL start an independent execution with a freshly minted LoopID. No submission SHALL
attach to or rebind an existing execution. Commands requiring a loop SHALL use an explicit target argument; their
existing control/read admission and custom-command signatures remain unchanged.

Config.AutoContinue, UserMessage.ReplyTo and HTTPMessageRequest.ReplyTo SHALL be absent. Supplying `auto_continue`
in component JSON or `reply_to` in submission JSON SHALL be explicitly refused, including null or empty values;
omission SHALL remain valid. The check SHALL recognize the case-folded spellings previously consumed by encoding/json
without introducing general strict-JSON policy or accepting a compatibility value.

A routable rejected USER input SHALL publish the existing typed negative response and receive PubAck before
Terminate. Publication failure SHALL Retry. HTTP SHALL return its existing synchronous error response. Retired-key
refusal SHALL occur before command effects, task lookup/minting/publication or loop mutation.

Same-source redelivery SHALL validate and reuse its retained task. Source comparison SHALL preserve ordered history
and all surviving correlation fields, with nil and empty history equivalent; it SHALL NOT reinterpret a replay as a
new conversational turn. Different history under the same source identity SHALL use existing correlation quarantine,
without overwriting the task or minting a replacement LoopID.

The adapter supplies its displayed user text and UserResponse.Content; SemStreams SHALL NOT promise automatic hosted
conversation recall. Existing transport limits apply without silent history trimming or new caller-computed limits.

#### Scenario: Displayed history crosses the registered task boundary

- **GIVEN** a follow-up contains prior user text and the delivered assistant UserResponse.Content
- **WHEN** dispatch accepts it through HTTP or a registered UserMessage
- **THEN** the registered durable TaskMessage contains that ordered history unchanged
- **AND** this remains true when the displayed Decision.Reason differs from the provider's raw Result

#### Scenario: A new turn never targets an active execution

- **GIVEN** another execution exists for the user/channel route
- **WHEN** an otherwise valid new work submission arrives
- **THEN** its task has a fresh LoopID and contains only the supplied displayed history
- **AND** no existing execution is rebound or mutated

#### Scenario: Empty history has no presence semantics

- **WHEN** otherwise identical inputs omit prior_messages, set it to null, or supply an empty array
- **THEN** their history validation and routing behavior are equivalent
- **AND** retained-source correlation treats them as the same history

#### Scenario: Retired targeting is refused explicitly

- **WHEN** component JSON contains auto_continue, or submission JSON contains reply_to, with any value
- **THEN** the applicable boundary refuses and names the retired key
- **AND** no command, task publication, new task/loop identity or loop mutation occurs
- **AND** a routable durable USER receives its negative response before termination
- **AND** failed negative publication retries without acknowledging that source

#### Scenario: Commands require an explicit target

- **WHEN** a command requiring a loop omits loop_id
- **THEN** dispatch reports that an explicit loop_id is required
- **AND** nonempty history on any command is an input error, not ignored data

#### Scenario: Explicit controls remain available

- **WHEN** a caller supplies an explicit cancel/status/read/approval target
- **THEN** its existing token, authority and permission checks remain in force
- **AND** no target is inferred from another active loop

#### Scenario: Redelivery retains the committed conversation input

- **GIVEN** dispatch committed a task and was replaced before settling its source
- **WHEN** the source redelivers
- **THEN** dispatch reuses its committed LoopID and history without creating a new conversational turn
- **AND** changed ordered history under the same source identity quarantines rather than overwrites or remints

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.complete`, and `agent.failed` through their owning durable callbacks.
It SHALL NOT consume `agent.created` or `agent.approval_pending` as correctness inputs. Business handlers SHALL
receive only an immutable owner-supplied work view and SHALL return a typed semantic outcome. Native message and
settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL NOT be positively acknowledged until every required publication has synchronous JetStream
PubAck, except for the task-only retained-commitment proof defined below. A successfully decoded and validated exact
retained TaskMessage whose TaskID, LoopID and source correlation match the submission SHALL satisfy that task's
publication obligation without another task publication, even when the original PubAck was not observed.

All other required publications, including cancel signals, approval responses and user responses, SHALL retain their
existing synchronous PubAck requirement. Ordinary publications remain at-least-once. Terminal events SHALL retain
their typed ancestry and read-through contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

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

### Requirement: Dispatch uses one authority-backed current-state projection

Dispatch SHALL use one caught-up graph view over `AGENT_LOOPS` for `/activity`, `/loops`, and `/debug/state`.
`LoopTracker` and pending-approval process caches SHALL NOT exist. `/loops` and `/debug/state` SHALL
preserve the existing immutable `LoopInfo` JSON schema except for `execution_id` on the existing nested
`PendingApprovalInfo`. That identity SHALL come from observed pending authority and appear in the corresponding
JSON/OpenAPI schema. The existing optional `context_request_id` field SHALL remain empty in the authority-backed
projection, as accepted by the edge-gateway design; no historical notification lookup SHALL reconstruct it.
All other unrelated DTO fields and projection contracts SHALL remain unchanged. This identity
addition SHALL NOT authorize tracker retirement or unrelated projection expansion as part of the approval correction.
`/debug/state` SHALL expose the view's caught-up readiness
and current poison diagnostics rather than reporting a false empty state.

Explicit LoopID approval, read, cancellation, terminal-route, and command-owner operations SHALL
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
- **AND** no path assumes zero loops

#### Scenario: Loop DTO shape is preserved

- **WHEN** `/loops` or `/debug/state` reports a valid view-derived loop
- **THEN** it uses the existing immutable `LoopInfo` JSON schema with only the declared nested pending
  `execution_id` addition
- **AND** `context_request_id` retains its optional schema but is empty, as accepted for the authority-backed view
- **AND** JSON/OpenAPI verification preserves every other unrelated field and mapping
- **AND** no mutable loop entity, tracker state, or projection internals enter the response

### Requirement: Dispatch task redelivery recovers the committed LoopID

Dispatch SHALL derive stable TaskID from validated `UserMessage` identity. For new work it SHALL mint a
random framework LoopID and retain that LoopID in the committed `TaskMessage`. On source redelivery it SHALL
exact-read the retained task by TaskID, validate the TaskID/source mapping, and recover the retained LoopID. One
TaskID naming two LoopIDs SHALL quarantine.

When this exact retained-task validation succeeds, both durable UserMessage handling and HTTP submission SHALL reuse
the task commitment without republishing the task. This proof SHALL apply only to that task publication. Typed
absence SHALL retain the existing preparation/publication path; failed reads and invalid or conflicting evidence
SHALL retain each caller's existing classifications and SHALL NOT mint or publish replacement work.

The durable UserMessage path SHALL still obtain PubAck for its required user response before source ACK; response
publication failure SHALL retry. HTTP submission SHALL retain its existing synchronous response, exact-read refusal,
and optional stream-mirror behavior. This change SHALL NOT make the optional HTTP mirror a required publication.
Retained-task reuse SHALL NOT delete the task, advance its destination consumer, or claim to repair late DeliverNew
consumers or exhausted delivery budgets.

Cancel, approval-response, refusal, terminal user-response, and other ordinary publications SHALL be at-least-once.
Source ACK SHALL wait for every required PubAck, subject only to the task-specific retained-commitment exception above.
A stable `Nats-Msg-Id` MAY suppress duplicates inside the configured
server window, but SHALL NOT be treated as exact commitment proof or a guarantee beyond that window. Dispatch SHALL
NOT add exact committed-output lookup for ordinary publications.

#### Scenario: User delivery repeats after task commit

- **GIVEN** the exact correlated TaskMessage remains retained
- **WHEN** a UserMessage redelivers, including after replacement lost the original PubAck observation
- **THEN** dispatch validates and reuses its committed TaskID, LoopID and source correlation
- **AND** does not publish another task, including beyond the duplicate-suppression window
- **AND** obtains PubAck for its required user response before acknowledging the source
- **AND** failure of that response publication retries without replacing the task

#### Scenario: HTTP submission reuses a retained task

- **WHEN** HTTP submission finds and validates its exact retained task
- **THEN** it reuses that commitment without another task publication
- **AND** preserves the existing synchronous response and optional stream-mirror behavior
- **AND** an exact-read failure retains the existing HTTP refusal behavior

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
SHALL poison authoritative listing until a greater-revision valid write or tombstone heals them.
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
- **AND** authoritative listing may resume after that revision is applied

#### Scenario: Non-authority record is present

- **WHEN** a key is neither a canonical LoopID nor a completion key
- **THEN** it is excluded without interpreting its value or optional producer's namespace
- **AND** it does not become a loop or poison current-loop authority

#### Scenario: Ordinary completion remains activity-only

- **GIVEN** an ordinary raw or registered completion with a canonical key and matching payload LoopID
- **WHEN** the shared view decodes it
- **THEN** it preserves the existing activity fields
- **AND** it does not supply a current-loop record

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

#### Scenario: An explicit control reads authority after replacement

- **GIVEN** dispatch was replaced and an exact durable loop record remains
- **WHEN** a caller explicitly requests status, cancellation or approval for that loop
- **THEN** dispatch uses that durable authority and the operation's existing admission rules
- **AND** it neither creates nor rebinds an execution

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

### Requirement: One gate admits every request that names an existing loop

Dispatch MUST admit every supported explicit control/read request that names a loop through exactly one gate, and
no seam MAY hand-roll
any part of the decision. The gate runs three checks in a FIXED order — **form, then existence, then
ownership** — so that a later reason never masks an earlier one: a malformed token is always answered as
malformed, never as "not found" or "not yours", and an absent loop is always answered as absent, never as
"not yours". Ordering is the requirement, not an implementation note: it is what makes a refusal reason
diagnostic rather than a leak of whether some other party's loop exists.

- **Form** MUST reuse the canonical loop-token predicate that `entity-id-contract` defines. The gate MUST NOT
  contain a second spelling of loop-token shape — no length test, no prefix test, no regular expression.
- **Existence** MUST be decided from validated durable authority (see the merged-facts requirement below), never from process
  memory alone.
- **Ownership** MUST be decided by the ownership model below, against the loop's own recorded owner.

Every refusal MUST be a classified error carrying a machine-readable `Code` and a `Detail` map naming the seam
and the failing field; an unclassified `Wrap`-family error is not a refusal this gate may return. Exactly one
home MUST map a refusal to its metric reason label, so two seams cannot disagree about what the same refusal is
called. Exactly one named log constant MUST carry the refusal WARN, so a test pins the production string rather
than a copy of it. A refusal MUST increment its counter exactly once and MUST NOT be counted again by a seam
that already returned.

The gate MUST NOT read `AGENT_TRAJECTORIES`, ObjectStore evidence, or any other execution-audit surface. Agent
execution evidence stays write-only from execution's side; nothing in the admission decision may depend on it.

#### Scenario: a malformed token is refused as malformed even when the loop does not exist

- **GIVEN** a request naming the loop `loop_ab12cd34`, which no `AGENT_LOOPS` record holds
- **WHEN** the gate admits it on any seam
- **THEN** the refusal carries the invalid-token code and names the token field, not the not-found code
- **AND** the refusal counter increments exactly once with the invalid-token reason
- **AND** the test that verifies this is `TestFormRefusalPrecedesExistenceRefusal`

#### Scenario: an absent loop is refused as absent even when the requester is not its owner

- **GIVEN** a canonical loop token that no `AGENT_LOOPS` record holds
- **WHEN** a requester who owns no such loop requests its cancellation
- **THEN** the refusal carries the not-found code, not the not-owned code
- **AND** the test that verifies this is `TestExistenceRefusalPrecedesOwnershipRefusal`

#### Scenario: every seam refuses through the one gate with one counted reason

- **GIVEN** explicit `/cancel` and `/status` commands on the channel and HTTP paths, and the `GET` and `approval`
  loop endpoints
- **WHEN** each is given a non-canonical loop token
- **THEN** each refuses through the shared gate, each emits the single named refusal log constant, and the
  refusal counter records one increment labelled with that seam
- **AND** the tests that verify this are `TestEverySeamRefusesThroughTheGate` and
  `TestRefusalIsCountedExactlyOncePerSeam`

Submission `reply_to` is retired and refused before this gate; it is not an operation naming an existing loop.

### Requirement: The ownership model binds the user lane, and approval is deliberately not owner-scoped

The gate MUST apply exactly this ownership model to requests arriving on the user lane, and MUST NOT extend it:

- **cancel**: the requester MUST equal the loop's recorded owner OR appear in the configured cancel-any list.
- **approve**: the requester MUST appear in the configured approve list. **Ownership is deliberately NOT
  consulted.** A second-party reviewer is the entire point of an approval, and a future change that "fixes" this
  by adding an owner check removes the capability. The approve list has been advertised in configuration and
  unread by any call site; this requirement is what makes it load-bearing. Its default admits everyone, so
  enforcing it changes no default deployment's behaviour.
- **read** (`GET` of a loop): form is checked; ownership is NOT. Scoping reads is a separate question and is
  not decided here.

An unknown owner MUST fail closed. When a user-lane request names a loop whose recorded owner cannot be
determined from validated durable authority — absent or present with no recorded owner — the gate MUST refuse. The
consequence is stated so it is not later mistaken for a bug: a user-lane request naming a **system-lane** loop
is refused, because a system-lane loop has no user owner to match.

Two lanes exist and only one is bound by the model above. The **user lane** is dispatch: identity, permissions,
and channels. The **system lane** is the rule engine's agent-publish action and the graph-research continuation
subject; loops born on that lane carry no user owner, never traverse this gate, and MUST NOT be refused for
having no owner.

#### Scenario: a non-owner on the cancel-any list may cancel

- **GIVEN** a loop created by `user-a` and an operator in the cancel-any list
- **WHEN** the operator cancels it by command
- **THEN** it is admitted, and a requester on neither the cancel-any list nor the loop's ownership is refused
- **AND** the test that verifies this is `TestCancelAnyAdmitsNonOwnerCancel`

#### Scenario: an approver who does not own the loop is admitted

- **GIVEN** a loop created by `user-a` awaiting approval, and `reviewer-b` in the approve list
- **WHEN** `reviewer-b` submits the approval
- **THEN** it is admitted and published, and ownership is never consulted
- **AND WHEN** `stranger-c`, absent from the approve list, submits the same approval
- **THEN** it is refused with the permission reason
- **AND** the tests that verify this are `TestApprovalIsNotOwnerScoped` and
  `TestApprovalRefusedForCallerOutsideApproveList`

#### Scenario: a system-lane loop is not refused for having no owner

- **GIVEN** a loop spawned by a rule's agent-publish action, carrying no user owner
- **WHEN** it runs, publishes, and settles
- **THEN** no admission refusal occurs anywhere on its path, because it never traverses the user-lane gate
- **AND** the test that verifies this is `TestSystemLaneLoopIsNotOwnerChecked`

### Requirement: The gate is not authorization, and the spec says so

This capability MUST NOT be read, cited, or extended as an authorization boundary. Caller identity on this plane
is **asserted by the caller**: it is taken from product middleware when middleware supplied it, otherwise from
the request body's own claimed user field, otherwise from a fixed default. Nothing verifies it. A party that can
reach a dispatch seam can therefore claim any identity, and every check above will pass for the identity it
claimed.

The gate applies the declared control permissions and makes every gate refusal countable. Live attachment is
retired independently; its removal is not an authorization boundary. What it does not buy is isolation between mutually untrusted parties. Authorization — authenticated
identity, and a policy surface that binds it — is a separate contract and is not delivered here.

#### Scenario: an asserted identity is accepted at face value

- **GIVEN** no authenticating middleware installed
- **WHEN** a client submits a request claiming any user identity it likes
- **THEN** that identity is used for every check in this capability, unverified
- **AND** the test that verifies this is `TestAssertedIdentityIsNotVerified`

### Requirement: A refused or unpublishable submission leaves no tracked loop and no moved gauge

A submission that does not result in a published task MUST leave dispatch's observable state exactly as it found
it. Dispatch has no loop tracker or active-loops gauge to update. A refusal MUST NOT publish task work or mutate
loop authority. Every routable submission failure MUST answer the submitter with a typed error response that
names the offending field, synchronously on the HTTP path and on the response subject on the channel path, and
MUST increment a counter. A logged bare return is not an acceptable outcome on any submission path.

#### Scenario: a task that fails payload validation answers the submitter and leaks nothing

- **GIVEN** a submission whose task message fails validation at serialization time, for example an empty prompt
  or an empty role
- **WHEN** dispatch handles it on the channel path
- **THEN** an error response naming the offending field is published to the response subject, a refusal is
  counted, no task is published and loop authority is unchanged
- **AND WHEN** the same submission arrives on the HTTP path
- **THEN** the client receives a synchronous error response naming the offending field rather than a generic
  retry suggestion, a refusal is counted, no task is published and loop authority is unchanged
- **AND** the tests that verify this are `TestValidationFailureAnswersChannelSubmitter`,
  `TestValidationFailureAnswersHTTPSubmitter`, and `TestFailedSubmissionLeavesGaugeAndTrackerUnchanged`

### Requirement: The ungated seams are named, with the reason each is exempt

Every seam that accepts a loop token and does NOT pass through the gate MUST be listed here with its reason, so
that an ungated seam is a recorded decision rather than an omission a later reader has to rediscover:

- **Framework-published loop events** — loop-created, approval-pending, and terminal completion and failure
  events. These correlate by a loop id the framework itself published on a stream, not by a caller-controlled
  field. There is no requester to check. Dispatch consumes only complete/failed for terminal routing, not
  created/pending as correctness inputs.
- **Outbound approval-pending events.** An event agentic-loop publishes, not a request dispatch admits. Its token still
  carries the form check at its own payload boundary.
- **Read-projection wire types** for the loops view and for completion decoding. These decode framework-written
  records; they are not an intake of untrusted input.
- **The user-message payload's own validation.** Surviving run/reply-lineage tokens are checked at the dispatch
  seam. Decoding a retired `reply_to` key records only private, nonserialized presence, not a target or value.
  Dispatch validates before command/task handling so routable rejection can publish its negative response;
  decode-time rejection MUST NOT replace that response. Malformed, unregistered and unroutable input retains its
  existing refusal behavior without an invented route.
- **The HTTP request body type for submissions.** A retired `reply_to` key is rejected during body decoding through
  the existing synchronous error response. Surviving run/reply-lineage fields keep their existing validation.
- **Loop read (`GET`)** is gated for form and existence only; ownership is not consulted, per the ownership
  model above.

#### Scenario: a framework-published loop event is not owner-checked

- **GIVEN** a terminal completion event for a loop with a recorded owner
- **WHEN** dispatch handles it
- **THEN** it is not refused for ownership, and terminal routing follows its existing contract
- **AND** created/pending events are not dispatch correctness inputs
- **AND** the test that verifies this is `TestFrameworkPublishedEventsAreNotOwnerChecked`
