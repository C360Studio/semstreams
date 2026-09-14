# Inventory: approval-required ToolResult replay after gate closure

base: 615997c658ef4c5c4ce2e8a44709913c5b6d70fe

Frozen parent: `417beae5552f8f15ad3540edd7d8504c87174c13`.

## Scope and checkpoints

This refresh follows `HandleToolResult` → `checkApprovalGate`, its two production callers, and the existing
retained-result/application-proof owners. It reuses accepted inventory
`inventory-approval-applied-boundary-2026-09-10.md` at SHA-256
`6640c375572e2171790d7910de7663cf5928ea2b8aab99dd5c3d68ce50f197cb`.

The accepted one-gate-per-execution contract remains unchanged. This inventory proposes no new authority,
identifier, receipt, store, public surface, or read operation.

The worktree is dirty with concurrent implementation and documentation work. Inspected source snapshots:

| File | SHA-256 |
|---|---|
| `processor/agentic-loop/handlers.go` | `9943d5288dec1b642526a491652d6f7184fe768d6a1516b0fa2383637b671f06` |
| `processor/agentic-loop/state.go` | `6af06e8c44a873fb805b3c81c5e1f5084013c59acddad9feff51108b659a64fb` |
| `processor/agentic-loop/component.go` | `f50b92fb161eaa5ec2de0e917adc2ecc358732732d9baba5b1d63b52417eb141` |
| `processor/agentic-loop/approval_response_handler.go` | `8f6866568d344748024e317918b1d3aea8a3ebb60bf3fe5f6c3cc6571eb1c345` |
| `processor/agentic-loop/settlement_recovery.go` | `ee65014793946f11cb52ff63c93256a2a2946951ce22f52e84b958fd134f6501` |
| `processor/agentic-loop/approval_recovery_test.go` | `6ab5da7b69fc076557a24bbf3489d17ba9e65cc8327e361ef4953ca77cda7301` |

RED log: `/private/tmp/gh1146-approval-execution-echo.Rm4ZA5/closed-gate-replay-red.log`,
SHA-256 `e18ac886ea9cece86eb318ea0a7152cdfef8a705e37e09407ffcbc22a4ccda6d`.

The test calls the ordinary approval callback, observes cleared pending state, then calls the production
tool-result callback with the original serialized approval-required result. It observes the same execution
reopened as awaiting approval and changed fake-KV bytes. This is a unit callback checkpoint proof, not native
PubAck, native ACK, executor-effect, or cross-owner race proof.

No tests, Docker operations, repository writes, or Git mutations were performed by this inventory.

`orchestration-check` confirms that these existing execution and settlement responsibilities belong to the loop
component; no rule, lifecycle, or separate coordination owner is introduced.

## 1. Immediate owners and mutation order

`gopls references` found two production callers of HandleToolResult: the component's ToolResult callback and the
approval rejection synthesizer. The sole checkApprovalGate caller is HandleToolResult.

