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

## Design checkpoint

Status: advisory target-state draft; not owner-approved and not implementation authority.

This is a small prerequisite to task 4 review. It fixes the missing durable identity premise without adding recovery
state or reopening tasks 9.5–9.7.

## Measured premises

1. `TaskMessage.LoopID` is optional on the wire and `TaskMessage.Validate` accepts absence; the accepted inventory pins
   both facts at `agentic/user_types.go:314` and `agentic/user_types.go:407-457`.
2. Dispatch is the existing correct shape: it calls `uuid.NewString()` before constructing and marshaling the retained
   task, and reuses retained bytes. The accepted inventory pins that path at
   `processor/agentic-dispatch/task_recovery.go:89-117`.
3. Rule is the only other in-repository production constructor and currently omits LoopID before validation, marshal,
   and publication. The accepted inventory pins the path at `processor/rule/actions.go:1709-1961`.
4. Agentic-loop conditionally skips cold recovery when LoopID is absent and has two downstream mint branches. The
   accepted inventory pins them at `processor/agentic-loop/component.go:1266-1423` and
   `processor/agentic-loop/handlers.go:837-878`.
5. No task identity bucket, TaskID-to-LoopID map, exported TaskMessage constructor, or exported loop-token mint helper
   exists. The accepted inventory records the closing searches.
6. The adopter sweep found 13 direct production constructors requiring migration: eight omit LoopID and five use a
   noncanonical prefixed NUID. One semsage spawn path already uses a canonical UUID. Seven sister repositories have no
   direct production constructor.
7. Tasks 4.1–4.3 consume the durable task-to-loop mapping. Tasks 9.5–9.7 separately own rule publisher admission,
   subject coverage, durable publication, and registered-payload proof. The accepted inventory pins both task groups.

## Options considered

### Option A: do nothing

Keep LoopID optional and let agentic-loop mint when it receives an empty value. This has no immediate migration cost,
but exact redelivery of one durable TaskMessage can create a second loop after process replacement. Task 4 cannot make
that path restart-safe without another identity owner.

### Option B: derive LoopID from TaskID in agentic-loop

A deterministic derivation avoids an extra field requirement, but changes the random loop-token contract, couples two
identities with different semantics, and makes the consumer manufacture a fact the producer could have fixed before
durability. It also creates a second derivation contract every adopter must reproduce or trust implicitly.

### Option C: recover an absent LoopID through a scan, map, ledger, or bucket

A consumer-side TaskID-to-LoopID lookup could recover old messages, but it creates another durable authority,
lifecycle, conflict policy, and repair surface for a fact that fits in the existing retained bytes. It is expressly
outside the approved streams-first design.

### Option D: add an exported SemStreams constructor or loop-ID helper

A constructor could make the happy path more discoverable, but `TaskMessage` must remain directly constructible for
payload decoding and its many optional fields make a required all-fields constructor disproportionate. A helper that
only returns `uuid.NewString()` adds public framework surface without enforcing anything that the standard UUID mint
plus `TaskMessage.Validate` does not already enforce. Neither option creates compile-time requiredness while the wire
payload remains a struct.

### Option E: make the task producer the birth seam and mint locally before marshal

Each execution of a new-task producer calls `uuid.NewString()` once and puts the result in TaskMessage before
validation and marshal. A retry of that same already-marshaled publication and downstream redelivery of its retained
AGENT bytes reuse those bytes. Re-execution of the upstream producer is a separate production attempt outside this
identity-reuse claim. A continuation producer echoes the admitted existing LoopID. The consumer validates and observes
that value; it never creates or recovers a missing one. This changes the outward contract, but adds no API, state,
lookup, or runtime primitive.

## Recommendation

Choose option E.

Producer-local `uuid.NewString()` is the framework task-birth mint seam. Do not add `agentic.NewLoopID`, a
TaskMessage constructor, an injectable generator, or another identity service. “Framework-minted” should be corrected
to mean “minted at the framework task-production seam,” not “minted later by agentic-loop.” A direct component author
who publishes TaskMessage participates in that seam and therefore mints the new-loop token before publication.

The strongest case against this recommendation is discoverability: a new component author can still write a struct
literal without LoopID and learns at runtime, whereas a constructor advertises required arguments. The constructor
does not remove the raw struct or the validation obligation, however, and would wrap a standard one-line UUID mint
while imposing a broad initialization API over a large optional payload. Loud validation at both producer and consumer,
a required schema field, examples, and a migration note are the smaller greenfield contract.

This recommendation requires an owner ruling because it corrects ADR-105's sentence that no component authors a loop
token. It retains the ADR's substantive invariants: random v4 minting, canonical wire form, no configurable or
injectable identity policy, form validation at accepting seams, and no provenance claim.

## Target contract

1. Every published TaskMessage carries a nonempty canonical LoopID.
2. For new loop work, each task-producer execution mints one random v4 UUID before `Validate`, envelope marshal, and
   publication. A retry of that same already-marshaled publication reuses its TaskMessage bytes. This contract makes
   no identity-stability claim across a fresh execution of the upstream producer.
