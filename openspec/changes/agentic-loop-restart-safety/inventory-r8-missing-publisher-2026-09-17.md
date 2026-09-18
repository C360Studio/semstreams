# R8 missing-publisher inventory

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Scope: only the existing `publish_agent` absent-publisher refusal obligation. Inventory-only; no target state,
options, implementation authorization, or whole-R8 completeness claim.

Read-only worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
HEAD verified; working tree clean before and after inspection. No edits, Git mutations, tests, server observations,
or external writes. Root materialized the architect's complete handoff; formatting only was normalized.

## Accepted obligation and claimed gap

- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:47` — `An uncovered or malformed subject, absent publisher, refused AGENT dependency, marshal failure, or missing PubAck`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:48` — `SHALL fail the action before any post-send `rule.task.spawned` side effect. No core-NATS fallback is allowed for a`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:51` — `connection owner. This capability SHALL NOT add a second matcher, classifier, gate, or public API.`

The absent-publisher obligation already exists. Current implementation explicitly treats absence as a successful
no-op; the existing test requires that obsolete outcome. This is missing behavior, not merely missing proof.

- `processor/rule/actions.go:1962` — `if e.publisher != nil {`
- `processor/rule/actions.go:1981` — `} else if e.logger != nil {`
- `processor/rule/actions.go:1982` — `e.logger.Debug("Agent task not published (no publisher configured)",`
- `processor/rule/actions.go:2026` — `return nil`
- `processor/rule/actions_test.go:2222` — `func TestAction_PublishAgent_NoPublisher(t *testing.T) {`
- `processor/rule/actions_test.go:2236` — `// Should not error, just log and return`
- `processor/rule/actions_test.go:2238` — `require.NoError(t, err)`

## Current owners, spellings, and call graph

`Publisher` is an existing exported bytes-oriented interface. `ActionExecutor` owns the optional dependency;
the actual configured adapter remains `actionPublisher`.

- `processor/rule/actions.go:483` — `type Publisher interface {`
- `processor/rule/actions.go:487` — `Publish(ctx context.Context, subject string, data []byte) error`
- `processor/rule/actions.go:541` — `publisher     Publisher                    // Optional: if nil, publish actions are logged but not sent`
- `processor/rule/publisher.go:30` — `type actionPublisher struct {`
- `processor/rule/publisher.go:41` — `func (p *actionPublisher) Publish(ctx context.Context, subject string, data []byte) error {`

Four existing exported constructors expose combinations of capabilities:

- `processor/rule/actions.go:738` — `func NewActionExecutor(logger *slog.Logger) *ActionExecutor {`
- `processor/rule/actions.go:755` — `func NewActionExecutorWithMutator(`
- `processor/rule/actions.go:778` — `func NewActionExecutorFull(`
- `processor/rule/actions.go:828` — `func NewActionExecutorComplete(`

`NewActionExecutor` and `WithMutator` do not accept a publisher. `Full` and `Complete` accept `Publisher` and permit
nil. No constructor signature change is implied. `gopls references` enumerated all four constructor call sites in
this workspace. Only `NewActionExecutor` and `Complete` have production callers; `Full` and `WithMutator` references
are tests. Native-tagged fixtures also use the existing constructors. No sister-repository caller census is claimed.

- `processor/rule/processor.go:737` — `publisher := newActionPublisher(rp)`
- `processor/rule/processor.go:741` — `actionExecutor = NewActionExecutorComplete(rp.logger, mutator, publisher, kvWriter, rp.platform)`
- `processor/rule/processor.go:744` — `actionExecutor = NewActionExecutorComplete(rp.logger, nil, publisher, kvWriter, rp.platform)`
- `processor/rule/processor.go:748` — `actionExecutor = NewActionExecutor(rp.logger)`

The last line is a syntactic no-NATS constructor branch, not proof that normal startup reaches it: the same
enclosing initializer calls `rp.natsClient.JetStream()` earlier at line 701. No reachability claim is made.

`gopls call_hierarchy` found one caller function for private `publishAgentOnce`: `executePublishAgent`, at its
ordinary, resolved fan-out, and unresolved-list fallback sites.

- `processor/rule/actions.go:1623` — `func (e *ActionExecutor) executePublishAgent(ctx context.Context, action Action, ec *ExecutionContext) error {`
- `processor/rule/actions.go:1670` — `return e.publishAgentOnce(ctx, action, ec, "", "")`
- `processor/rule/actions.go:1672` — `if len(items) == 0 {`
- `processor/rule/actions.go:1679` — `return nil`
- `processor/rule/actions.go:1682` — `if err := e.publishAgentOnce(ctx, action, ec, action.ForEachVar, item); err != nil {`
- `processor/rule/actions.go:1689` — `return e.publishAgentOnce(ctx, action, ec, "", "")`

An empty resolved fan-out currently performs no dispatch and returns success without entering the per-publication
body. That differs from attempting one publication with no publisher.

## Effects and failure observation

Current ordering: substitute → construct/validate task → possible AgentRun mint/anchor writes → marshal/publication
if publisher exists → optional spawned-task triple → return nil.

- `processor/rule/actions.go:1721` — `LoopID:       uuid.NewString(),`
- `processor/rule/actions.go:1893` — `if err := task.Validate(); err != nil {`
- `processor/rule/actions.go:1926` — `if _, mintErr := agentrun.Mint(ctx, e.lifecycle,`
- `processor/rule/actions.go:1946` — `e.stampRunAnchors(ctx, ec, entityID, pendingRunMint.org, pendingRunMint.platform, pendingRunMint.firingLoopID)`
- `processor/rule/actions.go:1964` — `baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")`
- `processor/rule/actions.go:1970` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/rule/actions.go:2001` — `} else if published && e.tripleMutator != nil {`
- `processor/rule/actions.go:2010` — `if _, err := e.tripleMutator.AddTriple(ctx, ec.RuleID(), spawnedTriple); err != nil {`

Absence already prevents `rule.task.spawned` because `published` stays false. It does not prevent preceding run
mint/anchor effects when those dependencies are installed. A refusal only at the late nil branch would occur after
those effects. This is an ordering fact, not a request to redesign AgentRun.

The evaluator already owns action-error logging and the bounded failure metric:

- `processor/rule/stateful_evaluator.go:423` — `ec.State.ActionIterations[actionID] = fired + 1`
- `processor/rule/stateful_evaluator.go:426` — `if err := e.actionExecutor.Execute(ctx, action, ec); err != nil {`
- `processor/rule/stateful_evaluator.go:437` — `e.logger.Error("Failed to execute action",`
- `processor/rule/stateful_evaluator.go:448` — `e.metrics.actionFailuresTotal.WithLabelValues(action.Type).Inc()`

`ActionIterations` increments before execution; it is not proof of successful publication. `runActions` returns
no error and continues after ordinary action failure. This inventory does not supply source-ACK/redelivery safety;
that remains the separately held #1311 boundary.

## Existing problem-shape instances

The shape is an action requiring a capability and refusing observably when it is absent. It exists in this executor:

- `processor/rule/actions_lifecycle.go:166` — `func (e *ActionExecutor) requireLifecycleManager(actionType string) (LifecycleManager, error) {`
- `processor/rule/actions_lifecycle.go:167` — `if e.lifecycle == nil {`
- `processor/rule/actions_lifecycle.go:168` — `return nil, fmt.Errorf("%s: no lifecycle.Manager wired on the rule processor (call SetLifecycleManager during component init, or remove the action from the rule)", actionType)`

The related existing action preflight checks substituted data before publication:

- `processor/rule/actions.go:1708` — `if targetsReservedUserResponseSubject(subject) {`
- `processor/rule/actions.go:1709` — `return errs.WrapInvalid(`
- `processor/rule/user_response_subject_reservation_test.go:55` — `func TestActionExecutorRejectsDynamicReservedUserResponseSubjectBeforeSideEffects(t *testing.T) {`

No durable, communication or coordination primitive is proposed; a new-primitive collision table is not triggered.
The existing owners are the executor, its publisher dependency and the evaluator's error observation.

## Existing proof seams

- `processor/rule/actions_test.go:2242` — `func TestAction_PublishAgent_ErrorHandling(t *testing.T) {`
- `processor/rule/action_failure_metrics_test.go:35` — `func TestRunActions_ActionExecutionFailure_IncrementsActionFailuresMetric(t *testing.T) {`
- `processor/rule/action_failure_metrics_test.go:89` — `func TestRunActions_SuccessfulAction_DoesNotIncrementActionFailuresMetric(t *testing.T) {`
- `processor/rule/actions_test.go:1573` — `func TestAction_PublishAgent_ForEach_EmptyListNoDispatch(t *testing.T) {`

These locate no-publisher, publisher-error, evaluator error-observation, success-control and no-work fan-out seams.
No test ran during inventory; no GREEN is claimed.

## Adopter seam inventory

Person: an external Go developer composing rule actions through exported constructors without reading actions.go.

They must know `publish_agent` needs a publishing-capable executor. `NewActionExecutor`/`WithMutator` do not supply
  one; `Full`/`Complete` can receive nil.

Doing nothing can return nil for an unpublished task, after earlier run effects if lifecycle/mutator are installed.
Discovery is currently a debug log, if configured; the API compiles and a test endorses success.
They should need only the action's capability requirement and ordinary observable refusal when absent, not
  knowledge that nil means simulated success. Stream-retention arithmetic is irrelevant to this narrow seam.

The NATS-backed factory path wires the publisher. The constructor contract, fixtures and action error are the
outward impact. No new API or downstream mutation is proposed.

## Residual R8 observations — incomplete inventories

Six classifier surfaces have reviewed classification tests at publisher_test.go:84, not four producer-to-loop proof.
Generic actionPublisher correctly serves core-NATS users. Its publisher.go:50 fallback is not itself the missing
  action-specific refusal.

publishAgentOnce validates the constructed task and marshals its envelope; its call hierarchy contains no
  production-registry decode before send. Registry-boundary lowering is separate.

Exact tracked-Go search for agentstreamadmission, ObserveAndValidate and agent_stream_replay_inadmissible returned
  zero. Stream admission implementation/lowering remains unfinished.

Owner comment 5682070598 supersedes only the finite R7 verdict-retention prerequisite. Other admission, DiscardNew,
  source safety, #1311, frozen-parent and combined-proof holds remain. None is waived by this inventory.

## Measurements and searches

Fully read architect contract, project Purpose/Product Boundary, current proposal/design/tasks, rule-agent-publishing
delta, selected R7-admission/R8 checkpoint and publisher reviews under bounded intake ruling 5679435736.
No current openspec/specs/rule-agent-publishing/spec.md exists; the capability delta is additive.

Structural commands:

```text
gopls workspace_symbol -matcher=caseSensitive publishAgentOnce
gopls workspace_symbol -matcher=caseSensitive actionPublisher
gopls call_hierarchy processor/rule/actions.go:1697:26
gopls references processor/rule/actions.go:483:6
gopls workspace_symbol -matcher=caseSensitive ActionExecutor
gopls references processor/rule/actions.go:738:6
gopls references processor/rule/actions.go:755:6
gopls references processor/rule/actions.go:778:6
gopls references processor/rule/actions.go:828:6
gopls workspace_symbol -matcher=caseSensitive runActions
```

Environment: GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache, GOCACHE=/private/tmp/semstreams-r7-test-cache,
GOPROXY=off, GOSUMDB=off, GOFLAGS=-mod=readonly. Commands reported denied optional goimports index refresh but
returned workspace results. Initial reference at actions.go:486:6 addressed a comment and failed; corrected to
:483:6. A range-read with a mistyped workdir failed and was rerun in the correct worktree.

String searches:

```text
git grep -n -E 'agentstreamadmission|ObserveAndValidate|agent_stream_replay_inadmissible' -- '*.go'
git grep -n -E 'publish_agent|agent_task|agent.task' -- configs
git grep -n -E 'unregistered|malformed|missing publisher|no publisher|not configured|publish_agent|PublishAgent' -- processor/rule/*test.go
git grep -n -E 'payloadRegistry|PayloadRegistry|NewActionExecutorComplete|newActionPublisher|payloadregistry' -- processor/rule/processor.go processor/rule/factory.go processor/rule/registry.go processor/rule/*.go
git grep -n -E 'no.publisher|no publisher|NoPublisher|nil.publisher|without.publisher|without a publisher|publish_agent' -- openspec/specs/rule* docs/concepts/18-rule-driven-artifacts.md processor/rule/README.md
```

Broad config/test searches were locators, not completeness evidence. Exploratory exact queries for
Processor.payloadRegistry, UnmarshalBaseMessage and ValidateSubject returned no hits; fuzzy payloadRegistry located
the registry; fuzzy Unmarshal output was truncated. No absence premise rests on these exploratory results.
Numbered range reads verified pins.

```text
1403942ea497a7214bca8dbfa641d14cb5d93f6024f26b4561abf5ee592d7933  processor/rule/actions.go
751d8604359167aea7e964e1a2d04ce3b56da17073e6ca4fed607917facfa5a4  processor/rule/actions_test.go
d99301763e2ed76fde3e445284c1a62e09cc748e7810f105d109ed5b511c9053  processor/rule/stateful_evaluator.go
703682ee8283dfc85899b68f1f965d10ca514fc4ca9bc4b16ab4e5626c289157  processor/rule/action_failure_metrics_test.go
895f1b8fbfb2abf441d59cda1efebf6a36a91cbc296d827e1cb388cfaaea6f83  openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md
```

## Independent enumeration supplement

The reviewer independently located these additional current seams before reading inventory conclusions.
Root verified their source ranges; they extend the artifact without selecting target state.

- `processor/rule/actions_test.go:2636` — `func TestAction_PublishAgent_NoPublisherSkipsTriple(t *testing.T) {`
- `processor/rule/actions.go:864` — `return e.executePublishAgent(ctx, action, ec)`
- `processor/rule/cron_scheduler.go:660` — `if err := s.executor.Execute(ctx, action, ec); err != nil {`
- `processor/rule/cron_scheduler_test.go:936` — `func TestCronScheduler_Metrics_FireErrorRecorded(t *testing.T) {`

The second nil-publisher test also expects success while proving no spawned triple. Cron is the other production
Execute caller and already reports action failure through its warning and error-status metric. Generic publish
and approve nil behavior belongs to separate actions and is outside this slice. No new telemetry owner is needed.

Stop at inventory handoff for independent INVENTORY PASS.
