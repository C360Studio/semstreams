## ADDED Requirements

### Requirement: All six loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or exported no-heartbeat
adapter.

Each fast physical subscription SHALL acquire with AckWait 30s and enforce a 25s cancellation-and-join work budget,
leaving a positive 5s margin. Budget expiry SHALL return Retry without ACK. If one subscription cannot prove this
bound using legitimate context-cooperative work, that subscription alone SHALL move to the typed heartbeat owner with
bounded work timeout 120s and heartbeat 15s. A sibling in either loop proof group SHALL not migrate solely because
another did. Cancellation-ignoring or non-returning work SHALL fail lifecycle review under both routes.

Decode, correlation, KV, Store, transition, and required publication failures SHALL NOT become successful callback
completion. ACK means the lane-specific durable transition or defined refusal and every required PubAck completed;
Retry means stable identity and reconciliation make re-execution safe; Terminate means permanently invalid with no
useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure prevents a safe choice.

Cancel SHALL remain the entire durable UserSignal vocabulary. ApprovalResponse SHALL remain a separate input.
`LoopStatePaused` and wire value `paused` SHALL be removed from the valid state vocabulary, exported transition
acceptance, schema, examples, and documentation. Persisted `state:"paused"` SHALL be refused as invalid. No
compatibility shim, alias, reserved enum, migration, checkpoint, supervisor, or workflow state machine SHALL be
added. `ResponseAction.Signal` and `ClassifiedIntent.SignalType` SHALL remain outside ownership of durable
`agent.signal.*` settlement.

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

#### Scenario: Semantic identity collision quarantines

- **WHEN** one stable semantic identity is observed with different canonical content
- **THEN** agentic-loop quarantines and stops the exact owner
- **AND** does not apply either value by preference

#### Scenario: Unknown control signal is permanent

- **WHEN** a registered UserSignal carries any value other than cancel
- **THEN** validation or handling terminates it as permanently invalid
- **AND** no warning-only return becomes ACK

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the deterministic terminal event receives PubAck before source ACK

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** the delivery quarantines and stops the exact owner
- **AND** the panic is never rewritten to nil

#### Scenario: Verdict arrives without a waiter

- **WHEN** an exact verdict arrives after replacement with no waiter
- **THEN** its retained identity remains recoverable for response replay
- **AND** missing or full process channel is not completed log-and-drop

#### Scenario: Fast loop work reaches its budget

- **WHEN** cancel, approval-response, approved-verdict, or rejected-verdict work remains incomplete at 25 seconds
- **THEN** that private owner cancels and joins before AckWait 30s expires
- **AND** returns Retry without ACK or concurrent delivery

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
- **AND** deterministically publishes or replays the next output

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

### Requirement: Loop and required-output identities are deterministic and reconcilable

For a new task, agentic-loop SHALL validate deterministic TaskID and LoopID and SHALL reject a conflicting mapping.
Every logical turn SHALL use deterministic RequestID. Every created, request, tool-call, approval, continuation,
terminal, and other required publication SHALL derive a stable output identity from its source identity and
canonical content and SHALL use deterministic `Nats-Msg-Id`.

Before repeating a required publication after redelivery, agentic-loop SHALL use an operation-specific exact
committed-output lookup and validate canonical content, including requests, responses, tool results, governance
proposals/verdicts, and terminal outputs. A match proves the output; a collision quarantines; absence outside admitted
retention remains unknown. No general stream scan or query front door is admitted.

#### Scenario: Task redelivery finds exact loop birth outputs

- **WHEN** a task redelivers after its LoopEntity and initial request committed
- **THEN** agentic-loop validates the deterministic TaskID, LoopID, RequestID, and canonical outputs
- **AND** any required republish uses the same `Nats-Msg-Id`

#### Scenario: Required output identity collides

- **WHEN** exact lookup finds a required output identity with different canonical content
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either output

### Requirement: Approval continuation survives retained-message eviction

Before acknowledging an approval-required ToolResult, agentic-loop SHALL durably store and verify a registered
`ApprovalContinuationV1` and persist its typed StorageReference in `PendingApprovalState`. This guarantee applies only
while that authoritative pending loop record remains inside its admitted finite lifetime.

#### Scenario: Process is replaced while approval waits

- **WHEN** AGENT request/response messages expired but the loop remains awaiting approval
- **THEN** agentic-loop resolves the exact registered Store and reconstructs the reviewed call
- **AND** can approve, modify, reject, or time out without guessing

#### Scenario: Continuation Store is unavailable

- **WHEN** continuation cannot be stored and verified
- **THEN** no successful approval-pending transition is published
- **AND** ToolResult is not acknowledged and the unresolved Store is named

#### Scenario: Conflicting approval responses arrive

- **WHEN** a second response for one execution identity has a different semantic fingerprint
- **THEN** the delivery quarantines and the tool is not dispatched a second way

#### Scenario: Pending authority expired

- **WHEN** authoritative PendingApprovalState expired
- **THEN** agentic-loop does not scan Store objects to guess a pending approval
- **AND** does not advertise indefinite restart safety

### Requirement: Approval continuation is a registered payload

`ApprovalContinuationV1` SHALL carry exact JSON fields `loop_id` string, `task_id` string, `request_id` string,
`execution_id` string, `call_id` string, `call_ordinal` integer, `request` `AgentRequest` object, and `response`
`AgentResponse` object. All five IDs SHALL be non-empty, ordinal SHALL be positive, nested
`AgentRequest` and `AgentResponse` SHALL validate, their RequestIDs SHALL equal top-level RequestID, the response's
tool call at that ordinal SHALL match CallID, and execution identity SHALL recompute from RequestID, CallID, and
ordinal. When attached to pending state, LoopID and TaskID SHALL match that loop authority.