3. For continuation work, the producer echoes the admitted existing LoopID; it does not mint a replacement.
4. `TaskMessage.Validate` is the one requiredness and form-validation home. It returns the ordinary validation error
   `loop_id required` for absence before applying the existing canonical-form predicate; it does not classify errors.
5. The rule publish boundary, agentic-loop intake preflight, and public direct `MessageHandler.HandleTask` boundary
   classify a validation failure as invalid before any loop, context, graph, or publication side effect. Durable
   intake terminates and counts an empty or malformed LoopID.
6. Task intake and HandleTask always use the supplied LoopID. Their empty-value mint branches are removed. The
   prerequisite adds no scan, replay cache, map, ledger, bucket, deterministic derivation, or second owner.
7. Existing `LoopManager.CreateLoop` and `GenerateLoopID` are not broadened or made adopter seams by this prerequisite.
   They may remain as standalone in-process creation mechanics, but no TaskMessage path reaches them to repair absent
   identity. Any later zero-consumer cleanup is separate removal work.
8. Governance preserves LoopID while forwarding. It neither validates birth ownership nor mints a replacement.
9. Rule `publish_agent` mints LoopID in its existing TaskMessage construction before the existing Validate/marshal
   sequence. This coordinates with tasks 9.5–9.7 but does not absorb or alter their admission validator, wildcard
   coverage, publisher classification, PubAck, or registered-payload scope.
10. Dispatch behavior is already conforming and remains the reference pattern.

## Invariants and proof homes

| Invariant | Spec home |
|---|---|
| A published TaskMessage always carries LoopID | entity-id-contract, modified loop-token requirement |
| A new-task producer mints v4 once before durable bytes exist | agentic-loop, modified required-correlation requirement |
| Continuation echoes its admitted existing LoopID | agentic-loop, modified required-correlation requirement |
| Missing or noncanonical LoopID creates no loop state | entity-id-contract, missing-token refusal scenario |
| Exact redelivery after replacement names the same loop | agentic-loop, task-mapping redelivery scenario |
| Rule output satisfies identity before payload validation and marshal | rule-agent-publishing, modified registered-payload requirement |
| No loop-side identity repair state exists | agentic-loop, task-mapping redelivery scenario |

Validation proves presence and canonical form, not UUID version or provenance. Producer tests prove v4 minting. The two
claims remain deliberately separate, matching the existing internal form predicate.

## Artifact deltas

### Proposal and design

Add this prerequisite to the change's `What Changes` and `Holds`: every durable task producer fixes LoopID before
publication; task intake refuses absence; no consumer-side recovery authority is added. Update Impact from seven to
eight capability deltas by adding `entity-id-contract`.

### entity-id-contract capability

Add `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md` and MODIFIED-copy the complete
current requirement under the replacement heading `A loop instance token is minted at its framework birth seam`; do
not retain or shadow the old `A loop instance token is a framework-minted UUID` heading. Retain every unaffected form,
admission, HTTP, AgentRun, research, and carrier scenario. Replace only the ownership language and TaskMessage cases so
the full requirement says:

- a loop token is minted as v4 at its framework birth seam and carried canonically;
- a producer of new durable TaskMessage work is that birth seam and mints before publication;
- a continuation producer echoes an admitted existing token;
- no config, end-user input knob, injectable generator, or validation strictness knob exists;
- TaskMessage.Validate requires LoopID and validates every present loop-token field's canonical form; and
- validation checks form, while producer tests check v4 and no seam claims provenance.

Add this scenario verbatim:

#### Scenario: a task without loop identity is refused before state

- **GIVEN** a decoded TaskMessage whose `loop_id` is absent
- **WHEN** `TaskMessage.Validate` runs
- **THEN** it returns an ordinary validation error naming `loop_id`
- **AND WHEN** the rule publish boundary, durable agentic-loop intake, or direct `MessageHandler.HandleTask` accepts it
- **THEN** that operation boundary classifies the validation failure as invalid before any side effect
- **AND** durable intake terminates the delivery and increments the intake-rejection counter
- **AND** no loop, context, graph fact, request, event, map entry, or recovery state is created

### agentic-loop capability

MODIFIED-copy the complete active requirement `Loop task, request, and tool work use only required correlation`.
Change its opening from dispatch-only ownership to every task producer. Add these scenarios verbatim while retaining
the unaffected request, execution, terminal-marker, and PubAck text:

#### Scenario: task identity is fixed before durable publication

- **GIVEN** first-party dispatch or rule production of a new TaskMessage
- **WHEN** the producer validates and marshals the registered envelope
- **THEN** LoopID is already a random version 4 UUID in canonical form
- **AND** retry of that same already-marshaled publication and downstream redelivery of its retained AGENT bytes reuse
  that TaskMessage identity
- **AND** a fresh execution of the upstream producer is outside this identity-reuse claim
- **AND** agentic-loop does not mint, derive, scan for, or separately persist a replacement identity

#### Scenario: task mapping is stable across process replacement

