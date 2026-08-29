# Inventory: agentic-loop restart-safe settlement

## Checkpoint status

- Baseline: `origin/main@b060511f383d74aa6a8684e39e42020a3b073a9b`.
- Claim commit: `9a9a2ea22474155ca7ce4bdb22a117a2ebcd4a75`.
- Source: completed read-only SemStreams architect census reconciled to that baseline and claim.
- Review state: **PENDING independent `INVENTORY PASS`**.
- This checkpoint authorizes inventory review only. It does not authorize target-state design or implementation.

## Claimed gap

The current implementation has durable inputs and some durable outputs, but it does not consistently connect callback
return—and therefore JetStream settlement—to the required durable business outcome.

`LoopManager` allocates empty maps for loop execution, request/call correlation, tool ordering, cached configuration,
and timing (`processor/agentic-loop/state.go:17-49,68-125`). `ContextManager` separately allocates empty conversation
regions (`processor/agentic-loop/context_manager.go:38-55,74-99`). Startup opens or creates `AGENT_LOOPS`, starts
consumers, and starts the approval sweeper (`processor/agentic-loop/component.go:514-583,772-848`), but the production
search below finds no enumerator, watcher, unmarshal, or startup hydration of `LoopEntity` records.

`setupSubscriptions` routes AgentResponse, ToolResult, signal, approval-response, and rule-verdict inputs through
`adaptVoidInputHandler` (`processor/agentic-loop/component.go:869-921`). That adapter always returns nil
(`component.go:177-181`). Task decoding, type errors, task execution failures, transition persistence failures, and
downstream publication failures are logged and then become successful callback completion
(`component.go:1161-1264,1608-1637,1847-2008`). Missing ToolResult correlation is explicitly logged as
`stale_callid` and dropped (`component.go:1760-1787`).

The model callback similarly invokes a void handler and returns nil
(`processor/agentic-model/component.go:398-407`). Provider invocation has no durable outcome authority; result and
error publication failures remain log-only (`component.go:583-635,717-755,1049`).

## Surface and lane census

### `agent.task.*` / `TaskMessage`

Dispatch publishes; loop `taskInputHandler` calls `handleTaskMessage`. `LoopEntity` is written to `AGENT_LOOPS`, but
decode, type, and `HandleTask` failures return nil (`agentic-loop/component.go:1161-1200`). `publishResults` and
`persistLoopState` are void (`component.go:1259-1264`). `pendingTaskResults` is process-only same-process lineage retry
state (`component.go:99-103,1274-1297`).

### `agent.request.>` / `AgentRequest`

Loop publishes; model consumes. The model heartbeat callback invokes void `handleRequest` and returns nil
(`agentic-model/component.go:398-407`). Provider calls have no durable result authority; response and error
publication are log-only (`component.go:583-635,717-755,1049`). The owner-listed invocation windows remain unclosed.

### `agent.response.>` / `AgentResponse`

Model publishes; loop `handleResponseMessage` consumes. The handler is adapted from void
(`agentic-loop/component.go:893-895,1382-1399`). Decode, type, missing request correlation, persistence, and
publication failures become successful callback completion. Structured request recovery still requires the loop in
the in-memory map (`state.go:983-1005`).

### `tool.execute.>` / `ToolCall`

Loop publishes; tools consumes. #949 already owns immutable completed outcomes in `TOOL_CALL_OUTCOMES`. The completed
result is persisted before result PubAck and replayed without executor work (`agentic-tools/component.go:627-858`;
`outcomes.go:21-30,72-80`). No claimed/in-progress record exists by design. Post-effect/pre-completion ambiguity
remains the executor's CallID-idempotency boundary; #1146 must not duplicate this authority.

### `tool.result.>` / `ToolResult`

