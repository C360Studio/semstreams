# GitHub #1094 Workflow Terminal Delivery — Inventory and Design Handoff

Baseline: `5cc0c7fbe569c6398fc534025218639b4c7e0345` (`origin/main` == `HEAD`, clean working tree, 2026-08-26)

Phase: `pre-owner-design`, revision 3. Inventory review: `INVENTORY PASS WITH DIVERGENCES` on revision 1 (PR #1098
at `01b0f37f`), corrected in revision 2 (`4995b831`, §I.6). Owner ruling (2026-08-26, owner-run Codex round on PR
#1098, recorded on the issue): items 1–7 and 9–10 accepted as recommended; 8 accepted conditionally; 11 —
AGENT_LOOPS plane accepted, traversal corrected — see R4′. Four binding corrections C1–C4 are folded in this
revision (§I.7). Ruled items are binding; the revision as a whole awaits owner acceptance.

Body SHA-256: `27d44cb16e708888e15a90ac67c930ba1742932f5df427930564f21a264d8018` (computed over every line below the `## Complete handoff body` marker)

Owner acceptance: NONE — unsigned architect draft. Binding rulings stay with the owner (Part II §II.9).

Related: #1094 (this), #354 (closed 2026-06-26; `properties` → `TaskMessage.Metadata`), #1090 (open; decision
`reason` rendering), ADR-053 (agent-run substrate), ADR-026/ADR-028 (`decide` terminal tool), spec
`agentic-terminal-events`, spec `user-response-subject-ownership`, archived change
`2026-08-14-normalize-agent-terminal-settlement` (PR #953, commit `0f989366`).

No repository file was changed and no test was run during this pass. Every `file:line` is at the baseline above.

## Complete handoff body

# Part I — Inventory checkpoint

## I.0 Problem statement

A product (semteams) submits a root task over HTTP. The root coordinator loop owns the HTTP route in
`AGENT_LOOPS/<rootLoopID>` and in dispatch's process-local tracker. It terminates with
`decide(action="autoresearch")`, which is a **handoff** to a rule chain, not an answer. Rules spawn baseline,
propose/execute, synthesize, reviewer, and a final coordinator wake-up through `publish_agent`. The final wake-up
terminates with `decide(action="respond_direct", reason="Optimized …")`, which is the **answer**.

Measured at the baseline, dispatch does the opposite of what the user needs:

1. The root's handoff is published as `type=result` because the root loop owns the route
   (`processor/agentic-dispatch/terminal_settlement.go:110-134,198-209`).
2. The wake-up's answer is settled `route_less_settled` (`terminal_settlement.go:192-196`) because a
   `publish_agent`-spawned `TaskMessage` carries no channel fields (`processor/rule/actions.go:1504-1511`) and the
   loop therefore never calls `SetUserContext` (`processor/agentic-loop/handlers.go:494-501`).

Two facts are being modeled: **(F1) which route a workflow's answer belongs to** (origin correlation) and **(F2)
which loop terminal is the answer** (terminal selection). The inventory enumerates every current home of each.

## I.1 Surface inventory

### A. Dispatch terminal settlement path, end to end

| Step | Evidence (file:line at baseline) |
|---|---|
| Two physical terminal lanes, one settlement path | `processor/agentic-dispatch/component.go:521-544` (`agent.complete`), `:570-593` (failure lane); both call `handleTerminalDelivery` `:641-651` → `settleAgentTerminal` |
| Consumer posture | `DeliverPolicy: "new"`, `AckPolicy: "explicit"`, `MaxDeliver: 0`, `AutoCreate: false` (`component.go:522-531`, `:571-580`); `ConsumeWithHeartbeat(…, 10s, …)` (`:644`); permanent errors → `natsclient.TerminateDelivery` (`:646-648`); everything else returns the error → delayed NAK by the natsclient helper |
| Decode (fail-closed, shared) | `internal/agentterminal/terminal.go` `Decode` (the `Event` struct carries `RunID`, `RunEntityID`, `ParentLoopID` is NOT carried, `ChannelType/ChannelID/UserID`, `Result`, `Metadata`); reasons `ReasonEnvelope/Payload/Timestamp/Identity/Collision` |
| Tracker snapshot | `terminal_settlement.go:165` → `loop_tracker.go:194-206` `getSnapshot`; tracker populated at `component.go:872-883` (bus submission, with route), `http.go:315` (HTTP sync submission, with route), `component.go:971-982` (`agent.created` for loops dispatch did not originate — NO route fields) |
| Persisted loop | `terminal_settlement.go:82-108` `loadPersistedLoop` reads `AGENT_LOOPS/<loopID>` via `natsClient.GetKeyValueBucket` with the hardcoded constant `agentLoopsBucket` (`http_activity.go:19-20`) and dispatch declares no KV read port for it (`config.go`) — unlike agentic-tools, which declares `{Name: "agent_loops", Config: KVReadPort{Bucket: "AGENT_LOOPS"}}` (`processor/agentic-tools/config.go:134`), its executors and every `research-graph-*` component, which take `LoopsBucket` from configuration (`processor/agentic-tools/executors/register.go:55-60`, `processor/research-graph-*/config.go`), and agentic-loop, whose bucket name is operator-configurable (`processor/agentic-loop/config.go:54`); a non-default bucket name silently breaks dispatch's own-route read today; `ErrKeyNotFound/ErrKeyDeleted` → transient (`:95-97`); malformed JSON / ID mismatch → permanent (`:101-106`) |
| Route reconciliation | `terminal_settlement.go:55-80` `reconcileTerminalRoute` merges tracker, event, persisted per field (`:38-53`); partial pair → permanent (`:76-78`) |
| Route-less branch | `terminal_settlement.go:192-196`: empty `ChannelType` → `recordCompletionReceived`, reason `route_less_settled`, `return nil` (ACK) |
| Response projection | `terminal_settlement.go:110-134` `terminalResponse`: success → `ResponseTypeResult` with `Content = event.Result` verbatim; failure → `error`; cancel → `status`; `InReplyTo = event.LoopID`; `ResponseID = "terminal-user-response:" + SourceMessageID` (`:16`, `:125`) |
| Publish | `terminal_settlement.go:136-153` `publishTerminalResponse` → `PublishToStreamWithMsgID(subject, data, ResponseID)`; subject `user.response.<type>.<id>` via output port `user.response` (`config.go:93`, USER stream) |
| Dedup contract | `natsclient/client.go:955-963`: dedup holds only within the USER stream duplicate window — the operator's USER `duplicates` declaration (NATS default 2m when unset), clamped client-side to the stream's MaxAge (`config/stream_drift.go:276-283`), so a USER `max_age` below 2m shrinks the "once" window; the spec forbids claiming exactly-once (`openspec/specs/agentic-terminal-events/spec.md` "Delivery declaration SHALL remain bounded and honest") |
| Telemetry | `metrics.go:323-325` `recordTerminalSettlement(reason)`; closed reason list pinned by `terminal_settlement_test.go:48-67` (`requireOneTerminalReason`) |
| AGENT source retention | spec `agentic-terminal-events` "Delivery attempts SHALL be unlimited only within AGENT retention": 24h MaxAge, 256MiB, DiscardOld |
| AGENT_LOOPS retention | `processor/agentic-loop/component.go:759-767`: bucket created with `History: 10`, `TTL: 24 * time.Hour` — a key is evicted 24h after its last write |
| Restart proof (own route only) | `terminal_settlement_integration_test.go:23-89` empties the tracker and recovers the route from `AGENT_LOOPS/<loopID>` — the terminal loop's OWN record; no ancestor is consulted |

### B. `publish_agent` — what it threads onto the spawned `TaskMessage` today

| Field / key | Evidence |
|---|---|
| `TaskID`, `Role`, `Model`, `Prompt`, `WorkflowSlug`, `WorkflowStep` | `processor/rule/actions.go:1504-1511` |
| `ParentLoopID` — set only when the trigger entity is a loop-execution entity | `actions.go:1524-1526` (`agentic.LoopIDFromExecutionEntityID`) |
| `RunID` per `run_scope` (`new` mints a run rooted at the FIRING loop; `inherit`/`""` copies the firing entity's `agent.loop.run` triple; `none` suppresses) | `actions.go:1528-1615`; mint after validation `:1681-1716` via `agentrun.Mint`; `agent.loop.run` + `agent.run.entity-id` stamped on the FIRING entity `:1706-1711` |
| `Tools` allowlist | `:1617-1628` |
| Author `properties` → `Metadata` (gh#354), `agent.*` keys reserved | `:1247-1290` (`isReservedTaskMetadataKey`, `stampAuthorMetadata`) |
| `agent.decide.action_allowlist` | `:1647-1656` |
| `ResponseFormat`, `ToolChoice` | `:1319-1332` |
| `agent.related_loops` | `:1660-1664` (`stampRelatedLoops`) |
| `agent.exec.filesystem_policy`, `agent.exec.scratch_paths` | `:1292-1317` |
| `MaxIterations` | `:1334-1363` |
| `rule.task.spawned` triple on the firing entity after publish | `:1754-1783` |
| **Channel fields** (`ChannelType`, `ChannelID`, `UserID`) | **Never set.** `grep -n "ChannelType\|ChannelID\|UserID" processor/rule/actions.go` → 0 hits in the publish path (only `errs`/log fields elsewhere). The reserved-subject guard `:1492-1496` rejects `user.response.>` targets |
| `agentic.TryChainExecutionEntityID` | `agentic/entity_ids.go:127`; consumed by `actions.go:1711`, `agentrun.go:225,311,343`, `handlers.go:562-575` (`resolveRunEntityID`), `loop_execution_entity.go:127` |

### C. ADR-053 agent-run substrate — what it persists and where

| Item | Evidence |
|---|---|
| Participant fields: `EntityIDField` (full 6-part, from KV key), `PhaseField`, `ParentRunEntityID` — **nothing else** | `agentic/agentrun/agentrun.go:114-124` |
| Phase graph `dispatched → executing ⇄ awaiting_approval → completed/failed/cancelled` | `agentrun.go:50-57` |
| Storage: ENTITY_STATES via `pkg/lifecycle` Manager (graph plane; History 1) | ADR-053 "Storage" §; `agentrun.go:176-207` |
| Mint at `run_scope=new`, run-id == the firing (root) loop id | `agentrun.go:224-250`; `actions.go:1578` (`task.RunID = firingLoopID`) |
| Terminal authority (D3): framework CREATES + OBSERVES; the PRODUCT emits the terminal run decision via `lifecycle_transition`; the framework never infers run completion from loop events | ADR-053 D3 (`docs/adr/053-agent-run-substrate.md:136-147`); observation-only subscriber `agentrun.go:409-567` |
| Loop-side run anchor persisted in AGENT_LOOPS | `LoopEntity.RunID` `agentic/state.go:57-59`; set from `TaskMessage.RunID` at `handlers.go:476-482`; emitted on all terminal events `handlers.go:1991-1992`, `:2577-2578` |
| Loop-side ancestry persisted in AGENT_LOOPS | `LoopEntity.ParentLoopID` `state.go:56`; set at `handlers.go:467-473`; emitted as `parent_loop` on completion `handlers.go:1982` |
| Same two facts on the graph plane | `agent.loop.parent`, `agent.loop.run`, `agent.run.entity-id` triples at spawn: `agentic/loop_execution_entity.go:118-129`; read back by `agentrun/nats_reader.go:35-45` (exact authority read) |
| Ancestry walker (graph plane) | `agentrun.ResolveRun` `agentrun.go:297-370`, bound `maxAncestryHops = 32` (`:282`) |
| Per-run record that could carry an origin route today | **None.** `grep -rn "Channel\|Origin\|origin" agentic/agentrun/` → 0 hits |
| Origin route ALREADY durable for the run root | The HTTP root loop's `AGENT_LOOPS/<rootLoopID>` record carries `ChannelType/ChannelID/UserID` (`state.go:81-84`, written by `SetUserContext` `state.go:913-926` from `handlers.go:494-501`, persisted by `persistLoopState` `component.go:1968`) |

### D. The `decide` terminal tool and the decision vocabulary

| Item | Evidence |
|---|---|
| Tool is vocabulary-agnostic by design; the persona prompt enumerates actions; the description MUST NOT pre-load names | `processor/agentic-tools/decide.go:162-181` |
| Emits `coordinator.decision.next-action` + `coordinator.decision.reason` (+ optional subtopics, SAP audit) on the loop entity, atomically | `decide.go:336-413`; predicates `vocabulary/agentic/predicates.go:680,689` |
| Returns `ToolResult{Content: JSON{action,reason,…}, StopLoop: true, Metadata{action, reason, loop_entity_id, …}}` | `decide.go:418-447` |
| Loop turns a `StopLoop` result into completion with `Result = toolResult.Content` | `handlers.go:2164-2170` → `handleCompleteResponse` `:1959-2059`; the loop KNOWS the terminal tool's name via `GetToolName(callID)` (`handlers.go:2241`, `:2302`) |
| Per-spawn `action_allowlist` + SAP coercion | `decide.go:246-317`, `:488-532` |
| Deployment-level restriction (`restricted_decide_actions`) — the one existing seam where the framework learns product action names; cites `ask_user` as the example | `processor/agentic-tools/config.go:22-31`; `decide.go:99-130`, `:270-297` |
| Framework-owned action names that exist today | exactly one: `needs_clarification`, synthesized on terminal-tool-less completion by graphWriter AFTER the completion event is built (`processor/agentic-loop/graph_writer.go:160-230`, `:182`; trigger `handlers.go:2011-2040`) — a graph triple, never a tool result |
| `respond_direct` / `ask_user` / `research` / `autoresearch` in framework code | `respond_direct`: 0 non-test Go hits; 3 test fixtures (`processor/agentic-tools/decide_test.go:377,434,490`); in docs only `docs/concepts/25-phased-agentic-chains.md:59` as an example category. `ask_user`: comments only (`agentic-tools/config.go:26`, `decide.go:116,537`; `agentic/user_types.go` reply doc). `research`/`autoresearch`: only the `research` message domain and role strings |
| The `"decide"` tool name is spelled in three places | `agentictools.DecideToolName` (`decide.go:71`), literal `"decide"` at `handlers.go:1921` and `:1935` |
| ADR-026 enumerates `fan_out`, `synthesize`, `retry`, `done` for the deep-research coordinator; ADR-045 names none of the issue's actions | `docs/adr/026-…:34,93`; `docs/adr/045-…` (grep: no `respond_direct`) |
| In-repo reference chains use `fan_out`/`synthesize` and end in a rule-spawned synthesizer with no route | `configs/rules/deep-research/03,04,07-*.json`; `test/e2e/scenarios/deep-research/scenario.go:285-335` |

### E. `wakeup_mode` / `chain_terminal_*`

`grep -rn 'wakeup_mode\|chain_terminal\|WakeupMode' --include='*.go' --include='*.md' --include='*.json' .` → **0 hits**
outside other agents' `.claude/worktrees/` copies. The key in the issue's event (`"metadata":{"wakeup_mode":
"chain_terminal_autoresearch"}`) reaches `LoopCompletedEvent.Metadata` through the gh#354 author-`properties` path
(`actions.go:1270-1290` → `handlers.go:503-509` `SetMetadata` → `handlers.go:1990`). It is **product-authored,
free-form, and invisible to the framework**. It is not, and cannot be, a framework terminal marker.

### F. Tests around terminal settlement, rule-spawned chains, and restart

| Area | Existing tests |
|---|---|
| Settlement unit | `processor/agentic-dispatch/terminal_settlement_test.go:84` (stable success, optional UserID), `:117` (cancel on completion lane), `:133` (failure), `:161` (field-wise reconcile), `:188` (disposition classes), `:231` (exactly one fixed reason) |
| Settlement integration (real NATS) | `terminal_settlement_integration_test.go:23` (restart from own AGENT_LOOPS record + dedup + unlimited attempts), `:91` (eviction), `:122` (malformed persisted), `:147` (invalid terminal Termed), `:175` (retries then ACK after PubAck), `:264` (shutdown delayed NAK) |
| Dispatch handler | `agent_complete_handler_test.go:99,139,166`; `lifecycle_integration_test.go:82` |
| e2e single-loop terminal delivery | `test/e2e/scenarios/agentic/scenario.go:525-580` reads `user.response.e2e.<taskID>` and checks `terminal-user-response:<source id>`; the terminal is a plain model text result (no `decide`) |
| `publish_agent` | `processor/rule/actions_test.go:884-2201` (43 tests: allowlist, properties, related loops, for_each, parent loop id `:2075`, non-loop trigger `:2107`); `config_validation_test.go:251-310` (`run_scope`) |
| Agent-run | `agentic/agentrun/agentrun_test.go` (mint idempotence, resolve typed/walk, subscriber demux), `agentrun_integration_test.go` (projection round trip, stream presence) |
| Spawn identity triples | `processor/agentic-loop/spawn_identity_test.go:95` (parent), `:298-361` (run id) |
| **Absent** | No test anywhere exercises a terminal whose route must come from an ancestor; no test distinguishes a handoff decision from an answer; no test covers `ask_user`; no e2e covers a rule-spawned chain's user-facing terminal (`task e2e:agentic` and `deep-research` observe loops and triples, not a chain's `user.response`) |

### G. Every current spelling of "which loop's decision is user-facing" (F2)

1. **Dispatch:** "a loop with a complete route" (`terminal_settlement.go:192`). The only runtime home — and it
   answers a different question (who owns a route), which is why the root's handoff is delivered.
2. **The decide tool:** none; explicitly vocabulary-agnostic (`decide.go:162-181`).
3. **Rule packs (product):** the persona prompt + `action_allowlist` + downstream rules matching
   `coordinator.decision.next-action`; `wakeup_mode` metadata (product-only, §E).
4. **ADR-053 D3:** the product transitions the RUN to `completed` via `lifecycle_transition`; the framework observes.
   Not consumed by dispatch; not correlated to a loop result.
5. **Docs:** `docs/concepts/25-phased-agentic-chains.md:59` ("the coordinator knows only category names like
   `research` / `respond_direct`") — prose, not code.
6. **#1090's proposal:** parse `Result` JSON for `action ∈ {respond_direct, ask_user}` inside dispatch — a
   proposed fourth interpreter of the decide payload (`decideArgs` is unexported in agentic-tools, `decide.go:216-221`).

More than one home is the defect: today there is zero framework home, and two product-side conventions.

### H. Claimed-gap measurements

| Claim | Measurement | Verdict |
|---|---|---|
| "rule-spawned loop has no channel metadata" | `actions.go:1504-1511`; `handlers.go:494` guard | True — and incomplete: the loop DOES persist `ParentLoopID` (`handlers.go:467`) and `RunID` (`:476`), which reach the origin without any new field |
| "`route_less_settled` when all three empty" | `terminal_settlement.go:192-196` | True |
| "The final `agent.complete` contains the result but no route" | `handlers.go:1973-1993` copies `entity.ChannelType…` (empty) | True |
| "The run substrate is the natural carrier of the origin route" (triage) | `agentrun.go:114-124` carries phase + parent only; the origin is ALREADY durable on the run root's AGENT_LOOPS record | Carrier claim does not survive: adding it to the run entity would be a second home for a bucket fact on the graph plane |
| "published exactly once" (issue AC) | spec `agentic-terminal-events` forbids claiming exactly-once; dedup = `Nats-Msg-Id` within the USER `duplicates` window clamped to MaxAge (`client.go:955-963`; `config/stream_drift.go:276-283`) | Wording does not survive; deliverable is one stable response identity per terminal |
| "`respond_direct`/`ask_user` decision" is a framework fact | §D: 0 non-test code hits (3 fixtures) | Does not survive; it must become one (owner ruling) or be declared |
| Four structural walk-end cases for any ancestry walk | (1) routed root (HTTP, or bus with channel fields) — resolvable; (2) route-less root: a bus `agent.task` submission without channel fields (`test/e2e/scenarios/research-graph/scenario.go:370-390`) — nobody to deliver to; (3) ancestor record absent — key expired (24h TTL) or its best-effort `persistLoopState` Put never succeeded (`agentic-loop/component.go:1985-1987` logs and continues); (4) severed hop: a spawn fired from a non-loop entity has no `ParentLoopID` and inherits a `RunID` only if that entity carries `agent.loop.run` (`actions.go:1519-1526`, `:1604-1614`). All 11 in-repo `publish_agent` rules condition on `agent.loop.role` (loop-entity triggers); the one message-path spawn (`configs/rules/agentic-workflow/architect-editor.json`, `$message.role`) is wired into no shipped flow | Cases 2 and 4 are indistinguishable from the bucket; each case needs a named disposition (R4) |

## I.2 Same-class collision table

Semantic class under design: **(F1) the route a workflow answer is delivered to; (F2) the classification of a loop
terminal as the user-facing answer.**

| Dimension | F1 origin route | F2 user-facing classification |
|---|---|---|
| Owners | agentic-dispatch (reconcile + publish, `terminal_settlement.go:55-80,136-153`); agentic-loop (persists route on the loop record `state.go:81-84`); rule engine (does NOT thread it, §B) | none in the framework; product persona/rule pack; #1090 proposes dispatch |
| Catalogs | `TaskMessage.ChannelType/ChannelID/UserID` (`user_types.go:275-278`), `LoopEntity` (`state.go:81-84`), `LoopCompletedEvent`/`LoopFailedEvent` (`events.go:76-78,143-145`; `LoopCancelledEvent` lacks them), `LoopInfo` (`loop_tracker.go:26-28`), `Loop` wire (`loop_wire.go:21-22`), schema `schemas/agentic-loop.v1.json`, `agentic-dispatch.v1.json` | `coordinator.decision.next-action` predicate (`predicates.go:680`), `ToolResult.Metadata["action"]` (`decide.go:427-433`), `Result` JSON (`decide.go:418-419`), `agent.decide.action_allowlist` (`tools.go:265`), `restricted_decide_actions` (`agentic-tools/config.go:31`), synthetic `needs_clarification` (`graph_writer.go:182`) |
| Status | settlement reason label (`metrics.go:323`); `/loops` and `/activity` SSE expose route fields (`loop_wire.go:42-52`, `http_activity.go`) | none |
| Lifecycle | route written once at loop creation; AGENT_LOOPS key TTL 24h after last write (`component.go:761-766`); tracker entries process-local | decision written once at terminal; graph triple History 1 |
| Ownership | one route per loop; a rule-spawned loop legitimately has none | one decision per coordinator loop; `decide` "call exactly once" (`decide.go:186`) |
| Readers | dispatch settlement; `/loops`; SSE; message-logger (diagnostic) | rules (`$entity.triple.coordinator.decision.next-action`), `read_loop_result`, agentrun handlers (no decision field), OTel |
| Writers | `http.go:139-157` (HTTP defaults `channel_type=http`, `channel_id=http-<ns>`), bus `user.message`, `handlers.go:494`; rule engine: never | decide tool (`decide.go:407`), synthetic writer (`graph_writer.go:160`) |
| Recovery | `AGENT_LOOPS` re-read at settlement (`terminal_settlement.go:82-108`); transient → NAK; tracker not required (`integration_test.go:23-58`) | triples on ENTITY_STATES; `COMPLETE_<loopID>` record in AGENT_LOOPS (`component.go:1889-1960`) |
| Collision to consolidate | ancestry is spelled twice (AGENT_LOOPS `ParentLoopID`/`RunID` vs graph `agent.loop.parent`/`agent.loop.run`) with two walkers possible (`agentrun.ResolveRun` over the graph; none over AGENT_LOOPS yet) | the `"decide"` tool name literal ×3 (§D); `decideArgs` unexported; #1090 would add a second parser |

## I.3 Adopter seam inventory

The adopter: a product developer who submits a root task over HTTP and writes `publish_agent` rule chains. They have
never opened `terminal_settlement.go`.

1. **What must they know today?** (a) Only a loop that owns a route gets its result delivered
   (`terminal_settlement.go:192`); (b) a `publish_agent` spawn owns no route (`actions.go:1504-1511`) and there is no
   authoring surface to give it one — `properties` cannot set `channel_*` because those are struct fields, not
   metadata; (c) the framework does not know which of their `decide` actions is an answer, so a routed loop's
   decision JSON is delivered verbatim regardless of meaning. Three debts; two more than the contract allows — a
   design finding, not a documentation task.
2. **What happens if they do nothing?** Exactly the issue: `status: Task submitted`, `result: {"action":
   "autoresearch",…}`, and the real answer disappears into `route_less_settled`. Silent loss of the work product. A
   downstream test that asserts "some typed response arrived" passes (the issue reports this happened).
3. **Where do they find out?** Metric label `router_terminal_settlement_total{reason="route_less_settled"}` and a
   Debug-level tracker log; the spec literally calls this state "intentionally route-less". Rank: metric/log
   line → a finding for a correctness fact.
4. **What SHOULD they have to know?** Nothing about routing. The route is a fact the framework already holds
   durably (the run root's AGENT_LOOPS record) and the ancestry to reach it is a fact the framework already
   persists (`ParentLoopID`, `RunID`). The one fact the framework does NOT hold is which decide actions are answers
   — because the tool was designed vocabulary-agnostic. So the residual adopter knowledge is at most **the
   names of the reply actions** (two words), and the design question is whether those names are a framework
   contract (adopter learns two reserved names once) or a per-deployment declaration (adopter learns a config key
   and the names).

**Prefer observation to prediction.** Any shape that asks the rule author to mark "this spawn is the terminal"
(a `terminal: true` action field, `wakeup_mode` conventions, a `lifecycle_transition` to run `completed` wired per
chain end) makes the author predict the future shape of their own chain, and fails silently when the chain grows a
step. The framework can instead OBSERVE, at the moment a loop terminates, (i) that its terminal tool was `decide`
(`handlers.go:2241`), (ii) which action it chose, and (iii) the route of its nearest routed ancestor. Nothing is
predicted.

## I.4 Searches that closed empty

- `grep -rn 'wakeup_mode\|chain_terminal' --include='*.go' --include='*.md' --include='*.json' .` (excl. worktrees) → 0.
- `grep -rn 'respond_direct' --include='*.go' .` → 3, all fixtures in `processor/agentic-tools/decide_test.go:377,434,490`; 0 non-test hits. `grep -rn '"ask_user"' --include='*.go' . | grep -v _test | grep -v '//'` → 0 (comments only).
- `grep -rn "Channel\|Origin\|origin" agentic/agentrun/` → 0.
- `grep -n "ChannelType\|ChannelID\|UserID" processor/rule/actions.go` → 0 in the publish path.
- `grep -n "KVReadPort\|KVWatchPort\|AGENT_LOOPS" processor/agentic-dispatch/config.go` → 0 (dispatch reads the bucket without a declared port; the sibling declaration exists at `processor/agentic-tools/config.go:134`).
- `ls configs/personas/` → `fragments` only (no in-repo coordinator persona naming reply actions).
- `grep -rn "user.response\|decide" test/e2e/scenarios/agentic/*.go` → the single-loop terminal check only; no chain.
- `ls openspec/changes/` → `archive` only (no active change touches this surface).
- Open issues: #1090 (rendering) is the only adjacent open issue found by the briefing and the triage comment.

## I.5 Open evidence questions (not answerable from this repository)

1. Do semteams' chain packs fire every hop from a loop-execution entity (so `ParentLoopID` is continuous,
   `actions.go:1524`) and/or mint a run with `run_scope: "new"` on the first spawn (so `RunID` == HTTP root)? The
   issue's event shows `parent_loop` on the final loop; intermediate hops are UNVERIFIED.
2. Does any product front-door coordinator end with a decide action outside `{respond_direct, ask_user}` and expect
   that decision JSON delivered as `result` (the one behaviour this design changes)? UNVERIFIED (sister repos are
   hands-off).
3. Is the chain's total wall time under AGENT_LOOPS' 24h key TTL from the root's last write? Otherwise the origin
   record is gone before the terminal (`component.go:766`).

## I.6 Inventory review divergences corrected in this revision

1. §I.1-D / §I.4: `respond_direct` has 3 test-fixture hits (`decide_test.go:377,434,490`); revision 1 said 0. Now "0 non-test hits; 3 fixtures".
2. P7 / owner item 10: the missing KV read port was scoped to dispatch as hygiene; agentic-tools already declares `agent_loops` (`config.go:134`) and every sibling observes `LoopsBucket` from configuration. Dispatch's constant is a predicted bucket name; it is now in scope as R8.
3. §I.1-A dedup: the window is the USER `duplicates` declaration clamped to MaxAge (`config/stream_drift.go:276-283`), not a bare "2m default".
4. R4: an absent ancestor key can also mean a best-effort `persistLoopState` Put that never succeeded (`component.go:1985-1987`); the causal claim and the Warn text are corrected; the disposition is unchanged.
5. Line drift: `graph_writer.go:182` → `:182`; run-triple stamp `actions.go:1706-1711` → `:1706-1711`.
6. The undispositioned walk-end case (route-less root) and the fourth structural case (severed hop) are named in §I.1-H and ruled in R4 with a scenario and a test.
7. Owner facts added: two-plane walker asymmetry (§II.9-11), completion plumbing is new (P4), synthesized decisions never populate `Decision` (§II.9-2), `ask_user` on the cancelled lane (§II.9-4).

## I.7 Owner ruling and binding corrections folded in revision 3

Ruling (2026-08-26): items 1–7 and 9–10 accepted as recommended; 8 accepted CONDITIONALLY (C2); 11 — the AGENT_LOOPS
plane accepted, the r2 traversal NOT (C1). Confirmed unchanged: component-observed terminal selection; no new durable
authority; no graph routing metadata; no new communication path; reserved `respond_direct`/`ask_user` (ADR-101
Proposed → to be Accepted); synthesized `needs_clarification` never populates `Decision`; routed handoff publishes
nothing; any in-run `ask_user` is user-facing, cancelled lane unchanged; internal-phase failure silent; #1090's
reason rendering folded; exact name match; declared `agent_loops` read port (R8) in scope.

- **C1 — resolver order.** r2 chose `ParentLoopID` first and settled `origin_unresolvable` on an absent parent key
  even when the terminal carried a durable `RunID` naming the root. R4′ is typed-first on `RunID` (mirroring
  `agentrun.ResolveRun`, `agentrun.go:284-296`), parent walk for unthreaded chains, and a missing parent lookup
  falls back to `RunID` before anything settles.
- **C2 — item 8 conditional.** `origin_unresolvable` is recorded only after the parent chain AND every encountered
  run anchor are exhausted; it stays distinct from `route_less_settled`; the exhaustion order is in the requirement
  text and in the log reason.
- **C3 — decision stamping guard.** The loop resolves the terminal tool through its existing name-fallback chain
  (`GetToolName(callID)`, then `toolResult.Name`; `handlers.go:2241-2245`, synth path `:1370-1378`; agentic-tools
  stamps `Name` before publishing, `agentic-tools/component.go:680`, `:710-711`), so a restart / cache loss does not
  turn a decide terminal into a no-decision terminal.
- **C4 — decision validation guard.** A present `Decision` with an empty `Action` or `Reason` fails
  `LoopCompletedEvent.Validate()` and is permanently rejected by the fail-closed normalizer; it is never silently a
  handoff. Unknown non-empty actions remain valid handoffs.

**— end of inventory checkpoint (`INVENTORY PASS WITH DIVERGENCES`, corrected; owner ruling recorded above) —**

# Part II — Design (contingent on `INVENTORY PASS`)

## II.0 Decision skills applied

- **orchestration-check** — triggered (rule/component boundary: who selects the terminal?). Outcome: the
  COMPONENT observes. agentic-loop observes that a terminal tool was `decide` and carries the typed decision;
  agentic-dispatch classifies it and resolves the origin. Rules keep triggering transitions and gain no new action or
  field; the Lifecycle harness is not involved (the run's phase is product-declared per ADR-053 D3 and stays so).
  Rule 3 (components are workflow-agnostic) holds: dispatch reads generic loop facts (`ParentLoopID`, `RunID`,
  route), never a workflow id.
- **entity-or-bucket** — triggered by "origin correlation across restart is durable state". Outcome: **no new
  durable state**. The origin route already lives in `AGENT_LOOPS` on the run root's loop record (ground 4/5 —
  the loop's operational record, ADR-049's named exception) and the ancestry already lives beside it. Putting the
  route on the `AgentRun` graph entity would create a second home for the same fact on the graph plane and would
  force the rule engine to read AGENT_LOOPS at mint time. Rejected.
- **kv-or-stream** — triggered (new communication path?). Outcome: none. Settlement stays on the existing
  `agent.complete`/failure JetStream consumers (queue, side effects, redelivery); origin resolution is a bounded
  sequence of KV `Get`s on the consumer's own path (Test 1: on restart the consumer resumes unacked work and re-reads
  current facts — exactly the existing split).
- **new-payload** — triggered by an additive field on a registered payload (`LoopCompletedEvent.Decision`). Outcome:
  no new registration; `task schema:generate` regenerates `schemas/agentic-loop.v1.json`; a production-decoder
  round-trip test is required.
- **query-pattern** — not triggered: dispatch already reads `AGENT_LOOPS` directly (`terminal_settlement.go:82-108`)
  and gains no graph read.

## II.1 Options considered

### Option 0 — Do nothing / product-side workaround

Semteams restores a flat `user.response.>` rule writer. Boot rejects it (`user-response-subject-ownership`:
"unmigrated rule pack starts → boot fails"; `actions.go:1492-1496`). A product-shell adapter would be a bridge the
issue forbids. Cost: the framework's typed response bus cannot deliver a workflow's answer. Rejected.

### Option 1 (briefing) — The run carries the origin route; only the run's terminal decision publishes

Add `OriginChannelType/ID/UserID` lifecycle predicates to `AgentRun`, set at `Mint`; dispatch resolves the run via
`RunEntityID` on the wire and reads it from the lifecycle Manager; "the run's terminal decision" = the loop whose
completion coincides with the run's `completed` transition (ADR-053 D3, product-declared).

- Durable state: NEW triples on the run entity in ENTITY_STATES (`entity-or-bucket`: a second home for a bucket
  fact; History 1 is fine, but the fact is routing metadata, not graph knowledge).
- Restart safety: graph plane, fine. Exactly-once: unchanged identity.
- "Terminal" decided by: the product's `lifecycle_transition` rule — the rule author predicts the terminal
  (the shape the seam inventory rejects) AND the transition carries no loop id, so dispatch cannot know WHICH
  loop's result to publish without a further correlation field.
- Mint must learn the root's route: the rule engine would have to read AGENT_LOOPS (new reader, new dependency).
- Dispatch would need a lifecycle Manager / graph read (new dependency; readiness coupling to the graph plane).
- Root-handoff suppression: unsolved — the root's own `AGENT_LOOPS` record and completion event never carry a
  `RunID` (mint stamps graph triples on the firing entity only, `actions.go:1706-1711`; `handlers.go:1991`), and the
  mint's ordering against the root's completion event is unconstrained (P10); dispatch cannot observe "this root is
  in a run" at the root's settlement, so a run-carried route still delivers the root's handoff.
- Consumer at birth for the new predicates: dispatch only. Rejected in this form.

### Option 1' (recommended) — Observe the origin from the run root's existing record; classify by the typed decision

No new durable state; no rule change. Two observable facts drive settlement:

- **Origin (F1):** when a terminal loop's own reconciled route is empty, dispatch walks `AGENT_LOOPS` records
  `ParentLoopID` → (else) `RunID` until it finds a record with a complete route (the HTTP root), bounded at 32 hops
  with a visited set. The run root IS the route owner by construction (`actions.go:1578`, `agentrun.Mint`).
- **Terminal (F2):** the loop carries a typed `Decision{Action, Reason}` on `LoopCompletedEvent` when — and only
  when — its terminal tool was `decide` (`handlers.go:2241` `GetToolName`). Dispatch classifies the action against a
  framework reply vocabulary (`respond_direct` → `result`, `ask_user` → `prompt`); any other decision is a
  **handoff** and publishes nothing; a terminal with no decision keeps today's behaviour (own route or route-less).

Costs: one additive wire field; one bounded KV walk on a path that already reads the bucket; one reserved
two-name vocabulary (owner ruling, §II.9-1); a behaviour change for routed loops that end in a non-reply decision
(§II.7). Residual: AGENT_LOOPS 24h TTL bounds origin resolution (§II.3 R6).

### Option 2 — The rule action marks the terminal step

`publish_agent` gains e.g. `deliver_to_origin: true` (or reads `wakeup_mode`), and the executor copies the trigger
loop's route… which it does not hold (the rule engine reads ENTITY_STATES, and channel fields are not triples —
`loop_execution_entity.go:91-150` stamps `agent.loop.user` but no channel). It would need to read AGENT_LOOPS or
carry an opaque "origin loop id" for dispatch to resolve — at which point dispatch is doing Option 1' anyway, plus
the author predicts the terminal. Silent failure when the chain gains a step. Rejected (seam inventory §I.3).

### Option 3 — Copy channel fields onto every spawn (rejected baseline)

`publish_agent` copies the trigger loop's route onto every child. Every internal phase (baseline, gather, reviewer)
then owns the route and is delivered as `result`; the root handoff is still delivered. This is strictly worse than
today for the user and is the shape the issue rejects. Kept only for contrast.

### Option 4 — Variant of 1': deployment-declared reply actions instead of reserved names

Same as 1' but the user-facing action set is a dispatch config list (`user_facing_decide_actions`, mirroring
`restricted_decide_actions`, `agentic-tools/config.go:31`). Adopter debt: a config key + the names; "do nothing"
= empty list = today's defect (no answer ever borrows the origin). Precedent exists (gh#239) but the seam
inventory prefers zero knobs; the names are the same two words either way, and #1090 needs them too. Presented as
the owner's alternative to reserved names (§II.9-1); the forced-omission discipline applies to whichever is chosen.

