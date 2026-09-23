# agentic-dispatch Specification

## Purpose
`agentic-dispatch` governs **admitting a request that names a loop**. Dispatch is the one plane where a party
outside the framework — a chat channel, an HTTP client, a product shell — hands the framework a loop instance
token and asks it to do something with the loop that token names: continue it, cancel it, approve a tool
call inside it, read it. This capability owns the single gate every one of those seams passes through, the
order its checks run in, the classified vocabulary a refusal carries, the one metric-reason mapping and the one
log line a refusal produces, and the explicit list of seams deliberately left ungated.

It owns the fact that these checks are **correctness and accident-prevention guards, not authorization** — caller
identity on this plane is asserted by the caller, so the gate can prevent an accident and cannot resist an
adversary. Authorization is a separate, later contract (epic #1205).

**What it does NOT cover.** Loop-token grammar belongs to `entity-id-contract`; the gate consumes that predicate
and never re-derives it. Loop execution, the loop entity, and the create-vs-exists fence inside `LoopManager`
belong to `agentic-loop`. Terminal-event settlement and origin routing are already specified by
`agentic-terminal-events` and `user-response-subject-ownership`. Reconstructing process state after a process
replacement is not this capability's answer here and is claimed separately (#1146 / PR #1159). Only the
requirements below are seeded; the rest of the component's contract is written when a change first touches it.
## Requirements
### Requirement: One gate admits every request that names an existing loop

Dispatch MUST admit every inbound request that names a loop through exactly one gate, and no seam MAY hand-roll
any part of the decision. The gate runs three checks in a FIXED order — **form, then existence, then
ownership** — so that a later reason never masks an earlier one: a malformed token is always answered as
malformed, never as "not found" or "not yours", and an absent loop is always answered as absent, never as
"not yours". Ordering is the requirement, not an implementation note: it is what makes a refusal reason
diagnostic rather than a leak of whether some other party's loop exists.

- **Form** MUST reuse the canonical loop-token predicate that `entity-id-contract` defines. The gate MUST NOT
  contain a second spelling of loop-token shape — no length test, no prefix test, no regular expression.
- **Existence** MUST be decided from merged facts (see the merged-facts requirement below), never from process
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

- **GIVEN** a request naming the loop `loop_ab12cd34`, which no tracker entry and no `AGENT_LOOPS` record holds
- **WHEN** the gate admits it on any seam
- **THEN** the refusal carries the invalid-token code and names the token field, not the not-found code
- **AND** the refusal counter increments exactly once with the invalid-token reason
- **AND** the test that verifies this is `TestFormRefusalPrecedesExistenceRefusal`

#### Scenario: an absent loop is refused as absent even when the requester is not its owner

- **GIVEN** a canonical loop token that no tracker entry and no `AGENT_LOOPS` record holds
- **WHEN** a requester who owns no such loop attaches to it
- **THEN** the refusal carries the not-found code, not the not-owned code
- **AND** the test that verifies this is `TestExistenceRefusalPrecedesOwnershipRefusal`

#### Scenario: every seam refuses through the one gate with one counted reason

- **GIVEN** the channel submission path, the HTTP submission path, the `/cancel` and `/status` commands, and the
  `GET` and `approval` loop endpoints
- **WHEN** each is given a non-canonical loop token
- **THEN** each refuses through the shared gate, each emits the single named refusal log constant, and the
  refusal counter records one increment labelled with that seam
- **AND** the tests that verify this are `TestEverySeamRefusesThroughTheGate` and
  `TestRefusalIsCountedExactlyOncePerSeam`

### Requirement: The ownership model binds the user lane, and approval is deliberately not owner-scoped

The gate MUST apply exactly this ownership model to requests arriving on the user lane, and MUST NOT extend it:

- **continue** — a submission resolving to an existing loop, whether by explicit `reply_to` or by auto-continue:
  the requester MUST equal the loop's recorded owner.
- **cancel**: the requester MUST equal the loop's recorded owner OR appear in the configured cancel-any list.
- **approve**: the requester MUST appear in the configured approve list. **Ownership is deliberately NOT
  consulted.** A second-party reviewer is the entire point of an approval, and a future change that "fixes" this
  by adding an owner check removes the capability. The approve list has been advertised in configuration and
  unread by any call site; this requirement is what makes it load-bearing. Its default admits everyone, so
  enforcing it changes no default deployment's behaviour.
- **read** (`GET` of a loop): form is checked; ownership is NOT. Scoping reads is a separate question and is
  not decided here.

An unknown owner MUST fail closed. When a user-lane request names a loop whose recorded owner cannot be
determined — absent from both sources, or present with no recorded owner — the gate MUST refuse. The
consequence is stated so it is not later mistaken for a bug: a user-lane request naming a **system-lane** loop
is refused, because a system-lane loop has no user owner to match.

Two lanes exist and only one is bound by the model above. The **user lane** is dispatch: identity, permissions,
and channels. The **system lane** is the rule engine's agent-publish action and the graph-research continuation
subject; loops born on that lane carry no user owner, never traverse this gate, and MUST NOT be refused for
having no owner.

#### Scenario: a second holder of a loop token cannot continue another user's loop

- **GIVEN** a loop created by `user-a`
- **WHEN** `user-b` submits a message whose `reply_to` is that loop's token
- **THEN** the request is refused with the not-owned reason, no task is published, the loop's tracker record
  still names `user-a`, and its active-loop indexes still point at `user-a`
- **AND** a completion for that loop is still routed to `user-a`
- **AND** the tests that verify this are `TestSecondHolderCannotContinueAnotherUsersLoop` and
  `TestRefusedContinuationDoesNotRepointOwnership`

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

#### Scenario: an attach to a terminal loop is refused

- **GIVEN** a loop in a terminal state, still observable in the tracker or in `AGENT_LOOPS`
- **WHEN** its own owner continues it by `reply_to`
- **THEN** the request is refused with the terminal reason, and no new loop is minted under that token
- **AND** the test that verifies this is `TestAttachToTerminalLoopIsRefused`

### Requirement: The gate is not authorization, and the spec says so

This capability MUST NOT be read, cited, or extended as an authorization boundary. Caller identity on this plane
is **asserted by the caller**: it is taken from product middleware when middleware supplied it, otherwise from
the request body's own claimed user field, otherwise from a fixed default. Nothing verifies it. A party that can
reach a dispatch seam can therefore claim any identity, and every check above will pass for the identity it
claimed.

What the gate does buy is real and worth having: it converts an accidental cross-attach into a typed refusal,
it stops a token holder from silently repointing another party's completion routing, and it makes every refusal
countable. What it does not buy is isolation between mutually untrusted parties. Authorization — authenticated
identity, and a policy surface that binds it — is a separate contract and is not delivered here.

#### Scenario: an asserted identity is accepted at face value

- **GIVEN** no authenticating middleware installed
- **WHEN** a client submits a request claiming any user identity it likes
- **THEN** that identity is used for every check in this capability, unverified
- **AND** the test that verifies this is `TestAssertedIdentityIsNotVerified`

### Requirement: A refused or unpublishable submission leaves no tracked loop and no moved gauge

A submission that does not result in a published task MUST leave dispatch's observable state exactly as it found
it. Tracking the loop and incrementing the active-loops gauge MUST NOT happen until the task message has been
successfully serialized — serialization is where payload validation runs, so a validation failure currently
lands after both. Every failure on a submission path MUST answer the submitter with a typed error response that
names the offending field, synchronously on the HTTP path and on the response subject on the channel path, and
MUST increment a counter. A logged bare return is not an acceptable outcome on any submission path.

#### Scenario: a task that fails payload validation answers the submitter and leaks nothing

- **GIVEN** a submission whose task message fails validation at serialization time, for example an empty prompt
  or an empty role
- **WHEN** dispatch handles it on the channel path
- **THEN** an error response naming the offending field is published to the response subject, a refusal is
  counted, no loop is tracked, and the active-loops gauge does not move
- **AND WHEN** the same submission arrives on the HTTP path
- **THEN** the client receives a synchronous error response naming the offending field rather than a generic
  retry suggestion, a refusal is counted, no loop is tracked, and the gauge does not move
- **AND** the tests that verify this are `TestValidationFailureAnswersChannelSubmitter`,
  `TestValidationFailureAnswersHTTPSubmitter`, and `TestFailedSubmissionLeavesGaugeAndTrackerUnchanged`

### Requirement: One control-signal payload travels the loop signal subject

Exactly one payload type MUST travel `agent.signal.<loop_id>`: `agentic.UserSignal`, wrapped in the standard
`BaseMessage` envelope, published by the `/cancel` command lane. Two types shared that subject before this
change — the chat command lane published the user control signal, the HTTP `POST /loops/{id}/signal` endpoint
published a dispatch-local type — while the loop's only handler for the subject accepts the first and drops
anything else as an unexpected payload type. Both halves are closed here, and neither by repair:

- The dispatch-local control-signal payload, its registry category, and the composition-root registration that
  installed it MUST be retired. It had one producer and **zero consumers**, so nothing reads what would be
  repaired; and it carried no requester identity, no channel route, and no signal id, so it could never satisfy
  this capability's ownership model.
- **`POST /loops/{id}/signal` MUST NOT exist.** It never worked: it answered `200 {"accepted": true}` and the
  loop was never signalled. Of its three verbs, only cancel was ever implemented on the loop side, and cancel is
  already served on the same HTTP surface by `POST /message` with `/cancel <loop_id>`; pause and resume set a
  loop field no code read, and have since been deleted outright (#1239) rather than implemented. The same
  measurement then applied to the rest of the vocabulary: `approve`, `reject`, `feedback` and `retry` were
  admitted by validation and reached no handler either, so the loop logged a warning and **acknowledged the
  message as delivered** — the caller saw success and got nothing. **`cancel` MUST be the entire signal
  vocabulary**, because it is the only verb the loop's signal consumer handles, and a control signal carrying
  any other verb MUST be refused by validation rather than accepted and ignored. Approval and rejection are
  unaffected: they travel as `ApprovalResponse` on `agent.approval_response.*` (ADR-039) and were never
  served by this payload. Deleting the endpoint therefore removes an adopter-facing surface that promised an
  outcome it never delivered, rather than growing it to carry an identity it never had.

The control signal dispatch does publish MUST carry the **requester's** identity as its user, not the loop
owner's: a control signal records who acted, and the loop's cancellation path attributes the action to that
field. Its channel route MUST be taken from the loop's merged facts rather than recomputed by the caller, and
its subject MUST be resolved from the declared output port rather than concatenated.

#### Scenario: the cancel lane actually stops the loop

- **GIVEN** a running loop owned by `loop-owner`, and an operator on the cancel-any list
- **WHEN** the operator posts `/cancel <loop_id>` to the message endpoint
- **THEN** a user control signal naming the **requester** is published on the loop's signal subject, the loop's
  handler accepts it, and the loop transitions to cancelled with the operator recorded as who cancelled it
- **AND** no payload is dropped as an unexpected type on that subject
- **AND** the tests that verify this are `TestCancelCommandCancelsTheLoop` and
  `TestSignalSubjectCarriesExactlyOnePayloadType`

#### Scenario: the retired control-signal payload is gone from the registry

- **GIVEN** a composed binary registering the framework's built-in payloads
- **WHEN** the payload registry is inspected
- **THEN** the dispatch-local control-signal category is absent, and the user control-signal category is the
  only registered payload for the loop signal subject
- **AND** no composition root still calls a dispatch payload registration that registers nothing
- **AND** the test that verifies this is `TestRetiredSignalMessageCategoryIsUnregistered`

#### Scenario: the loop signal endpoint is gone

- **GIVEN** dispatch's registered HTTP routes and its published OpenAPI document
- **WHEN** either is inspected
- **THEN** no `POST /loops/{id}/signal` route, request type, response type, or path entry is present
- **AND** a caller that wants to cancel a loop uses `POST /message` with `/cancel <loop_id>`, which stays
  registered
- **AND** the test that verifies this is `TestLoopSignalEndpointIsGone`

#### Scenario: cancel is the whole vocabulary, and a removed verb is refused by name

- **GIVEN** a control signal carrying any verb other than `cancel` — `pause`, `resume`, `approve`, `reject`,
  `feedback` or `retry`
- **WHEN** it is validated
- **THEN** validation MUST refuse it rather than accept it and acknowledge a message no handler reads
- **AND** the refusal MUST NOT list the rejected verb among the permitted types — it names the verb as removed
  and lists only `cancel`, so an adopter is not told the fault lies elsewhere
- **AND** the tests that verify this are `TestUserSignal_Validate` and `TestSignalTypeConstants`

#### Scenario: a cancel from a non-owner without cancel-any is refused before publication

- **GIVEN** a running loop owned by `user-a` and a requester `user-b` absent from the cancel-any list
- **WHEN** `user-b` asks to cancel that loop
- **THEN** the gate refuses it, nothing is published on the loop's signal subject, and the loop keeps running
- **AND** the test that verifies this is `TestIntegrationRefusedCancelPublishesNothingOnTheSubject`

### Requirement: The ungated seams are named, with the reason each is exempt

Every seam that accepts a loop token and does NOT pass through the gate MUST be listed here with its reason, so
that an ungated seam is a recorded decision rather than an omission a later reader has to rediscover:

- **Framework-published loop events** — loop-created, approval-pending, and terminal completion and failure
  events. These attach by a loop id the framework itself published on a stream, not by a caller-controlled
  field. There is no requester to check, and gating them would break completion routing and the
  approval-pending arrival buffer.
- **Outbound approval-pending events.** An event dispatch publishes, not a request it admits. Its token still
  carries the form check at its own payload boundary.
- **Read-projection wire types** for the loops view and for completion decoding. These decode framework-written
  records; they are not an intake of untrusted input.
- **The user-message payload's own validation.** The loop-token fields a user message carries are checked at the
  dispatch seam, not at decode, deliberately: a decode-time refusal has no submitter to answer and would
  reintroduce the silent drop this change removes.
- **The HTTP request body type for submissions.** It has no validation method; its loop-token fields are copied
  onto the user message and reach the gate at the seam that can answer the client synchronously.
- **Loop read (`GET`)** is gated for form and existence only; ownership is not consulted, per the ownership
  model above.

#### Scenario: a framework-published loop event is not owner-checked

- **GIVEN** a loop-created event and a terminal completion event for a loop with a recorded owner
- **WHEN** dispatch handles each
- **THEN** neither is refused for ownership, and terminal routing settles as it does today
- **AND** the test that verifies this is `TestFrameworkPublishedEventsAreNotOwnerChecked`

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.complete`, and `agent.failed` through their binding owner, and SHALL
NOT settle any of them before its durable effect has committed. `agent.created` and `agent.approval_pending` leave
this list because this change deletes their subscriptions and handlers, and there is no owner to settle an input
this component no longer consumes. Business handlers SHALL receive only an immutable owner-supplied work view and
SHALL return a typed semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, cancel signal, approval response,
and user-response publication has synchronous JetStream PubAck. The cancel signal SHALL travel its stream with
PubAck rather than as a core publication, so the published fact a classification reads names a durable effect
rather than a hope. Whether an unacknowledged publication retries or quarantines SHALL be decided by whether its
redelivery is effect-free, and that decision SHALL be recorded at the call site rather than taken by default. A
command SHALL NOT be retried when its target was resolved rather than named by the message and this component
either published a signal during that delivery or attempted one whose outcome it cannot account for. A refusal
raised while RESOLVING a command's target SHALL be published to the user and settled on that publication unless it
is transient: a redelivery re-reads the same authority and cannot change a nontransient answer, so retrying one
spends the source's redelivery budget and tells the user nothing. A failed
publish SHALL count as an unaccounted attempt unless the error PROVES nothing was stored — a refusal the client
returns before the bytes leave the process — and that set SHALL fail closed, so an error it does not recognize is
unaccounted rather than refused. Both the attempt and the published fact SHALL be recorded where this component's
own publication happens, never inferred from the command name or the response text, because a command that
published nothing is replayable no matter how its target was chosen. The recorder is internal to this component,
so a command handler an adopter registers cannot record a publication of its own: its failed response after a
durable publication retries exactly as it did before this change, and exporting the recorder is an addition a
later change owns. Terminal events SHALL retain their typed
ancestry and deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

The `user.message` subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; dispatch SHALL
NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout.

The first owner-fatal result in an owner family SHALL synchronously latch before the exact handle is drained, and
later fatal results in that family SHALL neither overwrite nor recount it. Existing Health SHALL report
`Healthy=false`. The `user.message` lane latches under the component-wide status `delivery ownership lost` — the
one latch the three lanes brought under settlement shared before this change retired two of them, and it stays
shared for whatever lane joins it next; the two terminal lanes keep the separate latches they already had, so their
loss alone keeps the narrower `terminal delivery ownership lost` status — it is the whole truth only while no other
lane has lost ownership. `LastError` SHALL carry every latched cause and the error count SHALL be the number of
owner families that lost ownership, so per-family aggregation is preserved rather than replaced. This adds no
metric family, public state, durable state, or communication path.

#### Scenario: Task publication succeeds but user response fails

- **WHEN** the TaskMessage receives PubAck
- **AND** the required user response does not receive PubAck
- **THEN** the delivery quarantines rather than retrying the UserMessage
- **AND** no second task is published; a redelivery recovers the committed task identity rather than minting
  another, and the in-process tracking that was the other half of the reason is gone with the tracker, so what
  keeps this arm from retrying is the submission counter it would move a second time

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

- **WHEN** a bare `/cancel` resolves its target from durable loop authority, publishes that loop's signal, and the
  required user response does not receive PubAck
- **THEN** the delivery quarantines, because the message does not carry the identity the delivery acted on
- **AND** the redelivery is not effect-free: it resolves afresh against a world this delivery changed rather than
  repeating what this delivery did
- **AND** resolution SHALL be scoped to the exact user and channel route, never widened to the user's other
  channels, so the terminal loop resolves to nothing rather than falling through to a loop the user never named

#### Scenario: A resolved cancel's signal publish fails without proving refusal

- **WHEN** a bare `/cancel` resolves its target from durable loop authority and its signal publish fails with an
  error that does not prove the broker stored nothing
- **THEN** the delivery quarantines rather than retrying, because the failure describes what the client
  experienced and not whether the signal was stored
- **AND** the hazard is the published one reached through an error: were it retried and the first attempt had
  stored, the redelivery would resolve afresh against a world this delivery had already changed rather than
  repeating what this delivery did
- **AND** the same failure on a target the message named retries, as does a failure the client proves is a refusal
  it made before publishing, because neither can have changed the world

#### Scenario: A command that resolved a target and published nothing

- **WHEN** a command whose target was resolved from durable loop authority publishes no signal — a read-only
  command, or a cancel that was refused, found no loop, or found one already settled — and its response does not
  receive PubAck
- **THEN** the delivery retries, because a command that did nothing can be replayed whatever its target was
- **AND** the lane is not latched, so later user messages are still admitted

#### Scenario: A command's target cannot be resolved

- **WHEN** a command that consumes a target arrives on the user-message stream naming none, and durable loop
  authority refuses nontransiently, as it does for a route matching more than one current loop
- **THEN** that refusal is published to the user and the delivery settles only on its PubAck
- **AND** a transient resolution failure retries instead, publishing nothing, because a redelivery is what answers it

#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its typed user error receives PubAck before the delivery is acknowledged, and a publication that fails
  is classified rather than swallowed
- **AND** no loop bookkeeping moves: the in-process tracker and the `active_loops` gauge this clause used to name
  are deleted by this change, and the obligation survives them as no durable loop record written and no
  submission counted for a message that was refused
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

### Requirement: An approval decision names the execution it answers

The HTTP approval endpoint SHALL require the execution identity the caller reviewed — the `execution_id` carried on
the `ApprovalPendingEvent` — in the request body, and SHALL refuse a body that omits it. A decision naming an
execution other than the one currently gated SHALL be refused as a conflict. Both refusals SHALL land before
anything is published, and SHALL leave the pending gate exactly as they found it. A successful submission SHALL
echo the execution identity it answered.

Without the field the endpoint approved whichever gate was pending when the request landed: a decision made about
one execution, retried after that call finished and the next one gated, was republished as an approval of a call
nobody had reviewed, and the loop's matcher accepted it because dispatch had stamped the current identity onto it.
A human approves one call, not "the next one".

An execution identity SHALL be compared only when the pending gate carries one, matching the loop's own matcher: a
gate with no identity has nothing to compare and SHALL remain answerable.

#### Scenario: A decision is retried after the gate it answered has moved on

- **WHEN** an approval body naming one execution arrives while a DIFFERENT execution is the pending gate
- **THEN** the request is refused as a conflict, naming the execution the caller asked about
- **AND** nothing is published and the pending gate is neither cleared nor re-pointed

#### Scenario: An approval body names no execution

- **WHEN** an approval body omits the execution identity, whatever the loop's state is
- **THEN** the request is refused as malformed, naming the missing field
- **AND** the pending gate is unchanged, because a required field is never defaulted from current state

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
AutoContinue. For a command, AutoContinue SHALL resolve a target only when the command declares it consumes one,
so a command that declares none — `/loops`, `/help` — does not inherit the route ambiguity that refuses
resolution. It inherits the view's readiness only where its own handler reads the view: `/loops` does and keeps
refusing one that is not caught up, `/help` reads no loop state and answers regardless.
`LoopTracker` and pending-approval process caches SHALL NOT exist. `/loops` and `/debug/state` SHALL preserve the
existing immutable `LoopInfo` JSON schema, including `execution_id` on the existing nested `PendingApprovalInfo`,
which SHALL come from observed pending authority. The existing optional `context_request_id` field SHALL remain
empty in the authority-backed projection, as accepted by the edge-gateway design; no historical notification lookup
SHALL reconstruct it. All other unrelated DTO fields and projection contracts SHALL remain unchanged.
`/debug/state` SHALL expose the view's caught-up readiness and current poison diagnostics rather than reporting a
false empty state.

Explicit LoopID approval, read, continuation, cancellation, terminal-route, and command-owner operations SHALL
exact-read and validate `AGENT_LOOPS/<LoopID>`. A partial, stale, watcher-lost, or relevant-poisoned projection SHALL
never be treated as empty.

An approval SHALL read the current durable record on every decision rather than a process-local pending cache, and
SHALL obtain from validated `PendingApproval` state both its CallID and the framework execution identity it echoes
onto the published `ApprovalResponse` — the loop authorises on that identity and refuses a response that omits it,
so the record is what makes the decision answerable. The RECORDED STATE SHALL decide before any property
of `PendingApproval` does: a record that is readable but not `awaiting_approval` SHALL refuse as conflict regardless
of whether it still carries a pending block. An unreadable or invalid record, and an `awaiting_approval` record
whose pending CallID or ExecutionID is empty, SHALL refuse as unavailable. Admission and publication SHALL NOT
mutate loop authority, so a failed publish leaves the decision retryable.

An unavailable answer SHALL carry a fixed client-facing phrase. A refusal body SHALL NOT contain framework type or
method names; the wrapped detail belongs in the log line correlated by request id.

#### Scenario: Approval follows replacement

- **GIVEN** exact current state is awaiting approval
- **WHEN** an authorized approval names its canonical LoopID
- **THEN** dispatch obtains CallID from validated `PendingApproval` state read at decision time
- **AND** echoes that record's execution identity onto the response it publishes
- **AND** requires no earlier approval-pending event

#### Scenario: Pending output carries observed identity after replacement

- **GIVEN** exact validated current authority contains a pending approval
- **WHEN** the existing pending HTTP projection is read after dispatch replacement
- **THEN** it carries the pending record's ExecutionID as `execution_id`
- **AND** the caller need not compute identity or have received the original event

#### Scenario: Current authority refuses a decision the memory cache would have accepted

- **GIVEN** the durable record is executing, terminal, or incoherent
- **WHEN** an approval decision arrives
- **THEN** dispatch refuses from the record alone, as conflict or unavailable
- **AND** no process-local cache can override or stand in for that record

#### Scenario: A terminal names a loop id that cannot be one

- **WHEN** a complete or failed event carries a LoopID that is not a canonical framework loop token
- **THEN** dispatch refuses it as permanently malformed routing without reading `AGENT_LOOPS`
- **AND** publishes no user response

#### Scenario: A loop that left awaiting-approval still carries its pending block

- **GIVEN** a record whose state is cancelled, failed or executing and whose `PendingApproval` was never cleared
- **WHEN** an approval decision names it
- **THEN** dispatch refuses as conflict, because the state is permanent and the caller must not retry it
- **AND** the unavailable answer is reserved for a record that could not be read or did not validate

#### Scenario: An unavailable answer names no framework internals

- **GIVEN** the shared loop view is not yet available
- **WHEN** a message submission or a loop listing is refused
- **THEN** the response body is a fixed retryable phrase
- **AND** it contains no framework type or method name, which remain in the correlated log line

#### Scenario: A failed publish leaves the decision retryable

- **GIVEN** a validated pending approval
- **WHEN** publishing the decision fails
- **THEN** the durable record is byte-identical to what it was before the request
- **AND** the caller may submit the same decision again

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

#### Scenario: A command that consumes no target runs while resolution would refuse

- **GIVEN** a route whose current loops are ambiguous
- **WHEN** a command that declares no target arrives on either command lane
- **THEN** it runs and answers, so `/loops` still lists the loops whose ambiguity refuses the others
- **AND** a command that does declare a target still refuses
- **AND** readiness is not in scope of the declaration: `/help`, whose handler reads no loop state, answers while
  the view is not caught up, and `/loops`, whose handler reads the view, still refuses one that is not

### Requirement: Loop existence and ownership come from durable authority alone

Dispatch SHALL decide a loop's existence, ownership and state ONLY from the exact `AGENT_LOOPS/<LoopID>` record read
at decision time. No process-local observation, prior successful admission, or cached fact SHALL establish, extend or
substitute for that record. This requirement replaces "Loop existence and ownership are merged facts, never process
memory alone": there is no second source left to merge, because the tracker that was the other half is deleted.

The durable bucket name SHALL be OBSERVED from the component's declared KV read port through the existing port
projection. No reader SHALL carry a bucket-name default of its own.

Degradation SHALL stay explicit and SHALL NOT collapse into one another. Key absence is the not-found refusal. Any
other read failure, and any record that is decodable but not valid current authority — a wrong key/ID pairing, a
non-canonical loop token, a missing or unknown state, a non-positive iteration budget — SHALL refuse as transient and
unreadable: the request is answerable later and SHALL NOT be admitted on an unread or invalid record. Invalid
authority SHALL be refused BEFORE ownership is considered, so a refusal never discloses whether the requester owns
the loop.

A seam that reports a loop's state SHALL report the state it read; it SHALL NOT render one fixed word over
executing, paused and awaiting-approval. Terminal authority SHALL refuse continuation while remaining readable,
cancellable and approvable at the gate. Reading authority SHALL NOT mutate it.

#### Scenario: a continuation after a process replacement is admitted from the durable record

- **GIVEN** a loop created before dispatch was replaced, whose `AGENT_LOOPS` record names its owner and route
- **AND** a replacement process with no memory of that loop
- **WHEN** that loop's owner continues it by `reply_to`
- **THEN** the request is admitted with the record's owner, route and state
- **AND** the test that verifies this is `TestContinuationAfterReplacementIsAdmittedFromDurableRecord`

#### Scenario: a loop this process admitted before, whose record is now gone, is not found

- **GIVEN** a loop this process successfully admitted while its `AGENT_LOOPS` record existed
- **WHEN** the record is gone and the same request arrives again
- **THEN** the refusal is not-found, and the earlier observation establishes nothing
- **AND** the test that verifies this is `TestPreviouslyObservedLoopWithoutDurableRecordIsRefused`

#### Scenario: an unreadable durable record refuses as transient

- **GIVEN** an `AGENT_LOOPS` read that fails with anything other than key absence
- **WHEN** a request names a loop
- **THEN** the refusal is classified transient and unreadable, never not-found, and no loop is created for the token
- **AND** the test that verifies this is `TestUnreadableDurableRecordRefusesTransient`

#### Scenario: a prior admission is no fallback for a later read failure

- **GIVEN** a request that was admitted while the record was readable
- **WHEN** the same request arrives after the read starts failing
- **THEN** it refuses as transient and unreadable, carrying no ownership from the earlier admission
- **AND** the test that verifies this is `TestPriorAdmissionDoesNotBypassADurableReadFailure`

#### Scenario: only the current record establishes ownership

- **GIVEN** a loop whose record named one owner when a request was last admitted
- **WHEN** the record now names a different owner
- **THEN** the former owner is refused as not-owner and the current owner is admitted
- **AND** the test that verifies this is `TestCurrentOwnerReplacesPreviouslyObservedOwner`

#### Scenario: terminal authority refuses continuation and stays readable

- **GIVEN** a record in complete, failed or cancelled state
- **WHEN** its owner continues it
- **THEN** the refusal is terminal
- **AND** read, cancel and approve still resolve that record and report its terminal state
- **AND** the test that verifies this is `TestGateTerminalAuthorityRefusesContinuation`

#### Scenario: a read reports the exact state and mutates nothing

- **WHEN** a status read resolves a record in any state
- **THEN** the reported state equals the recorded state and the record is byte-identical afterwards
- **AND** the tests that verify this are `TestGateReportsExactCurrentStateWithoutMutatingAuthority` and
  `TestStatusReportsTheRecordedStateNotAFabricatedRunning`

#### Scenario: invalid authority refuses before ownership is considered

- **GIVEN** a record that is absent, keyed under another identity, non-canonically identified, stateless,
  unknown-stated or without a positive iteration budget
- **WHEN** a stranger names that loop
- **THEN** the refusal is unreadable and its message does not say the requester does not own the loop
- **AND** the tests that verify this are `TestGateRefusesInvalidCurrentAuthorityBeforeOwnership` and
  `TestLoopAdmissionValidatesPersistedAuthority`

#### Scenario: every read seam answers from the record after replacement

- **GIVEN** a replacement process with no memory of any loop
- **WHEN** the read seams are asked about a loop whose record exists
- **THEN** each answers from that record rather than reporting absence
- **AND** the test that verifies this is `TestReadSeamsAnswerFromTheDurableRecordAfterReplacement`

#### Scenario: /status reports iteration progress and age from the record

- **WHEN** `/status` resolves a loop whose record carries its iteration count, budget and timestamps
- **THEN** the answer names the iteration progress and the loop's age, as it did before the tracker was removed
- **AND** neither field is reconstructed from process memory

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

### Requirement: The task submission counter is at-least-once under redelivery
`tasks_submitted_total` SHALL be treated as an at-least-once count of task submissions: a redelivered
`UserMessage` whose task already committed SHALL increment the counter again, and SHALL remain otherwise
idempotent — it reuses the retained LoopID and mints no second LOGICAL task: the retained task is republished
under the same `TaskID`, so the stream can hold more than one physical copy of one task identity.

The counter is a submission-attempt signal, not a distinct-task count. Nothing in dispatch or in the loop
suppresses the second increment, and no arm is added to make it exactly-once.

#### Scenario: A redelivered task submission counts again

- **GIVEN** a `UserMessage` whose task committed with a retained LoopID
- **WHEN** the source redelivers that `UserMessage`
- **THEN** `tasks_submitted_total` increments a second time
- **AND** the retained LoopID is reused, the republished task carries the same `TaskID` rather than a second task
  identity, and no loop is created

