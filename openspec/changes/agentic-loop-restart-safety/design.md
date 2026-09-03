# Design: agentic-loop restart-safe settlement

## Status

Owner-accepted target state reconciled after nested PR #1251 at exact branch checkpoint
`P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`, whose merge base with the frozen staged #759 parent is exact
`F=417beae5552f8f15ad3540edd7d8504c87174c13`. Active OpenSpec materialization requires independent
pre-implementation design review before production work begins.

## Accepted inventory

This design incorporates three accepted evidence checkpoints:

- exact #759 foundation inventory
  `inventory-rebaseline-2026-09-02-F.md`, base
  `417beae5552f8f15ad3540edd7d8504c87174c13`, SHA-256
  `3b53c6d3d4f3298d63ffc2231b209aa8e1f4379a6c1bf75b7aa5edc6a4f65ffb`, `INVENTORY PASS`, 555/555 pins; and
- post-#1251 refresh `inventory-rebaseline-2026-09-03-post-1251.md`, base/head
  `09ba38b1de5e7200e72281c8e4b8941d81be1da2`, SHA-256
  `2888e28a7439ff4dc62345bf9a1e476054c292326ac291ab1d4519f9c0600a73`, `INVENTORY PASS`, 181/181 pins.
- first-party publisher addendum `inventory-addendum-first-party-agent-publisher-2026-09-03.md`, base/head
  `09ba38b1de5e7200e72281c8e4b8941d81be1da2`, SHA-256
  `0adba4f0092017d84f1ef181ebaf3299323f5cc75b999825bd1e16d6e292930f`, `INVENTORY PASS`, 226/226 pins.

The standalone `design-reconciliation-F-2026-09-02.md` preserves the reviewed reasoning and owner-ruling record.
After this materialization it is non-normative evidence: proposal, this design, tasks, and capability deltas are the
only active target-state authority. A baseline or touched-surface change invalidates the checkpoint and requires
reinventory before implementation.

## Holds

- PR #1159 remains stacked on `codex/gh759-semantic-settlement`; #759 does not merge first. The reviewed parent stays
  frozen through #1159 implementation and review. Any advance requires a new pin, rebase, inventory verification,
  test, and re-review.
- #1146 owns its 17-subscription tranche of #1155's real-NATS process-replacement proof. #1155 remains open until
  #1249 supplies transferred AgentRun complete/failed proof; the combined 19-subscription gate is completed later.
- The full 17-subscription scope remains intact. AgentRun complete/failed fanout is transferred to #1249 from exact
  post-#1146 checkpoint `A` and is not implemented or specified here.
- Nested PR #1251 is integrated: pause/resume handlers, request fields, and unused signal verbs are gone. Binding
  product ruling #1239 comment `5526837992`, linked from #1146 comment `5526837994`, supersedes the earlier review
  premise that `LoopStatePaused` should remain legacy-valid. Cancel and ApprovalResponse remain separate durable
  lanes; `paused` is removed from state vocabulary, API, schema, docs, and persisted-value acceptance with no shim,
  alias, reserved enum, or migration. `ResponseAction.Signal` and `ClassifiedIntent.SignalType` remain outside
  durable `agent.signal.*` settlement.
- Governance policy content remains #1140; framework-wide pattern generalization remains #1145; #1244 follows #1146
  and designs declared transitions against the settled handler exits.
- No native settlement escapes a private owner, no exported no-heartbeat API is added, and no production work begins
  before this active materialization passes pre-implementation design review.
- The framework-owned rule `publish_agent` producer that feeds row 15 is part of this vertical. Its six configured
  classifier surfaces and four statically loaded producer configurations use the existing rule publisher plus the
  same repo-internal AGENT admission validator; no second gate or exported API is introduced.

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

The owner also accepted strict completion-before-lease as the first route for each physical fast subscription:
AckWait 30s, enforced cancellation-and-join work budget 25s, and positive margin 5s. A legitimate
context-cooperative operation measured beyond 25s compels heartbeat fallback only for that subscription.
Cancellation-ignoring or non-returning work is a lifecycle failure under both routes and is never heartbeat evidence.

The shared decision skills resolve as follows:

- `kv-or-stream`: no new communication path. Work remains on JetStream; current loop facts remain in existing KV;
  bulky approval continuation uses the existing Store.
- `entity-or-bucket`: continuation is private, bulky, high-churn execution material. Its reference stays in existing
  `AGENT_LOOPS`; no new bucket or rule-readable graph fact is added.
- `orchestration-check`: settlement, projection hydration, and approval timer repair are component execution and
  lifecycle behavior. Rules still trigger and components execute; there is no third orchestration layer.
- `new-payload`: `ApprovalContinuationV1` registers through `agentic.RegisterPayloads` and
  `payloadbuiltins.Register`, uses exact `agentic.approval_continuation.v1`, alias JSON and production decoding, has
  the control indexing floor and nil projection contracts, and has no `init()` or raw fallback.

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
- Every required dispatch task/control/user-response, model response, loop request/tool/approval/terminal,
  governance validated-output/proposal/verdict, and tool-result publication uses deterministic `Nats-Msg-Id`.
- Before repeating one of those outputs, its owning capability performs an operation-specific exact committed-output
  lookup and validates canonical content. This explicitly includes validated governance output. Exact match proves
  commitment, collision quarantines, and absence outside admitted retention remains unknown.
- Stream-window deduplication is an optimization. Beyond that window, consumers reconcile semantic identity and
  committed outcomes.

A different payload under the same semantic identity is an invariant collision and is quarantined.

## Current-spec reconciliation

Three current requirements conflict with the accepted restart contract and are replaced in full rather than
shadowed by additive text:

- `agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone` retains its exact
  heading and unaffected scenarios, but makes `AGENT_LOOPS` authority, `LoopTracker` a projection, tracker-only
  existence a refusal, and `paused` invalid.
- `agentic-tools / Tool-call completion SHALL be durable before request acknowledgement`,
  `Tool-result bounds SHALL be observed rather than predicted`, and
  `Executor panic and ambiguous pre-completion effects SHALL be explicit` retain observed-bounds and panic behavior
  while replacing provider CallID keys, surrogate IDs, and blanket external-effect idempotency with framework
  execution identity, deterministic `Nats-Msg-Id`, exact outcome reconciliation, and operation-specific effect
  authority.