## II.2 Recommendation

**Option 1'.** Grounding sentence: *the origin route and the ancestry to reach it are already durable facts the
framework holds in the bucket dispatch already reads (`state.go:56-59,81-84`; `terminal_settlement.go:82-108`), and
whether a terminal is an answer is a fact the loop observes at its own completion (`handlers.go:2241`), so the only
thing missing is for the framework to consult what it already knows instead of asking the rule author to predict
it.*

Durable-state ruling (draft, for the owner): **no new durable state; no new bucket; no new graph predicate.**

## II.3 The contract (target state)

- **R1 Typed decision (C3, C4).** When a loop's terminal `StopLoop` tool result came from the `decide` tool,
  agentic-loop sets `LoopCompletedEvent.Decision = &CoordinatorDecision{Action, Reason}` from the tool result's
  metadata (`decide.go:427-433`); otherwise `Decision` is nil. The terminal tool is identified through the loop's
  EXISTING name-fallback chain, exactly as approval gating does: `GetToolName(callID)` first, then `toolResult.Name`
  (`handlers.go:2241-2245` — "when the LoopManager cache has been cleared (e.g., process restart)"; the synth path
  `:1370-1378` recovers the tracked name the same way). agentic-tools stamps `Name` on every result before publishing
  (`agentic-tools/component.go:680`, `:710-711`), so the fallback is populated on the wire; a tracked-name-only
  guard would misclassify every decide terminal settled after a restart. `Result` keeps its current content (the
  full decision JSON) so `read_loop_result` and rules are unchanged. A PRESENT `Decision` with an empty `Action` or
  an empty `Reason` FAILS `LoopCompletedEvent.Validate()` (`agentic/events.go:89-97`, which today checks only loop
  and task IDs); because the normalizer runs `Validate()` (`internal/agentterminal/terminal.go:114`), such a
  terminal is permanently rejected with `ReasonPayload` and Termed — never silently classified as a handoff. An
  unknown but non-empty action remains a valid handoff (publishes nothing). `agentterminal.Event` carries
  `Decision`.
