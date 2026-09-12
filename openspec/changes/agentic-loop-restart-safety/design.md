# Design: agentic-loop restart-safe settlement

## Status

This document records the accepted #1146 contracts; it does not certify their complete implementation.
[Tasks](tasks.md) is the current execution and evidence record. Its completed checkpoints are baseline-scoped;
the combined candidate, remaining approval proof, and final review/landing gates remain open.

The detailed contracts below include the owner-approved 2026-09-12 reduction of research-rendering scope. All dated
designs and inventories, and the frozen task journal in
`history/`, are historical provenance rather than competing execution instructions. They neither revive withdrawn
proposals nor make historical tests current. Old task IDs resolve through the mapping in `tasks.md`.

The accepted rebaseline followed nested PR #1251 at
`P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`, whose merge base with the frozen staged #759 parent is exact
`F=417beae5552f8f15ad3540edd7d8504c87174c13`. P records implementation ancestry, not the current worktree HEAD.
The dispatch edge-gateway direction is owner-approved and independently reviewed. On 2026-09-12 the owner accepted
retaining its KV authority and tracker removal while withdrawing the unpublished research-rendering detour.
The subtractive slice passed independent design review at SHA-256
`9e575e1c405c59d351a05693b0752b5c10851d3fd0a0c32af92c5fc1a8c7db4a`.
The smaller decoder-preservation amendment passed review at SHA-256
`7947ae7a6310c69105c7842a20658565b4cdeb33ca2f1b05929388b981606c25`: only the research-specific branch is removed;
ordinary registered completion support is retained alongside private raw completion records.
Its implementation and proof remain in tasks R1 and R11; design review is not implementation approval.
The producer-identity prerequisite is independently reviewed and owner-accepted by #1146 comment `5575482141`.

The bounded approval-gate correction is independently reviewed and owner-accepted on 2026-09-10 by
[comment 5618375806](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5618375806).
Reviewed design SHA-256 `92a19372c2fe2f19ac30264b3525654b5b68a005847f3435681dc0e991322fc4` and inventory SHA-256
`6640c375572e2171790d7910de7663cf5928ea2b8aab99dd5c3d68ce50f197cb` remain immutable provenance. The approval
contract below and capability deltas materialize that acceptance; task 6.6's separate Store ruling remains open.

The approval-required ToolResult phase-supersession amendment is owner-approved by
[comment 5619622099](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5619622099).
It admits an exact pre-mutation authority read on that path even with a warm route, and effect-free superseded
settlement only from positive execution-specific later-phase evidence. The initial reviewed artifacts remain
unchanged; this amendment does not revoke task 6.6 or change ordinary final-result content proofs.

The sequential-chat addendum is independently reviewed and owner-accepted on 2026-09-08: optional PriorMessages and,
as a separate product choice, AutoContinue=false. Reviewed draft SHA-256
`b10470f4a67066f1dc27e3ff19688a3eea2b4352c83b5e1ed105c0d3789ec1cb` is provenance; the contract below and active
capability deltas are target-state authority. This does not accept the earlier blanket continuation-removal proposal.

## Independent chat turns

UserMessage, HTTPMessageRequest, and TaskMessage gain optional `PriorMessages []ChatMessage`, serialized as
`prior_messages,omitempty`. Omission, null, and empty mean no history. Each entry has a user or assistant role and
nonempty text content; name, tool, reasoning, and execution-only fields are absent. Generic ChatMessage and direct
AgentRequest validation remain unchanged. One private TaskMessage validator owns this subset. Dispatch routes invalid
history to its existing observable refusal before publishing work; nonempty history on commands is also refused.

AutoContinue defaults to false in typed config and schema. Ordinary no-ReplyTo submissions start an independent
execution even when another execution is active. Explicit ReplyTo or configured AutoContinue retains existing
attachment. After attachment admission, supplied nonempty history is conflicting intent and is refused before
publication or mutation. Commands needing a target require explicit loop_id under defaults; their errors say so.
Same-source redelivery resolves the committed task before reconsidering attachment and compares ordered role/content
history, treating nil and empty equally. Conflicting history uses existing correlation quarantine, not overwrite.

The adapter supplies what it displayed: user text and delivered UserResponse.Content, which can differ from raw
provider Result. Dispatch forwards this history into the existing durable task. Loop initial assembly is fresh
execution instructions, existing embedded context, prior messages, then the current prompt once. ContextManager
receives prior history once before the prompt so later tool iterations retain it. Cold reconstruction from TaskMessage
includes history; matching retained-request restoration reuses the request without reseeding. A history-bearing task
cannot attach to an execution owned by a different TaskID.

There is no transcript store, automatic recall, new payload type, retrieval API, or recovery owner. Existing transport
and provider limits apply without silent history trimming or caller-predicted size budgets. Once the task commits,
execution recovery no longer depends on the adapter or older executions. The adapter owns its displayed transcript
across its own restart. Adopters find the HTTP example and AutoContinue/command change in the migration guide and
agentic concepts guide; they need no loop identity or storage knowledge for ordinary independent chat.

Acceptance uses real NATS with started dispatch, loop, and model components and a deterministic fake provider. Turn
one completes; components stop/join and are replaced; turn two supplies the displayed exchange. A distinct LoopID,
fresh budget, exactly-once input assembly, and a response dependent on the earlier exchange are asserted. Provider
post-PubAck/pre-source-ACK replacement reuses a matching response; confirmed absence permits another call and failed
required publication never ACKs. These remain execution gates, not claims made by this accepted design.

## Accepted inventory

This design incorporates the accepted evidence checkpoints:

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
- dispatch bridge inventory `inventory-dispatch-bridge-boundary-2026-09-04.md`, base
  `79b0f29f82ce5391013f6c931fae69a28216ac93`, SHA-256
  `cf5660a3b4196324a3695dc1174dacfb804cef56e2336536d4a9f7d8f4197daa`, independent `INVENTORY PASS`, 249/249 pins.
- task/loop cardinality inventory `inventory-task-loop-cardinality-2026-09-04.md`, same base, SHA-256
  `afd93139bc520651c3432fc00df792cab12afc426fb9666439228d15d58be8d1`.
- provider-settlement inventory `inventory-task3-provider-settlement-2026-09-05.md`, base
  `78d5498649b09711eecfe77ba3196110ca00eab8`, SHA-256
  `97a679b79b297796b3ee071a451eaf4e967ebba3afa684ba2ef7d3cd3f4bc668`, independent `INVENTORY PASS`, 144/144 pins.
- producer LoopID inventory `inventory-task-producer-loop-id-2026-09-07.md`, base
  `af829616305afa039dac0550efa78d07e856dd5f`, SHA-256
  `7b273f91996e71df860226c83d615691e6b08de0fa0153c7c5d4869a53a78c26`, independent `INVENTORY PASS`, 69/69 pins.
- producer LoopID design checkpoint `design-task-producer-loop-id-2026-09-07.md`, SHA-256
  `70ca0bb503465c76dce06a08f0be21a88870f184a1d0ebdfc77d88ff14c8818f`, independent `DESIGN REVIEW PASS`, accepted
  by owner ruling #1146 comment `5575482141`.
- approved dispatch edge-gateway checkpoint `design-dispatch-edge-gateway-2026-09-04.md`, current SHA-256
  `d26c0667692e5b5a6e3950f5b097966c17d2750b90aaeb8e54d2873a564275b5`, independent `DESIGN REVIEW PASS`.
  SHA-256 `339cf2b2c734ef48a2898ce6b79c3783577a8b4ae152b65a1078b00445949b76` is superseded provenance.

The dated artifacts preserve reviewed reasoning, intermediate proposals, owner-ruling records, and evidence at
their recorded baselines. Proposal, this design, tasks, and the eight capability deltas are the active authority.
Historical files and their pinned hashes remain unchanged. A changed baseline or touched surface requires refreshing
the affected inventory before its evidence is reused; historical acceptance is not current implementation proof.

## Holds

- PR #1159 remains stacked on `codex/gh759-semantic-settlement`; #759 does not merge first. The reviewed parent stays
  frozen through #1159 implementation and review. Any advance requires a new pin, rebase, inventory verification,
  test, and re-review.
- #1146 owns its 15-subscription tranche of #1155's real-NATS process-replacement proof. #1155 remains open until
  #1249 supplies transferred AgentRun complete/failed proof; the combined 19-subscription gate is completed later.
- The 15-subscription scope remains intact. Dispatch `agent.created` and `agent.approval_pending` correctness inputs
  are removed; their loop outputs and external subscribers remain. AgentRun complete/failed fanout is transferred to
  #1249 from exact post-#1146 checkpoint `A` and is not implemented or specified here.
- Nested PR #1251 is integrated: pause/resume handlers, request fields, and unused signal verbs are gone. Binding
  product ruling #1239 comment `5526837992`, linked from #1146 comment `5526837994`, supersedes the earlier review
  premise that `LoopStatePaused` should remain legacy-valid. Cancel and ApprovalResponse remain separate durable
  lanes; `paused` is removed from state vocabulary, API, schema, docs, and persisted-value acceptance with no shim,
  alias, reserved enum, or migration. `ResponseAction.Signal` and `ClassifiedIntent.SignalType` remain outside
  durable `agent.signal.*` settlement.
