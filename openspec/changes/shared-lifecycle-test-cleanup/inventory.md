# Shared lifecycle test cleanup — post-implementation inventory

base: 32c013574eb82981188bd7ed79c9a924fcb99ecb

Status: source reconciliation against the independently approved implementation freeze. Exact guard records were
reviewed and installed; the real guard passed. Final preflight and archive review remain separate gates.

## Historical authority and refresh boundary

The accepted pre-change inventory is preserved verbatim at
`32c01357:openspec/changes/shared-lifecycle-test-cleanup/inventory.md`, SHA256
`1d225f08a8c1148a16e130ef4d00ff04728b932467753086d31558bbf003c424`.
Its INVENTORY PASS applies to source baseline `926677874711f888cb06c86584484996a001d9f6`, over `bb98043a`.
That immutable record retains the original 156 pins, full searches, five categories, adopter seams, historical
hypotheses and #1416 limits. Its review is not a verdict on the changed implementation or this implementation refresh.
This document replaces obsolete active coordinates with changed-surface observations; it does not rewrite history.

This read inspected only changed support/proof/rule-fixture sources and changed testing guidance. Unchanged
production contracts, same-shape owners and external adopter measurements remain incorporated from the accepted
checkpoint. No new gopls sweep, tests, Docker run, mutation experiment or cleanup-guard invocation was performed.

## Current shared ownership surface

The public factory and three suite entry signatures remain present. New private support owns a returned instance,
rejects nil and typed nil before method dispatch, and records cancellation plus concrete terminal-attempt/results.
The five-second constant currently supplies accepted work and terminal contexts. Work contexts derive from caller
contexts; terminal Stop receives a fresh finite Background child. No operation context is stored on this owner.

- `component/lifecycle_test_suite.go:21` — `type LifecycleFactory func() LifecycleComponent`
- `component/lifecycle_test_suite.go:23` — `const lifecycleTestBudget = 5 * time.Second`
- `component/lifecycle_test_suite.go:28` — `type lifecycleTestOwner struct {`
- `component/lifecycle_test_suite.go:43` — `if value.IsNil() {`
- `component/lifecycle_test_suite.go:51` — `ctx, cancel := context.WithTimeout(parent, lifecycleTestBudget)`
- `component/lifecycle_test_suite.go:57` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), lifecycleTestBudget)`
- `component/lifecycle_test_suite.go:63` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`
- `component/lifecycle_test_suite.go:64` — `o.concreteStopErr = o.component.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:68` — `result = errors.Join(result, fmt.Errorf("terminal context ended: %w", o.stopBoundErr))`
- `component/lifecycle_test_suite.go:85` — `defer o.cancelStart() // Stop completes before accepted Start authority ends.`
- `component/lifecycle_test_suite.go:87` — `if o.attempted {`

Normal portable cases now install lexical finish before invoking the case. Rejected-Start cases do likewise.
Initialize and nil-context contract probes remain separate. CompletedRepeatedStop explicitly invokes the second
concrete Stop after the first success. Explicit abort validates the component's own caller-context cause, separately
from the owner's joined diagnostic. A returned concrete error suppresses implicit repetition; this is an attempt
record, not a claim that every runtime resource joined.

- `component/lifecycle_test_suite.go:96` — `func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {`
- `component/lifecycle_test_suite.go:124` — `defer func() {`
- `component/lifecycle_test_suite.go:129` — `workCtx = owner.workContext(t.Context())`
- `component/lifecycle_test_suite.go:153` — `require.NoError(t, owner.abortContractError(), "abort Stop must preserve its exact caller-context cause")`
- `component/lifecycle_test_suite.go:165` — `require.NoError(t, owner.component.Stop(secondCtx), "completed repeated Stop should be a no-op")`
- `component/lifecycle_test_suite.go:175` — `assert.Error(t, owner.component.Stop(nil), "Stop must reject a nil context")`
- `component/lifecycle_test_suite.go:204` — `defer func() {`
- `component/lifecycle_test_suite.go:214` — `require.NoError(t, owner.stop(workCtx, false), "pre-action Start rejection must leave Stop safe")`

## Error paths, worker admission, injection and benchmarks

The cycle installs finish before Initialize; operation and finalization errors can remain joined and distinguishable.
Workers check one failure flag before acquiring another instance, send results while peers finish, and close results
only after their WaitGroup returns. This is a concurrent admission check, not global linearizable admission election:
work already passing the check remains owned. NoLeaks returns after the failing cycle finalizes; aggregate memory
and goroutine observations remain supplementary. Benchmarks check each iteration's result before Fatal.

