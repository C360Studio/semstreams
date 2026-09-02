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

## ADDED Requirements

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

### Requirement: Loop existence and ownership are merged facts, never process memory alone

The gate MUST decide existence and ownership from the union of the process-local loop tracker and the durable
`AGENT_LOOPS` record, because neither source alone is authority: the tracker is empty after a process
replacement, and the durable record may be absent for a live loop because persisting it is best-effort. A loop
observed in EITHER source exists. When both are observed, their owner and route fields MUST be reconciled by
the same merge rule the terminal-settlement path already uses — a conflicting non-empty value is a refusal, not
a silent preference for one source.

The durable bucket name MUST be OBSERVED from the component's declared KV read port through the existing port
projection. No reader may carry a bucket-name default of its own.

Degradation is explicit. A tracker hit is sufficient to admit even when the durable read fails transiently. A
tracker miss plus a durable read that fails for any reason other than key absence MUST refuse as transient —
the request is answerable later and MUST NOT be admitted on an unread record. A tracker miss plus a durable
read that reports key absence is the not-found refusal.

#### Scenario: a continuation after a process replacement is admitted from the durable record

- **GIVEN** a loop created before dispatch was replaced, whose `AGENT_LOOPS` record names its owner
- **AND** an empty loop tracker in the replacement process
- **WHEN** that loop's owner continues it by `reply_to`
- **THEN** the request is admitted, and the loop is continued rather than silently forked under the same token
- **AND** the test that verifies this is `TestContinuationAfterReplacementIsAdmittedFromDurableRecord`

#### Scenario: a live loop with no durable record is admitted from the tracker

- **GIVEN** a loop tracked in process whose best-effort `AGENT_LOOPS` write has not landed
- **WHEN** its owner continues it
- **THEN** the request is admitted from the tracker without requiring the durable record
- **AND** the test that verifies this is `TestLiveLoopWithoutDurableRecordIsAdmitted`

#### Scenario: an unreadable durable record with no tracker entry refuses as transient

- **GIVEN** an empty tracker and an `AGENT_LOOPS` read that fails with anything other than key absence
- **WHEN** a request names a loop
- **THEN** the refusal is classified transient, not not-found, and no loop is created for the token
- **AND** the test that verifies this is `TestUnreadableDurableRecordRefusesTransient`

#### Scenario: conflicting owners across the two sources are refused

- **GIVEN** a tracker entry and an `AGENT_LOOPS` record for the same token whose recorded owners differ
- **WHEN** the gate admits a request naming it
- **THEN** the request is refused with the conflict reason rather than one source being silently preferred
- **AND** the test that verifies this is `TestConflictingOwnersAcrossSourcesAreRefused`

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
  loop field no code reads. Deleting the endpoint therefore removes an adopter-facing surface that promised an
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
- **AND** a caller that wants to cancel a loop uses `POST /message` with `/cancel <loop_id>`

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
