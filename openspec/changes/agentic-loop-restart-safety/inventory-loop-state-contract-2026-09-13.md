# Inventory: bounded LoopEntity state contract and adopter seams

base: 5e0e2259aa7392f7f3255d7f01533869862d8174

## Evidence boundary

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
Frozen parent: `417beae5552f8f15ad3540edd7d8504c87174c13`.
Starting enumeration: `/private/tmp/gh1146-state-model-check.6QRvVj/inventory.md`, fully read; 117 pins and 42 recorded
searches. Its main-side pins refer to `8e41e46f2dfd757e9350fe27f2004b00c40388b4`, not this branch.
Root reports main's subsequent `bb08a2f` change is documentation/contracts only. This inventory does not substitute
main for the branch runtime baseline.

`agentic/state.go`, loop `state.go`, loop `component.go`, lifecycle transitions and AgentRun source are committed
branch evidence. Loop `handlers.go`, `settlement_recovery.go`, `approval_response_handler.go`, `agentic/events.go`
and active change documents include held WIP. Historical artifacts in the active change were read as historical
evidence. Their old task IDs and superseded designs are not active scope.
This document supplements the existing raw enumeration; it is not an inventory of all framework state machines.

Materialized from the architect's complete inventory-only handoff. Root normalized prose bullets to paragraphs and
the Searches/Adjacent claims headings for the pin verifier, and made sister-file paths absolute for this worktree.
These are locator/format changes, not a target-state decision. Independent inventory review remains pending.

WIP source fingerprints:

| File | SHA-256 |
|---|---|
| `agentic/state.go` | `b80c4b07f254a015eed53874c0e3c13f472f25c827078039013ab7e5ffe76c7d` |
| `pkg/lifecycle/transitions.go` | `22665d65cad450a5b0e5b139c4fdb8c9951b9fd277bf63004afb7b59fd3276c1` |
| `processor/agentic-loop/state.go` | `54b0747124434d259b4fceab06077f21ea7d637e8115e61fec76d524bf178a9e` |
| `processor/agentic-loop/component.go` | `389ec626f135a470419fa4496a864554fdebc52d6629af34a415633d3ed9e248` |
| `processor/agentic-loop/handlers.go` | `cfed1f89704adcaf4cc934a7529b2668a9d1d1c75d1679bfb25cc4b6b26a556e` |
| `processor/agentic-loop/settlement_recovery.go` | `ad135fa1606f103ed36b07927fcd5b53aa2686f8e5b1393ec7611a58404d414d` |
| `processor/agentic-loop/approval_response_handler.go` | `7850ba07b8cbe3ece0c70301c4d5b7627f234f7b59a05d3e82f4f29333c5020e` |
| `agentic/events.go` | `92faee8b22c62a66910d01c3116c8b01b913b5c6523f83582727ccd2a92bc973` |

## 1. Existing vocabulary and admission rules disagree

- `agentic/state.go:12` — `type LoopState string`
- `agentic/state.go:18` — `LoopStateExploring    LoopState = "exploring"`
- `agentic/state.go:19` — `LoopStatePlanning     LoopState = "planning"`
- `agentic/state.go:20` — `LoopStateArchitecting LoopState = "architecting"`
- `agentic/state.go:21` — `LoopStateExecuting    LoopState = "executing"`
- `agentic/state.go:22` — `LoopStateReviewing    LoopState = "reviewing"`
- `agentic/state.go:25` — `LoopStateComplete  LoopState = "complete"`
- `agentic/state.go:26` — `LoopStateFailed    LoopState = "failed"`
- `agentic/state.go:27` — `LoopStateCancelled LoopState = "cancelled"`
- `agentic/state.go:32` — `LoopStatePaused           LoopState = "paused"`
- `agentic/state.go:33` — `LoopStateAwaitingApproval LoopState = "awaiting_approval"`
- `agentic/state.go:42` — `func (s LoopState) IsTerminal() bool {`
- `agentic/state.go:105` — `if !isValidLoopState(e.State) {`
- `agentic/state.go:117` — `case LoopStateExploring, LoopStatePlanning, LoopStateArchitecting,`
- `agentic/state.go:118` — `LoopStateExecuting, LoopStateReviewing, LoopStateComplete,`
- `agentic/state.go:128` — `func (e *LoopEntity) TransitionTo(newState LoopState) error {`
- `agentic/state.go:130` — `if e.State == newState {`
- `agentic/state.go:134` — `if e.State.IsTerminal() {`
- `agentic/state.go:137` — `e.State = newState`

