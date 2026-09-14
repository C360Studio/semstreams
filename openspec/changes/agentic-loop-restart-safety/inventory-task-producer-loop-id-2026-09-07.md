# Inventory: task producer LoopID ownership
base: af829616305afa039dac0550efa78d07e856dd5f

## Problem statement
A durable `TaskMessage` can carry no LoopID. When process-local correlation is absent, current intake skips retained-loop recovery and `HandleTask` mints a new random LoopID. Redelivery of the same retained bytes after replacement can therefore birth a second loop.

## 1. Claimed gap
- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id,omitempty"` // loop to continue, or empty for new`
- `agentic/user_types.go:407` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:456` — `// Empty is valid throughout: an unset token is the ordinary case, and the`
- `agentic/user_types.go:457` — `// framework mints it downstream. The caller's only verb is echo.`
- `processor/agentic-loop/component.go:1266` — `if task.LoopID != "" {`
- `processor/agentic-loop/component.go:1277` — `if result.LoopID == "" {`
- `processor/agentic-loop/component.go:1278` — `result, err = c.handler.HandleTask(ctx, *task)`
- `processor/agentic-loop/component.go:1422` — `if task.LoopID == "" {`
- `processor/agentic-loop/component.go:1423` — `task.LoopID = c.handler.loopManager.GenerateLoopID()`
- `processor/agentic-loop/handlers.go:877` — `} else {`
- `processor/agentic-loop/handlers.go:878` — `loopID, err = h.loopManager.CreateLoop(task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/state.go:171` — `// CreateLoop creates a new loop entity with a generated UUID`
- `processor/agentic-loop/state.go:173` — `loopID := m.GenerateLoopID()`

## 2. Every spelling and owner of task-to-loop birth identity

The durable carrier and its current validation contract are:

- `agentic/user_types.go:313` — `type TaskMessage struct {`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/user_types.go:433` — `if err := t.validateLoopTokens(); err != nil {`
- `agentic/user_types.go:463` — `{"loop_id", t.LoopID},`
- `agentic/user_types.go:469` — `if err := validateLoopTokenField(token.field, token.value); err != nil {`
- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`

Dispatch already fixes a new task's LoopID before marshal, then retains and validates that exact task evidence:

- `processor/agentic-dispatch/task_recovery.go:89` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:98` — `return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil`
- `processor/agentic-dispatch/task_recovery.go:109` — `if loopID == "" {`
- `processor/agentic-dispatch/task_recovery.go:110` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:112` — `task := c.buildTaskMessage(ctx, msg, loopID, slot.taskID)`
- `processor/agentic-dispatch/task_recovery.go:113` — `data, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch"))`
- `processor/agentic-dispatch/task_recovery.go:192` — `case task.LoopID == "":`
- `processor/agentic-dispatch/component.go:921` — `LoopID:           loopID,`

Rule is the other production constructor. It assigns TaskID but not LoopID, validates successfully, then publishes:

- `processor/rule/actions.go:1709` — `taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())`
- `processor/rule/actions.go:1712` — `task := agentic.TaskMessage{`
- `processor/rule/actions.go:1884` — `if err := task.Validate(); err != nil {`
- `processor/rule/actions.go:1955` — `baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")`
- `processor/rule/actions.go:1956` — `data, err := json.Marshal(baseMsg)`
- `processor/rule/actions.go:1961` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`

Governance republishes the decoded or modified message. It is a forwarding writer, not a second identity owner:

- `processor/agentic-governance/component.go:422` — `result.AddGovernanceMetadata()`
- `processor/agentic-governance/component.go:426` — `outputMsg := result.ModifiedMessage`
- `processor/agentic-governance/component.go:428` — `outputMsg = &msg`
- `processor/agentic-governance/component.go:438` — `outputData, err := json.Marshal(outputMsg)`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`

Loop intake validates, attempts retained recovery only when LoopID is present, and still exposes downstream minting:

- `processor/agentic-loop/component.go:1408` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/component.go:1268` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/handlers.go:837` — `// Use provided loop_id if present, otherwise create new one.`
- `processor/agentic-loop/handlers.go:855` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/state.go:180` — `func (m *LoopManager) GenerateLoopID() string {`
- `processor/agentic-loop/state.go:181` — `return uuid.NewString()`

The public component documentation also exposes the old optional contract:

- `processor/agentic-loop/README.md:338` — ``loop_id` is optional — omit it and the loop mints one. It is never a value you`
- `processor/agentic-loop/doc.go:170` — `//	result, err := handler.HandleTask(ctx, TaskMessage{`
- `processor/agentic-loop/doc.go:171` — `//	    TaskID: "task_123",`

Production constructors in this repo: `git grep -n -F 'TaskMessage{' -- '*.go' ':!**/*_test.go'` returns only payload factory/zero returns, dispatch at `component.go:920`, loop docs, rule at `actions.go:1712`, and three E2E builders. No production constructor/mint helper exists: `git grep -n -E 'NewTaskMessage|NewLoopID|func (New|Prepare|Mint).*Task' -- '*.go'` returns no task constructor or loop-token mint API.

## 3. Adjacent claims

- `openspec/specs/entity-id-contract/spec.md:651` — `### Requirement: A loop instance token is a framework-minted UUID`
- `openspec/specs/entity-id-contract/spec.md:653` — `Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and`
- `openspec/specs/entity-id-contract/spec.md:655` — `4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. No component, config, client, or tool call MAY author`
- `openspec/specs/entity-id-contract/spec.md:674` — `- `TaskMessage.Validate` MUST refuse a task carrying ANY loop-token field — `loop_id`, `parent_loop_id`,`
- `openspec/specs/entity-id-contract/spec.md:679` — `- `LoopManager.CreateLoopWithID` MUST refuse before registering any loop state.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:154` — `For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:172` — `#### Scenario: Task mapping is stable across redelivery`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:175` — `- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:77` — ``publish_agent` SHALL construct and validate `agentic.TaskMessage`, wrap it in a registered `BaseMessage`, and publish`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:85` — `- **GIVEN** a valid registered `TaskMessage` that does not implement `graph.Graphable``
- `openspec/changes/agentic-loop-restart-safety/tasks.md:90` — `- [x] 2.1 RED: prove stable TaskID, random LoopID minting for new work, retained-`TaskMessage` LoopID recovery on`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:93` — `- [x] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:138` — `## 4. Loop task and response settlement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:140` — `- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:145` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:151` — `- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:317` — `- [ ] 9.5 RED: add all-six-configuration and four-static-producer real-NATS tests, both `agent.task`/`agent_task``
- `openspec/changes/agentic-loop-restart-safety/tasks.md:323` — `- [ ] 9.6 Implement rule-processor caller-local admission through the same internal validator before evaluator start.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:329` — `- [ ] 9.7 GREEN: prove six classifier surfaces cannot select core NATS for covered task subjects, four static producer`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:54` — `1. **A loop instance token is a framework-minted v4 UUID**, carried in canonical RFC 4122 text form (36 bytes,`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:55` — `lowercase, hyphenated). No component, config, client, tool, or injected generator authors one — stated as the`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:57` — `2. **Enforcement lives at the mint seams**, not in a registry or family-table mechanism: task validation`

The current entity-ID contract and ADR say components do not author loop tokens, while the active task-correlation
requirement names only dispatch as the producer. The rule capability requires a valid registered TaskMessage without
stating who fixes the new task's LoopID. Task 4 assumes the durable task-to-loop mapping already exists. Producer-
identity design coordinates with tasks 4.1–4.3 and the rule publisher seam in tasks 9.5–9.7; it does not absorb or
reopen rule admission, subject coverage, publisher classification, or registered-payload scope.

## 4. Present consumers
No new symbol is proposed in inventory phase. Present consumer of `TaskMessage.LoopID` is agentic-loop intake/handler. Dispatch, rule, direct Go producers, governance forwarding, tests and docs are existing writers/adopters.

## 5. Problem shape
Shape: assign stable correlation before durable publication; on retry/redelivery reuse the correlation already carried by the work item; consumer validates and observes, never predicts/re-mints. Existing closest instance is dispatch itself (`task_recovery.go:103-117`, `:75-100`). AgentRequest RequestID follows the same producer-before-publish shape. This is adoption of an existing pattern, not establishment of a reusable runtime primitive.

## Same-class collision table
| Dimension | Evidence |
|---|---|
| Semantic class | Owner of new-task loop instance identity across durable delivery |
| Owners | dispatch before publish (`task_recovery.go:103-117`); rule currently omits (`actions.go:1708-1719`); loop currently mints on absence (`component.go:1422-1424`, `handlers.go:877-883`) |
| Catalogs | TaskMessage registered control payload (`agentic/payload_registry.go:34`); no separate task identity catalog |
| Status | LoopEntity records ID+TaskID in AGENT_LOOPS; retained TaskMessage records wire mapping |
| Lifecycle | AGENT redelivers unacked bytes; loop replacement discards process maps; AGENT_LOOPS survives |
| Ownership | Current birth ownership is split between dispatch and loop; rule delegates silently to loop |
| Readers | loop intake/handler; graph birth, events, OTEL and terminal readers consume resulting pair (accepted cardinality inventory) |
| Writers | dispatch, rule, governance forwarder; external direct producers below |
| Recovery | dispatch exact-reads retained TaskMessage; loop task4 exact-reads LoopEntity/request only when LoopID is present; no scan/secondary map exists |

## Adopter seam inventory
Specific adopter: a developer outside SemStreams constructing `agentic.TaskMessage` and publishing it on `agent.task.*`.

| Repository and observed revision | Production construction seams | Observed state |
|---|---|---|
| semdev at ca3956af2ed8 | internal/intake/coordinatortask.go line 64 | Missing LoopID |
| semteams at ce22c961d300 | cmd/semteams/chainpause/decision_handler.go line 230 | Missing LoopID |
| semspec at 5a9496eecc45 | lesson-decomposer line 568; qa-reviewer line 544; researcher-manager line 442; question tool line 389 | All missing LoopID |
| semmachina at 841c45e8bb01 | internal/persona/spec.go lines 402 and 413 | Missing LoopID, then calls Validate |
| semsage at 4d28b4dc1210 | UI API line 422; spawn tool lines 176 and 211–222 | UI API missing; spawn already mints canonical child identity |
| semdragon at 07f4de9b6588 | questbridge, questdagexec, and questtools constructors | Prefill noncanonical prefixed NUID tokens |
| semops, semsource, semconnect, semboids, semembed, semlink, semmem | No production TaskMessage constructor found | No direct code migration found |

Rule JSON users in semdev, semteams, and semspec reach the in-repo rule producer and carry no separate code migration.
Sister repositories are read-only inventory sources; this artifact neither changes them nor claims their validation.

Questions:
1. What must they know today? LoopID is optional; if omitted the consumer mints. If set it must be canonical, while ADR-105 says adopters author none.
2. If they do nothing under a required-field contract? A producer that calls Validate gets a typed runtime error; one that publishes without validating is terminated at loop intake and no loop is born. This is loud, not silent, but it is a breaking migration.
3. Where do they find out? Typed validation/intake error plus SemStreams-owned beta.162→163 migration note; compile-time discovery is unavailable because TaskMessage remains a struct.
4. What should they know? One rule: every newly published TaskMessage must already carry the one stable loop identity retained in those bytes. The inventory exposes an unresolved seam debt: current ADR says an external component must never author a token, but there is no exported SemStreams mint/constructor API, while making LoopID required obliges that producer to obtain one somehow.

## Searches

```text
git grep -n -F 'TaskMessage{' -- '*.go'
git grep -n -E 'TaskMessage|loop.?id|framework-mint|mint.*loop|new work' -- openspec/specs openspec/changes/agentic-loop-restart-safety docs/adr
git grep -n -E 'LoopID: *(uuid.New|uuid.NewString)|GenerateLoopID\(|CreateLoop\(' -- '*.go' ':!**/*_test.go'
git grep -n -E 'NewTaskMessage|NewLoopID|func (New|Prepare|Mint).*Task' -- '*.go'
git -C <sister-repo> grep -n -F 'TaskMessage{' -- '*.go'
```

Every sister-repository constructor hit was inspected at its observed revision with tests excluded. The direct-
constructor search returned zero production hits for semops, semsource, semconnect, semboids, semembed, semlink, and
semmem.

## Open evidence questions

1. Does the owner treat a product component calling `uuid.NewString()` at the task-production seam as framework
   minting, requiring a correction to ADR wording, or require a narrow exported SemStreams constructor/mint operation?
   No such operation exists today.
2. Do direct `MessageHandler.HandleTask` and `LoopManager.CreateLoop` remain supported public composition seams, or
   become explicit removal work? Current documentation advertises both.
3. Is semdragon pinned to a pre-ADR-105 SemStreams version or already broken? No downstream writes are authorized.