- `agentic-loop / Per-loop in-process state is released at terminal, through the one release point` retains the one
  idempotent release point and result/sweeper behavior, but replaces unconditional quiet settled-drop with exact
  applied proof, Retry for unresolved authority, and Quarantine for collision or impossible transition.

## Provider ambiguity

Before invoking, agentic-model checks for an already committed matching `agent.response.<requestID>` after its local
AGENT replay-admission gate succeeds.

The staged #759 foundation already supplies immutable `DeliveryAttempt` through `DeliveryWork`. Natsclient constructs
it from native message metadata before invoking work; agentic-model receives delivery number, metadata availability,
and redelivery classification only. It receives no native message, settlement method, sequence, consumer identity,
header, or mutable state and adds no model-private wrapper.

Agentic-model validates `HeartbeatDeliveryPolicy` against the exact acquisition config before allocating the
consumer. Missing metadata prevents work, produces typed `delivery_metadata_unavailable`, quarantines without
positive settlement, and stops the exact owner.

When no response exists after redelivery, the configured provider policy is one of:

- `fail_commit_unknown`: do not invoke again; publish a typed commit-unknown `AgentResponse`. This is the default.
- `at_least_once`: invoke again with the same RequestID and record that this is a redelivery attempt. This requires
  explicit operator opt-in and may duplicate spend or provider effects.
- `provider_reconcile`: use demonstrated provider idempotency or result lookup keyed by RequestID.

The outward field is exactly `ProviderAmbiguityPolicy string` with JSON key
`provider_ambiguity_policy,omitempty`; schema enum is
`fail_commit_unknown|at_least_once|provider_reconcile`, default `fail_commit_unknown`, category `advanced`, and its
description names paid/effectful duplicate risk. Omission or empty defaults to `fail_commit_unknown`, while every
other value fails Config validation before consumer allocation. Shipped fixtures and operator docs carry the same
enum and default.

`provider_reconcile` is not an operator assertion and is not implied by formatting-only `ProviderAdapter` or
`ResponsesAdapter`. The model package owns a private `providerCommitReconciler` seam with exact method
`ReconcileProviderCommit(context.Context, agentic.AgentRequest) (providerReconcileResult, error)`. Its closed result
kinds are `exact_match`, `proven_not_invoked`, `unresolved`, and `collision`; exact-match carries a validated
`AgentResponse`, and reconciliation observes by RequestID without invoking. Before request-consumer allocation,
setup enumerates `RegistryReader.ListEndpoints` and validates every endpoint reachable through direct, default, or
capability routing. If any lacks the internal seam, `provider_reconcile` refuses readiness with typed
`provider_reconcile_unsupported` naming the endpoint. At checkpoint P no shipped backend declares this capability,
so the option cannot start until a separately reviewed backend supplies and proves it.

The default avoids a second paid or effectful invocation, but may report commit-unknown when the prior process died
before invoking. This tradeoff is intentional and must be visible to the operator.

Commit-unknown is machine-readable through this exact field:

```go
FailureKind AgentResponseFailureKind `json:"failure_kind,omitempty"`
```

The wire encoding is an optional JSON string;
its only new non-empty value is `provider_commit_unknown`, valid only when `status:"error"`. Empty is omitted and
ordinary responses remain valid; every unknown value or non-empty value on another status is invalid. Consumers
branch on this field and never parse free-text error content.

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
high-churn operational execution material, not a graph fact. `PendingApprovalState` is private operational state in
the existing `AGENT_LOOPS` authority. It produces no triples and is not directly rule-readable. Rules continue to
observe their admitted event and graph facts; they do not read continuation references or decision fingerprints.

`ApprovalContinuationV1` contains exact JSON fields `loop_id` string, `task_id` string, `request_id` string,
`execution_id` string, `call_id` string, `call_ordinal` integer, `request` `AgentRequest`, and `response`
`AgentResponse`. Validation requires all five IDs, a positive ordinal, valid nested values, matching nested
RequestIDs, the matching provider call at that ordinal, recomputed execution identity, and LoopID/TaskID agreement
with the enclosing pending state.

It implements `Schema` as exact
`message.Type{Domain: "agentic", Category: "approval_continuation", Version: "v1"}` through
`CategoryApprovalContinuation = "approval_continuation"`, `Validate`, and own-field alias JSON marshal/unmarshal.
`agentic.RegisterPayloads` registers its factory with `vocabulary.IndexingProfileControl` and nil projection
contracts because it is non-Graphable Store material. `payloadbuiltins.Register` carries the owner registration to
`cmd/semstreams`, `cmd/e2e-semstreams`, test helpers, and every first-party composition root found by the repeated
binary census. Production decoder round-trip is mandatory; there is no `init()`, raw fallback, anonymous decoder, or
duplicate registration site.

Loop config adds exact field `ApprovalContinuationStorageInstance string` with JSON key
`approval_continuation_storage_instance,omitempty`, schema type string/default `objectstore`/category `advanced`, and
a description naming the registered Store for approval continuation.
Omission installs that default; explicit empty after defaults or an unresolved `StoreRegistry` name refuses
approval-capable setup. The existing `trajectory_evidence_storage_instance` is not reused: repository evidence scopes
it to full trajectory evidence, whose retention and ownership are independent. No physical bucket key or hidden
fallback is added.

The content key is SHA-256 of canonical payload JSON, encoded lowercase base32 without padding beneath
`agentic/approval-continuation/v1/`. The digest excludes `BaseMessage` random ID and timestamp. Stored bytes are one
normal registered `BaseMessage` envelope, and the `StorageReference` records exact Store instance, deterministic key,
content type, and envelope size.

Persistence validates, derives the key, resolves the exact Store, and performs get-before-put. A matching existing
object is reused only after production decoding to `*ApprovalContinuationV1`, validation, canonical payload equality,
digest/key recomputation, and equality of LoopID, TaskID, RequestID, execution identity, provider CallID, and ordinal.
BaseMessage UUID and timestamp are deliberately ignored because retry constructs a fresh normal envelope. Absence
permits one Put followed by Get and the same verification. An unknown Put result is reconciled by Get: a matching
canonical payload succeeds, unresolved absence retries, and malformed or conflicting content quarantines. Store
overwrite behavior is never collision resolution.