- Governance policy content remains #1140; framework-wide pattern generalization remains #1145; #1244 follows #1146
  and designs declared transitions against the settled handler exits.
- No native settlement enters business work. The only non-heartbeat transport export is the narrow shared
  `SettleDelivery` interpreter; no work-owning adapter, deadline policy, consumer owner, or lifecycle API is added.
  The recorded pre-implementation review does not replace review of subsequent implementation changes.
- The framework-owned rule `publish_agent` producer that feeds row 15 is part of this vertical. Its six configured
  classifier surfaces and four statically loaded producer configurations use the existing rule publisher plus the
  same repo-internal AGENT admission validator; no second gate or exported API is introduced.
- Every `TaskMessage` producer fixes LoopID before validation and marshal. New work mints producer-local v4 UUID;
  continuation work echoes an admitted existing token. `TaskMessage.Validate` returns an ordinary field-naming error;
  rule, intake, and direct-handler operation boundaries classify it before side effects. Agentic-loop performs no
  absent-ID mint or recovery. The change adds no helper API, compatibility path, scan, map, ledger, bucket, or second
  owner, and does not reopen tasks 9.5–9.7.

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

### Use streams-first recovery with an evidence-gated continuation exception

This uses exact reads at the named recovery and authority boundaries. Approval storage remains conditional on a real
replacement proof and second owner ruling. It preserves the current orchestration layers and is the recommended
design.

### Fix absent task identity at the producer

Doing nothing permits exact redelivery of one empty-ID task to birth a second loop after replacement. Deterministic
consumer derivation couples TaskID and LoopID and changes the random-token contract. A consumer scan, mapping, ledger,
or bucket creates a second owner for a fact that fits in retained bytes. An exported constructor cannot replace raw
TaskMessage decoding, and a helper wrapping `uuid.NewString()` adds surface without enforcement. The accepted option
makes each task-production execution mint once before marshal and makes the retained bytes authoritative for their
own downstream retry/redelivery.

## Decision

Adopt streams-first, lane-specific settlement and replay.

Each consumer holds its source delivery until its lane-specific durable business outcome and every required
downstream PubAck have completed. Replacement recovery uses:

1. source redelivery;
2. stable TaskID, framework-minted LoopID, RequestID, and tool execution identity;
3. exact reads only at named provider, tool-effect, governance-waiter, approval-evidence, and terminal-route
   boundaries;
4. read-through hydration of current `LoopEntity`;
5. existing immutable `TOOL_CALL_OUTCOMES`; and
6. current `LoopEntity.PendingToolResults` plus exact current request/response evidence for the approval gate.

The change adds no generic supervisor, recovery state machine, checkpoint or outbox bucket, CQRS path, or
event-sourced loop.

For task birth, producer-local `uuid.NewString()` is the framework mint seam. Every TaskMessage has nonempty canonical
LoopID before publication. The same already-marshaled publication and downstream retained-AGENT redelivery reuse its
bytes; fresh upstream producer execution is a separate production attempt outside this identity-reuse claim.
`TaskMessage.Validate` owns ordinary requiredness/form validation; rule publish, loop intake, and direct HandleTask
boundaries classify failure as invalid before side effects. Task intake never manufactures or recovers absent identity.

The owner withdrew a universal work deadline derived from AckWait in #1146 comment `5530950829`. JetStream consumer
configuration owns AckWait and redelivery. A component may apply an ordinary `context.WithTimeout` only where its
actual operation requires a business timeout. Every delivery-derived task joins before settlement. A physical
subscription uses the existing heartbeat owner only when measured legitimate work can exceed its configured
acknowledgement interval. Cancellation-ignoring or non-returning work remains a lifecycle failure and is never
heartbeat evidence.

The shared decision skills resolve as follows:

- `kv-or-stream`: no new communication path. Work remains on JetStream; current loop facts remain in existing KV.
  The evidence gate decides whether the already-approved Store fallback is needed.
- `entity-or-bucket`: no new fact is added before the evidence gate. If retained, approval continuation remains
  private Store material referenced by existing `AGENT_LOOPS`, never a graph fact or new bucket.
- `orchestration-check`: settlement, authority-backed projection, and approval timer repair are component execution and
  lifecycle behavior. Rules still trigger and components execute; there is no third orchestration layer.

## Cost

- A text-only model turn adds no durable write.
- An ordinary tool turn adds no bucket or Store write. Cold recovery may use one or two exact AGENT reads.
- An approval-required turn adds no Store write if the exact-evidence gate passes. If it fails, the approved Store
  fallback retains its previously reviewed cost.
- Dispatch maintains one `AGENT_LOOPS` projection rather than a second tracker.
- Approval timeout performs a bounded hydration of awaiting-approval records when configured.
- Ordinary delivery recovery never scans all loops or the whole AGENT stream.

## Correlation is lane-scoped

- Every TaskMessage producer supplies LoopID before validation and marshal. For new work, that execution mints a random
  v4 UUID locally; for continuation, it echoes the admitted existing token. Dispatch derives stable TaskID from
  validated `UserMessage` identity and already retains the mapping. Rule `publish_agent` joins that shape. Retry of
  the same marshaled publication and downstream redelivery reuse retained bytes; upstream producer re-execution is
  outside the reuse claim.
- Every `AgentRequest` carries a stable RequestID for the provider-work boundary.
- Provider `ToolCall.ID` remains conversational data. The framework stamps a separate execution identity from
  RequestID, provider CallID, and positive call ordinal for tool completion and approval/governance correlation.
- Ordinary created, request, approval, continuation, terminal, validated, verdict, ToolResult, and user-response
  publications are durable at-least-once. Their source ACK waits for required PubAck.
- `Nats-Msg-Id` is bounded duplicate suppression, not durable identity or long-horizon publication proof.
- Exact retained reads exist only where a named boundary needs them: provider invocation, completed tool effects,
  governance waiter loss, approval reconstruction, explicit LoopID/terminal-route lookup, or durable applied-state
  proof. There is no general exact-output layer or canonical-output fingerprint system.

A conflicting TaskID-to-LoopID, RequestID, execution identity, or boundary-specific correlation is quarantined. The
framework does not manufacture identity for every ordinary publication.

## Current-spec reconciliation

Six current requirements across four capabilities conflict with the accepted restart contract and are replaced in
full rather than shadowed by additive text:

- `agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone` retains its exact
  heading and unaffected scenarios, but makes `AGENT_LOOPS` authority, removes `LoopTracker`, makes projection-only
  existence a refusal, and makes `paused` invalid.
- `agentic-tools / Tool-call completion SHALL be durable before request acknowledgement`,
  `Tool-result bounds SHALL be observed rather than predicted`, and
  `Executor panic and ambiguous pre-completion effects SHALL be explicit` retain observed-bounds and panic behavior
  while replacing provider CallID keys and blanket external-effect idempotency with framework execution identity,
  bounded `Nats-Msg-Id` suppression, exact completed-outcome reads at the executor boundary, and operation-specific
  effect authority.
- `agentic-loop / Per-loop in-process state is released at terminal, through the one release point` retains the one
  idempotent release point and result/sweeper behavior, but replaces unconditional quiet settled-drop with
  lane-specific durable applied proof for ordinary final-tool/model responses, positive phase-supersession proof
  for approval-required statuses, and observable, effect-free settlement of approval decisions whose gate is no
  longer current. Process absence proves none of these outcomes. Unresolved authority retries; required-correlation
  conflicts retain their existing refusal classification.
- `entity-id-contract / A loop instance token is a framework-minted UUID` is removed and replaced by the ADDED
  `A loop instance token is minted at its framework birth seam` requirement. It preserves form, admission, HTTP,
  AgentRun,
  research, and carrier behavior while making the durable TaskMessage producer the birth mint seam and requiring
  LoopID before accepting-boundary side effects.

## Provider at-least-once recovery

Provider invocation is durably at-least-once. Before each provider invocation, agentic-model performs the
operation-specific exact read for retained `agent.response.<requestID>`.

A validated response matching the stable RequestID and expected response correlation is reused. The provider is not
called, and the source may be positively acknowledged because the required response already has durable publication
evidence. A conflicting retained response quarantines before provider invocation. A typed not-found result permits a
provider call with the same RequestID, including on redelivery after an ambiguous process stop. A lookup failure is
not absence and retries without invoking.

The staged settlement foundation supplies immutable `DeliveryAttempt` through `DeliveryWork`. Natsclient constructs
it from native message metadata before invoking work; agentic-model receives delivery number, metadata availability,
and redelivery classification only. It receives no native message, settlement method, sequence, consumer identity,
header, or mutable state and adds no model-private wrapper.

Agentic-model validates `HeartbeatDeliveryPolicy` against the exact acquisition config before allocating the
consumer. Missing metadata prevents work, produces typed `delivery_metadata_unavailable`, quarantines without
positive settlement, and stops the exact owner.

After a provider returns, every required success or provider-error `AgentResponse` receives synchronous JetStream
PubAck before source ACK. If the process stops before that PubAck becomes durable, replacement repeats the retained
response check. When no matching response exists, it may call the provider again. This is the accepted duplicate-risk
boundary; no exactly-once claim is made.