- **R2 Reply vocabulary.** `agentic.DecideActionRespondDirect = "respond_direct"` and
  `agentic.DecideActionAskUser = "ask_user"` are framework-reserved decide actions with user-facing semantics;
  `agentic.IsUserFacingDecideAction(action)` is the ONE classifier. The decide tool description stays
  vocabulary-agnostic; `restricted_decide_actions` may still bar them (semteams "autonomous" bars `ask_user`).
  `agentic.DecideToolName` becomes the single home of the `"decide"` literal (consolidating `decide.go:71`,
  `handlers.go:1921,1935`).
- **R3 Selection.** At settlement of a succeeded terminal:
  - `Decision != nil && IsUserFacing` → publish: `respond_direct` as `type=result`, `ask_user` as `type=prompt`;
    `Content = Decision.Reason`; route = own reconciled route if complete, else the resolved origin (R4);
    `InReplyTo = event.LoopID` (the deciding loop, so a reply can re-enter the run via `in_reply_to` + `run_id`).
  - `Decision != nil && !IsUserFacing` (a handoff) → publish nothing to any route; reason `handoff_settled`; ACK.
  - `Decision == nil` → unchanged: own route → `result` with `Result` verbatim; no route → `route_less_settled`.
  - Failed and cancelled terminals: unchanged (own route only). Internal-phase failures do not reach the user
    (owner item §II.9-5).