- `component/lifecycle_test_suite.go:224` — `defer func() {`
- `component/lifecycle_test_suite.go:226` — `resultErr = errors.Join(resultErr, fmt.Errorf("%s cleanup: %w", label, err))`
- `component/lifecycle_test_suite.go:262` — `if failed.Load() {`
- `component/lifecycle_test_suite.go:267` — `failed.Store(true)`
- `component/lifecycle_test_suite.go:277` — `workers.Wait()`
- `component/lifecycle_test_suite.go:282` — `report(err) // Caller reports while workers finish already-owned instances.`
- `component/lifecycle_test_suite.go:298` — `if err := runLifecycleCycle(t.Context(), factory, fmt.Sprintf("NoLeaks iteration %d", iteration)); err != nil {`
- `component/lifecycle_test_suite.go:300` — `return // The current lexical owner has already finalized.`
- `component/lifecycle_test_suite.go:330` — `if err := benchmarkLifecycleIteration(b, factory, mode); err != nil {`
- `component/lifecycle_test_suite.go:350` — `if err := owner.finish(workCtx, false); err != nil {`

Error injection retains the wrapper's early configured-error return. The suite owns its base separately, finishes
that base through the owner, checks Initialize/Start prerequisites and sends a finite context to the injected Stop
operation. The wrapper error therefore no longer substitutes for a base terminal attempt in the inspected source.

- `component/lifecycle_test_suite.go:437` — `func (e *ErrorInjectingComponent) Stop(ctx context.Context) error {`
- `component/lifecycle_test_suite.go:439` — `return e.stopError`
- `component/lifecycle_test_suite.go:464` — `if err := owner.finish(workCtx, false); err != nil {`
- `component/lifecycle_test_suite.go:468` — `wrapped := NewErrorInjectingComponent(owner.component)`
- `component/lifecycle_test_suite.go:480` — `require.NoError(t, owner.component.Initialize(), "base Initialize prerequisite")`
- `component/lifecycle_test_suite.go:482` — `require.NoError(t, owner.component.Start(workCtx), "base Start prerequisite")`
- `component/lifecycle_test_suite.go:483` — `operationCtx, cancelOperation := context.WithTimeout(context.Background(), lifecycleTestBudget)`
- `component/lifecycle_test_suite.go:490` — `require.ErrorIs(t, operationErr, injected, "expected %s wrapper operation error", tt.operation)`

## Current proof surface and measurement limits

New unit fixtures observe base calls, finite/live contexts, distinct errors, failed admission and selected child
process failures. They are source evidence that tests exist, not a claim here that final tests or mutations passed.
The self-reexecuted child uses this test binary, a four-second context, file-backed output and one synchronous Run
owner; no nested build or shell is present. Final source review accepted this child ownership; retained execution evidence is summarized in
`review/implementation-evidence.md`. The canceled-context Stop fake is cooperative and establishes no ignored-cancellation containment.

- `component/lifecycle_test_support_test.go:77` — `func runLifecycleSupportChild(t *testing.T, marker, runPattern string) (string, error) {`
- `component/lifecycle_test_support_test.go:79` — `ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)`
- `component/lifecycle_test_support_test.go:86` — `cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+runPattern, "-test.v")`
- `component/lifecycle_test_support_test.go:89` — `runErr := cmd.Run() // The child has no descendants; Cmd.Run owns its only Wait.`
- `component/lifecycle_test_support_test.go:132` — `func TestSharedLifecycleFatalExitPrecedesSubstrateCleanup(t *testing.T) {`
- `component/lifecycle_test_support_test.go:149` — `func TestSharedLifecycleInjectionFinalizesBase(t *testing.T) {`
- `component/lifecycle_test_support_test.go:166` — `func TestSharedLifecycleAbortRequiresConcreteCallerCause(t *testing.T) {`
- `component/lifecycle_test_support_test.go:196` — `func TestSharedLifecycleOwnedCycleTransitions(t *testing.T) {`
- `component/lifecycle_test_support_test.go:263` — `<-stopCtx.Done() // Cooperative fake; no wall-clock containment is inferred.`
- `component/lifecycle_test_support_test.go:304` — `func TestSharedLifecycleParallelReportedFailureKeepsLivePeerOwned(t *testing.T) {`
- `component/lifecycle_test_support_test.go:403` — `func TestSharedLifecycleOperationAndCleanupErrorsRemainDistinct(t *testing.T) {`
- `component/lifecycle_test_support_test.go:429` — `func TestSharedLifecycleBenchmarkChecksOperationAndFinalizer(t *testing.T) {`

The existing fatal child fails Initialize, before accepting Start. Its source alone must not be promoted into proof
of fatal exit after an accepted live Start. Separate transition, mixed-live-peer and error-retention cases, plus the exact after-Start/before-Stop mutation,
provide the reviewed combined evidence. Both mixed-peer fixture gates share one finite two-second context; omission
of Start produces a named failure after all workers join. Final tests, mutations and measured costs are retained in
`review/implementation-evidence.md`. The fatal child alone does not establish post-Start fatal behavior.

## Real rule adopter and retained external evidence

The rule fixture still uses one TestClient, platform metadata, production creation and an explicit ENTITY_STATES
watcher. A mutex-protected forwarding observer preserves exact Stop arguments/results, distinguishes nil probes,
and records nonnil invocation entry/return plus finite context supply. The old cohort Stop loop has been replaced
by an assertion-only t.Cleanup registered after TestClient, so that observer runs before NATS teardown. It accepts
one or two nonnil calls per actually returned component, accommodating the explicit repeated-Stop case.