Tools publishes; loop `handleToolResultMessage` consumes. The void adapter drops missing process-only CallID mapping
as `stale_callid` (`agentic-loop/component.go:895-896,1760-1787`). `GetLoopForToolCallWithRecovery` parses an identity
but still requires an in-memory loop (`state.go:1007-1029`).

### `agent.signal.*` / `UserSignal`

Dispatch publishes; loop consumes. The void adapter covers pause/cancel/resume
(`agentic-loop/component.go:897-898,2070-2103`); transition, KV write, and downstream publication cannot classify
source settlement.

### `agent.approval_pending.*` / `ApprovalRequest`

Loop publishes; dispatch consumes. Dispatch ACKs after a void process-cache update
(`agentic-dispatch/component.go:616-645,1021-1060`). `LoopTracker` and its early buffer are process-only. Replacement
loses HTTP correlation and returns 404/409 (`http.go:705-872`).

### `agent.approval_response.*` / `ApprovalResponse`

Dispatch HTTP publishes with synchronous PubAck; loop consumes. The response carries LoopID and CallID, and
`PendingApprovalState` persists exact call fields (`agentic/state.go:142-186`), but loop startup does not hydrate
them. The void adapter swallows decode, type, handler, and persistence failures
(`agentic-loop/component.go:899-900`; `approval_response_handler.go:155-187`). Panic becomes nil on the assumption
that the sweeper recovers (`approval_response_handler.go:31-48`).

### `agent.toolcall.proposed.*` / proposed call

The loop governance dispatcher publishes stable LoopID, CallID, tool, and arguments. Enforcement correlation remains
process-only `waiters` (`governance_dispatcher.go:334-384`).

### `agent.toolcall.approved.>` and `.rejected.>` / verdict

The rule path publishes and loop demux consumes. A void adapter receives verdicts
(`agentic-loop/component.go:901-909,2295+`); `HandleVerdict` ignores late or missing waiter arrivals
(`governance_dispatcher.go:468-507`). #1146 owns only proven restart/correlation behavior; #1140 owns
content-governance coverage.

### Terminal loop events / `COMPLETE_<loopID>`

The loop writes and publishes; dispatch and rules observe. Dispatch terminal routing already rereads `AGENT_LOOPS`
and requires synchronous PubAck under `agentic-terminal-events`. That closes terminal projection only, not loop
transition recovery.

## Startup and replacement fact

The approval sweeper claims restart safety (`processor/agentic-loop/approval_sweeper.go:40-42`) but snapshots only
the in-memory loop map (`approval_sweeper.go:65-109`). Because startup does not hydrate that map, the claim is false on
this baseline.

Dispatch approval correlation is also process-only (`processor/agentic-dispatch/component.go:616-645,1021-1060`).
Governance verdict correlation uses process-only waiters; late or replacement-process verdicts without a waiter are
ignored (`processor/agentic-loop/governance_dispatcher.go:334-384,468-507`).

## Same-class collision inventory

- **Semantic class:** source settlement, durable transition/result, replay, and replacement correlation. The job is
  split across JetStream delivery, component state, durable result stores, and operation-specific identity. No
  general recovery owner exists.
- **Catalogs and configuration:** component ports (`processor/agentic-loop/config.go:396-460`), AGENT stream/bucket
  registry, and payload registry. No new subject, bucket, payload, or operator recovery knob is selected.
- **Status and current facts:** `AGENT_LOOPS` `LoopEntity`, `COMPLETE_` records, and the #733 consumer-info inflight
  query. `LoopEntity` is current loop state, not proof of outstanding work. #733 rejects deriving liveness from stale
  loop state.
- **Audit:** `AGENT_TRAJECTORIES` and Store evidence. The `agentic-loop` spec says audit failure does not block
  transition, publication, or ACK. Terminal audit is not a seal, checkpoint, completeness, or recovery authority.
- **Delivery ownership:** #759 / PR #1156 owns stateless settlement; each component owns exact native consume handles.
  #1146 must classify through #759 after acceptance, not add a second settlement or lifecycle owner.