- **R4′ Origin resolution (owner-corrected: C1, C2).** Only for a user-facing decision whose own reconciled route
  is empty. The resolver mirrors `agentrun.ResolveRun` (`agentrun.go:284-296`): typed-first, walk-fallback, and it
  never settles while an untried durable link remains.
  1. **Typed-first (`RunID`).** If the terminal record carries `RunID` ≠ its own ID, load `AGENT_LOOPS/<RunID>` — the
     run root by construction (`actions.go:1578`). Routed → origin. Present but route-less → continue at step 2
     FROM THE ROOT record (a routed loop may sit above a product-minted run). Absent → note "run anchor absent" and
     continue at step 2 from the terminal.
  2. **Parent walk (nearest routed ancestor — unthreaded chains, and above a route-less run root).** From the start
     record: a complete route → origin; else follow `ParentLoopID`. At every hop whose parent key is ABSENT, before
     anything settles, try the current record's `RunID` if it is non-empty, not self, and not yet tried (an
     intermediate record may carry a run anchor the terminal did not): routed → origin; present and route-less →
     continue the walk from it; absent or none → `origin_unresolvable`. A record with no `ParentLoopID`, no untried
     `RunID`, and no route is the walk end → `route_less_settled` (there was no origin: a route-less bus-submitted
     root, `test/e2e/scenarios/research-graph/scenario.go:370-390`, or a hop severed by a non-loop-entity trigger,
     `actions.go:1519-1526`, `:1604-1614` — indistinguishable from the bucket; a limit of the walk, not a product
     question).
  3. **Bounds.** 32 hops, visited set; a cycle or the bound → `origin_unresolvable`.
  Dispositions: transient KV error on any record → delayed NAK (`routing_read_transient`, unchanged); malformed JSON,
  ID mismatch, or partial route on any record → permanent (unchanged). `origin_unresolvable` is recorded ONLY after
  both the parent chain and every encountered run anchor are exhausted (C2), and its Warn names the exhaustion:
  `origin_unresolvable: parent chain ended at absent <loopID>; run anchor <RunID> absent | none` — the metric reason
  stays the bounded `origin_unresolvable`; absence means the key expired (24h TTL) or its best-effort
  `persistLoopState` Put never succeeded (`agentic-loop/component.go:1985-1987`), and redelivery cannot help because
  an ancestor's record is written before its child exists. `route_less_settled` is the walk-end answer "there was no
  origin" (expected for bus-submitted work; not an alert) and is never used when a link pointed at something
  unobservable; `origin_unresolvable` answers "the origin could not be observed" (a retention/persistence alert).
