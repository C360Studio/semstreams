# agentic-dispatch Delta

## ADDED Requirements

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
preserve the existing immutable `LoopInfo` JSON schema, including `execution_id` on the existing nested
`PendingApprovalInfo`, which SHALL come from observed pending authority. The existing optional
`context_request_id` field SHALL remain empty in the authority-backed projection, as accepted by the edge-gateway
design; no historical notification lookup SHALL reconstruct it. All other unrelated DTO fields and projection
contracts SHALL remain unchanged.
`/debug/state` SHALL expose the view's caught-up readiness
and current poison diagnostics rather than reporting a false empty state.

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

## REMOVED Requirements

### Requirement: Loop existence and ownership are merged facts, never process memory alone

**Reason**: The requirement mandates deciding from "the union of the process-local loop tracker and the durable
`AGENT_LOOPS` record" and states that "A tracker hit is sufficient to admit even when the durable read fails
transiently". This change deletes `LoopTracker`, so there is no second source: `lookupLoop` reads the record and
nothing else. FOUR tests the requirement rests on are deleted with it —
`TestLiveLoopWithoutDurableRecordIsAdmitted`, the merge-preference test behind
`TestMergeLoopStatePrefersSettledThenTheTracker`, `TestConflictingOwnersAcrossSourcesAreRefused`, whose scenario
"conflicting owners across the two sources" cannot arise when there is one source (the surviving conflict, a terminal
event disagreeing with the record, was never this requirement's: it is `mergeRouteField` at
`terminal_settlement.go:48`, specified in `openspec/specs/agentic-terminal-events/spec.md` and untouched here), and
`TestIntegrationInvalidPersistedRecordIsToleratedOnlyBecauseTheTrackerAnswers`, added under the requirement's sixth
scenario by the paused-state removal: its whole assertion is that a defective record is TOLERATED because the tracker
answers instead, `facts.Tracked` true and `facts.Persisted` false. With no tracker there is nothing to answer, and
the record's refusal is the whole outcome — which is what `TestIntegrationPersistedInvalidStateIsPermanent`, the
surviving half of that pair, already asserts. A fifth,
`TestPreviouslyObservedLoopWithoutDurableRecordIsRefused`, survives and now asserts the NEGATION of the
scenario "a live loop with no durable record is admitted from the tracker". Keeping the requirement as current truth
while the code refutes it is the failure this block exists to prevent; the heading itself is false once there are no
merged facts, so it is removed rather than reworded.

**Migration**: "Loop existence and ownership come from durable authority alone" above carries every obligation that
survives — port-observed bucket name, explicit not-found vs transient degradation, the recorded state reported as
read, and invalid authority refused before ownership — and adds the ones the union hid: a prior admission is no
fallback, and only the current record establishes ownership. The admission that used to succeed from a tracker hit
with no durable record now refuses as not-found; a producer relying on best-effort persistence lagging its loop must
write the record before the loop is continuable. Adopter-facing consequences are in
`docs/operations/migration-beta162-to-beta163.md`.

## MODIFIED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner, and SHALL NOT settle any of them before its durable effect has
committed. Business handlers SHALL receive only an immutable owner-supplied work view and SHALL return a typed
semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, cancel signal, approval response,
and user-response publication has synchronous JetStream PubAck. The cancel signal SHALL travel its stream with
PubAck rather than as a core publication, so the published fact a classification reads names a durable effect
rather than a hope. Whether an unacknowledged publication retries or quarantines SHALL be decided by whether its
redelivery is effect-free, and that decision SHALL be recorded at the call site rather than taken by default. A
command SHALL NOT be retried when both of two facts hold: this component published a signal during the delivery,
and its target was resolved rather than named by the message. That
published fact SHALL be recorded where this component's own publication happens, never inferred from the command
name or the response text, because a command that published nothing is replayable no matter how its target was
chosen. The recorder is internal to this component, so a command handler an adopter registers cannot record a
publication of its own: its failed response after a durable publication retries exactly as it did before this
change, and exporting the recorder is an addition a later change owns. Terminal events SHALL retain their typed
ancestry and deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

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

#### Scenario: A command that resolved a target and published nothing

- **WHEN** a command whose target was resolved from durable loop authority publishes no signal — a read-only
  command, or a cancel that was refused, found no loop, or found one already settled — and its response does not
  receive PubAck
- **THEN** the delivery retries, because a command that did nothing can be replayed whatever its target was
- **AND** the lane is not latched, so later user messages are still admitted

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