- **Tool-effect replay:** #949 `TOOL_CALL_OUTCOMES`. Reuse only; do not add another call ledger or pre-effect `started`
  marker.
- **Large payloads:** registered `storage.Store` and typed references under ADR-063. This is the existing durable
  material path; no unregistered side store is justified.
- **Approval current call:** `LoopEntity.PendingApproval` and dispatch `LoopTracker` cache. The persisted fact exists,
  but neither loop nor dispatch reconstructs its process indexes. The cache is not authority.
- **Governance correlation:** in-process dispatcher waiters under ADR-039. No durable correlation owner exists. Later
  design may add one only if a named replacement failpoint proves redelivery plus existing results insufficient.
- **AgentRun:** ADR-053 and PR #1148 occupy the same semantic and file neighborhood. Exclude it until #1148 merges and
  the surface is reinventoried.
- **General restart patterns:** #1145, targeted for beta.164, generalizes only patterns proved by #1146 and existing
  components. It does not own this critical vertical.
- **Replacement proof:** #1155, implemented inside #759's change, owns the real-NATS replacement harness/evidence.
  Existing same-process tests are not proof.
- **Governance result content:** #1140 owns tool-result governance/classification, not #1146 settlement/correlation.
- **Lifecycle:** the component/service process owner retains exact consumer handles. No restart supervisor or
  in-process replacement authority was found.
- **Readers:** loop, model, tools, dispatch, governance, HTTP approval clients, rules, and tests depend on a mixture of
  durable IDs and process-only indexes. No single recovery read path exists.
- **Writers:** the same components, HTTP approval input, and rule verdict publishers perform durable writes, but source
  settlement does not consistently observe their outcome.
- **Recovery:** JetStream redelivery, completed tool replay, terminal route reread, and the approval sweeper provide
  lane-specific incomplete coverage. No generic recovery ledger, state machine, or checkpoint implementation exists.

## Active change, issue, spec, and ADR overlap

Fully applicable current specs are `jetstream-consumer-policy`, `nats-streaming`, `runtime-context-ownership`,
`agentic-loop`, `agentic-tools`, and `agentic-terminal-events`. `gated-dag-dispatch` is adjacent only.

The active #759 OpenSpec delta becomes binding only after merge. It individually holds model, all three loop
long-running lanes, and AgentRun behind fresh addenda, review, owner acceptance, and #1155 evidence. PR #1148 clears
only AgentRun's file collision; it does not authorize restart behavior.

Applicable ADRs are ADR-023 (provider adapters), ADR-028 (rules trigger and components execute), ADR-039
(rule-driven tool governance and process waiters), ADR-053 (AgentRun), ADR-063 (Store resolver), and ADR-089
(commit-unknown effects are not safe retry). #759's ADR-095/096 boundary keeps settlement separate from lifecycle and
rejects a second lifecycle authority.

False or overstated documentation appears at `docs/concepts/03-streams-vs-kv-watches.md:171-191`,
`docs/concepts/17-approval-flow.md:65-68`, and `docs/concepts/27-frontier-harness-mapping.md:109-119`.
`recovery_integration_test.go:33-40` is same-process coverage; `recovery_test.go:638-646` manually reconstructs state
and therefore does not prove process replacement.

## Context and lifecycle census

No production struct retaining `context.Context` was found on the touched components. Private cancel functions exist
for exact component, sweeper, and activity lifecycles; they do not establish a second owner. Nearby detached contexts
are bounded terminal/durability helpers (`processor/agentic-loop/trajectory_handler_wiring.go:148`, tools web
emitters, and a recording finalizer). No unbounded `context.WithoutCancel` recovery loop or supervisor was found.

The exact search for `supervisor|checkpoint|outbox|recovery state machine|event sourc|recovery ledger` found only
prose or domain state labels and no generic recovery implementation.

## Adopter seam inventory