A pre-invocation started marker is prohibited because it cannot prove whether the provider ran. No ambiguity policy,
commit-unknown response kind, provider reconciliation interface, endpoint-capability enumeration, ledger, outbox,
supervisor, or replay-admission dependency is introduced. Stable RequestID remains available to provider
implementations that independently support idempotency.

## Loop cold recovery

Ordinary deliveries do not enumerate all loops.

A response, result, or signal follows the sequence below. An approval response first validates its echoed
ExecutionID against exact current loop authority; only a matching pending gate proceeds to reconstruction.

1. derives or reads LoopID from its typed identity;
2. loads the exact `AGENT_LOOPS/<loopID>` record;
3. initializes only that loop's process indexes;
4. reconstructs context and configuration from the latest committed `AgentRequest` and exact originating
   `AgentResponse`;
5. validates the incoming lane correlation against that material; and
6. performs the lane-specific transition and publishes any required output at least once.

A later committed request or terminal state may prove that an older delivery was already applied. Absence of
process memory alone never proves staleness. Approval's separately defined inapplicable outcome proves only that
the submitted decision cannot act on the current gate; it does not prove historical application.

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

### The decision names the reviewed execution gate

Use existing framework ExecutionID as the opaque gate identity. `ApprovalPendingEvent` and the existing pending
HTTP projection expose it; `ApprovalResponse` and HTTP `ApprovalRequest` require `execution_id`. The client echoes
the value attached to the prompt the human reviewed, never computes it or fetches a replacement identity when an
old prompt is submitted. The timeout publisher echoes the identity of its expired pending snapshot.

Dispatch exact-reads current authority and compares the echo before publishing; loop compares again when applying.
Missing HTTP identity returns 400, a noncurrent displayed gate returns 409 without publication, and missing direct
wire identity terminates as invalid. No omitted or outdated echo is replaced with the current gate's identity.
The existing durable pending Put precedes pending-prompt publication; a failed Put prevents that publication.

After payload validation and an exact current LoopEntity read, a matching pending ExecutionID enters the existing
pending/request/response validation and decision branch. Conflicting CallID or other required correlation within
that matching gate quarantines without clearing pending state. Missing, unreadable, or malformed loop authority
retains the existing unresolved/absence/poison contract; failed observation never proves inapplicability.

A valid, coherent current loop with another pending ExecutionID or no pending gate makes the decision inapplicable.
The owner records this through its refusal/skip diagnostic ownership, using a structured log and a narrow private
loop metric, then ACKs with zero business publications and zero durable authority mutations. LoopID and submitted
ExecutionID belong in the log, not metric labels; labels remain bounded. This extends private loop diagnostics,
not a public status, helper, configuration surface, or receipt. It does not claim that the identity existed, the
decision applied, its approver won, or its requested effect completed. No applied-decision audit event is fabricated.

Applicable approve/modify retains the actual approver and chosen arguments in ToolCall; reject/timeout retains
existing rejection provenance. Inapplicable input neither overwrites provenance nor changes authority. The retained
source records a submitted decision, not proof of its successful application.

There is one logical approval gate per execution. Retry reconstructs that gate; replay after closure must not
reopen it. A new human review after closure requires a new execution. New model requests already produce distinct
execution identities when providers reuse CallID, preserving ordinary multi-turn chat. This identity condition does
not itself provide cross-owner exclusion: existing same-gate single-resolution and provenance obligations remain.
A measured failure of those obligations returns for design before adding coordination or retained decision state.

The existing nested `PendingApprovalInfo` gains `execution_id`, carried from observed pending authority through
LoopInfo JSON and OpenAPI. All unrelated DTO fields and projection contracts remain unchanged. This correction
does not authorize tracker retirement or unrelated projection work.

### Approval-required ToolResult phase supersession

This paragraph owns only ToolResults classified `approval_required`, not ordinary final results or ApprovalResponse.
Before accumulator, pending-tool, trajectory, restoration, or gate mutation, the existing component delivery owner
uses `readLoopEntityRevision` to exact-read and validate current authority, including on a warm execution route.
Existing originating-response and latest-request reads remain the only admitted stream evidence; no scan,
CallID-indexed lookup, receipt, or new authority is added.

Positive later-phase evidence must identify the same RequestID, ExecutionID, ordinal, provider CallID, tool, and
loop, with existing normalization and correlation checks. Admissible evidence is:

- a coherent closed-gate checkpoint retaining the exact original gated result with pending cleared;
- a validated post-gate result for that execution retained in current LoopEntity; or
- a later retained request whose validated originating assistant batch and ordinal-selected tool message record
  that execution's progression beyond the gate phase.

The first predicate depends on the writer invariant that an unseen gated sibling is never merely accumulated
under another open gate. Map membership, missing pending state, a different RequestID, or unequal result content
alone is not positive evidence. A matching open gate or contradictory required correlation prevents supersession.
Unresolved phase observation retries; malformed or conflicting evidence retains existing refusal classification.
The existing reconstruction path still durably fails with `continuation_unavailable` when required retained
evidence is confirmed absent. This amendment does not turn that confirmed-absence outcome into indefinite Retry.

Positive supersession logs the exact execution, increments a private unlabeled counter in the existing loop metrics
owner, and ACKs without business publication, durable authority mutation, accumulator replacement, or a fabricated
applied-decision event. This is substrate settlement telemetry; execution identifiers belong only in the log, and
the approval-decision inapplicability counter is not reused for a ToolResult. It means only that this
approval-required status has been overtaken by a validated later phase. It proves neither the historical winning
approver nor completion of every later effect, and never makes arbitrary unequal final results interchangeable.

A matching pending gate still requires its declared prompt publication and PubAck. Reconstruct that publication
from the persisted pending snapshot without resetting its identity, RequestedAt, or timeout and without rewriting
the gate merely to replay the prompt. A genuinely new eligible gate retains the existing pending-before-prompt
ordering. An unseen different approval-required sibling returns Retry/refusal before insertion or other mutation;
it cannot become false consumed-gate evidence after the current gate closes.

Any new pending-state write is conditional on the exact observed revision, using existing KV Update/CAS before
publication. A lost revision retries without publishing the speculative gate or falling back to unconditional Put.
Stale process restoration must not regress committed closure. Reuse existing owner synchronization; any necessary
local critical section must be explicit, bounded, and tested. No lock registry, coordinator, runtime, or new durable
state is authorized. If existing primitives cannot satisfy the stale-observation proof, return the exact failing
boundary before introducing another mechanism.

### Evidence gate before mechanism

The already-approved ObjectStore design is not revoked by this design pass. Implementation first proves whether
retained framework evidence already reconstructs the approval boundary.

The real-NATS gate settles an approval-required `ToolResult`, persists `PendingToolResults` and awaiting-approval
state, and obtains PubAck for `ApprovalPendingEvent`. It then replaces agentic-loop and agentic-dispatch, discards all
process maps and caches, retains `AGENT` and `AGENT_LOOPS`, and independently exercises approve, modify, reject,
timeout, and redelivery. Matching-gate decisions prove their declared transition and no duplicate tool execution;
noncurrent-gate decisions prove observable, effect-free settlement, not the historical winning decision.

Reconstruction uses only current `AGENT_LOOPS/<LoopID>`, latest exact-subject `agent.request.<LoopID>`, and the exact
`agent.response.<RequestID>` named by that request. It performs no stream list or scan.

A provider-authored CallID is request-scoped, not globally unique. No `tool.result.<CallID>` lookup is admissible.
The approval-required result was already persisted at
`LoopEntity.PendingToolResults[PendingApproval.ExecutionID]`. The current response must contain exactly one tool call
matching the pending provider CallID. Every envelope and payload validates, and all available loop, task, request,
execution, ordinal, call, tool, argument, and trace identities agree.

A same-CallID/different-RequestID proof retains two conflicting responses. For a matching pending ExecutionID,
reconstruction follows only the response named by the current request and is unaffected by the older response.
An older decision carrying another ExecutionID cannot select the current call even when provider CallID is reused.

| Matching-gate evidence result | Settlement |
|---|---|
| transport/read failure, unobservable retention, or unresolved visibility | Retry; publish nothing |
| observed retention confirms required exact evidence should remain, but it is absent | durable `continuation_unavailable` |
| malformed envelope, duplicate current call, or identity/content conflict | Quarantine |
| exact validated match with no durable applied-state proof | publish the required branch output at least once, then continue |
| durable state proves the branch already applied | settle without repeating non-repeatable work |
| required correlation conflicts with durable state | Quarantine |

No applicable branch clears `PendingApproval` before its required next publication receives PubAck or durable
applied-state proof exists. An inapplicable decision clears nothing. If all branches pass without a new durable
fact, implementation stops for an owner ruling. A pass under the revised inapplicable-decision semantics is not
proof of the superseded historical applied-decision claim. Only
explicit revocation of comment `5463183450` authorizes deletion of `ApprovalContinuationV1` and its Store plan. If
the proof fails, retain that already-approved plan unchanged. No third mechanism is introduced.

## Dispatch edge-gateway boundary

Dispatch is exclusively an edge gateway. It:

