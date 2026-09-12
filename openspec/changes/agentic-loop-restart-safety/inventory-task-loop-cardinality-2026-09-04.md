# Inventory: TaskID and LoopID identity/cardinality
base: 79b0f29f82ce5391013f6c931fae69a28216ac93

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:154` — `For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:93` — `- [x] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained`
- `processor/agentic-loop/state.go:260` — `//     dedupes on TaskID via HasActiveLoopForTask, so a redelivery of THIS task`
- `processor/agentic-loop/state.go:264` — `//     Residual, known and accepted: the rebind preserves dedup for THIS turn`
- `processor/agentic-loop/state.go:266` — `//     than one turn — that needs a set or a window. So a redelivery of turn`
- `processor/agentic-dispatch/http.go:531` — `// handleListLoops returns all tracked loops with optional filtering.`
- `processor/agentic-dispatch/http.go:532` — `func (c *Component) handleListLoops(w http.ResponseWriter, r *http.Request) {`
- `processor/agentic-dispatch/http.go:549` — `loops = c.loopTracker.GetAllLoops()`

No `ListTasks`, task `GetTask`, task store/index/bucket/registry, `/tasks`, or dedicated task-to-loop mapping API was located under the searched spellings; see zero-hit searches under Searches.

## Spellings of the fact