- **R5 Identity.** Unchanged: `ResponseID` = `Nats-Msg-Id` = `terminal-user-response:<source id>`; timestamp =
  validated terminal timestamp; a redelivery reuses the same route. The guarantee stays "at most one distinct
  response identity per terminal, deduplicated within the USER `duplicates` window as clamped to the USER MaxAge"
  — never "exactly once".
- **R6 Restart safety.** Origin resolution consults only `AGENT_LOOPS` (the tracker is never consulted for an
  ancestor), so an empty process tracker after restart resolves the same origin. Residual: a route-owning ancestor
  whose AGENT_LOOPS key expired (24h after its last write, `component.go:766`) yields `origin_unresolvable`; this
  is the same 24h horizon the AGENT source already has and is documented, not guaranteed against.
- **R7 `publish_agent`.** Unchanged. A guard test pins that a rule-spawned `TaskMessage` carries no channel fields
  (the rejected baseline stays rejected).
- **R8 Declared read port.** Dispatch declares `{Name: "agent_loops", Config: KVReadPort{Bucket: "AGENT_LOOPS"}}`
  (mirroring `agentic-tools/config.go:134`); `loadPersistedLoop` and the `/activity` SSE lane resolve the bucket
  from that port instead of the constant at `http_activity.go:20`, so a non-default bucket name is observed from
  configuration, never predicted. Config-additive; the default name is unchanged.