- **GIVEN** the exact registered bytes of a rule-produced TaskMessage have been retained
- **WHEN** agentic-loop is replaced and those bytes redeliver after any earlier loop birth evidence committed
- **THEN** the task still names exactly the producer-supplied LoopID
- **AND** recovery validates the same TaskID-to-LoopID mapping and cannot birth a second loop identity

### rule-agent-publishing capability

MODIFIED-copy `Publish-agent preserves the registered payload boundary`. Preserve all admission and Graphable
boundaries. Add that `publish_agent` mints a random v4 LoopID before TaskMessage validation, envelope marshal, and
publication. Add this scenario verbatim:

#### Scenario: publish-agent fixes new-loop identity before publication

- **GIVEN** a valid publish_agent action producing new loop work
- **WHEN** the action constructs its registered TaskMessage
- **THEN** it mints LoopID once before Validate and envelope marshal
- **AND** the durably published payload carries that canonical UUID
- **AND** no loop consumer is responsible for supplying an absent identity

### Tasks

Insert a prerequisite section immediately before task 4:

## 3A. Task producer identity prerequisite

- [ ] 3A.1 RED: prove TaskMessage validation refuses missing LoopID; dispatch and rule produce canonical v4 LoopID
  before marshal; direct HandleTask and durable intake create no state for absence; and governance preserves the field.
  Cite exactly `// spec: entity-id-contract / A loop instance token is minted at its framework birth seam`,
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`, and
  `// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary`.
- [ ] 3A.2 Implement producer-owned task identity. Keep dispatch's retained-byte path; mint rule LoopID once before
  Validate/marshal; require LoopID in TaskMessage.Validate and schema; remove TaskMessage-path fallback minting from
  preflight and HandleTask; validate the direct HandleTask seam. Add no exported helper, constructor, configurable
  generator, deterministic derivation, scan, map, ledger, bucket, or second owner. Do not alter tasks 9.5–9.7
  admission, subject coverage, classifier, registry, or PubAck contracts.
- [ ] 3A.3 GREEN: through real NATS, redeliver the exact rule-produced registered bytes across agentic-loop process
  replacement and prove one TaskID-to-LoopID mapping, one loop identity, no fallback mint, and loud missing-ID refusal.
  Run the affected unit/race tests and serialized `task e2e:agentic`; update generated schema, examples, and the
  beta.162-to-beta.163 migration note before task 4 review resumes.

Task 4.1 remains unchecked until 3A is implemented, independently reviewed, and its real-NATS proof is green. Tasks
9.5–9.7 remain the sole owners of rule admission and durable publisher selection.

## Adopter migration

This is a greenfield breaking change. Use fresh NATS state after all producers are updated; add no legacy reader,
empty-ID compatibility path, alias, online migration, or rollback mode. If retained deployed tasks are discovered,
stop for a separate owner-reviewed recovery design.

Migration note for direct producers:

1. For new loop work, call `uuid.NewString()` once when constructing TaskMessage and assign it to LoopID before
   Validate or marshal.
2. For continuation work, assign the admitted existing LoopID instead.
3. Retain the same serialized bytes when retrying that publication after an uncertain publish result; do not
   reconstruct the TaskMessage or regenerate LoopID for that retry.
4. Treat `loop_id required` as producer-invalid input, not a retryable consumer failure.

Observed adopter actions:

| Adopter | Required action |
|---|---|
| semdev coordinator intake | Add one producer-local v4 LoopID before validation/marshal |
| semteams chain decision handler | Add one producer-local v4 LoopID before validation/marshal |
| semspec lesson decomposer, QA reviewer, researcher manager, question tool | Add one producer-local v4 LoopID at each new-task construction |
| semmachina persona | Add LoopID before its existing Validate call |
| semsage UI API | Add producer-local LoopID; spawn executor already conforms and needs no change |
| semdragon quest bridge, DAG executor, and explore tool | Replace prefixed NUID birth tokens with canonical v4 UUIDs |
| semops, semsource, semconnect, semboids, semembed, semlink, semmem | No direct TaskMessage code migration found |

The framework-owned rule producer migration fixes every rule-JSON adopter without sister-repository code changes.
SemStreams records these instructions; sister repository owners implement and validate their own migrations.

## Decision-skill results

- `kv-or-stream`: no new communication path. Task work remains on the existing JetStream stream and identity remains
  in the registered retained TaskMessage bytes.
- `orchestration-check`: producer assignment, validation, and consumer refusal are component execution mechanics.
  They add no rule semantics, workflow layer, or lifecycle supervisor.
- `entity-or-bucket` is not triggered because the design adds no durable fact or bucket; the explicit outcome is to
  reject a second state owner.
- `new-payload` is not triggered because TaskMessage already exists and is registered.
- `query-pattern` is not triggered because no query seam is added.

## Owner docket

1. Approve or reject the semantic correction that a direct TaskMessage producer is the framework birth mint seam and
   uses producer-local `uuid.NewString()`, with no exported SemStreams helper or constructor.
2. Approve or reject making LoopID required for every TaskMessage, including the direct HandleTask seam, with loud
   refusal and no compatibility path.
3. Confirm the prerequisite lands and is reviewed before task 4 review resumes, while tasks 9.5–9.7 remain fenced.
