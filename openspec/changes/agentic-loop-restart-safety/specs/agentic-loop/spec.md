## ADDED Requirements

### Requirement: All six loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or work-owning no-heartbeat
adapter.

Each non-heartbeat physical subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; agentic-loop SHALL
NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout. A physical
subscription SHALL move to the existing heartbeat owner only after measured legitimate work can exceed its
configured acknowledgement interval. Cancellation-ignoring or non-returning work SHALL fail lifecycle review.

Decode, correlation, KV, Store, transition, and required publication failures SHALL NOT become successful callback
completion. ACK means the lane-specific durable transition or defined refusal and every required PubAck completed;
Retry means the lane's declared correlation and durable evidence make re-execution safe; Terminate means permanently
invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure
prevents a safe choice.

Cancel SHALL remain the entire durable UserSignal vocabulary. ApprovalResponse SHALL remain a separate input.
`LoopStatePaused` and wire value `paused` SHALL be removed from the valid state vocabulary, exported transition
acceptance, schema, examples, and documentation. Persisted `state:"paused"` SHALL be refused as invalid. No
compatibility shim, alias, reserved enum, migration, checkpoint, supervisor, or workflow state machine SHALL be
added. `ResponseAction.Signal` and `ClassifiedIntent.SignalType` SHALL remain outside ownership of durable
`agent.signal.*` settlement.

