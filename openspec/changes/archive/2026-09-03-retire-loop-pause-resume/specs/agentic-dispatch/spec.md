## MODIFIED Requirements

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

The merged facts MUST carry the loop's recorded STATE, not only whether it has settled, and a seam that reports
a loop's state to a caller MUST report the state that was read. "Not settled" covers executing and awaiting
approval; a seam that renders one fixed word for both tells a user whose loop is waiting on their own approval
to go on waiting for the agent — a fabricated fact, and worse than the not-found this seam answered before
existence was merged. A merged observation that carries no state MUST say so rather than name one. When both
sources report a state they reconcile on the same fail-closed rule terminality uses: a settled observation in
either source wins.

A durable record that decodes but does not validate as a loop entity MUST be refused by the reader under the
same permanent classification a malformed record receives, and MUST NOT reach the merge or any seam. A state
outside the loop state vocabulary is the case this change adds: it never becomes valid, so retrying it is not
an answer, and reporting it would republish a state the framework no longer defines. The refusal is the whole
entity's, not one field's — every production record on this key is a marshalled loop entity, so a record that
fails validation is a record no seam should reason about.

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

#### Scenario: a status read after a process replacement reports the recorded state

- **GIVEN** an empty loop tracker and an `AGENT_LOOPS` record whose loop is `awaiting_approval`
- **WHEN** its owner asks for that loop's status
- **THEN** the answer names `awaiting_approval` rather than a fixed "running", so the user learns the loop is
  waiting on them
- **AND** a record carrying no state is reported as unknown, never as a state nobody read
- **AND** the tests that verify this are `TestStatusReportsTheRecordedStateNotAFabricatedRunning` and
  `TestMergeLoopStatePrefersSettledThenTheTracker`

#### Scenario: conflicting owners across the two sources are refused

- **GIVEN** a tracker entry and an `AGENT_LOOPS` record for the same token whose recorded owners differ
- **WHEN** the gate admits a request naming it
- **THEN** the request is refused with the conflict reason rather than one source being silently preferred
- **AND** the test that verifies this is `TestConflictingOwnersAcrossSourcesAreRefused`

#### Scenario: a persisted record whose state is outside the vocabulary is refused permanently

- **GIVEN** an `AGENT_LOOPS` record written before the paused state was removed, carrying `"state":"paused"`
- **WHEN** the dispatch reader loads it
- **THEN** it is refused under the reader's permanent classification — the same one a malformed record
  receives — and no new class is invented for it
- **AND** the record does not reach the merge, `/status`, or any other seam
- **AND** a record whose state IS in the vocabulary is still returned by the same reader, so the refusal is the
  state's and not the path's
- **AND** the test that verifies this is `TestIntegrationPersistedInvalidStateIsPermanent`
