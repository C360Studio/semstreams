# R8 live-attachment retirement — supplemental inventory

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

## Baseline and scope

Read-only inventory of claim `codex/gh1146-agentic-loop-restart`, HEAD
`68c14c8eb25c512e988f740cbf7ea14b6815976f`, including preserved task-reuse WIP. No files, tests, Git state,
or external records changed.

Owner ruling: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5728438234.
This supplement inventories retirement surfaces; it does not lower the decision into implementation.

Accepted inventories `fb937d0e…`, `cd9c12ae…`, `bb968d55…`, `53fe8a90…`, and `b51881ee…` remain unchanged evidence.
Their retention, storage, provider, approval, and refusal-carrier findings are not re-enumerated here.

## Surface inventory

### Configuration and implicit targeting

- `processor/agentic-dispatch/config.go:14` — `AutoContinue               bool                  `json:"auto_continue" schema:"type:bool,description:Automatically continue last active loop,default:false,category:basic"` // Continue last loop if explicitly enabled`
- `processor/agentic-dispatch/config.go:102` — `AutoContinue: false,`
- `processor/agentic-dispatch/component.go:788` — `} else if c.config.AutoContinue {`
- `processor/agentic-dispatch/http.go:254` — `} else if c.config.AutoContinue {`
- `processor/agentic-dispatch/http.go:950` — `AutoContinue bool   `json:"auto_continue"``
- `processor/agentic-dispatch/http_activity.go:311` — `func (c *Component) activeLoop(ctx context.Context, msg agentic.UserMessage) (string, error) {`
- `processor/agentic-dispatch/http_activity.go:320` — `entity.UserID != msg.UserID || entity.ChannelType != msg.ChannelType || entity.ChannelID != msg.ChannelID {`
- `processor/agentic-dispatch/component.go:216` — `if err := json.Unmarshal(rawConfig, &config); err != nil {`

AutoContinue has four production selection callers: USER command, USER task, HTTP command, HTTP task. `activeLoop`
has exactly those four production references. DebugConfig also exposes the setting. The projection itself has retained
listing/debug/SSE callers; retirement of AutoContinue does not imply retirement of that projection.

Generated/configuration occurrences:
`schemas/agentic-dispatch.v1.json:8`; `specs/openapi.v3.yaml:1076,1087`;
`configs/agentic.json:127`;
`configs/examples/research-graph-pipeline.json:125`;
`configs/research-graph-e2e.json:125`;
`configs/flows/{crud-tools-test:102,deep-research-test:113,deep-research:103,lesson-example:100,ops-agent-test:102,ops-agent:104}.json`.
All nine shipped JSON declarations currently set false.

The constructor uses permissive `json.Unmarshal`; deleting a Go field alone would not establish constructor rejection
of a supplied obsolete key. This is an observed decoding seam, not a recommendation for a compatibility reader.

### Submission targeting versus retained controls and lineage

- `agentic/user_types.go:49` — `ReplyTo          string            `json:"reply_to,omitempty"`           // loop_id if continuing`
- `processor/agentic-dispatch/http.go:39` — `ReplyTo     string            `json:"reply_to,omitempty"``
- `processor/agentic-dispatch/http.go:157` — `ReplyTo:       req.ReplyTo,`
- `processor/agentic-dispatch/component.go:934` — `if msg.ReplyTo != "" {`
- `processor/agentic-dispatch/http.go:351` — `if msg.ReplyTo != "" {`
- `processor/agentic-dispatch/loop_admission.go:106` — `loopOpContinue = "continue"`
- `processor/agentic-dispatch/loop_admission.go:265` — `case loopOpContinue:`
- `agentic/rule_fields.go:419` — `putString(fields, "reply_to", m.ReplyTo)`
- `agentic/user_types.go:109` — `return json.Unmarshal(data, (*Alias)(m))`
- `processor/agentic-dispatch/http.go:124` — `if err := json.NewDecoder(r.Body).Decode(&req); err != nil {`

Both submission paths resolve explicit ReplyTo, otherwise AutoContinue, only after exact retained-task absence.
Existing attachment admission checks ownership/terminality and rejects supplied PriorMessages. USER refusals use
registered UserResponse; HTTP refusals return synchronously. UserMessage also exposes ReplyTo to rules.

**Naming correction:** HTTP `/message` has `reply_to`, not a `loop_id` request field. Current command targeting uses
the first command argument, not UserMessage.ReplyTo:

- `processor/agentic-dispatch/component.go:787` — `loopID = args[0]`
- `processor/agentic-dispatch/http.go:253` — `loopID = args[0]`
- `processor/agentic-dispatch/commands.go:104` — `Content:     "An explicit loop_id is required. Use /cancel <loop_id>.",`
- `processor/agentic-dispatch/commands.go:116` — `Operation: loopOpCancel,`
- `processor/agentic-dispatch/commands.go:147` — `LoopID:      targetLoopID,`
- `processor/agentic-dispatch/http.go:104` — `mux.HandleFunc("GET "+prefix+"loops/{id}", c.handleGetLoop)`
- `processor/agentic-dispatch/http.go:105` — `mux.HandleFunc("POST "+prefix+"loops/{id}/approval", c.handleLoopApproval)`
- `processor/agentic-dispatch/http.go:747` — `Operation: loopOpApprove,`
- `agentic/user_types.go:126` — `LoopID      string    `json:"loop_id"``
- `agentic/user_types.go:359` — `InReplyTo string `json:"in_reply_to,omitempty"``

Cancel, status/read, HTTP approval, UserSignal.LoopID, and response InReplyTo are separate surfaces.
Task/UserMessage `RunID`, `InReplyTo`, and `ParentLoopID` describe run/reply/parent lineage, not live-execution
attachment. Their misleading historical “paused run” comments are not evidence that these fields implement AutoContinue.

USER and HTTP payload decoding are permissive. Removing `reply_to` from exported structs alone could make old
serialized input silently ignored. Existing raw-wire refusal behavior after retirement therefore remains an explicit
lowering obligation.

### Dispatch identity and producers

- `processor/agentic-dispatch/task_recovery.go:103` — `if err := validateRetainedDispatchTask(retained, msg, taskID, msg.ReplyTo); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:118` — `if loopID == "" {`
- `processor/agentic-dispatch/task_recovery.go:119` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:203` — `case requestedLoopID != "" && task.LoopID != requestedLoopID:`
- `processor/agentic-dispatch/component.go:982` — `// Validated retained task evidence already proves this publication committed.`
- `processor/agentic-dispatch/http.go:392` — `// Validated retained task evidence already proves this publication committed.`
- `processor/rule/actions.go:1721` — `LoopID:       uuid.NewString(),`
- `agentic/user_types.go:317` — `LoopID          string `json:"loop_id"` // producer-minted loop identity`
- `agentic/user_types.go:414` — `return fmt.Errorf("loop_id required")`
- `agentic/user_types.go:574` — `return json.Unmarshal(data, (*Alias)(t))`

The dispatch task builder accepts a caller-selected LoopID; its two production callers are USER and HTTP task
submission. The rule constructor already mints a fresh UUID. The four static rule configurations from prior
inventories are not four independent task builders.

The reviewed task-reuse correction remains: exact validated retained task commitment skips task republication while
preserving each caller's response behavior. Removing attachment-specific comparison is distinct from weakening
source/task/prompt/history/run/reply correlation.

TaskMessage has no birth-versus-attachment discriminator. Its present validation verifies required fields, token
forms, history and task options; it does not establish whether an otherwise valid LoopID was previously used.

### Loop-local rebind versus same-task recovery

