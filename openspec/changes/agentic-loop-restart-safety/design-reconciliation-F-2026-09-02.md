# Design reconciliation: #1146 against exact settlement foundation F

## Status and evidence

This is preserved historical design evidence. The owner accepted its two open choices on #1146 comment
`5516511726`; nested PR #1251 was subsequently integrated and the active proposal, canonical design, tasks, and six
capability deltas were reconciled at checkpoint `P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`, whose merge base with the
frozen staged parent is `F=417beae5552f8f15ad3540edd7d8504c87174c13`. Those active OpenSpec artifacts are
the normative target-state authority. The exact amendment blocks below remain review provenance only, MUST NOT be
applied a second time, and independently authorize no implementation.

Owner ruling #1146 comment `5530950829` later withdrew this artifact's universal 30s/25s/5s work-deadline design
and its prohibition on the narrow stateless `SettleDelivery` transport interpreter. The canonical active artifacts
contain the replacement contract; this file remains unchanged provenance below this supersession note.

The post-#1251 surface refresh is preserved separately as
`inventory-rebaseline-2026-09-03-post-1251.md`, reviewed SHA-256
`2888e28a7439ff4dc62345bf9a1e476054c292326ac291ab1d4519f9c0600a73`, `INVENTORY PASS`, 181/181 pins. It confirms
17 in-scope physical subscriptions: row 7 is cancel-only, ApprovalResponse remains separate, and no pause/resume
handler or persisted request field survives. At checkpoint P, the inventory correctly observed `LoopStatePaused` as
legacy-valid. That target premise is superseded by binding product ruling #1239 comment `5526837992`, linked from
#1146 comment `5526837994`: active target state removes and refuses `paused` everywhere with no compatibility path.
`ResponseAction.Signal` and `ClassifiedIntent.SignalType` are not durable `agent.signal.*` settlement authority.

The later first-party publisher addendum is preserved at
`inventory-addendum-first-party-agent-publisher-2026-09-03.md`, SHA-256
`0adba4f0092017d84f1ef181ebaf3299323f5cc75b999825bd1e16d6e292930f`, `INVENTORY PASS`, 226/226 pins. Its
active target is the sixth `rule-agent-publishing` capability delta; the older five-delta amendment text below is
superseded and non-normative.

The complete active design review subsequently superseded four additional historical drafts below. Normative target
state now: (1) replaces the three conflicting current-spec requirements through full MODIFIED blocks—dispatch treats
`AGENT_LOOPS` as authority, tools keys completion by framework execution identity, and loop late deliveries require
exact applied proof rather than quiet settled-drop; (2) uses exact model config `provider_ambiguity_policy`, exact
`AgentResponse.failure_kind`, and setup refusal for unsupported `provider_reconcile`; (3) registers exact
`agentic.approval_continuation.v1` with control indexing floor and resolves it through dedicated
`approval_continuation_storage_instance` rather than the trajectory-evidence key; and (4) calls
`component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)` from the graph-level caller without duplicating
the matcher. #1146 completes only its 17-subscription tranche of #1155; #1155 remains open for #1249 AgentRun proof
and the later combined gate. Any conflicting amendment text below is retained solely as review history.

It incorporates the accepted inventory
`openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-02-F.md` unchanged:

- evidence base `F`: `417beae5552f8f15ad3540edd7d8504c87174c13`;
- inventory SHA-256: `3b53c6d3d4f3298d63ffc2231b209aa8e1f4379a6c1bf75b7aa5edc6a4f65ffb`;
- inventory materialization commit: `b755e4ff08d889055797fafd1ef98dc4a4864758`;
- independent verdict: `INVENTORY PASS`;
- pin verification: `555/555`; and
- recorded searches: `64`.

At drafting time, local HEAD is the inventory materialization commit, the merge base with `F` is exact `F`, and
`origin/codex/gh759-semantic-settlement` is exact `F`. An unexpected remote-parent advance invalidates this design
checkpoint and requires a new pin, rebase, inventory verification, test, and review cycle.

The accepted surface inventory is the mandatory first deliverable. Its complete 19-subscription matrix is at
inventory lines 274-333; rows 18 and 19 are transferred to #1249, leaving the 17 #1146 lanes reconciled below. The
accepted adopter-seam observations are restated before target state in this artifact so an external adopter never has
to infer the temporary branch-only contract.

## Accepted surface inventory

### Foundation and ownership

- The permanent typed foundation exists at `natsclient/delivery_settlement.go:16-51,153-165,229-298,348,399-443`.
  It owns immutable attempt observation, work result interpretation, heartbeat, and the one typed terminal-method
  gate.
- The branch-only legacy helper remains exported at `natsclient/heartbeat.go:76-103` and has exactly three production
  callers: AgentRun at `agentic/agentrun/agentrun.go:812`, loop at
  `processor/agentic-loop/component.go:1096`, and model at `processor/agentic-model/component.go:399`.
- Tools and dispatch terminal already use the typed owner at
  `processor/agentic-tools/delivery_owner.go:55` and
  `processor/agentic-dispatch/delivery_owner.go:55`.
- Ten fast subscriptions retain native message authority in their existing binding callbacks: dispatch ACKs at
  `processor/agentic-dispatch/component.go:558,621,697`; governance ACKs at
  `processor/agentic-governance/component.go:504`; and loop ACK/NAK at
  `processor/agentic-loop/component.go:1034-1038`.
- Every scoped owner already retains the exact native consume handle and drains it before waiting for exact `Closed`:
  dispatch `component.go:480-492`, governance `component.go:615-623`, loop `component.go:692-730`, model
  `component.go:523-531`, and tools `component.go:616-654`.

### Durable and process-only authority

- `AGENT_LOOPS` is the current loop-state authority: the port declaration is
  `processor/agentic-loop/config.go:54,380,433` and loop persistence is
  `processor/agentic-loop/component.go:2032`.
- Loop execution correlation is presently process-only in `LoopManager` maps at
  `processor/agentic-loop/state.go:61-73`; missing map entries become stale log-and-drop for model response and tool
  result at `processor/agentic-loop/component.go:1448-1450,1808-1810`.
- `TOOL_CALL_OUTCOMES` is the existing immutable completed-tool authority
  (`processor/agentic-tools/outcomes.go:21-145`; `openspec/specs/framework-bucket-catalog/spec.md:13`).
- Dispatch `LoopTracker` is process state; the existing exact-read merge is
  `processor/agentic-dispatch/loop_admission.go:320-407`.
- Pending approval facts already live in `PendingApprovalState` at `agentic/state.go:78,142`; the registered `Store`
  abstraction and registry are `storage/storage.go:51-94` and `storage/storeregistry/storeregistry.go:41-97`.
- Governance verdict waiters are process-only at
  `processor/agentic-loop/governance_dispatcher.go:334-504`.
- `ApprovalContinuationV1` has no production declaration, registration, reader, writer, or Store use at `F`; the
  inventory's exhaustive zero-result search is recorded at lines 648-656.

### Material unsafe exits

- Loop task errors at `processor/agentic-loop/component.go:1165,1171,1198,1201,1210,1388-1389`, response errors at
  `component.go:1393-1452`, and tool-result errors at `component.go:1780-1842` can return success to the outer
  settlement owner.
- Required loop publications and KV writes are log-only at
  `processor/agentic-loop/component.go:1538-1539,1879-1880,1952,1979,2004,2015-2033`.
- Model decode, resolution, and response publication failures are void/log-only at
  `processor/agentic-model/component.go:583-605,736-779,1049,1082` while the legacy callback returns nil at
  `component.go:399-402`.
- Governance parse, filter, output resolution, marshal, and publish errors return to an unconditional ACK at
  `processor/agentic-governance/component.go:349-436,502-504`.
- Dispatch handlers return void through decode, routing, publication, and response failures at
  `processor/agentic-dispatch/component.go:782-1057,1088-1199`, then ACK at `component.go:558,621,697`.
- Approval panic is erased by `err = nil` at
  `processor/agentic-loop/approval_response_handler.go:41-48`; approval decode/handle failures return void at
  `approval_response_handler.go:161-200`.
- A verdict with no waiter or a full waiter channel is dropped at
  `processor/agentic-loop/governance_dispatcher.go:483-504` before the source delivery is ACKed.

### Replay admission and collisions

- The shipped AGENT stream is bounded by `max_age=24h`, `max_bytes=268435456`, and `discard=old` at
  `configs/agentic.json:15-21`. The scoped production search for `StreamInfo(` returned zero results; the only scoped
  hit is an integration test (`inventory` lines 605-615).
- #1239's owner ruling deletes pause/resume handlers and `PauseRequested` fields. At `F`, those branches still exist
  at `processor/agentic-loop/component.go:2120-2122,2218-2296` and settle through the same raw row-7 ACK. Cancel
  remains.
- #1244 follows #1146 and declares the state arm of the same handler exits. #1146 may not encode log-and-ACK as a
  legal result, and it must leave every exit classifiable by #1244.
- AgentRun rows 18/19 are not #1146 scope. #1249 begins from exact post-#1146 staged checkpoint `A` and owns the
  outward-facing fanout contract, both binding migrations, and replacement proof.

### Reviewer corrections applied after inventory acceptance

- `natsclient/stream.go:615-624` gives a zero `MessageTimeout` an effective 30-second handler context, while
  `natsclient/stream.go:823-827` gives a zero `AckWait` an effective 30-second server lease. Equal timeout and lease
  leave zero cancellation/join margin.
