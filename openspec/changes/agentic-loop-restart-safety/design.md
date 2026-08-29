# Design: agentic-loop restart-safe settlement

## Status

Draft after accepted inventory. Owner ruling and independent design review are required before implementation.

## Accepted inventory

This design incorporates `openspec/changes/agentic-loop-restart-safety/inventory.md` unchanged. The accepted
inventory is based on `origin/main@b060511f383d74aa6a8684e39e42020a3b073a9b`, has reviewed SHA-256
`70603493e56887c3e355dcf9087891e03cf7ea7764454fcf528e0686b1bdfe9d`, and received independent
`INVENTORY PASS` at commit `8059bb25` and issue comment `5462731405`. A baseline or touched-surface change
invalidates this reference and requires reinventory before implementation.

## Holds

- #1146 remains blocked by #759.
- #1155 owns the Stage A real-NATS process-replacement proof.
- AgentRun is excluded until #1148 merges and its surface is reinventoried.
- Governance content and policy coverage remain #1140.
- Framework-wide restart generalization remains #1145.
- No implementation is authorized by this draft.

## Options considered

### Do nothing

This has no implementation cost, but retains acknowledged loss, restart-stranded loops, model ambiguity, and
false approval-timeout recovery claims.

### Hydrate all current maps from `AGENT_LOOPS`

This adds an O(active loops) startup scan and process memory. It cannot recover conversation, request and tool
ordering, provider outcomes, or governance waiters because those facts are absent from `LoopEntity`.

### Add a supervisor, checkpoint, or outbox

This adds a new authority, storage, lifecycle, reconciliation, and operator concepts. It duplicates JetStream and
component state and violates ADR-028's two-layer boundary.

### Carry full continuation through every tool message

This avoids a lookup but adds O(batch bytes multiplied by calls and results) NATS cost. It increases max-payload
failure risk and exposes continuation mechanics to adopters.

### Use streams-first recovery with one narrow continuation exception

This uses exact reads only after process correlation is missing. It adds one Store write only when entering an
approval that may outlive stream retention. It preserves the current orchestration layers and is the recommended
design.

## Decision

Adopt streams-first, lane-specific settlement and replay.

Each consumer holds its source delivery until its exact durable business outcome and every required downstream
PubAck have completed. Replacement recovery uses:

1. source redelivery;
2. deterministic LoopID, RequestID, and tool execution identity;
3. exact reads of already committed AGENT messages;
4. read-through hydration of current `LoopEntity`;
5. existing immutable `TOOL_CALL_OUTCOMES`; and
6. a registered Store reference only for approval continuation whose wait can exceed AGENT retention.

The change adds no generic supervisor, recovery state machine, checkpoint or outbox bucket, CQRS path, or
event-sourced loop.

## Cost

- A text-only model turn adds no durable write.
- An ordinary tool turn adds no bucket or Store write. Cold recovery may use one or two exact AGENT reads.
- An approval-required turn adds one content-addressed Store `Put` plus verification and one small typed reference.
- Dispatch performs one bounded `AGENT_LOOPS` projection hydration after replacement.
- Approval timeout performs a bounded hydration of awaiting-approval records when configured.
- Ordinary delivery recovery never scans all loops or the whole AGENT stream.

## Stable identity

- Dispatch derives TaskID and a new LoopID deterministically from validated `UserMessage` identity.
- Every `AgentRequest` RequestID deterministically identifies the loop and logical turn.
- Provider `ToolCall.ID` remains the provider conversational identifier.
- The framework stamps a separate execution identity from RequestID, provider CallID, and call ordinal.
- Tool result, approval, governance, and completed-outcome correlation use that execution identity.
- Required downstream publications use deterministic `Nats-Msg-Id` values.
- Stream-window deduplication is an optimization. Beyond that window, consumers reconcile semantic identity and
  committed outcomes.

A different payload under the same semantic identity is an invariant collision and is quarantined.

## Provider ambiguity

Before invoking, agentic-model checks for an already committed matching `agent.response.<requestID>`.