- `processor/agentic-loop/state.go:202` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:213` — `if _, exists := m.loops[loopID]; exists {`
- `processor/agentic-loop/state.go:275` — `func (m *LoopManager) attachContinuation(task agentic.TaskMessage) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/state.go:309` — `entity.TaskID = task.TaskID`
- `processor/agentic-loop/handlers.go:877` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:878` — `if existingID != task.LoopID {`
- `processor/agentic-loop/handlers.go:912` — `entity, err = h.loopManager.attachContinuation(task)`
- `processor/agentic-loop/handlers.go:954` — `if continuation {`
- `processor/agentic-loop/handlers.go:988` — `if !continuation {`
- `processor/agentic-loop/handlers.go:1018` — `if continuation {`
- `processor/agentic-loop/component.go:1329` — `if errors.Is(err, ErrLoopBusy) {`

`attachContinuation` has one production caller. It rebinds TaskID while preserving context and other state;
terminal/pending-tool/approval cases refuse. Its dedicated ErrLoopTerminal/ErrLoopBusy declarations are at
state.go:40,52. ErrLoopBusy's production special settlement branch is only component.go:1329.
ErrLoopAlreadyExists remains the distinct create-without-overwrite result.

Attachment-conditioned work in HandleTask spans trajectory reuse, assembled-system-prompt suppression,
existing-context selection and iteration reuse. Internal model/tool/approval continuation is separate; the presence
of “continuation” in a name or comment is not sufficient evidence for removal.

- `processor/agentic-loop/settlement_recovery.go:388` — `if !found {`
- `processor/agentic-loop/settlement_recovery.go:391` — `if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1310` — `if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {`
- `processor/agentic-loop/component.go:1316` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/settlement_recovery.go:1045` — `return natsclient.DeliveryDecisionQuarantine`
- `processor/agentic-loop/settlement_recovery.go:1047` — `return natsclient.DeliveryDecisionTerminate`

Current actual intake distinguishes:

1. Invalid payload/token: Terminate before task effects.
2. Same active TaskID and same LoopID: dedup/pending-output path.
3. Same TaskID mapped to another local LoopID: fatal correlation conflict.
4. Retained authority with different TaskID/role/model: fatal conflict; Quarantine.
5. Matching terminal authority: ACK without new request.
6. Matching nonterminal authority: retained-request recovery or current reconstruction path.
7. Authority absent: current birth path; absence alone does not identify expired prior use.

The last case is not an inventory finding that eternal duplicate detection is required. The accepted contract
explicitly excludes indefinite deduplication of arbitrary resubmission after evidence expiry. Existing missing-request
RED and finite supported-path proof remain separate R8 obligations.

## Existing tests and documentation affected

Attachment-specific tests occur in dispatch `loop_admission_test.go`, `loop_seams_test.go`, `loop_token_test.go`,
`loop_owner_test.go`, `loop_projection_test.go`, `loop_projection_integration_test.go`, `prior_messages_test.go`, and
attachment modes in `restart_identity_integration_test.go`. Public-field fixtures also occur in
`agentic/{reply_to,user_types}_test.go` and dispatch `build_task_message_test.go`.

Loop attachment tests: `create_vs_exists_fence_test.go` at 183,263,306,358,418,499,550;
`prior_messages_test.go:109`. The create-collision/form tests in the same file are distinct retained behavior.

Retained acceptance anchors:

- `processor/agentic-dispatch/sequential_chat_integration_test.go:35` — `func TestIntegrationSequentialChatAfterComponentReplacement(t *testing.T) {`
- `processor/agentic-dispatch/sequential_chat_integration_test.go:179` — `require.False(t, DefaultConfig().AutoContinue, "ordinary submissions must be independent under defaults")`
- `processor/agentic-loop/settlement_recovery_test.go:170` — `func TestColdTaskRedeliveryConflictingMappingQuarantines(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:214` — `func TestColdTaskRedeliveryWithTerminalAuthorityCreatesNoRequest(t *testing.T) {`

Sequential-chat integration already exercises fresh-run history after component replacement; its AutoContinue field
assertion is a mechanical dependency, not the behavior being retired.

Active contract claims needing reconciliation are catalogued, not changed:

1. Current loop spec requirement at `openspec/specs/agentic-loop/spec.md:681` explicitly requires create refusal
   followed by live attachment; its scenarios extend through 786.
2. Current dispatch spec continuation/ownership clauses occur at 97–110,140,164,198.
3. Change dispatch delta contains AutoContinue/attachment requirements at 11–17,43–55,139–150,215–241,313–343.
4. Change loop delta has the producer-continuation clause at 392–393 and direct create/attach wording at 1013;
   approval continuation at 509 onward is distinct.
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:878` — `deduplication guarantee for arbitrary caller resubmission after evidence expiry.`
5. Current proposal occurrences: 21,33,65,87,178–180. Design occurrences: 89–106,532,565–573,636,843,1045,1278,1312,1334.
6. User docs: dispatch README:17,26,44,50–64; doc.go:6; concepts/13-agentic-systems.md:198–201;
   migration guide:1064–1066,1336–1340,1382,1406–1436.
7. Pattern-adoption guide:79 and historical skill example `.claude/skills/semstreams-dev/SKILL.md:132` also mention
   AutoContinue. Dated evidence/archived designs remain provenance, not current instructions to preserve attachment.

### Entity-ID contract correction after independent inventory review

The retirement also reaches the `entity-id-contract` family. Its active delta and base spec explicitly describe
live attachment; omitting them from the original contract catalog was an inventory omission.

Active change delta:

- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:9` — `producer MUST echo the admitted existing LoopID. End-user input, configuration, and tool-call arguments MUST NOT`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:16` — `that authors a fresh canonical UUID and supplies it as `reply_to` is ACCEPTED. Producer-local minting is therefore`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:38` — `- Dispatch MUST refuse a non-canonical resolved continuation token, `run_id`, or `in_reply_to` on an inbound`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:40` — `published to the response subject on the channel path — validating after auto-continue resolution and BEFORE`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:86` — `#### Scenario: a non-canonical reply_to fails at the client boundary on both intake paths`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:110` — `- **GIVEN** a client submitting a message whose `reply_to` is a canonical UUID that this framework never minted`

The attachment-specific blocks are lines 8–9,16–20,38–43,86–94,108–117. The new-conversation scenario at 59–64 and
retained run/reply-lineage validation scenario at 96–106 also use `reply_to` absence as a premise. Their independent
UUID-minting and `run_id`/`in_reply_to` validation obligations are distinct from attachment retirement.

Base capability:

- `openspec/specs/entity-id-contract/spec.md:661` — `that authors a fresh canonical UUID and supplies it as `reply_to` is ACCEPTED. "Author no token" is therefore the`
- `openspec/specs/entity-id-contract/spec.md:680` — `- Dispatch MUST refuse a non-canonical resolved continuation token, `run_id`, or `in_reply_to` on an inbound`
- `openspec/specs/entity-id-contract/spec.md:685` — `and an auto-continued value pass one check.`
- `openspec/specs/entity-id-contract/spec.md:716` — `#### Scenario: a non-canonical reply_to fails at the client boundary on both intake paths`
- `openspec/specs/entity-id-contract/spec.md:740` — `- **GIVEN** a client submitting a message whose `reply_to` is a canonical UUID that this framework never minted`

Corresponding base blocks are lines 661–665,680–685,716–724,738–747; scenarios at 699–704 and 726–736 likewise
mention `reply_to` absence. These are active capability claims, not merely archived evidence.

Retained neighboring requirements remain separately catalogued: TaskMessage token validation, CreateLoopWithID form
refusal, user control/approval token validation, HTTP path-token validation, canonical form versus provenance, and
the independent authorization limitation. No weakening or new token scheme is inferred.

## Adopter seam inventory

| Adopter | Present dependency / no-change consequence | Discovery and boundary |
|---|---|---|
| JSON operator | Explicit AutoContinue config exists, including false. Current constructor permissively decodes unknown keys; deletion alone does not prove rejection. | Generated component schema, runtime refusal and SemStreams migration guide must agree; exact retirement handling awaits lowering. |
| HTTP/channel adapter | `reply_to` selects an existing execution today. Ordinary omitted-target submissions already create independent turns. | HTTP schema/README, registered UserMessage and migration notes. Old serialized targeting must not be silently described as preserved attachment. |
| Go component author | Exported Config.AutoContinue and UserMessage.ReplyTo are source references; raw TaskMessage has only LoopID/TaskID, no attachment intent field. | Compiler/API docs and task contract. No new field or compatibility contract is assumed. |
| Control caller | `/cancel <id>`, `/status <id>`, UserSignal.LoopID and HTTP approval use separate targets. AutoContinue additionally supplies implicit command targets today. | Explicit-target commands/control endpoints remain distinct; removal of implicit targeting must be stated. |
| Chat adapter | PriorMessages is caller-supplied displayed user/assistant text, not a request to restore another execution. | Existing sequential-chat example and integration fixture already establish this product seam. |

Read-only downstream checkouts:

1. SemTeams `ce22c961`: `configs/flow-bootstrap.json:610` and `configs/e2e-flow-bootstrap.json:593` set AutoContinue
   true; generated schema/OpenAPI/types expose it and ReplyTo. Its actual `ui/src/lib/services/agentApi.ts:301–303`
   sends content plus optional `run_id`/`in_reply_to`, not `reply_to`.
2. SemDev `ca3956a`: `configs/semdev-bootstrap.json:693` and `configs/semdev-live-gemini.json:713` set true.
3. SemSpec `5a9496ee`: thirteen configs explicitly set false; generated type exposes both fields.
   `workflow/task.go:49` has an unrelated workflow AutoContinue field, not this dispatch setting.
4. SemStreams UI `39f5f04`: mirrored component schema, OpenAPI and generated types expose both fields.
5. SemSource `4093d3c` and SemOps `602c619`: no matches in the stated tracked-code/config search, not proof about
   external deployments.
6. Raw TaskMessage construction sites exist in SemTeams chainpause, SemDev intake, and SemSpec lesson-decomposer,
   qa-reviewer, researcher-manager and question tool. Inspected literals do not establish an attachment dependency;
   several already omit the producer LoopID required by this branch. That prior migration issue is not newly caused
   by attachment retirement and was not expanded here.
7. Sister repositories were not modified; migration implementation and validation remain their owners' responsibility.

## Searches and structural limits

Searches were executed in the claim unless a checkout is named:

1. `rg --files openspec/changes/agentic-loop-restart-safety | rg '(inventory|docket|decision|retire|migration|proposal|design|tasks)'`.
2. `rg -n 'AutoContinue|auto_continue|func .*activeLoop|func .*attachContinuation|func .*admitLoopRequest' processor/agentic-dispatch processor/agentic-loop schemas`.
3. `rg -n '5728438234|retir|attachment|ff890002' .../tasks.md`.
4. `git grep -n -E 'AutoContinue|auto_continue|attachContinuation|loopOpContinue' -- ':!openspec/changes/agentic-loop-restart-safety/*' ':!go.sum'`.
5. `git grep -n -E 'ReplyTo|LoopID|processCommandSync|handleCommand|activeLoop'` over dispatch http/component/http_activity/loop_admission.
6. `gopls references` at config.go:14:2, state.go:275:23, http_activity.go:311:21, user_types.go:49:2 and task_recovery.go:112:21.
   Initial sandbox query failed workspace loading; repeated read-only escalated queries succeeded. Default build-tag
   references do not enumerate integration-tag fixtures.
7. `git grep -n -E 'ReplyTo|reply_to|ActionAgentRequest|agent_request'` over dispatch commands/command_registry/builtin_commands/intent_classifier/config/loop_admission;
   nonexistent optional paths supplied no evidence.
8. `rg -n 'func |case loopOp'` on loop_admission; `rg -n 'ErrLoopAlreadyExists|ErrLoopBusy|ErrLoopTerminal' processor/agentic-loop --glob '*.go'`.
9. `rg -n '^func Test.*(Continu|Attach|Dedup|Redeliver|SameTask|Conflict|Prior|Terminal|Busy|Create)'` over state/handlers/create_vs_exists/settlement_recovery tests;
   `rg -n '^func Test'` over named dispatch tests and loop prior_messages tests.
10. `rg -n 'AutoContinue|auto_continue|attachContinuation|loopOpContinue|reply_to|ReplyTo'` over current proposal/design and current/base dispatch/loop specs.
11. `git grep -n -E 'agent_request|ReplyTo|reply_to' -- processor/agentic-dispatch '*.go' ':!*_test.go' ':!openspec/**' ':!docs/**'`
    excluding previously read dispatch http/component/loop_admission and user_types.
12. `git grep -n 'auto_continue' -- configs schemas specs processor/agentic-dispatch/README.md docs/concepts/13-agentic-systems.md`.
13. `rg -n 'func loopSettlementDecision|ErrorInvalid|ErrorFatal'` over settlement files; nonexistent delivery.go yielded
    no evidence. `rg -n 'func .*UnmarshalJSON|json.Unmarshal\(data' agentic/user_types.go`.
14. `rg -n` for continuation/attachment requirement headings in both loop specs, and AutoContinue/attachment/ReplyTo in
    dispatch delta; `git grep -l -E 'AutoContinue|auto_continue|attachContinuation|loopOpContinue'` over current
    production/docs/config/schema paths.
15. Read-only `git grep -n` in the six named sisters for AutoContinue/auto_continue/reply_to/ReplyTo over tracked
    Go/JSON/YAML/TS/Svelte; repeated with narrowed pattern `AutoContinue|auto_continue|(^|[^n_])reply_to|\.ReplyTo` to
    separate lineage and record checkout identities.
16. In the same six sisters, `git grep -n -E 'agentic\.TaskMessage\{|LoopID:|loop_id:|loopId:'` over tracked Go and
    service sources, excluding tests/generated types; inspected returned constructor/adapter sites.
17. Supplemental bounded OpenAPI/config reference reads using `rg` with head/tail were navigational only, not
    completeness evidence. All cited facts were read directly with numbered source ranges.
18. Reviewer-identified contract correction only:
    `nl -ba openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md | sed -n '1,120p'` and
    `nl -ba openspec/specs/entity-id-contract/spec.md | sed -n '653,750p'`.

**Inventory handoff only.** No new payload, intent field, compatibility reader, storage, TTL policy, or expired-identity
detector is proposed. Runtime/source retirement and exact raw-wire refusal behavior await independent inventory review
and subsequent bounded lowering.
