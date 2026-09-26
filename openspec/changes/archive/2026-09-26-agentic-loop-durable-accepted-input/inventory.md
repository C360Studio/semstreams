# Inventory: agentic-loop-durable-accepted-input

base: 9e5d84554016fbc21f20e69b3539edabed85292e

Claim commit `7c371bfc5ce5fef936eb90d1b29299b740b8162f` (worktree HEAD) adds only `proposal.md` on top of this base
(`git diff --stat 9e5d8455 7c371bfc` → one file, `proposal.md`, 61 insertions). Every code pin below is therefore
identical whether read at `9e5d8455` or at HEAD.

Brief: enumerate every reader and writer of the three durable-input facts named in #1365/#1345 (the deferred-turn
marker, the task-prompt cache, the resumable intake record), re-pin the five task-intake failure sites named at
#1345's filing (`9d157232`) against this base, and the contract rows, record-size policy, sister-repo readers, and
Tier 1 surface those facts touch. No judgment, no options, no verdict — enumeration only.

## Claimed gap

- `agentic/state.go:125` — `PendingContinuation bool `json:"pending_continuation,omitempty"``
- `agentic/state.go:141` — `PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"``
- `processor/agentic-loop/state.go:90` — `taskPrompts          map[string]string                   // loopID -> original task prompt (for context recovery)`
- `processor/agentic-loop/component.go:151` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1699` — `} else if err := c.createLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1741` — `if err := c.publishResults(ctx, result); err != nil {`

The last two are pinned here deliberately: at `9d157232` (#1345's filing) these two sites were fire-and-forget swallows
(`c.publishResults(ctx, result)` and `c.persistLoopState(ctx, result.LoopID)`, called without checking their error,
followed unconditionally by `return nil`). At this base they are not — see Fact 3 below, which re-pins all five sites
and states plainly that only three of the five still match the issue's description today.

## Fact 1 — The deferred turn (`PendingContinuation` / `PendingContinuationRequestID`)

Confirmed by `gopls references agentic/state.go:125:2` (13 refs: 7 production, 6 test) and
`gopls references agentic/state.go:141:2` (22 refs: 8 production, 14 test) — no writer or reader outside the sites
below.

### Set

- `processor/agentic-loop/state.go:327` — `if _, outstanding := m.outstandingRequests[loopID]; outstanding {`
- `processor/agentic-loop/state.go:328` — `entity.PendingContinuation = true`
- `processor/agentic-loop/state.go:333` — `entity.PendingContinuationRequestID = ""`

(inside `attachContinuation`, declared `processor/agentic-loop/state.go:292` — see Admission below)

- `processor/agentic-loop/component.go:3140` — `entity.PendingContinuation = true`
- `processor/agentic-loop/component.go:3144` — `entity.PendingContinuationRequestID = ""`

(inside `persistDeferredContinuationMarker`, `processor/agentic-loop/component.go:3105` — this is the in-memory
marker from `attachContinuation` reaching the KV record; it renders the record it already read, never the live
entity, and is best-effort against a lost compare-and-swap.)

- `processor/agentic-loop/state.go:1234` — `func (m *LoopManager) TrackRequest(requestID, loopID string) {`
- `processor/agentic-loop/state.go:1239` — `if entity, exists := m.loops[loopID]; exists && entity.PendingContinuation {`
- `processor/agentic-loop/state.go:1240` — `entity.PendingContinuationRequestID = requestID`

`TrackRequest` is the site that gives an already-set marker its carrier: once a request is tracked for the loop, a
pending continuation's `PendingContinuationRequestID` moves from empty to that request's ID.

### Clear

- `processor/agentic-loop/state.go:1295` — `func (m *LoopManager) SettleRequest(loopID, requestID string) {`
- `processor/agentic-loop/state.go:1301` — `if entity, exists := m.loops[loopID]; exists && requestID != "" && entity.PendingContinuationRequestID == requestID {`
- `processor/agentic-loop/state.go:1302` — `entity.PendingContinuation = false`
- `processor/agentic-loop/state.go:1303` — `entity.PendingContinuationRequestID = ""`

This is the ordinary clear: the request that carried the turn settled, so the marker and its carrier both clear.

- `processor/agentic-loop/state.go:435` — `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" {`
- `processor/agentic-loop/state.go:442` — `entity.PendingContinuation = false`

This is the L4a rebuild clear (see below) — a marker with an EMPTY carrier is cleared with a warning rather than
carried forward, because the turn's text is not in the record.

### Read

- `processor/agentic-loop/state.go:633` — `func (m *LoopManager) HasPendingContinuation(loopID string) bool {`
- `processor/agentic-loop/state.go:637` — `return exists && entity.PendingContinuation && entity.PendingContinuationRequestID == ""`
- `processor/agentic-loop/handlers.go:1564` — `if h.loopManager.HasPendingContinuation(loopID) {`
- `processor/agentic-loop/handlers.go:2764` — `carrying := h.loopManager.HasPendingContinuation(loopID)`

`HasPendingContinuation` reads true only for a marker with NO carrier yet (an uncarried, undeferrable-again turn);
once `TrackRequest` gives it a carrier it reads false again until `SettleRequest` or the L4a clear.

### Admission — what text arrives, in which type, where it goes

- `processor/agentic-loop/state.go:292` — `func (m *LoopManager) attachContinuation(loopID, taskID string) (agentic.LoopEntity, bool, error) {`
- `processor/agentic-loop/handlers.go:918` — `entity, deferred, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`

`attachContinuation` itself carries no text — it flips the marker and returns `(entity, deferred bool, error)`.
The text is `agentic.TaskMessage.Prompt`, a plain string:

- `agentic/user_types.go:324` — `Prompt          string `json:"prompt"``

In `HandleTask`, the turn's text is appended to the loop's live, in-memory context manager and the task-prompt
cache — on EITHER the ordinary or the deferred branch, before the `deferred` check is read:

- `processor/agentic-loop/handlers.go:1013` — `Role:    "user",`
- `processor/agentic-loop/handlers.go:1014` — `Content: task.Prompt,`
- `processor/agentic-loop/handlers.go:1016` — `h.loopManager.CacheTaskPrompt(loopID, task.Prompt)`

Then the deferral itself returns without publishing:

- `processor/agentic-loop/handlers.go:1097` — `return h.deferredContinuationResult(loopID, task.TaskID, entity), nil`

So today the turn's text lives in exactly two places, both in-memory only: the loop's `ContextManager` (via
`cm.AddMessage`, `RegionRecentHistory`) and `taskPrompts[loopID]` (last-task-only, overwritten by the next task) —
neither is part of the `agentic.LoopEntity` record the marker rides on.

### L4a rebuild — the clear-with-warning

- `processor/agentic-loop/state.go:383` — `func (m *LoopManager) restoreLoopFromRequest(`
- `processor/agentic-loop/state.go:435` — `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" {`
- `processor/agentic-loop/state.go:442` — `entity.PendingContinuation = false`

A NON-empty `PendingContinuationRequestID` is left untouched by this function (the retained request replays and
still names its carrier); only the empty-carrier case is cleared.

### Graph projection / schema

- `agentic/state.go:49` — `type LoopEntity struct {`

`LoopEntity` declares no `Triples()` / `EntityID()` methods (`git grep -n "func (e \*LoopEntity)" agentic/*.go` →
5 hits, none named `Triples` or `EntityID`; `git grep -n "func.*Triples" agentic/*.go` → 18 hits, all on other
agentic entity types — `AgentLessonEntity`, `LoopExecutionEntity`, `ModelEndpointEntity`, `OpsDiagnosisEntity`,
`WebObservationEntity`). `LoopExecutionEntity` (a distinct type, `agentic/loop_execution_entity.go:102`) is the
graph-facing projection of a loop's lifecycle; it is not generated from or synced with `LoopEntity`'s
`PendingContinuation` field. No `schemas/*.json` file contains `pending_continuation` (`## Searches`) — the
`schemas/` directory holds only per-component CONFIGURATION schemas (e.g. `agentic-loop.v1.json`), not entity/record
schemas, so `LoopEntity` is not schema-generated at all.

## Fact 2 — The task prompt (`taskPrompts`)

Confirmed by `gopls references processor/agentic-loop/state.go:90:2` → 6 refs, all listed below; no writer or
reader outside them.

### Writer

- `processor/agentic-loop/state.go:1059` — `// CacheTaskPrompt stores the original task prompt for context recovery.`
- `processor/agentic-loop/state.go:1062` — `func (m *LoopManager) CacheTaskPrompt(loopID, prompt string) {`
- `processor/agentic-loop/state.go:1065` — `m.taskPrompts[loopID] = prompt`
- `processor/agentic-loop/handlers.go:1016` — `h.loopManager.CacheTaskPrompt(loopID, task.Prompt)`

One writer, one call site: `HandleTask`, on every task delivery (birth and continuation alike), before the
`deferred`/`Created` branches.

### Readers

- `processor/agentic-loop/state.go:1069` — `func (m *LoopManager) GetTaskPrompt(loopID string) string {`
- `processor/agentic-loop/state.go:1072` — `return m.taskPrompts[loopID]`
- `processor/agentic-loop/handlers.go:2529` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`
- `processor/agentic-loop/handlers.go:3336` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`
- `processor/agentic-loop/handlers.go:3302` — `prompt := h.loopManager.GetTaskPrompt(loopID)`
- `processor/agentic-loop/handlers.go:3303` — `if prompt == "" {`

`:2529` sets `agentic.LoopCompletedEvent.Prompt` (inside `handleCompleteResponse`), `:3336` sets
`agentic.LoopFailedEvent.Prompt` (inside `buildFailureEvent`), `:3302-3303` is `recoverEmptyContext`'s fallback —
when the cache is empty it substitutes the literal `"Continue with the task."` (confirmed at
`processor/agentic-loop/doc.go:295`, quoted below).

### Birth field

- `agentic/user_types.go:324` — `Prompt          string `json:"prompt"``

`TaskMessage.Prompt` is present at birth on every task delivery, including a redelivered one — the prompt is never
missing from the wire message itself; it is missing only from `agentic.LoopEntity`, the record a rebuild reads.

### Mirror — caches that ARE restored on rebuild

`restoreLoopFromRequest` restores five other per-loop caches from the retained request/record it rebuilds from, in
the same function that clears the `PendingContinuation` marker (Fact 1):

- `processor/agentic-loop/state.go:481` — `m.cachedTools[record.ID] = request.Tools`
- `processor/agentic-loop/state.go:482` — `m.cachedToolChoice[record.ID] = request.ToolChoice`
- `processor/agentic-loop/state.go:483` — `m.cachedResponseFormat[record.ID] = request.ResponseFormat`
- `processor/agentic-loop/state.go:485` — `m.cachedRequestTimeout[record.ID] = request.Timeout`
- `processor/agentic-loop/state.go:501` — `m.cachedMetadata[record.ID] = metadata`

The first three read off the retained REQUEST (never off a `TaskMessage` this process never saw); `cachedMetadata`
reads off the RECORD, written once at birth. `taskPrompts` is the one cache with no field on either the request or
the record to restore itself from — this is the fact `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts`-era doc comment names directly:

- `processor/agentic-loop/doc.go:295` — `// The loop's TASK PROMPT is the same limitation one field over. taskPrompts is the one`

## Fact 3 — The resumable intake record and the five intake failure sites

### `rememberPendingTaskResult` / `pendingTaskResult` / `clearPendingTaskResult`

- `processor/agentic-loop/component.go:147` — `// pendingTaskResults retains the not-yet-published spawn result when a`
- `processor/agentic-loop/component.go:151` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1757` — `func (c *Component) rememberPendingTaskResult(taskID string, result HandlerResult) {`
- `processor/agentic-loop/component.go:1761` — `c.pendingTaskResults = make(map[string]HandlerResult)`
- `processor/agentic-loop/component.go:1763` — `c.pendingTaskResults[taskID] = result`
- `processor/agentic-loop/component.go:1766` — `func (c *Component) pendingTaskResult(taskID, loopID string) (HandlerResult, bool) {`
- `processor/agentic-loop/component.go:1769` — `result, ok := c.pendingTaskResults[taskID]`
- `processor/agentic-loop/component.go:1778` — `delete(c.pendingTaskResults, taskID)`

`gopls references processor/agentic-loop/component.go:151:2` → 6 refs, all inside these three methods — no other
reader or writer, and nothing marshals this map to KV, a stream, or any durable store: it is a plain in-process
`map[string]HandlerResult` on the `Component` struct, guarded by `c.mu`, and is lost on process replacement. It is
scoped to exactly one failure class today — the transient-lineage-write NAK:

- `processor/agentic-loop/component.go:1616` — `return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)`
- `processor/agentic-loop/component.go:1630` — `return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)`

(the second is guarded by `errs.IsTransient(err)`, which is where `rememberPendingTaskResult` is actually called;
the first is the birth-failure branch, which calls `clearPendingTaskResult` first and then fails the loop, never
remembering the result.)

### The five failure sites, re-pinned at `9e5d8455` (behaviour compared against `9d157232`)

At `9d157232` (#1345's filing and the archived `settle-after-durable-effect` design/tasks residual), the five
named sites in `handleTaskMessage` were: an undecodable envelope (`:1279-1282`), a wrong payload type
(`:1284-1288`), a `HandleTask` failure (`:1304-1318`), a failed first publication (`:1387`, a bare
`c.publishResults(ctx, result)` call with the returned error discarded), and a failed loop-state write (`:1390`, a
bare `c.persistLoopState(ctx, result.LoopID)` call with the returned error discarded) — all five followed by an
unconditional `return nil` at the function's end, i.e. all five logged and ACKed.

Re-pinned at this base:

1. **Decode failure** — still logs and ACKs:
   - `processor/agentic-loop/component.go:1473` — `baseMsg, err := c.decoder.Decode(data)`
   - `processor/agentic-loop/component.go:1475` — `c.logger.Error("Failed to unmarshal BaseMessage", "error", err)`
   - `processor/agentic-loop/component.go:1476` — `return nil`

2. **Wrong payload type** — still logs and ACKs:
   - `processor/agentic-loop/component.go:1479` — `task, ok := baseMsg.Payload().(*agentic.TaskMessage)`
   - `processor/agentic-loop/component.go:1481` — `c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))`
   - `processor/agentic-loop/component.go:1482` — `return nil`

3. **`HandleTask` failure** — still logs and ACKs (except the `ErrLoopBusy` refusal, which was already a
   deliberate non-error `Warn`+`return nil` at `9d157232` too — not a change):
   - `processor/agentic-loop/component.go:1533` — `result, err := c.handler.HandleTask(ctx, *task)`
   - `processor/agentic-loop/component.go:1546` — `c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)`
   - `processor/agentic-loop/component.go:1547` — `return nil`

4. **First publication failure** — NO LONGER a swallow at this base. The record is now created BEFORE the
   publish (disposition/`createLoopState` block, below), and a publish failure returns the error rather than
   discarding it:
   - `processor/agentic-loop/component.go:1741` — `if err := c.publishResults(ctx, result); err != nil {`
   - `processor/agentic-loop/component.go:1742` — `c.logger.Error("Birth did not publish the request its record names — the delivery is not acknowledged",`
   - `processor/agentic-loop/component.go:1745` — `return err`

5. **Loop-state write failure** — also NO LONGER a swallow at this base. `createLoopState` runs before the
   publish and its error is returned (Retry), distinguishing a benign already-exists race from a genuine write
   failure:
   - `processor/agentic-loop/component.go:1699` — `} else if err := c.createLoopState(ctx, result.LoopID); err != nil {`
   - `processor/agentic-loop/component.go:1707` — `c.logger.Error("Failed to write the loop record at birth — the first request is not published",`
   - `processor/agentic-loop/component.go:1716` — `return errs.WrapTransient(err, "agentic-loop", "handleTaskMessage",`

So of the five sites #1345 named, three (decode, wrong payload type, `HandleTask` failure) still match the issue's
description at this base; two (first publication, loop-state write) have already been converted to Retry by work
that landed since `9d157232` (the record-before-publish reordering visible at `component.go:1658-1718`, part of
the `#1330`/durable-applied-facts and transition-result series). Neither conversion added a durable resumable-intake
record — a redelivery of #4 or #5 still meets the SAME dedup-and-ACK path (below) once the record it wrote lets a
retry find an existing loop, so #1345's core claim — "the lane needs resumable intake before Retry can mean
anything" — is unaffected by this narrowing; only the count of still-swallowing sites is smaller than the issue
states.

### The ack-without-publish dedup path

The issue and the archived L1 design cite `handleTaskMessage:1320-1327` at `9d157232` for this path (confirmed by
reading that revision: `git show 9d157232:processor/agentic-loop/component.go` lines 1320-1327 are the
`!result.Created` / `pendingTaskResult` / `"Task deduplicated — loop already active"` / `return nil` block). Re-pinned
at this base:

- `processor/agentic-loop/component.go:1577` — `if !result.Created {`
- `processor/agentic-loop/component.go:1578` — `pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1579` — `if !ok {`
- `processor/agentic-loop/component.go:1580` — `c.logger.Debug("Task deduplicated — loop already active",`
- `processor/agentic-loop/component.go:1583` — `return nil`

This is `HandleTask`'s in-memory redelivery dedup surfacing at the component layer: `HandleTask` itself returns
`HandlerResult{LoopID: existingID}, nil` (with `Created` false) when `HasActiveLoopForTask` already matches:

- `processor/agentic-loop/handlers.go:896` — `return HandlerResult{LoopID: existingID}, nil`

A second failure of one of the five sites above, on redelivery, meets this branch — the loop was already created,
`pendingTaskResult` finds nothing (nothing remembers those five classes), and the delivery is acknowledged with no
publish. This is #1345's "not a classification change" argument, verified against the current dedup path.

### `handleSpawnIdentityFailure` — the one intake path L1 already converted

- `processor/agentic-loop/component.go:1843` — `func (c *Component) handleSpawnIdentityFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, err error) error {`

Its body ends by calling `handleLoopFailure`, which commits a terminal failure through the terminal owner
(`commitTerminal`) — a durable effect precedes its ACK, unlike the five sites above.

## Fact 4 — Contract rows and the settle-after-durable-effect exemption

`openspec/specs/agentic-loop/spec.md`, under `### Requirement: Loop input classes settle after owner-specific
durable done` (`:886`):

- `openspec/specs/agentic-loop/spec.md:886` — `### Requirement: Loop input classes settle after owner-specific durable done`
- `openspec/specs/agentic-loop/spec.md:1073` — `#### Scenario: The task lane's results settle on their own owner, and its errors stay exempt`
- `openspec/specs/agentic-loop/spec.md:1075` — `- **WHEN** `HandleTask` returns a result that created the loop`
- `openspec/specs/agentic-loop/spec.md:1076` — `- **THEN** the task lane writes the record by create-once, then publishes the first request; a refused create or a`
- `openspec/specs/agentic-loop/spec.md:1077` — `failed publish releases the loop and is retried`
- `openspec/specs/agentic-loop/spec.md:1084` — `- **THEN** the failure is logged and the delivery acknowledged, the exemption tracked as issue #1345; when that issue`
- `openspec/specs/agentic-loop/spec.md:1087` — `#### Scenario: The deferred continuation's replacement behaviour is owed to #1365`
- `openspec/specs/agentic-loop/spec.md:1092` — `- **AND** issue #1365, with #1345, owns making the rebuilt loop recover them from durable accepted-input facts; that`

A second, separate requirement immediately below names the exemption again:

- `openspec/specs/agentic-loop/spec.md:1104` — `### Requirement: Task intake is the one loop input class this layer does not convert`
- `openspec/specs/agentic-loop/spec.md:1107` — `reads as covering it. An undecodable task envelope, a task payload of the wrong type, a `HandleTask` failure, and a`
- `openspec/specs/agentic-loop/spec.md:1108` — `failed first publication or loop-state write SHALL keep their pre-existing log-and-acknowledge settlement. Converting`
- `openspec/specs/agentic-loop/spec.md:1111` — `The exemption is tracked as issue #1345 and is not a permanent property of the lane.`
- `openspec/specs/agentic-loop/spec.md:1120` — `- **AND** the exemption is recorded here and tracked as issue #1345, with resumable intake named as its`

Recorded as a plain fact, not a verdict: the requirement at `:1104-1111` states that "a failed first publication or
loop-state write SHALL keep their pre-existing log-and-acknowledge settlement," which is the same claim #1345 made
at `9d157232`; the scenario nine lines above it at `:1076-1077`, in the same file, already reads "a refused create or
a failed publish releases the loop and is retried" — and Fact 3 above pins the code at this base doing exactly that
(Retry, not Ack) for both sites. The spec's two passages disagree with each other, and `:1104-1111` disagrees with
the code pinned in Fact 3.

The `settle-after-durable-effect` exemption text itself lives in the archived L1 change, not in an open change
(`openspec list` → one open change, this one, "(no tasks.md)"):

- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md:238` — `## Declared residual — task intake is the one loop input class this layer does not convert`
- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md:240` — ``handleTaskMessage` still answers five failures with a log line and an ACK: an undecodable envelope`
- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md:253` — `The loop delta names this exemption as a requirement rather than leaving it to the absence of a scenario, because`
- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md:254` — `the requirement above it reads as covering the class. It is tracked as **#1345** (beta.163,`
- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/tasks.md:192` — `- [ ] 8c.9 (R6c residual, owner) Task intake's own swallows are unconverted and now named in the loop delta:`
- `openspec/changes/archive/2026-09-19-settle-after-durable-effect/tasks.md:193` — `(decode), `:1284-1288` (wrong payload type), `:1304-1318` (handler failure),`

(the task item is checked `[ ]` unchecked — the archived tasks.md leaves 8c.9 open/owner-owned rather than closed,
consistent with #1345 remaining open today)

## Fact 5 — Record size and retention

The `AGENT_LOOPS` bucket (the `loops` KVWritePort, `processor/agentic-loop/config.go:426`) is acquired through:

- `processor/agentic-loop/internal/loopbucket/acquire.go:20` — `bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`

History 10, TTL 24h, MaxAge 24h, and MaxBytes required to be `<= 0` (no bucket-level byte cap) — enforced by
observation with no reconciliation (a bucket that drifts from this policy fails acquisition rather than being
repaired).

- `processor/agentic-loop/component.go:109` — `loopsBucket           jetstream.KeyValue`
- `processor/agentic-loop/component.go:870` — `loopsBucket, err := loopbucket.AcquireOwner(ctx, js, name)`
- `processor/agentic-loop/component.go:874` — `c.loopsBucket = loopsBucket`

`loopsBucket` is typed as the raw `jetstream.KeyValue`, not the `natsclient` wrapper. The wrapper's per-value size
guard exists elsewhere in the codebase but does not apply to this bucket:

- `natsclient/kv.go:29` — `MaxValueSize          int           // Maximum size for values (default: 1MB)`
- `natsclient/kv.go:40` — `MaxValueSize:          1024 * 1024, // 1MB default max value size`
- `natsclient/kv.go:358` — `if kv.options.MaxValueSize > 0 && len(newValue) > kv.options.MaxValueSize {`

That check runs inside `natsclient`'s own `KV` type; `AGENT_LOOPS` writes go through `jetstream.KeyValue.Update`
directly (`processor/agentic-loop/component.go` — e.g. `c.loopsBucket.Update(...)` at the marker-persist site
pinned in Fact 1), so no application-level per-value size guard is in force for a `LoopEntity` record today — only
whatever limit the NATS server itself applies to a JetStream message, which this bucket's `KeyValueConfig` does not
set.

No existing size bound names `LoopEntity`, `AGENT_LOOPS`, or a loop record specifically under the payload-size class:

The search `git grep -n "payload-size-chokepoints\|payload_size_chokepoints\|#857" -- 'openspec/*' 'docs/*'` → 60 hits, none
  naming `AGENT_LOOPS` or a loop record (`## Searches`); the one hit inside this change's own `proposal.md:58` is
  the proposal citing #857 as the class the design must decide a bound under, not an existing bound.

The current serialized size of a `LoopEntity` record cannot be measured from static inspection; a measurement would
run as a unit test that marshals a representative `agentic.LoopEntity` (with `PendingToolResults`, `PendingApproval`,
`Metadata`, and — under this change — the deferred-turn text populated) via `json.Marshal` and reports `len(data)`,
or as an integration assertion against a live `AGENT_LOOPS` bucket entry's `len(value)`.

## Fact 6 — Sister repositories (read-only, one bounded pass)

`ls /Users/coby/Code/c360/` siblings searched (git repos only; `semdocs`, `semlink`, `workspaces`, `archive`,
`c360studio.github.io` excluded as non-code or absent): `semsource`, `semboids`, `semsage`, `semops`, `semdragon`,
`semconnect`, `semmem`, `semembed`, `seminstruct`, `semmachina`, `semteams`, `semspec`, `semdev`, `servicesim`.

`git grep -nE "PendingContinuation|pending_continuation"` run in every repo above → **0 in every repo**. No sister
reads or writes `PendingContinuation`, `pending_continuation`, or `pending_continuation_request_id` under any
spelling.

`git grep -nE "LoopEntity|AGENT_LOOPS"` run in every repo above, then filtered to non-evidence, non-vendored-doc
code:

| Sister | file:line | reads/writes which key |
|---|---|---|
| semsage | `processor/ui-api/component.go:217`, `http.go:49`, `sse.go:15` | reads `AGENT_LOOPS` bucket wholesale for loop listing/SSE/trajectory, decodes into its OWN narrow struct (`processor/ui-api/types.go:14-27`) with named fields `id/task_id/state/role/model/iterations/max_iterations/depth/max_depth/parent_loop_id/started_at/completed_at/outcome/error` — no `pending_continuation` or `prompt`/`task_prompt` field exists in that struct, confirmed zero hits for those terms in `agentgraph/*.go` and `processor/ui-api/*.go` |
| semspec | `agentgraph/graph.go:166`, `agentgraph/entities.go:65` | `LoopEntityID` is an entity-ID STRING formatter only (six-part ID builder); does not decode `AGENT_LOOPS` JSON at all; zero hits for `pending_continuation`/`task_prompt` in `agentgraph/*.go` |
| semteams | `schemas/agentic-loop.v1.json`, `cmd/semteams/main.go:492` | vendors the agentic-loop COMPONENT CONFIG schema (not the `LoopEntity` record schema) and threads the `loops_bucket` NAME through `extractLoopsBucket`; zero hits for `pending_continuation`/`task_prompt` in the schema or `main.go` |
| semdev | `configs/semdev-bootstrap.json`, `configs/semdev-live-gemini.json`, `internal/tools/checkfloors/checkfloors.go` | threads the `AGENT_LOOPS` bucket name through config only; `checkfloors.go`'s `routeLoopEntityID` is a graph-entity-ID parameter name, not a decode of the `LoopEntity` JSON record; zero hits for `pending_continuation`/`task_prompt` |
| semdragon | `config/semdragons*.json` | config bucket-name threading only; zero hits for `pending_continuation`/`task_prompt` in those files |
| semmachina | `internal/resume/doc.go:80`, `internal/resume/pending.go:88` | design-note PROSE explaining that `AGENT_LOOPS` `state=running` cannot answer a liveness question for semmachina's own resume package (only a handler transitions a loop out of `running`) — no code reads `pending_continuation` or a task prompt; the package explicitly does not read `AGENT_LOOPS` for its own resume decisions |
| semconnect | (evidence logs only) | 33 raw hits, all inside `openspec/changes/*/evidence/**` conformance logs/JSON dumps from prior qualification runs — not source that reads or decodes the record |
| semboids, semops, semmem, semembed, seminstruct, servicesim | — | 0 hits for `LoopEntity`/`AGENT_LOOPS` under any spelling |

No sister reads or decodes a task-intake ACK/Retry/Quarantine disposition: none of the above touch `agent.task`,
`handleTaskMessage`, or the JetStream consumer policy on that subject (`git grep -n "agent\.task"` was not run
separately against every sister in this pass — named here as **NOT RUN**, see `## Searches`).

## Fact 7 — Tier 1 surface

`agentic` is listed in the Tier 1 package set:

- `release/tier1-packages.txt:39` — `github.com/c360studio/semstreams/agentic`

Exported `LoopEntity` fields today (`agentic/state.go:49-160`), each a pin:

- `agentic/state.go:50` — `ID                 string                `json:"id"``
- `agentic/state.go:51` — `TaskID             string                `json:"task_id"``
- `agentic/state.go:52` — `State              LoopState             `json:"state"``
- `agentic/state.go:53` — `Role               string                `json:"role"``
- `agentic/state.go:54` — `Model              string                `json:"model"``
- `agentic/state.go:55` — `Iterations         int                   `json:"iterations"``
- `agentic/state.go:56` — `MaxIterations      int                   `json:"max_iterations"``
- `agentic/state.go:57` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID`
- `agentic/state.go:81` — `PublishedRequestID string    `json:"published_request_id,omitempty"``
- `agentic/state.go:82` — `StartedAt          time.Time `json:"started_at,omitempty"`     // When the loop was created`
- `agentic/state.go:83` — `TimeoutAt          time.Time `json:"timeout_at,omitempty"`     // When the loop should timeout`
- `agentic/state.go:84` — `ParentLoopID       string    `json:"parent_loop_id,omitempty"` // Parent loop ID for architect->editor relationship`
- `agentic/state.go:87` — `RunID string `json:"run_id,omitempty"``
- `agentic/state.go:90` — `Depth    int `json:"depth,omitempty"`     // Current depth in agent tree (0 = root)`
- `agentic/state.go:91` — `MaxDepth int `json:"max_depth,omitempty"` // Maximum allowed depth for spawned agents`
- `agentic/state.go:94` — `CancelledBy string    `json:"cancelled_by,omitempty"` // User who cancelled the loop`
- `agentic/state.go:95` — `CancelledAt time.Time `json:"cancelled_at,omitempty"` // When the loop was cancelled`
- `agentic/state.go:105` — `PendingApproval     *PendingApprovalState `json:"pending_approval,omitempty"``
- `agentic/state.go:106` — `StateBeforeApproval LoopState             `json:"state_before_approval,omitempty"``
- `agentic/state.go:125` — `PendingContinuation bool `json:"pending_continuation,omitempty"``
- `agentic/state.go:141` — `PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"``
- `agentic/state.go:144` — `UserID      string `json:"user_id,omitempty"`      // User who initiated the loop`
- `agentic/state.go:145` — `ChannelType string `json:"channel_type,omitempty"` // cli, slack, discord, web`
- `agentic/state.go:146` — `ChannelID   string `json:"channel_id,omitempty"`   // Channel/session ID for routing responses`
- `agentic/state.go:149` — `WorkflowSlug string `json:"workflow_slug,omitempty"` // e.g., "add-user-auth"`
- `agentic/state.go:150` — `WorkflowStep string `json:"workflow_step,omitempty"` // e.g., "design"`
- `agentic/state.go:154` — `Outcome     string    `json:"outcome,omitempty"`      // success, failed, cancelled`
- `agentic/state.go:155` — `Result      string    `json:"result,omitempty"`       // LLM response content`
- `agentic/state.go:156` — `Error       string    `json:"error,omitempty"`        // Error message on failure`
- `agentic/state.go:157` — `CompletedAt time.Time `json:"completed_at,omitempty"` // When the loop completed`
- `agentic/state.go:160` — `Metadata map[string]any `json:"metadata,omitempty"``

28 exported fields. Exported methods on `*LoopEntity` (also Tier 1 surface):

- `agentic/state.go:164` — `func (e *LoopEntity) Validate() error {`
- `agentic/state.go:199` — `func (e *LoopEntity) TransitionTo(newState LoopState) error {`
- `agentic/state.go:243` — `func (e *LoopEntity) BeginAwaitingApproval(callID, toolName string, arguments map[string]any, reason string, timeout time.Duration, traceID string) error {`
- `agentic/state.go:274` — `func (e *LoopEntity) ResolveApproval() error {`
- `agentic/state.go:307` — `func (e *LoopEntity) IncrementIteration() error {`

`scripts/api-compat.sh` runs `apidiff` per Tier 1 package (`release/tier1-packages.txt`) between a base version and
HEAD and classifies each package's result into one of: REMOVED (loads at base, not at HEAD — "an adopter cannot
compile the import"), ADDED (loads at HEAD only — a brand-new Tier 1 package, "compatible here"), or by `apidiff`'s
own verdict on packages present at both ends:

- `scripts/api-compat.sh:149` — `if printf '%s' "$out" | grep -q '^Incompatible changes:'; then`

A package whose `apidiff` output contains the literal line `Incompatible changes:` is counted incompatible
(`n_incompatible`); anything else compared is counted clean (`n_clean`). `api-compat.sh` itself makes no
field-level distinction — the addition-vs-break line is entirely `apidiff`'s classification of the exported-API
diff between the two `agentic` package snapshots, not a rule spelled out in this script. This script was not run
end-to-end for this inventory (per the brief: "do not run the whole check").

## Problem shape

The closest existing instance of "restore a per-loop cache from a durable record on rebuild" is Fact 2's mirror:
`restoreLoopFromRequest` (`processor/agentic-loop/state.go:383`) already restores `cachedTools`, `cachedToolChoice`,
`cachedResponseFormat`, `cachedRequestTimeout` (off the retained AgentRequest) and `cachedMetadata` (off the
record) in the same function, on the same rebuild path, that clears the `PendingContinuation` marker with a warning
because it has no field to restore FROM. The shape a durable-turn/durable-prompt field would need to fit is exactly
this one: a value written at the moment the corresponding in-memory cache is populated, read back in this same
function, seated into the in-memory cache the way the other five already are.

## Adjacent claims

- #1365 — agentic-loop: a rebuilt loop recovers its deferred turn text and task prompt (durable-turn field on
  LoopEntity) — one of the two issues this change claims
- #1345 — agentic-loop: task intake still logs and ACKs five failure classes; needs resumable intake, not
  reclassification — the other issue this change claims
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart — parent epic; ruling 3 on
  issuecomment-5828511934 binds #1365+#1345 as one design
- #1377 — agentic-loop: make committed terminal outcomes govern recovery — named "next" after this change in
  `proposal.md`
- #1352 — agentic: bind a trusted input-source reference at loop birth for later roles — explicitly named
  out-of-scope in `proposal.md`'s "What does not change"
- #1330 — the durable-applied-facts (L4a/L4) change whose Q2/Q8 rulings are the historical acceptance this change
  supersedes for beta.163
- #857 — payload-size class: framework writes that scale with data volume — the class the deferred-turn text's size
  bound must be decided under (`proposal.md:58`)
- PR #1387 — feat(agentic-loop): durable accepted input — deferred turn, task prompt, resumable intake (design
  phase) — the claim PR for this change, `Closes #1365` and `Closes #1345`, open, draft
- `openspec list` shows exactly one open change: `agentic-loop-durable-accepted-input` (this one; "(no tasks.md)" —
  design phase)

## Searches

- `git diff --stat 9e5d8455 7c371bfc` → 1 file (`proposal.md`, 61 insertions)
- `git grep -n "PendingContinuation" -- '*.go'` → 66
- `git grep -n "pending_continuation"` (whole repo, no path filter) → 18
- `gopls workspace_symbol -matcher=fuzzy PendingContinuation` → 4 (struct field ×2, method, test-helper method)
- `gopls references agentic/state.go:125:2` (PendingContinuation field) → 13 (7 production, 6 test)
- `gopls references agentic/state.go:141:2` (PendingContinuationRequestID field) → 22 (8 production, 14 test)
- `grep -n "func.*attachContinuation" processor/agentic-loop/*.go` → 1
- `git grep -n "attachContinuation" -- '*.go'` → 14
- `grep -n "func.*Triples" agentic/*.go` → 18 (matches counted with test functions; 5 distinct non-`LoopEntity` types implement it)
- `grep -rn "LoopEntity" schemas/` → 0
- `grep -n "type LoopEntity" agentic/*.go; grep -n "func (e \*LoopEntity)\|func (e LoopEntity)" agentic/*.go` → 1 struct decl + 5 methods
- `grep -rn "LoopEntity" schemas/ specs/` → 0
- `find schemas -iname "*.json" | xargs grep -l "pending_continuation"` → 0
- `git grep -n "taskPrompts\|CacheTaskPrompt\|GetTaskPrompt" -- 'processor/agentic-loop/*.go'` → 20
- `gopls workspace_symbol -matcher=fuzzy taskPrompts` → 2 (field + one unrelated test function name match)
- `gopls references processor/agentic-loop/state.go:90:2` (taskPrompts field) → 6, all pinned above
- `grep -n "type TaskMessage struct" -A 30 processor/agentic-loop/*.go` → 0 (wrong path)
- `git grep -n "TaskMessage struct"` → 2 (`agentic/user_types.go:318`, one ADR doc)
- `grep -n "CacheTools\|CacheToolChoice\|CacheMetadata\|CacheRequestTimeout\|CacheResponseFormat" processor/agentic-loop/state.go` → 8 (declarations + one comment)
- `grep -n "m.cachedTools\[record.ID\]\|m.cachedToolChoice\[record.ID\]\|m.cachedResponseFormat\[record.ID\]\|m.cachedRequestTimeout\[record.ID\]\|m.cachedMetadata\[record.ID\]" processor/agentic-loop/state.go` → 5
- `git grep -n "rememberPendingTaskResult\|pendingTaskResult" -- '*.go'` → 14
- `gopls workspace_symbol -matcher=fuzzy pendingTaskResults` → 1
- `gopls references processor/agentic-loop/component.go:151:2` (pendingTaskResults field) → 6, all pinned above
- `grep -n "func.*handleSpawnIdentityFailure" processor/agentic-loop/*.go` → 1
- `gh issue view 1345 --json ...` → fetched (body quoted for the five-site citation and `9d157232` pin)
- `gh issue view 1365 --json ...` → fetched
- `gh issue view 1377 --json ...` → fetched
- `git cat-file -e 9d157232` → exists
- `git show 9d157232:processor/agentic-loop/component.go | sed -n '1270,1400p'` → read (historical reference only, not a pin)
- `git show 9d157232:processor/agentic-loop/component.go | sed -n '1310,1330p' | cat -n` → read (historical reference only, not a pin)
- `grep -n "func.*handleTaskMessage" processor/agentic-loop/component.go` → 1
- `grep -n "#1365\|#1345" openspec/specs/agentic-loop/spec.md` → 5
- `grep -n "settle-after-durable-effect\|resumable intake\|accepted input\|accepted-input\|Loop input classes settle" openspec/specs/agentic-loop/spec.md` → 4
- `git grep -rn "settle-after-durable-effect" -- openspec/` → 21
- `grep -n "8c.9\|8c\." openspec/changes/archive/2026-09-19-settle-after-durable-effect/tasks.md` → located (8c.1-8c.9, 8d.1-8d.7)
- `grep -n "task intake\|#1345\|exempt" openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md` → 2
- `git grep -n "AGENT_LOOPS" -- 'natsclient/*.go' 'processor/agentic-loop/config.go'` → 1
- `git grep -n "AGENT_LOOPS" -- '*.go'` → 222
- `git grep -n "MaxValueSize\|max_bytes\|MaxBytes" -- 'natsclient/*.go'` → 120
- `git grep -n "func.*bindKVWrite\|KVWritePort{}.*Bucket\|EnsureBucket\|CreateOrUpdateKV\|KeyValueConfig{" -- '*.go'` (excluding `_test.go`) → located `processor/agentic-loop/internal/loopbucket/acquire.go:20`
- `git grep -n "loopbucket.AcquireOwner\|loopsBucket " processor/agentic-loop/*.go` → 8 (2 production, 6 test)
- `gh issue view 857 --json number,title,state,url` → fetched
- `git grep -n "payload-size-chokepoints\|payload_size_chokepoints\|#857" -- 'openspec/*' 'docs/*'` → 60
- `ls /Users/coby/Code/c360/` → 25 entries, 14 non-semstreams git repos checked (see Fact 6)
- `git -C <repo> grep -nE "PendingContinuation|pending_continuation"` in each of 15 sister repos → 0 in every repo
- `git -C <repo> grep -nE "LoopEntity|AGENT_LOOPS"` in each of 15 sister repos → semsource 1, semboids 0, semsage 87,
  semops 0, semdragon 5, semconnect 33, semmem 0, semembed 0, seminstruct 0, semmachina 3, semteams 57, semspec 684,
  semdev 23, servicesim 0 (semsummarize skipped — not a git repo)
- `git -C semsage grep -nE "pending_continuation|PendingContinuation|json:\"pending" agentgraph/*.go processor/ui-api/*.go` → 0
- `git -C semsage grep -n "AGENT_LOOPS" agentgraph/*.go processor/ui-api/*.go` → 17
- `grep -n "json.Unmarshal\|json:\"" processor/ui-api/types.go` (semsage) → 33
- `git -C semconnect grep -lE "LoopEntity|AGENT_LOOPS"` → 11 (all under `openspec/changes/*/evidence/**`)
- `git -C semteams grep -lE "LoopEntity|AGENT_LOOPS"` → 20; filtered to non-`.md` code files → 14
- `git -C semteams grep -nE "pending_continuation|PendingContinuation|task_prompt|TaskPrompt" schemas/agentic-loop.v1.json cmd/semteams/main.go cmd/semteams/tools/emitdevviatestmeasurement/executor.go cmd/semteams/tools/projectspecplan/executor.go cmd/semteams/runanchor/runanchor_test.go` → 0
- `git -C semdragon grep -lE "LoopEntity|AGENT_LOOPS"` → 5; `git -C semmachina grep -lE "LoopEntity|AGENT_LOOPS"` → 3; `git -C semsource grep -nE "LoopEntity|AGENT_LOOPS"` → 1
- `grep -n "LoopEntity\|AGENT_LOOPS\|pending_continuation\|PendingContinuation" semmachina/internal/resume/pending.go semmachina/internal/resume/doc.go` → 2
- `git -C semdev grep -lE "LoopEntity|AGENT_LOOPS"` → 5 (filtered, non-`.md`); `grep -n "LoopEntity\|AGENT_LOOPS\|pending_continuation" semdev/internal/tools/checkfloors/checkfloors.go` → 0
- `git -C semspec grep -lE "LoopEntity|AGENT_LOOPS"` filtered to non-evidence/non-`.md` → 12 (mostly `agentgraph/*` and `docs/adr/*`)
- `git -C semspec grep -nE "pending_continuation|PendingContinuation|task_prompt|TaskPrompt" agentgraph/*.go cmd/semspec/watch_live.go` → 0
- `grep -n "json:\"" semspec/agentgraph/entities.go` → 0 (no struct tags in that file — it is ID-formatting helpers only)
- `which gopls; gopls version` → `v0.20.0`, present
- `grep -n "^agentic$\|/agentic$" release/tier1-packages.txt` → 1 (`:39`)
- `grep -n "agentic\|Tier 1\|tier1\|breaking\|addition" scripts/api-compat.sh` → located the `apidiff`/REMOVED/ADDED/Incompatible classification (`:149`, `:169`, `:186-192`)
- `task openspec:queue` → 1 change in flight (`agentic-loop-durable-accepted-input`, no tasks.md)
- `gh pr view 1387 --json number,title,state,body,headRefName,baseRefName` → fetched (the claim PR)
- `gh issue list --search "durable accepted input" --state open --json number,title` → 7 hits (#1377, #1345, #1146, #1365, #1140, #1352, #1369)

### NOT RUN (named for a follow-up pass, not a conclusion)

- `git grep -n "agent\.task"` was not run individually against every sister repo to confirm none decode the
  task-intake wire subject or its ACK/Retry/Quarantine disposition; the `LoopEntity`/`AGENT_LOOPS` sweep above is
  the evidence in hand for the record side, but the wire-subject side of Fact 3's sister-visibility question is
  unverified.
- `gopls implementation` was not run — `LoopEntity` is a struct with no interface to implement here (Fact 1
  established it implements no `Graphable` interface via absence of `Triples`/`EntityID`, checked by `git grep`
  instead).
- The archived `2026-09-23-agentic-loop-durable-applied-facts` and `2026-09-24-agentic-loop-restart-l4b` change
  directories were read for cross-reference (via the earlier `openspec/changes/archive/.../inventory.md` example)
  but not independently re-swept line-by-line for this inventory; `doc.go:295`'s comment is treated as the current,
  authoritative statement of the taskPrompts limitation rather than re-deriving it from those archives.
- A live measurement of a marshaled `LoopEntity`'s byte size (Fact 5) was not run — this requires a running test or
  a live bucket read, out of scope for a static inventory pass.