### Product or UI developer submitting approval

- **Must know today:** LoopID, exact CallID, reviewed tool and arguments, and response route.
- **If they do nothing:** replacement loses dispatch tracking; HTTP returns 404/409 or loop ACKs an uncorrelated
  response.
- **Discovery today:** runtime logs or HTTP only; no boot or compile proof.
- **Should know:** stable loop identity and decision only. The framework preserves the exact reviewed call.

### Tool executor author

- **Must know today:** repeated CallID may represent the same ambiguous external effect.
- **If they do nothing:** a post-effect/pre-outcome crash can repeat the effect.
- **Discovery today:** prose and runtime behavior; no type enforcement.
- **Should know:** operation-specific idempotency/effect policy, not settlement mechanics.

### Model adapter or operator

- **Must know today:** provider invocation ambiguity and any provider reconciliation/idempotency support.
- **If they do nothing:** paid invocation may repeat or a returned result may be lost.
- **Discovery today:** no durable model-outcome surface; logs only.
- **Should know:** the explicitly selected provider ambiguity contract, not a recovery-bucket prediction.

### SemStreams component author

- **Must know today:** which durable transition/result and downstream PubAck define done on every path.
- **If they do nothing:** a nil/log-only callback ACKs incomplete work.
- **Discovery today:** distributed handler code and false runtime success.
- **Should know:** return a domain decision/error; #759 owns settlement mechanics.

### Framework operator

- **Must know today:** consumer/BackOff and Store registrations.
- **If they do nothing:** restart can strand active loops despite a healthy process and consumer.
- **Discovery today:** misleading docs and same-process tests.
- **Should know:** no supervisor/recovery-mode/checkpoint knob; observe actual replacement outcomes.

The orchestration-boundary check confirms that restart safety does not create a third orchestration layer. Rules still
trigger, lifecycle still owns declared entity phase, and components still execute work. Any later durable fact must
have one exclusive owner and a named reader; this inventory admits no generic supervisor or workflow-aware component.

## Exact gap-closing searches

- Subjects and payloads: all agentic port spellings plus `TaskMessage`, `AgentRequest`, `AgentResponse`, `ToolCall`,
  `ToolResult`, `UserSignal`, `ApprovalRequest`, `ApprovalResponse`, and governance verdict types.
- Delivery and settlement: every `ConsumeWithHeartbeat`, void adapter, `Ack`, `Nak`, `Term`, `InProgress`, and PubAck
  call on the touched agentic surfaces.
- Durable state: every `AGENT_LOOPS` read/write/watch/list operation and every `TOOL_CALL_OUTCOMES` completed-outcome
  read/write/replay path.
- Process correlation: every request, call, waiter, pending-result, approval-tracker, and early-buffer map.
- Context/lifecycle: stored contexts, root creation, detachment, cancel functions, Start/Stop, drain, and consumer
  ownership.
- Adjacent claims: active OpenSpec directories, open PRs, #759, #949, #1140, #1145, #1148, #1155, and the specs and
  ADRs named above.

The production hydration search was:

```text
rg -n 'AGENT_LOOPS|LoopEntity|rehydrat|restore|Watch\(|ListKeys|Keys\('
  processor/agentic-loop --glob '*.go' --glob '!**/*_test.go'
```

It found no `LoopEntity` enumerator, watcher, unmarshal, or startup hydration. No second runtime recovery authority was
discovered.

## Open evidence questions

- Which exact stable identity and replay authority is sufficient at each lane after #759 settlement is available?
- What provider idempotency or reconciliation capability, if any, exists at the model boundary?
- Which committed results can be replayed beyond the NATS duplicate window?
- Can both loop and dispatch reconstruct the exact approval call without adding a duplicate durable authority?
- Which governance replacement failpoint, if any, proves that existing redelivery and results are insufficient?
- What remains after #1155 Stage A replacement proof and PR #1148 merge/reinventory?