The first fatal result from any loop delivery owner SHALL synchronously latch into the component's existing health
surface before owner-stop observation drains the exact handle. Owner loss SHALL take status precedence over
trajectory-audit degradation without reclassifying either condition: health SHALL report `Healthy=false`, status
`delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the existing error count.
Later fatal results SHALL neither overwrite nor recount the first cause. This adds no metric family, public state,
durable state, or communication path.

#### Scenario: Paused state is refused everywhere

- **GIVEN** an exported transition input, decoded persisted loop, or schema-validated document carries `paused`
- **WHEN** state validation runs
- **THEN** it is refused as an invalid state
- **AND** no shim, alias, reserved enum, or migration converts it to another state

#### Scenario: Operational quiesce is lifecycle shutdown

- **GIVEN** an operator needs the component to quiesce
- **WHEN** Stop is invoked
- **THEN** admission stops, exact handles drain, owned work cooperatively cancels, and all work joins
- **AND** no arbitrary execution pause or resume API is used

#### Scenario: Future suspension requires a new contract

- **WHEN** suspend-at-next-durable-boundary behavior is proposed
- **THEN** it requires a new evidence-backed capability contract and owner ruling
- **AND** this capability supplies no reserved state or compatibility API for it

#### Scenario: Required output publication fails

- **WHEN** a handler computes a transition
- **AND** a required publication does not receive PubAck
- **THEN** the source is not positively acknowledged
- **AND** its disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Duplicate is proven applied

- **WHEN** a redelivered input's exact identity is present in a committed later request or terminal outcome
- **THEN** agentic-loop acknowledges the duplicate without repeating non-idempotent work

#### Scenario: Missing process correlation is not proof of staleness

- **WHEN** a delivery arrives after replacement and its process map entry is absent
- **THEN** agentic-loop performs exact durable read-through
- **AND** does not log-and-drop or ACK solely because memory is empty

#### Scenario: Required correlation conflicts

- **WHEN** one stable task, request, or tool-execution identity is observed with conflicting required correlation
- **THEN** agentic-loop quarantines and stops the exact owner
- **AND** does not apply either mapping by preference

#### Scenario: Unknown control signal is permanent

- **WHEN** a registered UserSignal carries any value other than cancel
- **THEN** validation or handling terminates it as permanently invalid
- **AND** no warning-only return becomes ACK

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the terminal event receives PubAck before source ACK

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** handler recovery returns a non-nil fatal-classified error
- **AND** the production delivery callback returns Quarantine without persistence or settlement
- **AND** the exact owner stops and drains
- **AND** the panic is never rewritten to nil

#### Scenario: Loop delivery metadata is unavailable

- **WHEN** a loop settlement adapter cannot observe native delivery metadata
- **THEN** it invokes no loop work and makes no heartbeat or settlement call
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** drains the exact consume handle
- **AND** loop health becomes negative with the exact cause and one error-count increment

#### Scenario: Verdict arrives without a waiter

- **WHEN** an exact verdict arrives after replacement with no waiter
- **THEN** its retained identity remains recoverable for response replay
- **AND** missing or full process channel is not completed log-and-drop

#### Scenario: Loop business work reaches its own deadline

- **WHEN** a delivery-owned loop operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

### Requirement: Loop recovery is lane-specific and read-through

Agentic-loop SHALL load the exact `LoopEntity` identified by an incoming delivery and reconstruct only the material
required for that delivery. Ordinary recovery SHALL NOT enumerate or replay the full AGENT stream.

#### Scenario: Model response arrives after replacement

- **WHEN** AgentResponse carries a structured RequestID
- **THEN** agentic-loop resolves LoopID and loads current loop state
- **AND** reconstructs request context from committed AgentRequest material
- **AND** validates the response against the active or already-applied turn

#### Scenario: Tool result arrives after replacement

- **WHEN** ToolResult carries RequestID and execution identity
- **THEN** agentic-loop reads the originating AgentResponse
- **AND** reconstructs the ordered batch from response and accumulated durable results
- **AND** publishes the next required output at least once and waits for PubAck before source ACK

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

### Requirement: Loop task, request, and tool work use only required correlation

For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL
validate their mapping and SHALL reject a conflicting mapping. Provider work SHALL carry a stable RequestID. Tool
work SHALL carry the framework execution identity derived from RequestID, provider CallID, and positive call ordinal.

Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.
Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT
be treated as permanent identity or proof of publication. Exact retained reads SHALL exist only at named boundaries
where they prevent repeating non-repeatable work or prove a lane-specific durable transition already applied.

#### Scenario: Task mapping is stable across redelivery

- **WHEN** a task redelivers after its LoopEntity or initial request committed
- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping
- **AND** any required ordinary publication may repeat and receives PubAck before source ACK

#### Scenario: Request or execution correlation conflicts

- **WHEN** one RequestID or framework execution identity names conflicting required correlation
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either mapping

#### Scenario: Ordinary required publication repeats

- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat
- **THEN** the duplicate is an admitted at-least-once outcome
- **AND** consumers use the lane's required correlation and durable transition rules

### Requirement: Approval continuation after replacement is exact and evidence-bounded

After an approval-required `ToolResult` settles, agentic-loop SHALL persist both awaiting-approval state and the
result in current `LoopEntity` before source ACK. After replacement, approve, modify, reject, and timeout SHALL
reconstruct or prove exactly one next transition from validated current state and admitted retained evidence.

Reconstruction SHALL use current `LoopEntity`, latest exact `agent.request.<LoopID>`, and exact
`agent.response.<RequestID>`. It SHALL perform no stream scan and no `ToolResult` lookup by provider CallID.

Provider CallID SHALL be interpreted only within the current RequestID. An older response carrying the same CallID
SHALL not participate.

A transient or unresolved read SHALL Retry. Confirmed retained absence SHALL durably fail
`continuation_unavailable`. Malformed or identity-conflicting evidence SHALL Quarantine. Durable applied-state proof
SHALL permit settlement; otherwise the required continuation publication MAY repeat and SHALL receive PubAck before
source ACK.

The storage mechanism remains gated by the accepted replacement proof. If retained exact evidence satisfies every
branch, no continuation Store is required. If it does not, the approved content-addressed ObjectStore design remains
the fallback after owner ruling.

#### Scenario: Same CallID exists under two requests

- **GIVEN** an old and current `AgentResponse` share CallID but have different RequestIDs and arguments
- **WHEN** continuation reconstructs
- **THEN** only the response named by the current `AgentRequest` participates
- **AND** the older response cannot change or satisfy the result

#### Scenario: Exact approval continuation matches

- **WHEN** current state and retained request/response evidence validate and agree
- **THEN** approve or modify publishes tool work at least once or proves its durable transition already applied
- **AND** reject or timeout publishes a rejection transition at least once or proves it already applied
- **AND** `PendingApproval` clears only after required PubAck or durable applied-state proof

#### Scenario: Required retained evidence is confirmed absent

- **WHEN** observed retention says required evidence should remain but its exact subject is absent
- **THEN** the loop durably fails with `continuation_unavailable`

#### Scenario: Approval evidence conflicts

- **WHEN** an identity, call, name, argument, or durable applied-state fact conflicts
- **THEN** the delivery quarantines
- **AND** no pending state clears

### Requirement: Approval lifetime is bounded by loop-state authority

When approval gating is enabled, timeout SHALL be finite, nonzero, and within observed AGENT_LOOPS TTL after the
framework safety margin. The default SHALL be 12 hours. Zero, empty, indefinite, and over-retention values SHALL fail
before dependent loop work.

#### Scenario: Timeout is omitted

- **WHEN** approval gating has no supplied timeout
- **THEN** agentic-loop uses 12 hours and validates it against observed loop-state authority

#### Scenario: Timeout is indefinite or too long

- **WHEN** timeout is zero, empty, indefinite, or exceeds observed retention after margin
- **THEN** startup fails with exact observed and required values

### Requirement: Approval deadlines are reconstructed narrowly

When timeout is configured, agentic-loop SHALL reconstruct awaiting-approval deadlines from current AGENT_LOOPS facts
after replacement. This owns approval timers only and SHALL NOT become a generic supervisor.

#### Scenario: Timer owner is replaced

- **WHEN** replacement occurs while a persisted approval deadline remains active
- **THEN** agentic-loop reconstructs that deadline without enumerating unrelated work for replay

### Requirement: Delivery work joins before settlement

Every goroutine spawned by delivery work SHALL join before its callback returns. A deadline cancels the operation but
SHALL NOT authorize return while work remains live.

#### Scenario: Delivery work exceeds its budget

- **WHEN** bounded work reaches its deadline
- **THEN** the owner cancels and joins before callback return

#### Scenario: Terminal approval rejection reaches a bounded graph write

- **WHEN** an approval rejection produces a terminal result and its bounded graph write reaches cancellation
- **THEN** graph-write work observes the delivery-derived context and joins before the callback returns
- **AND** the approval source is not settled while that work remains live

### Requirement: Loop shutdown closes every delivery owner

Agentic-loop shutdown SHALL stop admission, drain every task, response, tool-result, cancel, approval-response, and
verdict consume handle, await each handle's exact `Closed` signal, then cancel and join delivery workers, loop-state
observers, and the approval sweeper. Shutdown SHALL NOT return while any callback can settle, publish, or mutate loop
authority.

#### Scenario: Shutdown races all owner classes

- **WHEN** loop Stop begins while heartbeat and fast-owner callbacks and the approval sweeper are active
- **THEN** admission stops, every exact handle drains and closes, and every worker, observer, and sweeper joins
- **AND** Stop returns only after no later ACK, publication, or loop-state mutation is possible

### Requirement: Restart-safe replay observes and admits local stream bounds

Each recovery-dependent model, dispatch, governance, and loop owner SHALL invoke pure internal
`agentstreamadmission.ObserveAndValidate` after resolving its own PortFacts and before its own first dependent
allocation. Stream identity and requirement SHALL derive only from that component's resolved facts and local typed
AckWait, BackOff, MaxDeliver, maximum work/replay need, and PubAck dependency. No owner SHALL read another config,
shared maxima, factory names, or raw JSON. Dispatch SHALL admit its AGENT outputs before USER intake. Non-agentic
components SHALL perform zero lookup.

Admission SHALL require observed DiscardNew, sufficient MaxAge, and no earlier message bound. Refusal SHALL be typed
`agent_stream_replay_inadmissible`, name observed/required values, leave only the affected closure not ready, and
allocate or positively settle nothing. It SHALL mutate no stream and persist no state. Approval lifetime is excluded
and belongs only to loop-state acquisition.

#### Scenario: Capacity policy discards old evidence

- **WHEN** an affected resolved stream uses DiscardOld
- **THEN** that closure does not start and readiness names capacity-eviction risk

#### Scenario: Capacity is full under admitted policy

- **WHEN** DiscardNew refuses required publication
- **THEN** the producer returns Retry and retains its source without core-NATS fallback

#### Scenario: Concurrent components reject before allocation

- **WHEN** model, dispatch, governance, and loop start concurrently against inadmissible resolved streams
- **THEN** each affected closure remains not ready with zero dependent allocation or positive settlement
- **AND** queued USER remains unconsumed

#### Scenario: Non-agentic and mixed-stream components are isolated

- **WHEN** composition contains a non-agentic component and agentic stream overrides
- **THEN** the non-agentic component performs zero lookup and can start
- **AND** each agentic owner observes only its own resolved stream

### Requirement: Loop-state authority is acquired and observed before loop work

Agentic-loop SHALL resolve the bucket from its admitted `loops` KV-write port and call internal
`loopbucket.AcquireOwner`. The helper SHALL get first, create only for typed `jetstream.ErrBucketNotFound`, return all
other lookup errors without creation, and on typed `jetstream.ErrBucketExists` perform exactly one get. Creation SHALL
declare History 10, TTL 24h, and non-binding MaxBytes. The helper SHALL then observe actual policy and publish the
handle or start dependent consumers/sweeper only when History is exactly 10, TTL exactly 24h, and MaxBytes nonbinding.
It SHALL refuse retained drift without reconciliation and validate approval timeout only against this authority.

#### Scenario: Two owners race to create a fresh bucket

- **WHEN** two processes acquire the same absent bucket with matching declaration
- **THEN** one create wins and the other gets the existing bucket
- **AND** both observe matching actual policy before dependent work

#### Scenario: Retained or race-winning policy drift exists

- **WHEN** actual History, TTL, or MaxBytes differs
- **THEN** startup refuses without updating the bucket
- **AND** publishes no handle and allocates no dependent work

#### Scenario: Lookup fails for a reason other than absence

- **WHEN** initial lookup returns permission, timeout, transport, or another non-not-found error
- **THEN** acquisition returns it and calls CreateKeyValue zero times

#### Scenario: Concurrent create wins between lookup and create

- **WHEN** CreateKeyValue returns typed ErrBucketExists
- **THEN** acquisition performs exactly one KeyValue get and validates the winner

### Requirement: Long-running loop heartbeat policy is valid before acquisition

Task, response, and tool-result consumers SHALL default to heartbeat 15s against BackOff `[30s,2m]`. They SHALL
validate the exact acquisition config before consumer allocation; heartbeat SHALL be no greater than half the
shortest positive BackOff. MaxDeliver SHALL be at least the number of BackOff entries, so the fixed two-entry BackOff
requires MaxDeliver at least 2. Omitted or zero MaxDeliver SHALL default to 2. An explicit value below 2 SHALL be
refused before consumer allocation; the owner SHALL NOT truncate BackOff or admit a single-delivery posture.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s,2m]`
- **THEN** it returns a typed error naming the values and 15s ceiling
- **AND** allocates no consumer