A permanently missing referenced object fails the loop durably with `approval_continuation_unavailable`; recovery
never guesses from partial arguments. The reference remains until the deterministic downstream outcome commits.
Deletion afterward is best-effort and does not gate settlement. A crash may leave a content-addressed orphan; #1146
adds metrics but no scanner or reaper. Any later cleanup authority requires measured growth and a separate ruling.

For approve or modify, the pending record retains the applied response fingerprint until the approved `ToolResult`
arrives. This lets a replacement replay the exact call and reject a conflicting decision. For reject or timeout,
the pending record is cleared only after the deterministic next request or terminal outcome is committed.

When approval timeout is configured, agentic-loop performs narrow startup hydration of awaiting-approval records so
their existing deadlines survive replacement. This is component-owned timer repair, not a general supervisor.

### Reference lifetime

The Store object is discoverable only while `PendingApprovalState` remains in `AGENT_LOOPS`, whose current TTL is 24
hours. Therefore the earlier indefinite claim is false. Approval timeout defaults to 12 hours and must be finite,
nonzero, and within observed `AGENT_LOOPS` retention after the framework-computed safety margin.

Persisting the reference in the graph is rejected because it exposes private execution material. A new bucket is not
admitted. If truly indefinite approval is required, design stops for a separately reviewed change to the existing
loop authority or a discoverable reference authority rather than claiming the Store object alone solves it.

## Dispatch projection recovery

`LoopTracker` remains a cache. Dispatch builds a candidate projection from an all-current `AGENT_LOOPS` watch while
applying ordered concurrent updates. It exposes that projection to AutoContinue only after the watch's initial
snapshot completion boundary, then installs it atomically. An interrupted initial or live watch makes AutoContinue
unavailable until another complete projection is installed. A partial projection is never authoritative.

Explicit LoopID operations use exact read-through and may remain available while AutoContinue hydration is
incomplete. `LoopCreatedEvent` and `ApprovalPendingEvent` update the projection but are not durable authority.

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

`fast owner` means the existing binding callback retains native settlement and business work returns a typed
decision; no message or settlement closure escapes. `heartbeat owner` means staged
`ConsumeDeliveryWithHeartbeat` owns metadata, renewal, and the terminal method. Every owner stops admission, drains
its retained handle, awaits exact `Closed`, then cancels and joins its own observer and work goroutines.

| # | Physical subscription and owner | Happy-path done | Sad-path settlement | Durable authority and replacement |
|---:|---|---|---|---|
| 1 | dispatch `user.message`; fast | Required deterministic task/signal/approval and user-response publications receive PubAck; refusal response receives PubAck without tracker/gauge mutation | permanent invalid after response → Terminate; transient lookup/marshal/publish → Retry; collision/panic → Quarantine | source MessageID, deterministic output IDs, `AGENT_LOOPS` merge; tracker is cache |
| 2 | dispatch `agent.created`; fast | projection update or exact `AGENT_LOOPS` proof | invalid → Terminate; unreadable authority/interrupted projection → Retry; conflict → Quarantine | LoopID plus current loop authority; replacement installs only a complete snapshot |
| 3 | dispatch `agent.approval_pending`; fast | projection update or exact matching pending state | invalid → Terminate; unreadable live authority → Retry; identity conflict → Quarantine | `PendingApprovalState` and continuation reference; cache absence proves nothing |
| 4 | governance `task_validation`; fast | completed policy; allowed output has deterministic JetStream PubAck; blocked means deliberate non-forwarding | invalid → Terminate; filter/resolution/marshal/publish uncertainty → Retry; collision/panic → Quarantine | source MessageID plus deterministic validated-output identity |
| 5 | governance `request_validation`; fast | same row-4 contract for exact `AgentRequest` | same as row 4; core-NATS publish never proves done | RequestID and validated-output fingerprint |
| 6 | governance `response_validation`; fast | same row-4 contract for exact `AgentResponse` | same as row 4 | RequestID and validated-output fingerprint |
| 7 | loop `agent.signal`; fast, cancel-only after #1251 | current cancellation state and `COMPLETE_` commit; deterministic terminal event receives PubAck | invalid/unknown → Terminate; missing live authority/KV/publication → Retry; conflict/panic → Quarantine | LoopID and MessageID; exact loop state distinguishes live missing from terminal duplicate |
| 8 | loop `agent.approval_response`; fast | approve/modify dispatches exact call and retains fingerprint; reject/timeout commits deterministic next outcome before clear | invalid → Terminate; Store/KV/publication/unreadable authority → Retry; panic/mismatch/conflict → Quarantine | LoopID, CallID, execution identity, pending state, verified Store continuation |
| 9 | loop `agent.toolcall.approved`; fast | exact verdict reaches waiter or remains durably recoverable for response replay | invalid → Terminate; retained lookup unavailable → Retry; mismatch/panic → Quarantine | execution identity, proposal fingerprint, retained AGENT verdict |
| 10 | loop `agent.toolcall.rejected`; fast | same row-9 contract for rejection | same as row 9 | same as row 9 |
| 11 | tools `tool.execute`; heartbeat | exact immutable completed outcome exists and exact ToolResult receives PubAck | permanent invalid → Terminate; transient Store/publish → Retry; collision → Quarantine | existing `TOOL_CALL_OUTCOMES`; completed replay invokes no executor |
| 12 | dispatch `agent.complete`; heartbeat | exact terminal ancestry/read-through and deterministic user response PubAck | invalid → Terminate; unreadable authority/publish → Retry; conflict/unknown publish → Quarantine | SourceMessageID, LoopID, current loop state, deterministic response ID |
| 13 | dispatch `agent.failed`; heartbeat | same row-12 contract for failure | same as row 12 | same as row 12 |
| 14 | model `agent.request`; heartbeat | matching durable AgentResponse receives PubAck; replay checks before provider | invalid/endpoint permanent → error response or Terminate; safe transient → Retry; collision/metadata/panic → Quarantine | RequestID and fingerprint; default unresolved redelivery emits commit-unknown with zero provider calls |
| 15 | loop `agent.task`; heartbeat | loop/graph birth or continuation commits; initial/next request and created/refusal outputs receive PubAck | invalid → Terminate; safely reconcilable dependency failure → Retry; collision/impossible partial birth/panic → Quarantine | TaskID/LoopID, `AGENT_LOOPS`, graph identity, deterministic request/event IDs |
| 16 | loop `agent.response`; heartbeat | exact turn is hydrated and resulting loop/terminal/next output commits with PubAck | invalid → Terminate; missing correlation/dependency → Retry; conflict/panic → Quarantine; proven duplicate → ACK | RequestID, current loop, originating request/response, committed later output |
| 17 | loop `tool.result`; heartbeat | exact batch is rebuilt, result persists, continuation is verified when needed, and next output receives PubAck | invalid → Terminate; missing live continuation/dependency → Retry; conflict/permanent missing reference/panic → Quarantine or durable loop failure; proven duplicate → ACK | RequestID, execution identity, current loop, originating request/response, completed outcomes, Store reference |

