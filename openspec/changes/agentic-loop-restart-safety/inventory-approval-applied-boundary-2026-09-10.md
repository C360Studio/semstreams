# Inventory: approval input correlation after durable application

base: 615997c658ef4c5c4ce2e8a44709913c5b6d70fe

Frozen staged parent: `417beae5552f8f15ad3540edd7d8504c87174c13`.

## Scope and checkpoint

This refresh inventories how a redelivered `ApprovalResponse` identifies its pending execution, what execution evidence
survives application, and what is currently established when provider CallID repeats. It adds no target state,
mechanism, public field, store, or implementation task.

The refresh reuses the accepted change inventories and their established settlement ownership. It updates the live
approval and execution-evidence surface at the baseline above. Historical pins are not represented as current pins.

Production files remained unchanged during this investigation. The root session owns the task-document changes;
other agents own the two modified approval tests. No tests, Docker operations, Git mutations, or downstream writes
were performed here.

The `orchestration-check` skill was applied to classify the existing ownership: approval execution state remains
component-owned; the inventory introduces no rule, lifecycle, workflow, or communication owner. No new communication,
durable fact, query surface, or payload is proposed.

## 1. Claimed gap: applied approval redelivery reaches an unimplemented guard

The independently reviewed native reproduction proves that the same approval source can redeliver after real
application and completion while the current handler returns Retry. It does not establish missing storage.

Evidence file:

`/private/tmp/gh1146-approval-applied.k5BLPg/native-applied-red.log`

SHA-256: `f96e5f8a475ef7c1a48ac21030f12af8eb38312b5593644296ef271268ce056e`.

The log records:

1. Line 128: before replacement, source sequence 8, delivery count 1, ACK pending, final LoopEntity revision 7,
  one executor invocation, two provider calls, and complete state.
2. Line 129: original pending LoopEntity, original AgentRequest, originating AgentResponse, and submitted approval.
3. Line 130: the latest AgentRequest contains the original assistant call stamped with RequestID, ExecutionID,
  ordinal, CallID, tool name and arguments, followed by the matching tool message.
4. Lines 163–173: the replacement receives source sequence 8 with delivery count 2 and records ACK=0, NAK=1,
  TERM=0; the expected ACK assertion fails.

The fixture’s current `PendingToolResults` contains the successful result under the same ExecutionID after
`PendingApproval` clears. This is positive execution evidence in this fixture. General selection under later
RequestIDs, repeated CallIDs, multiple decisions, or later approvals remains unproven.

Current source explains why that evidence is not consulted:

- `processor/agentic-loop/approval_response_handler.go:193` — `persisted, revision, err := c.readLoopEntityRevision(ctx, response.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:197` — `if revision == 0 || persisted.State != agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/approval_response_handler.go:199` — `fmt.Errorf("approval continuation for loop %q has no proven current pending state", response.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:205` — `if needsRecovery {`
- `processor/agentic-loop/approval_response_handler.go:206` — `if err := c.recoverApprovalResponse(ctx, response, persisted); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:223` — `if result.staleDrop {`
- `processor/agentic-loop/approval_response_handler.go:225` — `fmt.Errorf("approval for loop %q lacks durable branch-applied proof", response.LoopID)`

The non-awaiting guard precedes the recovery read. Its failure therefore cannot measure whether retained
request/result evidence could establish application.

## 2. Existing spellings, producers, consumers, and retained fields

### Public approval carriers

`ApprovalResponse` carries LoopID and provider CallID, a decision, optional modified arguments and reason,
approver identity, and DecidedAt. Its declared fields contain no RequestID, ExecutionID, or ordinal.

- `agentic/approval.go:104` — `type ApprovalResponse struct {`
- `agentic/approval.go:105` — `LoopID   string `json:"loop_id"``
- `agentic/approval.go:106` — `CallID   string `json:"call_id"``
- `agentic/approval.go:110` — `ModifiedArguments map[string]any `json:"modified_arguments,omitempty"``
- `agentic/approval.go:114` — `Reason string `json:"reason,omitempty"``
- `agentic/approval.go:119` — `ApprovedBy string    `json:"approved_by,omitempty"``
- `agentic/approval.go:120` — `DecidedAt  time.Time `json:"decided_at"``

Validation checks the loop token, nonempty CallID, decision vocabulary, and approver presence for approve/modify.
It does not establish that the decision names a particular framework execution.