When no response exists after redelivery, the configured provider policy is one of:

- `fail_commit_unknown`: do not invoke again; publish a typed commit-unknown `AgentResponse`. This is the default.
- `at_least_once`: invoke again with the same RequestID and record that this is a redelivery attempt. This requires
  explicit operator opt-in and may duplicate spend or provider effects.
- `provider_reconcile`: use demonstrated provider idempotency or result lookup keyed by RequestID.

The default avoids a second paid or effectful invocation, but may report commit-unknown when the prior process died
before invoking. This tradeoff is intentional and must be visible to the operator.

A pre-invocation started marker is prohibited because it cannot close both sides of the call boundary. A
completed-result ledger is not introduced because it closes only return-to-publication loss, not invocation
ambiguity.

## Loop cold recovery

Ordinary deliveries do not enumerate all loops.

A response, result, signal, or approval response:

1. derives or reads LoopID from its typed identity;
2. loads the exact `AGENT_LOOPS/<loopID>` record;
3. initializes only that loop's process indexes;
4. reconstructs context and configuration from the latest committed `AgentRequest` and exact originating
   `AgentResponse`;
5. validates the incoming semantic identity against that material; and
6. performs or replays the next deterministic transition.

A later committed request or terminal state may prove that an older delivery was already applied. Absence of
process memory alone never proves staleness.

Exact retained-message lookup is an admitted, operation-specific recovery seam. It does not introduce a general
embedded query front door or a whole-stream scan.

## Tool continuation

`ToolCall` and `ToolResult` carry RequestID and framework execution identity. `agentic-tools` stamps those fields on
every result; executor implementations hosted by the component do not manage them.

During an active tool batch, the latest `agent.request.<loopID>` remains the request that produced the exact
`agent.response.<requestID>`. The response contains the ordered assistant tool calls. `LoopEntity` contains the
accumulated pending results. These sources reconstruct the queue and next transition without a new ledger.

`TOOL_CALL_OUTCOMES` remains the sole completed tool-outcome authority. Its identity evolves to include framework
execution identity without creating another bucket.

## Approval continuation

### Proven exception

A human approval may wait beyond AGENT retention. After replacement, `PendingApprovalState` alone does not contain
the conversation and model turn needed to continue. This is a named failpoint where source redelivery, stable
identity, and existing retained outputs are insufficient.

Before ACKing an approval-required `ToolResult`, agentic-loop:

1. builds an `ApprovalContinuationV1` containing the exact originating `AgentRequest`, `AgentResponse` tool batch,
   and required loop correlation;
2. wraps it as a registered payload;
3. writes it content-addressably through the configured registered Store;
4. reads it back and verifies its digest and identities;
5. stores the typed `StorageReference` in `PendingApprovalState`;
6. persists the awaiting-approval `LoopEntity`; and
7. publishes `ApprovalPendingEvent` with synchronous PubAck.

No new bucket is introduced. The continuation satisfies private-storage grounds 4 and 5: it is potentially bulky,
high-churn operational execution material, not a graph fact. Rules do not read it. The graph-visible current loop
state carries only the typed reference and the narrow approval classification needed by rule consumers.

`ApprovalContinuationV1` must implement the payload contract, use alias-based JSON marshaling, and be registered
explicitly through every required composition root. A production-decoder round-trip and binary registration census
are required. There is no `init()` or unregistered raw envelope.

For approve or modify, the pending record retains the applied response fingerprint until the approved `ToolResult`
arrives. This lets a replacement replay the exact call and reject a conflicting decision. For reject or timeout,
the pending record is cleared only after the deterministic next request or terminal outcome is committed.

When approval timeout is configured, agentic-loop performs narrow startup hydration of awaiting-approval records so
their existing deadlines survive replacement. This is component-owned timer repair, not a general supervisor.

## Dispatch projection recovery