AgentRun complete/failed subscriptions are not rows 18/19 here. #1249 owns their separate post-#1146 contract and
replacement proof from checkpoint `A`.

### Fast-subscription lease boundary

The ten physical fast subscriptions are organized into four parallel proof groups only to bound test wall-clock:
dispatch, governance, loop signal/approval, and loop verdict. Boundary proof and fallback apply to each physical
subscription independently and never to a whole group.

| Physical subscriptions | Strict AckWait/work/join margin | Evidence-triggered fallback |
|---|---|---|
| dispatch rows 1-3 | 30s / 25s / 5s | bounded work 30s / heartbeat 10s |
| governance rows 4-6 | 30s / 25s / 5s | bounded work 30s / concrete reviewed heartbeat `<=15s` |
| loop rows 7-10 | 30s / 25s / 5s | bounded work 120s / heartbeat 15s |

Every blocking NATS, KV, Store, provider, and filter operation receives the exact delivery context. Budget expiry is
Retry without ACK, and cancellation plus all joins complete within the work budget. Legitimate context-cooperative
work measured beyond 25s compels fallback only for that subscription. Cancellation-ignoring or non-returning work is
a lifecycle blocker under both routes. Governance has no accepted concrete heartbeat default; only the policy ceiling
is proven, so implementation stops for review if a governance subscription actually needs fallback.

### Heartbeat-policy migration

- Model keeps AckWait 120s and changes default heartbeat 90s → 60s.
- Loop task/response/tool-result keep BackOff `[30s,2m]` and change default/schema heartbeat 60s → 15s.
- Tools remain 5s against shortest BackOff 15s; dispatch terminal remains 10s against effective AckWait 30s.

Validation uses the exact acquisition config and occurs before allocation. With BackOff, the shortest positive value
is the effective acknowledgement interval; otherwise positive AckWait or the server 30s default is effective.
Heartbeat may be at most half that interval. Errors name component, port, observed heartbeat, effective interval, and
ceiling. Config structs, defaults, generated schemas, docs, and every example/test fixture migrate together.

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

## User-facing control and quiesce contract

The supported execution controls are cancel and durable ApprovalResponse wait. A safe retry or process replacement
continues from the last proven durable boundary through redelivery and reconciliation; it does not restore an
arbitrary in-memory instruction pointer. Operational quiesce is lifecycle behavior: stop admission, drain exact
consumer handles, cooperatively cancel owned work, and join before Stop returns.

`paused` is not a compatibility state. Code constants, accepted state vocabularies, exported transition acceptance,
schemas, examples, and docs remove it. A persisted `state:"paused"` is invalid and is refused without shim, alias,
migration, checkpoint, supervisor, or workflow state machine. A future suspend-at-next-durable-boundary feature would
require a new evidence-backed contract and owner ruling; this change reserves no enum or API for it.

## Cross-lane invariants and proof sources

Property and fuzz tests cite these normative delta requirements rather than reconstructing properties from code:

| Invariant | Normative source |
|---|---|
| one semantic identity cannot accept different canonical content | `agentic-loop / Loop and required-output identities are deterministic and reconcilable` |
| missing process state never proves stale or complete | `agentic-loop / All six loop input classes settle after owner-specific durable done` |
| ACK follows every required durable effect and PubAck | `agentic-loop / All six loop input classes settle after owner-specific durable done` |
| commit-unknown is closed and error-only | `agentic-model / Provider commit-unknown is machine-readable` |
| matching response reconciliation makes zero provider calls | `agentic-model / Model request settlement is bound to a durable response` |
| continuation equality ignores fresh envelope metadata but detects semantic collision | `agentic-loop / Approval continuation storage is content-addressed and verified` |
| partial projection never licenses AutoContinue | `agentic-dispatch / Dispatch process state is a reconstructable projection` |
| observed DiscardOld cannot satisfy strong recovery | `agentic-loop / Restart-safe replay observes and admits local stream bounds` |
| every delivery task joins before result and Stop | `agentic-loop / Delivery work joins before settlement`; owner-specific shutdown requirements |
| first-party rule task output is durable before row 15 | `rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication` |
| fatal delivery-owner loss is visible and drains only its exact handle | model durable-response and loop all-six-input requirements |
| fixed loop BackOff is admitted only when MaxDeliver covers every entry | `agentic-loop / Long-running loop heartbeat policy is valid before acquisition` |

## Context and lifecycle

Production structs do not retain `context.Context`. Delivery work uses the exact delivery context. Cancellation
cancels, joins, and only then settles.

`runWithBudget` and trajectory batch recording must not return while their goroutines remain live. Prefer direct
synchronous calls under bounded contexts. An owned worker, if required, joins before callback and `Stop` return.

Dispatch, governance, model, loop, and tools each stop admission, drain their exact retained consume handles, await
each exact `Closed` signal, then cancel and join owner-stop observers and work. Their capability deltas own these
shutdown clauses; this paragraph is explanation rather than the sole authority.

Audit remains nonblocking as specified. Nonblocking does not authorize an abandoned goroutine.

### Fatal delivery-owner loss