#### Scenario: Single delivery is refused before allocation

- **WHEN** setup observes MaxDeliver 1 with BackOff `[30s,2m]`
- **THEN** it returns a typed policy error naming observed 1 and required minimum 2
- **AND** allocates no consumer

#### Scenario: Minimum valid delivery count reaches acquisition

- **WHEN** setup observes MaxDeliver 2, heartbeat 15s, and BackOff `[30s,2m]`
- **THEN** heartbeat and delivery-count validation pass
- **AND** setup may allocate the consumer with the unchanged two-entry BackOff

## MODIFIED Requirements

### Requirement: Per-loop in-process state is released at terminal, through the one release point

Every per-loop map the loop manager holds MUST be released when a loop reaches a terminal state. The release MUST
happen at the component's existing single terminal-release point after the loop's terminal observation, terminal
graph write, durable loop-state transition, and required terminal publication have completed. It MUST remain
idempotent and MUST release the loop entity, context manager, pending-tool set, queued tool calls, cached tool
definitions, tool choice, metadata, request timeout and response format, task prompt, truncation-retry counter,
trajectory step aggregate, and observed-audit-loss marker.

Release changes no durable authority. The exact `AGENT_LOOPS` record and operation-specific committed outputs remain
readable without process maps. Approval-timeout sweeping remains limited to nonterminal awaiting-approval records.
Direct create/attach refusal for a settled token remains owned by durable admission; process memory is defense in
depth only.