### Walk-end table (R4′)

| Case | What was observed | Reason | Disposition |
|---|---|---|---|
| Terminal carries `RunID`; the run root's record is routed (typed-first) | one read | `response_settled` | publish, ACK after PubAck |
| A routed record is reached on the parent walk (unthreaded chain, or above a route-less run root) | nearest routed ancestor | `response_settled` | publish, ACK after PubAck |
| Parent key absent; a not-yet-tried `RunID` on the current record resolves a routed root (C1) | fallback hit | `response_settled` | publish, ACK after PubAck |
| Parent key absent; `RunID` key absent, or no `RunID` on the path (C2 exhaustion) | parent chain AND run anchor exhausted | `origin_unresolvable` | no publish, Warn names both, ACK |
| Walk end: no `ParentLoopID`, no untried `RunID`, no route (route-less root; severed hop) | every link resolved; nothing to deliver to | `route_less_settled` | no publish, ACK |
| Cycle, or 32-hop bound | walk cannot complete | `origin_unresolvable` | no publish, ACK |
| Transient KV read on any record | not classified | `routing_read_transient` | delayed NAK |
| Malformed JSON, ID mismatch, or partial route on any record | permanent | `routing_malformed` / `routing_collision_or_malformed` | Term |

## II.4 Changes by file (implementation map)

| File | Change |
|---|---|
| `agentic/types.go` (or `agentic/tools.go`) | `type CoordinatorDecision struct{Action, Reason string}`; consts `DecideToolName`, `DecideActionRespondDirect`, `DecideActionAskUser`; `func IsUserFacingDecideAction(string) bool` |
| `agentic/events.go:59-86` | `Decision *CoordinatorDecision \`json:"decision,omitempty"\`` on `LoopCompletedEvent` |
| `processor/agentic-loop/handlers.go:2164-2170,1959-1993` | NEW plumbing: `handleCompleteResponse` receives only `toolResult.Content` today (`:2166`); thread the tool result into it; resolve the tool name with the existing chain `GetToolName(callID)` → `toolResult.Name` (`:2241-2245`); set `completion.Decision` when it equals `agentic.DecideToolName`; replace literals at `:1921,:1935` |
| `agentic/events.go:89-97` | `LoopCompletedEvent.Validate` rejects a present `Decision` with empty `Action` or `Reason` (C4) |
| `processor/agentic-tools/decide.go:71` | `DecideToolName = agentic.DecideToolName` |
| `internal/agentterminal/terminal.go` | `Event.Decision` copied from `LoopCompletedEvent` |
| `processor/agentic-dispatch/terminal_settlement.go` | selection (R3), `resolveOriginRoute` (R4) over `loadPersistedLoop`, reasons `handoff_settled` / `origin_unresolvable`, `prompt` projection for `ask_user` |
| `processor/agentic-dispatch/terminal_settlement_test.go:48-82` | extend the closed reason list |
| `processor/agentic-dispatch/config.go`, `http_activity.go:19-20`, `terminal_settlement.go:89` | declare the `agent_loops` KV read port; resolve the bucket from the port in both readers (R8) |
| `schemas/agentic-loop.v1.json` | regenerated |
| `processor/rule/actions_test.go` | guard test only (no production change) |
| Docs (technical writer) | `docs/operations/38-agent-terminal-settlement.md`, `processor/agentic-dispatch/README.md:54-64`, `docs/concepts/25-phased-agentic-chains.md` (reply vocabulary sentence), release note |

## II.5 Named tests per acceptance criterion