`LoopTracker` remains a cache. At startup, dispatch reconstructs its active-loop and pending-approval projection
from current `AGENT_LOOPS` records. HTTP handlers use exact read-through when the cache misses. `LoopCreatedEvent`
and `ApprovalPendingEvent` update the cache but are not durable authority.

This bounded hydration is required because AutoContinue addresses loops by user and channel rather than LoopID. It
creates no durable state.

## Governance correlation

Governance stays in the existing rule and component layers.

Each proposal carries LoopID, RequestID, execution identity, and a proposal fingerprint. Verdict subjects use the
NATS-safe execution identity. A replacement response handler first checks for an exact matching retained verdict
before publishing a proposal again.

A verdict arriving without a process waiter is validated and remains recoverable; it is not silently discarded as
completed work. No governance bucket is admitted unless a real replacement failpoint proves that retained verdict
lookup and response redelivery are insufficient. Policy content and feature expansion remain #1140.

## Per-lane definition of done

### `user.message` task

Happy-path done is deterministic TaskID and LoopID, `agent.task` PubAck, and deterministic user acknowledgement
PubAck. Invalid or unauthorized input terminates only after user-error PubAck. Transient publication failure retries.

### `user.message` command

Happy-path done is PubAck for the required signal or approval publication and the user response. Invalid commands
terminate after user-error PubAck. Dependency failure retries.

### `agent.task`

Happy-path done is matching `LoopEntity` persistence, required graph birth, and PubAck for deterministic initial
`AgentRequest` and `LoopCreatedEvent`. Permanent rejection becomes a durable terminal loop outcome once identity
exists. Transient assembly, storage, or publication failure retries. Identity collision quarantines.

### `agent.request`

Happy-path done is PubAck for matching durable `AgentResponse`. Pre-invocation permanent errors become typed error
responses. Commit-unknown follows configured provider policy. Invalid envelopes terminate. Collisions quarantine.

### `agent.response` complete or error

Happy-path done is committed `LoopEntity` and `COMPLETE_<loopID>` plus required terminal event PubAck. Transient
writes and publications retry. Permanently malformed responses terminate. Unknown correlation quarantines unless a
committed later output proves prior application.

### `agent.response` tool call

Happy-path done is resolved governance where enabled, stable execution identities, persisted current loop state, and
PubAck for the first tool request or deterministic next request. Store or publication failures retry. Conflicting
identity or fingerprint quarantines.

### `tool.execute`

Happy-path done follows #949: an immutable completed outcome exists and exact `ToolResult` has PubAck. Existing
CallID or external-effect ambiguity remains operation-specific; #1146 adds no second ledger.

### `tool.result`

Happy-path done is hydrated loop, reconstructed originating request and response, persisted result, and PubAck for
the next queued tool, request, approval event, or terminal event. Missing continuation during a live turn retries. An
exact late duplicate on a terminal loop ACKs. Conflicting execution identity quarantines and never log-and-drops.

### `agent.signal`

Happy-path done is persisted current-state transition. Cancel additionally requires `COMPLETE_` and terminal event
PubAck. Invalid signals terminate. A missing live loop retries within its retention horizon; a terminal duplicate
ACKs from committed state.

### `agent.created`

Happy-path done is updated dispatch projection or proof that authoritative `LoopEntity` makes it reconstructable.
Invalid payload terminates. Cache absence is not a business failure.

### `agent.approval_pending`

Happy-path done is updated dispatch projection or an exactly readable `PendingApprovalState` in `AGENT_LOOPS`.
Invalid payload terminates. Replacement reads authority rather than returning false 404 or 409 from an empty cache.

### `agent.approval_response` approve or modify

Happy-path done is exact pending-call validation, stable `ToolCall` PubAck, and persisted approval fingerprint until
the matching `ToolResult` arrives. A same-fingerprint duplicate replays the same call. A conflict quarantines.
Publication failure retries without clearing pending state.

### `agent.approval_response` reject or timeout

Happy-path done is applied synthetic `ToolResult`, PubAck for the resulting request or terminal event, and pending
state cleared only after that durable outcome. Transient dependency failure retries. A stale response ACKs only when
the exact downstream outcome proves it was already applied.