- `processor/agentic-loop/component.go:2014` — `result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)`
- `processor/agentic-loop/approval_response_handler.go:160` — `return h.HandleToolResult(ctx, loopID, synthetic)`
- `processor/agentic-loop/handlers.go:2263` — `func (h *MessageHandler) HandleToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:2269` — `entity, err := h.loopManager.GetLoop(loopID)`
- `processor/agentic-loop/handlers.go:2329` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`
- `processor/agentic-loop/handlers.go:2335` — `entity, err = h.loopManager.GetLoop(loopID)`
- `processor/agentic-loop/handlers.go:2341` — `err = h.loopManager.RemovePendingTool(loopID, toolResult.CallID)`
- `processor/agentic-loop/handlers.go:2362` — `if gated, err := h.checkApprovalGate(loopID, &entity, toolResult, &result); gated || err != nil {`
- `processor/agentic-loop/handlers.go:2415` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/handlers.go:2422` — `if !agentic.IsApprovalRequired(toolResult.Error) {`
- `processor/agentic-loop/handlers.go:2425` — `pubMsg, err := h.gateForApproval(loopID, entity, toolResult)`

The checkApprovalGate snapshot is obtained AFTER insertion of the incoming result. It therefore cannot establish
that a matching map entry predated this delivery. Trajectory and pending-tool changes also precede that check.

An already-awaiting loop returns before constructing another gate; this branch absorbs results without requiring
their execution to be the execution named by PendingApproval. The existing sibling test exercises a normal sibling
result, not a second approval-required sibling.

- `processor/agentic-loop/approval_gate_test.go:188` — `// Second arrival: a normal result for the sibling. Must NOT trigger`
- `processor/agentic-loop/approval_gate_test.go:191` — `siblingRes, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{`

Reachability of a distinct approval-required sibling under the current serial native dispatcher: NOT RUN.
No assertion that every retained approval-required map member individually opened a gate follows from this test.

## 2. Initial gate and closed gate are distinct durable snapshots

Initial gate construction retains request, execution, and ordinal on PendingApproval and updates the process entity.
The component persists the nonterminal entity before publishing its required output.

- `processor/agentic-loop/handlers.go:2451` — `entity.PendingApproval.RequestID = toolResult.RequestID`
- `processor/agentic-loop/handlers.go:2452` — `entity.PendingApproval.ExecutionID = toolResult.ExecutionID`
- `processor/agentic-loop/handlers.go:2453` — `entity.PendingApproval.CallOrdinal = toolResult.CallOrdinal`
- `processor/agentic-loop/handlers.go:2461` — `if err := h.loopManager.UpdateLoop(*entity); err != nil {`
- `processor/agentic-loop/component.go:1794` — `} else if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1798` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_gate_settlement_test.go:71` — `require.Equal(t, *toolResult, entity.PendingToolResults[toolResult.ExecutionID],`

Approve/modify resolves pending process state and redispatches the original execution. The component publishes the
branch before the cleared-pending CAS commit. The original approval-required result is not replaced by this branch.

- `agentic/state.go:210` — `e.State = restore`
- `agentic/state.go:211` — `e.StateBeforeApproval = ""`
- `agentic/state.go:212` — `e.PendingApproval = nil`
- `processor/agentic-loop/approval_response_handler.go:97` — `return result, h.dispatchApprovedCall(loopID, pending, pending.Arguments, response.ApprovedBy, &result)`
- `processor/agentic-loop/approval_response_handler.go:103` — `return result, h.dispatchApprovedCall(loopID, pending, args, response.ApprovedBy, &result)`
- `processor/agentic-loop/approval_response_handler.go:124` — `ExecutionID: pending.ExecutionID,`
- `processor/agentic-loop/approval_response_handler.go:259` — `resolved, err := c.handler.GetLoop(result.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:270` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:276` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`

Thus the measured ordinary checkpoints are:

First gate persisted: exact gated result plus matching PendingApproval.

Approve/modify closure persisted: same gated result, PendingApproval cleared, restored executing state.

The closed snapshot records consumed gate state after branch publication. It is not itself a receipt for the earlier
ApprovalPendingEvent PubAck: the initial pending Put precedes that publication.

## 3. Supersession changes what the retained result proves

Reject uses a synthetic approval_rejected result with the same ExecutionID. Timeout publishes a reject decision
through that same owner. These branches replace the original approval_required result in the existing map.

- `processor/agentic-loop/approval_response_handler.go:152` — `ExecutionID: pending.ExecutionID,`
- `processor/agentic-loop/approval_response_handler.go:157` — `Error:       fmt.Sprintf("%srejected by %s: %s", agentic.ApprovalRejectedPrefix, approver, reasonSuffix),`
- `processor/agentic-loop/approval_sweeper.go:150` — `Decision:    agentic.ApprovalDecisionReject,`
- `processor/agentic-loop/state.go:1093` — `resultKey := result.ExecutionID`
- `processor/agentic-loop/state.go:1100` — `entity.PendingToolResults[resultKey] = result`

A final result for the approved execution likewise replaces the old gated result. A new request's first result
removes prior-request results. Context extraction retains current results but evicts execution routes.

- `processor/agentic-loop/approval_recovery_test.go:529` — `completed.Error, completed.ErrorKind, completed.Content = "", "", "approved deletion"`
- `processor/agentic-loop/approval_recovery_test.go:530` — `result, err := c.handler.HandleToolResult(t.Context(), f.entity.ID, completed)`
- `processor/agentic-loop/state.go:1088` — `if stored.RequestID != "" && stored.RequestID != result.RequestID {`
- `processor/agentic-loop/state.go:1089` — `delete(entity.PendingToolResults, key)`
- `processor/agentic-loop/handlers.go:2591` — `allResults := h.loopManager.toolResults(loopID, false)`
- `processor/agentic-loop/state.go:1144` — `delete(m.toolCallToLoop, executionID)`

The same ExecutionID deliberately spans gated and post-decision results. Identity equality alone does not make
their different contents interchangeable.

## 4. Existing durable-read and applied-proof boundary

Warm routing does not read current LoopEntity authority before HandleToolResult. Cold routing does.

- `processor/agentic-loop/component.go:1954` — `loopID := c.findLoopIDForToolCall(toolResult.ExecutionID)`
- `processor/agentic-loop/component.go:1955` — `if loopID == "" {`
- `processor/agentic-loop/component.go:1956` — `loopID, err = c.recoverToolResult(ctx, toolResult)`
- `processor/agentic-loop/settlement_recovery.go:431` — `entity, found, err := c.readLoopEntity(ctx, result.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:438` — `response, found, err := c.readRetainedAgentResponse(ctx, result.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:474` — `request, found, err := c.readRetainedAgentRequest(ctx, result.LoopID)`

Later-request proof compares the exact stamped assistant batch and the incoming result's ordinal-selected tool
message. History containing a rejection or successful final result does not automatically equal the original
approval_required result.

- `processor/agentic-loop/settlement_recovery.go:489` — `if request.RequestID != result.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:490` — `want := c.handler.buildToolMessages([]agentic.ToolResult{result})[0]`
- `processor/agentic-loop/settlement_recovery.go:510` — `resultIndex := index + int(result.CallOrdinal)`
- `processor/agentic-loop/settlement_recovery.go:511` — `if resultIndex < len(request.Messages) && reflect.DeepEqual(request.Messages[resultIndex], want) {`
- `processor/agentic-loop/settlement_recovery.go:515` — `return "", fmt.Errorf("later request %q lacks execution-specific applied proof for %q", request.RequestID, result.ExecutionID)`

Current-batch restoration validates retained identity, but does not compare incoming content with an already-stored
result's content. It reinstalls a route for the incoming execution even if that execution already has a stored result.

- `processor/agentic-loop/state.go:439` — `if stored.RequestID != call.RequestID || stored.ExecutionID != call.ExecutionID || stored.CallID != call.ID ||`
- `processor/agentic-loop/state.go:445` — `results[call.ExecutionID] = stored`
- `processor/agentic-loop/state.go:495` — `if call.ExecutionID == incoming.ExecutionID {`
- `processor/agentic-loop/state.go:496` — `m.toolCallToLoop[call.ExecutionID] = entity.ID`
- `processor/agentic-loop/settlement_recovery.go:523` — `if err := c.handler.loopManager.restoreToolBatch(entity, request, response, result); err != nil {`

Therefore a superseded result while the originating request remains current is not covered by the later-request
comparison. The current path can reach the accumulator again. No supersession replay test was run here.

Terminal proof requires exact result content and one of its declared direct terminal consequences; it does not make
a bare terminal record or an approval_required result generic applied proof.

- `processor/agentic-loop/settlement_recovery.go:550` — `if !reflect.DeepEqual(stored, result) {`
- `processor/agentic-loop/settlement_recovery.go:557` — `if entity.PendingApproval == nil && !agentic.IsApprovalRequired(result.Error) &&`
- `processor/agentic-loop/settlement_recovery.go:562` — `if entity.PendingApproval == nil && ordinaryBatch && len(results) == len(calls) &&`
- `processor/agentic-loop/settlement_recovery.go:567` — `return fmt.Errorf("terminal loop lacks execution-specific applied proof for %q", result.ExecutionID)`

## 5. Adopter seam refresh

No new outward surface is introduced by this inventory. The already-inventoried external tool-result producer
retains the existing RequestID, ExecutionID, CallID, ordinal, and result payload on redelivery.

What it currently must know: existing correlation fields, unchanged by this question.

Do-nothing path: the measured valid repeated bytes can reopen the gate; the defect is framework replay handling.

Discovery: an approval-pending output and loop state expose the repeated gate; the unit callback test measures it.

What belongs to the framework: distinguishing its durable phase evidence and settling/refusing replay.

Downstream re-enumeration: NOT RUN; no proposed payload, API, or caller obligation changed.

## 6. Search and inspection record

Commands ran in the named worktree unless noted otherwise:

1. `git status --short`
2. `git rev-parse HEAD`
3. Read `.agents/skills/orchestration-check/SKILL.md`; its required concept 14 was read in this architect session.
4. `git grep -n -E 'func .*HandleToolResult|func .*checkApprovalGate|TestApprovalRequiredResultReplayCannotReopenClosedGate|PendingToolResults' -- processor/agentic-loop agentic/state.go`
5. `GOCACHE=/private/tmp/rule-task-redelivery.nFTf4H/go-build GOPLSCACHE=/private/tmp/gh1146-gopls-cache GOPROXY=off GOSUMDB=off gopls references processor/agentic-loop/handlers.go:2263:26`
6. Same environment: `gopls references processor/agentic-loop/handlers.go:2408:26`
7. Line-numbered reads: handlers 2248–2478 and 2580–2628; state 398–530 and 1066–1156;
   component 1784–1805, 1800–2036, and 2034–2056; approval_response_handler 28–163 and 183–293;
   settlement_recovery 405–572 and 650–674; agentic/state 156–218; approval_sweeper 90–155.
8. Line-numbered test reads: approval_recovery 355–425 and 480–540; approval_gate_settlement 1–105;
   approval_gate 143–207.
9. Read RED log with numbered lines 1–65; byte-array failure output was large/truncated. No claim depends on its
   truncated byte arrays; the callback setup, assertions, and short observed-state line were inspected.
10. SHA-256 commands over the six source checkpoints and RED log listed above.
11. Read active loop spec 238–289 for unchanged tool-result correlation/applied-proof constraints.
12. `git grep -n -E 'checkApprovalGate|gateForApproval|StoreToolResult|validatedToolBatchResults|recoverToolResult|approval_response_handler.go:1[45]' -- openspec/changes/agentic-loop-restart-safety/inventory-approval-applied-boundary-2026-09-10.md`
    returned no results because the accepted inventory artifact is untracked; this is not an absence claim.
13. Read the accepted inventory header and ran
    `rg -n 'checkApprovalGate|gateForApproval|StoreToolResult|validatedToolBatchResults|recoverToolResult|handlers.go:23|handlers.go:24' openspec/changes/agentic-loop-restart-safety/inventory-approval-applied-boundary-2026-09-10.md`
    against that exact known untracked file; it includes the gate identity writer and existing recovery discussion.

No broad state-machine, downstream, or framework sweep was performed.