Model and loop reuse the existing agentic-tools problem shape. The first fatal delivery-owner result is latched
synchronously before the owner-stop observer can drain its exact consume handle. Existing component health reports
`Healthy=false`, status `delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the
existing error count. Later fatal results neither overwrite nor recount the first cause. Loop owner loss takes status
precedence over trajectory-audit degradation while both facts retain their existing meanings.

Unavailable delivery metadata invokes no business work and makes no heartbeat or settlement call. It quarantines,
latches negative health, and drains only the exact owner through the existing `drainOnce`. This adds no public state,
metric family, bucket, communication path, or supervisor. Task 10 still owns the broader stop-admission, await-Closed,
cancel, and join proof.

The loop's fixed BackOff `[30s,2m]` requires `MaxDeliver >= len(BackOff)`, currently at least 2. Omitted or zero
MaxDeliver defaults to 2; explicit 1 refuses before allocation. BackOff is never truncated and restart-safe loop work
does not advertise a single-delivery posture.

## Observed AGENT replay admissibility

`MaxAge` alone cannot prove a recovery horizon while `MaxBytes` plus `DiscardOld` may evict required evidence early.
The current shipped posture therefore cannot advertise bounded restart-safe continuation under capacity pressure.

Each recovery-dependent agentic owner calls pure repo-internal
`internal/agentstreamadmission.ObserveAndValidate` after resolving its own admitted port facts and before its own
first dependent consumer, subscription, observer, worker, or publisher success. Model, dispatch, governance, and loop
derive stream identity only from their resolved `component.PortFacts`; no factory name, raw JSON, shared maxima, or
another component's configuration is read. Dispatch admits its resolved AGENT outputs before consuming USER input.
First-party AGENT publishers admit their resolved output and propagate refusal through their existing source outcome.
A non-agentic component imports nothing, performs zero lookup, and remains independently startable.

Each owner builds its local `Requirement` only from resolved AckWait, BackOff, MaxDeliver, maximum local processing
and replay need, and required producer PubAck dependency. Loop timeout informs only loop's local requirement.
Approval lifetime and `AGENT_LOOPS` TTL/capacity are excluded from stream admission and are validated only by the
loop-owned bucket gate below. The strongest local requirement refuses only that dependency closure.

The validator reads actual StreamInfo and requires `DiscardNew`, MaxAge covering the locally computed horizon and
safety margin, and no MaxMsgs, MaxMsgsPerSubject, or other observed bound that can evict required evidence earlier.
It retains no context, starts no goroutine, mutates no stream, and writes no durable or process state. Refusal is typed
`agent_stream_replay_inadmissible`, names stream plus observed/required fields, leaves the affected closure not ready,
and allocates or positively settles nothing in it.

`DiscardNew` trades availability under a full stream for non-loss: a capacity refusal from required JetStream
publication makes the producer Retry and retain its source delivery. There is no core-NATS fallback. Deterministic
`Nats-Msg-Id` deduplicates only inside the configured server duplicate window; semantic identity and adopter
idempotency remain load-bearing after longer downtime.

The owner selected the strong `DiscardNew` contract. Existing `DiscardOld` deployments require an explicit migration
before restart-safe admission. Copying every turn to Store is rejected as checkpoint-like material without an
ordinary-lane failpoint.

External purge, administrative deletion, and storage loss remain operator data-loss events outside either guarantee.

A missing configured continuation Store does not silently downgrade approval safety. An approval-required result
remains unsettled and retries while health reports the exact unresolved storage instance.

### Loop-owned `AGENT_LOOPS` acquisition

AGENT replay admission and loop-state authority acquisition are separate gates. Agentic-loop resolves its bucket from
the admitted `loops` KV-write port, then calls internal
`processor/agentic-loop/internal/loopbucket.AcquireOwner`. The helper calls `KeyValue` first and calls
`CreateKeyValue` only when `errors.Is(err, jetstream.ErrBucketNotFound)`. Permission, timeout, transport, and every
other lookup error return with zero create mutation. A typed `jetstream.ErrBucketExists` create race permits exactly
one KeyValue retry before observation.

Creation declares History 10, TTL 24h, and non-binding MaxBytes. After get, create, or race-get, the helper observes
actual status/backing stream and requires exact History 10, exact TTL 24h, and MaxBytes `<=0`. It publishes the handle
and starts task/response/tool-result/signal/approval/verdict consumers and the approval sweeper only after that
observation succeeds. Retained or race-winning drift is refused without update or reconciliation because earlier
eviction may already have destroyed authority. Approval timeout is validated against this observed bucket only.

### First-party rule publisher admission

The publisher addendum proves six enabled rule-processor configurations declare a local JetStream output covering
`agent.task.*`; five name it `agent.task` and `configs/agentic.json` names it `agent_task`. Four configurations load
eleven first-party `publish_agent` definitions. The current exact-subject classifier sends every concrete substituted
subject down core NATS even though composition correctly recognizes that explicit stream `AGENT` subject `agent.>`
covers the declared wildcard. Static composition connectivity is therefore not durable publication proof.

An affected rule processor derives activation and stream identity only from its own resolved `PortFacts`. It locates
the declared local AGENT task output by its resolved facts and preserves its configured name, including
`agent_task`; it does not hard-code one name, inspect another component, scan factory names, or parse global raw JSON.
Before its action evaluator can execute `publish_agent`, it invokes the same
`internal/agentstreamadmission.ObserveAndValidate` function used by the other affected closures with a caller-local
producer requirement. A rule processor without that resolved dependency performs zero lookup. This is one shared
read-only validator with separate caller-local invocations, not a second gate or API.

At execution, the existing `actionPublisher` classifies the fully substituted concrete subject by calling
`component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)` in that exact directional order. A covered
subject uses `PublishToStream` and waits
for PubAck; an uncovered or malformed subject, missing publisher, or refused AGENT dependency fails before
publication and action-specific post-send effects. The six existing classifier surfaces are tested; all four
configurations with static definitions prove the real producer-to-row-15 path. The two declaration-only
configurations prove a future loaded definition cannot regress to core NATS. Composition continues to own graph-level
connectivity. `component/flowgraph` owns the matcher; graph-level composition is its existing caller and connection
owner. This change reuses the exact function and adds no competing matcher or composition rule.

`publish_agent` continues to construct and validate registered `agentic.TaskMessage`, wrap it in `BaseMessage`, and
publish bytes. `TaskMessage` implements `message.Payload`, not `graph.Graphable`; publisher admission requires the
registered payload contract and SHALL NOT invent a Graphable requirement. Repository-wide wire-envelope census and
unrelated publisher migration remain #1158.

## Adopter seam

| External person | What they must know | If they do nothing | Discovery | What they should have to know |
|---|---|---|---|---|
| Component author | Return the owner-specific domain outcome; do not settle native messages | Void/log-only success can ACK incomplete work and fails review | typed API and compile/test failure | The definition of done only; the private owner handles ACK/Retry/Terminate/Quarantine |
| Tool executor author | Raw external executors preserve RequestID and execution identity; provider CallID is not globally unique | Replay is refused loudly when correlation is missing | payload validation and migration doc | Hosted executors know nothing; agentic-tools stamps correlation |
| Approval UI developer | Submit LoopID, execution identity, and decision | Durable read-through resolves replacement; conflicts are typed refusals | HTTP/bus typed error | Public approval input only; no Store, subject, digest, or timeout arithmetic |
| Model operator | Set only `provider_ambiguity_policy`; default may conservatively report commit-unknown | Omission safely makes no second call; unsupported reconciliation refuses boot | config/schema validation, typed `failure_kind`, readiness, docs | Semantic policy only; no settlement or recovery-bucket mechanics |
| Framework operator | Register `approval_continuation_storage_instance` (default `objectstore`) and admissible stream/bucket policy | Affected closure refuses readiness rather than silently degrading | boot error with exact observed/required fields | Supply the Store name and capacity; framework observes horizons and policy |
| Governance author | Supply approve/reject policy only | Late verdict remains recoverable; invalid conflict is refused | typed outcome and telemetry | No waiter, subject, or replay mechanics |
| Rule author | Use `publish_agent` with valid task fields and subject | An uncovered or inadmissible output refuses before send | typed rule-load/runtime error and rule-agent publishing docs | No port spelling, stream policy, wildcard, or PubAck mechanics |
| Sister-repository developer | Preserve registered envelopes and opaque correlation on raw NATS seams | Old code remains unchanged until its owner performs the recorded migration | SemStreams-owned migration document | Final contract and local migration list, not branch choreography |

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

They return a typed domain outcome through the subscription's admitted owner. They do not call ACK or NAK directly
or understand replay storage.

### Framework operator

They register the named Store used for approval continuation and configure provider ambiguity policy. There is no
supervisor, recovery mode, checkpoint interval, or outbox knob.

### Governance author

They supply approve or reject semantics against framework-generated proposal identity. Subject construction,
identity validation, cold correlation, and settlement belong to the framework.

## Verification and landing sequence

The #1146-owned tranche of #1155's real-NATS matrix kills the process rather than calling Stop, restarts against the
same file-backed NATS,
and proves convergence plus invocation/publication counts at: dispatch task and response PubAcks; loop/graph birth;
model before call, during call, after return, after response PubAck, and before source ACK; response persistence and
every next publication; tool-result persistence, continuation verification, next publication, and source ACK;
approval decision, tool PubAck, and pending clear; cancel state/`COMPLETE_`/terminal PubAck; governance proposal,
verdict, and tool publication; projection hydration; and representative NATS restart.

Four parallel proof groups cover all ten physical fast subscriptions, but each subscription independently blocks its
last legitimate cooperative dependency through 25s, proves cancellation and all joins before 30s, proves Retry/no
ACK and no concurrent delivery, then exercises its exact fallback only if cooperative work truly exceeds 25s. A
sibling never migrates because it shares a group.

Additional proof covers exact acquisition-config validation, affected-closure admission, non-agentic zero lookup,
resolved-stream overrides, DiscardNew capacity refusal, loop-bucket absence/match/races/drift/status failures,
registered continuation round trip and canonical equality, Store failures and eviction, lifecycle drain/Closed/join,
focused race/integration/contract tests, schema generation, and serialized `task e2e:agentic`.

PR #1159 keeps `Closes #1146`, `Refs #759`, `Refs #1155`, `Refs #1249`, and `implemented-by: Sol`, and states that
its replacement matrix is the #1146-owned tranche. It does not carry `Closes #1155`. #1155 remains open until #1249
lands AgentRun complete/failed proof and the later combined gate; final default-branch PR #1156 then owns closing
authority.