- Dispatch's three raw configs omit both values at `processor/agentic-dispatch/component.go:542-556,606-619,
  681-695`, so each currently has effective AckWait 30s, work budget 30s, margin 0s.
- Governance's generic raw config omits both values at `processor/agentic-governance/component.go:482-504`; each of
  task/request/response validation therefore has effective AckWait 30s, work budget 30s, margin 0s.
- Loop gives all four fast ports AckWait 30s but uses the component's 120-second default message timeout at
  `processor/agentic-loop/component.go:997-1016` and `processor/agentic-loop/config_test.go:192`: effective margin is
  negative 90s.
- Model currently defaults to AckWait 120s and heartbeat 90s at
  `processor/agentic-model/component.go:362-372`; exact foundation `F` requires heartbeat no greater than half the
  effective AckWait, so target heartbeat remains the previously accepted 60s.
- Loop task/response/tool-result use BackOff `[30s,2m]` at
  `processor/agentic-loop/component.go:977-996`. `F` treats the shortest BackOff value, 30s, as the effective
  acknowledgement interval, so target heartbeat remains the previously accepted 15s rather than the current 60s
  default at `processor/agentic-loop/config.go:347-348`.

## Adopter seam inventory

| External person | What they must know today | If they do nothing | Where they learn it today | What they should have to know |
|---|---|---|---|---|
| Component author using the temporary #759 branch | `ConsumeWithHeartbeat` is temporary; the three-file census is only a zero-growth staging guard; semantic done belongs to the component | A new caller could compile on the branch yet prevent #759 closure and inherit nil/error semantics | Branch comment/test only; below the correctness bar | Nothing about native ACK, NAK, Term, heartbeat goroutines, or staging topology; return a domain outcome through an admitted owner |
| Tool executor author | RequestID and framework execution identity are framework correlation; provider CallID alone is not globally unique | A raw external executor that drops opaque fields prevents exact replay and is refused loudly | Typed payload validation and migration note | Hosted executors should know nothing; the component stamps and preserves correlation |
| Approval UI developer | Submit LoopID, execution identity, and decision; never carry Store references or reconstruct tool arguments | Durable read-through resolves the pending call; a conflict is typed and refused | HTTP/bus typed error | Only the public approval input; no Store, bucket, subject, digest, or timeout arithmetic |
| Model operator | Provider ambiguity has one explicit policy; default `fail_commit_unknown` can conservatively report unknown after a crash before the call | Replacement makes no second paid/effectful call and publishes typed commit-unknown | Config validation, typed response, readiness, then docs | Select semantic policy only; never choose settlement methods, retry count, or a recovery bucket |
| Framework operator | Strong recovery requires observed AGENT bounds and a configured Store for approval continuation | The shipped `DiscardOld` AGENT stream is refused for strong admission; missing Store refuses approval recovery rather than degrading | Boot/readiness error with observed and required fields | Provide capacity and Store instances; the framework computes its horizon and observes server state |
| Raw NATS tool adopter | Preserve registered envelopes and opaque correlation fields; deterministic message IDs are not unbounded dedupe | Decode/correlation fails loudly; beyond server duplicate window the adopter's idempotency remains load-bearing | SemStreams-owned migration document and typed validation | Prefer the component API and know none of the transport grammar |
| Governance author | Policy content stays #1140; proposal/verdict identity is framework-owned | A late verdict remains recoverable; an invalid or conflicting verdict is refused | Typed result and bounded telemetry | Approve/reject semantics only; no waiter, subject, or replay mechanics |
| Sister-repository developer | The permanent typed foundation is staged, not on default branch; their repository remains read-only to this change | Their old code remains unchanged; final removal cannot land until its migration is recorded and owned | SemStreams-owned migration document | The final semantic contract and their local work list, not SemStreams branch choreography |

Findings:

1. The temporary exported helper is an outward-facing debt, not an API allowlist. #1146 adds no caller and exposes no
   replacement no-heartbeat API.
2. The current strong AGENT contract cannot silently coexist with the shipped `DiscardOld` configuration. Boot must
   name the mismatch, and the owned shipped configuration must be migrated before the restart-safe claim can pass.
3. No adopter computes retention, consumer names, subjects, digests, execution IDs, or native settlement. The
   framework observes or derives each.

## Options and costs

### Option 1 — strict completion-before-lease for the ten physical fast subscriptions (recommended)

Keep native delivery authority inside each existing fast binding callback, make business work return an exhaustive
typed decision, and enforce `AckWait=30s`, maximum work budget `25s`, and cancellation/join margin `5s` on every one
of the ten physical subscriptions. The owner must cancel and join synchronously inside 25s; reaching the budget is
Retry, never ACK. This preserves the existing surface and exports no no-heartbeat helper.

Cost: every fast operation, including Store/KV and synchronous PubAck, must honor the delivery context and finish or
join within 25s. A component that cannot enforce that bound cannot use this option. Benefit: it is the smallest safe
existing surface and does not turn lease renewal into a default for bounded validation/projection work.

### Option 2 — migrate an affected fast physical subscription to the admitted heartbeat owner

If any physical subscription cannot enforce the 25-second budget plus 5-second join margin, migrate that individual
subscription to `ConsumeDeliveryWithHeartbeat` using its exact acquisition config, owner-stop latch, retained handle,
drain/Closed/cancel/join sequence, and setup-time policy validation. This is safer than pretending a blocking
dependency is fast and adds no new exported API, but it adds heartbeat control and metadata admission to lanes that
do not presently need it. Select it only when context-cooperative legitimate work is measured above 25 seconds.
Cancellation-ignoring work is a lifecycle violation under both routes and cannot justify heartbeat.

### Option 3 — export a typed no-heartbeat settlement router

This avoids component-local decision interpretation, but establishes a new exported natsclient API while #759 is
trying to retire its temporary dual surface. There is no present cross-component requirement that cannot be met by
the existing binding owners. It would require separate owner design review and adopter migration. Reject now; stop
and revisit only if implementation proves a shared semantic job rather than shared switch syntax.

### Option 4 — add a supervisor, checkpoint, ledger, outbox, or loop state-machine runtime

This creates a second authority and reconciliation lifecycle without a failpoint that existing Streams/KV/Store
cannot close. It violates the accepted orchestration boundary and delays #1244 behind machinery it does not need.
Reject.

### Option 5 — do nothing

This retains unconditional ACK, log-and-drop, commit-unknown provider duplication risk, empty-cache 404/409 behavior,
and a shipped stream posture that can evict evidence earlier than the claimed horizon. Reject.

## Recommendation

Recommend option 1 for all ten physical fast subscriptions, subject to the explicit 25s/5s boundary proofs. Semantic settlement is a message
pump with an owner-defined definition of done, not a watchdog or state machine. A heartbeat watches the lease only
when work cannot be bounded safely inside its lease; it never decides whether work is done. If any of the four loop
fast subscriptions or the six dispatch/governance subscriptions cannot pass that proof, option 2 is compelled for
that individual physical subscription only. Because the earlier accepted posture said only “do not force heartbeat” without
measuring a positive margin, this selection required—and has now received—an explicit owner ruling.

Apply the shared decision skills as follows:

- `kv-or-stream`: no new communication path. Work stays on JetStream; current facts stay in existing KV; large private
  continuation material uses the existing Store.
- `entity-or-bucket`: approval continuation is bulky, high-churn private execution material, not a graph fact.
  `PendingApprovalState` remains in existing `AGENT_LOOPS`; no new bucket or rule-readable triple is added.
- `orchestration-check`: settlement and narrow timer/projection repair are component execution/lifecycle behavior.
  Rules still trigger and components execute; there is no third orchestration layer.
- `new-payload`: `ApprovalContinuationV1` is registered through `agentic.RegisterPayloads`, reached through
  `payloadbuiltins.Register`, uses alias-based JSON methods and production decoding, declares an empty indexing
  profile and nil projections, and has no `init()` or raw fallback.

## Per-lane contract

Lifecycle notation used below:

- **fast owner**: existing binding callback retains native message authority; read-only bytes enter business work;
  the business result returns to one private exhaustive settlement switch; no native message or settlement closure
  escapes, and no exported no-heartbeat API is added.
- **heartbeat owner**: `ConsumeDeliveryWithHeartbeat` owns metadata, lease renewal, and terminal method; business work
  sees only context, immutable `DeliveryAttempt`, and read-only bytes.
- **close**: owner stops admission, drains every retained handle, awaits exact `Closed`, then cancels and joins its
  own observer/work goroutines. Callback cancellation joins before returning a settlement result.

| # | Lane and owner | Happy-path definition of done | Sad-path settlement | Durable authority, correlation, and replacement | Lifecycle and heartbeat |
|---:|---|---|---|---|---|
| 1 | dispatch `user.message`; fast owner | Task: deterministic TaskID/LoopID, task PubAck, and deterministic user-response PubAck. Command: required signal/approval PubAck and user-response PubAck. Refusal: typed user-error PubAck and no tracker/gauge mutation. | Malformed/permanently unauthorized after error PubAck → Terminate; transient lookup/marshal/publish → Retry; identity/route collision or panic → Quarantine and stop this owner. No void return authorizes ACK. | Source MessageID, deterministic task/output IDs, exact `AGENT_LOOPS` merge. Redelivery republishes identical IDs and reconciles existing loop facts. Process tracker is cache only. | Dispatch drains handle and awaits `Closed`; callback joins. AckWait 30s, enforced work budget 25s, join margin 5s. Budget exhaustion → Retry. If the proof fails, migrate this physical subscription to the admitted heartbeat owner. |
| 2 | dispatch `agent.created`; fast owner | Projection update completes, or exact `AGENT_LOOPS/<loopID>` proves the same authoritative loop is reconstructable. | Invalid envelope → Terminate; unreadable authority or projection installation interruption → Retry; conflicting owner/route/state → Quarantine. | LoopID plus current `AGENT_LOOPS`; event is notification, not authority. Replacement builds a complete snapshot before installing AutoContinue projection. | Same dispatch close. AckWait 30s, budget 25s, join margin 5s; exhaustion → Retry. Failed proof compels heartbeat migration for this subscription. |
| 3 | dispatch `agent.approval_pending`; fast owner | Projection update completes, or exact `PendingApprovalState` with matching LoopID/CallID/execution identity is readable in `AGENT_LOOPS`. | Invalid → Terminate; unreadable/missing-while-source-live authority → Retry; conflicting pending identity → Quarantine. Cache absence alone is never failure or success. | `AGENT_LOOPS` pending state plus continuation reference; source IDs update only a cache. Replacement HTTP paths exact-read authority. | Same dispatch close. AckWait 30s, budget 25s, join margin 5s; exhaustion → Retry. Failed proof compels heartbeat migration for this subscription. |
| 4 | governance `task_validation`; fast owner | Policy completes. If allowed, the exact validated task is published through the declared JetStream output with deterministic message ID and synchronous PubAck. If blocked, non-forwarding is the terminal policy consequence; configured audit remains nonblocking as its current capability specifies. | Decode/invalid payload → Terminate; filter dependency, subject resolution, marshal, or publish uncertainty → Retry; identity/fingerprint conflict or panic → Quarantine. Audit failure remains observable but does not change policy settlement. | Source Message.ID plus deterministic validated-output identity; source and validated message remain in AGENT under admitted retention. Replacement reconciles matching output before republishing. | Governance drains handle and awaits `Closed`; callback joins. AckWait 30s, budget 25s, join margin 5s; exhaustion → Retry. Failed proof compels heartbeat migration for this subscription. |
| 5 | governance `request_validation`; fast owner | Same as row 4 for exact validated `AgentRequest`; allowed requires JetStream PubAck, blocked requires completed policy decision. | Same as row 4. No core-NATS publish may be treated as PubAck. | Source Message.ID/RequestID and deterministic validated-output fingerprint. | AckWait 30s, budget 25s, join margin 5s; same proof/fallback as row 4. |
| 6 | governance `response_validation`; fast owner | Same as row 4 for exact validated `AgentResponse`; allowed requires JetStream PubAck, blocked requires completed policy decision. | Same as row 4. | Source Message.ID/RequestID and deterministic validated-output fingerprint. | AckWait 30s, budget 25s, join margin 5s; same proof/fallback as row 4. |
| 7 | loop `agent.signal`; fast owner after #1239 deletion | Cancel: current state is durably cancelled, `COMPLETE_<loopID>` is committed, and deterministic terminal event has PubAck. A terminal duplicate ACKs after exact proof. Unknown signal is permanent invalid input, not a warning-only success. | Invalid/unknown → Terminate; missing live authority, KV failure, or publication failure → Retry; conflicting identity or impossible state → Quarantine. Pause/resume branches and fields are deleted by #1239, not translated. | LoopID and signal MessageID; exact `AGENT_LOOPS` plus `COMPLETE_` and terminal event. Replacement read-through distinguishes live missing from terminal duplicate. | Loop drains and awaits `Closed`; callback joins. Replace current 120s work timeout with 25s under AckWait 30s, leaving 5s join margin; exhaustion → Retry. Failed proof compels heartbeat migration for this subscription. |
| 8 | loop `agent.approval_response`; fast owner | Approve/modify: exact pending fingerprint validates, stable ToolCall gets PubAck, and applied decision fingerprint remains until matching ToolResult. Reject/timeout: synthetic result and next request/terminal event get PubAck before pending state clears. Exact duplicate ACKs only from committed proof. | Decode/validation → Terminate; Store/KV/publication/unreadable authority → Retry; panic, mismatched call, or conflicting fingerprint → Quarantine. Missing process loop is never ACK. | LoopID, CallID, execution identity, decision fingerprint, current `PendingApprovalState`, and referenced verified continuation Store object. Replacement reconstructs exact reviewed call. | Same loop close and exact 30s/25s/5s boundary. Store/KV/publication must honor context; failed proof compels heartbeat migration for this subscription. |
| 9 | loop `agent.toolcall.approved`; fast owner | Verdict identity/fingerprint validates and is delivered to the matching waiter, or the retained source verdict is proven exact and therefore recoverable by redelivered response work. | Invalid decision → Terminate; retained lookup unavailable → Retry; proposal mismatch/conflict/panic → Quarantine. Missing/full waiter is not log-and-ACK. | Execution identity + proposal fingerprint; exact retained verdict on admitted AGENT stream is recovery evidence. No verdict bucket. | Same loop close and exact 30s/25s/5s boundary; failed proof compels heartbeat migration for this subscription. |
| 10 | loop `agent.toolcall.rejected`; fast owner | Same contract as row 9 for rejected verdict; response replay deterministically applies rejection or proves it applied. | Same as row 9. | Same as row 9. | Same loop close and exact 30s/25s/5s boundary; failed proof compels heartbeat migration for this subscription. |
| 11 | tools `tool.execute`; heartbeat owner already present | Existing #949 contract: exact immutable COMPLETED outcome exists in `TOOL_CALL_OUTCOMES`, and exact ToolResult receives PubAck. Approval-required is phase-distinct nonterminal coordination and creates no COMPLETED outcome. | Invalid/collision mapping remains typed: permanent invalid → Terminate; transient Store/publication → Retry; invariant collision → Quarantine. External effect before outcome remains executor-specific ambiguity, never hidden. | Framework execution identity keys/fingerprints outcome; provider CallID remains conversational data. Completed replay publishes deterministic result without executor invocation. | Existing tools owner-stop observer, drain, exact `Closed`, cancel/join. Heartbeat justified by arbitrary tool latency. |
| 12 | dispatch `agent.complete`; heartbeat owner already present | Exact terminal event ancestry and `AGENT_LOOPS` reread reconcile; deterministic user response gets PubAck before source ACK. | Malformed → Terminate; unreadable loop/publication → Retry; route/identity conflict or unknown publication invariant → Quarantine and stop exact owner. | `SourceMessageID`, LoopID, current loop state, deterministic response ID. Replacement replays the same response. | Existing typed owner and observer; drain/Closed/cancel/join. Heartbeat retained because terminal projection may include durable reads/publication and already has reviewed policy. |
| 13 | dispatch `agent.failed`; heartbeat owner already present | Same as row 12 for failed terminal outcome and deterministic user response. | Same as row 12. | Same as row 12. | Same as row 12; heartbeat retained. |
| 14 | model `agent.request`; heartbeat owner migrated from legacy | A matching durable AgentResponse has synchronous PubAck. Before any provider call, exact response reconciliation runs. Delivery 1 may invoke; unresolved redelivery follows configured policy. | Invalid/endpoint permanent error → typed error response then ACK, or Terminate when no valid response identity exists; transient provider/publication → Retry only when replay is safe; collision/metadata unavailable/panic → Quarantine and stop owner. Default unresolved redelivery publishes `provider_commit_unknown` and never calls provider. | RequestID and request fingerprint; exact retained AgentResponse. No started ledger. `at_least_once` is explicit opt-in; `provider_reconcile` requires demonstrated adapter support. Matching response means zero provider calls. | Model drains and awaits `Closed`; callback work joins. Heartbeat justified by paid/remote model latency. |
| 15 | loop `agent.task`; heartbeat owner migrated from legacy | New loop: matching LoopEntity, graph birth/lineage, deterministic initial AgentRequest PubAck, and LoopCreated PubAck. Continuation: existing context reconstructed or preserved, task identity applied once, and next request PubAck. Defined terminal/busy refusal leaves state unchanged and completes its required typed negative response. | Invalid → Terminate; transient graph/KV/Store/publication or post-registration handler error → Retry only with deterministic reconciliation/rollback; identity collision/impossible partial birth/panic → Quarantine. No post-registration error may log-and-ACK. | TaskID/LoopID, exact `AGENT_LOOPS`, graph identity, deterministic request/event IDs, retained request material. Pending process maps are caches. | Loop drain/Closed; callback joins. Heartbeat justified by 30-minute declared task budget and graph/assembly work. |
| 16 | loop `agent.response`; heartbeat owner migrated from legacy | Exact loop/request is hydrated; response identity validates; resulting LoopEntity/`COMPLETE_` and required next tool/request/terminal publications commit with PubAck. Later committed output may prove a duplicate already applied. | Malformed → Terminate; missing live correlation/KV/Store/publication → Retry; conflicting response, impossible correlation, or panic → Quarantine. Terminal exact duplicate → ACK. Never `stale_request_id` log-and-drop. | RequestID embeds LoopID; exact current loop plus originating request/response and later output identity reconstruct transition. No durable model-result ledger beyond retained response. | Loop drain/Closed; callback joins. Heartbeat justified by handler transition, context rebuild, and possible multi-output work. |
| 17 | loop `tool.result`; heartbeat owner migrated from legacy | Exact loop/request/response batch is reconstructed; result is persisted; next queued tool, model request, approval event, or terminal event gets PubAck. Approval-required branch additionally stores/verifies continuation and persists its reference before ACK. | Malformed → Terminate; missing live continuation, Store/KV/publication → Retry; conflicting execution identity, missing permanent referenced continuation, or panic → Quarantine or durable loop failure as specified. Terminal exact duplicate → ACK. Never `stale_callid` log-and-drop. | RequestID + execution identity; current loop pending results; exact originating request/response; existing `TOOL_CALL_OUTCOMES`; approval Store reference only when needed. | Loop drain/Closed; callback joins. Heartbeat justified by reconstruction, Store, and multi-publication work. |

Rows 18 and 19, AgentRun complete/failed fanout, are deliberately absent. They transfer intact to #1249 at exact
post-#1146 staged checkpoint `A`; #1146 neither ratifies partial ACK nor changes the exported handler contract.

### Fast-lane lease boundary

The accepted inventory has ten physical raw fast subscriptions. They are organized into four parallel proof groups
(dispatch, governance, loop signal/approval, and loop verdict) only to bound test wall-clock; fallback is decided for
each individual subscription and never inherited by its proof group.

| Physical subscription | Proof group | Strict AckWait/work/margin | If cooperative >25s compels fallback: maximum work / heartbeat |
|---|---|---|---|
| dispatch `user.message` | dispatch | 30s / 25s / 5s | 30s / 10s |
| dispatch `agent.created` | dispatch | 30s / 25s / 5s | 30s / 10s |
| dispatch `agent.approval_pending` | dispatch | 30s / 25s / 5s | 30s / 10s |
| governance `task_validation` | governance | 30s / 25s / 5s | 30s / 15s maximum |
| governance `request_validation` | governance | 30s / 25s / 5s | 30s / 15s maximum |
| governance `response_validation` | governance | 30s / 25s / 5s | 30s / 15s maximum |
| loop `agent.signal` | loop signal/approval | 30s / 25s / 5s | 120s / 15s |
| loop `agent.approval_response` | loop signal/approval | 30s / 25s / 5s | 120s / 15s |
| loop `agent.toolcall.approved` | loop verdict | 30s / 25s / 5s | 120s / 15s |
| loop `agent.toolcall.rejected` | loop verdict | 30s / 25s / 5s | 120s / 15s |

Fallback values are framework-owned, not caller predictions: dispatch reuses its accepted 10s local heartbeat and
30s bounded handler maximum; loop reuses its typed 120s local message timeout and accepted 15s heartbeat; governance
has no earlier heartbeat default, so only the policy-derived ceiling (15s, half AckWait 30s) is proven. Implementation
must choose and owner-review a concrete governance interval at or below 15s before fallback can be enabled; until then
governance heartbeat fallback is explicitly unproven, not guessed. Each fallback config is validated before allocation.

The 25-second boundary includes cancellation and join, not merely context cancellation. Every blocking NATS, KV,
Store, and filter operation receives the exact delivery context. A deadline result is Retry with no ACK. A real-NATS
test blocks the last dependency until 25s, observes cancellation and callback return before the 30s lease, and proves
no concurrent redelivery. A second test uses legitimate context-cooperative work that completes after 25s: strict
completion-before-lease is rejected for that individual physical subscription, while the admitted heartbeat owner renews
the lease, completes once, and settles through the same semantic outcome. A dependency that ignores cancellation or
never returns is a lifecycle-contract failure and stop condition under both routes, never heartbeat evidence.

### Heartbeat-policy migration

Every heartbeat policy is validated against the exact `StreamConsumerConfig` passed to acquisition before the
consumer is allocated.

- Model keeps AckWait 120s and changes default heartbeat 90s → 60s. With no BackOff, effective AckWait is 120s and
  60s is the inclusive validator ceiling.
- Loop task/response/tool-result keep configured AckWait but use BackOff `[30s,2m]`; the shortest entry makes the
  effective interval 30s. Change component default and schema default heartbeat 60s → 15s. Every fixture value above
  15s (`deep-research` 60s, `ops-agent` 60s, `lesson-example` 20s) becomes 15s; already-valid 15s/10s fixtures stay.
- Tools remain 5s against shortest BackOff 15s. Dispatch terminal remains 10s against effective AckWait 30s.

Reconcile implementation constants, `ConsumerConfig` defaults/validation, generated schemas, timeout-chain and
JetStream-tuning docs, all example/test flow fixtures, and operator-facing messages. Validation must say that the
heartbeat is at most half the shortest positive BackOff when BackOff exists, otherwise at most half positive AckWait
or the 30-second server default. It must reject before acquisition and name observed heartbeat, effective interval,
ceiling, component, and port. Setup tests capture the exact config passed to both validation and acquisition, reject
model 90/120, reject loop 60/[30s,2m], accept model 60/120 and loop 15/[30s,2m], and prove zero consumer allocations
on failure.

## Cross-lane invariants and proof sources

These are the only property/fuzz sources. Tests must cite the requirement and scenario, not reconstruct a property
from implementation:

| Invariant | Normative spec-delta source |
|---|---|
| One semantic identity cannot accept different content | `agentic-loop` — “Tool execution has stable framework correlation,” scenario “Provider repeats a CallID in another request”; plus new scenario “Semantic identity collision quarantines” below |
| A missing process map entry never proves stale or complete | `agentic-loop` — “Agentic-loop settles each durable input after its durable outcome,” scenario “Missing process correlation is not proof of staleness” |
| ACK follows every required durable effect and PubAck | `agentic-loop` — same requirement, scenario “Required output publication fails”; dispatch/governance lane-specific requirements below |
| `provider_commit_unknown` is closed and error-only | `agentic-model` — “Provider commit-unknown is machine-readable,” both scenarios |
| Matching response reconciliation causes zero provider calls | `agentic-model` — “Model request settlement is bound to a durable response,” scenario “Matching response already exists” |
| Framework execution identity changes when RequestID or ordinal changes while preserving provider CallID | `agentic-loop` — “Tool execution has stable framework correlation”; `agentic-tools` — “Completed tool outcome identity is globally unambiguous” |
| Continuation key is canonical and collision-detecting while envelope metadata is non-semantic | `agentic-loop` — “Approval continuation storage is content-addressed and verified,” scenarios “Matching object already exists,” “Key contains conflicting content,” and new “Retry constructs a fresh continuation envelope” below |
| Partial projection never licenses AutoContinue | `agentic-dispatch` — “Dispatch process state is a reconstructable projection,” scenarios “Snapshot is interrupted” and “Complete snapshot is empty” |
| Observed `DiscardOld` cannot satisfy strong restart admission | `agentic-loop` — “Restart-safe replay observes and admits stream bounds,” scenario “Capacity policy discards old evidence” |
| Every spawned delivery task joins before result/Stop | `agentic-loop` — “Delivery work joins before settlement,” scenario “Delivery work exceeds its budget” |

Add the missing semantic-collision scenario to the `agentic-loop` delta so the first property above has a direct
normative home. This repairs the inherited uncited design property; a property named only in design is not admissible.

## Replay admissibility against the shipped stream

The earlier owner ruling selecting the strong contract remains binding. #1146 does not silently narrow it.

Two admission boundaries were compared:

1. **Whole-composition refusal:** add a generic ComponentManager preflight before every component Start. It prevents
   even unrelated components from starting, requires generic lifecycle code to interpret agentic configuration, and
   would need a new `framework-composition` capability delta plus an owner ruling.
2. **Affected dependency closure (recommended and already selected):** each recovery-dependent agentic component
   invokes one shared pure validator after resolving its own admitted port facts and before allocating its own first
   dependent consumer, subscription, observer, or worker. Dispatch performs this before USER intake; rule
   `publish_agent` and other first-party AGENT producers validate the resolved output stream before publishing and
   propagate refusal through their existing source outcome. Non-agentic components perform zero admission lookups.

Option 2 preserves the accepted affected-consumer refusal (“recovery-dependent durable consumers are not started”),
keeps failure inside the dependency closure, and gives non-agentic adopters the intended zero-cost default. Therefore
it is compelled by the prior ruling and adopter-seam result unless the owner explicitly reopens that ruling; this
reconciliation does not add a generic ComponentManager change or a `framework-composition` delta.

Add pure repo-internal `internal/agentstreamadmission.ObserveAndValidate(ctx, *natsclient.Client, streamName,
Requirement) (Observation, error)`. Activation and `streamName` come only from each component's already-resolved
`component.PortFacts`. Each caller constructs `Requirement` only from its own typed validated config and resolved
facts—never another component config, a shared maxima object, factory names, global state, or raw JSON:

| Caller | Exact local requirement fields |
|---|---|
| model | resolved request input/response output stream; local AckWait, BackOff, MaxDeliver, maximum model-processing timeout, response-reconciliation need, and response-PubAck dependency |
| dispatch | resolved AGENT inputs plus task/signal/approval outputs; local fast and terminal AckWait/BackOff/MaxDeliver/work budgets, projection replay need, and downstream PubAck dependencies; USER intake is gated by these local output facts |
| governance | resolved validation inputs/validated outputs; local AckWait/BackOff/MaxDeliver/work budget, output-reconciliation need, and validated-output PubAck dependency |
| loop | resolved task/response/tool-result/signal/approval/verdict inputs and AGENT outputs; local AckWait/BackOff/MaxDeliver/work budgets, transition replay need, and required-output PubAck dependencies |

The strongest local requirement wins only by refusing that caller's dependency closure. Approval lifetime and
`AGENT_LOOPS` TTL/capacity are excluded from stream `Requirement`; loop-owned KV acquisition validates them.
Nothing switches on factory names or reparses `types.ComponentConfig.Config`. An affected component with no resolved
AGENT dependency fails its existing port validation; a genuinely non-agentic component never imports or calls this
package. The validator returns immutable observation or typed `agent_stream_replay_inadmissible`, retains no context,
starts no goroutine, mutates no stream, and writes no durable/process state.

For each invocation, the validator computes only that caller's local ordinary horizon from its resolved AckWait,
BackOff, MaxDeliver, maximum local work/replay need, and required producer PubAck dependency. Loop timeout informs only
loop's local `Requirement`; approval lifetime is excluded from stream admission and is validated only by
`loopbucket.AcquireOwner`. The validator verifies:

1. `DiscardPolicy == DiscardNew`;
2. actual MaxAge covers the computed horizon and safety margin;
3. MaxMsgs, MaxMsgsPerSubject, and other observed bounds cannot evict required evidence earlier; and
4. only the caller's local processing/replay and producer-settlement dependency is considered; approval lifetime and
   `AGENT_LOOPS` retention belong exclusively to the separate loop-owned KV gate below.

The current shipped `configs/agentic.json` is `DiscardOld`, so it must be refused. Exact behavior:

- recovery-dependent durable consumers are not started;
- component readiness is false/degraded with typed reason `agent_stream_replay_inadmissible`;
- the error names stream, observed discard policy and bounds, required discard policy and computed horizon;
- startup does not mutate the stream;
- no consumer is started and then rolled back after the mismatch; admission precedes allocation; and
- the shipped config, fixtures, and E2E environment move to `DiscardNew` before #1146 can claim green.

Concurrency tests start model, dispatch, governance, and loop together against rejected StreamInfo and assert each
affected component allocates zero dependent consumers/workers while an unrelated non-agentic component starts with
zero StreamInfo lookup. A producer test places a USER message before boot, refuses dispatch's resolved AGENT output,
and proves dispatch never consumes or ACKs it. Mixed-stream tests prove each component observes its resolved override,
not the literal `AGENT`; external-first-party publisher tests prove refusal propagates without positive source
settlement. An allowed test proves each affected component allocates only after its own observation succeeds.

At capacity under `DiscardNew`, a required JetStream publication failure returns Retry and retains the source
delivery; the producer does not ACK and does not fall back to core NATS. Deterministic `Nats-Msg-Id` is only bounded
server dedupe within the stream's duplicate window. Semantic reconciliation is load-bearing after longer downtime.

External purge, administrative deletion, and total persistence loss remain operator data-loss events. The design
does not create a shadow copy to mask them.

### Loop-owned `AGENT_LOOPS` fresh-boot authority

AGENT stream replay admission and `AGENT_LOOPS` KV acquisition are separate gates. The stream gate proves retained
work/evidence bounds; the bucket gate establishes the current loop-state authority before any loop consumer.

Agentic-loop resolves the bucket name from its admitted `loops` KV-write `component.PortFacts`, then calls repo-
internal `processor/agentic-loop/internal/loopbucket.AcquireOwner`. The helper calls `JetStream.KeyValue` first. It
calls `CreateKeyValue` only when `errors.Is(err, jetstream.ErrBucketNotFound)`; permission, timeout, transport, and
every other lookup error return unchanged with zero create mutation. Create uses History 10, TTL 24h, and no binding
MaxBytes. `jetstream.ErrBucketExists` means one concurrent winner and permits exactly one `KeyValue` retry; any other
create error returns. After get, create, or race-get, the helper reads actual bucket status/backing StreamInfo and
requires exact History 10, exact TTL 24h, and MaxBytes <= 0. Only after
that observation matches does it publish `c.loopsBucket` and allocate task/response/tool-result/signal/approval/
verdict consumers or the approval sweeper.

An absent bucket is created. Two fresh processes racing with the same declaration converge on one bucket and both
validate the winner. A create/get/status transport failure is transient setup failure. A retained bucket with drifted
History, TTL, or binding MaxBytes is refused without update or reconciliation because its prior eviction may already
have destroyed authority; the error names bucket and every expected/observed value. This remains an application/
product bucket outside the framework bucket catalog (`graph/kvcatalog.go:9-13`), adds no new bucket, and changes no
reader into a creator.

Required real-NATS cases are: absent fresh boot; matching retained boot; two-process same-config create race; race
where a foreign config wins; retained TTL drift; retained History drift; retained positive MaxBytes; and status read
failure. Every refusal proves zero dependent loop consumer/sweeper allocation and no partial bucket handle publication.
Fake seam tests additionally prove a non-`ErrBucketNotFound` lookup error performs zero CreateKeyValue calls and an
`ErrBucketExists` create race performs exactly one race-get before observation. The internal package prevents adopter
import; no exported natsclient or component API is added.

## Approval continuation detail

`ApprovalContinuationV1` contains LoopID, TaskID, RequestID, execution identity, provider CallID, positive ordinal,
the exact originating `AgentRequest`, and the originating tool-call `AgentResponse`. Validation checks nested types,
recomputes execution identity, and proves the ordinal names the reviewed call.

Registration and storage are exact:

1. implement `Schema`, `Validate`, and alias-based `MarshalJSON`/`UnmarshalJSON` for the payload's own fields;
2. add its factory to `agentic.RegisterPayloads` with the control indexing floor and nil projection contracts;
3. reach first-party roots through existing `payloadbuiltins.Register` and repeat the composition-root census;
4. store one normal registered `BaseMessage` envelope through a configured `storage.Store` resolved on every
   operation from `StoreRegistry`;
5. derive the key as lowercase unpadded base32 SHA-256 of canonical `ApprovalContinuationV1` payload JSON, excluding
   the enclosing BaseMessage UUID and timestamp, below `agentic/approval-continuation/v1/`;
6. get-before-put. Production-decode an existing envelope into `*ApprovalContinuationV1`, validate it, canonicalize
   its payload fields, compare canonical payload bytes to the expected canonical payload, recompute the digest/key,
   and verify LoopID/TaskID/RequestID/execution identity/provider CallID/ordinal. Fresh BaseMessage UUID and timestamp
   are deliberately ignored for semantic equality;
7. absence permits Put of a fresh normal registered envelope followed by Get and the same production-decode,
   validation, canonical-payload equality, digest, key, and identity checks; and
8. on unknown Put result, Get reconciles; matching succeeds, unresolved absence retries, malformed/conflicting content
   quarantines.

Deterministic envelope UUID/timestamp was considered and rejected. Those fields are transport metadata, not the
continuation's semantic identity; making them deterministic would add a second identity contract and fight the normal
new-payload envelope path. A required test constructs two fresh envelopes around the same canonical payload with
different UUIDs/timestamps, loses the first Put reply, and proves retry reuses the existing object after decoded
canonical payload equality. A second test keeps the key but changes one semantic field and proves quarantine.

The typed `StorageReference` and applied-decision fingerprint live only in existing `PendingApprovalState`. There is
no new bucket, graph triple, scanner, or reaper. Permanently missing referenced material durably fails the loop as
`approval_continuation_unavailable`; best-effort cleanup after deterministic downstream commit is metered and never
reverses source settlement.

## #1239 collision and staged sequence

The signal lane cannot pass #1146 review while pause/resume remains a warning/void/ACK branch, and #1146 must not take
ownership of the dead feature. Recommended staging:

1. Keep remote parent `codex/gh759-semantic-settlement` frozen at exact `F`.
2. Complete and accept this #1146 reconciliation before production implementation.
3. Preserve existing PR #1251 and branch `claude/gh1239-delete-pause-resume`; do not open a duplicate claim. Its
   current remote head observed by review is `943570116f2eeb2237715eb81bbd11d2d04e4a6a`, including six later fixes; its
   retained hosted record carries `Closes #1239`, `implemented-by: opus`, and existing review provenance. Immediately
   before staging, read and pin the then-current remote head; never reset or replay obsolete
   `d21e26122d0cb0095e3e4a988bae444c17c8ba77`.