The type SHALL implement Schema as exact
`message.Type{Domain: "agentic", Category: "approval_continuation", Version: "v1"}` using new constant
`CategoryApprovalContinuation = "approval_continuation"`, plus Validate and alias-based JSON marshal/unmarshal for
its own fields. `agentic.RegisterPayloads` SHALL register the factory with
`vocabulary.IndexingProfileControl` and nil projection contracts. `payloadbuiltins.Register` SHALL carry that owner
registration into `cmd/semstreams`, `cmd/e2e-semstreams`, test helpers, and every first-party composition root found
by the implementation-time binary census. The payload is private non-Graphable Store material; control is its
registry indexing floor, not a claim that graph projection occurs. There SHALL be no init registration, raw fallback,
anonymous decoder, or duplicate registration site.

#### Scenario: Required composition root registers continuation

- **WHEN** a binary can host approval recovery
- **THEN** it explicitly registers ApprovalContinuationV1
- **AND** production decoding a `message.NewBaseMessage` envelope yields that payload type

#### Scenario: Registration is absent

- **WHEN** approval recovery encounters an unregistered continuation type
- **THEN** startup or decode fails loudly without raw JSON fallback

#### Scenario: Registered continuation round-trips through production decoder

- **WHEN** a valid `agentic.approval_continuation.v1` is wrapped by `message.NewBaseMessage`, marshaled, and decoded
  through the production registry decoder
- **THEN** the payload is `*ApprovalContinuationV1`
- **AND** every exact wire field, nested value, and identity validates unchanged

#### Scenario: Continuation identity is inconsistent

- **WHEN** ordinal, nested RequestID, selected provider CallID, or recomputed execution identity differs
- **THEN** validation fails before Store write or pending-state mutation

### Requirement: Approval continuation storage is content-addressed and verified

Agentic-loop `Config` SHALL add exact field `ApprovalContinuationStorageInstance string` with JSON tag
`approval_continuation_storage_instance,omitempty` and schema type string, default `objectstore`, category
`advanced`, and a description naming the registered Store for approval continuation. Omission SHALL install
`objectstore`; an explicit empty value after defaults or a name unresolved in
the injected `StoreRegistry` SHALL fail approval-capable setup. The existing
`trajectory_evidence_storage_instance` SHALL NOT be reused because it owns full trajectory evidence and its retention
policy is independently configurable. No physical bucket name or hidden fallback SHALL be accepted.

Agentic-loop SHALL derive the key from SHA-256 of canonical ApprovalContinuationV1 payload JSON, lowercase unpadded
base32 below `agentic/approval-continuation/v1/`, excluding enclosing BaseMessage identity and timestamp. It SHALL use
get-before-put. Every existing or post-write object SHALL production-decode to `*ApprovalContinuationV1`, validate,
match canonical payload bytes, recompute digest/key, and match LoopID, TaskID, RequestID, execution identity, provider
CallID, and ordinal. Store overwrite SHALL NOT resolve collision.

#### Scenario: Matching object already exists

- **WHEN** the expected key contains a valid matching continuation
- **THEN** agentic-loop reuses it without another write

#### Scenario: Continuation storage key is omitted

- **WHEN** `approval_continuation_storage_instance` is omitted
- **THEN** setup selects registered Store instance `objectstore`
- **AND** does not borrow `trajectory_evidence_storage_instance`

#### Scenario: Continuation Store name is unresolved

- **WHEN** the configured continuation Store name is absent from `StoreRegistry`
- **THEN** approval-capable setup refuses readiness naming the unresolved instance
- **AND** no approval-required source is positively settled

#### Scenario: Put result is unknown

- **WHEN** Put returns an unknown commit result
- **THEN** agentic-loop reads the deterministic key
- **AND** accepts matching content or retries unresolved absence

#### Scenario: Retry constructs a fresh continuation envelope

- **WHEN** the first Put reply is lost and retry wraps the same canonical payload in a new BaseMessage
- **AND** envelope UUID and timestamp differ
- **THEN** decoded canonical payload equality, digest, key, and identities accept reuse
- **AND** transport metadata does not create a semantic collision

#### Scenario: Key contains conflicting content

- **WHEN** the deterministic key contains malformed or semantically different content
- **THEN** agentic-loop quarantines the collision and fails health loudly

#### Scenario: Referenced continuation is permanently absent

- **WHEN** pending state names a permanently absent continuation
- **THEN** the loop durably fails with `approval_continuation_unavailable`
- **AND** does not reconstruct from partial arguments

#### Scenario: Continuation is no longer needed

- **WHEN** the deterministic downstream outcome commits
- **THEN** deletion is best-effort and cleanup failure is metered
- **AND** source settlement is not reversed and no scanner is added

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
shortest positive BackOff.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s,2m]`
- **THEN** it returns a typed error naming the values and 15s ceiling
- **AND** allocates no consumer

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
is absent or the loop is terminal. The lane owner MUST read the exact durable loop state and reconcile the exact
committed output that would prove this semantic input already applied. Exact matching applied proof permits the
owner's typed already-applied terminal outcome. An unreadable authority or unresolved absence returns Retry. A
malformed input, identity collision, impossible transition, or contradictory committed output returns Quarantine.
There is no unconditional quiet settled-drop.

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

#### Scenario: a late approval response has exact applied proof

- **GIVEN** a settled loop whose process state has been released
- **AND** durable state plus the exact committed downstream output prove the same approval response was applied
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

#### Scenario: a late response conflicts with committed identity

- **GIVEN** the expected semantic identity names different canonical committed content or an impossible transition
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