Implementation and proof precede implementation review, then owner-requested cross-agent review, fixes and repeated
review, OpenSpec archive as the final content commit, and narrow archive/current-spec-sync review. Hosted CI,
undraft, remote-base verification, and non-default merge follow those OpenSpec tasks. The staged merge creates
checkpoint `A` for #1249. PR #1251 remains the retained #1239 authorship/review record; PR #1159 records integration;
final default-branch PR #1156 carries the closing authority.

## Documentation ownership

#759 owns `docs/concepts/33-semantic-settlement.md` and its message-pump, lease-watchdog, owner-defined done, replay,
and disposition explanation. #1146 links it and documents only provider ambiguity, approval continuation, strong
AGENT admission, loop-state acquisition, metrics, and raw external-executor migration. It corrects false restart
claims in concepts 03, 17, and 27; reconciles model 60s and loop 15s heartbeat defaults in operator docs, schemas, and
fixtures; removes paused vocabulary from `agentic/README.md`, `processor/agentic-loop/README.md`,
`docs/concepts/13-agentic-systems.md`, `docs/operations/migration-beta162-to-beta163.md`, and generated
`specs/openapi.v3.yaml`; and documents boot order as resolved AGENT admission → observed `AGENT_LOOPS` acquisition →
dependent work.

## Measurable premises

The design is rejected or revised if any premise fails:

1. Exact frozen parent `F=417beae5552f8f15ad3540edd7d8504c87174c13` supplies the accepted permanent
   `DeliveryResult` foundation. Implementation begins from exact post-#1251 checkpoint
   `P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`; PR #1159 targets the non-default parent and does not require #759
   to merge first.
2. The #1146-owned tranche of #1155 proves replacement rather than same-process reconstruction; #1155 remains open
   for #1249 AgentRun complete/failed proof and the later combined gate.