A late approval response, tool result, or model response MUST NOT be positively settled merely because process state
is absent or the loop is terminal. The lane owner MUST read the exact durable loop state and use only its declared
lane-specific evidence to prove that input already applied. Durable applied-state proof permits the owner's typed
already-applied terminal outcome. An unreadable authority or unresolved absence returns Retry. A malformed input,
required-correlation conflict, impossible transition, or contradictory durable state returns Quarantine. There is no
unconditional quiet settled-drop.

#### Scenario: a completed loop's per-loop state is released

- **GIVEN** a loop that has run several iterations with a populated conversation, cached tool definitions, and a
  task prompt
- **WHEN** it reaches a terminal state and its terminal observation, graph write, durable state, and publication
  have returned
- **THEN** every per-loop entry the loop manager held for that token is gone
- **AND** the tests that verify this are `TestTerminalReleaseClearsEveryPerLoopMap` and
  `TestTerminalReleaseIsIdempotent`

#### Scenario: releasing does not run before the terminal readers have finished

- **GIVEN** a loop reaching a terminal state
- **WHEN** its terminal trajectory observation, terminal graph write, durable persistence, and terminal publication
  run
- **THEN** each observes the loop entity it needs, and release happens after all of them
- **AND** the test that verifies this is `TestTerminalReleaseHappensAfterTerminalReaders`