Measured behavior:

| Boundary | Current rule |
|---|---|
| `LoopState.IsTerminal` | Only complete, failed and cancelled are terminal; an unknown string is not terminal. |
| `LoopEntity.Validate` | Checks nonempty ID, membership in nine admitted states and positive MaxIterations. Paused is rejected. It does not validate approval-field coherence. |
| `TransitionTo` | Same state succeeds first, including unknown/retired same-state values. A different transition from a terminal state fails. Any target from another state is otherwise assigned without vocabulary or edge validation. |
| JSON decoding | String-backed states decode independently of `Validate`; successful decoding does not establish valid state. |

The five developer-phase constants have no measured production behavioral branches distinguishing them. Their
current non-declaration uses are the validation list, the ordinary birth seed, approval fallback and the separate
research seed. Public examples still teach phase transitions.

- `agentic/state.go:207` — `restore = LoopStateExecuting`
- `agentic/state.go:245` — `State:         LoopStateExploring,`
- `frameworkcapabilities/graphresearch/executor.go:249` — `loopEntity.State = agentic.LoopStateExecuting`
- `agentic/README.md:127` — `entity.TransitionTo(agentic.LoopStatePlanning)`
- `agentic/README.md:128` — `entity.TransitionTo(agentic.LoopStateExecuting)`
- `agentic/doc.go:117` — `//	entity.TransitionTo(agentic.LoopStatePlanning)`
- `agentic/doc.go:118` — `//	entity.TransitionTo(agentic.LoopStateExecuting)`
- `processor/agentic-loop/doc.go:147` — `//	err = manager.TransitionLoop(loopID, agentic.LoopStateExecuting)`

The research seed is an existing consumer, not permission to reopen research #1288 or change that ownership.

## 2. Existing transition, assignment and restoration boundaries

### Ordinary mutation and direct approval mutation

- `processor/agentic-loop/state.go:844` — `func (m *LoopManager) TransitionLoop(loopID string, newState agentic.LoopState) error {`
- `processor/agentic-loop/state.go:853` — `return entity.TransitionTo(newState)`
- `agentic/state.go:164` — `func (e *LoopEntity) BeginAwaitingApproval(callID, toolName string, arguments map[string]any, reason string, timeout time.Duration, traceID string) error {`
- `agentic/state.go:177` — `e.StateBeforeApproval = e.State`
- `agentic/state.go:178` — `e.State = LoopStateAwaitingApproval`
- `agentic/state.go:195` — `func (e *LoopEntity) ResolveApproval() error {`
- `agentic/state.go:209` — `e.State = restore`
- `agentic/state.go:210` — `e.StateBeforeApproval = ""`
- `agentic/state.go:211` — `e.PendingApproval = nil`
- `processor/agentic-loop/state.go:650` — `if err := entity.ResolveApproval(); err != nil {`
- `processor/agentic-loop/state.go:1468` — `entity.State = agentic.LoopStateCancelled`

`BeginAwaitingApproval` refuses terminal state, another pending CallID, empty CallID and empty tool name. It directly
saves the prior state and assigns awaiting approval. `ResolveApproval` requires awaiting approval and a pending
record, then directly restores `StateBeforeApproval`; empty or awaiting-approval prior state falls back to executing.
Other prior-state strings are not independently validated at that restoration.

`CancelLoop` holds the manager mutex, refuses already-terminal state and directly sets cancellation state, outcome,
actor, timestamps and error. It does not call `TransitionTo`.

### Production transition callers

These are the measured production `TransitionLoop` call sites; result-state assignments nearby are separate
`HandlerResult` fields, not additional LoopEntity owners.

| Site | Existing request |
|---|---|
| `processor/agentic-loop/component.go:1611` | failed |
| `processor/agentic-loop/handlers.go:1216` | failed, timeout; transition error discarded |
| `processor/agentic-loop/handlers.go:2038` | failed |
| `processor/agentic-loop/handlers.go:2158` | complete |
| `processor/agentic-loop/handlers.go:2303` | failed; transition error discarded |
| `processor/agentic-loop/handlers.go:2569` | failed |
| `processor/agentic-loop/settlement_recovery.go:822` | failed after retained-absence approval recovery |

Additional direct boundaries are the NewLoopEntity birth seed, research seed, BeginAwaitingApproval,
ResolveApproval and CancelLoop.

### Whole-record replacement and restoration