4. Freeze that freshly observed #1251 remote head, return PR #1251 to draft, rebase its branch onto the exact current
   remote #1159 head, retarget its base from
   `main` to `codex/gh1146-agentic-loop-restart`, resolve only measured conflicts, and verify its diff still contains
   the accepted #1239 deletion/migration surface and no #1146 authorship claim.
5. Re-run #1251's unit/race, integration, schema, OpenSpec/property, API-compatibility, and agentic E2E gates on the
   rebased stack. Repeat implementation review, owner-requested cross-agent review, fixes/re-review, archive
   `retire-loop-pause-resume` as its final content commit, and narrow archive/current-spec-sync review. Then merge the
   retained hosted child record into the #1146 branch. This advances #1159, not frozen parent `F`.
   Any unexpected #1251 head advance invalidates the pin and requires reread, rebase, full proof, and re-review.
6. Implement row 7 against the post-deletion signal surface: cancel plus permanently invalid unknown enum only.
7. Before #1159 implementation and cross-agent review, add `Refs #1239` to #1159 and add `Closes #1239` to final
   default-branch PR #1156's complete claim set. The non-default child merge does not close the issue.

Strongest alternative: land #1239 into the #759 parent before #1146 implementation. That is simpler topology but
advances remote `F`, invalidates this inventory/design review, and requires a new parent pin, rebase, test, and review.
Do not merge #1239 independently to `main` while the final atomic stack is open.