- `agentic/user_types.go:312` — `// TaskMessage represents a task to be executed by an agentic loop`
- `agentic/user_types.go:313` — `type TaskMessage struct {`
- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id,omitempty"` // loop to continue, or empty for new`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/user_types.go:316` — `SourceMessageID string `json:"source_message_id,omitempty"``
- `agentic/user_types.go:408` — `if t.TaskID == "" {`
- `agentic/user_types.go:409` — `return fmt.Errorf("task_id required")`
- `agentic/user_types.go:531` — `func (t *TaskMessage) Schema() message.Type {`
- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`
- `agentic/state.go:48` — `type LoopEntity struct {`
- `agentic/state.go:49` — `ID                 string                `json:"id"``
- `agentic/state.go:50` — `TaskID             string                `json:"task_id"``
- `agentic/state.go:238` — `func NewLoopEntity(id, taskID, role, model string, maxIterations ...int) LoopEntity {`
- `agentic/state.go:244` — `ID:            id,`
- `agentic/state.go:245` — `TaskID:        taskID,`
- `processor/agentic-loop/state.go:172` — `func (m *LoopManager) CreateLoop(taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:173` — `loopID := m.GenerateLoopID()`
- `processor/agentic-loop/state.go:174` — `return m.CreateLoopWithID(loopID, taskID, role, model, maxIterations...)`
- `processor/agentic-loop/state.go:181` — `return uuid.NewString()`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:223` — `entity := agentic.NewLoopEntity(loopID, taskID, role, model, maxIter)`
- `processor/agentic-loop/state.go:225` — `m.loops[loopID] = &entity`
- `processor/agentic-loop/state.go:240` — `// attachContinuation binds a continuation task to the loop already registered`
- `processor/agentic-loop/state.go:258` — `//   - The loop's task association is rebound to the continuation's task ID.`
- `processor/agentic-loop/state.go:273` — `func (m *LoopManager) attachContinuation(loopID, taskID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/state.go:299` — `entity.TaskID = taskID`
- `processor/agentic-loop/state.go:305` — `func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {`
- `processor/agentic-loop/state.go:309` — `for _, entity := range m.loops {`
- `processor/agentic-loop/state.go:310` — `if entity.TaskID == taskID && !entity.State.IsTerminal() {`
- `processor/agentic-loop/handlers.go:830` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:852` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/handlers.go:856` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/handlers.go:874` — `loopID, err = h.loopManager.CreateLoop(task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-dispatch/task_recovery.go:21` — `const dispatchTaskIDPrefix = "dispatch-"`
- `processor/agentic-dispatch/task_recovery.go:29` — `type vacantDispatchTaskSlot struct {`
- `processor/agentic-dispatch/task_recovery.go:34` — `type retainedTaskEvidenceReader interface {`
- `processor/agentic-dispatch/task_recovery.go:64` — `func stableDispatchTaskID(msg agentic.UserMessage) string {`
- `processor/agentic-dispatch/task_recovery.go:75` — `func (c *Component) findRetainedDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:79` — `if err := msg.Validate(); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:82` — `taskID := stableDispatchTaskID(msg)`
- `processor/agentic-dispatch/task_recovery.go:103` — `func (c *Component) prepareNewDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:110` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:120` — `func dispatchTaskAddress(ports []component.PortDefinition, taskID string) (string, string, error) {`
- `processor/agentic-dispatch/task_recovery.go:147` — `func (c *Component) readRetainedDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:158` — `return agentic.TaskMessage{}, nil, false, errs.WrapTransient(`
- `processor/agentic-dispatch/task_recovery.go:170` — `if err := decoded.Validate(); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:183` — `func validateRetainedDispatchTask(`
- `processor/agentic-dispatch/component.go:972` — `prepared, vacant, found, err := c.findRetainedDispatchTask(ctx, msg)`
- `processor/agentic-dispatch/component.go:974` — `if errs.IsFatal(err) || errs.IsTransient(err) {`
- `processor/agentic-dispatch/component.go:982` — `if !found && !c.hasPermission(msg.UserID, "submit_task") {`
- `processor/agentic-dispatch/component.go:1020` — `prepared, err = c.prepareNewDispatchTask(ctx, msg, loopID, vacant)`
- `processor/agentic-dispatch/http.go:302` — `prepared, vacant, found, err := c.findRetainedDispatchTask(ctx, msg)`
- `processor/agentic-dispatch/http.go:309` — `if !found && !c.hasPermission(msg.UserID, "submit_task") {`
- `processor/agentic-dispatch/http.go:348` — `prepared, err = c.prepareNewDispatchTask(ctx, msg, loopID, vacant)`
- `processor/rule/actions.go:1709` — `taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())`
- `processor/rule/actions.go:1712` — `task := agentic.TaskMessage{`
- `processor/rule/actions.go:1713` — `TaskID:       taskID,`
- `processor/rule/actions.go:1733` — `task.ParentLoopID = parentLoopID`
- `agentic/loop_execution_entity.go:83` — `Task     *TaskMessage `json:"task,omitempty"``
- `agentic/loop_execution_entity.go:126` — `if e.Task.TaskID != "" {`
- `agentic/loop_execution_entity.go:127` — `triples = append(triples, triple(agvocab.LoopTask, e.Task.TaskID))`
- `agentic/rule_fields.go:259` — `"task_id": t.TaskID,`
- `agentic/rule_fields.go:263` — `putString(fields, "loop_id", t.LoopID)`
- `vocabulary/agentic/predicates.go:281` — `TaskAssigned = "agent.task.assigned"`
- `vocabulary/agentic/predicates.go:291` — `TaskSubtask = "agent.task.subtask"`
- `vocabulary/agentic/predicates.go:301` — `TaskStatus = "agent.task.status"`
- `vocabulary/agentic/register.go:377` — `vocabulary.Register(TaskAssigned,`
- `vocabulary/agentic/register.go:389` — `vocabulary.Register(TaskDependency,`
- `vocabulary/agentic/register.go:393` — `vocabulary.Register(TaskStatus,`
- `processor/agentic-loop/component.go:104` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1374` — `c.pendingTaskResults[taskID] = result`
- `processor/agentic-loop/component.go:1380` — `result, ok := c.pendingTaskResults[taskID]`
- `agentic/events.go:13` — `LoopID           string         `json:"loop_id"``
- `agentic/events.go:14` — `TaskID           string         `json:"task_id"``
- `agentic/events.go:61` — `LoopID       string    `json:"loop_id"``
- `agentic/events.go:62` — `TaskID       string    `json:"task_id"``
- `agentic/events.go:137` — `LoopID     string `json:"loop_id"``
- `agentic/events.go:138` — `TaskID     string `json:"task_id"``
- `agentic/events.go:204` — `LoopID      string `json:"loop_id"``
- `agentic/events.go:205` — `TaskID      string `json:"task_id"``
- `processor/agentic-dispatch/loop_tracker.go:14` — `type LoopInfo struct {`
- `processor/agentic-dispatch/loop_tracker.go:15` — `LoopID string `json:"loop_id"``
- `processor/agentic-dispatch/loop_tracker.go:16` — `TaskID string `json:"task_id"``
- `processor/agentic-dispatch/loop_tracker.go:100` — `loops        map[string]*LoopInfo // loop_id -> LoopInfo`
- `processor/agentic-dispatch/task_recovery_test.go:25` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/task_recovery_test.go:26` — `func TestUnreadableRetainedTaskEvidenceDoesNotMintOrRefuse(t *testing.T) {`
- `processor/agentic-dispatch/task_recovery_test.go:40` — `require.True(t, errs.IsTransient(err), "unreadable evidence must retry instead of pretending this is new work")`
- `processor/agentic-dispatch/task_recovery_test.go:42` — `require.Empty(t, prepared.task.LoopID, "a random LoopID requires an exact retained-absence result")`
- `processor/agentic-dispatch/task_recovery_test.go:45` — `require.True(t, errs.IsTransient(err), "the durable source owner must retry an unreadable evidence check")`
- `processor/agentic-dispatch/task_recovery_test.go:46` — `require.Empty(t, sink.all(), "a retryable evidence outage is not a permanent user refusal")`
- `processor/agentic-dispatch/task_recovery_test.go:47` — `require.Empty(t, c.loopTracker.GetAllLoops(), "unreadable evidence must not track newly minted work")`
- `processor/agentic-dispatch/task_recovery_test.go:50` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/task_recovery_test.go:51` — `func TestInvalidUserMessageIdentityIsRejectedBeforeTaskIdentity(t *testing.T) {`
- `processor/agentic-dispatch/task_recovery_test.go:64` — `require.Empty(t, vacant.taskID, "invalid source identity must not derive a TaskID")`
- `processor/agentic-dispatch/task_recovery_test.go:65` — `require.Empty(t, prepared.task.LoopID, "invalid source identity must not mint a LoopID")`
- `processor/agentic-dispatch/restart_identity_integration_test.go:212` — `// Mutable AutoContinue state has moved on while the source was waiting for`
- `processor/agentic-dispatch/restart_identity_integration_test.go:221` — `decision, cause = replacementDispatch.handleUserMessage(ctx, secondSource.Data())`
- `processor/agentic-dispatch/restart_identity_integration_test.go:238` — `require.Equal(t, firstTask.LoopID, secondTask.LoopID,`
- `processor/agentic-dispatch/restart_identity_integration_test.go:245` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/restart_identity_integration_test.go:246` — `func TestIntegrationUserMessageTaskMappingConflictQuarantines(t *testing.T) {`
- `processor/agentic-dispatch/restart_identity_integration_test.go:265` — `name:   "retained TaskMessage is malformed",`
- `processor/agentic-dispatch/restart_identity_integration_test.go:302` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-dispatch/restart_identity_integration_test.go:309` — `require.Equal(t, uint64(1), info.State.Msgs, "conflict must not publish or overwrite either mapping")`
- `processor/agentic-dispatch/loop_wire.go:15` — `LoopID        string `json:"loop_id"``
- `processor/agentic-dispatch/loop_wire.go:16` — `TaskID        string `json:"task_id,omitempty"``
- `processor/agentic-dispatch/loop_wire.go:82` — `TaskID:        e.TaskID,`
- `processor/agentic-loop/doc.go:241` — `// **AGENT_LOOPS bucket**: Stores LoopEntity as JSON, keyed by loop ID`
- `processor/agentic-loop/README.md:151` — `| loops | AGENT_LOOPS | `{loop_id}` | Loop entity state |`
- `docs/concepts/13-agentic-systems.md:395` — `Agent loops are stored in NATS KV (`AGENT_LOOPS`) as queryable entities:`
- `docs/concepts/13-agentic-systems.md:399` — `│ Key: <loop-uuid>                            │`
- `docs/concepts/13-agentic-systems.md:402` — `│ task_id        = "task_456"                 │`

## Adjacent claims

- `docs/adr/053-agent-run-substrate.md:29` — `The framework models the loop *tree* (`LoopEntity.ParentLoopID`, `agent.loop.parent``
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:31` — `The loop instance token is the identity plane the agentic substrate keys on: the `AGENT_LOOPS` record, the`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:54` — `1. **A loop instance token is a framework-minted v4 UUID**, carried in canonical RFC 4122 text form (36 bytes,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:134` — `Dispatch SHALL derive stable TaskID from validated `UserMessage` identity. For new work it SHALL mint a`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:152` — `### Requirement: Loop task, request, and tool work use only required correlation`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:88` — `## 2. Lane-scoped task, request, and tool-work correlation`
- `openspec/changes/agentic-loop-restart-safety/design.md:439` — `### `agent.task``
- `openspec/specs/agentic-terminal-events/spec.md:11` — `validates, its loop and task IDs are nonempty, its applicable terminal timestamp is nonzero, and its category/outcome`
- #807 — agentic: persist TaskID claims beyond retained message dedupe
- #1207 — rule: the rule-&lt;entityID&gt;-&lt;unixnano&gt; TaskID convention has no framework decoder
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1244 — agentic-loop: adopt the StopAll exit contract for loop state — two silent stalls leave a loop wedged with no transition and no observer
- PR #1159 (draft) — fix(agentic-loop): preserve durable work across process restart
- PR #1156 (draft) — refactor(natsclient): add semantic delivery settlement
- PR #1254 (draft) — docs(auth): inventory-only deliverable for the principal primitive
- PR #1141 (draft) — fix(agentic-tools)!: restore bounded GET-only page retrieval

## Consumers

- `processor/agentic-loop/handlers.go:827` — `// Dedup: if a non-terminal loop already exists for this task, skip.`
- `processor/agentic-loop/handlers.go:830` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:832` — `slog.String("task_id", task.TaskID),`
- `processor/agentic-loop/component.go:1288` — `pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1331` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/handlers.go:1101` — `SourceCorrelation: task.TaskID,`
- `processor/agentic-loop/handlers.go:2118` — `completion := agentic.LoopCompletedEvent{`
- `processor/agentic-loop/handlers.go:2705` — `failure := &agentic.LoopFailedEvent{`
- `processor/agentic-dispatch/component.go:1033` — `taskID := task.TaskID`
- `processor/agentic-dispatch/component.go:1119` — `TaskID:           created.TaskID,`
- `processor/agentic-dispatch/http.go:356` — `taskID := task.TaskID`
- `processor/agentic-dispatch/loop_wire.go:45` — `TaskID:        in.TaskID,`
- `processor/agentic-dispatch/loop_wire.go:82` — `TaskID:        e.TaskID,`
- `processor/agentic-dispatch/loop_tracker.go:538` — `// GetUserLoops returns all loops for a specific user`
- `processor/agentic-dispatch/loop_tracker.go:539` — `func (t *LoopTracker) GetUserLoops(userID string) []*LoopInfo {`
- `processor/agentic-dispatch/loop_tracker.go:552` — `// GetAllLoops returns all tracked loops`
- `processor/agentic-dispatch/loop_tracker.go:553` — `func (t *LoopTracker) GetAllLoops() []*LoopInfo {`
- `processor/agentic-dispatch/http.go:547` — `loops = c.loopTracker.GetUserLoops(userID)`
- `processor/agentic-dispatch/http.go:549` — `loops = c.loopTracker.GetAllLoops()`
- `output/otel/span_collector.go:495` — `spanKey := event.LoopID + ":" + event.TaskID`
- `output/otel/span_collector.go:506` — `"agent.task_id": event.TaskID,`
- `output/otel/span_collector.go:523` — `spanKey := event.LoopID + ":" + event.TaskID`
- `output/otel/span_collector.go:549` — `parentKey = event.LoopID + ":" + event.TaskID`
- `internal/agentterminal/terminal.go:136` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `internal/agentterminal/terminal.go:157` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `internal/agentterminal/terminal.go:177` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `agentic/loop_execution_entity.go:127` — `triples = append(triples, triple(agvocab.LoopTask, e.Task.TaskID))`
- `agentic/rule_fields.go:259` — `"task_id": t.TaskID,`
- `processor/agentic-dispatch/README.md:186` — `| `agent.task.{task_id}` | Publish | Task dispatch |`
- `processor/agentic-loop/README.md:132` — `| agent.task | jetstream | agent.task.* | Task requests from external systems |`

## Problem shape

- `processor/agentic-loop/state.go:191` — `// Two refusals, in a fixed order (#1227). The token FORM check runs first, so a`
- `processor/agentic-loop/state.go:194` — `// because those writes OVERWRITE an existing record, its pending-tool set, and`
- `processor/agentic-loop/state.go:197` — `// continuation. Callers that mean a continuation branch on ErrLoopAlreadyExists`
- `processor/agentic-loop/handlers.go:839` — `// A supplied token that already names a registered loop is a CONTINUATION,`
- `processor/agentic-loop/handlers.go:856` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/state.go:258` — `//   - The loop's task association is rebound to the continuation's task ID.`
- `processor/agentic-loop/state.go:264` — `//     Residual, known and accepted: the rebind preserves dedup for THIS turn`
- `processor/agentic-loop/state.go:265` — `//     and drops it for the previous one. A single scalar cannot dedupe more`
- `processor/agentic-loop/state.go:299` — `entity.TaskID = taskID`
- `processor/agentic-dispatch/loop_tracker.go:98` — `userLoops    map[string]string    // user_id -> most recent loop_id`
- `processor/agentic-dispatch/loop_tracker.go:99` — `channelLoops map[string]string    // channel_id -> most recent loop_id`
- `processor/agentic-dispatch/loop_tracker.go:100` — `loops        map[string]*LoopInfo // loop_id -> LoopInfo`

## Searches

- `git rev-parse HEAD` → `79b0f29f82ce5391013f6c931fae69a28216ac93`
- `git grep -n '^## Purpose\\|^## Product Boundary' -- openspec/project.md` → 2
- `gopls workspace_symbol -matcher=fuzzy TaskMessage` → 59
- `gopls workspace_symbol -matcher=fuzzy LoopEntity` → 100
- `gopls workspace_symbol -matcher=fuzzy HasActiveLoopForTask` → 2
- `gopls workspace_symbol -matcher=fuzzy pendingTaskResults` → 1
- `gopls references agentic/user_types.go:315:2` → 195
- `gopls references agentic/state.go:50:2` → 75
- `gopls references processor/agentic-loop/state.go:305:23` → 11
- `gopls references processor/agentic-loop/component.go:104:2` → 6
- `gopls implementation agentic/user_types.go:313:6` → 10
- `gopls implementation agentic/state.go:48:6` → 2
- `gopls call_hierarchy processor/agentic-loop/component.go:1244:21` → 2 callers, 25 callees
- `gopls call_hierarchy processor/agentic-loop/handlers.go:811:26` → 71 callers, 33 callees
- `git grep -n 'TaskID' -- '*.go' ':!**/*_test.go'` → 111
- `git grep -n -E 'task_id|task-id|TASK_ID' -- agentic processor/agentic-dispatch processor/agentic-loop processor/rule openspec/specs docs/adr openspec/changes docs/operations/'migration-*.md'` → 75
- `git grep -n -E 'agent\\.task|AGENT_TASK|TaskSubject|taskSubject' -- '*.go' '*.md' '*.json' '*.yaml' '*.yml'` → 422
- `git grep -n -E 'AGENT_LOOPS|agent_loops|AgentLoops' -- '*.go' '*.md' '*.json' '*.yaml' '*.yml'` → 750
- `git grep -n -E '(List|Get|Find|Enumerate|Query|Watch)(Task|Tasks)|task(s)?(_|-)?(store|index|bucket|registry|mapping)|TASK(S)?(_|-)?(STORE|INDEX|BUCKET)' -- '*.go' '*.md' '*.json' '*.yaml' '*.yml'` → 5 (all `GetTaskPrompt` declaration/calls)
- `git grep -n 'ListTasks' -- '*.go' '*.md' '*.json'` → 0
- `git grep -n -E 'func .*GetTask\\(|GetTask\\(' -- '*.go' '*.md'` → 0
- `git grep -n -E 'Task(Store|Index|Bucket|Registry)' -- '*.go' '*.md' '*.json'` → 0
- `git grep -n -E 'TASKS(_|-)?(STORE|INDEX|BUCKET)|TASK_(STORE|INDEX|BUCKET)' -- '*.go' '*.md' '*.json'` → 0
- `git grep -n -E 'TaskToLoop|task_to_loop|task-to-loop|task.*loop.*map|map.*task.*loop' -- '*.go' '*.md' '*.json'` → 3
- `git grep -n -E '/tasks|tasks/' -- '*.go' '*.md' '*.json'` → 51 (all returned hits are repository documentation paths or one unrelated `/api/tasks` metrics fixture; no task endpoint declaration)
- `git grep -n -E 'GetAllLoops|GetUserLoops|ListLoops|/loops|loop(s)?/\\{|loopsBucket\\.Keys|\\.Keys\\(\\)' -- processor/agentic-dispatch processor/agentic-loop docs/advanced/08-agentic-components.md docs/basics/07-agentic-quickstart.md docs/concepts/13-agentic-systems.md` → 128
- `git grep -n -E 'TaskAssigned|TaskCapability|TaskSubtask|TaskDependency|TaskStatus|agent\\.task\\.(assigned|capability|subtask|dependency|status)' -- ':!vocabulary/agentic/predicates.go'` → 11 (vocabulary registration/tests only)
- `git grep -n -E 'ParentLoopID|parent_loop_id|Depth:|MaxDepth:' -- '*.go' ':!**/*_test.go'` → 76
- `git grep -n -E 'TaskID|task_id|task ID|LoopID|loop_id|loop ID' -- docs/adr openspec/specs openspec/changes/agentic-loop-restart-safety docs/operations/'migration-*.md'` → 236
- `git grep -n -E 'CategoryTask|TaskMessage\\{\\}|Register.*Task|TaskMessage' -- agentic/payload_registry.go agentic/types.go agentic/*.go payloadbuiltins '*.go'` → 536
- `git grep -n -E 'taskID :=|loopID :=|TaskID:' -- processor/agentic-dispatch/component.go processor/agentic-dispatch/http.go processor/rule/actions.go` → 17
- `git grep -n -E 'ParentLoopID|parent_loop_id|Depth:|MaxDepth:' -- '*.go' ':!**/*_test.go'` → 76
- `gh issue list --search 'TaskID' --state open --json number,title` → 4
- `gh issue list --search 'task loop mapping' --state open --json number,title` → 3
- `gh issue list --search 'task enumeration' --state open --json number,title` → 3
- `gh pr list --state open --json number,title,body,isDraft` → 4 draft PRs
- `openspec list` → 2 active changes

### Refresh searches (2026-09-04)

- `git grep -n '^## \(Purpose\|Product Boundary\)' -- openspec/project.md` → 2
- `rg -n 'agentic-loop/spec\.md:(153|151)|tasks\.md:(82|73)|agentic-dispatch/spec\.md:111|design\.md:422' openspec/changes/agentic-loop-restart-safety/inventory-task-loop-cardinality-2026-09-04.md` → 6
- `git grep -n -E 'Approval continuation and dispatch projection|Approval evidence|projection|AutoContinue|incomplete hydration|explicit LoopID|Loop task, request|lane-scoped|at-least-once|edge gateway|agent.task|TaskID' -- openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → 113
- `git grep -n -E 'frozen-parent|Frozen parent|F =|F=|79b0f29|417beae|base:' -- openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/inventory-dispatch-bridge-boundary-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task-loop-cardinality-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task2-stable-identity-2026-09-03.md` → 6
- `shasum -a 256 openspec/changes/agentic-loop-restart-safety/design-dispatch-edge-gateway-2026-09-04.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/tasks.md` → 4
- `git grep -n -E 'pins=.*moved=.*ambiguous|inventory.*verif|malformed.*unparsed' -- . ':!openspec/changes/agentic-loop-restart-safety/inventory-*.md'` → 22