| Acceptance criterion | Test (package) |
|---|---|
| Root handoff (`research`/`autoresearch`) is not emitted as the final result | `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing` (agentic-dispatch, unit); `TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing` |
| Correlation survives a rule-spawned multi-loop workflow | `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry` (unit; unthreaded 3-deep `ParentLoopID` chain via `loadPersistedLoopFn`); `TestSettleAgentTerminalMissingParentFallsBackToRunID` (C1; subtests `parent_key_absent` — the ruled shape: terminal → absent parent key, `terminal.RunID` → observable routed root → delivered; `parent_link_empty`; `typed_lookup_precedes_parent_walk` — the load sequence is `[terminal, root]`, the parent key is never read) |
| Correlation survives process restart/recovery | `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` (agentic-dispatch, `-tags=integration`; empty tracker, chain records only in AGENT_LOOPS) |
| Published as typed `agentic.user_response/v1` to the originating channel, one identity | same integration test asserts `user.response.http.<origin>` holds exactly one message with `terminal-user-response:<source id>` after two deliveries; `TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery` (unit) |
| Internal phase completions never reach the user channel | `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess` (baseline/reviewer text terminal inside a run → nothing) |
| Direct response (single-loop front door) | `TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason` |
| Clarification | `TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin` |
| Loop carries the typed decision only for a decide terminal | `TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal`, `TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent` (C3: tracked name absent, `toolResult.Name == "decide"` → stamped), `TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal` (agentic-loop); `TestLoopCompletedEventDecisionRoundTrip` (agentic, production decoder) |
| A present decision with empty fields is rejected, not a handoff | `TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason` (C4; subtests `empty_action`, `empty_reason`, `unknown_nonempty_action_valid`); the dispatch Term disposition for `ReasonPayload` is already pinned by `TestSettleAgentTerminalDispositionClasses` |
| One classifier | `TestIsUserFacingDecideActionTable` (agentic) |
| Walk bounds and dispositions | `TestResolveOriginRouteBoundsHopsAndDetectsCycles`, `TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted` (C2; fixtures: absent parent + absent `RunID` key; absent parent + no `RunID` on the path), `TestResolveOriginRouteTransientReadDelaysNak`, `TestResolveOriginRouteMalformedAncestorIsPermanent` |
| Bounded telemetry | `TestSettleAgentTerminalRecordsExactlyOneFixedDisposition` extended with the two new reasons |
| Route-less root and severed chain settle route-less, never origin-unresolvable | `TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess` (terminal record with neither link and no route) |
| Bucket name observed from the declared port, not predicted | `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort` (`-tags=integration`; port bound to a non-default bucket) |
| `publish_agent` unchanged | `TestAction_PublishAgent_CarriesNoChannelFields` (rule) |
| No flat writer / alias / bridge | existing `user-response-subject-ownership` guards remain; `grep -rn "user.response" processor/rule configs/` recorded in conformance |
| e2e | `task e2e:agentic` and `task e2e:semantic`'s deep-research scenario stay green (front-door text terminal and rule-spawned synthesizer are R3's unchanged branches); a chain-terminal e2e is filed as a coverage gap, not claimed |

## II.6 Forced omissions (one per new selector / carrier / mapper)

| New thing | Omission | Test that must go RED |
|---|---|---|
| Carrier: `LoopCompletedEvent.Decision` populated by the loop | do not set `completion.Decision` | `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry` (terminal classified as no-decision → route-less) and `TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal` |
| Selector: `IsUserFacingDecideAction` | return `true` for every action | `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing` |
| Mapper: `resolveOriginRoute` | skip the walk (return empty) | `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` |
| Carrier: declared `agent_loops` port | resolve the bucket from the constant again | `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort` |
| Mapper: the `RunID` path of R4′ (typed-first lookup + retry at an absent parent) | delete the `RunID` path; keep the parent walk | `TestSettleAgentTerminalMissingParentFallsBackToRunID` and only that test (its three subtests) |
| Selector: `toolResult.Name` fallback in the decision guard (C3) | resolve the tool name from the tracked name only | `TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent` and only that test |
| Guard: `Validate` rejection of a present empty-field `Decision` (C4) | remove the check | `TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason` and only that test |

Each omission is applied to a committed GREEN tree, the RED output captured verbatim, and the file restored by
`cp` from a checksummed copy (`shasum -a 256` before and after).

## II.7 BREAKING assessment

- **Wire:** additive optional field (`decision`) on a registered payload; old readers ignore it; new readers
  tolerate absence. Not breaking. Schema regenerated in the same commit.
- **Behaviour:** (a) a route-less user-facing decision now publishes to its origin — strictly new delivery; (b) a
  **routed** loop whose terminal is a non-reply decision (today: `result` with the JSON) publishes nothing. (b) is the
  intended fix for the root handoff, and it is the one observable change an existing product could feel: a
  front-door coordinator ending in `decide(action="done")` and a client waiting for `result`. In-repo reference
  chains are unaffected (their coordinators are rule-spawned and route-less; the front door ends in text). Sister
  repos: UNVERIFIED (§I.5-2). Named in the release note; not a BREAKING tag (no removed surface, no incompatible
  schema, no fresh-state requirement, no migration).
- **Config:** the `agent_loops` read port is additive with the default bucket name; an operator running a non-default
  `loops_bucket` on agentic-loop must bind the same name on dispatch — that deployment is silently broken today.
- **What a product that worked around this must remove:** any flat `publish` to `user.response.>` (already rejected
  at boot), any product-shell bridge that re-reads `agent.complete.*` to forward a wake-up's result, and any
  `wakeup_mode`-driven delivery logic; `wakeup_mode` may stay as inert metadata.
- **E2E gate before merge:** `task e2e:agentic` (touches the terminal delivery path) — required although not tagged
  BREAKING, because unit + integration do not drive the full ingest → loop → dispatch → USER wire.

## II.8 Design premises with measurements

| # | Premise | Measurement |
|---|---|---|
| P1 | The HTTP root loop's route is persisted in AGENT_LOOPS | `http.go:139-157` defaults; `handlers.go:494-501`; `state.go:913-926`; proven by `terminal_settlement_integration_test.go:42-58` |
| P2 | Rule-spawned loops persist `ParentLoopID` when fired from a loop entity | `actions.go:1524-1526`; `handlers.go:467-473`; `TestAction_PublishAgent_ParentLoopIDFromLoopEntity` (`actions_test.go:2075`) |
| P3 | `RunID` == run root loop id and inherits down the chain | `actions.go:1578,1604-1614`; `agentrun.go:224-233`; `loop_execution_entity.go:125-129` |
| P4 | The primitive to know the terminal tool's name exists; the path into completion does not | `TrackToolName` at dispatch `:1546` and `GetToolName(toolResult.CallID)` `:2241,:2302` exist, but `handleCompleteResponse(&result, loopID, entity, toolResult.Content)` (`handlers.go:2166`) receives only the content, and the only knowledge of a decide terminal today is the post-hoc trajectory scan `hasDecideToolCall` (`:1919-1927`). Threading the tool result into completion is NEW plumbing (Slice A) |
| P5 | The decide tool returns the action and reason as typed metadata | `decide.go:427-433` |
| P6 | No framework home exists for "user-facing decision" | §I.1-D, §I.1-G (searches) |
| P7 | Dispatch predicts the AGENT_LOOPS bucket name with a constant while every sibling observes it from a declared port or configuration | `terminal_settlement.go:89`, `http_activity.go:20,200` vs `agentic-tools/config.go:134`, `executors/register.go:55-60`, `agentic-loop/config.go:54` — the predicted-name seam the seam inventory rules against; corrected by R8 |
| P8 | Dedup is window-bounded, not exactly-once; the window is the USER `duplicates` declaration clamped to MaxAge | `client.go:955-963`; `config/stream_drift.go:276-283`; spec "Delivery declaration SHALL remain bounded and honest" |
| P9 | AGENT_LOOPS keys expire 24h after last write, and a record may never have been written (best-effort Put) | `component.go:761-766`; `:1985-1987` |
| P10 | Run membership of the ROOT is not observable at the root's settlement | the decide triples land at `decide.go:407` before the tool result even returns to the loop, so the rule may fire and mint (`actions.go:1685`) before or after the completion is published (`handlers.go:2042-2054`) — the order is unconstrained; and the mint writes only graph triples on the firing entity (`actions.go:1706-1711`), so the root's own `AGENT_LOOPS` record and its completion event never carry a `RunID` (`handlers.go:1991` copies `entity.RunID`, which is set only from `TaskMessage.RunID` at `:476`) |

## II.9 Owner items (decisions only the owner can make)

Owner ruling 2026-08-26: **1–7 and 9–10 accepted as recommended; 8 conditional (C2); 11 corrected — see R4′.**

1. **Home of the reply vocabulary.** (a) Framework-reserved `respond_direct` + `ask_user` as a cross-repo contract
   (recommended; ADR draft staged as `docs/adr/101-…`), or (b) a dispatch config list defaulting to those two
   names (Option 4), or (c) a config list defaulting to empty. (a) and (b) make semteams work with no config; (c)
   keeps today's defect for a product that does nothing.
