# Inventory: R2 terminal-write boundary

## R2 addendum: cancellation between authority observation and absence settlement

base: 5e0e2259aa7392f7f3255d7f01533869862d8174

This supplements `inventory-r2-approval-evidence-2026-09-12.md`; it does not replace its evidence,
adopter inventory, or unresolved validation findings. Scope is the reproduced cancellation overwrite only.

Frozen production SHA-256:

```text
settlement_recovery.go: f7bb4576cec61452d72c5ba91e8ed37c3da86f05694677014c6473b46a129d40
approval_response_handler.go: 7850ba07b8cbe3ece0c70301c4d5b7627f234f7b59a05d3e82f4f29333c5020e
```

The existing change artifacts and applicable specs were read in earlier checkpoints. This pass refreshed
the R2 inventory, current runtime diff, task-truth diff, and relevant terminal/revision requirements.
`orchestration-check` applies: the measured boundary is existing component execution and current-state
ownership. This inventory proposes no orchestration layer, synchronization primitive, or public surface.

### Measured failure and proof limit

The existing RED log is
`/private/tmp/gh1146-approval-absence.gYminX/missing-evidence-cancel-order-red.log`,
SHA-256 `6d85ef17855dba41cf6158827ff5bfbc24c6cec2e5e9ee63e243ae22ba21162a`.

Observed output: `cancel revision=2 final revision=3 final state=failed decision=1 err=<nil> failure_read=<nil>`.
The assertions also show a newly written failure `COMPLETE_` record. Decision 1 is returned by the ACK branch
identified below. This is not an inferred race or an exactly-once-output claim.

- `processor/agentic-loop/approval_replacement_integration_test.go:107` — `// Current authority and the input are seeded unit records. Real NATS`
- `processor/agentic-loop/approval_replacement_integration_test.go:109` — `// this is an owner-ordering proof, not native cancellation settlement.`
- `processor/agentic-loop/approval_replacement_integration_test.go:129` — `closed, err := f.c.handler.CancelLoop(f.entity.ID, "reviewer")`
- `processor/agentic-loop/approval_replacement_integration_test.go:132` — `require.NoError(t, f.c.persistLoopState(ctx, f.entity.ID))`
- `processor/agentic-loop/approval_replacement_integration_test.go:135` — `require.Equal(t, uint64(2), committedRevision)`
- `processor/agentic-loop/approval_replacement_integration_test.go:136` — `f.c.releaseLoopTransientState(f.entity.ID)`
- `processor/agentic-loop/approval_replacement_integration_test.go:141` — `decision, callbackErr := f.c.handleApprovalResponseMessage(ctx, settlementEnvelope(t, &f.approval))`
- `processor/agentic-loop/approval_replacement_integration_test.go:147` — `_, failureErr := stream.GetLastMsgForSubject(ctx, "agent.failed."+f.entity.ID)`

The hook runs a distinct cancellation owner between approval's observation and its missing-evidence return.
It does not overlap two callbacks for the same fast input, execute the complete cancellation signal handler,
or prove native source ACK/redelivery. Native NATS supplies retention observation and attempted publication;
current KV authority/revision and the approval input are seeded test records.

### Observation, restoration, and terminal consequence

