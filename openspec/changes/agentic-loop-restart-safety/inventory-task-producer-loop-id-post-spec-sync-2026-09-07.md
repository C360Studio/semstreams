# Inventory: task producer LoopID ownership
base: af829616305afa039dac0550efa78d07e856dd5f
refresh-of: inventory-task-producer-loop-id-2026-09-07.md sha256:7b273f91996e71df860226c83d615691e6b08de0fa0153c7c5d4869a53a78c26
reason: owner-approved active spec and documentation synchronization; production implementation remains pending

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

The public component documentation now exposes the approved required-ID target while production implementation remains pending:

- `processor/agentic-loop/README.md:338` — ``loop_id` is required. A producer creating new loop work calls `uuid.NewString()` once before validation and marshal;`
- `processor/agentic-loop/doc.go:173` — `//	result, err := handler.HandleTask(ctx, TaskMessage{`
- `processor/agentic-loop/doc.go:175` — `//	    TaskID: "task_123",`

Production constructors in this repo: `git grep -n -F 'TaskMessage{' -- '*.go' ':!**/*_test.go'` returns only payload factory/zero returns, dispatch at `component.go:920`, loop docs, rule at `actions.go:1712`, and three E2E builders. No production constructor/mint helper exists: `git grep -n -E 'NewTaskMessage|NewLoopID|func (New|Prepare|Mint).*Task' -- '*.go'` returns no task constructor or loop-token mint API.

## 3. Adjacent claims

- `openspec/specs/entity-id-contract/spec.md:651` — `### Requirement: A loop instance token is a framework-minted UUID`
- `openspec/specs/entity-id-contract/spec.md:653` — `Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and`
- `openspec/specs/entity-id-contract/spec.md:655` — `4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. No component, config, client, or tool call MAY author`
- `openspec/specs/entity-id-contract/spec.md:674` — `- `TaskMessage.Validate` MUST refuse a task carrying ANY loop-token field — `loop_id`, `parent_loop_id`,`
- `openspec/specs/entity-id-contract/spec.md:679` — `- `LoopManager.CreateLoopWithID` MUST refuse before registering any loop state.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:154` — `Every TaskMessage producer SHALL supply a nonempty canonical LoopID before validation, envelope marshal, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:194` — `#### Scenario: Task mapping is stable across redelivery`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:197` — `- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:77` — ``publish_agent` SHALL mint one random version 4 LoopID locally for each execution producing new loop work, assign it`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:95` — `- **GIVEN** a valid registered `TaskMessage` that does not implement `graph.Graphable``
- `openspec/changes/agentic-loop-restart-safety/tasks.md:94` — `- [x] 2.1 RED: prove stable TaskID, random LoopID minting for new work, retained-`TaskMessage` LoopID recovery on`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:97` — `- [x] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:161` — `## 4. Loop task and response settlement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:163` — `- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:168` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:174` — `- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:340` — `- [ ] 9.5 RED: add all-six-configuration and four-static-producer real-NATS tests, both `agent.task`/`agent_task``
- `openspec/changes/agentic-loop-restart-safety/tasks.md:346` — `- [ ] 9.6 Implement rule-processor caller-local admission through the same internal validator before evaluator start.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:352` — `- [ ] 9.7 GREEN: prove six classifier surfaces cannot select core NATS for covered task subjects, four static producer`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:62` — `1. **A loop instance token is minted at its framework birth seam as a v4 UUID**, carried in canonical RFC 4122 text`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:63` — `form (36 bytes, lowercase, hyphenated). For newly published TaskMessage work, its producer is that seam and mints`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:67` — `2. **Enforcement lives at accepting seams**, not in a registry or family-table mechanism: `TaskMessage.Validate``
- `openspec/changes/agentic-loop-restart-safety/proposal.md:83` — `- Binding owner ruling #1146 comment `5575482141` accepts producer-local `uuid.NewString()` as the framework`
- `openspec/changes/agentic-loop-restart-safety/design.md:33` — `- producer LoopID inventory `inventory-task-producer-loop-id-2026-09-07.md`, base`
- `openspec/changes/agentic-loop-restart-safety/design.md:36` — `- producer LoopID design checkpoint `design-task-producer-loop-id-2026-09-07.md`, SHA-256`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:3` — `### Requirement: A loop instance token is minted at its framework birth seam`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:6` — `research-pipeline loops alike — MUST be minted at its framework birth seam as a version 4 UUID and carried in`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:177` — `#### Scenario: task identity is fixed before durable publication`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:187` — `#### Scenario: task mapping is stable across process replacement`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:85` — `#### Scenario: publish-agent fixes new-loop identity before publication`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:142` — `## 3A. Task producer identity prerequisite`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:144` — `- [ ] 3A.1 RED: prove TaskMessage validation returns an ordinary error naming missing LoopID; dispatch and rule produce`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:150` — `- [ ] 3A.2 Implement producer-owned task identity. Keep dispatch's retained-byte path; mint rule LoopID once per`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:155` — `- [ ] 3A.3 GREEN: through real NATS, redeliver the exact rule-produced registered bytes across agentic-loop process`

The current base entity-ID spec still carries the pre-amendment heading until archive. The active replacement delta,
amended ADR-105, proposal, design, and task 3A now agree that the direct TaskMessage producer is the framework birth
seam and LoopID is required before publication. Tasks 4.1–4.3 consume that mapping. Tasks 9.5–9.7 remain the sole owners
of rule admission, subject coverage, publisher classification, PubAck, and registered-payload proof.

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
1. What must adopters know at this checkpoint? Production still implements the old optional path, while the approved
   active target requires a producer-fixed LoopID. Task 3A and the migration table are the required coordinated break.
2. If they do nothing under a required-field contract? A producer that calls Validate gets an ordinary validation
   error naming `loop_id`; one that publishes without validating is classified invalid and terminated at loop intake,
   and no loop is born. This is loud, not silent, but it is a breaking migration.
3. Where do they find out? Typed validation/intake error plus SemStreams-owned beta.162→163 migration note; compile-time discovery is unavailable because TaskMessage remains a struct.
4. What should they have to know? One birth/echo rule: new work mints once before marshal; continuation work echoes an
   admitted token; retry of that same uncertain publication reuses serialized bytes. The owner ruled that producer-local
   `uuid.NewString()` is the framework seam, so no helper, constructor, or recovery storage is exposed.

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

1. Do direct `MessageHandler.HandleTask` and `LoopManager.CreateLoop` remain supported public composition seams, or
   become explicit removal work? Current documentation advertises both.
2. Is semdragon pinned to a pre-ADR-105 SemStreams version or already broken? No downstream writes are authorized.