1. admits external work and publishes task, cancel, and approval messages;
2. answers explicit LoopID operations from exact `AGENT_LOOPS` authority;
3. serves reverse lookup, listing, debug, activity, and AutoContinue from one caught-up read-only view; and
4. translates terminal complete/failed outcomes into durable user responses only when a validated user route exists.

Agentic-loop exclusively owns loop birth, pending approval, every intermediate transition, and terminal state.
Dispatch neither consumes created/pending events for correctness nor communicates intermediate loop state. A valid
system-lane terminal outcome with no user route is settled without inventing a `user.response`.

`LoopTracker`, its approval buffer, and dispatch's `agent.created` and `agent.approval_pending` inputs are removed.
Those events remain loop outputs for external subscribers. The existing shared graph view becomes the sole local
projection and adds no storage or retention.

### Current authority and existing activity

Retain tracker removal and one existing shared view. Bare canonical LoopID keys are the current-authority domain.
Their values must validate as `LoopEntity` with key/ID equality; malformed current values poison authority until
a greater-revision clean write or tombstone heals them. Other non-completion keys are outside that domain and
are excluded without optional-producer namespace knowledge. This explicitly withdraws the added requirement that
unknown noncanonical keys poison current-loop authority; it does not weaken validation of current-loop values.

Preserve existing ordinary `COMPLETE_` activity, its field mappings, and completion key/identity validation.
Completion never supplies current-loop authority. Unsupported or malformed completion values remain observable
activity errors rather than fabricated results, and do not block current-loop authority. Research completion
rendering is not part of this acceptance; its registered payload, producer, and #1288 ownership remain unchanged.
Add no renderer interface, shared record package, additional view, compatibility decoder, or completion owner.

The runtime reduction removes only the unpublished `SearchResult` concrete-type branch and its optional research
import. Both ordinary completion representations retain their current validation and mapping behavior: private raw
terminal records and registered terminal envelopes. Registry decoding, explicit payload validation, terminal-family
validation, canonical suffix agreement and field mappings remain unchanged, including the ordinary registered
decode/marshal/projection sequence. Replacing that sequence is not a rescue prerequisite. Native registry-bound
stream terminal decoding is unchanged. No decoder fallback or validator relaxation is introduced. The nine research
producer/prefix edits are withdrawn, not moved into a new shared package.

### AutoContinue

AutoContinue defaults to false. When explicitly enabled, it matches only exact `(UserID, ChannelType, ChannelID)`.
Empty or partial matches and cross-channel
fallback never match. Zero current nonterminal matches creates work, one continues it, and more than one refuses as
ambiguous. Bootstrap, watcher loss, relevant poison, or unreadable authority retries durable intake and returns 503
to HTTP.

The gap after task PubAck and before first `LoopEntity` birth remains explicit. A second route-only AutoContinue
request in that gap may create a second task. Callers requiring continuity use explicit LoopID. No route-claim bucket
or prediction mechanism is added.

### Command, HTTP, metric, and lifecycle seams

`CommandContext.LoopTracker` becomes the narrow `LookupLoopOwner(context.Context, LoopID)` operation and returns only
LoopID and UserID with classified invalid, absent, missing-owner, invalid-record, and unavailable outcomes. It exposes
no raw entity, KV handle, bucket, tracker, or generic query.

`LoopInfo` remains only as an immutable view-derived `/loops` and `/debug/state` DTO. `/debug/state` returns 503 when
projection truth is unavailable and exposes readiness/poison diagnostics. `router_active_loops` is removed with no
Prometheus replacement; the loop gauge remains local telemetry, not durable authority.

The accepted edge-gateway checkpoint's value-mapping exception remains explicit: `context_request_id` keeps its
optional JSON field but is empty because it was never durable or an authority field. This is distinct from the
later nested pending `execution_id` schema addition. Every other unrelated DTO field and mapping is preserved.

Exported `graphview.View.Restart()` and its retained context closure are removed without replacement. Dispatch's
lifecycle-control goroutine stops and recreates a failed view with its active run context and joins it during Stop.

Terminal response reconstruction is bounded by the intersection of complete/failed source retention and exact loop-
state retention. Route facts must agree. Transient absence retries; deletion, purge, expiry, or eviction reports
`terminal_route_unavailable`. Process memory never fabricates a route.

## Governance correlation

Governance stays in the existing rule and component layers.

Each proposal carries LoopID, RequestID, execution identity, and a proposal fingerprint. Verdict subjects use the
NATS-safe execution identity. A replacement response handler first checks for an exact matching retained verdict
before publishing a proposal again.

A verdict arriving without a process waiter is validated and remains recoverable; it is not silently discarded as
completed work. No governance bucket is admitted unless a real replacement failpoint proves that retained verdict
lookup and response redelivery are insufficient. Policy content and feature expansion remain #1140.

## Per-lane definition of done

`non-heartbeat binding` means the existing private callback retains the native message, business work returns a typed
decision, and the callback passes that completed decision to `SettleDelivery` only after work joins. No message or
settlement closure enters business work. `heartbeat owner` means staged
`ConsumeDeliveryWithHeartbeat` owns metadata, renewal, and the terminal method. Every owner stops admission, drains
its retained handle, awaits exact `Closed`, then cancels and joins its own observer and work goroutines.

| # | Physical subscription and owner | Happy-path done | Sad-path settlement | Durable authority and replacement |
|---:|---|---|---|---|
| 1 | dispatch `user.message`; fast | required task, cancel, approval, or user-response publication receives PubAck; refusal response receives PubAck without projection mutation | permanent invalid after response → Terminate; transient lookup/marshal/publish → Retry; required-correlation conflict/panic → Quarantine | source MessageID; stable TaskID-to-random-LoopID mapping; exact LoopID authority or caught-up AutoContinue view |
| 4 | governance `task_validation`; fast | completed policy; allowed output receives JetStream PubAck; blocked means deliberate non-forwarding | invalid → Terminate; filter/resolution/marshal/publish uncertainty → Retry; correlation conflict/panic → Quarantine | source correlation; allowed outputs are at-least-once |
| 5 | governance `request_validation`; fast | same row-4 contract for `AgentRequest` | same as row 4; core-NATS publish never proves done | RequestID; allowed outputs are at-least-once |
| 6 | governance `response_validation`; fast | same row-4 contract for `AgentResponse` | same as row 4 | RequestID; allowed outputs are at-least-once |
| 7 | loop `agent.signal`; fast, cancel-only after #1251 | current cancellation state and `COMPLETE_` commit; terminal event receives PubAck | invalid/unknown → Terminate; missing live authority/KV/publication → Retry; conflict/panic → Quarantine | LoopID and exact current loop state distinguish live missing from durable terminal proof |
| 8 | loop `agent.approval_response`; fast | matching gate applies its branch and clears pending only after PubAck or durable applied proof; noncurrent gate records log plus metric and ACKs without business publication or authority mutation | invalid including missing ExecutionID → Terminate; unresolved authority/evidence/publication → Retry; panic or matching-gate correlation conflict → Quarantine | exact coherent current LoopEntity decides gate applicability; matching-gate reconstruction uses exact retained request/response; task 6.6 still governs Store |
| 9 | loop `agent.toolcall.approved`; fast | verdict reaches waiter or remains recoverable for response replay | invalid → Terminate; retained lookup unavailable → Retry; mismatch/panic → Quarantine | execution identity, proposal fingerprint, exact retained verdict at waiter-loss boundary |
| 10 | loop `agent.toolcall.rejected`; fast | same row-9 contract for rejection | same as row 9 | same as row 9 |
| 11 | tools `tool.execute`; heartbeat | immutable completed outcome exists and ToolResult receives PubAck | permanent invalid → Terminate; transient Store/publish → Retry; collision → Quarantine | `TOOL_CALL_OUTCOMES` is the executor-effect boundary; completed replay invokes no executor; result publication is at-least-once |
| 12 | dispatch `agent.complete`; heartbeat | retained terminal source and exact current loop route agree; routed user response receives PubAck, or validated no-user-route outcome settles | invalid → Terminate; unreadable authority/publish → Retry; route conflict → Quarantine | SourceMessageID, LoopID, exact loop route; response publication is at-least-once |
| 13 | dispatch `agent.failed`; heartbeat | same row-12 contract for failure | same as row 12 | same as row 12 |
| 14 | model `agent.request`; heartbeat | matching retained AgentResponse is reused, or a newly invoked provider response receives PubAck | invalid/endpoint permanent → error response or Terminate; retained lookup failure → Retry; correlation conflict/metadata/panic → Quarantine | RequestID; retained match prevents another call; typed absence permits another call; responses are at-least-once |
| 15 | loop `agent.task`; heartbeat | loop/graph birth or continuation commits; initial/next request and created/refusal outputs receive PubAck | invalid → Terminate; safely repeatable dependency failure → Retry; TaskID/LoopID conflict, impossible partial birth, or panic → Quarantine | stable TaskID-to-random-LoopID mapping, `AGENT_LOOPS`, and graph identity; ordinary events are at-least-once |
| 16 | loop `agent.response`; heartbeat | turn is hydrated and resulting loop/terminal/next output commits with PubAck | invalid → Terminate; missing correlation/dependency → Retry; conflict/panic → Quarantine; durable applied proof → ACK | RequestID, current loop, originating request/response, and lane-specific applied-state proof |
| 17 | loop `tool.result`; heartbeat | ordinary result persists and required next output receives PubAck; approval-required status first reads authority and may ACK effect-free on positive phase-supersession proof | invalid → Terminate; unresolved phase observation or unseen gated sibling → Retry before mutation; conflict/panic → Quarantine; confirmed required retained absence → existing durable continuation_unavailable; ordinary final-result proofs unchanged | RequestID, execution identity, exact current loop including warm approval-required delivery, admitted request/response evidence; conditional gate writes; Store remains task 6.6-gated |