3. Request identity uniquely and deterministically binds LoopID and logical turn.
4. Tool execution identity is a digest of RequestID, provider CallID, and ordinal; provider CallID is unchanged.
5. Cold `ToolResult` handling reconstructs its active batch without scanning.
6. The observed AGENT admissibility contract requires `DiscardNew` and does not infer a horizon from MaxAge while
   an earlier capacity-eviction bound remains possible.
7. Approval replacement succeeds after deliberate AGENT eviction by resolving the registered Store reference.
8. Governance replacement reads the exact retained verdict or safely re-obtains it. Failure returns for new design.
9. `fail_commit_unknown` produces zero second provider calls; `at_least_once` records repeated attempts explicitly.
10. No process-only map is required to classify a delivered source after replacement.
11. Only complete dispatch projection and configured approval-deadline repair enumerate current loop state.
12. AgentRun complete/failed settlement is transferred to #1249, which begins from exact post-#1146 staged
    checkpoint `A` and receives separate inventory, design, implementation, and replacement review.
13. The post-#1251 signal surface contains cancel only; ApprovalResponse remains separate, while
    `ResponseAction.Signal` and `ClassifiedIntent.SignalType` do not own durable signal settlement.
14. Each of ten physical fast subscriptions independently proves 30s AckWait / 25s work-and-join / 5s margin or only
    that subscription migrates to its accepted heartbeat fallback.
15. `AGENT_LOOPS` is observed as exact History 10, TTL 24h, and non-binding MaxBytes before loop work starts.
16. At P, `agentic/constants.go:11-25` has no `approval_continuation` category, while
    `agentic/payload_registry.go:26-55` is the single agentic registration owner and already assigns control floors
    to approval/control payloads. The new category therefore extends that owner without collision.
17. At P, `agentic/types.go:169-178` has no `failure_kind`; `processor/agentic-model/config.go:13-18` has no provider
    ambiguity key. Both are outward contract additions and must migrate schema, fixtures, and docs atomically.
18. At P, `processor/agentic-loop/config.go:56` and `processor/agentic-loop/README.md:113` scope
    `trajectory_evidence_storage_instance` to full trajectory evidence. Approval continuation therefore uses the
    separate exact key `approval_continuation_storage_instance` rather than borrowing unrelated retention ownership.
19. `component/flowgraph/flowgraph.go:381-389` defines the canonical directional matcher as
    `SubjectCovers(filter, pattern)` and identifies graph-level composition as its current caller. Rule publication
    passes declared filter first and concrete substituted subject second; it adds no matcher.
20. The conflicting current requirements are exactly `openspec/specs/agentic-dispatch/spec.md:72`,
    `openspec/specs/agentic-tools/spec.md:435`, `openspec/specs/agentic-tools/spec.md:467`,
    `openspec/specs/agentic-tools/spec.md:487`, and `openspec/specs/agentic-loop/spec.md:788`; their full MODIFIED
    blocks preserve unaffected scenarios and citations.

## Risks and unproven claims

- Exact retained-message lookup APIs and performance are not yet implemented or measured for every AGENT subject.
- The accepted inventories do not separately admit strong retention for the USER and TOOL source streams used by
  physical rows 1, 11, and 17. Implementation review must measure their actual source-delivery guarantees and prove
  their observed bounds sufficient for the complete 17-lane horizon. AGENT admission is not proof for another
  stream.
- Model provider adapters have not demonstrated `provider_reconcile`; `fail_commit_unknown` remains the safe default.
- Governance deterministic output fingerprint and exact committed-output lookup are target state, not current truth.
- `AGENT_LOOPS` matching/race/drift behavior is target state. Its existing create literal is not observed authority;
  a retained sibling creator is a collision to prove through the foreign-config race test, not a reason to reconcile.
- The approval Store failpoint has not yet proved deliberate AGENT eviction recovery end to end.
- The 25s work/5s join boundary has not yet passed real-NATS proof. A failing physical subscription must use its
  admitted bounded fallback; no sibling inherits that result. Governance has only an accepted heartbeat ceiling of
  15s, so a concrete interval requires conditional review if a governance subscription triggers fallback.
- Affected-closure admission, canonical continuation equality, and all new exact replay lookups are target state,
  not current behavior.
- The six rule-publisher classifier surfaces currently use exact equality and the four static producer configurations
  currently take core NATS. Until the new capability proof passes, composition coverage alone is not PubAck proof.

## Stop conditions

Implementation or review stops if:

- the remote staged parent is not exact `F`, implementation does not begin from exact `P`, an inventory verification
  drifts, or the active artifacts regain merge-first or #1148 AgentRun language;
- a fast physical subscription lacks explicit AckWait 30s, enforced 25s cancellation-and-join budget, and positive
  5s margin, or fails its real-NATS boundary proof without migrating only that subscription to an admitted fallback;
- a fast lane exposes native settlement outside its private owner or adds an exported no-heartbeat API;
- model does not ship heartbeat 60s against AckWait 120s, loop does not ship heartbeat 15s against shortest BackOff
  30s, or validation observes a configuration other than the exact one passed to acquisition;
- an affected component allocates dependent work before its own resolved-stream admission, reads another component's
  configuration, uses factory names/raw JSON/global maxima, or a non-agentic component performs admission lookup;
- dispatch consumes or positively settles queued USER work after its own dependency refusal;
- a rule processor with a resolved AGENT task output starts its action evaluator before caller-local admission,
  classifies covered concrete task subjects by exact equality, publishes them via core NATS, requires `TaskMessage`
  to be Graphable, or introduces another admission gate/API;
- `paused` remains accepted by code, exported transition APIs, schemas, docs, fixtures, or persisted-state decoding;
  any compatibility shim, alias, reserved enum, migration, checkpoint, supervisor, or workflow state machine is added;
- `AGENT_LOOPS` is exposed before actual History/TTL/MaxBytes observation, a lookup error other than typed
  `jetstream.ErrBucketNotFound` creates a bucket, a typed create-exists race is not followed by one get-and-validate,
  or retained drift is silently reconciled;
- continuation comparison uses raw envelope equality or treats fresh BaseMessage UUID/timestamp as semantic change;
- any path ACKs after log-only failure, missing process correlation, panic, or unknown required publication;
- default model redelivery can invoke the provider twice, AGENT remains `DiscardOld`, or observed AGENT bounds cannot
  prove the caller-local horizon;