PR #1251 remains the authorship and implementation-review record for #1239. PR #1159 records only its integration;
PR #1156's `Closes #1239` supplies final default-branch issue-closing authority. This staging choice remains a genuine
owner ruling requested by this reconciliation.

## Exact active-artifact amendments

### `proposal.md`

Replace the second paragraph under `Why`, the `Holds` list, and the `Impact` dependency wording with:

```markdown
This is the owner-classified critical beta.163 vertical in #1146. It builds, proves, and receives review against the
exact reviewed non-default #759 settlement checkpoint
`F=417beae5552f8f15ad3540edd7d8504c87174c13`. PR #1159 targets
`codex/gh759-semantic-settlement`; #759 does not merge first.

The full accepted scope remains user-message intake and commands, model, loop, tools, signals, approval, projections,
governance correlation, replay admissibility, and context/lifecycle closure. Migration of model plus loop task,
response, and tool-result legacy heartbeat bindings is additive to that scope.

## Holds

- Remote `codex/gh759-semantic-settlement` remains frozen at exact `F` through #1159 implementation, proof, and
  review. Unexpected advance invalidates the review and requires a new pin, rebase, inventory verification, test, and
  review cycle.
- Every fast durable-input lane uses its existing private native-message owner with typed business outcomes and exact
  AckWait 30s / work budget 25s / join margin 5s, or that individual physical subscription migrates to the admitted typed
  heartbeat owner under the accepted per-subscription ruling using the exact fallback matrix: dispatch 30s/10s,
  loop 120s/15s, governance
  work 30s with concrete heartbeat <=15s still unproven and requiring owner review. Four proof groups never imply
  group migration. No native message escapes, no direct business-handler ACK/NAK remains, and no exported
  no-heartbeat settlement API is added.
- Model uses AckWait 120s / heartbeat 60s. Loop task/response/tool-result use heartbeat 15s against shortest BackOff
  30s. The exact acquisition config is validated before consumer allocation, and invalid policy is a setup failure.
- Each recovery-dependent agentic component validates its resolved AGENT stream after port resolution and before its
  own dependent allocation; dispatch gates USER intake, and first-party publishers propagate refusal through their
  source outcome. Non-agentic components perform zero admission lookup; ComponentManager remains generic.
- Agentic-loop separately acquires `AGENT_LOOPS` from its resolved KV-write port through internal absent-only
  `loopbucket.AcquireOwner`: create only for typed `jetstream.ErrBucketNotFound`, one get after typed
  `jetstream.ErrBucketExists`, then actual History 10/TTL 24h/non-binding MaxBytes observation. Only then may it
  publish the handle or start dependent loop consumers/sweeper; retained drift is refused, not reconciled.
- AgentRun complete/failed fanout is transferred to #1249 at exact post-#1146 checkpoint `A`; it is not #1146
  production or spec scope.
- #1239 separately deletes pause/resume before final signal-lane review; #1146 owns cancel settlement only.
- No supervisor, generic state machine, checkpoint bucket, outbox, event-sourced loop, or universal exactly-once claim
  is admitted.
- Error propagation does not land separately from the durable authority that makes redelivery safe.
- Additional persistent state requires a named replacement failpoint proving source redelivery plus existing
  Streams/KV/Store authority insufficient.

## Impact

- Tracking issue: #1146; parent epic: #1147.
- Staged prerequisite: exact #759 checkpoint `F`; PR #1159 targets its non-default branch.
- AgentRun successor: #1249 from post-#1146 `A`; transition-contract successor: #1244.
- Blocks restart-safe approval/enforcement claims in #1140.
- Capability deltas: `agentic-dispatch`, `agentic-loop`, `agentic-model`, `agentic-tools`, and
  `agentic-governance`.
- Required verification includes the #1146-owned tranche of #1155's real-NATS process-replacement matrix and
  serialized agentic E2E. #1155 remains open for #1249 AgentRun proof and the later combined gate.
```

