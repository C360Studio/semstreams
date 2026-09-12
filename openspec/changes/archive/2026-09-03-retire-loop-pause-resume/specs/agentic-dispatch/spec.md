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
