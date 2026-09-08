# Inventory addendum: sequential chat history handoff

base: af829616305afa039dac0550efa78d07e856dd5f

Scope: refresh the existing continuation and settlement inventories for one completed loop execution per chat turn,
with a later turn receiving the prior exchange after component replacement. This addendum inventories carriers and
existing owners only. It does not select a mechanism or authorize removal of same-running-loop attachment.
The earlier inventory checkpoint remains unchanged in `inventory-task4-continuation-response-proof-2026-09-08.md`.

## 1. Claimed gap

The current default submission path does not carry a completed conversation into a new loop.

- `processor/agentic-dispatch/component.go:1002` — `if msg.ReplyTo != "" {`
- `processor/agentic-dispatch/component.go:1003` — `loopID = msg.ReplyTo`
- `processor/agentic-dispatch/component.go:1005` — `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`
- `processor/agentic-dispatch/loop_tracker.go:212` — `if info := t.loops[loopID]; info != nil && !isTerminalState(info.State) {`
- `processor/agentic-dispatch/loop_tracker.go:220` — `if info := t.loops[loopID]; info != nil && !isTerminalState(info.State) {`
- `openspec/specs/agentic-dispatch/spec.md:199` — `- **THEN** the request is refused with the terminal reason, and no new loop is minted under that token`

Explicit ReplyTo names the existing execution and reaches terminal refusal after completion. Auto-continue excludes
terminal executions. With neither resolved, submission starts new work; the builder below contains no transcript handoff.

## 2. Current spellings and readers

### Inbound routing and lineage

- `agentic/user_types.go:46` — `ReplyTo          string            `json:"reply_to,omitempty"`           // loop_id if continuing`
- `agentic/user_types.go:49` — `ContextRequestID string            `json:"context_request_id,omitempty"` // links to assembled context`
- `agentic/user_types.go:66` — `InReplyTo string `json:"in_reply_to,omitempty"``
- `agentic/user_types.go:354` — `InReplyTo string `json:"in_reply_to,omitempty"``
- `processor/agentic-dispatch/component.go:926` — `Prompt:           msg.Content,`
- `processor/agentic-dispatch/component.go:930` — `ContextRequestID: msg.ContextRequestID,`
- `processor/agentic-dispatch/component.go:936` — `InReplyTo: msg.InReplyTo,`

ReplyTo selects an execution. InReplyTo records reply lineage; it does not load messages. UserMessage has no Messages
or embedded Context field. HTTPMessageRequest additionally has no ContextRequestID field; its complete declaration is
at `processor/agentic-dispatch/http.go:33–48`.

### Embedded context

- `agentic/user_types.go:358` — `Context *types.ConstructedContext `json:"context,omitempty"``
- `pkg/types/context.go:18` — `Content       string          `json:"content"`                  // Formatted context for LLM`
- `processor/agentic-loop/handlers.go:645` — `if task.Context != nil && task.Context.Content != "" {`
- `processor/agentic-loop/handlers.go:647` — `Role:    "system",`
- `processor/agentic-loop/handlers.go:648` — `Content: fmt.Sprintf("[Context]\n%s", task.Context.Content),`
- `processor/agentic-loop/handlers.go:976` — `Content: task.Context.Content,`

This consumed carrier represents formatted context as a system message, not a role-preserving transcript.
ConstructedContext also carries token count, entity IDs, sources, and construction time; it contains no ChatMessage
sequence. Dispatch's shared builder does not populate it.

### ContextRequestID is a link, not a loader

- `agentic/user_types.go:361` — `ContextRequestID string `json:"context_request_id,omitempty"``
- `processor/agentic-loop/handlers.go:602` — `ContextRequestID: task.ContextRequestID,`
- `processor/agentic-dispatch/loop_tracker.go:489` — `if info.ContextRequestID == "" && contextRequestID != "" {`
- `processor/agentic-dispatch/loop_tracker.go:490` — `info.ContextRequestID = contextRequestID`
- `processor/agentic-dispatch/task_recovery.go:206` — `case task.ContextRequestID != msg.ContextRequestID:`
- `processor/agentic-dispatch/task_recovery.go:207` — `return fmt.Errorf("retained context_request_id does not match source")`