This replaces stale proposal lines 10-12, 30, and 44. Keep the existing claim-scope sequence and anti-complexity
text where it does not duplicate the replacement.

### `design.md`

Replace current lines 5-22 (`Status`, accepted inventory, and holds) with:

```markdown
## Status

Previously accepted direction, reconciled against exact reviewed non-default #759 checkpoint `F`. This reconciliation
requires independent design review and explicit owner acceptance before implementation.

## Accepted inventory

This design incorporates
`openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-02-F.md` unchanged. Its base is
`F=417beae5552f8f15ad3540edd7d8504c87174c13`, reviewed SHA-256 is
`3b53c6d3d4f3298d63ffc2231b209aa8e1f4379a6c1bf75b7aa5edc6a4f65ffb`, pin verification is `555/555`, recorded
searches are `64`, and the independent verdict is `INVENTORY PASS`. Materialization commit is
`b755e4ff08d889055797fafd1ef98dc4a4864758`.

## Holds

- PR #1159 targets `codex/gh759-semantic-settlement`, and its merge base is exact `F`.
- The remote parent remains frozen at exact `F` throughout #1159 implementation, proof, and review. Unexpected
  advance invalidates review and requires a new pin, rebase, inventory verification, test, and re-review.
- The full accepted #1146 scope remains; model and three loop legacy-heartbeat migrations are additive.
- Ten physical fast durable-input subscriptions keep settlement in their private binding owners and return typed business outcomes.
  Each enforces AckWait 30s, work budget 25s, and join margin 5s; a failed boundary proof migrates that physical
  subscription alone to the admitted heartbeat owner using dispatch 30s/10s, loop 120s/15s, or governance bounded
  work 30s with owner-reviewed heartbeat <=15s. Four proof groups never imply group migration. No native settlement
  escapes and no exported no-heartbeat API is added.
- #1239 separately deletes pause/resume before final row-7 review. Cancel remains #1146 scope.
- AGENT replay admission fences only affected dependency closures; ComponentManager stays generic and non-agentic
  components perform zero lookup. Agentic-loop's separate `AGENT_LOOPS` owner acquisition must pass before loop work.
- AgentRun transfers to #1249 from exact post-#1146 checkpoint `A`; no AgentRun production or spec work lands here.
- #1146 owns its tranche of #1155 replacement proof; #1155 remains open for #1249 AgentRun proof and the later
  combined gate. #1140 owns governance policy content; #1145 owns later pattern
  generalization; #1244 designs declared loop transitions against post-#1146 exits.
```

Replace the stale provider-ambiguity paragraphs at current design lines 95-104 with:

```markdown
Exact foundation `F` already supplies immutable `DeliveryAttempt` through `DeliveryWork`. Natsclient constructs it
from native message metadata before invoking work; agentic-model receives delivery number, metadata availability, and
redelivery classification only. It receives no native message, settlement method, sequence, consumer identity,
header, or mutable state, and it adds no model-private wrapper.

Agentic-model validates its `HeartbeatDeliveryPolicy` against the exact acquisition config before allocating the
consumer. Missing metadata prevents work, returns typed `delivery_metadata_unavailable`, quarantines without positive
settlement, and stops the exact owner. This is implemented foundation consumed by #1146, not a pending #759 addendum.
```

No grep-equivalent statement may say #759 is unimplemented or that its work signature exposes only context and bytes.

Insert the 17-row contract, replay-admission section, approval registration/storage section, decision-skill outcomes,
property-source table, fast-lane lease boundary, and heartbeat-policy migration from this artifact after `Decision`.
Replace the old shorter `Per-lane definition of done`, `Context and lifecycle`, `Observed AGENT replay
admissibility`, and `Adopter seam` sections with those exact sections.

Replace measurable premise 1 at current line 408 with:

```markdown
1. Exact reviewed remote #759 checkpoint `F` supplies the accepted permanent `DeliveryResult` foundation. PR #1159
   targets `codex/gh759-semantic-settlement`, is based on exact `F`, and receives implementation and cross-agent review
   while that remote parent remains frozen. This premise does not require #759 to merge first.
```

Replace premise 12 at current line 420 with:

```markdown
12. AgentRun complete/failed settlement is transferred to #1249. #1249 begins from exact remote post-#1146 staged
    checkpoint `A`, inventories and designs against those handler shapes, and receives separate review before either
    binding migrates.
```

Replace the related out-of-scope line at current line 424 with:

```markdown
- AgentRun production implementation and capability deltas; #1249 owns its post-#1146 inventory, design,
  complete/failed migration, and replacement proof from exact staged checkpoint `A`.
```

Add to `Out of scope`:

```markdown
- Pause/resume implementation or settlement; #1239 deletes that dead surface, while #1146 settles cancel.
- A core-NATS fallback for a declared JetStream output, raw native settlement outside the private owner, or a new
  exported no-heartbeat settlement API.
```

### `tasks.md`

Replace tasks 1.1 and 1.4 and add 1.9-1.11:

```markdown
- [ ] 1.1 Before implementation, verify PR #1159 targets `codex/gh759-semantic-settlement`, its branch has exact
      remote merge base `F=417beae5552f8f15ad3540edd7d8504c87174c13`, and its diff contains only #1146 work
      above `F`.
- [ ] 1.4 Reconcile the full accepted design against exact `F` and post-#1231/#1245 surfaces; stop if any touched
      authority differs materially.
- [ ] 1.9 Keep the remote parent frozen at exact `F`; immediately before every #1159 review and hosted staging merge,
      verify the remote full SHA. Unexpected advance requires a new pin, rebase, inventory verification, test, and
      review.
- [ ] 1.10 Inventory and test all ten physical fast durable-input subscriptions through their existing private owners. Remove direct
      business-handler settlement and void/log-only success; add no exported no-heartbeat API.
- [ ] 1.11 Receive independent review and explicit owner acceptance of
      `design-reconciliation-F-2026-09-02.md` before production implementation.
```

Replace task 1.2 and reconcile tasks 1.5-1.8 with foundation `F`:

```markdown
- [ ] 1.2 Confirm each of the seven heartbeat-class lanes uses the permanent #759 `DeliveryResult` owner. Confirm
      each of the ten physical fast subscriptions returns `DeliveryDecision, error` through its existing private native-message owner,
      enforces AckWait 30s / work budget 25s / join margin 5s, and migrates that individual physical subscription to the
      admitted heartbeat owner if that proof fails; use dispatch 30s/10s and loop 120s/15s fallback work/heartbeat,
      and keep governance fallback blocked until a concrete interval <=15s is owner-reviewed. No native message or
      settlement closure escapes; proof groups never determine migration scope.
- [x] 1.5 Exact foundation `F` supplies immutable `DeliveryAttempt` without native message, settlement method,
      sequence, consumer identity, headers, or mutable state.
- [x] 1.6 The #759 addendum received design review and owner acceptance before #1146 model work.
- [ ] 1.7 Quarantine and stop the exact heartbeat owner when delivery metadata is unavailable; fast lanes do not
      invent or fetch attempt metadata merely to share this path.
- [ ] 1.8 Test first delivery, second delivery, crash-before-call conservative false unknown, and unavailable metadata
      on the model heartbeat lane.
- [ ] 1.12 Change model default heartbeat 90s → 60s against AckWait 120s. Change loop default/schema heartbeat 60s →
      15s against shortest BackOff 30s; reconcile config structs, defaults, generated schemas, docs, every fixture,
      and operator-facing validation.
- [ ] 1.13 Prove setup validates the exact acquisition config and allocates no consumer for model 90/120 or loop
      60/[30s,2m]; prove model 60/120 and loop 15/[30s,2m] pass.
```

Replace task 2.4 with:

```markdown
- [ ] 2.4 Add operation-specific exact committed-message lookup and collision validation for requests, responses,
      validated governance outputs, and verdicts; add no general query front door or stream scan.
```

Add to section 3:

```markdown
- [ ] 3.9 Prove by invocation counter that matching-response replay and default unresolved redelivery make zero
      provider calls; prove `at_least_once` repeats only under explicit opt-in.
```

Replace 4.1 and add 4.7-4.9:

```markdown
- [ ] 4.1 Migrate loop task, response, and tool-result bindings from the legacy helper to the permanent typed
      heartbeat owner; replace void adapters with classified delivery work.
- [ ] 4.7 Prove no provider double-call, no response/tool-result log-and-ACK, no stale-correlation loss, and no replay
      beyond admitted retained evidence.
- [ ] 4.8 Keep every handler exit available to #1244 as a declared transition or defined refusal; do not encode
      log-and-ACK as terminal success.
- [ ] 4.9 Test task post-registration failure and dropped initial-request publication across real process replacement.
```

Add to section 7:

```markdown
- [ ] 7.5 Refactor the cancel signal handler to return classified outcomes through the existing private row-7 owner;
      unknown signal types terminate and no failure returns through the void adapter.
- [ ] 7.6 Integrate the separately reviewed #1239 deletion before final row-7 implementation review; no pause/resume
      field or handler is translated into a settlement outcome. Preserve PR #1251 and branch
      `claude/gh1239-delete-pause-resume`: immediately read/pin its current remote head (review observed
      `943570116f2eeb2237715eb81bbd11d2d04e4a6a`; never replay obsolete `d21e261...`), freeze it, return it to draft,
      rebase onto the exact current remote #1159 head, retarget it to `codex/gh1146-agentic-loop-restart`, re-run every
      proof/review/archive gate, and merge that retained hosted child record into #1159. Any head advance invalidates
      the pin and requires repeat proof/re-review; do not open a duplicate claim.
- [ ] 7.7 For each of the four loop fast subscriptions, set explicit AckWait 30s and work budget 25s, prove callback
      cancellation and join leave 5s margin, and migrate only that failing individual subscription to the typed heartbeat
      owner with bounded work timeout 120s and heartbeat 15s under the accepted per-subscription fallback ruling.
```

Replace section 8 with:

```markdown
## 8. Governance settlement and correlation proof

- [ ] 8.1 Convert task/request/response validation handlers from void to classified outcomes returned through the
      existing private governance binding owner.
- [ ] 8.2 Publish allowed messages through the declared JetStream output with deterministic identity and synchronous
      PubAck; remove core-NATS publication from the settlement proof.
- [ ] 8.3 Preserve the current nonblocking audit contract without treating filter, marshal, resolution, or required
      output failure as successful ACK.
- [ ] 8.4 Add stable proposal identity and fingerprint without changing #1140 policy content.
- [ ] 8.5 Replace missing/full-waiter log-and-drop with validated retained-verdict recovery.
- [ ] 8.6 Test replacement before proposal, after proposal, after verdict ACK, and before tool publication.
- [ ] 8.7 If retained verdict and response redelivery succeed, add no governance bucket; if they fail, stop at the
      named failpoint for owner review.
- [ ] 8.8 Set explicit AckWait 30s and work budget 25s on all three governance subscriptions; prove cancellation and
      join leave 5s margin. Migrate only a failing individual subscription with bounded work timeout 30s; heartbeat
      must be a separately owner-reviewed concrete value <=15s because accepted evidence proves only that ceiling.
```

Replace section 9 with:

```markdown
## 9. AGENT replay admissibility

- [x] 9.1 Preserve the owner-selected strong observed `DiscardNew` contract.
- [ ] 9.2 Implement pure repo-internal `internal/agentstreamadmission.ObserveAndValidate`. Invoke it in each affected
      model/dispatch/governance/loop Start after resolved port facts and before that component's first dependent
      allocation; dispatch gates USER intake, and first-party AGENT publishers validate their resolved output before
      publish. Each caller builds requirements only from its own resolved facts and typed local
      AckWait/BackOff/MaxDeliver/work/replay/PubAck dependencies; use no cross-component config, shared maxima,
      factory-name/raw-JSON switch, durable/process state, watcher, or exported adopter surface.
- [ ] 9.3 For each affected caller, compute only its local horizon from resolved AckWait, BackOff, MaxDeliver,
      maximum local work/replay need, and required producer PubAck dependency. Loop timeout informs only loop's local
      `Requirement`; exclude approval lifetime from stream admission and validate it only in `loopbucket.AcquireOwner`.
- [ ] 9.4 Refuse the shipped `DiscardOld` posture with typed `agent_stream_replay_inadmissible`, no affected consumer
      start, false readiness, and exact observed/required fields; never mutate server policy silently.
- [ ] 9.5 Migrate owned config, fixtures, and E2E to `DiscardNew`; test full-stream backpressure preserves the source
      delivery and never falls back to core NATS.
- [ ] 9.6 Test insufficient MaxAge, MaxMsgs/MaxMsgsPerSubject early eviction, missing StreamInfo, and external evidence
      deletion separately from admitted recovery.
- [ ] 9.7 Start model/dispatch/governance/loop concurrently against inadmissible resolved AGENT streams and prove each
      affected component remains not ready with zero dependent allocation and zero positive publisher settlement;
      prove queued USER remains unconsumed, a non-agentic component performs zero lookup and starts, mixed overrides
      use their resolved stream identity, and admitted observation precedes each affected allocation.
- [ ] 9.8 Separately resolve agentic-loop's `loops` KV-write bucket from admitted port facts. Acquire it through
      internal `loopbucket.AcquireOwner`: KeyValue first; CreateKeyValue only for exact typed
      `jetstream.ErrBucketNotFound`; one KeyValue retry for `jetstream.ErrBucketExists`; after get/create/race-get,
      observe actual status and publish the handle only when History 10/TTL 24h/non-binding MaxBytes match.
- [ ] 9.9 Refuse missing-status, drifted retained History/TTL, or positive MaxBytes without reconciliation; name every
      expected/observed value and allocate zero loop consumer/sweeper work.
- [ ] 9.10 Real-NATS test absent, matching retained, same-config concurrent create, foreign-config race winner, each
      drift arm, and status failure. Fake-test non-not-found lookup → zero create and create-exists → exactly one get.
      Keep AGENT stream admission and AGENT_LOOPS authority as two ordered gates; validate approval timeout only here.
```

Add dispatch fast-boundary task:

```markdown
- [ ] 6.22 Set explicit AckWait 30s and work budget 25s on dispatch user-message, created, and approval-pending
      subscriptions; prove cancellation and join leave 5s margin or migrate only the failing individual subscription to the
      admitted heartbeat owner with bounded work timeout 30s and heartbeat 10s.
```

Replace tasks 6.13-6.14 with:

```markdown
- [ ] 6.13 Derive the deterministic key from canonical `ApprovalContinuationV1` payload JSON excluding BaseMessage
      UUID/timestamp. On every get/read-back, production-decode to `*ApprovalContinuationV1`, validate, compare
      canonical payload bytes, recompute digest/key, and verify every semantic identity field before reuse.
- [ ] 6.14 Test matching reuse, malformed and semantic collision, transient Get, and a lost Put reply retried with a
      freshly constructed registered envelope whose UUID/timestamp differ but canonical payload is identical.
```

Replace task 11.1 and tasks 11.4-11.8 with:

```markdown
- [ ] 11.1 Add table-driven unit/property/fuzz tests for every lane using only the requirement/scenario citations in
      design `Cross-lane invariants and proof sources`; no property is reconstructed from implementation.
- [ ] 11.4 Correct false restart claims in `docs/concepts/03-streams-vs-kv-watches.md`,
      `docs/concepts/17-approval-flow.md`, and `docs/concepts/27-frontier-harness-mapping.md`; reconcile model 60s and
      loop 15s heartbeat defaults in timeout/JetStream tuning docs, generated schemas, and every example/test fixture.
- [ ] 11.5 Link `docs/concepts/33-semantic-settlement.md` and document only #1146's provider-ambiguity and approval-
      continuation exceptions; do not duplicate the message-pump concept.
- [ ] 11.6 Confirm PR #1159 shows `Closes #1146`, `Refs #759`, `Refs #1155`, `Refs #1249`, and, if the recommended
      nested deletion is accepted, `Refs #1239`, plus the full accepted scope and `implemented-by` record. Confirm
      PR #1251 retains `Closes #1239`, `implemented-by: opus`, and its own review provenance, while final PR #1156
      carries `Closes #1239` before its complete-claim implementation and cross-agent reviews.
- [ ] 11.7 Complete implementation and proof, then obtain SemStreams implementation review of the complete claim set.
- [ ] 11.8 Obtain the owner-requested cross-agent review.
- [ ] 11.9 Apply every finding and repeat implementation and cross-agent review until accepted.
- [ ] 11.10 Archive `agentic-loop-restart-safety` as the final content commit.
- [ ] 11.11 Historical five-delta archive task; superseded by the active six-delta archive task in `tasks.md`.
```

Remove the `Hold: AgentRun` section and replace it with:

```markdown
## Transferred: AgentRun

Tasks H.1/H.2 are removed. #1249 owns post-#1146 inventory, design, complete/failed migration, and replacement proof
against exact staged parent checkpoint `A`. This transfer does not narrow any other #1146 lane.
```

Undraft, CI, remote-base verification at merge time, and hosted merge remain hosted landing checks after task 11.11;
they are not OpenSpec implementation tasks.

## Exact spec-delta amendments

### `specs/agentic-dispatch/spec.md`

Replace the first requirement body with:

```markdown
### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner. Business handlers SHALL receive an immutable owner-supplied work view —
payload plus exact `F` `DeliveryAttempt` only where the typed heartbeat binding owns it — and SHALL return a typed
semantic outcome; native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, signal, approval, and user-response
publication has synchronous JetStream PubAck. Created and approval-pending events SHALL settle after projection update
or exact proof from `AGENT_LOOPS`. Terminal events SHALL retain their existing typed ancestry/read-through and
deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become ACK.

The `user.message`, `agent.created`, and `agent.approval_pending` subscriptions SHALL each acquire with explicit
AckWait 30s and enforce a 25s cancellation-and-join work budget, leaving a positive 5s lease margin. Budget expiry
SHALL return Retry without ACK. If a physical subscription cannot prove this bound, that individual subscription SHALL
move to the admitted typed heartbeat owner with bounded work timeout 30s and heartbeat 10s before implementation
review. Other subscriptions in its dispatch proof group SHALL not migrate solely because this one did.
```

Add scenarios:

```markdown
#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its deterministic typed user error receives PubAck before termination
- **AND** tracker and gauge state remain unchanged

#### Scenario: Created event arrives after replacement

- **WHEN** dispatch has no process tracker entry
- **AND** the exact `AGENT_LOOPS` record proves the same loop
- **THEN** dispatch updates or reconstructs its projection
- **AND** positively acknowledges without treating cache absence as not-found

#### Scenario: Terminal publication is uncertain

- **WHEN** the deterministic user response does not receive PubAck
- **THEN** the terminal source is not positively acknowledged
- **AND** replay uses the same source-derived response identity

#### Scenario: Fast dispatch work reaches its budget

- **WHEN** any fast dispatch dependency remains incomplete at the 25-second work boundary
- **THEN** the owner cancels and joins the work before the 30-second AckWait expires
- **AND** returns Retry without positive settlement or concurrent redelivery
```

Keep all existing projection scenarios.

### `specs/agentic-loop/spec.md`

Replace the first requirement with:

```markdown
### Requirement: All six loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. The four loop subscriptions (rows 7-10 of the ten physical raw fast subscriptions) — cancel signal,
approval response, approved verdict, and rejected verdict — SHALL retain native settlement only in their private binding owner and SHALL expose no native
message or exported no-heartbeat adapter.

Each fast physical subscription SHALL acquire with AckWait 30s and enforce a 25s cancellation-and-join work budget,
leaving a positive 5s lease margin. Budget expiry SHALL return Retry without ACK. If any physical subscription cannot
prove this bound, that individual subscription SHALL move to the admitted typed heartbeat owner before implementation
review with bounded work timeout 120s and heartbeat 15s. Other subscriptions in either loop proof group SHALL not
migrate solely because this one did.

Decode, correlation, KV, Store, transition, and required publication failures SHALL NOT become successful callback
completion. ACK means the lane-specific durable transition or defined refusal and every required PubAck completed;
Retry means stable identity and reconciliation make re-execution safe; Terminate means permanently invalid with no
useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure prevents a safe choice.
```

Add scenarios:

```markdown
#### Scenario: Semantic identity collision quarantines

- **WHEN** one stable semantic identity is observed with different canonical content
- **THEN** agentic-loop quarantines and stops the exact owner
- **AND** does not apply either value by preference

#### Scenario: Unknown signal is permanent

- **WHEN** a registered `UserSignal` carries a signal enum other than cancel after #1239 deletion
- **THEN** the delivery terminates as permanently invalid
- **AND** no warning-only return becomes ACK

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the deterministic terminal event receives PubAck before source ACK

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** the panic becomes Quarantine and stops the exact owner
- **AND** it is never rewritten to a nil error

#### Scenario: Verdict arrives without a waiter

- **WHEN** an exact valid verdict arrives after process replacement with no waiter
- **THEN** it remains recoverable through its retained identity for response replay
- **AND** missing or full process channel is not treated as completed log-and-drop

#### Scenario: Fast loop work reaches its budget

- **WHEN** cancel, approval-response, approved-verdict, or rejected-verdict work remains incomplete at 25 seconds
- **THEN** its private owner cancels and joins the work before the 30-second AckWait expires
- **AND** returns Retry without ACK or concurrent redelivery
```

Amend replay admission so every recovery-dependent agentic component SHALL call the shared pure
`internal/agentstreamadmission.ObserveAndValidate` after resolving its admitted port facts and before its own first
dependent consumer/subscription/observer/worker allocation. Stream identity SHALL come from resolved
`component.PortFacts`, never a factory-name/raw-config switch. Dispatch SHALL admit its resolved AGENT outputs before
USER intake; first-party AGENT publishers SHALL admit their resolved output before publish and propagate refusal
through the source outcome. Non-agentic components SHALL perform zero lookup and remain startable. Rejection returns
typed `agent_stream_replay_inadmissible`, leaves only the affected dependency closure not ready, and allocates or
positively settles nothing within it. No ComponentManager change, durable/process state, watcher, or exported adopter
surface is introduced.

Each component SHALL construct its requirement only from its own resolved facts and typed local config: AckWait,
BackOff, MaxDeliver, maximum local processing/replay need, and producer PubAck dependency. No component SHALL read
another component config or shared/global maxima. The strongest local requirement SHALL refuse only that closure.
Approval timeout and AGENT_LOOPS TTL/capacity SHALL be validated only by loop-owned bucket acquisition.

Add:

```markdown
#### Scenario: Concurrent components reject before allocation

- **WHEN** model, dispatch, governance, and loop start concurrently against inadmissible resolved AGENT streams
- **THEN** every affected component returns the typed refusal and remains not ready
- **AND** no dependent consumer, subscription, observer, worker, or publisher success is allocated in those closures
- **AND** an already queued USER delivery remains unconsumed and unacknowledged

#### Scenario: Non-agentic and mixed-stream components are isolated

- **WHEN** the same composition includes an unrelated non-agentic component and agentic stream overrides
- **THEN** the non-agentic component performs zero admission lookup and can start
- **AND** each affected agentic component observes only the stream identity in its resolved port facts
```

Add:

```markdown
### Requirement: Loop-state authority is acquired and observed before loop work

Agentic-loop SHALL resolve its loop bucket from the admitted `loops` KV-write port and call internal
`loopbucket.AcquireOwner`. The helper SHALL get first, create only when the get returns typed
`jetstream.ErrBucketNotFound`, return every other lookup error without create mutation, and on typed
`jetstream.ErrBucketExists` perform exactly one get before observation. It SHALL create with History 10, TTL 24h, and
no binding MaxBytes, then observe actual bucket status. It SHALL publish the handle and start
dependent consumers/sweeper only when actual History is 10, TTL is exactly 24h, and MaxBytes is non-binding. It SHALL
not reconcile drifted retained policy. It SHALL validate configured approval timeout against this observed authority.
This gate is distinct from AGENT stream replay admission.