AgentRun complete/failed subscriptions are not rows 18/19 here. #1249 owns their separate post-#1146 contract and
replacement proof from checkpoint `A`.

### Non-heartbeat settlement boundary

The eight-row production-binding matrix captures the callback installed by every actual setup branch and exercises it
through its real business handler and deepest controllable dependency. A lane label or generic settlement test is not
branch proof. Business work uses the callback context, applies only its own operation-specific timeout, and joins all
delivery-derived work before returning a decision. The private callback then calls `SettleDelivery` and reacts to
`OwnerStopRequired` through its existing admission, health, and exact-handle drain path.

`SettleDelivery` validates the closed decision/error tuple and attempts Ack, immediate Nak, Term, or no settlement.
It reads no payload or metadata, invokes no work, creates no context or goroutine, derives no deadline, sends no
heartbeat, and owns no consumer lifecycle. AckWait, BackOff, MaxDeliver, and missing-settlement redelivery remain
JetStream consumer configuration. Heartbeat adoption requires measured evidence for that physical subscription.

### Heartbeat-policy migration

- Model keeps AckWait 120s and changes default heartbeat 90s → 60s.
- Loop task/response/tool-result keep BackOff `[30s,2m]` and change default/schema heartbeat 60s → 15s.
- Tools remain 5s against shortest BackOff 15s; dispatch terminal remains 10s against effective AckWait 30s.

Validation uses the exact acquisition config and occurs before allocation. With BackOff, the shortest positive value
is the effective acknowledgement interval; otherwise positive AckWait or the server 30s default is effective.
Heartbeat may be at most half that interval. Errors name component, port, observed heartbeat, effective interval, and
ceiling. Config structs, defaults, generated schemas, docs, and every example/test fixture migrate together.

### `user.message` task

Happy-path done is a stable TaskID-to-random-LoopID mapping, `agent.task` PubAck, and user acknowledgement PubAck.
Invalid or unauthorized input terminates only after user-error PubAck. Transient publication failure retries and an
ordinary output may repeat.

### `user.message` command

Happy-path done is PubAck for the required signal or approval publication and the user response. Invalid commands
terminate after user-error PubAck. Dependency failure retries.

### `agent.task`

Happy-path done is matching `LoopEntity` persistence, required graph birth, and PubAck for the initial
`AgentRequest` and `LoopCreatedEvent`. Permanent rejection becomes a durable terminal loop outcome once the
TaskID-to-LoopID mapping exists. Transient assembly, storage, or publication failure retries. Mapping conflict
quarantines. Ordinary request and created-event publication is at-least-once.

### `agent.request`

Happy-path done is an already committed matching `AgentResponse` or PubAck for a newly produced matching response.
A retained match prevents provider invocation; a conflicting response quarantines; typed absence invokes the provider
with the same RequestID, including on redelivery. Pre-invocation permanent errors become typed error responses.
Invalid envelopes terminate.

### `agent.response` complete or error

Happy-path done commits every settlement-required terminal effect—`COMPLETE_<loopID>`, required synthetic effects,
and required terminal-event PubAck—and then the bare terminal `LoopEntity` Put as the final lane-applied marker.
Best-effort trajectory audit and the existing atomic completion/failure graph batch, including evidence-integrity
condition evidence, remain nonblocking and are not marker prerequisites. A pre-marker failure discards speculative
process-local loop state and retries from the prior
nonterminal `LoopEntity` plus exact retained request. Ordinary terminal effects may repeat. During the brief
COMPLETE/event-before-terminal-LoopEntity window, the lane is not settled and cross-surface readers must retry rather
than continue or discard. Permanently malformed responses terminate. Unknown correlation quarantines unless a
committed later output proves prior application.

### `agent.response` tool call

Happy-path done is resolved governance where enabled, stable execution identities, persisted current loop state, and
PubAck for the first tool request or next request. Store or publication failures retry. Conflicting required
correlation quarantines. Ordinary tool and request publication is at-least-once.

### `tool.execute`

Happy-path done follows #949: an immutable completed outcome exists and exact `ToolResult` has PubAck. Existing
CallID or external-effect ambiguity remains operation-specific; #1146 adds no second ledger.

### `tool.result`

Happy-path done is hydrated loop, reconstructed originating request and response, persisted result, and PubAck for
the next queued tool, request, approval event, or terminal event. Missing continuation during a live turn retries. A
bare terminal `LoopEntity` does not prove which `ToolResult` applied; cold tool-result delivery retries until task 5
supplies execution-specific applied proof. Conflicting execution identity quarantines and never log-and-drops.

Approval-required statuses additionally follow the narrow phase-supersession contract above: exact authority is
read before mutation even on warm delivery, and validated later-phase evidence permits effect-free superseded ACK.
This does not weaken ordinary final-result content matching or license ACK from bare terminal state.

### `agent.signal`

Happy-path done is persisted current-state transition. Cancel additionally requires `COMPLETE_` and terminal event
PubAck. Invalid signals terminate. A missing live loop retries within its retention horizon; a terminal duplicate
ACKs from committed state.

### `agent.approval_response` approve or modify

For a matching pending ExecutionID, happy-path done is exact pending-call validation, correlated `ToolCall` PubAck,
and durable approval-state transition. Pending clears only after required PubAck or durable applied-state proof;
ordinary tool publication may repeat. Preserve the actual approver and chosen arguments. Matching-gate conflict
quarantines; publication failure retries without clearing pending state. A validated noncurrent gate instead records
the inapplicable log and metric and ACKs without business publication, authority mutation, or applied-provenance claim.

### `agent.approval_response` reject or timeout

Happy-path done is applied synthetic `ToolResult`, PubAck for the resulting request or terminal event, and pending
state cleared only after that durable outcome, for a matching pending ExecutionID. Preserve existing rejection
provenance. Transient dependency failure retries. A validated noncurrent gate records the inapplicable log and metric
and ACKs without business publication, authority mutation, or a claim that this historical decision applied.

### Governance verdict

Happy-path done is validated proposal identity and delivery to the waiter or exact retained verdict recoverable by
response replay. A missing waiter is not silently ignored. Invalid or mismatched verdict terminates or quarantines.
Audit-mode observability remains nonblocking.

## Settlement terms

- ACK means the source's required durable transition or result and every required downstream PubAck have completed.
- Retry means the lane's declared correlation, durable authority, and side-effect policy make another attempt safe.
- Terminate means the payload is permanently invalid and no useful retry exists.
- Quarantine means an identity collision, impossible correlation, panic, or invariant failure prevents a safe
  decision.

No handler converts decode, correlation, KV, Store, provider, or publish failure into successful callback return.

## User-facing control and quiesce contract

The supported execution controls are cancel and durable ApprovalResponse wait. A safe retry or process replacement
continues from the last proven durable boundary through redelivery and lane-specific recovery; it does not restore an
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
| one TaskID cannot select two LoopIDs | `agentic-dispatch / Dispatch task redelivery recovers the committed LoopID` |
| one RequestID cannot select different provider work | `agentic-model / Model request settlement is bound to a durable response` |
| one framework execution identity cannot select different tool work or completed outcome | `agentic-tools / Completed tool outcome identity is globally unambiguous` |
| ordinary publications are at-least-once and ACK waits for required PubAck | owner-specific settlement requirements |
| `Nats-Msg-Id` supplies bounded suppression only | owner-specific publication requirements |
| missing process state never proves stale or complete | `agentic-loop / All six loop input classes settle after owner-specific durable done` |
| ACK follows every required durable effect and PubAck | `agentic-loop / All six loop input classes settle after owner-specific durable done` |
| matching retained response makes zero provider calls | `agentic-model / Model request settlement is bound to a durable response` |
| retained response absence permits another provider invocation with the same RequestID | `agentic-model / Model request settlement is bound to a durable response` |
| approval reconstruction is current-request scoped and conflict detecting | `agentic-loop / Approval continuation after replacement is exact and evidence-bounded` |
| an approval echoes the reviewed ExecutionID; one execution has one logical gate, never reopened after closure | `agentic-loop / Approval continuation after replacement is exact and evidence-bounded` |
| a noncurrent approval ACK is observable and effect-free, not historical applied-decision proof | `agentic-loop / Approval continuation after replacement is exact and evidence-bounded`; `agentic-loop / Per-loop in-process state is released at terminal, through the one release point` |
| HTTP approval never substitutes the current identity for a missing or outdated echo | `agentic-dispatch / Dispatch uses one authority-backed current-state projection` |
| partial projection never licenses AutoContinue | `agentic-dispatch / Dispatch uses one authority-backed current-state projection` |
| observed DiscardOld cannot satisfy strong recovery | `agentic-loop / Restart-safe replay observes and admits local stream bounds` |
| every delivery task joins before result and Stop | `agentic-loop / Delivery work joins before settlement`; owner-specific shutdown requirements |
| first-party rule task output is durable before row 15 | `rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication` |
| fatal delivery-owner loss is visible and drains only its exact handle | dispatch, governance, model, and loop owner requirements |
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