- `processor/agentic-loop/approval_response_handler.go:181` — `persisted, revision, err := c.readLoopEntityRevision(ctx, response.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:123` — `func (c *Component) readLoopEntityRevision(ctx context.Context, loopID string) (agentic.LoopEntity, uint64, error) {`
- `processor/agentic-loop/settlement_recovery.go:140` — `if err := entity.Validate(); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:145` — `if entity.ID != loopID || entity.TaskID == "" {`
- `processor/agentic-loop/settlement_recovery.go:151` — `return entity, entry.Revision(), nil`
- `processor/agentic-loop/approval_response_handler.go:216` — `settled, err := c.recoverApprovalResponse(ctx, response, persisted)`
- `processor/agentic-loop/approval_response_handler.go:221` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/settlement_recovery.go:735` — `return c.settleAbsentApprovalEvidence(ctx, entity, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:749` — `return c.settleAbsentApprovalEvidence(ctx, entity, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:788` — `info, err := stream.Info(ctx)`
- `processor/agentic-loop/settlement_recovery.go:796` — `age := time.Since(entity.StartedAt)`
- `processor/agentic-loop/settlement_recovery.go:806` — `if _, err := c.handler.GetLoop(entity.ID); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:813` — `if err := entity.ResolveApproval(); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:816` — `if err := c.handler.UpdateLoop(entity); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:819` — `if err := c.handler.loopManager.TransitionLoop(entity.ID, agentic.LoopStateFailed); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:826` — `failure, messages, err := c.handler.BuildFailureMessages(entity.ID, "continuation_unavailable", errorMsg)`
- `processor/agentic-loop/settlement_recovery.go:830` — `if err := c.persistHandlerResult(ctx, HandlerResult{LoopID: entity.ID, State: agentic.LoopStateFailed,`

The caller obtains the revision, but neither recovery function receives it. Following retained absence and
retention checks, the new path creates missing process state and installs the earlier entity. No intervening
current-authority observation or conditional authority write precedes its failure effects.

### Existing terminal writers and ordering

`persistHandlerResult` is the reached owner, not a missing abstraction. Its six production call sites are
model-response success/prepared failure, tool-result success/prepared failure, terminal approval rejection,
and the new absence path:

- `processor/agentic-loop/component.go:1527` — `if persistErr := c.persistHandlerResult(ctx, result); persistErr != nil {`
- `processor/agentic-loop/component.go:1549` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2026` — `if persistErr := c.persistHandlerResult(ctx, result); persistErr != nil {`
- `processor/agentic-loop/component.go:2068` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:244` — `if err := c.persistHandlerResult(ctx, result); err != nil {`

The sixth call is settlement_recovery.go:830, pinned above.

- `processor/agentic-loop/component.go:1765` — `terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed`
- `processor/agentic-loop/component.go:1772` — `defer c.releaseLoopTransientState(result.LoopID)`
- `processor/agentic-loop/component.go:1779` — `if err := c.persistFailureState(ctx, result.LoopID, result.FailureState); err != nil {`
- `processor/agentic-loop/component.go:1798` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1804` — `return c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:2107` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:2205` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2206` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2247` — `entity, err := c.handler.GetLoop(loopID)`
- `processor/agentic-loop/component.go:2257` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`

Complete/failed settlement writes required terminal material, publishes, then unconditionally persists the
process snapshot fetched at the final write. A conditional final marker alone would occur after the conflicting
failure material/publication observed in this RED; this is an ordering fact, not a selected correction.

Direct production callers of `persistLoopState` are task birth, `handleLoopFailure`, nonterminal/terminal
branches of `persistHandlerResult`, and cancellation:

- `processor/agentic-loop/component.go:1368` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1628` — `if persistErr := c.persistLoopState(ctx, loopID); persistErr != nil {`
- `processor/agentic-loop/component.go:1794` — `} else if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:2375` — `if err := c.persistLoopState(ctx, loopID); err != nil {`

component.go:1804 is pinned above.

Cancellation has its own existing ordering; it does not call `persistHandlerResult`:

- `processor/agentic-loop/component.go:2370` — `entity, err := c.handler.CancelLoop(loopID, signal.UserID)`
- `processor/agentic-loop/component.go:2412` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`
- `processor/agentic-loop/component.go:2427` — `if err := c.persistCancellationState(ctx, loopID, &completion); err != nil {`
- `processor/agentic-loop/component.go:2430` — `c.releaseLoopTransientState(loopID)`

Thus the actual cancellation handler persists bare cancelled state before terminal publication and
`COMPLETE_`. Its full final-marker conformance is not proved by this R2 regression.

### Existing synchronization and closest revision-bound shape

- `processor/agentic-loop/state.go:327` — `func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/state.go:340` — `return *entity, nil`
- `processor/agentic-loop/state.go:520` — `m.mu.Lock()`
- `processor/agentic-loop/state.go:527` — `m.loops[entity.ID] = &entity`
- `processor/agentic-loop/state.go:613` — `// establish cross-owner exclusion.`
- `processor/agentic-loop/state.go:621` — `func (m *LoopManager) ResolveApprovalIfPending(loopID, executionID, callID string) (agentic.PendingApprovalState, bool, error) {`
- `processor/agentic-loop/state.go:1450` — `m.mu.Lock()`
- `processor/agentic-loop/state.go:1458` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/state.go:1468` — `entity.State = agentic.LoopStateCancelled`
- `processor/agentic-loop/state.go:1471` — `entity.Outcome = agentic.OutcomeCancelled`
- `processor/agentic-loop/trajectory_handler_wiring.go:68` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/state.go:687` — `delete(m.loops, loopID)`

These are operation-local mutex scopes. `UpdateLoop` replaces the snapshot without a revision or terminal
guard; `CancelLoop` checks terminality and updates cancellation fields under its existing mutex. Release removes
the process owner record, so a later successful create is not evidence that durable cancellation disappeared.

The closest measured problem shape is already in this component: commit consequences against an observed
authority revision, with a defined lost-revision outcome.

- `processor/agentic-loop/component.go:2080` — `func (c *Component) persistApprovalGate(ctx context.Context, result HandlerResult, revision uint64) error {`
- `processor/agentic-loop/component.go:2089` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/component.go:2093` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/approval_response_handler.go:262` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:268` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`

New-gate publication follows its conditional commit; approved continuation publication precedes its conditional
cleared-pending commit. These are distinct existing orderings. Neither proves that the terminal absence path
already has the needed boundary. No reusable primitive is proposed, so no establishing adoption sweep is triggered.

### Consumers, obligations, and adopter seam

- `processor/agentic-loop/state.go:1415` — `// This is called when a loop finishes to populate fields for SSE delivery via KV watch.`
- `processor/agentic-dispatch/terminal_settlement.go:111` — `entry, err := kv.Get(ctx, loopID)`
- `processor/agentic-dispatch/terminal_settlement.go:122` — `return &persisted, validatePersistedLoop(loopID, &persisted)`
- `processor/agentic-dispatch/terminal_settlement.go:68` — `persistedRoute = terminalRoute{ChannelType: persisted.ChannelType, ChannelID: persisted.ChannelID, UserID: persisted.UserID}`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:216` — `Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:217` — `Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:221` — `For every terminal `LoopEntity` transition, the bare `AGENT_LOOPS/<LoopID>` terminal Put SHALL be the final`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:227` — `retained evidence. The terminal marker proves application only where the lane's required correlation identifies the`

For an external chat/component author using existing cancellation and approval surfaces:

1. Existing responsibilities are valid correlated inputs and ordinary at-least-once output handling. No current
   caller contract requires predicting the framework's pending-record revision or its retention-read timing.
2. Doing nothing special cannot prevent the measured overwrite: valid earlier pending state becomes a later
   failure and publication after cancellation has committed.
3. The measured callback returns ACK without an error. Observers see failure state/output; the test, not a
   caller-facing typed error, exposes the lost cancellation.
4. The stale-observation boundary belongs to the framework's existing state/output owner, not an adopter knob.
   This inventory adds no method, payload, bucket, subject, or configuration field.

Owner comment 5646790026 sequences this existing terminal work forward for R2 only. R3 approval-Store evidence
and its separate ruling remain held; the present-gated-result/missing-request validation RED remains an adjacent
R2 check, not part of this terminal inventory. Parent #1156/#1249 atomic landing and 15-subscription duties survive.

## Searches, completeness, and open evidence

Structural queries used the current worktree with GOPROXY=off, GOSUMDB=off and existing temporary Go caches:

- `gopls references processor/agentic-loop/component.go:1764:21`: six production and six test call sites.
- `gopls references processor/agentic-loop/component.go:2242:21`: five production and two default-build test sites.
  The integration-tagged cancellation hook at approval_replacement_integration_test.go:132 was added by direct read.
- `gopls references processor/agentic-loop/state.go:1449:23`: handler delegation plus two state tests.
- Individual `gopls workspace_symbol -matcher=fuzzy` queries for `releaseLoopTransientState`,
  `LoopManager.GetLoop`, and `UpdateCompletion` located the exact owners read above.
  An initial combined symbol query returned no matches; it was not treated as an absence claim.
- `git diff 5e0e2259 -- processor/agentic-loop/settlement_recovery.go processor/agentic-loop/approval_response_handler.go`
  established the new absence path and unchanged reached terminal writer.
- `git grep -n -E 'terminal marker|final marker|terminal state|before.*mutation|at-least-once|supersed|revision|cancel'`
  over the active loop delta located the refreshed obligations.
- Targeted `rg -n` located named owner definitions/calls for range reads; structural completeness claims above
  come from gopls. Source and RED-log hashes were checked with `shasum -a 256`.

No tests were run by this inventory pass. It establishes the local stale-observation → terminal-consequence
shape and its existing owners, not a correction. Native cancellation settlement, source ACK/redelivery after
interruption, and full cancellation final-marker conformance remain unproven here. No claim of same-input
callback concurrency, active-active safety, or exactly-once terminal output is introduced.

Stop for independent inventory review.