- `agentic/approval.go:137` — `if err := validateLoopTokenField("loop_id", r.LoopID); err != nil {`
- `agentic/approval.go:140` — `if r.CallID == "" {`
- `agentic/approval.go:144` — `case ApprovalDecisionApprove, ApprovalDecisionModify:`
- `agentic/approval.go:145` — `if r.ApprovedBy == "" {`

The outward pending event also carries LoopID and CallID without execution correlation. It includes tool name,
arguments, reason, RequestedAt, timeout and trace.

- `agentic/approval.go:45` — `type ApprovalPendingEvent struct {`
- `agentic/approval.go:46` — `LoopID      string         `json:"loop_id"``
- `agentic/approval.go:47` — `CallID      string         `json:"call_id"``
- `agentic/approval.go:51` — `RequestedAt time.Time      `json:"requested_at"``

Both carriers already have payload registrations. Rule-readable approval fields expose LoopID, CallID,
decision, approver and decision time; free-text reason and modified arguments are withheld.

- `agentic/rule_fields.go:335` — `func (r *ApprovalResponse) RuleFields() map[string]any {`
- `agentic/rule_fields.go:337` — `"loop_id":  r.LoopID,`
- `agentic/rule_fields.go:338` — `"call_id":  r.CallID,`
- `agentic/rule_fields.go:339` — `"decision": r.Decision,`
- `agentic/rule_fields.go:341` — `putString(fields, "approved_by", r.ApprovedBy)`
- `agentic/rule_fields.go:342` — `putTime(fields, "decided_at", r.DecidedAt)`

The semantic references census for DecidedAt found production writes in dispatch and the timeout publisher,
plus RuleFields projection. It found no production approval-matching read of DecidedAt.

### Existing approval producers

The HTTP request DTO accepts decision, modified arguments, reason and user identity. The path names LoopID.
Dispatch reads current authority and obtains CallID from the pending record.