- `processor/agentic-loop/state.go:348` — `func (m *LoopManager) restoreLoopFromRequest(entity agentic.LoopEntity, request agentic.AgentRequest, batch *agentic.ChatMessage) error {`
- `processor/agentic-loop/state.go:394` — `restored := entity`
- `processor/agentic-loop/state.go:395` — `m.loops[entity.ID] = &restored`
- `processor/agentic-loop/state.go:519` — `func (m *LoopManager) UpdateLoop(entity agentic.LoopEntity) error {`
- `processor/agentic-loop/state.go:527` — `m.loops[entity.ID] = &entity`
- `processor/agentic-loop/settlement_recovery.go:816` — `if err := entity.ResolveApproval(); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:822` — `if err := c.handler.loopManager.TransitionLoop(entity.ID, agentic.LoopStateFailed); err != nil {`
- `processor/agentic-loop/component.go:2257` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`

`restoreLoopFromRequest` validates its supplied entity and request, checks their identity/role/model agreement,
then reconstructs process context and routes. When a current process entity exists, its comparison covers
TaskID/role/model, not its state or a durable revision. It can replace the whole current entity.

`UpdateLoop` requires an existing process entry but does not validate the supplied replacement or compare
state/revision. `GetLoop` returns a value copy, not a deep immutable snapshot.

Consequently, the existing absorbing terminal guard is not a guard over every mutation boundary. A stale approval
snapshot can replace newer process state, undergo an individually permitted failure transition and reach
unconditional durable Put. This is the boundary exercised by the held R2 cancellation evidence; it is not evidence
that the five names themselves caused the failure.

## 3. Existing small operational checks already carry behavior

The state field is not the only place operational distinctions live.

- `processor/agentic-loop/state.go:292` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/state.go:302` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/state.go:319` — `if entity.TaskID == taskID && !entity.State.IsTerminal() {`
- `processor/agentic-loop/state.go:561` — `if loop.State != agentic.LoopStateAwaitingApproval || loop.PendingApproval == nil {`
- `processor/agentic-loop/handlers.go:1234` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:527` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:530` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-dispatch/http.go:784` — `} else if persisted.State != agentic.LoopStateAwaitingApproval && persisted.PendingApproval != nil {`
- `processor/agentic-dispatch/http.go:786` — `} else if persisted.State == agentic.LoopStateAwaitingApproval &&`
- `processor/agentic-dispatch/http.go:801` — `if persisted.State != agentic.LoopStateAwaitingApproval {`

Measured operation-specific checks:

| Operation | Existing relevant checks/carriers |
|---|---|
| Create | Canonical token form, then process already-exists refusal before registration. |
| Continuation attachment | Existing loop, preserved context, terminal refusal, pending-tool refusal, awaiting-approval refusal; TaskID is rebound on acceptance. |
| Model response | Request correlation, terminal guard, timeout and iteration budget; outstanding model work is not represented by one of the five named phases. |
| Tool result | RequestID, ExecutionID, ordinal, provider CallID, name, ordered batch and accumulated result partition. |
| Approval | Awaiting state plus current PendingApproval identity; StateBeforeApproval carries restoration state. |
| Approval timeout | Awaiting state, nonnil pending record, RequestedAt and timeout. |
| Dispatch admission | Exact current record; terminal classification comes from `State.IsTerminal`, not a second transition table. |
| Terminal settlement | Separate state, outcome/event payload, required effects and source-specific application evidence. |

`processor/agentic-dispatch/loop_admission.go:143` is a typed `loopFacts.State` field, not a duplicate state validator.

## 4. Durable authority and input completion remain distinct from state edges

- `processor/agentic-loop/settlement_recovery.go:123` — `func (c *Component) readLoopEntityRevision(ctx context.Context, loopID string) (agentic.LoopEntity, uint64, error) {`
- `processor/agentic-loop/settlement_recovery.go:140` — `if err := entity.Validate(); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:681` — `// proveTerminalToolResultApplied uses the final marker only with the exact`
- `processor/agentic-loop/settlement_recovery.go:682` — `// retained execution and its direct tool-result terminal consequence.`
- `processor/agentic-loop/component.go:2242` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:2257` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`

Current mechanics include exact AGENT_LOOPS reads and exact retained request/response reads, with no ordinary
full-stream recovery scan. Revision-conditioned persistence exists at new approval gate and resolved approval
boundaries. Ordinary terminal COMPLETE writes still use Put in the held runtime baseline.

Completion/failure `persistHandlerResult` performs selected result work, required synthetic work when present,
output publication and then final loop persistence; cancellation still has its separately inventoried ordering.
A terminal model-response proof uses matching request evidence. Tool-result proofs require exact execution/batch
consequence. Current approval applicability has its own effect-free nonmatching-execution boundary.
Warm process correlation and cold durable reconstruction are separate paths. A business state does not by itself
determine whether effects are absent, complete or uncertain, hence does not determine Ack/Retry/Quarantine.

The active delta already specifies the distinction:

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:177` — `Agentic-loop SHALL load the exact `LoopEntity` identified by an incoming delivery and reconstruct only the material`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:221` — `For ordinary agentic-loop success, failure and cancellation, `COMPLETE_<LoopID>` SHALL select one terminal`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:227` — ` `LoopCompletedEvent.SyntheticDecideRequired` SHALL record whether the existing completion builder computed a`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:234` — `For every terminal `LoopEntity` transition, the bare `AGENT_LOOPS/<LoopID>` terminal write SHALL be the final`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:243` — `Required effects MAY repeat compatibly with the selected outcome. A changed authority revision SHALL NOT be`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:246` — `Already-cancelled durable authority SHALL NOT be regressed because its COMPLETE_ record is absent.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:249` — `terminality nor selected-record existence is generic tool-execution or source-applied proof.`

A table is not evidence that terminal selection, required synthetic action, PubAck, final-marker ordering or exact
delivered-source correlation has completed.

## 5. Reusable lifecycle validation versus AgentRun

### Pure validation surface

- `pkg/lifecycle/transitions.go:28` — `type Transitions map[string][]string`
- `pkg/lifecycle/transitions.go:46` — `func (t Transitions) Validate() error {`
- `pkg/lifecycle/transitions.go:87` — `func (t Transitions) IsTerminal(phase string) bool {`
- `pkg/lifecycle/transitions.go:89` — `if !declared {`
- `pkg/lifecycle/transitions.go:90` — `return true`
- `pkg/lifecycle/transitions.go:99` — `func (t Transitions) IsValidTransition(from, to string) bool {`
- `pkg/lifecycle/transitions.go:104` — `return slices.Contains(outEdges, to)`
- `pkg/lifecycle/transitions.go:110` — `func (t Transitions) Phases() []string {`
- `pkg/lifecycle/transitions.go:123` — `func (t Transitions) TerminalPhases() []string {`

This file imports only `fmt`, `slices` and `sort`; it performs no I/O, scheduling or context ownership.
`Validate` checks nonempty table/keys/targets, duplicate edges and target membership. It does not check initial-state
reachability or prohibit cycles. A self-transition is valid only when declared. Unknown states are terminal to its
`IsTerminal`, unlike existing `LoopState.IsTerminal`.

### Measured import cost

`go list -e -deps` reported no package errors and no `agentic` dependency in the lifecycle closure. Adding an
`agentic → pkg/lifecycle` import is cycle-free in the measured graph; no hypothetical source change was compiled.

Already shared with agentic:
`pkg/retry`, `pkg/errs`, `pkg/types`, `pkg/platform`, `vocabulary`, `pkg/projection/contract`, `payloadregistry`,
`pkg/timestamp`, `message`.

Additional in-module packages brought by lifecycle:
`pkg/security`, `pkg/acme`, `pkg/tlsutil`, `metric`, `pkg/cache`, `pkg/resource`, `natsclient`, `graph`,
`internal/graphmutation`, `pkg/projection`, and `pkg/lifecycle` itself.

Thus reuse is possible but not dependency-neutral. No extraction or adoption decision is made here.

### AgentRun is a different entity and authority

- `agentic/agentrun/agentrun.go:3` — `// An AgentRun represents the framework-level entity for a nested agentic loop`
- `agentic/agentrun/agentrun.go:4` — `// tree: a coordinator loop that spawns research/architect/builder child loops`
- `agentic/agentrun/agentrun.go:53` — `var agentRunTransitions = lifecycle.Transitions{`
- `agentic/agentrun/agentrun.go:54` — `"dispatched":        {"executing", "failed", "cancelled"},`
- `agentic/agentrun/agentrun.go:55` — `"executing":         {"awaiting_approval", "completed", "failed", "cancelled"},`
- `agentic/agentrun/agentrun.go:56` — `"awaiting_approval": {"executing", "cancelled"},`
- `agentic/agentrun/agentrun.go:65` — `PhasePredicate = "agent.run.phase"`
- `agentic/agentrun/agentrun.go:166` — `func (r *AgentRun) IsTerminal() bool { return agentRunTransitions.IsTerminal(r.PhaseField) }`
- `agentic/agentrun/agentrun.go:175` — `// The bare RunID == the dispatch-root loop UUID; it is NOT stored as a triple`
- `agentic/agentrun/agentrun.go:210` — `Transitions:    agentRunTransitions,`
- `agentic/state.go:61` — `RunID string `json:"run_id,omitempty"``
- `processor/agentic-loop/handlers.go:485` — `// Set run anchor if provided (ADR-053 D7). Inherited from firing entity's RunID.`
- `processor/agentic-loop/handlers.go:487` — `if err := h.loopManager.SetRunID(loopID, task.RunID); err != nil {`
- `pkg/lifecycle/manager.go:27` — `// Manager is the schema-and-discipline layer over ENTITY_STATES`

AgentRun's graph-backed run/chain phase can span child loops. It is neither one audit record per loop nor the current
LoopEntity authority. Its table uses `completed`, while LoopEntity uses `complete`; its awaiting-approval edges are
its own contract. Copying the AgentRun vocabulary/table is not equivalent to reusing the pure validator.

## 6. Same-class collision inventory

| Dimension | Existing surfaces and boundaries |
|---|---|
| Semantic class | Current loop state/transition admission; operation-specific approval/batch phase; run-level lifecycle; input settlement are adjacent but distinct facts. |
| Current owner | Agentic-loop owns ordinary LoopEntity mechanics and AGENT_LOOPS current records. Research has an existing separate seed/write seam, outside this task. |
| Catalog | Exported LoopState/LoopEntity; public methods and examples; dispatch Loop wire DTO/OpenAPI text; existing lifecycle.Transitions and AgentRun declaration. |
| Status | LoopEntity.State; approval PendingApproval/StateBeforeApproval; terminal Outcome and event category; process pending/queued/result partition; AgentRun.PhaseField. |
| Lifecycle | Process registration and restoration versus durable current KV; terminal release removes process state; graph-backed AgentRun participation is separate. |
| Readers | Loop handlers/recovery/sweeper; dispatch admission, approval and HTTP/SSE projection; external UI/readers; run subscribers consume terminal events. |
| Writers | Birth, TransitionTo/TransitionLoop, direct approval assignments, direct cancellation, whole-record UpdateLoop/restore and KV persistence; separate run Manager transitions. |
| Recovery | Current AGENT_LOOPS plus exact lane evidence; no graph-based second loop recovery authority. Trajectory remains observed-only audit. |
| Coordination | Manager mutex and native per-input durable owners already exist. No new runtime, scheduler, supervisor, bucket or ledger is inventoried as necessary. |

## 7. Adopter seam inventory

### Specific adopter A: external Go component using the advertised state API

What they know today: LoopState is an exported string type; README and Go documentation demonstrate
planning/executing transitions. A caller may also construct or unmarshal LoopEntity and call Validate or IsTerminal
separately.

What happens if they do nothing: under current code, paused decoding succeeds but validation fails; unknown
IsTerminal is false. Changing/removing exported constants can cause compile failures; tightening TransitionTo can
change runtime results without compile failures. Changing unknown terminal semantics can alter downstream behavior
even without changing JSON shape.

Where they find out:

- `agentic/README.md:48` — `| `LoopState` | Loop lifecycle state (exploring, planning, executing, etc.) |`
- `agentic/README.md:127` — `entity.TransitionTo(agentic.LoopStatePlanning)`
- `docs/operations/migration-beta162-to-beta163.md:994` — `**Current-record validation.** `LoopEntity.Validate()` now rejects `state: "paused"` for every caller, including`
- `docs/operations/migration-beta162-to-beta163.md:995` — `dispatch's exact reads and shared view. `LoopState` is a string type, so JSON decoding alone can still succeed;`

What they should have to know: the advertised operational meaning and actual failure contract of an API they
deliberately call—not infer internal execution stage, durable completion or a revision from a phase label. The
concrete future migration remains undecided.

Structural references found only one production caller of `LoopEntity.TransitionTo`, the loop manager wrapper,
plus its state test. That is an in-repo result, not proof of no external API users.

### Specific adopter B: SemTeams UI developer consuming /loops and /activity

Framework projection:

- `processor/agentic-dispatch/loop_wire.go:14` — `// Loop is the canonical wire contract for both the /loops and /activity endpoints.`
- `processor/agentic-dispatch/loop_wire.go:21` — `State         string `json:"state,omitempty"``
- `processor/agentic-dispatch/loop_wire.go:88` — `State:           e.State.String(),`
- `processor/agentic-dispatch/loop_info.go:59` — `ChannelType: e.ChannelType, ChannelID: e.ChannelID, State: e.State.String(),`
- `processor/agentic-dispatch/http.go:1086` — `{Name: "state", In: "query", Description: "Filter by loop state (exploring, planning, architecting, executing, awaiting_approval, reviewing, complete, failed, cancelled)"},`

SemTeams HEAD `ce22c961d30014c463a09f8f8a2a90044ee1a1cf`:

- `/Users/coby/Code/c360/semteams/ui/src/lib/types/agent.ts:1` — `export type AgentLoopState =`
- `/Users/coby/Code/c360/semteams/ui/src/lib/types/agent.ts:20` — `export type ActiveLoopState =`
- `/Users/coby/Code/c360/semteams/ui/src/lib/types/agent.ts:27` — `export function isActiveState(state: AgentLoopState): state is ActiveLoopState {`
- `/Users/coby/Code/c360/semteams/ui/src/lib/types/agent.ts:135` — `state: (w.state as AgentLoopState) ?? "exploring",`
- `/Users/coby/Code/c360/semteams/ui/src/lib/types/task.ts:81` — `export function loopStateToColumn(state: AgentLoopState): TaskColumn {`
- `/Users/coby/Code/c360/semteams/ui/src/lib/types/task.ts:107` — `"[loopStateToColumn] unknown state, defaulting to thinking",`

What they know today: five active labels are enumerated; exploring/planning/architecting map to thinking,
executing/reviewing to executing, awaiting_approval/paused to needs_you. The union also carries terminal aliases
explained by an older upstream projection.

What happens if they do nothing: an unfamiliar nonempty wire state survives the type assertion, fails
`isActiveState`, and reaches the kanban warning/default rather than a compile-time failure. Merely retaining string
JSON shape does not preserve UI behavior.

Where they find out: current OpenAPI description, framework wire shape, their manually maintained mirror, and
SemStreams migration notes. No generated cross-repo state contract was established by this inventory.

What they should have to know: the operational state exposed for rendering, not predict internal phase progression.
No future remapping or compatibility aliases are selected here.

### Specific adopter C: SemSpec completion/recovery component developer

SemSpec HEAD `5a9496eecc453747f4bc557b95444db6304c1420`:

- `/Users/coby/Code/c360/semspec/processor/execution-bridge/completion.go:68` — `var loop agentic.LoopEntity`
- `/Users/coby/Code/c360/semspec/processor/execution-bridge/completion.go:72` — `if !loop.State.IsTerminal() {`
- `/Users/coby/Code/c360/semspec/processor/execution-bridge/completion.go:92` — `if loop.State != agentic.LoopStateComplete {`
- `/Users/coby/Code/c360/semspec/processor/recovery-consumer/backstop.go:300` — `func classifyLoopLive(loop agentic.LoopEntity, now time.Time, margin time.Duration) bool {`
- `/Users/coby/Code/c360/semspec/processor/recovery-consumer/backstop.go:301` — `if loop.State.IsTerminal() {`

What they know today: terminality gates completion translation; complete is success and other terminal states
become failure. The backstop combines terminality with deadline/age logic.

What happens if they do nothing: a change to IsTerminal's unknown-state semantics changes both completion
translation and live-loop classification. Removing or renaming Complete also has a source seam. Their current JSON
decoding does not call LoopEntity.Validate before these branches.

Where they find out: exported Go API and SemStreams migration notes. Any necessary migration is SemStreams-owned
documentation; the SemSpec owner implements and validates it.

What they should have to know: whether a valid record represents terminal work, without interpreting internal
developer-workflow labels. This inventory does not approve their recovery policy or change it.

### Other measured external reach and coverage limit

SemSage HEAD `4d28b4dc1210f47da84a3031125167d164de9290` has typed LoopEntity reads and a string-state JSON mirror:

- `/Users/coby/Code/c360/semsage/processor/ui-api/types.go:12` — `// Fields mirror agentic.LoopEntity with UI-friendly additions.`
- `/Users/coby/Code/c360/semsage/processor/ui-api/types.go:16` — `State         string     `json:"state"``

A read-only exact-namespace Go sweep covered the main checkouts of SemDev, SemTeams, SemSpec, SemMachina, SemSage,
SemDragon, SemOps, SemSource, SemConnect, SemBoids, SemEmbed, SemLink and SemMem. It found typed production
LoopEntity/LoopState users in SemSpec and SemSage and no external TransitionTo/TransitionLoop calls with those
spellings.

This is not alias-aware structural coverage of every sister repository/worktree or external adopter. The broad
initial UI/string locator output was truncated and is not used as completeness evidence. The concrete
SemTeams/SemSpec/SemSage files above were then read directly. No sister repository was modified.

## Adjacent claims

Current specs and ADRs read: agentic-loop, agentic-terminal-events, lifecycle; ADR-049 and ADR-053. Relevant distinctions:

- Current loop spec separates consumer in-flight measurement from persisted loop state: stale state alone is not proof of live work.
- Existing continuation spec distinguishes terminal and busy refusal and preserves conversation; current #1146 work supersedes older quiet-drop assumptions with lane-specific durable evidence.
- Trajectory is observed-only audit, not response-applied or current loop authority.
- Lifecycle Manager is graph-backed ENTITY_STATES discipline; raw operational LoopEntity remains AGENT_LOOPS-backed.
- AgentRun run/child semantics are ADR-053, not a one-loop lifecycle replacement.

Issue #1244 was read directly. Its body's proposed shape is not a ruling. The owner seam agreement at
`https://github.com/C360Studio/semstreams/issues/1244#issuecomment-5509501362` composes two obligations: #1146
durability/settlement and #1244 declared transitions/refusals. It does not approve a concrete LoopEntity vocabulary,
import choice or table.

Existing task groups—not new families—contain the relevant work:

- `openspec/changes/agentic-loop-restart-safety/tasks.md:74` — `- [ ] R2 Complete approval replacement/replay evidence and its remaining corrections (old 6.1, 6.1a, 6.2, 6.3, 6.5,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:139` — `- [ ] R4 Close loop task/response and remaining no-premature-ACK proof (old C.4, 4.1–4.3). Account for birth/lineage,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:142` — `Keep declared transition/refusal exits for #1244, no process-absence success, and no speculative terminal cache.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:152` — `- [ ] R6 Finish cancel/approval/verdict fast lanes and retired vocabulary (old 7.2–7.8). Remove the remaining paused`

R2 covers the present stale-approval/cancellation overwrite evidence. R4 covers task/response exit and
premature-ACK obligations. R6 already owns retired vocabulary and fast lanes. R3's approval Store ruling remains
separate; #1288 and #1249 remain outside this task. R2–R15 are not closed by this inventory.

The independent causal advisory at `/private/tmp/gh1146-state-model-check.6QRvVj/causal-judgment.md` was read. It is
not an owner ruling. V4 remains unapplied and held; no inference that table adoption eliminates all terminal
arbitration is made.

## Searches

Starting raw enumeration retains its 42 searches. Bounded supplemental searches included:

```text
gopls references agentic/state.go:128:22
gopls references agentic/state.go:12:6
gopls references agentic/state.go:50:2

go list -e -f '{{.ImportPath}} {{join .Imports " "}} ERROR={{if .Error}}{{.Error.Err}}{{end}}' \
  ./pkg/lifecycle ./internal/graphmutation ./graph ./natsclient ./message \
  ./payloadregistry ./pkg/errs ./pkg/projection ./pkg/types ./vocabulary

go list -e -deps -f \
  '{{if .Module}}{{if eq .Module.Path "github.com/c360studio/semstreams"}}{{.ImportPath}}{{if .Error}} ERROR={{.Error.Err}}{{end}}{{end}}{{end}}' \
  ./pkg/lifecycle
# Same command for ./agentic.

git grep -n -E \
  '(\.State[[:space:]]*=|State:[[:space:]]*agentic.LoopState|TransitionTo\(|TransitionLoop\(|BeginAwaitingApproval\(|ResolveApproval\()' \
  -- agentic processor frameworkcapabilities gateway service internal ':!*_test.go'

git grep -n -E \
  'LoopStateExploring|LoopStatePlanning|LoopStateArchitecting|LoopStateExecuting|LoopStateReviewing|TransitionLoop\(|BeginAwaitingApproval\(|ResolveApproval\(' \
  -- '*.go' ':!*_test.go'

git grep -n -E \
  'LoopState|TransitionTo\(|TransitionLoop\(|StateBeforeApproval|state_before_approval|awaiting_approval|architecting|reviewing|exploring' \
  -- '*.go' '*.ts' '*.svelte' '*.json' ':!*_test.go' ':!*lock*'
# Sister locator only; truncated output was not completeness evidence.

git rev-parse HEAD
git grep -n -E \
  'agentic\.LoopState|agentic\.LoopEntity|\.TransitionTo\(|\.TransitionLoop\(|StateBeforeApproval' \
  -- '*.go' ':!*_test.go'
# Repeated in the 13 named sister main checkouts; read-only.

git grep -n -E 'LoopState|state|IsTerminal' -- \
  processor/agentic-dispatch/http_activity.go \
  processor/agentic-dispatch/openapi_spec.go \
  processor/agentic-dispatch/loop_admission.go \
  processor/agentic-dispatch/loop_tracker.go

git grep -n -E \
  'agentic.LoopState|TransitionTo|TransitionLoop|state.*(exploring|planning|architecting|executing|reviewing|paused)|LoopState' \
  -- agentic/README.md agentic/doc.go processor/agentic-loop/doc.go \
  docs/operations/migration-beta162-to-beta163.md

git grep -n -E 'R2|R4|R6|#1244|AGENT_LOOPS|LoopEntity' -- \
  openspec/changes/agentic-loop-restart-safety/tasks.md

git grep -n -E 'RunID|run_id|agent.loop.run|AGENT_LOOPS|ENTITY_STATES' -- \
  agentic/agentrun/agentrun.go processor/agentic-loop/handlers.go \
  agentic/state.go pkg/lifecycle/manager.go

rg --files processor/agentic-dispatch | rg 'openapi|loop_(info|wire|view|owner)'
rg -n 'exploring|paused|awaiting_approval' processor/agentic-dispatch --glob '!**/*_test.go'

git grep -n -E 'exploring|planning|architecting|executing|reviewing|paused|stateTo' -- \
  ui/src/lib/types/task.ts ui/src/lib/services/agentChatBridge.ts
# SemTeams.

gh issue view 1244 --repo C360Studio/semstreams \
  --json number,title,body,comments,state

git status --short
shasum -a 256 <the eight fingerprinted source files above>
```

Default-cache gopls initially failed under sandbox permission; the same read-only structural queries succeeded with
approved default-cache access. No alternate cache or module mutation was introduced.

One globbed OpenAPI locator failed before execution because the glob did not exist; subsequent tracked-file/direct
HTTP searches located the actual description. An attempted read of `internal/loopview/current.go` returned
nonexistent and supplies no evidence.

Tests, hypothetical import compilation, proposed table tests, native source-settlement reproduction and migration
validation: NOT RUN by this inventory task. Existing historical/owner test results are not relabeled as new runs.
Draft pin-verifier pass: NOT RUN by architect; independent review remains pending.

## 10. Measured facts versus unresolved facts

Already measured:

1. Vocabulary validation, transition admission and direct restoration currently disagree.
2. The five developer-phase names do not select distinct measured production branches; seeds and public examples exist.
3. Paused remains exported while current validation rejects it and its runtime resume surface is removed.
4. State mutations are not confined to TransitionTo: direct approval/cancel assignments and whole-record replacement/restoration exist.
5. The existing terminal guard does not protect against stale snapshot replacement followed by unconditional durable Put.
6. Approval and tool handling already distinguish operational phases using identity, pending fields and result partitions.
7. AGENT_LOOPS is current loop authority; graph-backed AgentRun is a separate run/chain entity spanning loops.
8. Lifecycle pure validation is cycle-free to import, adds eleven in-module packages and differs on unknown terminality/self-edges.
9. Concrete outward state consumers exist in SemTeams, SemSpec and SemSage.
10. Existing R2/R4/R6 already contain the relevant work boundaries.

Unresolved, bounded review questions:

1. Did the refreshed structural/literal census miss an alias, whole-record writer or restore entry on the named loop surface? Review that census, not every framework lifecycle.
2. Are the draft exact pins valid at the fingerprinted WIP snapshot? Run the inventory pin check.
3. Does the inventory sufficiently expose the behavioral meaning needed for the owner's next vocabulary/edge decision? No enum/table has yet been approved.
4. Which existing same-state, unknown-state and approval-field behaviors are intentional contracts versus defects? This is an owner/design question, not settled by enumeration.
5. What precise migration follows the eventual selected change? Known affected seams are identified; downstream implementation and validation remain their owners' work.
6. Alias-aware sister/worktree coverage and unknown third-party callers remain unverified; no no-adopters claim is made.
7. A table-only change has not been demonstrated to fix cancellation overwrite, pre-birth ordering or warm/cold settlement uncertainty. Those remain separate proof obligations.

Inventory handoff complete; stopping for independent review.