- `processor/rule/lifecycle_integration_test.go:30` — `func (o *ruleLifecycleObservation) Stop(ctx context.Context) error {`
- `processor/rule/lifecycle_integration_test.go:39` — `err := o.LifecycleComponent.Stop(ctx)`
- `processor/rule/lifecycle_integration_test.go:41` — `o.stopReturned++`
- `processor/rule/lifecycle_integration_test.go:55` — `tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))`
- `processor/rule/lifecycle_integration_test.go:59` — `config.EntityWatchBuckets = map[string][]string{`
- `processor/rule/lifecycle_integration_test.go:66` — `Platform:   component.PlatformMeta{Org: "c360", Platform: "lifecycle"},`
- `processor/rule/lifecycle_integration_test.go:74` — `t.Cleanup(func() {`
- `processor/rule/lifecycle_integration_test.go:84` — `if calls < 1 || calls > 2 || returned != calls || !finite {`
- `processor/rule/lifecycle_integration_test.go:91` — `created, err := rule.CreateRuleProcessor(rawConfig, deps)`

A returned invocation is not proof of every internal worker joining after abort. #1416's original running-error-
repeat premise remains source-refuted only to the extent recorded in the accepted inventory: running Stop becomes
terminal after a returned cleanup error; retained failed-Start cleanup is different. No reproduced intermittent
failure or issue-closing authority is asserted by this refresh. No #1404 production source is selected here.

Four StandardLifecycleTests adopters remain the accepted measured population: HTTP, UDP, graph-index and rule.
WebSocket calls ErrorInjection and the benchmark helper. Thirteen available local sister snapshots had zero tracked
API-spelling matches, plus the successful zero-reference semconnect structural query. Those exact HEADs/searches
remain in the accepted companion; this refresh does not claim new remote/generated/untracked consumer coverage.

## Exact guard exposure and reviewed reconciliation

Reviewed baseline JSON contains 334 unchanged debt entries and 89 resolutions (formerly 86). Before this repair, selected support and
rule-cohort sources had zero direct debt entries and four exposed resolution dependency sets: wrapper Stop,
testNilStopContext, testNoResourceLeaks and testParallelFreshInstances. The exact pre-change identities/hashes remain
in accepted `review/baseline-exposure.json`, SHA256
`555716939a4322dc8b4979f8e77a3cc6235e852bef58f96509a5816f42467a4d`.
Eight nearby WebSocket debt entries remain outside this slice; no decrease in 334 is asserted.

The no-harness census captured 1,395 sites, 2,338 sources and 2,335 typed sources before metadata installation.
The independently reviewed scratch candidate reconciled 334 unbounded, 72 bounded, 351 non-lifecycle, two contract
and 636 ordinary-only uncertain sites; its temporary metadata harness added one source and was removed.
Those diagnostic census counts are checkpoint-specific, not a claim that the final nonverbose guard printed them.
Final installed-guard validation passed in 7.660s. The 334 debt records and other 82 resolutions are unchanged.
Four exposed resolutions were removed/replaced with seven reviewed exact records; final count is 89 resolutions.
`review/approved-cleanup-records.json` retains only the installed changed records, and `review/implementation-review.md`
records the exact approval and baseline identity. No analyzer semantic or scan-depth change occurred. #1417 stays open.

## Searches

1. `git status --short`; `git diff --stat`; `git rev-parse HEAD` recorded the dirty snapshot and checkpoint above.
2. `nl -ba` ranges read component/lifecycle_test_suite.go:1–493, lifecycle_test_support_test.go:1–443 and
   processor/rule/lifecycle_integration_test.go:1–111. `rg -n` located proof declarations before the full range reads.
3. `git diff -- docs/contributing/01-testing.md` inspected the added shared-support guidance; no new policy approval.
4. Python compared baseline records and decoded checkpoint census counts. Developer ran scratch reconciliation;
   coordinator ran the installed real guard, `scripts/check-cleanup-roots.sh`, exit zero (7.660s).
5. Python refreshed exact current-line pins and source SHA256 checkpoints. The canonical inventory verifier passed
   all 58 pins after the final two moved proof coordinates were reconciled.

## Source byte checkpoints

`component/lifecycle_test_suite.go` SHA256 `ec7c53826c9cb8409778346fcd2619a85d31677f85bb93234998d69a86f48ac3`.
`component/lifecycle_test_support_test.go` SHA256 `b822f4b634747142ad70ba85fa05ab4670738a59208a215c5eb6f414d7c2f824`.
`processor/rule/lifecycle_integration_test.go` SHA256 `0d6dced7a2d32c98350d80a782850b153afbe9b5c2285af773c30b23c4d413d6`.
`docs/contributing/01-testing.md` SHA256 `6079498e2850cf530f4a6d952088cf72c88408b6d1879087aca9a720b074e163`.

Coordinator refresh: updated 2 moved coordinates against the restored final Go source and recorded its
byte hashes. The accepted pre-change inventory and its review remain available at the immutable checkpoint above.