- `processor/agentic-dispatch/http.go:501` — `type ApprovalRequest struct {`
- `processor/agentic-dispatch/http.go:771` — `persisted, readErr := c.loadPersistedLoop(ctx, loopID)`
- `processor/agentic-dispatch/http.go:792` — `if persisted.State != agentic.LoopStateAwaitingApproval {`
- `processor/agentic-dispatch/http.go:794` — `c.writeJSONError(w, http.StatusConflict, "loop not awaiting approval")`
- `processor/agentic-dispatch/http.go:797` — `callID := persisted.PendingApproval.CallID`
- `processor/agentic-dispatch/http.go:807` — `subject, err := c.publishApprovalResponse(ctx, loopID, callID, &req, approver)`
- `processor/agentic-dispatch/http.go:861` — `CallID:            callID,`
- `processor/agentic-dispatch/http.go:866` — `DecidedAt:         time.Now().UTC(),`
- `processor/agentic-dispatch/http.go:878` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`

The timeout sweeper is the other current production producer found by `gopls references ApprovalResponse`.
It publishes rejection work with the same LoopID/CallID carrier and a fresh DecidedAt. The native approval consumer
owns application.

- `processor/agentic-loop/approval_sweeper.go:138` — `candidates := c.handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())`
- `processor/agentic-loop/approval_sweeper.go:146` — `response := agentic.ApprovalResponse{`
- `processor/agentic-loop/approval_sweeper.go:147` — `LoopID:   cand.LoopID,`
- `processor/agentic-loop/approval_sweeper.go:148` — `CallID:   cand.CallID,`
- `processor/agentic-loop/approval_sweeper.go:149` — `Decision: agentic.ApprovalDecisionReject,`
- `processor/agentic-loop/approval_sweeper.go:153` — `DecidedAt:  time.Now().UTC(),`
- `processor/agentic-loop/approval_sweeper.go:155` — `if err := c.publishApprovalResponseToWire(ctx, response); err != nil {`

### The pending record carries the existing execution identity

Unlike the public approval input, PendingApprovalState carries RequestID, ExecutionID and positive ordinal alongside
CallID, name, arguments, reason, RequestedAt, timeout and trace.

- `agentic/state.go:146` — `type PendingApprovalState struct {`
- `agentic/state.go:147` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/state.go:148` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/state.go:149` — `CallID      string         `json:"call_id"``
- `agentic/state.go:150` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/handlers.go:2451` — `entity.PendingApproval.RequestID = toolResult.RequestID`
- `processor/agentic-loop/handlers.go:2452` — `entity.PendingApproval.ExecutionID = toolResult.ExecutionID`
- `processor/agentic-loop/handlers.go:2453` — `entity.PendingApproval.CallOrdinal = toolResult.CallOrdinal`

The current pending resolver matches only the supplied CallID within the selected in-process LoopEntity. It returns
the full pending snapshot, then clears PendingApproval and StateBeforeApproval.

- `processor/agentic-loop/state.go:618` — `func (m *LoopManager) ResolveApprovalIfPending(loopID, callID string) (agentic.PendingApprovalState, bool, error) {`
- `processor/agentic-loop/state.go:634` — `if entity.State != agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/state.go:637` — `if entity.PendingApproval == nil || entity.PendingApproval.CallID != callID {`
- `processor/agentic-loop/state.go:641` — `pending := *entity.PendingApproval`
- `processor/agentic-loop/state.go:642` — `if err := entity.ResolveApproval(); err != nil {`
- `agentic/state.go:211` — `e.StateBeforeApproval = ""`
- `agentic/state.go:212` — `e.PendingApproval = nil`

Approve and modify preserve the pending RequestID, ExecutionID and ordinal when redispatching. Reject synthesizes
a ToolResult carrying those same identifiers. The approve/modify ToolCall carries ApprovedBy; the synthesized reject
encodes approver and reason in its error text.

- `processor/agentic-loop/approval_response_handler.go:123` — `RequestID:   pending.RequestID,`
- `processor/agentic-loop/approval_response_handler.go:124` — `ExecutionID: pending.ExecutionID,`
- `processor/agentic-loop/approval_response_handler.go:125` — `CallOrdinal: pending.CallOrdinal,`
- `processor/agentic-loop/approval_response_handler.go:127` — `ApprovedBy:  approvedBy,`
- `processor/agentic-loop/approval_response_handler.go:151` — `RequestID:   pending.RequestID,`
- `processor/agentic-loop/approval_response_handler.go:152` — `ExecutionID: pending.ExecutionID,`
- `processor/agentic-loop/approval_response_handler.go:154` — `CallOrdinal: pending.CallOrdinal,`
- `processor/agentic-loop/approval_response_handler.go:160` — `return h.HandleToolResult(ctx, loopID, synthetic)`

Nonterminal application publishes the branch output before the revision-bound cleared-pending write. A faster result
owner can already have advanced that revision.

- `processor/agentic-loop/approval_response_handler.go:248` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:254` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:259` — `return natsclient.DeliveryDecisionAck, nil`

### Execution identity is already adopted by tool handling

Loop stamps tool execution identity from RequestID, provider CallID and the response-array ordinal. Redispatch keeps
that identity. ToolResult carries the same three correlation fields.

- `processor/agentic-loop/execution_identity.go:20` — `ordinal := uint32(i + 1)`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:25` — `calls[i].CallOrdinal = ordinal`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `agentic/tools.go:214` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:215` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:216` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-tools/outcomes.go:153` — `result.RequestID = call.RequestID`
- `processor/agentic-tools/outcomes.go:154` — `result.ExecutionID = call.ExecutionID`
- `processor/agentic-tools/outcomes.go:155` — `result.CallOrdinal = call.CallOrdinal`

Tools checks its existing completed outcome before execution. The outcome is keyed by execution identity and retains
RequestID, CallID, ordinal, a call fingerprint and ToolResult. Its fingerprint includes arguments and ApprovedBy.
Approval-required interception deliberately does not create a completed outcome.

- `processor/agentic-tools/component.go:710` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:713` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/outcomes.go:25` — `type completedOutcome struct {`
- `processor/agentic-tools/outcomes.go:31` — `Fingerprint string             `json:"fingerprint"``
- `processor/agentic-tools/outcomes.go:32` — `Result      agentic.ToolResult `json:"result"``
- `processor/agentic-tools/outcomes.go:82` — `func toolCallOutcomeKey(executionID string) string {`
- `processor/agentic-tools/outcomes.go:108` — `ApprovedBy  string         `json:"approved_by"``
- `processor/agentic-tools/component.go:745` — `// Approval-required is nonterminal coordination. The loop deliberately`
- `processor/agentic-tools/component.go:750` — `err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ExecutionID))`

This is an existing tools-owned authority. It is not one of the admitted approval-reconstruction reads, and this
inventory does not add it to that boundary.

### Current LoopEntity results survive one boundary but are not an indefinite history

Results are keyed by ExecutionID. The next-model transition extracts results without clearing the durable result map.
The first result from a different RequestID removes the prior request’s entries. Explicit drains clear immediately.
Batch restoration validates and replaces the retained map with the reconstructed current batch.

- `processor/agentic-loop/state.go:1080` — `for key, stored := range entity.PendingToolResults {`
- `processor/agentic-loop/state.go:1081` — `if stored.RequestID != "" && stored.RequestID != result.RequestID {`
- `processor/agentic-loop/state.go:1082` — `delete(entity.PendingToolResults, key)`
- `processor/agentic-loop/state.go:1086` — `resultKey := result.ExecutionID`
- `processor/agentic-loop/state.go:1093` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/handlers.go:2590` — `allResults := h.loopManager.toolResults(loopID, false)`
- `processor/agentic-loop/state.go:1137` — `delete(m.toolCallToLoop, executionID)`
- `processor/agentic-loop/state.go:1140` — `entity.PendingToolResults = nil`
- `processor/agentic-loop/state.go:483` — `entity.PendingToolResults = results`

Current results carry execution identity, content/error and tool name. They do not carry ApprovedBy,
ModifiedArguments, DecidedAt, or the approval source’s envelope identity as separate result fields.

### Retained request history carries execution-bearing assistant calls

ChatMessage carries `[]ToolCall` for assistant messages. Tool-role messages carry provider ToolCallID, name, content
and IsError, with no separate execution fields. The association used by existing late-tool handling is therefore the
stamped assistant batch plus the ordered tool message.

- `agentic/types.go:212` — `ToolCalls        []ToolCall `json:"tool_calls,omitempty"``
- `processor/agentic-loop/state.go:484` — `assistant := response.Message`
- `processor/agentic-loop/state.go:485` — `assistant.ToolCalls = calls`
- `processor/agentic-loop/handlers.go:2725` — `messages[i] = agentic.ChatMessage{`
- `processor/agentic-loop/handlers.go:2727` — `ToolCallID: r.CallID,`
- `processor/agentic-loop/handlers.go:2728` — `Name:       name,`
- `processor/agentic-loop/handlers.go:2729` — `Content:    content,`
- `processor/agentic-loop/handlers.go:2730` — `IsError:    isError,`

The next request uses the context manager’s current context, after tool-pair repair. This inventory does not establish
that every historical tool pair survives every later context operation.

- `processor/agentic-loop/handlers.go:2603` — `if removed := cm.RepairToolPairs(); removed > 0 {`
- `processor/agentic-loop/handlers.go:2609` — `messages := cm.GetContext()`
- `processor/agentic-loop/handlers.go:2636` — `Messages:       messages,`

## 3. Exact readers and matching

The private evidence reader exposes only exact request and response reads. The implementation reads the latest
message on the resolved exact subject. Current loop authority is an exact KV Get; key/payload LoopID and nonempty
TaskID are checked.

- `processor/agentic-loop/settlement_recovery.go:26` — `type loopSettlementEvidenceReader interface {`
- `processor/agentic-loop/settlement_recovery.go:27` — `ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:28` — `ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:54` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:125` — `entry, err := c.loopsBucket.Get(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:143` — `if entity.ID != loopID || entity.TaskID == "" {`

Pending approval recovery first selects by the current pending ExecutionID, validates its retained gated result, then
requires the latest request to equal PendingApproval.RequestID. It reads the response named by that request and
requires exactly one matching provider CallID, with the stamped execution, ordinal, tool, arguments and trace agreeing.

- `processor/agentic-loop/settlement_recovery.go:574` — `pending := entity.PendingApproval`
- `processor/agentic-loop/settlement_recovery.go:580` — `result, found := entity.PendingToolResults[pending.ExecutionID]`
- `processor/agentic-loop/settlement_recovery.go:594` — `request, found, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:601` — `if request.RequestID != pending.RequestID || request.Role != entity.Role || request.Model != entity.Model {`
- `processor/agentic-loop/settlement_recovery.go:605` — `response, found, err := c.readRetainedAgentResponse(ctx, request.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:617` — `if err := stampToolExecutionCorrelation(request.RequestID, calls); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:622` — `if call.ID != pending.CallID {`
- `processor/agentic-loop/settlement_recovery.go:635` — `if matches != 1 {`

Native source sequence and delivery count are available to the transport, but the approval work receives payload
bytes. No current branch persists a source-sequence-to-execution association.

- `processor/agentic-loop/component.go:924` — `settleHandlerFn = c.handleApprovalResponseMessage`
- `processor/agentic-loop/component.go:1077` — `decision, cause := runLoopDeliveryWork(msgCtx, msg.Data(), settleHandlerFn)`
- `processor/agentic-loop/component.go:1078` — `result := natsclient.SettleDelivery(msg, decision, cause)`

## 4. Adjacent existing problem shapes and authority overlap

The problem shape is “settle a repeated input only when existing durable consequences identify what it already
applied.” There are already operation-specific instances in loop settlement.

For a late ToolResult, the incoming message supplies RequestID, ExecutionID and ordinal. `recoverToolResult` checks its
originating response and can identify application in a later retained request by exact assistant-batch correlation
and the ordinal-selected tool message.

- `processor/agentic-loop/settlement_recovery.go:417` — `if result.RequestID == "" || result.ExecutionID == "" || result.CallOrdinal == 0 {`
- `processor/agentic-loop/settlement_recovery.go:438` — `response, found, err := c.readRetainedAgentResponse(ctx, result.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:489` — `if request.RequestID != result.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:498` — `if retained.ExecutionID != call.ExecutionID || retained.RequestID != call.RequestID ||`
- `processor/agentic-loop/settlement_recovery.go:510` — `resultIndex := index + int(result.CallOrdinal)`
- `processor/agentic-loop/settlement_recovery.go:511` — `if resultIndex < len(request.Messages) && reflect.DeepEqual(request.Messages[resultIndex], want) {`
- `processor/agentic-loop/settlement_recovery.go:512` — `return "", nil`

The current terminal-tool proof likewise starts with an identified ToolResult and permits only its two declared
direct terminal consequences. A bare terminal state is insufficient.

- `processor/agentic-loop/settlement_recovery.go:537` — `func proveTerminalToolResultApplied(entity agentic.LoopEntity, requestID string, calls []agentic.ToolCall, result agentic.ToolResult) error {`
- `processor/agentic-loop/settlement_recovery.go:542` — `stored, found := results[result.ExecutionID]`
- `processor/agentic-loop/settlement_recovery.go:550` — `if !reflect.DeepEqual(stored, result) {`
- `processor/agentic-loop/settlement_recovery.go:567` — `return fmt.Errorf("terminal loop lacks execution-specific applied proof for %q", result.ExecutionID)`

These are existing instances of the shape, not a recommendation to reuse them for approval. Their input already
identifies an execution; the approval selector question precedes that proof.

No new durable, communication, or coordination primitive is proposed, so no establishing-pattern adoption sweep is
triggered. The existing owners relevant to collision checking are:

| Dimension | Existing owner/evidence |
|---|---|
| Current approval and loop state | LoopEntity and agentic-loop; PendingApprovalState and result-map pins above |
| Retained request/response work | AGENT; exact-subject reader at settlement_recovery.go:54 |
| Completed tool outcome | Agentic-tools; completedOutcome and replay pins above |
| Catalogs | Existing approval payload registrations and component ports; no new entry |
| Status and lifecycle | AwaitingApproval; pending clear; revision-bound commit; native consumer settlement |
| Ownership/concurrency | In-process mutex at state.go:619; durable revision update at approval_response_handler.go:254 |
| Readers/writers | HTTP producer, timeout producer, loop consumer, tools result producer, rule projection, downstream observer |
| Recovery | Exact pending restoration and operation-specific applied proofs above |
| Replacement/expiry | Result-map turnover is measured above; this refresh does not expand the accepted retention inventory |

## 5. Existing contracts and conditional fallback

The active delta requires current LoopEntity plus latest exact request and exact response, forbids an AGENT scan and
CallID-indexed ToolResult lookup, and distinguishes unresolved reads, confirmed retained absence, corruption,
continuation and durable applied-state proof.

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:290` — `### Requirement: Approval continuation after replacement is exact and evidence-bounded`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:297` — ``agent.response.<RequestID>`. It SHALL perform no stream scan and no `ToolResult` lookup by provider CallID.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:299` — `Provider CallID SHALL be interpreted only within the current RequestID. An older response carrying the same CallID`

The accepted fallback remains conditional. Task 6.6 requires an explicit owner ruling before removal; a source-code
failure alone is not an evidence result showing existing storage insufficient.

- `openspec/changes/agentic-loop-restart-safety/design.md:314` — `The already-approved ObjectStore design is not revoked by this design pass. Implementation first proves whether`
- `openspec/changes/agentic-loop-restart-safety/design.md:345` — `explicit revocation of comment `5463183450` authorizes deletion of `ApprovalContinuationV1` and its Store plan. If`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:670` — `- [ ] 6.6 Stop for an owner mechanism ruling after the evidence gate. On PASS, obtain explicit revocation of comment`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:672` — `  claims. On FAIL, retain the already-approved ObjectStore plan unchanged. Introduce no third mechanism.`

Historical fallback promises include the original request and response, full execution correlation, canonical
content validation, a typed storage reference, and applied-decision fingerprint within PendingApprovalState.
These are recorded promises, not current production declarations or a new recommendation.

- `openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md:450` — ``ApprovalContinuationV1` contains LoopID, TaskID, RequestID, execution identity, provider CallID, positive ordinal,`
- `openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md:451` — `the exact originating `AgentRequest`, and the originating tool-call `AgentResponse`. Validation checks nested types,`
- `openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md:478` — `The typed `StorageReference` and applied-decision fingerprint live only in existing `PendingApprovalState`. There is`

No new exported consumer-at-birth question arises: this inventory introduces no symbol, field, subject, bucket or
configuration entry.

## 6. Adopter seam inventory

Specific adopter: the SemTeams developer maintaining the approval UI and existing approval-response observer.
Read-only downstream baseline: `ce22c961d30014c463a09f8f8a2a90044ee1a1cf`; worktree clean when inspected.

### What must this developer know?

For HTTP submission, the developer supplies the loop identifier, decision and human identity, with modified
arguments for modify. Dispatch observes current pending CallID. The UI does not supply RequestID, ExecutionID,
ordinal, storage reference, or source sequence.

Downstream pins use absolute paths under `/Users/coby/Code/c360/semteams`:

- `/Users/coby/Code/c360/semteams/ui/src/lib/services/agentApi.ts:383` — `  async submitApproval(`
- `/Users/coby/Code/c360/semteams/ui/src/lib/services/agentApi.ts:401` — `    const response = await fetch(`${DISPATCH_BASE}/loops/${id}/approval`, {`
- `/Users/coby/Code/c360/semteams/ui/src/lib/services/agentApi.ts:407` — `      body: JSON.stringify(body),`
- `/Users/coby/Code/c360/semteams/ui/src/lib/services/agentApi.ts:409` — `    if (!response.ok) {`
- `/Users/coby/Code/c360/semteams/ui/src/lib/services/agentApi.ts:417` — `    return response.json();`

A direct bus publisher additionally carries the event’s CallID, forms the subject, and publishes a registered
ApprovalResponse envelope. That is the existing documented interface; it does not give the publisher an execution
identity to echo.

The additional correctness debt is distinguishing decision submission/publication from completed application. The
SemTeams observer currently interprets receipt of ApprovalResponse as the trigger for its run-resumed marker.

- `/Users/coby/Code/c360/semteams/cmd/semteams/approvalpause/subscriber.go:34` — `const DefaultApprovalResponseSubject = "agent.approval_response.>"`
- `/Users/coby/Code/c360/semteams/cmd/semteams/approvalpause/subscriber.go:155` — `	var ev agentic.ApprovalResponse`
- `/Users/coby/Code/c360/semteams/cmd/semteams/approvalpause/subscriber.go:162` — `	result, err := s.pauser.HandleResponse(ctx, &ev)`
- `/Users/coby/Code/c360/semteams/cmd/semteams/approvalpause/pauser.go:129` — `	runEntityID, stamped, err := p.stampRunMarker(ctx, ev.LoopID, MarkerApprovalResumed)`

This is an existing downstream interpretation, included to identify a consumer. Correcting that product behavior is
outside this inventory and outside SemStreams write authority.

### What happens if they do nothing?

The HTTP client continues submitting its existing DTO. A current non-awaiting state receives HTTP 409; unreadable
authority receives HTTP 503. Successful publication returns the accepted submission response. The later native
approval-input failure demonstrated by the RED is not retroactively reported by that HTTP response.

The downstream core-NATS observer continues responding to decision publication independently of the framework’s
durable application. This observation establishes no delivery or recovery guarantee for that downstream component.

### Where do they find out?

HTTP admission and publication failures reach a typed client error. The post-application source-settlement failure is
currently observable through delivery error logs and retained runtime evidence. The current client receives no
execution-specific receipt through its existing approval response.

### What should they have to know?

The existing accepted design places execution and storage knowledge inside the framework. The developer should be
able to express the human’s decision for the intended pending action without predicting RequestID, execution
identity, storage placement, or broker delivery history.

The measured gap is whether that existing input can still be associated unambiguously with its intended action after
PendingApproval clears or a later pending action reuses CallID. This inventory does not prescribe how to close it.

## 7. Open evidence questions

1. Which currently retained facts identify the execution intended by an old ApprovalResponse once PendingApproval
   clears? This fixture contains a unique-looking successful execution witness; a general selector is unproven.
2. When a later pending action in the same loop reuses CallID, what distinguishes the older approval source from a
   decision for that current pending action? Current pending matching uses LoopID plus CallID.
3. What binds decision, approver and modified arguments to durable already-applied evidence for approve versus
   modify, including multiple decisions for one execution? ToolResult alone has no separate approval-decision fields.
4. What remains available after the next request’s first result supersedes older PendingToolResults, or context
   processing removes the corresponding history pair? No blanket history-retention guarantee is established here.
5. The admitted approval read follows the response named by the latest request. After advancement, that response
   may be a later completion response. The existing late-ToolResult proof obtains its originating RequestID from the
   delivered result. These are different input preconditions, and this inventory does not silently widen the
   approval read contract.
6. Do all four decisions have sufficient existing evidence for durable already-applied settlement across replacement?
   The native applied case supplied here proves the approve guard failure only.
7. The approved fallback contains an applied-decision fingerprint promise, but this inventory has not established
   that falling back is necessary or that its lifecycle answers every post-clear case. Task 6.6 remains owner-owned.

## Searches and measurements

Semantic queries used the baseline worktree and:

`GOCACHE=/private/tmp/rule-task-redelivery.nFTf4H/go-build GOPLSCACHE=/private/tmp/gh1146-gopls-cache GOPROXY=off GOSUMDB=off`

Queries completed:

- `gopls workspace_symbol -matcher=fuzzy ApprovalResponse`
- `gopls references agentic/approval.go:104:6`
- `gopls references agentic/approval.go:106:2`
- `gopls references agentic/approval.go:120:2`
- `gopls references agentic/state.go:56:2`
- `gopls workspace_symbol -matcher=fuzzy toolResultApplied`
- `gopls workspace_symbol -matcher=fuzzy correlateToolResult`
- `gopls workspace_symbol -matcher=fuzzy toolCallOutcome`

Initial ApprovalResponse and PendingApproval symbol queries failed package loading with the default Go cache due to
sandbox-denied cache writes. They loaded zero packages and provide no absence evidence. The first successful
ApprovalResponse query emitted gopls-cache write warnings; subsequent queries used the explicit writable cache.

Literal/prose searches included:

- `git grep -n -i -e 'approval' -e 'execution identity' -e 'objectstore' -- openspec/specs/agentic-loop/spec.md openspec/specs/agentic-tools/spec.md openspec/specs/agentic-model/spec.md openspec/specs/agentic-governance/spec.md docs/adr/*.md`
- `git grep -n -i -e 'approval' -e 'ExecutionID' -e 'PendingToolResults' -- openspec/changes/agentic-loop-restart-safety/inventory*.md`
- `git grep -n -e 'recoverApproval' -e 'approvalResponseCallback' -e 'PendingToolResults' -e 'ResolveApprovalIfPending' -- processor/agentic-loop/settlement_recovery.go processor/agentic-loop/state.go processor/agentic-loop/component.go`
- `git grep -n -e 'type ToolCall struct' -e 'type ToolResult struct' -e 'type ChatMessage struct' -e 'ToolCallID' -- agentic`
- `git grep -n -e 'func ' -e 'stampTool' -e 'PendingApproval' -e 'toolResults(' -e 'ExecutionID' -- processor/agentic-loop/handlers.go processor/agentic-loop/settlement_recovery.go`
- `git grep -n -e 'agent.approval_response' -e 'bindLoop' -e 'handleApprovalResponse' -- processor/agentic-loop/component.go processor/agentic-loop/delivery_owner.go`
- `git grep -n -e 'ApprovalRequired' -e 'RequestID' -e 'ExecutionID' -e 'CallOrdinal' -- processor/agentic-tools/component.go processor/agentic-tools/outcomes.go processor/agentic-tools/approval_filter.go`
- `git grep -n -i -e 'Approval continuation' -e '6.6 ' -e 'ObjectStore' -e 'CallID' -- openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md`
- `git grep -n -e 'ApprovalContinuationV1' -e '5463183450' -e 'ContinuationRef' -- openspec/changes/agentic-loop-restart-safety docs/adr docs/operations/migration-beta162-to-beta163.md`
- `git grep -n -e 'approval_response' -e 'approval_pending' -- docs/concepts/17-approval-flow.md agentic/rule_fields.go agentic/payload_registry.go`
- `git grep -n -e 'approval_response' -e 'approval_pending' -- schemas/agentic-loop.v1.json schemas/agentic-dispatch.v1.json specs/agentic-loop.v1.json`
- `git grep -n -e 'ApprovalResponse' -e 'approval_response' -e '/approval' -- . ':!go.sum' ':!package-lock.json' ':!node_modules' ':!docs' ':!openspec' ':!processor' ':!agentic' ':!test' ':!schemas' ':!specs' ':!.claude' ':!.agents'`

Broad initial search output was truncated and was used only to locate files, never to close an absence category.
Declaration and reader conclusions above use the cited source ranges and completed semantic queries.

The same bounded downstream search was run in semdev, semspec, semdragon, semteams and semstreams-ui:

`git grep -n -e 'agent.approval_response' -e 'agentic.ApprovalResponse' -e '/approval' -- '*.go' '*.ts' '*.svelte' ':!vendor' ':!node_modules' ':!.claude'`

Observed downstream HEADs:

| Repository | HEAD |
|---|---|
| semdev | `ca3956af2ed87d5fa5bdb8183cdb506f7beb7240` |
| semspec | `5a9496eecc453747f4bc557b95444db6304c1420` |
| semdragon | `07f4de9b65887801ff18a7273d14233023049321` |
| semteams | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` |
| semstreams-ui | `39f5f04030e54cd7e5ac1b20490b877bb7b7f2dd` |

These searches identify the concrete SemTeams seam and generated API references. They do not constitute an exhaustive
downstream adoption census.

SHA-256 checkpoints:

| File | SHA-256 |
|---|---|
| agentic/approval.go | `1f4c9b2d4606e5a54825413b462541f84cac1e5be41861cf2b0a2ff522f590e0` |
| agentic/state.go | `9b1d65d3216191b96fe44efa4a5f4b2e47a71739e5fe2b4630a1cf956f9015b4` |
| processor/agentic-loop/approval_response_handler.go | `b7cce83d4e5d70ec8d67efd6097ac9c7e0d94aa95374b8f459d984f602baa493` |
| processor/agentic-loop/settlement_recovery.go | `57b53c86f9b896d0d50c112dfe876f68df415dec53694f6273c149d280c918ea` |
| processor/agentic-loop/state.go | `f46fc422c213f6efac6f3a580681e4d19e238ae091b0b713dab97c077b16d228` |
| processor/agentic-loop/execution_identity.go | `bd3f80c54874173208a2c0a51a3d2cb373dd50916a57438ea2df629759697b74` |
| processor/agentic-loop/handlers.go | `98168ffc31b3021d2737a522116b72d64953a6ba48bcdf9b7880942caba5627f` |
| processor/agentic-dispatch/http.go | `c80bd630a38cda81dcebb945a1ac23fc4a085c8690dbbc03e18b7021f6c17918` |
| processor/agentic-loop/approval_recovery_test.go | `c0d06085471aa45d0b225035bf2923e0b3a1eb5907b0c06a5011893078239b7e` |
| processor/agentic-loop/approval_replacement_integration_test.go | `24bb4ab59c71c45f78a22d4c965776f787969d346cc5f004cc251b24b4ea1307` |

The root session must materialize and hash this complete artifact before independent inventory review. No inventory
pass, design acceptance, fallback revocation, or implementation completion is claimed.