`gopls references agentic/user_types.go:361:2` returned RuleFields, the dispatch builder, dispatch recovery comparison,
and LoopCreatedEvent construction. No loop hydration reader is among those references. UserMessage and LoopInfo field
references likewise contain forwarding, tracking, comparison, rule projection, and tests.

### Retained request and response carry the exchange

- `agentic/types.go:112` — `Messages    []ChatMessage    `json:"messages"``
- `agentic/types.go:170` — `RequestID    string      `json:"request_id"``
- `agentic/types.go:173` — `Message      ChatMessage `json:"message,omitempty"``
- `processor/agentic-loop/settlement_recovery.go:146` — `func (c *Component) readRetainedAgentRequest(`
- `processor/agentic-loop/settlement_recovery.go:162` — `evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:198` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-loop/settlement_recovery.go:214` — `evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:228` — `if !ok || evidence.subject != subject || response.RequestID != requestID {`
- `processor/agentic-loop/state.go:353` — `for _, msg := range request.Messages {`
- `processor/agentic-loop/state.go:360` — `if err := cm.AddMessage(region, msg); err != nil {`

The exact reads resolve the latest request for a LoopID and response for a RequestID. A correlated final request and
response contain the conversation sent to the provider and its subsequent answer. Current restoration uses them for
the same execution, not a new chat turn.

Retained requests also contain execution-specific instructions; a role-preserving copy is not automatically a clean
conversation transcript:

- `processor/agentic-loop/handlers.go:304` — `prefix := []agentic.ChatMessage{BuildIterationBudgetMessage(iteration, maxIterations)}`
- `processor/agentic-loop/handlers.go:305` — `if todoMsg := h.maybeBuildTodoMessage(ctx, loopID); todoMsg.Content != "" {`
- `processor/agentic-loop/handlers.go:308` — `return append(prefix, messages...)`
- `processor/agentic-loop/handlers.go:784` — `content = fmt.Sprintf("[Iteration Budget] Iteration %d of %d (%d%% used). Budget nearly exhausted — finalize and submit your work now.", iteration, maxIterations, pct)`

The reviewer independently traced prefix callers on initial requests, retries, tool follow-ups, and cold task
reconstruction. Existing restoration places system messages into RegionSystemPrompt. Thus retained Messages mixes
conversation with per-execution budget/todo instructions; the role alone cannot distinguish the two.

- `processor/agentic-loop/handlers.go:1275` — `_ = cm.AddMessage(RegionRecentHistory, response.Message)`
- `processor/agentic-loop/trajectory_handler_wiring.go:68` — `_ = c.handler.loopManager.DeleteLoop(loopID)`

The final assistant message enters process context before terminal release. That context is not another durable owner.

### Completed exchange and user-answer projection

- `agentic/events.go:65` — `Result       string    `json:"result"``
- `agentic/events.go:66` — `Prompt       string    `json:"prompt,omitempty"` // Original user task prompt; enables NL/BM25 search`
- `processor/agentic-loop/handlers.go:2158` — `Result:       responseContent,`
- `processor/agentic-loop/handlers.go:2159` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`
- `processor/agentic-loop/component.go:2130` — `data, err := json.Marshal(completion)`
- `processor/agentic-loop/component.go:2136` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2137` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:129` — `content = event.Result`
- `processor/agentic-dispatch/terminal_settlement.go:130` — `if decision := event.Decision; decision != nil && agentic.IsUserFacingDecideAction(decision.Action) {`
- `processor/agentic-dispatch/terminal_settlement.go:133` — `content = decision.Reason`
- `processor/agentic-dispatch/terminal_settlement.go:150` — `InReplyTo:   event.LoopID,`
- `processor/agentic-dispatch/terminal_settlement.go:152` — `Content:     content,`

LoopCompletedEvent is another existing completed-exchange carrier. Loop publishes it and persists its complete JSON
under COMPLETE_<LoopID>. Dispatch's terminalResponse projects the terminal into UserResponse.Content; a user-facing
decision uses Decision.Reason rather than Result. Provider response, terminal result, and delivered user answer are
distinct facts. The handoff inventory does not select among them.

## 3. Adjacent claims

- `openspec/specs/agentic-loop/spec.md:694` — `Task intake MUST use that distinction. A task carrying a loop token that already names a registered loop is a`
- `openspec/specs/agentic-loop/spec.md:695` — `**continuation**: intake attaches to the existing loop and MUST reuse its context manager, so the conversation`
- `openspec/specs/agentic-loop/spec.md:698` — `A continuation whose existing loop is in a terminal state MUST be refused rather than attached, and MUST NOT`
- `openspec/specs/agentic-loop/spec.md:719` — `a conversation whose process was replaced is explicitly NOT in scope and is claimed separately (#1146).`

The current spec preserves live attachment and separately assigns replacement recovery to #1146. The existing
`design-task4-continuation-response-proof-2026-09-08.md` proposes removing attachment; that proposal is not implementation
authority under the owner reset. #1244 remains adjacent transition work, not a prerequisite established here.

Existing downstream use is concrete; sister repositories were read only:

- `/Users/coby/Code/c360/semspec/processor/qa-reviewer/component.go:563` — `Context: &agentic.ConstructedContext{`
- `/Users/coby/Code/c360/semspec/processor/qa-reviewer/component.go:564` — `Content: assembled.SystemMessage,`
- `/Users/coby/Code/c360/semdragon/processor/questbridge/handler.go:324` — `Context: &pkgtypes.ConstructedContext{`
- `/Users/coby/Code/c360/semdragon/processor/questbridge/handler.go:325` — `Content:       contextContent,`
- `/Users/coby/Code/c360/semspec/ui/src/lib/stores/context.svelte.ts:30` — `const response = await api.context.get(requestId);`

The SemSpec context store describes ContextBuildResponse retrieval. That is a product context-build viewer; this sweep
established no SemStreams operational transcript loader behind ContextRequestID.

## 4. Consumer at birth

No new symbol, field, subject, or bucket is proposed by this inventory. Present consumers exist for TaskMessage.Context
in loop initial-message assembly, ContextRequestID in dispatch/created projections, AgentRequest.Messages in provider
calls and loop restoration, and InReplyTo in reply-lineage projection. They constrain reinterpretation of these fields.

## 5. Problem shape and tests

The shape is carrying existing conversation material into the next bounded execution, with exact retained-output
read-through after replacement. The provider settlement path independently implements exact correlated output reuse:

- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-model/provider_settlement.go:113` — `if evidence.subject != subject || response.RequestID != requestID {`

Tests prove separate parts:

- `processor/agentic-loop/handlers_internal_test.go:83` — `func TestBuildInitialMessages_SystemBeforeContext(t *testing.T) {`
- `processor/agentic-loop/handlers_internal_test.go:104` — `assert.Equal(t, "system", messages[1].Role)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:22` — `func TestIntegrationTaskAndResponseSettleAcrossProcessReplacement(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_integration_test.go:63` — `taskDecision, err := taskProcess.handleTaskMessage(ctx, taskData)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:76` — `responseDecision, err := newProcess().handleResponseMessage(ctx, responseData)`
- `processor/agentic-model/provider_settlement_integration_test.go:171` — `func TestIntegrationMatchingRetainedResponseSkipsProviderAndAcknowledgesSource(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:197` — `func TestIntegrationTypedAbsenceInvokesProviderAndPubAckPrecedesSourceAck(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:383` — `func TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:68` — `func TestPersistHandlerResultReturnsPublicationFailureAndDiscardsSpeculativeTerminalState(t *testing.T) {`

The loop integration test preloads durable request/state and calls handlers directly. The matching-response model
test preloads the response. Neither establishes dispatch → fake model → first answer → replacement → follow-up history,
nor the exact live crash-after-response-PubAck-before-source-ACK sequence.

## Existing owners and collisions

| Dimension | Existing ownership and evidence |
|---|---|
| Semantic class | Conversation input, current execution, and exact retained provider output. |
| Owners | Dispatch routes submission and projects terminal user answers; loop assembles requests and execution prefixes, owns ContextManager and completed prompt/result; model writes provider output; product context builders own ConstructedContext content. |
| Catalogs | Existing UserMessage, TaskMessage, AgentRequest, AgentResponse, LoopCreatedEvent, LoopCompletedEvent, UserResponse and LoopInfo fields; no new catalog proposed. |
| Status | Active lookup excludes terminal state (`loop_tracker.go:212`, `:220`); terminal rejection is specified at `agentic-dispatch/spec.md:199`. |
| Lifecycle | Process context releases at `trajectory_handler_wiring.go:68`; retained-window evidence is in the original inventory §2.11. No retention re-audit performed. |
| Ownership | Loop and provider owners check exact response identity (`settlement_recovery.go:228`; `provider_settlement.go:113`). |
| Readers | Provider clients, same-loop restoration, dispatch terminal-to-user projection, product context viewer. |
| Writers | Dispatch builds TaskMessage and UserResponse; products embed context; loop writes AgentRequest and LoopCompletedEvent (including COMPLETE_ KV); provider owner writes AgentResponse. |
| Recovery | Exact request/response reads and same-loop restoration. No cross-turn loader found through the enumerated context fields. |

## Adopter seam

Specific adopter: a developer building a chat channel on UserMessage or HTTP dispatch who has never opened the loop.
They must distinguish three facts: ReplyTo selects an execution; InReplyTo records lineage; ContextRequestID does not
hydrate history. More than two correctness facts is a seam finding.

After an answer, default auto-continue excludes the completed execution and the next task contains only new content.
Echoing that execution through ReplyTo returns terminal refusal. Setting ContextRequestID propagates a link without
loading history. Terminal refusal is a runtime response. Missing history has no dedicated error in the inspected
builder; it is visible through model behavior or implementation/docs. The desired knowledge is the conversation they
mean to continue, without predicting process memory, retained subjects, request identities, or lifecycle timing.

## Searches and open evidence

Structural searches used gopls workspace_symbol for ContextRequestID, UserMessage, ConstructedContext, GetActiveLoop,
TestBuildInitialMessages, ContextRequest, retrieveContext, and readRetainedAgentResponse; field references covered
TaskMessage.Context, all ContextRequestID carriers, and UserMessage.ReplyTo. retrieveContext produced no relevant
declaration. Exact TaskMessage.ContextRequestID reference query: `gopls references agentic/user_types.go:361:2`.

String searches:

- `git grep -n -E 'context_request_id|in_reply_to|reply_to|conversation.history|conversation_history|chat.history|chat_history' -- agentic processor/agentic-loop processor/agentic-dispatch openspec/specs docs/adr`
- `git grep -n -i -E 'retrieve.{0,20}context|context.{0,20}retriev|context_request_id|context\.get|context\.query|context\.response' -- '*.go'`
- `git grep -n -i -E 'sequential|multi.turn|follow.up|prior exchange|previous turn' -- processor/agentic-loop processor/agentic-dispatch processor/agentic-model`
- Sister searches: `git grep -n -E 'context_request_id|ContextRequestID|ReplyTo:|reply_to|ConstructedContext' -- service processor ui`

The full supported-path acceptance remains unproven. Retention absence during a requested cross-turn handoff has no
established behavior in this addendum. No mechanism is selected. Stop for independent INVENTORY PASS.