Dispatch, governance, model, and loop reuse the existing agentic-tools problem shape. The first fatal delivery-owner
result is latched synchronously before the owner-stop observer can drain its exact consume handle. Existing component
health reports
`Healthy=false`, status `delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the
existing error count. Later fatal results neither overwrite nor recount the first cause. Dispatch replaces per-lane
fatal aggregation with one component-wide first cause. Governance counts owner loss once independently of prior
business-error counts. Loop owner loss takes status precedence over trajectory-audit degradation while both facts
retain their existing meanings.

Unavailable delivery metadata invokes no business work and makes no heartbeat or settlement call. It quarantines,
latches negative health, and drains only the exact owner through the existing `drainOnce`. This adds no public state,
metric family, bucket, communication path, or supervisor. Task 10 still owns the broader stop-admission, await-Closed,
cancel, and join proof.

The loop's fixed BackOff `[30s,2m]` requires `MaxDeliver >= len(BackOff)`, currently at least 2. Omitted or zero
MaxDeliver defaults to 2; explicit 1 refuses before allocation. BackOff is never truncated and restart-safe loop work
does not advertise a single-delivery posture.

## Observed AGENT replay admissibility

`MaxAge` alone cannot prove a recovery horizon while `MaxBytes` plus `DiscardOld` may evict required evidence early.
The inventoried DiscardOld posture therefore could not support that recovery claim under capacity pressure.
The accepted observed-admission contract below remains subject to the current proof gates in tasks.

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
publication makes the producer Retry and retain its source delivery. There is no core-NATS fallback. `Nats-Msg-Id`
deduplicates only inside the configured server duplicate window and is never long-horizon publication proof. Beyond
that window, ordinary publications may repeat; named non-repeatable-effect boundaries use their own durable
authority and correlation.

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
eleven first-party `publish_agent` definitions. At that inventory checkpoint, exact-subject classification sent
concrete substituted subjects down core NATS even though composition recognized that explicit stream `AGENT`
subject `agent.>` covers the declared wildcard. Static composition connectivity is not durable publication proof;
the corrected publisher path still requires the complete R8 evidence.

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

`publish_agent` mints one producer-local v4 LoopID before it validates the registered `agentic.TaskMessage`, wraps it
in `BaseMessage`, and publishes bytes. `TaskMessage` implements `message.Payload`, not `graph.Graphable`; publisher
admission requires the registered payload contract and SHALL NOT invent a Graphable requirement. This identity step
does not change admission, subject coverage, publisher classification, PubAck, or registry ownership in tasks
9.5–9.7. Repository-wide wire-envelope census and unrelated publisher migration remain #1158.

## Adopter seam

| External person | What they must know | If they do nothing | Discovery | What they should have to know |
|---|---|---|---|---|
| Component author | Return the owner-specific domain outcome; do not settle native messages | Void/log-only success can ACK incomplete work and fails review | typed API and compile/test failure | The definition of done only; the private owner handles ACK/Retry/Terminate/Quarantine |
| Direct TaskMessage producer | New work mints LoopID once before Validate/marshal; continuation echoes admitted LoopID | Missing/noncanonical LoopID is refused before side effects | validation error, classified boundary, migration note | One birth/echo rule; no stream, bucket, scan, or recovery mechanics |
| Tool executor author | Raw external executors preserve RequestID and execution identity; provider CallID is not globally unique | Replay is refused loudly when correlation is missing | payload validation and migration doc | Hosted executors know nothing; agentic-tools stamps correlation |
| Approval UI developer | Submit LoopID, decision, and the opaque ExecutionID attached to the reviewed prompt | Missing echo receives HTTP 400 or invalid wire settlement; a stale HTTP prompt receives 409 | typed HTTP/wire refusal | Echo an observed value; never compute identity or select a replacement gate |
| Model operator | Know that ambiguous replacement may repeat a provider call | Omission requires no configuration; retained matches are reused and typed absence calls again | model operations documentation | Only the at-least-once duplicate-risk contract; no policy, reconciliation, or failure-kind mechanics |
| Custom command author | Use `LookupLoopOwner` with LoopID | Old field removal is a compile failure | compile error and migration note | Only LoopID and returned owner |
| HTTP/UI author | Existing `LoopInfo` wire gains only nested pending `execution_id`; projection endpoints can return 503 | Ordinary reads continue; approval inputs without the echo fail visibly | typed HTTP response and schema | No bucket, watcher, or cache |
| AutoContinue caller | Opt into exact user/type/channel matching | No partial or cross-channel fallback | schema/docs and typed ambiguity/unavailable response | Only whether convenience is wanted |
| Dashboard operator | Remove `router_active_loops` query | Series disappears | metric migration note | Use `/loops` with caught-up signal |
| graphview caller | Do not call removed `Restart()` | Compile failure | compile error and migration note | Lifecycle owner recreates the view |
| Framework operator | Configure admitted stream/bucket policy | Affected closure refuses readiness rather than silently degrading | boot error with observed/required fields | No recovery Store unless the evidence gate retains it |
| Governance author | Supply approve/reject policy only | Late verdict remains recoverable; invalid conflict is refused | typed outcome and telemetry | No waiter, subject, or replay mechanics |
| Rule author | Use `publish_agent` with valid task fields and subject | An uncovered or inadmissible output refuses before send | typed rule-load/runtime error and rule-agent publishing docs | No port spelling, stream policy, wildcard, or PubAck mechanics |
| Sister-repository developer | Preserve registered envelopes and opaque correlation on raw NATS seams | Old code remains unchanged until its owner performs the recorded migration | SemStreams-owned migration document | Final contract and local migration list, not branch choreography |

### Approval UI developer

They submit LoopID, decision, and the opaque ExecutionID attached to the reviewed prompt. Exact pending state
resolves CallID only after the echo matches. They do not carry Store references, construct NATS subjects, compute
execution identity, or reconstruct arguments. Missing echo returns 400; a stale displayed gate returns 409.

### Tool executor author

Executors hosted by `agentic-tools` receive and return ordinary tool values. The framework stamps RequestID,
execution identity, and continuation correlation. A raw external NATS executor must preserve those opaque fields and
receives a loud migration failure if it does not.

### Model adapter operator

They configure no SemStreams provider-ambiguity policy. Provider invocation is durably at-least-once, and an
ambiguous replacement may call again when no matching response is retained. The stable RequestID is available for a
provider implementation's own idempotency support.

### Component author

They return a typed domain outcome through the subscription's admitted owner. They do not call ACK or NAK directly
or understand replay storage.

### Direct TaskMessage producer

For new work, it calls `uuid.NewString()` once before Validate and marshal. For continuation, it echoes the admitted
existing LoopID. It retries the same uncertain publication with the same serialized bytes and does not reconstruct the
task or regenerate LoopID for that retry. Missing or malformed identity fails before side effects. No exported helper,
constructor, generator policy, recovery map, or storage concept is exposed.

### Framework operator

They configure admitted stream/bucket policy. No approval Store surface ships unless the replacement evidence gate
fails and the owner retains the approved fallback. There is no supervisor, recovery mode, checkpoint interval, or
outbox knob.

### Governance author

They supply approve or reject semantics against framework-generated proposal identity. Subject construction,
identity validation, cold correlation, and settlement belong to the framework.

## Verification and landing sequence

The #1146-owned tranche of #1155's real-NATS matrix kills the process rather than calling Stop, restarts against the
same file-backed NATS,
and proves convergence plus invocation/publication counts at: dispatch task and response PubAcks; loop/graph birth;
model before call, during call, after return, after response PubAck, and before source ACK; response persistence and
every next publication; tool-result persistence, approval-evidence verification, next publication, and source ACK;
approval decision, tool PubAck, and pending clear; cancel state/`COMPLETE_`/terminal PubAck; governance proposal,
verdict, and tool publication; caught-up-view recovery; and representative NATS restart.

The task-birth slice retains the exact registered bytes emitted by rule `publish_agent`, replaces agentic-loop, and
redelivers those same AGENT bytes. It proves one TaskID-to-LoopID mapping and one loop identity. It does not claim
identity stability across a fresh execution of the upstream rule action.

The eight-row production-binding matrix proves each physical non-heartbeat subscription's actual setup branch,
callback, business handler, and deepest controllable dependency. A shared settlement truth table proves every valid
decision, invalid tuple, nil message, and terminal-method error without wall-clock timing. Focused tests prove
approval panic, first-fatal health, exact-handle drain, and synchronous graph-write join. Heartbeat adoption remains
separate and requires measured evidence that the physical subscription can cross its configured acknowledgement
interval.

Additional proof covers exact acquisition-config validation, affected-closure admission, non-agentic zero lookup,
resolved-stream overrides, DiscardNew capacity refusal, loop-bucket absence/match/races/drift/status failures,
approval replacement evidence and same-CallID isolation, current-authority/activity separation, lifecycle drain/Closed/join,
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
and disposition explanation. #1146 links it and documents only provider at-least-once recovery and duplicate risk,
approval continuation, strong
AGENT admission, loop-state acquisition, metrics, and raw external-executor migration. It corrects false restart
claims in concepts 03, 17, and 27; reconciles model 60s and loop 15s heartbeat defaults in operator docs, schemas, and
fixtures; removes paused vocabulary from `agentic/README.md`, `processor/agentic-loop/README.md`,
`docs/concepts/13-agentic-systems.md`, `docs/operations/migration-beta162-to-beta163.md`, and generated
`specs/openapi.v3.yaml`; and documents boot order as resolved AGENT admission → observed `AGENT_LOOPS` acquisition →
dependent work. It also documents producer-owned TaskMessage LoopID, same-marshaled-publication retry, downstream
retained-byte redelivery, the direct sister migrations, and the absence of any empty-ID compatibility path or consumer
recovery owner.

## Measurable premises

The design is rejected or revised if any premise fails:

1. Exact frozen parent `F=417beae5552f8f15ad3540edd7d8504c87174c13` supplies the accepted permanent
   `DeliveryResult` foundation. Exact post-#1251 checkpoint
   `P=09ba38b1de5e7200e72281c8e4b8941d81be1da2` records the implementation's starting ancestry, not its current
   HEAD. PR #1159 targets the non-default parent and does not require #759 to merge first.
2. The #1146-owned tranche of #1155 proves replacement rather than same-process reconstruction; #1155 remains open
   for #1249 AgentRun complete/failed proof and the later combined gate.
3. RequestID stably identifies one logical provider-work turn and retains its LoopID correlation.
4. Tool execution identity is a digest of RequestID, provider CallID, and ordinal; provider CallID is unchanged.
5. Cold `ToolResult` handling reconstructs its active batch without scanning.
6. The observed AGENT admissibility contract requires `DiscardNew` and does not infer a horizon from MaxAge while
   an earlier capacity-eviction bound remains possible.
7. Approval replacement either succeeds from current `LoopEntity` plus exact current request/response evidence for
   every applicable decision branch and observable effect-free settlement of noncurrent gates, or the approved Store
   fallback remains unchanged after owner ruling. The revised gate does not prove historical decision provenance.
8. Governance replacement reads the exact retained verdict or safely re-obtains it. Failure returns for new design.
9. A matching retained response produces zero provider calls; typed absence permits another call with the same stable
   RequestID, including after ambiguous replacement.
10. No process-only map is required to classify a delivered source after replacement.
11. One caught-up dispatch view and configured approval-deadline repair observe current loop state; no tracker or
    second correctness projection exists.
12. AgentRun complete/failed settlement is transferred to #1249, which begins from exact post-#1146 staged
    checkpoint `A` and receives separate inventory, design, implementation, and replacement review.
13. The post-#1251 signal surface contains cancel only; ApprovalResponse remains separate, while
    `ResponseAction.Signal` and `ClassifiedIntent.SignalType` do not own durable signal settlement.
14. The production-binding matrix proves each of eight physical non-heartbeat subscriptions and the shared settlement
    truth table proves its transport mapping. No deadline is inferred from AckWait; heartbeat requires measured need.
15. `AGENT_LOOPS` is observed as exact History 10, TTL 24h, and non-binding MaxBytes before loop work starts.
16. Provider CallID is not globally unique. Approval reconstruction performs no CallID-indexed stream lookup and
    proves same-CallID/different-RequestID isolation. Decision inputs echo existing ExecutionID, and replay must
    preserve one logical approval gate per execution without reopening a closed gate.
17. At P, no provider ambiguity config, commit-unknown failure kind, or provider reconciliation seam exists; the
    simplified target preserves that absence.
18. The earlier `SearchResult` activity/token projection is withdrawn from #1146 and is not a landing gate.
19. `component/flowgraph/flowgraph.go:381-389` defines the canonical directional matcher as
    `SubjectCovers(filter, pattern)` and identifies graph-level composition as its current caller. Rule publication
    passes declared filter first and concrete substituted subject second; it adds no matcher.
20. The previously named current-spec reconciliation set includes `openspec/specs/agentic-dispatch/spec.md:72`,
    `openspec/specs/agentic-tools/spec.md:435`, `openspec/specs/agentic-tools/spec.md:467`,
    `openspec/specs/agentic-tools/spec.md:487`, `openspec/specs/agentic-loop/spec.md:788`, and
    `openspec/specs/entity-id-contract/spec.md:651`; their full modified or replacement deltas preserve unaffected
    scenarios and citations.

That set is not an exhaustive final-sync claim. Current `agentic-terminal-events` also retains tracker, DiscardOld,
and best-effort-persistence wording. R14 must account for that adjacent wording while preserving unaffected terminal
validation, response content, routing, ancestry, retention, and telemetry scenarios. This reconciliation selects no
additional capability delta or behavioral amendment.

## Risks and unproven claims

The checklist distinguishes implemented slices from complete acceptance. These are remaining proof obligations,
not assertions that every referenced mechanism is absent:

- Exact retained-message lookup correctness and performance require evidence at each named boundary.
- The accepted inventories do not separately admit strong retention for the USER and TOOL source streams used by
  physical rows 1, 11, and 17. Implementation review must measure their actual source-delivery guarantees and prove
  their observed bounds sufficient for the complete 15-lane horizon. AGENT admission is not proof for another
  stream.
- Governance retained-verdict recovery at the waiter-loss boundary requires the complete R7 evidence. Ordinary
  validated output remains at-least-once.
- `AGENT_LOOPS` matching/race/drift behavior requires the complete R8 evidence. A create literal is not observed
  authority; a retained sibling creator requires the foreign-config race proof, not reconciliation.
- The complete approval gate-identity, noncurrent-gate, and closed-gate replay matrix remains an acceptance
  obligation. Historical checkpoints and the separate Store ruling remain in tasks; partial greens do not
  complete that matrix or revoke the Store decision.
- No physical non-heartbeat subscription has measured evidence that legitimate work crosses its configured AckWait.
  Heartbeat migration remains unavailable without that evidence and a reviewed lane-specific policy.
- Affected-closure admission, named exact reads, all six rule-publisher classifier surfaces, and all four static
  producer configurations require their complete current-candidate evidence. Composition coverage alone is not
  PubAck proof.
- R1 requires subtraction of the unpublished research-rendering detour and proof of the retained minimal
  dispatcher. The previous research-boundary design is withdrawn, not implemented or accepted by this rescope.
  The inherited research completion/readback mismatch remains #1288; the approval-Store ruling and all remaining
  settlement/proof gates are unchanged.
- The combined candidate has not passed its complete gate. R11–R15 retain verification, documentation,
  implementation/cross-agent review, archive, current-spec reconciliation, and landing obligations.

## Stop conditions

Implementation or review stops if:

- the remote staged parent is not exact `F`, implementation no longer descends from exact `P`, an inventory verification
  drifts, or the active artifacts regain merge-first or #1148 AgentRun language;
- an ordinary publication is specified or implemented as exactly-once, requires a universal committed-output
  lookup, or treats `Nats-Msg-Id` as proof beyond the configured duplicate window;
- TaskID is regenerated on redelivery, LoopID is derived rather than randomly minted for new work, or redelivery
  fails to recover LoopID from the retained TaskMessage;
- a TaskMessage boundary accepts absent LoopID, agentic-loop mints or recovers a missing task identity, a producer
  exposes a helper/configurable generator, or a scan, map, ledger, bucket, or second owner appears;
- RequestID or execution identity is imposed on publications that do not need to distinguish provider, tool, or
  proposal/verdict correlation;
- a non-heartbeat physical subscription fails production-binding proof, settles before its work joins, derives a
  universal work deadline from AckWait, or migrates to heartbeat without measured lane-specific need;
- a binding exposes native settlement to business work or adds a work-owning settlement policy, owner, or lifecycle
  API beyond the narrow stateless `SettleDelivery` interpreter;
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
- approval reconstruction performs a CallID-indexed stream lookup, scans `AGENT`, clears pending state before its
  required next publication receives PubAck or durable applied-state proof exists, or collapses transient,
  confirmed-absent, and corrupt evidence into one result;
- an approval substitutes current identity for a missing/outdated echo, resolves another execution's gate, reopens
  a closed execution, or settles inapplicable input without its log and metric, with business consequences, or with
  fabricated applied-decision provenance;
- any path ACKs after log-only failure, missing process correlation, panic, or unknown required publication;
- a matching retained response still permits provider invocation, conflicting retained correlation does not
  quarantine, typed absence does not permit another call with the same RequestID, or source ACK can precede required
  response PubAck;
- AGENT remains `DiscardOld`, or observed AGENT bounds cannot prove the caller-local horizon;
- USER or TOOL source retention is assumed rather than measured from the actual source stream;
- approval continuation implements or removes the Store fallback before the evidence gate and second owner ruling;
- dispatch retains `LoopTracker`, a pending-approval cache, created/pending correctness inputs, or another current-
  state authority beside `AGENT_LOOPS`;
- dispatch owns an intermediate loop transition, retries a validated routeless non-user terminal indefinitely, or
  hides the AutoContinue birth gap with process memory;
- the view admits noncanonical non-completion keys as current loops, accepts malformed canonical current state,
  treats completion as current authority, or lets completion-only poison block current-loop authority;
- the withdrawn research renderer, payload behavior, namespace package, or optional implementation dependency
  reappears as a restart prerequisite;
- terminal response routing claims either source or loop-state retention beyond their actual intersection;
- graphview or dispatch retains context or adds a replacement exported restart lifecycle API;
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
- A core-NATS fallback for declared JetStream output, native settlement inside business work, or a work-owning
  non-heartbeat settlement API.

## Binding ruling conformance

This table is the active ruling-to-target authority. The historical reconciliation artifact supplies provenance but
is not required to interpret or complete any row. There are no deviations.

| Binding ruling or accepted correction | Exact active target | Conformance |
|---|---|---|
| Optional PriorMessages on three inputs | Independent chat turns | Sequential-chat implementation map below |
| AutoContinue=false, separately approved | Independent chat turns | Sequential-chat implementation map below |
| Build and review on staged #759; do not require merge-first | `proposal.md / Holds`; `tasks.md / 1.1` | exact `P`, frozen parent `F`, re-review on drift |
| Remove dispatch created/pending correctness inputs; retain their loop outputs; transfer AgentRun H.1/H.2 to #1249 checkpoint A | `proposal.md / Claim scope`; `tasks.md / 6.9`; `tasks.md / Transferred: AgentRun` | 15 physical subscriptions remain in #1146 |
| Remove paused state completely; support cancel, durable approval wait, restart from durable boundary, and lifecycle quiesce | `proposal.md / Holds`; `design.md / User-facing control and quiesce contract`; `agentic-loop / All six loop input classes settle after owner-specific durable done`; `tasks.md / 7.1–7.4` | comment 5526837992 supersedes compatibility premise; no shim/migration/reserved enum |
| Keep immutable `DeliveryAttempt`; expose no native settlement to business work | `design.md / Non-heartbeat settlement boundary`; `tasks.md / 1.4–1.7` | private callbacks only; narrow `SettleDelivery` is transport-level |
| Derive no universal work deadline from AckWait; heartbeat only after measured lane-specific need | `proposal.md / Holds`; dispatch, governance, and loop owner settlement requirements; `tasks.md / 1.5–1.7` | owner ruling `5530950829` |
| Ship model 60s/120s and loop 15s against shortest BackOff 30s; validate before allocation | `agentic-model / Model heartbeat policy is valid before acquisition`; `agentic-loop / Long-running loop heartbeat policy is valid before acquisition`; `tasks.md / 1.2–1.4` | invalid defaults fail setup |
| Make provider invocation durably at-least-once with retained-response reuse and no ambiguity framework | `design.md / Provider at-least-once recovery`; `agentic-model / Model request settlement is bound to a durable response`; `tasks.md / 3.1–3.3` | comment `5550778818`; retained match reuses, conflict quarantines, absence calls again, PubAck precedes source ACK |
| Persist bare terminal LoopEntity last as the lane-applied marker for every terminal loop outcome | `design.md / Per-lane definition of done`; `agentic-loop / Loop task, request, and tool work use only required correlation`; `tasks.md / 4.2–4.3, 6.5, 7.5–7.7` | comment `5571755835`; settlement-required effects precede the marker; pre-marker failure discards speculative process state and retries; ordinary effects may repeat; bare terminal state is not generic ToolResult proof; no second owner/runtime |
| Replace stale current-spec semantics through full MODIFIED blocks | `design.md / Current-spec reconciliation`; exact MODIFIED requirements in dispatch, tools, and loop deltas; `tasks.md / 5.2, 6.10, 7.7` | unaffected scenarios/citations preserved; no additive conflict |
| Use strong observed DiscardNew and refuse only the affected dependency closure | `agentic-loop / Restart-safe replay observes and admits local stream bounds`; `tasks.md / 9.1–9.4` | no whole-composition/global-maxima gate |
| Admit first-party rule AGENT output through the same internal validator and existing publisher | `rule-agent-publishing / First-party publish-agent output is admitted before action execution`; `rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication`; `tasks.md / 9.5–9.7` | six classifier surfaces; no duplicate gate/API |
| Make the TaskMessage producer the framework birth mint seam; require LoopID and add no helper or recovery owner | `entity-id-contract / A loop instance token is minted at its framework birth seam`; `agentic-loop / Loop task, request, and tool work use only required correlation`; `rule-agent-publishing / Publish-agent preserves the registered payload boundary`; `tasks.md / 3A.1–3A.3` | comment `5575482141`; producer-local v4 before marshal; ordinary validator error plus classified accepting boundaries; same-byte downstream retry/redelivery only; task 9 remains separate |
| Use exact canonical publisher matcher in its existing direction and ownership | `design.md / First-party rule publisher admission`; `rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication`; `tasks.md / 9.6` | `component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)`; no duplicate matcher |
| Treat `TaskMessage` as registered Payload, not Graphable | `rule-agent-publishing / Publish-agent preserves the registered payload boundary`; `tasks.md / 9.5–9.7` | no graph-interface prediction |
| Evidence-gate approval continuation before choosing the already-approved Store fallback | `design.md / Approval continuation`; `agentic-loop / Approval continuation after replacement is exact and evidence-bounded`; `tasks.md / 6.1–6.6` | no CallID lookup, scan, third mechanism, or Store deletion before the second ruling |
| Echo the reviewed ExecutionID; settle noncurrent approval gates observably without effects or historical applied claims | `design.md / Approval continuation`; approval-continuation and terminal-release requirements; dispatch current-state projection requirement; `tasks.md / 6.5–6.5b, 6.8, 6.10, 7.7` | owner comment `5618375806`; one gate per execution; narrow pending JSON/OpenAPI addition; tool/model proofs and task 6.6 remain unchanged |
| Make dispatch exclusively an edge gateway over one authority-backed view | `design.md / Dispatch edge-gateway boundary`; `agentic-dispatch / Dispatch is exclusively an edge gateway`; `agentic-dispatch / Dispatch uses one authority-backed current-state projection`; `tasks.md / 6.7–6.13` | no tracker, pending cache, created/pending correctness inputs, or intermediate-state ownership; routeless terminals settle; the AutoContinue birth gap remains explicit |
| Keep correlation lane-scoped; make ordinary publications at-least-once; retain exact reads only at named boundaries | `design.md / Correlation is lane-scoped`; corresponding dispatch, loop, model, tools, and governance publication requirements; `tasks.md / 2.1–2.6` | comment `5538906152`; bounded dedupe is not long-horizon proof |
| Every touched consumer owner stops admission, drains handles, awaits `Closed`, then cancels/joins | dispatch, governance, model, loop, tools, and graph-view lifecycle requirements; `tasks.md / 10.1–10.3` | lifecycle closure is capability-owned |
| #1146 settles every handler exit before #1244 declares transitions | `design.md / Holds`; `tasks.md / 1.5–1.7` | no log-and-ACK terminal outcome |
| Complete only the #1146-owned tranche of #1155 and leave combined gate open for #1249 | `proposal.md / Claim scope`; `design.md / Verification and landing sequence`; `tasks.md / 11.1`; `tasks.md / Transferred: AgentRun` | 15 subscriptions here; AgentRun complete/failed later |
| Add no supervisor, state-machine runtime, ledger, checkpoint, outbox, CQRS path, or new durable primitive | `proposal.md / Holds`; `design.md / Decision`; capability-specific no-new-authority clauses | existing Streams/KV/Store remain authority |
| Preserve #1239 authorship and default-branch closing authority | `proposal.md / Impact`; `design.md / Verification and landing sequence`; `tasks.md / 11.4` | PR #1251 retained; PR #1156 closes |
| Review implementation, cross-agent review, archive final content, then narrow archive review | `tasks.md / 11.5–11.8` | hosted landing follows archive review |
| Document the semantic-settlement concept and #1146 applications without duplicating normative authority | `design.md / Documentation ownership`; `tasks.md / 11.3` | concept 33 link plus scoped corrections |

### Sequential-chat implementation map

The owner accepted both choices on 2026-09-08 after independent design review. No additional public surface was added.

| Accepted obligation | Implementation evidence |
|---|---|
| User and durable task history fields | `agentic/user_types.go:46`, `agentic/user_types.go:324` |
| HTTP history field and normalization | `processor/agentic-dispatch/http.go:41`, `http.go:155` |
| One private task-input subset validator | `agentic/user_types.go:457` |
| Forward history through existing task producer | `processor/agentic-dispatch/component.go:932` |
| Validate and compare retained source history | `processor/agentic-dispatch/task_recovery.go:94`, `:215` |
| History in initial request and ContextManager | `processor/agentic-loop/handlers.go:654`, `:817` |
| Reject different-task history before rebinding | `processor/agentic-loop/state.go:273` |
| Default false in typed configuration and schema | `processor/agentic-dispatch/config.go:14`, `:102` |
| Refuse history on commands and attachment | `processor/agentic-dispatch/http.go:209`, `:354` |
| Actual replaced-component chat acceptance | `processor/agentic-dispatch/sequential_chat_integration_test.go:35` |
| Actual post-PubAck provider replacement proof | `processor/agentic-model/provider_post_puback_integration_test.go:39` |
