# R8 missing-history recovery inventory supplement

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: inventory only; read-only inspection of the existing dirty claim. No tests, runtime changes, or new recovery design.

Scope: matching task/loop authority with absent reconstruction request. Current proposal, design, tasks, base loop spec and loop delta were read. This refresh reuses the reviewed retained-facts, loop-reachability and attachment-retirement inventories; their historical pins are not represented as current-source pins.

## Surface inventory

### 1. Claimed gap and existing distinctions

Cold task intake validates authority and task correlation before selecting terminal suppression, retained-request restoration, or initial reconstruction:

- `processor/agentic-loop/component.go:1311` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1313` — `return loopSettlementDecision(err), err`
- `processor/agentic-loop/component.go:1316` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/settlement_recovery.go:384` — `entity, found, err := c.readLoopEntity(ctx, task.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:391` — `if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:402` — `request, retained, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:406` — `if retained {`
- `processor/agentic-loop/settlement_recovery.go:414` — `assembled := c.handler.assembleSystemPrompt(ctx, task)`
- `processor/agentic-loop/settlement_recovery.go:416` — `messages = c.handler.prependIterationContext(ctx, entity.ID, 1, entity.MaxIterations, messages)`
- `processor/agentic-loop/settlement_recovery.go:421` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`

The absent-request branch has **no partial-birth versus lost-history discriminator**. It unconditionally assembles initial work after the earlier terminal/correlation checks.

Typed absence is distinct from failed observation:

- `processor/agentic-loop/settlement_recovery.go:63` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:64` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-loop/settlement_recovery.go:291` — `return agentic.AgentRequest{}, false, fmt.Errorf("read exact request %s: %w", subject, err)`
- `processor/agentic-loop/settlement_recovery.go:1045` — `return natsclient.DeliveryDecisionQuarantine`
- `processor/agentic-loop/settlement_recovery.go:1049` — `return natsclient.DeliveryDecisionRetry`

The existing RED seeds `Iterations=2`, calls recovery directly and inspects its generated output. It demonstrates the wrong branch, not an approved discriminator, native expiry, or actual publication:

- `processor/agentic-loop/settlement_recovery_test.go:240` — `func TestColdTaskRedeliveryWithProgressAndMissingRequestRefusesInitialRebuild(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:243` — `entity.Iterations = 2`
- `processor/agentic-loop/settlement_recovery_test.go:252` — `// Seeded absence proves this recovery branch only, not production expiry.`

### 2. Existing representations and valid partial birth

Current authority stores task ownership, operational state, iteration budget, pending results and approval identity. It has no dedicated request-publication/birth-completion field; the full `LoopEntity` declaration was inspected at `agentic/state.go:46–94`.

Iterations are budget accounting, including replay adjustment:

- `agentic/state.go:242` — `Iterations:    0,`
- `processor/agentic-loop/handlers.go:1240` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls, propose); err != nil {`
- `processor/agentic-loop/handlers.go:2488` — `err := h.loopManager.IncrementIteration(loopID)`
- `processor/agentic-loop/state.go:389` — `entity.Iterations--`

First-round model/tool work can precede that increment. Neither running state nor zero iterations establishes unpublished birth.

Valid partial birth is a real existing case: loop persistence precedes both required publications; either publication can fail before any request commits.

- `processor/agentic-loop/component.go:1394` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1399` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/settlement_recovery_test.go:191` — `func TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop(t *testing.T) {`
- `processor/agentic-loop/prior_messages_test.go:79` — `func TestPriorMessagesColdReconstructionAndRestoration(t *testing.T) {`
- `processor/agentic-loop/task_output_settlement_integration_test.go:23` — `func TestIntegrationTaskRequiredOutputFailureSettlement(t *testing.T) {`
- `processor/agentic-loop/task_output_settlement_integration_test.go:31` — `{name: "created_failure_cold", failedOutput: "agent.created", replace: true},`
- `processor/agentic-loop/task_output_settlement_integration_test.go:32` — `{name: "initial_request_failure_cold", failedOutput: "agent.request", replace: true},`
- `processor/agentic-loop/task_output_settlement_integration_test.go:118` — `require.ErrorIs(t, err, jetstream.ErrMsgNotFound, "model work cannot escape a failed creation publication")`
- `processor/agentic-loop/task_output_settlement_integration_test.go:143` — `require.False(t, ok, "cold replay must not depend on the original pending cache")`
- `processor/agentic-loop/task_output_settlement_integration_test.go:191` — `require.NoError(t, observed.ackCheckErr, "successful task settlement requires both correlated outputs")`

That native fixture uses real KV/stream publication and controlled delivery through the production heartbeat owner, replacing component objects. Its own comments exclude OS-restart and native server-redelivery claims.

### 3. Existing unavailable-evidence refusal owner

`settleAbsentApprovalEvidence` already distinguishes unavailable observation from its approval-specific confirmed-absence condition, rechecks exact current authority, prepares a failed loop, and invokes the existing terminal owner:

- `processor/agentic-loop/settlement_recovery.go:923` — `return false, natsclient.DeliveryDecisionRetry, fmt.Errorf("approval evidence %q retention is not observable", subject)`
- `processor/agentic-loop/settlement_recovery.go:929` — `info, err := stream.Info(ctx)`
- `processor/agentic-loop/settlement_recovery.go:937` — `age := time.Since(entity.StartedAt)`
- `processor/agentic-loop/settlement_recovery.go:948` — `if revision == 0 || observed != revision || !reflect.DeepEqual(current, entity) {`
- `processor/agentic-loop/settlement_recovery.go:971` — `failure, messages, err := c.handler.BuildFailureMessages(entity.ID, "continuation_unavailable", errorMsg)`
- `processor/agentic-loop/settlement_recovery.go:976` — `FailureState: failure, PublishedMessages: messages}, failure, revision)`

Its timestamp/retention eligibility is existing **approval-specific behavior**, not evidence for a task discriminator. Structural references locate two calls, both within `recoverApprovalResponse`, at lines 876 and 890.

The common terminal owner already validates current revision/task ownership, selects existing COMPLETE authority, publishes the selected registered outcome, then conditionally writes the final marker:

- `processor/agentic-loop/component.go:1827` — `if revision == 0 || observed != revision || current.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1830` — `if current.TaskID != marker.TaskID {`
- `processor/agentic-loop/component.go:1835` — `selected, err := c.selectTerminalOutcome(ctx, result.LoopID, marker.TaskID, candidate)`
- `processor/agentic-loop/component.go:1906` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1916` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`

No new durable or coordination primitive is proposed, so this supplement triggers no new-primitive collision table. Existing owners remain task recovery, approval evidence classification and terminal settlement; their different eligibility contracts are explicit.

### 4. Adjacent constraints and consumers

Owner comments `5727252562` and `5728438234`, recorded in current tasks, preserve visible unavailable-history refusal, valid partial birth, same-task recovery, fresh chat with supplied PriorMessages, within-execution continuation and explicit controls. They do not authorize iteration/timestamp guesses, permanent facts, a task mode or new recovery machinery.

The active prior-message delta still states unqualified rebuilding when the initial request is absent. Its requirement and matching scenario coexist with the later unavailable-history acceptance; this is an active wording reconciliation point, not authority to fail valid partial birth:

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:43` — `prompt so subsequent tool iterations retain it. Cold reconstruction without a retained request SHALL rebuild from`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:75` — `- **WHEN** its initial retained request is absent`

The earlier reachability witness depended on republication of a retained task. Current reviewed dispatch now skips that publication:

- `processor/agentic-dispatch/component.go:957` — `// Validated retained task evidence already proves this publication committed.`
- `processor/agentic-dispatch/component.go:958` — `if !found {`

That historical witness is not reasserted as current admitted-policy failure. Remaining supported-retention proof stays separate.

No new exported symbol, subject, bucket, field or configuration is introduced by this inventory. Present consumers are the existing task delivery owner and downstream terminal observers.

### 5. Existing problem shape

The shape is classified refusal after observation, with structural/correlation checks preceding effects. The closest same-plane instance is the approval absence owner above. An independent cross-plane instance already validates structural identity before authority refusal and owns bounded refusal diagnostics:

- `processor/graph-ingest/authority_gate.go:40` — `// direct in-process persistence — before any KV I/O. Structural validation runs`
- `processor/graph-ingest/authority_gate.go:52` — `return semtypes.ValidateEntityIDAuthority(subject, c.org, c.platform, importLane)`
- `processor/graph-ingest/authority_gate.go:79` — `func (c *Component) recordAuthorityRejection(arrival, reason string, err error) {`

This records existing shapes; adoption is not selected during inventory.

## Adopter seam inventory

| Person | Current knowledge and default path | Discovery and gap |
|---|---|---|
| Component author publishing tasks | Supply fresh LoopID for new work; preserve serialized identity for retry. Matching cold nonterminal authority currently rebuilds initial work on exact request absence. | Payload validation and correlation refusal exist; the missing-history branch can generate new work without reporting lost history. The caller should not predict internal history survival. |
| Chat adapter author | Supply displayed PriorMessages with each fresh turn. Execution recovery uses the committed task. | Existing history restoration tests cover absent initial request and retained request. The adapter should not classify partial birth or reconstruct internal tool history. |
| User-response consumer | Receives existing terminal errors through dispatch when authority supplies a valid route. | `terminal_settlement.go:159–160` maps failure to `ResponseTypeError` and includes `event.Error`. This existing route is not proof that task missing-history refusal is wired. |
| Raw task/terminal observer | Observes the existing loop terminal carrier and authority; a user route is optional. | Current `agent.failed` publication and terminal marker provide existing observable surfaces. This supplement introduces no new refusal carrier. |

## Searches and limits

Executed in the claim checkout:

1. Read-only `git status --short`, `git rev-parse HEAD`, scoped `git diff --stat` and `git diff` for recovery-adjacent changed files.
2. `gopls workspace_symbol -matcher=fuzzy recoverTaskDelivery`, `continuationUnavailable`, `partialBirth`. Recovery found; no symbol named for unavailable continuation; partialBirth returned only an unrelated graph-ingest test. Zero symbol hits do not prove semantic absence.
3. `gopls references` at recovery `377:21`, approval absence `921:21`, terminal persistence `component.go:1791:21`, evidence interface `28:6`, and `LoopEntity.Iterations` `agentic/state.go:52:2`. Default-tag references exclude integration fixtures. Initial sandbox cache access failed; read-only escalated lookups succeeded.
4. Tracked searches for `continuation_unavailable|partial.birth|missing.request|5727252562|Iterations` over settlement/approval sources and the active loop delta; and `continuation_unavailable|partial birth|partial-birth` over loop sources and ADRs.
5. Tracked integration-test searches for `partial.birth|birth.*request|missing.*request|request.*missing` and `^func Test.*(Task|Birth|Initial)`.
6. Tracked terminal-bridge searches for `ResponseTypeError|LoopFailedEvent|event.Error|OutcomeFailed|no user route`; numbered reads verified cited behavior.
7. Numbered bounded reads covered the complete recovery branch, full LoopEntity declaration, birth/publication ordering, iteration mutation/restoration, terminal owner, partial-birth fixtures and relevant existing inventory sections.

Open evidence question: current code establishes valid partial-birth examples and definite prior progress examples, but this inspection finds no existing exhaustive cold absent-request discriminator. Existing terminal refusal machinery does not supply that missing eligibility fact.

Stop for independent inventory review.
