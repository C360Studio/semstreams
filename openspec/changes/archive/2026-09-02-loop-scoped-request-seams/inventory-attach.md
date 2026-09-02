# Inventory: attaching to an existing loop — who may, what is checked, what survives restart

base: 24ab736e4a0325736a9a7f4b3fe6f0941c675c92

## Claimed gap

- `processor/agentic-dispatch/component.go:923` — `		loopID = msg.ReplyTo`
- `processor/agentic-dispatch/component.go:925` — `		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`
- `processor/agentic-dispatch/http.go:326` (mirror of the above on the HTTP sync path) — `		loopID = msg.ReplyTo` (line-approximate; see block at `http.go:319-327`, same shape as `component.go:922-925`)
- `processor/agentic-dispatch/component.go:1176` — `func (c *Component) hasPermission(userID, permission string) bool {`
- `processor/agentic-dispatch/component.go:1204` — `func (c *Component) canUserControlLoop(userID, loopID string) bool {`
- `processor/agentic-dispatch/commands.go:87` — `	if loopInfo == nil {` (in `handleCancelCommand`, after `c.loopTracker.Get(targetLoopID)` at `commands.go:86`)
- `processor/agentic-loop/state.go:151` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`

## Spellings of the fact

### Attach entry points (name an existing loop rather than creating one)

- `processor/agentic-dispatch/component.go:920-931` — `handleTaskSubmission` (bus path): `loopID` set from `msg.ReplyTo` (923) or `GetActiveLoop` (925), then validated by `refuseNonCanonicalLoopTokens` (931) — form only, no existence/ownership check on this path before `loopTracker.Track` is called unconditionally later in the function.
- `processor/agentic-dispatch/component.go:777` — `handleCommand`: `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`; explicit loop id comes from command args at `component.go:775` (`loopID = args[0]`).
- `processor/agentic-dispatch/http.go:237,301` — `processCommandSync` mirrors `handleCommand`'s loop-id resolution (args[0] or `GetActiveLoop`).
- `processor/agentic-dispatch/http.go:319-327` — `processTaskSubmissionSync` mirrors `handleTaskSubmission`'s loop-id resolution (`msg.ReplyTo` or `GetActiveLoop`), same `refuseNonCanonicalLoopTokens` form check, same unconditional `loopTracker.Track` afterward (`http.go:337-346`).
- `processor/agentic-dispatch/commands.go:53-96` — `handleCancelCommand`: explicit loop id via `args[0]` (58) or the caller-resolved `loopID` param; checks `canUserControlLoop` (73) then `loopTracker.Get(targetLoopID) == nil` (86-87, existence only, no restore).
- `processor/agentic-dispatch/commands.go:148-172` — `handleStatusCommand`: same loop-id resolution as cancel; existence check `loopTracker.Get(targetLoopID) == nil` at `commands.go:172`; **no `canUserControlLoop` call in this function** (confirmed by `git grep`, see Searches — the only caller of `canUserControlLoop` in the repo is `commands.go:73`).
- `processor/agentic-dispatch/http.go:602-636` — `handleGetLoop`: loop id from `r.PathValue("id")` (608); existence check only, `loopTracker.Get(loopID)` (617) — **no permission or ownership call in this handler.**
- `processor/agentic-dispatch/http.go:642-737` — `handleLoopSignal`: loop id from path (648); existence check only, `loopTracker.Get(loopID)` (654) — **no permission or ownership call.**
- `processor/agentic-dispatch/http.go:739-847` — `handleLoopApproval`: loop id from path (745); existence check `loopTracker.Get(loopID) == nil` (754), comment at `http.go:750-753` names the restart failure mode explicitly (see Restart evidence); then atomic `GetPendingApprovalCallID(loopID)` (788) gates on pending-approval state, not on caller identity — approver identity comes from `IdentityFromRequest(r, req.UserID)` (`http.go:799`), not checked against the loop's `UserID`.
- `processor/agentic-dispatch/component.go:1023-1027,1079-1083` — `handleAgentComplete`/`handleAgentFailed` both call `settleAgentTerminal` (see Consumers) — these attach to an existing loop by `event.LoopID` decoded off the stream payload, not from a caller-controlled field, so no user-identity check applies; routing is by tracker + persisted record, not by permission.
- `processor/agentic-dispatch/component.go:1030-1078` — `handleAgentCreated`: attaches by `created.LoopID`; existence branch at `component.go:1047` (`if existing := c.loopTracker.Get(created.LoopID); existing != nil`) — the one attach path in this file that IS conditional on prior existence (updates workflow/context-request fields in place rather than overwriting the whole record); else falls through to `Track` a new record (`component.go:1063-1072`).
- `processor/agentic-dispatch/component.go:1092-1123` — `handleAgentApprovalPending`: attaches by `pending.LoopID` from the stream event; calls `c.loopTracker.SetPendingApproval` (1117) which the doc comment (1113-1116) says "handles the unknown-loop race internally by buffering" — not an authorization check, a race-buffer.

### `LoopTracker` (`processor/agentic-dispatch/loop_tracker.go`)

Every declared method (`git grep -n 'func (t \*LoopTracker)'`, 20 hits — see Searches) and every caller found by `gopls references` / `git grep`:

- `loop_tracker.go:118` `NewLoopTracker()` — 0 non-test callers (only `NewLoopTrackerWithLogger` is used in production).
- `loop_tracker.go:128` `NewLoopTrackerWithLogger(logger)` — 1 caller: `component.go:182` (`loopTracker: NewLoopTrackerWithLogger(logger)`), inside the `Component` constructor, not `Start`.
- `loop_tracker.go:148` `Track(info *LoopInfo)` — callers: `component.go:1063` (`handleAgentCreated`, new external loop), `component.go` bus-path `handleTaskSubmission` (Track call after `refuseNonCanonicalLoopTokens`), `http.go:337` (`processTaskSubmissionSync`).
- `loop_tracker.go:188` `Get(loopID) *LoopInfo` — callers: `commands.go:86,171`, `component.go:1047,1211`, `http.go:617,654,754`.
- `loop_tracker.go:197` `getSnapshot(loopID) *LoopInfo` — 1 caller: `terminal_settlement.go` inside `settleAgentTerminal` (`tracker := c.loopTracker.getSnapshot(event.LoopID)`); doc comment at `loop_tracker.go:195-196` says a raw pointer would race concurrent create/approval updates.
- `loop_tracker.go:209` `GetActiveLoop(userID, channelID)` — callers: `component.go:777,925`, `http.go:237,301`.
- `loop_tracker.go:233` `UpdateState`, `:257` `UpdateIterations`, `:278` `UpdateCompletion`, `:286` `updateCompletionAt` — `updateCompletionAt` caller: `terminal_settlement.go` inside `settleAgentTerminal`.
- `loop_tracker.go:336` `SetPendingApproval` — caller: `component.go:1117` (`handleAgentApprovalPending`).
- `loop_tracker.go:397` `GetPendingApprovalCallID` — caller: `http.go:788` (`handleLoopApproval`).
- `loop_tracker.go:412` `ClearPendingApproval` — caller: `http.go:825` (`handleLoopApproval`, after a successful publish).
- `loop_tracker.go:456` `UpdateWorkflowContext`, `:483` `UpdateContextRequestID` — callers: `component.go:1049,1051` (`handleAgentCreated`, existing-loop branch).
- `loop_tracker.go:507` `Remove`, `:543` `GetUserLoops`, `:557` `GetAllLoops`, `:569` `Count` — not traced further (out of this surface's attach question; listed for completeness of the method census).

**No method loads, restores, rehydrates, or populates the tracker's maps from durable state (KV, JetStream, ObjectStore).** Negative-result searches: `git grep -ni 'restore|rehydrat|hydrate|LoadFromKV|kv.WatchAll|kv.GetAll' processor/agentic-dispatch/` (excluding `_test.go`) → 2 hits, both comments about NOT doing per-client `kv.WatchAll` for an unrelated activity-streaming feature (`http.go:898`, `http_activity.go:283`), not tracker population.

**Where the tracker's maps are constructed**: `loop_tracker.go:119-125` (`NewLoopTracker`) and `loop_tracker.go:129-136` (`NewLoopTrackerWithLogger`) — both `make(map[...])`, empty at construction. **Nothing reads AGENT_LOOPS (or any bucket) into them at `Start`**: `component.go:337-403` (`Start`) calls only `setupSubscriptions` (`component.go:386`) before marking `started = true`; no KV read appears in `Start`'s body (confirmed by reading the full function body — no `GetKeyValueBucket`/`kv.Get`/`kv.Watch` call present).

### Authorization sites

- `processor/agentic-dispatch/component.go:1176-1192` `hasPermission(userID, permission)` — switch on the permission string; delegates to `inList` against `c.config.Permissions.{View,SubmitTask,CancelAny,Approve}` lists or the `CancelOwn` bool; returns `false` for an unrecognized permission (`default: return false`, `component.go:1190-1191`). No test in this repo names `hasPermission` directly by that identifier in a `_test.go` file under `processor/agentic-dispatch` that I could confirm — see Searches (grep for `canUserControlLoop|hasPermission(` in `*_test.go` returned 0 hits).
- `processor/agentic-dispatch/component.go:1194-1202` `inList(userID, list)` — `"*"` wildcard or exact match.
- `processor/agentic-dispatch/component.go:1204-1216` `canUserControlLoop(userID, loopID)` — short-circuits `true` if `inList(userID, CancelAny)` (1206-1208, no tracker lookup at all for this branch); else `loopInfo := c.loopTracker.Get(loopID)` (1211), returns `false` if nil (1212-1213); else `loopInfo.UserID == userID && c.config.Permissions.CancelOwn` (1216). **Only caller**: `commands.go:73` (`handleCancelCommand`). Not called from `handleStatusCommand`, `handleGetLoop`, `handleLoopSignal`, or `handleLoopApproval`.
- `processor/agentic-dispatch/commands.go:86-97` — nil-check after `Get`: returns a "Loop %s not found" error response (`commands.go:90-95`) with no distinction between "never existed" and "existed but tracker lost it" (e.g., after restart — see Restart evidence).
- `(UserID, ChannelID)` auto-continue scoping branch: `loop_tracker.go:209-231` `GetActiveLoop` — prefers `channelLoops[channelID]`, falls back to `userLoops[userID]`, both filtered by `!isTerminalState(info.State)`. This scoping is bypassed entirely by an explicit `reply_to`/`loop_id` (the `#1227` claim, confirmed at `component.go:923` and `commands.go:58`/`http.go:326`).
- `processor/agentic-dispatch/http.go` approval path: caller identity resolved via `IdentityFromRequest(r, req.UserID)` (`http.go:799`) — this value is used only as the `approver` field on the published `ApprovalResponse`; it is not checked against `loop.UserID` or any permission list before `publishApprovalResponse` is called (`http.go:806`).

### `LoopManager.CreateLoopWithID` (`processor/agentic-loop/state.go:151-181`)

- Callers (`git grep`, non-test): `processor/agentic-loop/handlers.go:834` (`HandleTask`, when `task.LoopID != ""`) and `processor/agentic-loop/state.go:132` (`CreateLoop` calls it with a freshly generated id, so `CreateLoop` never hits the "already exists" case).
- Overwritten maps, unconditionally, no existence branch: `m.loops[loopID] = &entity` (`state.go:171`), `m.pendingTools[loopID] = make(map[string]bool)` (`state.go:172`), `m.contextManagers[loopID] = NewContextManager(...)` (`state.go:179`). The doc comment immediately above the function (`state.go:143-149`) states this in prose: "the map write below OVERWRITES an existing record and its context manager."
- **No branch is conditional on the loop already existing** inside `CreateLoopWithID` itself — the only guard present is the token-shape check `looptoken.Valid(loopID)` (`state.go:152`), which is a form check, not an existence check.
- How a legitimate continuation reaches it: `processor/agentic-loop/handlers.go:820-841` (`HandleTask`) — dedupes on `TaskID` via `HasActiveLoopForTask` (820, see below), and if that dedup does not fire, unconditionally calls `CreateLoopWithID(task.LoopID, ...)` whenever `task.LoopID != ""` (`handlers.go:834`), with **no check for whether `task.LoopID` already names a loop in `m.loops`** before the call.
- Whether the conversation context manager is replaced or reused on that path: replaced, every time. `handlers.go:872` (`cm := h.loopManager.GetContextManager(loopID)`) reads the manager `CreateLoopWithID` just constructed fresh at `state.go:179`; `handlers.go:873-877` adds the (re-)assembled system prompt and `handlers.go:882-885` adds only the new `task.Prompt` as a user message — there is no code path in `HandleTask` that reads or merges a prior context manager's message history before this point.

### Completion routing

- `processor/agentic-dispatch/component.go:1023-1027` `handleAgentComplete` and `component.go:1079-1083` `handleAgentFailed` both call `settleAgentTerminal` (`processor/agentic-dispatch/terminal_settlement.go:168`).
- `terminal_settlement.go:168-260` `settleAgentTerminal`: reads `tracker := c.loopTracker.getSnapshot(event.LoopID)` (approx `terminal_settlement.go:178`) AND `persisted, err := c.loadPersistedLoop(ctx, event.LoopID)` (approx `:179`) — both a process-local and a durable read, reconciled by `reconcileTerminalRoute(tracker, event, persisted)` (approx `:186`), which merges `ChannelType`/`ChannelID`/`UserID` from tracker, event, and persisted record via `mergeRouteField` (`terminal_settlement.go:38-53`), erroring on conflicting nonempty values (`terminal_settlement.go:49-51`).
- Fields read from the tracker record specifically: `tracker.ChannelType`, `tracker.ChannelID`, `tracker.UserID` (`terminal_settlement.go:57-59`, inside `reconcileTerminalRoute`).
- `component.go:1153-1173` `sendUserResponseForLoop(loopInfo *LoopInfo, ...)` builds `agentic.UserResponse{ChannelType: loopInfo.ChannelType, ChannelID: loopInfo.ChannelID, UserID: loopInfo.UserID, InReplyTo: loopInfo.LoopID, ...}` directly off a `*LoopInfo` (fields at `component.go:1165-1168`) — the pinned construction site named in the brief.

### `HandleTask` dedupe (`processor/agentic-loop/handlers.go:800-841`)

- Dedupes on `task.TaskID` only: `handlers.go:820` `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {` — comment at `handlers.go:817-819` names the purpose as JetStream-redelivery dedup, not cross-submission continuation dedup.
- `HasActiveLoopForTask` (`processor/agentic-loop/state.go:187-198`) scans `m.loops` for `entity.TaskID == taskID && !entity.State.IsTerminal()`.
- TaskID is minted fresh per submission, never reused across a user's separate messages: `processor/agentic-dispatch/component.go:951` `taskID := uuid.New().String()` (bus path) and `processor/agentic-dispatch/http.go:330` `taskID := uuid.New().String()` (HTTP sync path) — both immediately before `buildTaskMessage`/task construction. Because a fresh UUID is minted every call, `HasActiveLoopForTask`'s dedup by `TaskID` cannot fire across two distinct submissions naming the same `loopID`/`reply_to` — only a redelivery of the *same* published message (same `TaskID`) dedupes.

## Restart evidence

### `main` pins (this worktree, base `24ab736e`)

- `processor/agentic-dispatch/component.go:337-403` (`Start`): no KV/bucket read of any kind before `started = true` is set (`component.go:392`); only `setupSubscriptions(runCtx)` (`component.go:386`) runs.
- `processor/agentic-dispatch/loop_tracker.go:118-136`: both constructors build the tracker's maps empty (`make(map[...])`); no seeding.
- `processor/agentic-dispatch/http.go:750-753` — comment directly on the attach path: *"Loop must exist in dispatch's tracker. A 404 here means we either never saw the loop (e.g., process restart lost the in-memory tracker before this request) or the loop has been removed."* This is the strongest in-source statement that the tracker does not survive restart, on the exact attach seam named in the brief.
- `processor/agentic-dispatch/http.go:785-791` — comment on `GetPendingApprovalCallID`: *"Returns (\"\", false) when the loop is no longer awaiting approval — the cache divergence case (process restart, race lost, already resolved)."*
- `processor/agentic-dispatch/terminal_settlement.go` doc comment on `resolveOriginRoute` (immediately above its declaration, approx `terminal_settlement.go:326-332`): *"It reads only persisted records: the process-local tracker is never consulted for an ancestor, so a restarted process resolves the same origin."* — this is the one attach-adjacent path (terminal/completion routing origin resolution) that is explicitly documented as restart-safe, by design, because it deliberately does NOT read the tracker.
- `processor/agentic-dispatch/terminal_settlement.go:81-111` `loadPersistedLoop(ctx, loopID)`: reads `AGENT_LOOPS` KV directly (`kv.Get(ctx, loopID)`, approx line 96) as a per-call fallback inside `settleAgentTerminal` — this is a durable read used for **completion routing only**, not a tracker-repopulation mechanism; it does not write back into `loopTracker`'s maps.

### `PR #1159` pins (`origin/codex/gh1146-agentic-loop-restart`, draft, "preserve durable work across process restart")

- `git diff origin/main...origin/codex/gh1146-agentic-loop-restart --stat` → 9 files changed, 1336 insertions(+), 0 deletions(-), **all 9 files under `openspec/changes/agentic-loop-restart-safety/`** (`.openspec.yaml`, `design.md`, `inventory.md`, `proposal.md`, `tasks.md`, and four `specs/<capability>/spec.md` delta files for `agentic-dispatch`, `agentic-loop`, `agentic-model`, `agentic-tools`). **The branch touches zero `.go` files** — it is a design/proposal-only branch over `openspec/`, not yet code.
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md` (as of that branch) — new (ADDED) Requirement *"Dispatch process state is a reconstructable projection"*: *"`LoopTracker` and pending-approval caches SHALL NOT be authority. Dispatch SHALL reconstruct them from current `AGENT_LOOPS` facts after replacement and SHALL perform exact read-through for explicit LoopID operations. AutoContinue SHALL route only after an initial snapshot completes and is installed atomically."* — this is proposed target state, not implemented.
- `openspec/changes/agentic-loop-restart-safety/inventory.md:86` (that branch): *"`LoopTracker` and its early buffer are process-only. Replacement loses HTTP correlation and returns 404/409 (`http.go:705-872`)."*
- `openspec/changes/agentic-loop-restart-safety/inventory.md:91`: *"Loop publishes; dispatch consumes, updates process-only `LoopTracker`, and ACKs ... Replacement loses that correlation, including the ability to attach buffered approvals to the newly observed loop."*
- `openspec/changes/agentic-loop-restart-safety/inventory.md:152`: *"**Approval current call:** `LoopEntity.PendingApproval` and dispatch `LoopTracker` cache. The persisted fact exists, but neither loop nor dispatch reconstructs its process indexes. The cache is not authority."*
- `openspec/changes/agentic-loop-restart-safety/inventory.md` (Startup and replacement fact section): *"The approval sweeper claims restart safety (`processor/agentic-loop/approval_sweeper.go:40-42`) but snapshots only the in-memory loop map (`approval_sweeper.go:65-109`). Because startup does not hydrate that map, the claim is false on this baseline."* and *"Dispatch approval correlation is also process-only (`processor/agentic-dispatch/component.go:616-645,1021-1060`)."*

**Plain statement**: PR #1159, as it currently stands, does not touch dispatch-side state code (`processor/agentic-dispatch/*.go`) at all. It is a design proposal that independently reaches the same conclusion the `main`-pinned comments above assert — that `LoopTracker` is process-only and not restart-safe today — and proposes (not yet implements) reconstructing it from `AGENT_LOOPS` on replacement.

## Adjacent claims

- `openspec/specs/entity-id-contract/spec.md:661-665` — current-truth spec text naming this exact gap: *"...NOT rely on loop tokens for isolation until attach-seam authorization lands (#1227) — the gap is authorization at the seam that ATTACHES to a loop, not provenance at the seam that MINTS one, and perfect mint-provenance would [not close it]."*
- `openspec/specs/entity-id-contract/spec.md:677` — `- LoopManager.CreateLoopWithID MUST refuse before registering any loop state.` (form refusal only, per ADR-105/#1192; does not cover the attach/ownership question).
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:35` — `... A dispatch collision was SILENT — CreateLoopWithID overwrites ...` (motivates the #1192 mint-provenance fix; explicitly framed as pre-existing, not the attach-authorization gap).
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:59` — names the four enforced seams (`TaskMessage.Validate`, dispatch submission, `LoopManager.CreateLoopWithID`, and rule-engine publish/loop-intake) — this is the sibling explorer's `internal/looptoken` axis; not swept further here.
- `docs/operations/migration-beta162-to-beta163.md:865,873` — same "CreateLoopWithID overwrites the colliding record and context manager, merging two conversations" language, migration-note framing of the #1192 fix.
- `docs/adr/102-entity-id-segment-semantics.md` — grep for "loop" (4 hits) surfaces run/loop entity-id authority provenance (`agent.loop.run`, `agent.run.entity-id`), not attach/ownership; adjacent by subject only.
- `openspec/changes/loop-scoped-request-seams/proposal.md` — this change's own proposal: frames #1227/#1228/#1225 as possibly sharing one missing primitive; names deliverable 2 (the two restart/context-preservation questions this file answers) verbatim.
- `openspec/changes/loop-scoped-request-seams/inventory-precedent.md` — sibling inventory file in this same change directory, base `0a40ddf3`, covering "existing framework admission/authority precedent" (a different axis: what admission-gate primitive already exists elsewhere in the framework) — not duplicated here.
- #1225 — `agentic-dispatch: a non-token TaskMessage.Validate failure is a silent drop — no response, no metric, and a leaked activeLoops gauge` (OPEN)
- #1226 — `rule/expression: isValidEntityID is a hand-rolled reimplementation that accepts entity IDs the canonical validator rejects` (OPEN) — appears in an `openspec/changes/loop-scoped-request-seams/tasks.md`-adjacent search but is not on this surface; listed because the brief named it.
- #1227 — `agentic-dispatch: continuing a loop by reply_to has no ownership check — a second holder of the token takes over the tracker entry, completion routing, and the in-flight context` (OPEN) — the issue this file's surface answers directly; its body cites the exact same line numbers this inventory independently re-derived (component.go:922-925 as of the issue's pinning commit `7431d036`; drifted slightly by base `24ab736e`, see Claimed gap).
- #1228 — `agentic: loop-token carriers outside the four enforced seams accept non-canonical tokens (UserSignal, ApprovalResponse, control requests)` (OPEN) — sibling explorer's axis.
- PR #1159 — `fix(agentic-loop): preserve durable work across process restart` (OPEN, draft, Codex, branch `codex/gh1146-agentic-loop-restart`) — see Restart evidence; design/proposal-only as of this base.
- PR #1231 — `spec(loop-seams): settle whether #1227/#1228/#1225 share one missing primitive` (OPEN) — the PR this inventory file is deliverable 1 of; body states deliverable 1 (inventory) in progress, no code/spec delta yet, `implemented-by: opus`.

## Consumers

- `LoopTracker.loops` map (`loop_tracker.go:104`) — written by `Track` (`component.go` bus/HTTP task submission, `component.go:1063` new-external-loop branch), `UpdateState`/`UpdateIterations`/`UpdateCompletion`/`updateCompletionAt`, `SetPendingApproval`/`ClearPendingApproval`, `UpdateWorkflowContext`/`UpdateContextRequestID`, `Remove`; read by `Get`, `getSnapshot`, `GetActiveLoop`, `GetUserLoops`, `GetAllLoops`, `Count`. No subject/bucket backs it directly — it is populated only by locally-handled stream events (`agent.created.*`, `agent.approval_pending.*`, terminal events) and locally-initiated submissions, per the "Restart evidence" section.
- `AGENT_LOOPS` KV bucket — read by `terminal_settlement.go`'s `loadPersistedLoop` (completion routing only); read (per event stream payload, not bucket) elsewhere. Bucket name resolved via `c.loopsBucketName()` (referenced at `terminal_settlement.go:88`, not further traced — out of this surface).
- `agent.created.*` stream / `LoopCreatedEvent` payload — consumed by `handleAgentCreated` (`component.go:1030-1078`), the one attach path that branches on tracker existence.
- `agent.approval_pending.*` stream / `ApprovalPendingEvent` payload — consumed by `handleAgentApprovalPending` (`component.go:1092-1123`), writes `SetPendingApproval`.
- `agent.approval_response.*` — published by `handleLoopApproval`/`publishApprovalResponse` (`http.go:806,858-873`); consumed by `agentic-loop` (not traced further — out of this surface per brief scope).
- `agent.task` subject — published by `handleTaskSubmission` (bus) and `processTaskSubmissionSync` (HTTP); consumed by `processor/agentic-loop`'s `HandleTask` (`handlers.go:800`).
- `m.loops`, `m.pendingTools`, `m.contextManagers` (`processor/agentic-loop/state.go:100-126`, `LoopManager` fields) — all three overwritten unconditionally by `CreateLoopWithID` (`state.go:171,172,179`); `m.loops` also read by `HasActiveLoopForTask` (`state.go:187-198`) and `GetLoop` (`state.go:194-210`); `m.contextManagers[loopID]` read by `GetContextManager` (caller: `handlers.go:872`).

## Searches

- `gopls workspace_symbol -matcher=fuzzy LoopTracker` — NOT RUN (gopls not invoked this session; `git grep`/`sed -n` used throughout per the time budget — see note below).
- `git grep -n "func (t \*LoopTracker)"` → 20 (loop_tracker.go method census, listed above)
- `grep -n "func (c \*Component)" processor/agentic-dispatch/component.go` → 40 (full method list scanned for attach-relevant entries)
- `grep -n "func (c \*Component)" processor/agentic-dispatch/http.go` → 15
- `grep -n "loopInfo == nil\|func (c \*Component) handle" processor/agentic-dispatch/commands.go` → 6
- `git grep -n "func.*settleAgentTerminal" processor/agentic-dispatch/` → 1
- `git grep -n "canUserControlLoop" -- '*.go'` → 3 (declaration + 1 caller in `commands.go:73` + doc comment)
- `git grep -n "canUserControlLoop\|hasPermission(" processor/agentic-dispatch/*_test.go` → 0
- `git grep -n "GetActiveLoop\|loopTracker.Get(" processor/agentic-dispatch/*.go` (non-test) → 11 (listed under LoopTracker consumers above)
- `git grep -n "\.CreateLoopWithID(\|\.CreateLoop(" -- '*.go'` (non-test) → 3 (`handlers.go:834`, `handlers.go:839`, `state.go:132`) + 1 doc-comment mention (`doc.go:145`)
- `grep -n "HasActiveLoopForTask|GenerateLoopID" processor/agentic-loop/*.go` (non-test) → 5
- `git grep -ni "restore|rehydrat|hydrate|LoadFromKV|kv.WatchAll|kv.GetAll" processor/agentic-dispatch/` (excl. `_test.go`) → 2, both irrelevant (activity-view `kv.WatchAll` comments)
- `git grep -n "taskID :=\|TaskID:\s*taskID\|uuid.New().String()" processor/agentic-dispatch/component.go processor/agentic-dispatch/http.go` → 28 (TaskID minting sites confirmed: `component.go:951`, `http.go:330`)
- `git diff origin/main...origin/codex/gh1146-agentic-loop-restart --stat` → 9 files, 1336(+)/0(-), all under `openspec/changes/agentic-loop-restart-safety/`
- `git diff origin/main...origin/codex/gh1146-agentic-loop-restart --name-only` → 9 (listed under Restart evidence)
- `git show origin/codex/gh1146-agentic-loop-restart:openspec/specs/agentic-dispatch/spec.md` → fatal: path does not exist (confirms PR #1159 does not touch the seeded `openspec/specs/` tree, only its own `openspec/changes/` proposal)
- `git show origin/codex/gh1146-agentic-loop-restart:openspec/changes/agentic-loop-restart-safety/inventory.md \| grep -n "LoopTracker\|CreateLoopWithID\|loopTracker"` → 3
- `gh issue list --search "1227" --state all --json number,title` → 3 (#1227, #1228, #1192)
- `gh issue view 1227 --json number,title,body,state` → 1 (full body read, cross-checked against independently-derived pins above)
- `gh issue view 1225/1226/1228 --json number,title,state` → 3
- `gh pr view 1159 --json number,title,state,body` → 1
- `gh pr view 1231 --json number,title,state,body` → 1 (full body read)
- `openspec list` → 1 (`loop-scoped-request-seams 0/7 tasks`)
- `ls openspec/changes/loop-scoped-request-seams/` → 3 files (`inventory-precedent.md`, `proposal.md`, `tasks.md`) — confirms the sibling inventory file's existence and this file's own target path was not yet present.
- `cat openspec/changes/loop-scoped-request-seams/proposal.md` → full read (28 lines shown)
- `cat openspec/changes/loop-scoped-request-seams/tasks.md` → full read
- `grep -n "^#\|^base:" openspec/changes/loop-scoped-request-seams/inventory-precedent.md` → 9 (headers only, content not read, per Bounds — stop at the surface named in this brief)
- `grep -n "CreateLoopWithID|reply_to|canUserControlLoop|LoopTracker" docs/adr/105-*.md docs/adr/102-*.md` → 5
- `grep -n "CreateLoopWithID|canUserControlLoop|LoopTracker" docs/operations/migration-beta162-to-beta163.md` → 2
- `ls openspec/specs/` → 49 capability dirs (scanned for `agentic-dispatch` — absent; confirms the dispatch capability spec is not yet seeded)
- `git grep -ln "loop" openspec/specs/*/spec.md` → 0 (unexpected zero — see note: the follow-up targeted grep against agentic-loop/spec.md and user-response-subject-ownership/spec.md for `reply_to|LoopTracker|attach|CreateLoopWithID` also returned 0, run in the same combined call)
- `git grep -ni "reply_to|LoopTracker|attach|CreateLoopWithID" openspec/specs/user-response-subject-ownership/spec.md openspec/specs/agentic-terminal-events/spec.md openspec/specs/entity-id-contract/spec.md` → 12 (all in `entity-id-contract/spec.md`, listed under Adjacent claims)

**NOT RUN** (time/tool-call budget; the brief's structural-enumeration tool was not invoked this session — every structural fact above was derived via `grep -n`/`git grep -n`/`sed -n` against tracked source, which independently reproduced the line-numbered claims in issue #1227's body):
- `gopls workspace_symbol`, `gopls implementation`, `gopls references`, `gopls call_hierarchy` — none invoked. All caller/reference enumeration above used `git grep -n` instead.
- Full-file reads of `processor/agentic-dispatch/http.go` beyond the sections cited (lines outside 194-412, 552-950 not read).
- `processor/agentic-loop/approval_sweeper.go` — named only via the PR #1159 inventory quote above; not independently read on `main`.
- `processor/agentic-loop/governance_dispatcher.go` — named only via the PR #1159 inventory quote; not independently read.
- Exact line numbers inside `terminal_settlement.go`'s `settleAgentTerminal` body past line 260 were approximated from the two `sed -n` reads (`1,60` and `60,260`) rather than re-verified with a third read; flagged as approx above where used.
- `git log -S` on any of the named symbols — not run; no claim made here about when/why a behavior was introduced beyond what ADR-105 and the migration doc already state.