### Governance verdict

Happy-path done is validated proposal identity and delivery to the waiter or exact retained verdict recoverable by
response replay. A missing waiter is not silently ignored. Invalid or mismatched verdict terminates or quarantines.
Audit-mode observability remains nonblocking.

## Settlement terms

- ACK means the source's required durable transition or result and every required downstream PubAck have completed.
- Retry means the operation can safely run again using stable identity and reconciliation.
- Terminate means the payload is permanently invalid and no useful retry exists.
- Quarantine means an identity collision, impossible correlation, panic, or invariant failure prevents a safe
  decision.

No handler converts decode, correlation, KV, Store, provider, or publish failure into successful callback return.

## Context and lifecycle

Production structs do not retain `context.Context`. Delivery work uses the exact delivery context. Cancellation
cancels, joins, and only then settles.

`runWithBudget` and trajectory batch recording must not return while their goroutines remain live. Prefer direct
synchronous calls under bounded contexts. An owned worker, if required, joins before callback and `Stop` return.

Audit remains nonblocking as specified. Nonblocking does not authorize an abandoned goroutine.

## Retention and readiness

At `Start`, the framework observes actual AGENT stream bounds. It rejects a configuration that cannot retain ordinary
continuation outputs for the admitted loop and delivery horizon. The adopter does not predict server-effective state.

A missing configured continuation Store does not silently downgrade approval safety. An approval-required result
remains unsettled and retries while health reports the exact unresolved storage instance.

## Adopter seam

### Approval UI developer

They submit LoopID, framework execution identity, and decision. They do not carry Store references, construct NATS
subjects, or reconstruct arguments. Existing dispatch calls continue through durable read-through after replacement.

### Tool executor author

Executors hosted by `agentic-tools` receive and return ordinary tool values. The framework stamps RequestID,
execution identity, and continuation correlation. A raw external NATS executor must preserve those opaque fields and
receives a loud migration failure if it does not.

### Model adapter operator

They select only the semantic provider ambiguity policy. The default is fail-closed `fail_commit_unknown`. They do
not configure a recovery bucket, checkpoint interval, or guessed retry count.

### Component author

They return domain result and error through #759's settlement router. They do not call ACK or NAK directly or
understand replay storage.

### Framework operator

They register the named Store used for approval continuation and configure provider ambiguity policy. There is no
supervisor, recovery mode, checkpoint interval, or outbox knob.

### Governance author

They supply approve or reject semantics against framework-generated proposal identity. Subject construction,
identity validation, cold correlation, and settlement belong to the framework.

## Measurable premises

The design is rejected or revised if any premise fails:

1. #759 merges the accepted `DeliveryResult` settlement foundation for each touched lane.
2. #1155 proves replacement rather than same-process reconstruction.
3. Request identity uniquely and deterministically binds LoopID and logical turn.
4. Tool execution identity is a digest of RequestID, provider CallID, and ordinal; provider CallID is unchanged.
5. Cold `ToolResult` handling reconstructs its active batch without scanning.
6. Observed AGENT bounds retain ordinary continuation outputs for the admitted loop and delivery horizon.
7. Approval replacement succeeds after deliberate AGENT eviction by resolving the registered Store reference.
8. Governance replacement reads the exact retained verdict or safely re-obtains it. Failure returns for new design.
9. `fail_commit_unknown` produces zero second provider calls; `at_least_once` records repeated attempts explicitly.
10. No process-only map is required to classify a delivered source after replacement.
11. Only dispatch projection and configured approval-deadline repair enumerate current loop state.
12. AgentRun remains absent until #1148 merges and a new inventory is accepted.

## Out of scope

- AgentRun before #1148 merge and reinventory.
- Content-governance behavior owned by #1140.
- Framework-wide restart generalization owned by #1145.
- Exactly-once provider or arbitrary external tool effects.
- A universal replay ledger or full AGENT stream replay.