#### Scenario: Two owners race to create the same fresh bucket

- **WHEN** two processes concurrently acquire the same absent loop bucket with the matching declaration
- **THEN** one create wins and the other opens the existing bucket
- **AND** both observe and accept the same actual History, TTL, and MaxBytes before dependent work

#### Scenario: A retained or race-winning bucket has drifted policy

- **WHEN** actual History, TTL, or MaxBytes differs from the loop authority contract
- **THEN** agentic-loop refuses startup without updating the retained bucket
- **AND** publishes no handle and allocates no dependent consumer or sweeper

#### Scenario: Lookup fails for a reason other than absence

- **WHEN** the initial KeyValue lookup returns permission, timeout, transport, or any non-not-found error
- **THEN** acquisition returns that error and calls CreateKeyValue zero times

#### Scenario: Concurrent create wins between lookup and create

- **WHEN** absence permits create and CreateKeyValue returns `jetstream.ErrBucketExists`
- **THEN** acquisition performs exactly one KeyValue get
- **AND** observes and validates the race winner before publishing a handle
```

Amend approval continuation storage to require production decoding into `*ApprovalContinuationV1`, payload
validation, canonical payload JSON equality, recomputed digest/key equality, and LoopID/TaskID/RequestID/execution
identity/provider CallID/ordinal equality. BaseMessage UUID and timestamp SHALL be ignored because each retry wraps a
fresh normal envelope.

Add:

```markdown
#### Scenario: Retry constructs a fresh continuation envelope

- **WHEN** the first continuation Put reply is lost and retry constructs a new registered BaseMessage envelope
- **AND** the new envelope UUID and timestamp differ but canonical `ApprovalContinuationV1` payload is identical
- **THEN** production decode, validation, canonical payload comparison, digest, key, and identity checks accept reuse
- **AND** the existing object is not treated as a semantic collision
```

Keep the existing approval, lifecycle, and recovery requirements; add the property-source scenario rather than
duplicating them.

Add:

```markdown
### Requirement: Long-running loop heartbeat policy is valid before acquisition

Agentic-loop task, response, and tool-result consumers SHALL default to heartbeat 15s against BackOff `[30s, 2m]`.
They SHALL validate the exact acquisition config before allocating any consumer; heartbeat SHALL be no greater than
half the shortest positive BackOff.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s, 2m]`
- **THEN** setup returns the typed policy error naming the observed values and 15s ceiling
- **AND** allocates no consumer
```

### `specs/agentic-model/spec.md`

Keep the current delta and add:

```markdown
#### Scenario: Default unresolved redelivery performs no second provider call

- **WHEN** a redelivered request has no exact matching committed response
- **AND** policy is `fail_commit_unknown`
- **THEN** the provider invocation count for that delivery is zero
- **AND** the typed commit-unknown response receives PubAck before source ACK

#### Scenario: Response publication fails after provider return

- **WHEN** a provider returns and response publication has no PubAck
- **THEN** the source is not positively acknowledged
- **AND** replacement applies the configured ambiguity policy without assuming the provider did not run
```

Clarify that exact response lookup is permitted only after AGENT admission succeeds; absence outside admitted
retention is unknown, never proof that invocation did not occur.

Add:

```markdown
### Requirement: Model heartbeat policy is valid before acquisition

Agentic-model SHALL default to AckWait 120s and heartbeat 60s. It SHALL validate the exact acquisition config before
allocating a consumer; heartbeat SHALL be no greater than half the shortest positive BackOff when BackOff exists,
otherwise no greater than half the positive AckWait or effective server default.

#### Scenario: Legacy model default is refused before allocation

- **WHEN** setup observes heartbeat 90s and AckWait 120s
- **THEN** setup returns the typed policy error naming the observed values and 60s ceiling
- **AND** allocates no consumer
```

### `specs/agentic-tools/spec.md`

Keep the current delta and add:

```markdown
### Requirement: Tool delivery retains the permanent typed owner contract

The existing `tool.execute` binding SHALL continue to use the permanent typed heartbeat owner, retain
`TOOL_CALL_OUTCOMES` as its sole completed authority, drain its exact consume handle, await exact `Closed`, and join
its owner-stop observer. #1146 correlation changes SHALL NOT introduce a second outcome owner or expose native
settlement to executors.

#### Scenario: Correlation migration preserves completed replay

- **WHEN** a completed outcome is read under framework execution identity
- **THEN** exact ToolResult replay occurs without executor invocation
- **AND** provider CallID remains present as conversation data
```

### New `specs/agentic-governance/spec.md`

Add:

```markdown
## ADDED Requirements

### Requirement: Governance validation settles after its declared consequence

The task, request, and response validation subscriptions SHALL return classified outcomes through their existing
private binding owners. Native messages and settlement methods SHALL NOT enter filter business logic or an exported
no-heartbeat adapter.

Each of the task, request, and response validation subscriptions SHALL acquire with explicit AckWait 30s and enforce
a 25s cancellation-and-join work budget, leaving a positive 5s lease margin. Budget expiry SHALL return Retry without
ACK. If any physical subscription cannot prove this bound, that individual subscription SHALL move to the admitted typed
heartbeat owner with bounded work timeout 30s before implementation review. The concrete heartbeat interval is
unproven beyond the accepted `<=15s` policy ceiling and SHALL receive owner review before fallback is enabled; no
other subscription in the governance proof group migrates automatically.

For an allowed message, done SHALL require deterministic publication through the declared JetStream output and
synchronous PubAck. For a blocked message, done SHALL be the completed policy decision and deliberate non-forwarding.
The existing governance audit contract remains nonblocking, but decode, filter-chain, output-subject, marshal, and
required output-publication failures SHALL NOT become ACK.

#### Scenario: Allowed output publication fails

- **WHEN** policy allows a message
- **AND** the declared validated output does not receive PubAck
- **THEN** the source delivery retries with the same semantic output identity
- **AND** no core-NATS fallback authorizes ACK

#### Scenario: Policy blocks a message

- **WHEN** policy completes and refuses forwarding
- **THEN** the source may be positively acknowledged
- **AND** non-forwarding is the declared terminal consequence
- **AND** an audit sink failure remains observable without reversing the policy decision

#### Scenario: Filter dependency fails

- **WHEN** the filter chain cannot complete because a dependency is transiently unavailable
- **THEN** the source delivery retries
- **AND** the callback does not log and return successful completion

#### Scenario: Governance handler panics

- **WHEN** validation work panics or observes a semantic identity collision
- **THEN** the delivery quarantines and the exact owner stops

#### Scenario: Governance work reaches its budget

- **WHEN** a validation dependency remains incomplete at the 25-second work boundary
- **THEN** the private owner cancels and joins before the 30-second AckWait expires
- **AND** returns Retry without ACK or concurrent redelivery
```

## Verification and process-replacement gates

### Unit, property, and lifecycle

- One table row per each of the 17 lanes: happy decision, every classified sad decision, terminal-method call, and
  settlement error observation.
- Exhaustive decision switch tests; unknown decision cannot ACK.
- No native message reaches business handlers; no direct ACK/NAK remains outside the private binding owners and the
  #759 terminal-method gate.
- All five owners stop admission, drain all retained handles before waiting, await exact `Closed`, cancel, and join;
  timeouts report failure and never pretend clean shutdown.
- `runWithBudget` and trajectory batch work cannot return before their goroutines join.
- Every property/fuzz test cites the exact requirement/scenario table above.
- The exact acquisition config is captured once and passed unchanged to heartbeat validation and consumer allocation;
  model 90/120 and loop 60/[30s,2m] fail with zero allocation, while model 60/120 and loop 15/[30s,2m] pass.
- Approval continuation reconciliation production-decodes both envelopes, ignores fresh envelope UUID/time, and
  requires canonical payload, digest/key, and all semantic identity fields to match.

### #1146-owned tranche of the real-NATS #1155 replacement matrix

For each named boundary, kill the process rather than calling `Stop`, restart against retained file-backed NATS, and
prove convergence plus invocation/publication counts:

1. user task PubAck before user-response PubAck and before source ACK;
2. task loop birth, graph birth, initial request, created event, and source ACK;
3. model before provider call, during call, after return, after response PubAck, and before source ACK;
4. response before/after loop persistence and each required next publication;
5. tool result before result persistence, continuation Store verification, next publication, and source ACK;
6. approval while pending, after approve/modify/reject/timeout decision, before tool PubAck, and before pending clear;
7. cancel before current-state/`COMPLETE_` write, terminal PubAck, and source ACK;
8. governance allowed publication and blocked decision; verdict before proposal, after proposal, after verdict ACK,
   and before tool publication;
9. dispatch projection replacement with empty cache and interrupted/complete snapshots; and
10. NATS restart from the same file store for representative model, tool-result, approval, and governance cases.

Run four parallel proof groups covering all ten physical raw fast subscriptions with production AckWait 30s and work
budget 25s:

- dispatch (`user.message`, `agent.created`, `agent.approval_pending`);
- governance (`task_validation`, `request_validation`, `response_validation`);
- loop signal/approval (`agent.signal`, `agent.approval_response`); and
- loop verdict (`agent.toolcall.approved`, `agent.toolcall.rejected`).

For every physical subscription, block its final required dependency through the 25-second boundary, prove cancellation and
all child joins complete before 30 seconds, prove Retry/no ACK, and observe no concurrent delivery before callback
return. Then run legitimate context-cooperative work whose measured completion exceeds 25 seconds: strict completion-
before-lease fails for that physical subscription alone, while its exact fallback route (dispatch 30s/10s; loop
120s/15s; governance 30s with owner-reviewed heartbeat <=15s) renews, completes once, and settles through the same
outcome. No sibling migrates because it shares a proof group. A cancellation-ignoring or non-returning dependency fails lifecycle review under
both routes and is never heartbeat evidence. These are real file-backed NATS tests, not mocked timer assertions;
parallel execution bounds wall-clock without weakening production values.

Assertions include: no silent loss, no log-and-ACK, no second default-policy provider call, no duplicate completed
tool execution, no ungoverned model admission, no raw fallback, no false empty projection, and no indefinitely stranded
nonterminal loop within the admitted horizon.

### Admission and E2E

- Real `StreamInfo` cases: shipped `DiscardOld` refusal, admitted `DiscardNew`, insufficient MaxAge, earlier message
  bounds, unavailable metadata/stream info, and full-capacity publish backpressure.
- Affected-closure admission: each model/dispatch/governance/loop instance observes its resolved stream before its own
  dependent allocation; refusals leave those closures not ready with zero allocation/positive source settlement and
  queued USER unconsumed. A non-agentic component performs zero lookup and starts; mixed overrides observe their own
  resolved streams; first-party publisher refusal propagates through its source outcome. Divergent local configs
  prove model/dispatch/governance/loop requirements use only their own AckWait/BackOff/MaxDeliver/work/replay/PubAck
  facts: strengthening one closure changes no other requirement, while an under-admitted local closure refuses.
- `AGENT_LOOPS` authority: absent fresh create, matching retained open, same-config two-process create race,
  foreign-config race winner, drifted History/TTL/MaxBytes, and status failure. Every accepted case observes actual
  History 10/TTL 24h/non-binding MaxBytes before handle publication; every refusal allocates zero loop consumer or
  sweeper. Fake tests prove non-not-found get error → zero CreateKeyValue and typed create-exists → exactly one
  race-get then observation. Approval timeout is checked only against this observed bucket authority. These tests are
  separate from AGENT stream admission.
- Payload registry: first-party root census, normal envelope round trip through production decoder, absent registration
  loud failure, control indexing floor, nil projections.
- Store: matching reuse, malformed/collision, unknown Put reply, transient Get, permanently missing reference,
  best-effort cleanup metric, and deliberate AGENT evidence eviction while pending authority remains.
- Store retry equality: construct fresh registered envelopes with different BaseMessage UUID/timestamp around one
  canonical continuation payload, lose the first Put result, and prove decoded validated reuse; change one semantic
  payload field under the same key and prove quarantine.
- Run focused race tests, full race/integration, lint, build, schema generation, contract tests, and serialized
  `task e2e:agentic`. Because AGENT policy changes from the shipped configuration, the covering E2E must be green
  before the breaking cutover lands.

