# Inventory: R2 approval replacement and replay evidence

base: 5e0e2259aa7392f7f3255d7f01533869862d8174

Scope: R2 only; historical IDs 6.1, 6.1a, 6.2, 6.3, 6.5, 6.5b–6.5d.
Source enumeration only. No tests executed; no current test-result or coverage verdict is recorded here.
R1 and the R3 mechanism ruling are outside this inventory.

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:936` — `- [ ] 6.1 RED: run the real-NATS approval replacement gate after an approval-required`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:940` — `- [ ] 6.1a Add an explicit assertion stage to the standard agentic E2E tier:`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:948` — `- [ ] 6.2 Prove the settled approval-required`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:953` — `- [ ] 6.3 Implement only operation-specific exact reads for latest`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:960` — `- [ ] 6.5 Table-test approve, modify, reject, and timeout across transient/unresolved Retry, confirmed retained`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:978` — `- [ ] 6.5b GREEN: prove the reviewed native applied-approval redelivery and seeded old-A/current-B regressions under`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:987` — `- [ ] 6.5c RED: extend the no-reopen callback regression with warm/cold exact-authority observation before mutation,`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:994` — `- [ ] 6.5d Implement the narrow approval-required branch in the existing delivery/recovery owner using the existing`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:832` — `after modify/reject/timeout; actual committed closure racing stale observation/restoration (the existing CAS test`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:835` — `the accepted`

## Spellings of the fact

### Existing approval application and evidence readers

- `processor/agentic-loop/approval_response_handler.go:28` — `func (h *MessageHandler) HandleApprovalResponse(`
- `processor/agentic-loop/approval_response_handler.go:154` — `func (c *Component) handleApprovalResponseMessage(`
- `processor/agentic-loop/approval_response_handler.go:181` — `persisted, revision, err := c.readLoopEntityRevision(ctx, response.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:186` — `return natsclient.DeliveryDecisionRetry,`
- `processor/agentic-loop/approval_response_handler.go:199` — `if persisted.State != agentic.LoopStateAwaitingApproval || pending.ExecutionID != response.ExecutionID {`
- `processor/agentic-loop/approval_response_handler.go:200` — `c.logger.WarnContext(ctx, "approval response inapplicable: no matching current gate",`
- `processor/agentic-loop/approval_response_handler.go:203` — `c.metrics.approvalDecisionsInapplicable.Inc()`
- `processor/agentic-loop/approval_response_handler.go:207` — `if pending.CallID != response.CallID {`
- `processor/agentic-loop/approval_response_handler.go:216` — `if err := c.recoverApprovalResponse(ctx, response, persisted); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:258` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:264` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:269` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/settlement_recovery.go:153` — `func (c *Component) readRetainedAgentRequest(`
- `processor/agentic-loop/settlement_recovery.go:205` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-loop/settlement_recovery.go:503` — `c.logger.InfoContext(ctx, "approval-required tool status superseded by observed execution phase",`
- `processor/agentic-loop/settlement_recovery.go:506` — `c.metrics.approvalStatusesSuperseded.Inc()`
- `processor/agentic-loop/settlement_recovery.go:603` — `func (c *Component) republishPendingApproval(`
- `processor/agentic-loop/settlement_recovery.go:623` — `func (c *Component) approvalRequiredResultSuperseded(`
- `processor/agentic-loop/settlement_recovery.go:718` — `func (c *Component) recoverApprovalResponse(`
- `processor/agentic-loop/settlement_recovery.go:767` — `func validatePendingApprovalEvidence(`

## Consumers

### Native NATS; replacement Component instances in the same test process

- `processor/agentic-loop/approval_replacement_integration_test.go:75` — `func TestIntegrationApprovalAfterLoopAndDispatchReplacement(`
- `processor/agentic-loop/approval_replacement_integration_test.go:81` — `func TestIntegrationAppliedApprovalRedeliversAfterOwnerReplacement(`
- `processor/agentic-loop/approval_replacement_integration_test.go:86` — `func TestIntegrationApprovalRequiredResultRedeliversAfterClosedGate(`
- `processor/agentic-loop/approval_replacement_integration_test.go:91` — `func TestIntegrationApprovalRequiredResultRedeliversAfterLaterHistory(`
- `processor/agentic-loop/approval_replacement_integration_test.go:98` — `func TestIntegrationApprovalRequiredResultRedeliversAfterModifiedGate(`
- `processor/agentic-loop/approval_replacement_integration_test.go:106` — `func TestIntegrationApprovalRequiredResultRedeliversAfterTimeout(`
- `processor/agentic-loop/approval_replacement_integration_test.go:113` — `func TestIntegrationApprovalRequiredResultRetriesMatchingPendingPrompt(`
- `processor/agentic-loop/approval_replacement_integration_test.go:118` — `func TestIntegrationModifiedApprovalAfterLoopAndDispatchReplacement(`
- `processor/agentic-loop/approval_replacement_integration_test.go:125` — `func TestIntegrationRejectedApprovalAfterLoopAndDispatchReplacement(`
- `processor/agentic-loop/approval_replacement_integration_test.go:132` — `func TestIntegrationApprovalReplacementIgnoresOlderSameCallIDResponse(`
- `processor/agentic-loop/approval_replacement_integration_test.go:138` — `func TestIntegrationApprovalTimeoutAfterLoopAndDispatchReplacement(`
- `processor/agentic-loop/approval_replacement_integration_test.go:144` — `// Real task/model/tools owners produce every retained checkpoint; no process`
- `processor/agentic-loop/approval_replacement_integration_test.go:306` — `require.Empty(t, c.handler.loopManager.loops, "replacement must not be seeded with another instance's process state")`
- `processor/agentic-loop/approval_replacement_integration_test.go:369` — `if !ok || !reflect.DeepEqual(pendingPromptBefore, *republished) || prompt.Sequence <= pendingPromptSequence {`
- `processor/agentic-loop/approval_replacement_integration_test.go:370` — `return errors.New("pending retry ACK preceded a newly retained exact prompt publication")`
- `processor/agentic-loop/approval_replacement_integration_test.go:391` — `if current.Revision() != closedGateBefore.Revision() || !bytes.Equal(current.Value(), closedGateBefore.Value()) {`
- `processor/agentic-loop/approval_replacement_integration_test.go:401` — `if streamInfo.State.LastSeq != settledStreamSequence || len(perLoopMapCount(c.handler.loopManager, result.LoopID)) != 0 {`
- `processor/agentic-loop/approval_replacement_integration_test.go:408` — `if testutil.ToFloat64(getMetrics(nil).approvalStatusesSuperseded) != supersededBefore+1 {`
- `processor/agentic-loop/approval_replacement_integration_test.go:823` — `require.Equal(t, want, *call, "approval must preserve request/execution/ordinal/call/arguments/trace")`
- `processor/agentic-loop/approval_replacement_integration_test.go:931` — `require.Equal(t, uint32(1), laterCall.CallOrdinal)`
- `processor/agentic-loop/approval_replacement_integration_test.go:1000` — `stopReplacement()`
- `processor/agentic-loop/approval_replacement_integration_test.go:1007` — `require.False(t, retained, "old execution must rely on exact history, not the current accumulator")`
- `processor/agentic-loop/approval_replacement_integration_test.go:1020` — `_, stopReplay := startOwners(replacementTimeout, "")`
- `processor/agentic-loop/approval_replacement_integration_test.go:1032` — `require.Equal(t, firstMeta.Sequence.Stream, replayedMeta.Sequence.Stream)`
- `processor/agentic-loop/approval_replacement_integration_test.go:1033` — `require.Greater(t, replayedMeta.NumDelivered, firstMeta.NumDelivered)`
- `processor/agentic-loop/approval_replacement_integration_test.go:1038` — `require.Equal(t, finalBefore.Revision(), finalAfter.Revision())`
- `processor/agentic-loop/approval_replacement_integration_test.go:1039` — `require.Equal(t, finalBefore.Value(), finalAfter.Value())`
- `processor/agentic-loop/approval_replacement_integration_test.go:1040` — `require.Equal(t, wantExecutions, executor.calls.Load(), "approval replay must not repeat the gated executor effect")`
- `processor/agentic-loop/approval_replacement_integration_test.go:1045` — `require.Equal(t, 1, replay.acks, "the exact replay must settle from its current-authority proof")`
- `processor/agentic-loop/approval_replacement_integration_test.go:1048` — `require.Equal(t, skipsBefore+1, testutil.ToFloat64(skipCounter))`

### Seeded unit callback matrices and existing assertions

- `processor/agentic-loop/approval_recovery_test.go:194` — `func newApprovalRecoveryFixture(`
- `processor/agentic-loop/approval_recovery_test.go:217` — `bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}`
- `processor/agentic-loop/approval_recovery_test.go:253` — `func TestOldApprovalCannotResolveLaterRequestWithSameCallID(`
- `processor/agentic-loop/approval_recovery_test.go:319` — `assert.Equal(t, before, f.bucket.values[f.entity.ID], "A's decision must leave B's current pending authority unchanged")`
- `processor/agentic-loop/approval_recovery_test.go:321` — `assert.Contains(t, logs.String(), "approval response inapplicable: no matching current gate")`
- `processor/agentic-loop/approval_recovery_test.go:324` — `assert.Equal(t, skipsBefore+1, testutil.ToFloat64(f.c.metrics.approvalDecisionsInapplicable))`
- `processor/agentic-loop/approval_recovery_test.go:328` — `func TestColdApprovalWithoutCurrentGateIsObservableNoop(`
- `processor/agentic-loop/approval_recovery_test.go:421` — `func TestColdApprovalUnresolvedOrConflictingEvidenceDoesNotResolve(`
- `processor/agentic-loop/approval_recovery_test.go:427` — `{"request unavailable",`
- `processor/agentic-loop/approval_recovery_test.go:428` — `{"response visibility unresolved",`
- `processor/agentic-loop/approval_recovery_test.go:429` — `{"nonawaiting state contradicts retained pending gate",`
- `processor/agentic-loop/approval_recovery_test.go:432` — `{"gated result missing",`
- `processor/agentic-loop/approval_recovery_test.go:433` — `{"pending arguments conflict",`
- `processor/agentic-loop/approval_recovery_test.go:436` — `{"duplicate current provider call",`
- `processor/agentic-loop/approval_recovery_test.go:441` — `for _, branch := range []string{"approve", "modify", "reject", "timeout"} {`
- `processor/agentic-loop/approval_recovery_test.go:463` — `require.Equal(t, before, f.bucket.values[f.entity.ID])`
- `processor/agentic-loop/approval_recovery_test.go:479` — `require.Equal(t, before, f.bucket.values[f.entity.ID], "required PubAck must precede clearing durable pending state")`
- `processor/agentic-loop/approval_gate_settlement_test.go:47` — `func TestApprovalRequiredLaterHistoryProvesExactExecutionPhase(`
- `processor/agentic-loop/approval_gate_settlement_test.go:52` — `{"post-gate history", natsclient.DeliveryDecisionAck},`
- `processor/agentic-loop/approval_gate_settlement_test.go:53` — `{"equal-text post-gate result", natsclient.DeliveryDecisionAck},`
- `processor/agentic-loop/approval_gate_settlement_test.go:54` — `{"missing batch", natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/approval_gate_settlement_test.go:55` — `{"conflicting batch", natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/approval_gate_settlement_test.go:56` — `{"wrong position", natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/approval_gate_settlement_test.go:57` — `{"unequal ordinary result", natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/approval_gate_settlement_test.go:58` — `{"empty optional fields", natsclient.DeliveryDecisionAck},`
- `processor/agentic-loop/approval_gate_settlement_test.go:59` — `{"history loop conflict", natsclient.DeliveryDecisionQuarantine},`
- `processor/agentic-loop/approval_gate_settlement_test.go:60` — `{"history trace conflict", natsclient.DeliveryDecisionQuarantine},`
- `processor/agentic-loop/approval_gate_settlement_test.go:61` — `{"matching open gate", natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/approval_gate_settlement_test.go:62` — `{"unrelated newer gate", natsclient.DeliveryDecisionAck},`
- `processor/agentic-loop/approval_gate_settlement_test.go:63` — `{"stored correlation conflict", natsclient.DeliveryDecisionQuarantine},`
- `processor/agentic-loop/approval_gate_settlement_test.go:64` — `{"stored trace conflict", natsclient.DeliveryDecisionQuarantine},`
- `processor/agentic-loop/approval_gate_settlement_test.go:65` — `{"stored poison", natsclient.DeliveryDecisionQuarantine},`
- `processor/agentic-loop/approval_gate_settlement_test.go:75` — `f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)`
- `processor/agentic-loop/approval_gate_settlement_test.go:132` — `require.Equal(t, uint32(2), f.result.CallOrdinal)`
- `processor/agentic-loop/approval_gate_settlement_test.go:246` — `func TestApprovalRequiredMatchingPromptRetriesWithoutRewritingGate(`
- `processor/agentic-loop/approval_gate_settlement_test.go:259` — `assert.ErrorContains(t, callbackErr, "publish result agent.approval_pending."+f.entity.ID)`
- `processor/agentic-loop/approval_gate_settlement_test.go:261` — `assert.Equal(t, before, f.bucket.values[f.entity.ID], "retry must preserve the original gate and deadline")`
- `processor/agentic-loop/approval_gate_settlement_test.go:280` — `func TestNewApprovalGateUsesObservedRevisionBeforePrompt(`
- `processor/agentic-loop/approval_gate_settlement_test.go:281` — `for _, mode := range []string{"publication fails", "revision conflicts"} {`
- `processor/agentic-loop/approval_gate_settlement_test.go:300` — `assert.Zero(t, bucket.puts, "new gate never falls back to unconditional Put")`
- `processor/agentic-loop/approval_gate_settlement_test.go:320` — `func TestApprovalRequiredReplayIsSupersededBeforeMutation(`
- `processor/agentic-loop/approval_gate_settlement_test.go:321` — `for _, route := range []string{"warm", "cold"} {`
- `processor/agentic-loop/approval_gate_settlement_test.go:367` — `assert.Equal(t, before, after, "superseded gate phase cannot rewrite durable authority")`
- `processor/agentic-loop/approval_gate_settlement_test.go:368` — `assert.Equal(t, beforeProcess, afterProcess, "classify before changing process state")`
- `processor/agentic-loop/approval_gate_settlement_test.go:372` — `assert.Empty(t, probe.facts, "superseded status must not record a second business trajectory fact")`
- `processor/agentic-loop/approval_gate_settlement_test.go:376` — `assert.Equal(t, supersededBefore+1, testutil.ToFloat64(f.c.metrics.approvalStatusesSuperseded))`
- `processor/agentic-loop/approval_gate_settlement_test.go:439` — `func TestApprovalRequiredSiblingCannotBeAbsorbedAsConsumedGate(`
- `processor/agentic-loop/approval_gate_settlement_test.go:455` — `require.Equal(t, uint32(2), sibling.CallOrdinal)`
- `processor/agentic-loop/approval_gate_settlement_test.go:492` — `assert.Equal(t, before, after, "B must not become retained approval evidence while A owns the gate")`
- `processor/agentic-loop/approval_timeout_recovery_test.go:99` — `require.Equal(t, f.entity, current, "replacement configuration must not rewrite the retained deadline or identity")`
- `processor/agentic-loop/approval_timeout_recovery_test.go:241` — `require.Equal(t, f.entity, current, "the timer only publishes work; the native approval owner applies it")`

### OS-process replacement and omission controls

- `test/e2e/scenarios/agentic/approval_restart.go:40` — `checkpoint, err := s.awaitSettledApprovalCheckpoint(ctx, task)`
- `test/e2e/scenarios/agentic/approval_restart.go:56` — `if err := s.replaceSemStreams(ctx, newComposeProcessController(s.config.ComposeFile)); err != nil {`
- `test/e2e/scenarios/agentic/approval_restart.go:60` — `if err != nil || after <= before {`
- `test/e2e/scenarios/agentic/approval_restart.go:72` — `!reflect.DeepEqual(recovered.PendingApproval, checkpoint.loop.PendingApproval) ||`
- `test/e2e/scenarios/agentic/approval_restart.go:82` — `if err := s.submitApproval(ctx, task.LoopID, checkpoint.call.ExecutionID, agentic.ApprovalDecisionApprove); err != nil {`
- `test/e2e/scenarios/agentic/approval_restart.go:302` — `if err != nil || executions != 1 {`
- `test/e2e/scenarios/agentic/approval_restart_test.go:47` — `{name: "omitted restart", key: "approval_restart_process_after"},`
- `test/e2e/scenarios/agentic/approval_restart_test.go:49` — `{name: "unsettled boundary", key: "approval_restart_source_ack_floor", value: uint64(16)},`
- `test/e2e/scenarios/agentic/approval_restart_test.go:52` — `{name: "omitted recovery", key: "approval_restart_outcome"},`

## Problem shape

### Same live handler; concurrent direct method calls, no native delivery

- `processor/agentic-loop/approval_response_test.go:243` — `func TestHandleApprovalResponse_ConcurrentResponsesAtomicResolve(`
- `processor/agentic-loop/approval_response_test.go:247` — `const n = 16`
- `processor/agentic-loop/approval_response_test.go:255` — `go func(i int) {`
- `processor/agentic-loop/approval_response_test.go:272` — `result, err := handler.HandleApprovalResponse(context.Background(), resp)`
- `processor/agentic-loop/approval_response_test.go:301` — `if got := atomic.LoadInt64(&dispatchCount); got > 1 {`
- `processor/agentic-loop/approval_response_test.go:314` — `if entity.PendingApproval != nil {`

### Same Component; synchronously interleaved tool-result and approval code through unit hooks

- `processor/agentic-loop/approval_recovery_test.go:105` — `func TestApprovalFinalPutCannotOverwriteCompletedToolResult(`
- `processor/agentic-loop/approval_recovery_test.go:108` — `f.c.loopsBucket = &approvalPutHookBucket{KeyValue: f.c.loopsBucket, beforePut: func(ctx context.Context, key string, data []byte) error {`
- `processor/agentic-loop/approval_recovery_test.go:122` — `decision, err := f.c.handleToolResultMessage(ctx, settlementEnvelope(t, &completed))`
- `processor/agentic-loop/approval_recovery_test.go:133` — `require.ErrorContains(t, err, "KV revision mismatch")`
- `processor/agentic-loop/approval_recovery_test.go:138` — `require.Equal(t, agentic.LoopStateComplete, final.State, "late approval snapshot must not overwrite the completed result owner's terminal marker")`
- `processor/agentic-loop/approval_recovery_test.go:148` — `func TestApprovalCommitDoesNotBorrowUncommittedTerminalState(`
- `processor/agentic-loop/approval_recovery_test.go:169` — `terminalResult, err = f.c.handler.HandleToolResult(ctx, f.entity.ID, completed)`
- `processor/agentic-loop/approval_recovery_test.go:184` — `require.Equal(t, agentic.LoopStateExecuting, durable.State, "approval must commit its own pre-publication snapshot, never another owner's uncommitted terminal state")`
- `processor/agentic-loop/approval_recovery_test.go:191` — `// This nil-client unit proof isolates snapshot ordering, not server PubAck.`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:290` — `### Requirement: Approval continuation after replacement is exact and evidence-bounded`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:426` — `### Requirement: Approval-required tool statuses settle by observed execution phase`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:532` — `### Requirement: Approval deadlines are reconstructed narrowly`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:848` — `The unmodified two-test diagnostic run passed in 1.592s. Separate scratch overlays omitting only the supersession`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:849` — `log or only its counter failed the intended assertions in both tests (0.602s and 0.741s). Their log SHA-256 values`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:852` — `Independent source and evidence reviews approved this narrow proof. Native ordinal-2/equal-text, modify/timeout`
- `openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md:1002` — `- [ ] 6.6 Stop for an owner mechanism ruling after the evidence gate. On PASS, obtain explicit revocation of comment`
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1238 — e2e(agentic): the agentic tier walks neither the approval nor the signal path — three loop-token carriers ship with no e2e coverage, and the tier reports assertions_run=0
- #320 — agentic-loop: harden approval-response wire-publish — test seam + failure metric (follow-ups from #266 review)
- #1159 — fix(agentic-loop): preserve durable work across process restart; draft body retains R2 and R3 as open.
- #1156 — refactor(natsclient): add semantic delivery settlement; draft parent claim returned by PR search.
- Active changes returned: agentic-loop-restart-safety; semantic-jetstream-settlement.

## Searches

All commands ran in the claimed worktree. Read ranges are recorded below; no neighboring-framework sweep followed.
Initial combined outputs were truncated by the orchestration response budget; those counts are explicitly unresolved.

### Structural queries

- `git rev-parse HEAD` → 1 SHA, recorded above.
- `gopls workspace_symbol -matcher=fuzzy Approval` → output not retained in truncated combined response; no pins adopted.
- `gopls workspace_symbol -matcher=fuzzy applyApproval` → 0 usable results; package load failed on sandbox Go-cache access.
- `gopls workspace_symbol -matcher=fuzzy recoverApprovalResponse` → 1, with normal-cache read permission.
- `gopls references processor/agentic-loop/settlement_recovery.go:718:21` → 1.
- `gopls workspace_symbol -matcher=fuzzy readRetainedAgent` → 3; agentic-model neighbor not followed.

### Literal queries

- `git grep -n -e '^## Purpose' -e '^## Product Boundary' -e '^## ' -- openspec/project.md` → 4.
- `git grep -n -e '^## ' -e 'R2' -e 'approval replacement' -e 'section 6' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 8, before root's execution-limit paragraph.
- `git grep -n -e '^## 6' -e '^### 6' -e '6\.1a' -- openspec/changes/agentic-loop-restart-safety` → 6.
- `git grep -n -e '^func Test' -e '^func test' -e '^func run' -- processor/agentic-loop/approval_replacement_integration_test.go` → 12.
- `git grep -n -E '^(- \[[ x]\] 6\.|###|##)' -- openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md` → count unresolved; combined output truncated.
- `git grep -n -E 'approval|Approval|continuation_unavailable' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md` → count unresolved; combined output truncated.
- `git grep -n -E 't\.Fatal|require\.|Stop\(|Start\(|redeliver|timeout|ExecutionID|Trace|Arguments|Publish|Wait|continuation_unavailable|AckPending|NumPending' -- processor/agentic-loop/approval_replacement_integration_test.go` → count unresolved; combined output truncated.
- `git grep -n -E '^func Test.*(Approval|Continuation)|^func .*approval|^func .*Approval' -- processor/agentic-loop/approval* processor/agentic-loop/recovery* processor/agentic-loop/settlement* processor/agentic-loop/handlers* test/e2e/scenarios/agentic*` → declaration/name locator, count unresolved.
- `git grep -n -E 'approval.*(supersed|replay|replace)|continuation_unavailable|ApprovalContinuationV1' -- openspec/changes/agentic-loop-restart-safety/design.md` → 15.
- `git grep -n -E '^(func Test|[[:space:]]*(t.Run|name:|"[^"]+":))|continuation_unavailable|assert.*(Equal|Contains|Nil)|require.*(Equal|Contains|Nil)' -- processor/agentic-loop/approval_gate_settlement_test.go processor/agentic-loop/approval_recovery_test.go processor/agentic-loop/approval_response_test.go processor/agentic-loop/approval_timeout_recovery_test.go` → count unresolved; combined output truncated.
- `git grep -n -E 'continuation_unavailable|diagnostic.*(omit|omission)|omit.*diagnostic|same.gate|stale.*restor|newer.*clos|omission' -- processor/agentic-loop/*approval* processor/agentic-loop/settlement* test/e2e/scenarios/agentic/approval*` → 2.
- `git grep -n -E '^func Test.*(Approval|approval|Gate|Superseded)|^func .*recordApproval' -- processor/agentic-loop/*decision* processor/agentic-loop/*phase* processor/agentic-loop/*diagnostic*` → NOT RUN: zsh rejected unmatched `*phase*` before Git execution.
- `git grep -n -E 'continuation_unavailable|diagnostic.*(omit|omission)|omit.*diagnostic|stale.*restor|newer.*clos' -- processor/agentic-loop 'test/e2e/scenarios/agentic/approval*'` → 0.
- `git grep -n -e 'approvalDecisionsInapplicable' -e 'approvalStatusesSuperseded' -e 'without_diagnostics' -e 'omit_diagnostic' -e 'continuation_unavailable' -e 'ContinuationUnavailable' -- processor/agentic-loop test/e2e/scenarios/agentic` → 25.
- `git grep -n -E 'log.*(omitted|removed)|metric.*(omitted|removed)|diagnostic.omission|diagnostic omission|no-log|no-metric|closure|stale restoration' -- openspec/changes/agentic-loop-restart-safety/history/task-checkpoint-2026-09-11.md openspec/changes/agentic-loop-restart-safety/inventory-approval-required-replay-2026-09-10.md` → 22.
- `git grep -n -E 'approval.*(authority|current gate)|matching.*gate|same.gate|gate.*changed|ExecutionID.*mismatch' -- 'processor/agentic-loop/*test.go' 'processor/agentic-dispatch/*test.go'` → 8.
- `git grep -n -e 'continuation_unavailable' -e 'ContinuationUnavailable' -e 'continuation-unavailable' -- processor/agentic-loop` → 0.
- `git grep -n -e 'handleApprovalResponseMessage' -- 'processor/agentic-loop/*test.go'` → 14.

### Claim queries

- `gh issue list --repo C360Studio/semstreams --state open --search 'approval' --json number,title` → 20 returned; only named adjacent claims recorded.
- `gh pr list --repo C360Studio/semstreams --draft --search '1159' --json number,title,body` → 2, #1159 and #1156.
- `openspec list` → 2 active changes.

### Located-range inspections

- Contract read: `.agents/contracts/semstreams-explorer.md:1–240` → complete 87-line contract.
- Purpose/Product Boundary: `openspec/project.md:3–66` → 64 lines.
- Active task reads before root's paragraph: `tasks.md:49–80,143–181` → 71 lines.
- Historical task read: `history/task-checkpoint-2026-09-11.md:936–1004` → 69 lines.
- Native replacement reads: `approval_replacement_integration_test.go:1–180,75–146,580–720,752–809,290–307,342–415,719–749,1000–1059`.
- Seeded matrix reads: `approval_gate_settlement_test.go:47–118,246–319,320–381,439–501`.
- Seeded recovery reads: `approval_recovery_test.go:194–229,421–470,105–193`.
- Same-handler contention read twice: `approval_response_test.go:243–318`.
- Production reads: `approval_response_handler.go:28–104,154–256,245–293`; `settlement_recovery.go:500–548,603–639,718–828`.
- E2E reads: `approval_restart.go:26–96,178–203,263–336`; `approval_restart_test.go:19–74`.
- Historical omission/limits read: `history/task-checkpoint-2026-09-11.md:824–886`.

### Inspected limits and unsearched spellings

- No `continuation_unavailable`, `ContinuationUnavailable`, or `continuation-unavailable` literal found in processor/agentic-loop.
- Diagnostic omission is recorded as historical scratch-overlay execution at history lines 848–851; no current overlay execution was performed.
- NOT RUN: independent source/assertion enumeration of `terminal_release_test.go:341` and `trajectory_recorder_test.go:736` callers returned by the bounded callback search.
- NOT RUN: independent matrix enumeration of dispatch HTTP gate echo tests returned at `approval_handler_test.go:58,337`.
- NOT RUN: new later-history/ordinal fixture, new concurrency fixture, native or unit tests, historical-log retrieval, and any proposed mechanism.
- Actual closure-after-observation/before-restoration ordering was not located by the recorded stale/newer-closure searches; the two existing interleaving fixtures above are recorded by their exact callback boundaries.

### Root pin verification

- `scripts/inventory-verify.sh openspec/changes/agentic-loop-restart-safety/inventory-r2-approval-evidence-2026-09-12.md`
  initially reported 138/142 exact pins, three moved and one ambiguous. No production source had changed.
- `nl -ba processor/agentic-loop/approval_response_handler.go | sed -n '174,210p'` resolved all four off-by-one pins;
  corrected only their line numbers above.
