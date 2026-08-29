## ADDED Requirements

### Requirement: Agentic-loop settles each durable input after its durable outcome

Agentic-loop SHALL classify task, response, tool-result, signal, approval-response, and governance-verdict
deliveries through the accepted semantic settlement contract. Decode, correlation, KV, Store, transition, and
required publication failures SHALL NOT become successful callback completion.

#### Scenario: Required output publication fails

- **WHEN** a handler has computed a transition
- **AND** a required downstream publication does not receive PubAck
- **THEN** the source delivery is not positively acknowledged
- **AND** the disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Duplicate is proven applied

- **WHEN** a redelivered input's exact semantic identity is present in a committed later request or terminal outcome
- **THEN** agentic-loop positively acknowledges the duplicate
- **AND** does not repeat non-idempotent work

#### Scenario: Missing process correlation is not proof of staleness

- **WHEN** a delivery arrives after replacement and its process-only map entry is absent
- **THEN** agentic-loop performs exact durable read-through
- **AND** does not log-and-drop or positively acknowledge solely because memory is empty

### Requirement: Loop recovery is lane-specific and read-through

Agentic-loop SHALL load the exact `LoopEntity` identified by an incoming delivery and reconstruct only the material
required for that delivery. Ordinary recovery SHALL NOT enumerate or replay the full AGENT stream.

#### Scenario: Model response arrives after replacement

- **WHEN** `AgentResponse` carries a structured RequestID
- **THEN** agentic-loop resolves LoopID
- **AND** loads the current `LoopEntity`
- **AND** reconstructs conversation and request configuration from committed `AgentRequest` material
- **AND** validates that the response matches the active or already-applied turn

#### Scenario: Tool result arrives after replacement

- **WHEN** `ToolResult` carries RequestID and framework execution identity
- **THEN** agentic-loop reads the exact originating `AgentResponse`
- **AND** reconstructs the ordered batch from the response and accumulated durable results
- **AND** deterministically publishes or replays the next required output

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider `ToolCall.ID` for provider conversation semantics and SHALL stamp a distinct
stable execution identity derived from RequestID, provider CallID, and call ordinal. Tool and governance durable
correlation SHALL use the framework execution identity.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same provider CallID under different RequestIDs
- **THEN** their framework execution identities differ
- **AND** their completed tool outcomes cannot collide

### Requirement: Approval continuation survives retained-message eviction

Before acknowledging an approval-required `ToolResult`, agentic-loop SHALL durably store and verify a registered
`ApprovalContinuationV1` object and persist its typed `StorageReference` in `PendingApprovalState`. This guarantee
applies while the authoritative pending loop record remains within its admitted finite approval lifetime.

#### Scenario: Process is replaced while approval waits

- **WHEN** AGENT request and response messages have expired
- **AND** the `LoopEntity` remains awaiting approval
- **THEN** agentic-loop resolves the exact registered Store named by the reference
- **AND** reconstructs the reviewed call and continuation
- **AND** can approve, modify, reject, or time out without guessing

#### Scenario: Continuation Store is unavailable

- **WHEN** an approval-required result cannot store or verify its continuation
- **THEN** agentic-loop does not publish a successful approval-pending transition
- **AND** does not positively acknowledge the `ToolResult`
- **AND** reports the exact unresolved storage instance

#### Scenario: Conflicting approval responses arrive

- **WHEN** a second response for the same execution identity has a different semantic fingerprint
- **THEN** agentic-loop quarantines the conflicting delivery
- **AND** does not dispatch the tool a second way

#### Scenario: Pending loop authority has expired

- **WHEN** the authoritative `PendingApprovalState` has expired
- **THEN** agentic-loop does not scan Store objects to guess a pending approval
- **AND** does not advertise the approval as indefinitely restart-safe

### Requirement: Approval continuation is a registered payload

`ApprovalContinuationV1` SHALL implement the payload contract and SHALL be registered explicitly in every required
composition root. Its JSON methods SHALL marshal only its own fields through a type alias. Its Store object SHALL use
the normal typed envelope and SHALL pass a production-decoder round trip.

Its registration SHALL declare no indexing-profile floor and no projection contract because the payload is private,
non-Graphable Store material.

#### Scenario: A binary needs approval continuation

- **WHEN** a composition root can host agentic-loop approval recovery
- **THEN** that root explicitly registers `ApprovalContinuationV1`
- **AND** decoding an object produced through `message.NewBaseMessage` yields that payload type

#### Scenario: The payload registration is absent