## Documentation ownership

#759 owns the promised plain-language `docs/concepts/33-semantic-settlement.md`: message pump, lease watchdog,
component-defined done, replay/reconciliation, and ACK/Retry/Terminate/Quarantine. #1146 does not create a second
concept document. It links that concept and corrects only the three false restart claims pinned in task 11.4, plus
documents provider ambiguity, approval Store requirements, strong AGENT admission, metrics, and raw external-executor
migration. The same documentation pass updates operator-visible model heartbeat default 60s and loop heartbeat
default 15s, explains that loop BackOff `[30s,2m]` makes 30s the effective validation interval, regenerates config
schemas, and reconciles every example/test fixture. An invalid operator value fails setup before allocation with the
component, port, observed heartbeat, effective interval, and ceiling; operators never choose ACK/NAK mechanics.
`processor/agentic-loop/README.md` also states the separate boot order: resolved AGENT stream admission, then
idempotent `AGENT_LOOPS` acquisition and actual History/TTL/MaxBytes observation, then dependent loop work. It names
retained drift as fail-closed and never tells operators to rely on a create literal or automatic reconciliation.

## Hosted PR implications

PR #1159 remains draft, targets `codex/gh759-semantic-settlement`, retains `Closes #1146`, `Refs #759`, `Refs #1155`,
`Refs #1249`, and `implemented-by: Sol`. Replace its current-stop text after review with exact inventory identity and
this design artifact's SHA/line count; do not claim implementation before owner acceptance.

If the recommended #1239 nested child is accepted, preserve the existing hosted record rather than opening a new
claim:

- read and pin PR #1251's current remote head immediately before staging (review observed
  `943570116f2eeb2237715eb81bbd11d2d04e4a6a`, including six fixes), freeze that head, return the PR to draft, rebase
  `claude/gh1239-delete-pause-resume` onto the exact current remote #1159 head, retarget its base from `main` to
  `codex/gh1146-agentic-loop-restart`, and verify no unrelated or duplicate #1146 claim entered its diff; never replay
  obsolete `d21e26122d0cb0095e3e4a988bae444c17c8ba77`, and repeat pin/rebase/proof/re-review after any remote head advance;
- PR #1251 retains `Closes #1239`, `Refs #1146`, `implemented-by: opus`, and its own author/implementation-review
  provenance; after rebase it repeats CI, implementation review, owner-requested cross-agent review, fixes/re-review,
  final-content OpenSpec archive, and narrow archive/current-spec-sync review before its non-default merge;
- PR #1159 adds `Refs #1239` before complete implementation review; and
- final default-branch PR #1156 adds `Closes #1239` before its complete-claim implementation and cross-agent reviews.

PR #1159's non-default staging merge does not close #1146. After its implementation/proof and complete claim set:

1. SemStreams implementation review;
2. owner-requested cross-agent review;
3. fixes and repeat both reviews until accepted;
4. archive as the final content commit;
5. narrow archive/current-spec-sync review;
6. only then hosted remote-base verification, CI, undraft, and non-default merge.

At merge time, remote parent must still equal exact `F`; otherwise stop and repeat pin/rebase/test/review. The staged
merge produces checkpoint `A` for #1249. #1249 independently inventories, designs, implements, reviews, archives, and
stages against `A`. Only after both child archives/current-spec sync, #759 zero-caller helper removal, and the
combined #1155 proof including #1249 AgentRun
proof rows does PR #1156 carry the final complete closing-keyword set and perform atomic default-branch integration.

## What changed from the prior accepted design

- Merge-first is removed at active design lines 5, 17, 22, and 408. Exact non-default `F` is the reviewed authority.
- Model and three loop binding migrations are explicit additions without narrowing the original #1146 scope.
- AgentRun/#1148 holds at design lines 19, 420, 424 and tasks H.1/H.2 transfer to #1249 from checkpoint `A`.
- The ten physical fast subscriptions now have explicit private owners, definitions of done, and no-heartbeat justification;
  exact AckWait 30s / work 25s / join 5s replaces the earlier unmeasured no-heartbeat posture, and failure of that
  proof compels migration of only that individual physical subscription to the admitted heartbeat owner. No exported fast settlement
  API is implied.
- Model heartbeat default is corrected from invalid 90/120 to 60/120; loop heartbeat default/schema/fixtures are
  corrected from invalid 60/[30s,2m] to 15/[30s,2m], with exact pre-allocation config validation.
- The unapproved generic ComponentManager/raw-config preflight is removed. One shared internal read-only AGENT
  validator runs only in affected closures, after resolved port facts and before their own allocation; non-agentic
  components do zero lookup. Whole-composition refusal remains the rejected alternative because the accepted ruling
  fenced recovery-dependent consumers, not unrelated work.
- `AGENT_LOOPS` now has a separate executable owner-acquisition contract: resolved KV-write port, idempotent
  get/create/race-get, actual History 10/TTL 24h/non-binding MaxBytes observation, then handle publication and loop
  consumer/sweeper allocation.
- Exact foundation `F`'s `DeliveryAttempt` replaces every stale pre-F claim that #759 is unimplemented or bytes-only.
- Approval continuation collision checks compare production-decoded canonical payload semantics and ignore fresh
  BaseMessage UUID/timestamp.
- Governance's declared JetStream outputs require synchronous PubAck; current core-NATS publish cannot authorize ACK.
- Existing PR #1251 preserves #1239 authorship/review provenance while staging its deletion onto #1159; #1146 owns
  only cancel settlement and cannot review a warning/void/ACK pause branch.
- The actual shipped `DiscardOld` posture is named as a startup refusal and owned config migration, not abstract policy.
- Property/fuzz invariants now cite their normative spec requirement/scenario; semantic collision gains a spec home.
- Review, cross-agent review, final-content archive, and narrow archive review are explicitly ordered.

## Risks and unproven claims

- Exact retained-message lookup APIs and performance are not yet implemented or measured for every AGENT subject.
- The accepted inventory proves the AGENT policy mismatch but does not separately admit a strong retention policy for
  the USER and TOOL source streams used by rows 1, 11, and 17. Before claiming the complete 17-lane horizon,
  implementation review must either show that their exact source-delivery guarantees are already sufficient under
  observed bounds or stop for an inventory/design amendment; AGENT admission must not be cited as proof for another
  stream.
- Model provider adapters have not demonstrated `provider_reconcile`; only `fail_commit_unknown` is the safe default.
- Governance deterministic output fingerprint and exact committed-output lookup are target state, not current truth.
- `AGENT_LOOPS` matching/race/drift behavior is target state. The existing 24-hour create literal alone is not proof;
  actual status must be observed, and preexisting sibling product creators remain a collision to catch in the
  foreign-config race test rather than an excuse to reconcile.
- The approval Store failpoint has not yet proved deliberate AGENT eviction recovery end to end.
- The 25s work/5s join boundary has not yet passed the required real-NATS tests. If any physical subscription cannot
  cancel and join before its 30s lease, strict completion-before-lease is rejected for that subscription and the
  admitted heartbeat owner is required with the matrix's bounded local work/heartbeat policy; neither a longer
  unrenewed timeout nor silent ACK is permitted. Governance's exact heartbeat value is unproven beyond <=15s and
  cannot be enabled without owner review.
- `internal/agentstreamadmission` and affected-component/publisher invocations are target state, not current truth;
  resolved-stream, non-agentic-zero-lookup, and zero-dependent-allocation proofs are required.
- Canonical continuation equality is target state; raw envelope byte equality would falsely quarantine a retry solely
  because normal BaseMessage UUID/timestamp changed.
- #1239 nested staging needs explicit owner acceptance because it adds one child PR and one final closing keyword to
  the reviewed greenfield stack.

## Owner rulings and residual conditional review

The owner accepted both recommendations on 2026-09-02, in this exact implementation context:

1. “existing PR1251 nested directly onto #1159 current staged head with re-review”; and
2. “per-physical-subscription strict 30/25/5 first with evidence-triggered heartbeat fallback only for a failing
   subscription.”

These rulings close the two open design choices. They authorize preserving the existing PR #1251 claim while
re-pinning, rebasing, retargeting, testing, and re-reviewing it on #1159, and they authorize fallback only for the
specific physical subscription whose real-NATS boundary proof fails. Proof-group membership never authorizes sibling
migration. Dispatch fallback is bounded work 30s / heartbeat 10s; loop fallback is bounded work 120s / heartbeat
15s. Governance fallback is bounded work 30s, but accepted evidence derives only the heartbeat policy ceiling
`<=15s`, not one concrete interval. If a governance subscription triggers fallback, selecting and reviewing that
concrete interval remains a conditional stop gate, not an open standing architecture choice and not permission to
guess.

Prior accepted rulings remain unchanged: `fail_commit_unknown` default; immutable `DeliveryAttempt`; 12-hour approval
default within observed authority; strong observed `DiscardNew`; best-effort metered continuation cleanup; no
supervisor/state machine/checkpoint/outbox/CQRS; AgentRun transfer to #1249; and #1146-before-#1244 composition.

Whole-composition versus affected-closure admission is not a third open ruling: the accepted prior wording limits
refusal to recovery-dependent consumers, and the adopter seam requires unrelated components to pay zero lookup. A
generic ComponentManager gate would reopen both decisions and require a `framework-composition` delta; this design
does not propose it. The owner may explicitly reopen that ruling, in which case implementation stops for a new delta.
Local `Requirement` authority also introduces no new choice: it is the executable form of that same affected-closure
ruling. Cross-component config/global maxima would recreate whole-composition coupling the ruling rejected; approval
timeout belongs to the loop-owned bucket authority it constrains.

## Stop conditions

Stop implementation or review if any of these occurs:

- remote `codex/gh759-semantic-settlement` is not exact `F`;
- inventory verification drifts or a touched authority differs materially;
- active proposal/design/tasks retain merge-first or #1148 AgentRun language;
- #1239's deletion is neither integrated through an accepted sequence nor removed from the complete review surface;
- PR #1251 is duplicated, loses its authorship/review provenance, is not returned to draft/rebased/retargeted onto the
  exact current #1159 head, uses obsolete `d21e26122d0cb0095e3e4a988bae444c17c8ba77`, advances after its fresh remote
  pin, or enters final review without repeating proof and archive gates;
- any fast lane needs native message outside its private owner or an exported no-heartbeat API;
- any fast physical subscription lacks explicit AckWait 30s, enforced 25s cancellation-and-join budget, positive 5s
  margin, or fails its real-NATS boundary proof without migrating to the admitted heartbeat owner;
- model ships heartbeat other than 60s against AckWait 120s, loop ships heartbeat above 15s against shortest BackOff
  30s, or policy validation observes a config different from the one passed to acquisition;
- any affected component allocates dependent work before validating its resolved stream, a factory-name/raw-JSON
  switch determines activation/identity, a non-agentic component performs an admission lookup, or dispatch consumes/
  ACKs USER input after its dependency refusal;
- `AGENT_LOOPS` handle or dependent loop consumer/sweeper is published before actual History/TTL/MaxBytes observation,
  any non-`jetstream.ErrBucketNotFound` lookup error triggers CreateKeyValue, a typed create-exists race is not followed
  by exactly one get-and-validate, or retained drift is silently reconciled;
- active design lines 95-104 or any grep-equivalent text still describe #759 as unimplemented or its work surface as
  context-and-bytes only rather than exact `F`'s immutable `DeliveryAttempt`;
- continuation reconciliation compares raw envelope bytes or treats fresh BaseMessage UUID/timestamp as semantic
  collision instead of production-decoded validated canonical payload equality;
- any handler path would settle ACK after log-only failure, missing process correlation, panic, or unknown publication;
- model redelivery can call the provider twice under default policy;
- AGENT remains `DiscardOld` or actual bounds cannot prove the computed horizon;
- a USER or TOOL source lane relies on a retention premise not measured from its actual stream;
- approval continuation requires a new bucket, graph fact, scanner, or unregistered/raw payload;
- governance still treats core-NATS publish success as synchronous durable PubAck;
- a new durable primitive, supervisor, ledger, checkpoint, outbox, state-machine runtime, or CQRS path appears;
- a delivery goroutine can outlive callback return or component Stop;
- a dependency ignores cancellation or cannot join under either the strict or heartbeat route;
- a property/fuzz test has no cited normative requirement/scenario; or
- implementation review, cross-agent review, archive-as-final-content, and narrow archive review occur out of order.