- USER or TOOL source retention is assumed rather than measured from the actual source stream;
- approval continuation adds a bucket, graph fact, scanner, or unregistered/raw payload;
- governance treats core-NATS publication as synchronous durable PubAck;
- a new durable primitive, supervisor, ledger, checkpoint, outbox, state-machine runtime, or CQRS path appears;
- a delivery goroutine outlives callback return/Stop, a dependency ignores cancellation, or work cannot join under
  either strict or heartbeat ownership; or
- implementation review, owner-requested cross-agent review, archive-as-final-content, and narrow archive review
  occur out of order.

## Out of scope

- AgentRun production implementation and capability deltas; #1249 owns them from checkpoint `A`.
- Arbitrary execution pause/resume. `paused` is removed rather than reserved; a future suspend-at-next-durable-
  boundary contract requires new evidence and review.
- Content-governance behavior owned by #1140.
- Framework-wide restart generalization owned by #1145.
- Exactly-once provider or arbitrary external tool effects.
- A universal replay ledger or full AGENT stream replay.
- A core-NATS fallback for declared JetStream output, raw native settlement outside a private owner, or a new exported
  no-heartbeat settlement API.

## Binding ruling conformance

This table is the active ruling-to-target authority. The historical reconciliation artifact supplies provenance but
is not required to interpret or complete any row. There are no deviations.

| Binding ruling or accepted correction | Exact active target | Conformance |
|---|---|---|
| Build and review on staged #759; do not require merge-first | `proposal.md:10`; `tasks.md:22` | exact `P`, frozen parent `F`, re-review on drift |
| Preserve all 17 physical subscriptions; transfer only AgentRun H.1/H.2 to #1249 checkpoint A | `proposal.md:21`; `proposal.md:27`; `tasks.md:295` | full scope remains additive |
| Remove paused state completely; support cancel, durable approval wait, restart from durable boundary, and lifecycle quiesce | `proposal.md:54`; `design.md:501`; `specs/agentic-loop/spec.md:23`; `tasks.md:165` | comment 5526837992 supersedes compatibility premise; no shim/migration/reserved enum |
| Keep immutable `DeliveryAttempt`; expose neither native settlement nor no-heartbeat API | `design.md:164`; `tasks.md:32`; `tasks.md:41` | private owners only |
| Start every fast physical subscription at 30s/25s/5s; fallback only the subscription whose cooperative proof fails | `proposal.md:34`; `specs/agentic-dispatch/spec.md:16`; `specs/agentic-governance/spec.md:9`; `specs/agentic-loop/spec.md:11` | owner-accepted choice is normative |
| Ship model 60s/120s and loop 15s against shortest BackOff 30s; validate before allocation | `tasks.md:26`; `specs/agentic-model/spec.md:176`; `specs/agentic-loop/spec.md:407` | invalid defaults fail setup |
| Default provider ambiguity to `fail_commit_unknown`; expose exact config/wire contract; refuse unsupported reconciliation before start | `design.md:180`; `specs/agentic-model/spec.md:48`; `specs/agentic-model/spec.md:134`; `tasks.md:74` | no second default-policy call; no unsupported start |
| Replace stale current-spec semantics through full MODIFIED blocks | `design.md:141`; `specs/agentic-dispatch/spec.md:135`; `specs/agentic-tools/spec.md:67`; `specs/agentic-loop/spec.md:419` | unaffected scenarios/citations preserved; no additive conflict |
| Use strong observed DiscardNew and refuse only the affected dependency closure | `specs/agentic-loop/spec.md:340`; `tasks.md:209` | no whole-composition/global-maxima gate |
| Admit first-party rule AGENT output through the same internal validator and existing publisher | `specs/rule-agent-publishing/spec.md:3`; `specs/rule-agent-publishing/spec.md:38` | six classifier surfaces; no duplicate gate/API |
| Use exact canonical publisher matcher in its existing direction and ownership | `design.md:614`; `specs/rule-agent-publishing/spec.md:38`; `tasks.md:227` | `component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)`; no duplicate matcher |
| Treat `TaskMessage` as registered Payload, not Graphable | `specs/rule-agent-publishing/spec.md:75` | no graph-interface prediction |
| Register exact approval-continuation payload with control floor and dedicated Store key | `design.md:277`; `design.md:286`; `specs/agentic-loop/spec.md:186`; `specs/agentic-loop/spec.md:228`; `tasks.md:137` | no trajectory-key reuse, bucket, scanner, reaper, raw decoder, or duplicate registration |
| Give every required output deterministic identity/`Nats-Msg-Id` and exact reconciliation, including validated governance output | `design.md:128`; `specs/agentic-governance/spec.md:69` | bounded dedupe is not long-horizon proof |
| Every touched consumer owner stops admission, drains handles, awaits `Closed`, then cancels/joins | `specs/agentic-dispatch/spec.md:123`; `specs/agentic-governance/spec.md:90`; `specs/agentic-model/spec.md:207`; `specs/agentic-loop/spec.md:327`; `specs/agentic-tools/spec.md:36` | lifecycle closure is capability-owned |
| #1146 settles every handler exit before #1244 declares transitions | `design.md:45`; `tasks.md:91` | no log-and-ACK terminal outcome |
| Complete only the #1146-owned tranche of #1155 and leave combined gate open for #1249 | `proposal.md:82`; `design.md:674`; `tasks.md:267` | 17 subscriptions here; AgentRun complete/failed later |
| Add no supervisor, state-machine runtime, ledger, checkpoint, outbox, CQRS path, or new durable primitive | `proposal.md:59`; `design.md:95` | existing Streams/KV/Store remain authority |
| Preserve #1239 authorship and default-branch closing authority | `proposal.md:78`; `tasks.md:281` | PR #1251 retained; PR #1156 closes |
| Review implementation, cross-agent review, archive final content, then narrow archive review | `tasks.md:285`; `tasks.md:286`; `tasks.md:287`; `tasks.md:290` | hosted landing follows archive review |
| Document the semantic-settlement concept and #1146 applications without duplicating normative authority | `tasks.md:273` | concept 33 link plus scoped corrections |