#### Scenario: a late approval response has durable applied proof

- **GIVEN** a settled loop whose process state has been released
- **AND** the approval lane's declared durable state proves the same approval response was applied
- **WHEN** that response is redelivered
- **THEN** the approval owner returns its typed already-applied outcome and positively settles the source
- **AND** the same proof rule applies to late tool and model responses
- **AND** the tests that verify this are `TestLateApprovalResponseRequiresAppliedProof`,
  `TestLateToolResultRequiresAppliedProof`, and `TestLateModelResponseRequiresAppliedProof`

#### Scenario: a late response lacks applied proof

- **GIVEN** process state is absent and durable state is terminal
- **WHEN** exact required state or output evidence is absent or transiently unreadable
- **THEN** the owner returns Retry without positive settlement
- **AND** process absence or terminal state alone is not treated as proof

#### Scenario: a late response conflicts with required correlation

- **GIVEN** the expected lane correlation names conflicting durable state or an impossible transition
- **WHEN** a late approval, tool, or model response arrives
- **THEN** the owner quarantines it with the typed collision/refusal reason
- **AND** it does not overwrite or silently drop either value

#### Scenario: a settled loop's result is still readable from the durable record

- **GIVEN** a completed loop whose per-loop in-process state has been released
- **WHEN** another agent reads that loop's result through the loop-result tool
- **THEN** the full result is returned from the durable loop record
- **AND** the test that verifies this is `TestSettledLoopResultReadableAfterRelease`

#### Scenario: approval-timeout sweeping is unaffected

- **GIVEN** a loop awaiting approval past its timeout and a set of already-settled loops
- **WHEN** the approval sweeper snapshots expired approvals
- **THEN** the awaiting loop is still a candidate and the settled loops contribute nothing
- **AND** the test that verifies this is `TestApprovalSweepUnaffectedByTerminalRelease`