- **WHEN** approval recovery encounters an unregistered continuation type
- **THEN** startup or decoding fails loudly
- **AND** the framework does not treat raw JSON as a compatible fallback

### Requirement: Approval continuation storage is content-addressed and verified

Agentic-loop SHALL derive the continuation key from SHA-256 of canonical payload JSON, excluding `BaseMessage`
identity and metadata. It SHALL use get-before-put and post-write production decoding, validation, digest, and
identity verification. Store overwrite behavior SHALL NOT resolve collisions.

#### Scenario: Matching object already exists

- **WHEN** the expected key contains a valid matching continuation
- **THEN** agentic-loop reuses it
- **AND** does not write another object

#### Scenario: Put result is unknown

- **WHEN** Put returns an unknown commit result
- **THEN** agentic-loop reads the deterministic key
- **AND** treats a matching object as committed success
- **AND** retries unresolved absence

#### Scenario: Key contains conflicting content

- **WHEN** the deterministic key contains malformed or semantically different content
- **THEN** agentic-loop quarantines the invariant collision
- **AND** fails health loudly

#### Scenario: Referenced continuation is permanently absent

- **WHEN** a pending loop references a continuation that is permanently absent
- **THEN** agentic-loop durably fails the loop with `approval_continuation_unavailable`
- **AND** does not reconstruct from partial arguments

#### Scenario: Continuation is no longer needed

- **WHEN** the deterministic downstream outcome has committed
- **THEN** continuation deletion is best-effort
- **AND** cleanup failure does not reverse source settlement
- **AND** cleanup failure and orphan bytes are metered without adding a scanner

### Requirement: Approval lifetime is bounded by its reference authority

When approval gating is enabled, approval timeout SHALL be finite, nonzero, and no longer than observed
`AGENT_LOOPS` retention with the required safety margin. Zero, empty, indefinite, and over-retention values SHALL fail
configuration validation. The default approval timeout SHALL be 12 hours.

#### Scenario: Approval timeout is omitted

- **WHEN** approval gating is enabled and no timeout is supplied
- **THEN** agentic-loop uses a 12-hour timeout
- **AND** validates it against observed `AGENT_LOOPS` retention

#### Scenario: Approval timeout is indefinite

- **WHEN** approval gating is enabled with zero or empty timeout
- **THEN** configuration validation fails
- **AND** the framework does not claim indefinite restart-safe approval

#### Scenario: Approval timeout exceeds loop retention

- **WHEN** configured timeout exceeds observed `AGENT_LOOPS` retention after safety margin
- **THEN** startup fails readiness with the observed and required values

### Requirement: Approval deadlines are reconstructed narrowly

When approval timeout is configured, agentic-loop SHALL reconstruct awaiting-approval deadlines from current
`AGENT_LOOPS` facts after replacement. This repair SHALL own only approval timers and SHALL NOT become a generic
recovery supervisor.

#### Scenario: Approval timer owner is replaced

- **WHEN** a process is replaced while a persisted approval deadline remains active
- **THEN** agentic-loop reconstructs that deadline from current awaiting-approval state
- **AND** does not enumerate unrelated loop work for replay

### Requirement: Delivery work joins before settlement

Every goroutine spawned by agentic-loop delivery work SHALL join before the delivery callback returns. A bounded
timeout SHALL cancel the operation but SHALL NOT authorize returning while the task remains live.

#### Scenario: Delivery work exceeds its budget

- **WHEN** bounded delivery work reaches its deadline
- **THEN** the owner cancels and joins the work
- **AND** the delivery callback returns only after that join completes

### Requirement: Restart-safe replay observes and admits stream bounds

At startup, agentic-loop SHALL observe actual AGENT stream bounds. Restart-safe admission SHALL require
`DiscardNew`, sufficient MaxAge for the framework-computed loop and delivery horizon, and no earlier per-subject or
message eviction bound. Configuration SHALL NOT require an adopter to predict server-effective retention.

#### Scenario: Observed retention is unsafe

- **WHEN** the actual AGENT stream bounds are shorter than the admitted ordinary continuation horizon
- **THEN** agentic-loop reports the observed mismatch in readiness
- **AND** does not claim restart-safe recovery

#### Scenario: Capacity policy discards old evidence

- **WHEN** the actual AGENT stream uses `DiscardOld`
- **THEN** restart-safe consumers do not start
- **AND** readiness reports that size pressure can evict continuation authority

#### Scenario: Capacity is full under admitted policy

- **WHEN** the admitted `DiscardNew` stream rejects a required publication for capacity
- **THEN** the producer returns Retry
- **AND** retains its source delivery rather than accepting continuation loss