2. **Synthesized decisions and `needs_clarification`.** The synthesized decision is written by graphWriter AFTER
   completion (`graph_writer.go:160-230`, `:182`), never as a tool result, so under R1 it never populates
   `Decision`: a text-only ROUTED coordinator is still delivered as a raw `result` (unchanged from today) and a
   text-only route-less one stays route-less. Ruling: (a) keep that (recommended — the synthetic decision is a
   graph routing marker for rule packs, and making `needs_clarification` user-facing would publish a `prompt` for
   every text-only coordinator completion); or (b) have the loop also stamp a synthetic `Decision` on the event
   when it synthesizes (`handlers.go:2030-2040` computes the request before the event is published, so it is
   feasible), at which point the classification of `needs_clarification` decides whether those loops deliver.
3. **A routed loop's non-reply decision:** publish nothing (recommended) or a `status` response carrying the
   action name (a progress signal; the `/activity` SSE lane already exists for progress, `http.go:105`).
4. **Is any in-run `ask_user` user-facing**, including from a non-terminal loop? Recommended: yes — that is the
   pause/resume shape gh#256 built (`http.go:42-46`). Explicit under R3: the cancelled lane is unchanged.
   `LoopCancelledEvent` carries no route fields and no result (`agentic/events.go:183-205`), so no `Decision` rides
   it; an in-run loop cancelled while awaiting an `ask_user` reply publishes a cancellation `status` only if it owns
   a route, and a route-less cancel stays route-less — the origin is not told. A follow-up issue if the owner wants
   the origin told.
5. **Internal-phase failure:** stay silent to the user (recommended; ADR-053 D3 says the product declares run
   failure) or publish `error` to the origin.
6. **Fold #1090's rendering** (`Content = Decision.Reason`) into this change (recommended: R3 already needs a
   content choice and holds the typed reason) — #1090 keeps its docs and non-decision-shape criteria — or keep
   `Result` JSON here and leave #1090 whole.
7. **Normalisation:** compare reserved names exactly (recommended; the decide tool already canonicalises when an
   `action_allowlist` is present) or SAP-normalise (`decide.go:563-568` would move to `agentic`).
8. **`origin_unresolvable` disposition:** settle route-less and ACK — **ACCEPTED CONDITIONALLY**: only after every
   available ancestry route (parent chain AND `RunID`) is exhausted, distinct from `route_less_settled`, with the
   exhaustion order explicit in the requirement text and the log reason (C2, R4′).
9. **Milestone placement** (beta.162 tag-blocker vs beta.163 first item) — per the triage comment.
10. **Declared `agent_loops` read port (R8) is now IN this change** — promoted from hygiene by the inventory review:
    a predicted bucket name that the sibling component already observes (`agentic-tools/config.go:134`). The owner
    may pull it back out; the recommendation is to keep it, because origin resolution multiplies the reads that rest
    on the constant today.
11. **Two walkers of one ancestry on two planes** — **AGENT_LOOPS plane ACCEPTED; the r2 traversal NOT**: it was
    parent-first and settled on an absent parent while a durable `RunID` was in hand; corrected to typed-first with
    parent fallback in R4′ (C1). `agentrun.ResolveRun` already walks `agent.loop.parent` over
    ENTITY_STATES (no TTL, History 1, `maxAncestryHops = 32`, `agentrun.go:282-370`); R4 walks `ParentLoopID`/`RunID`
    over AGENT_LOOPS (24h key TTL). The graph carries no channel predicate — `loop_execution_entity.go:142-143`
    stamps `agent.loop.user` only — so a graph walk can reach the ROOT but can never serve its route; the route exists
    only on the root's AGENT_LOOPS record. Options: (i) the AGENT_LOOPS walker (recommended: one plane, no graph read
    or readiness coupling in dispatch, no org/platform identity needed; every hop inherits the 24h horizon);
    (ii) reuse `ResolveRun`'s graph walk to find the root, then one AGENT_LOOPS read of the root's record (only the
    root's key inherits the horizon; costs dispatch a graph exact-read dependency, org/platform identity, and
    `LoopTripleReader` wiring, and still ends route-less on a route-less root exactly like (i)); (iii) stamp a
    channel predicate on the loop entity at spawn so the graph walk can serve the route (rejected: routing metadata
    on the graph plane, entity-or-bucket). Neither (i) nor (ii) removes the horizon; (ii) narrows it to one key.

## II.10 PREMISE FAILED lines

1. Briefing: "`wakeup_mode` (`chain_terminal_*`) — who sets it" → **not the framework**; 0 hits; it is product
   `properties` metadata via the gh#354 path (`actions.go:1270-1290`) and cannot mark a terminal.
2. Issue AC "published **exactly once**" → the current spec forbids the claim; the deliverable is one stable
   response identity per terminal, deduplicated within the USER `duplicates` window clamped to MaxAge
   (`client.go:955-963`; `config/stream_drift.go:276-283`).
3. Triage comment: "the ADR-053 agent-run substrate is the natural carrier for origin correlation" → the
   `AgentRun` participant carries phase and parent only (`agentrun.go:114-124`); the origin is already carried by
   the run root's `AGENT_LOOPS` record, so the run entity would be a second home. The resolver, not the carrier,
   is what is missing.
4. Triage comment: "Not BREAKING (additive correlation metadata)" → under the recommendation no correlation
   metadata is added at all; the behaviour change that IS present (a routed handoff decision no longer emits
   `result`) is what the release note must name.
5. Briefing: "ADR-045/ADR-026 distinguish `respond_direct`/`ask_user`/`research`/`autoresearch`" → ADR-026 names
   `fan_out/synthesize/retry/done`; ADR-045 names none; the issue's names appear only in a concept doc example
   (`25-phased-agentic-chains.md:59`).
6. Issue: "the final wake-up's `respond_direct` … decision" as a framework-recognisable terminal → the framework
   recognises no such action today (§I.1-D); this is the ruling in §II.9-1, not a given.
7. Spec `agentic-terminal-events`, scenario "intentionally route-less loop" → labels the rule-spawned answer's state
   as intentional; the delta narrows it to terminals without a user-facing decision.
8. Issue AC "survives process restart/recovery" → the existing restart proof covers only a loop that owns its route
   (`integration_test.go:23-58`); no ancestry recovery is tested anywhere today.
9. Issue "Naively copying channel fields onto every spawned loop … would expose baseline, gather, reviewer" →
   confirmed by measurement (`terminal_settlement.go:192-209` publishes any routed success), and ALSO would not
   fix the root handoff.
10. Unstated by issue and triage: AGENT_LOOPS key TTL 24h (`component.go:766`) and best-effort persistence
    (`:1985-1987`) bound any origin resolution that
    reads it; a chain longer than 24h after the root's last write cannot be delivered by any design that observes
    the root record (Option 1 would move, not remove, the horizon: the run entity has no TTL but the root's route
    would still have to be read at mint time).
11. Revision 1 of this design: "`respond_direct`: 0 hits in `.go`" → 3 test fixtures exist; corrected (§I.6).
12. Revision 1 P7 called the undeclared AGENT_LOOPS port dispatch-local hygiene → the sibling declares it and every
    other reader observes the name; it is a predicted-name seam, now R8.
13. Revision 1 R4 "absence means eviction" → absence also covers a best-effort Put that never succeeded
    (`component.go:1985-1987`); disposition unchanged, causal claim corrected.
14. Revision 2 R4 chose `ParentLoopID` first and settled `origin_unresolvable` on an absent parent key while the
    terminal's durable `RunID` could still resolve the root — contradicting `agentrun.ResolveRun`'s typed-first order
    (`agentrun.go:284-296`) and this design's own P3; corrected by C1 (R4′).
15. Revision 2 R1 guarded decision stamping on the tracked tool name only; after a restart or cache loss that name is
    absent (`handlers.go:2241-2245` exists for exactly that case) and every decide terminal would have settled as a
    no-decision terminal; corrected by C3. A present `Decision` with empty fields would have classified as a handoff
    silently; corrected by C4.

## II.11 Draft ADR

Staged as `docs/adr/101-coordinator-reply-vocabulary-and-workflow-terminal-delivery.md` (status: Proposed, unsigned).
It records only the cross-repo contract (two reserved reply actions with user-facing semantics; terminal is a
decision property observed by the loop, never a rule-declared step) — mechanics live in the spec deltas.

## II.12 OpenSpec artifacts

Staged as `openspec/changes/workflow-terminal-delivery/` (`proposal.md`, `design.md`, `tasks.md`,
`conformance.md`, deltas on `agentic-terminal-events`, `agentic-loop`, `agentic-tools`). `rule-engine` and
`user-response-subject-ownership` receive no delta (no `publish_agent` change; the subject family is unchanged).
