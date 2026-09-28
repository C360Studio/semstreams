# Inventory: processor/rule/cron_scheduler.go — CronScheduler Stop path and repo-wide owner-lane shape

base: 7a91400ad0d97d4fd2796c57e5d6ca79d17ff8eb

This pass exceeded the contract's 40-tool-call bound (~60 calls made). Every search run is recorded under
`## Searches` with its hit count. Items not run are marked `NOT RUN` in that section. See the handoff note to the
caller for the specific gaps this caused.

## Claimed gap

Issue #1283's body is entirely about `processor/rule/processor.go` (`settleRuntimeCommandFence` at `:1502`,
`submitRuntimeCommand` at `:644`, `failQueuedRuntimeCommands` at `:615`, `fenceRuntimeCommands` at `:661`,
`coordinatorDone` at `:588`) plus two unrelated residuals in `message_handler.go:88` and `entity_watcher.go:459`.
It names zero `cron_scheduler.go` lines. Our own draft PR #1409 (`Closes #1283`, this branch) is the only place in
the tree naming `cron_scheduler.go` lines against #1283 — see Adjacent claims for the exact quote. The lines below
are this file's structural analog of the same shape.

- `processor/rule/cron_scheduler.go:496` — `barrier := s.fenceDispatch()`
- `processor/rule/cron_scheduler.go:500` — `<-barrier`
- `processor/rule/cron_scheduler.go:505` — `<-dispatchDone`
- `processor/rule/cron_scheduler.go:337` — `return <-dispatch.result`
- `openspec/specs/component-lifecycle/spec.md:34` — `Stop MUST be caller-bounded`

The spec line's premise is "a successfully running component's Stop" with a caller context; `CronScheduler.Stop()`
(`cron_scheduler.go:472`) takes no `context.Context` parameter at all, so whether the spec's "caller-bounded"
requirement binds a non-`LifecycleComponent` type like `CronScheduler` directly was not resolved here (a judgment
question, out of scope for this inventory).

## Spellings of the fact

Struct declaration and fields (`cron_scheduler.go:41-63`):

- `processor/rule/cron_scheduler.go:41` — `type CronScheduler struct {`
- `processor/rule/cron_scheduler.go:42` — `*cronlib.Cron`
- `processor/rule/cron_scheduler.go:49` — `mu      sync.Mutex`
- `processor/rule/cron_scheduler.go:52` — `lifecycleMu`
- `processor/rule/cron_scheduler.go:53` — `lifecycleUsed`
- `processor/rule/cron_scheduler.go:54` — `startDone`
- `processor/rule/cron_scheduler.go:55` — `stopDone`
- `processor/rule/cron_scheduler.go:56` — `cancel`
- `processor/rule/cron_scheduler.go:57` — `registerFence`
- `processor/rule/cron_scheduler.go:58` — `dispatchMu`
- `processor/rule/cron_scheduler.go:59` — `dispatchQueue`
- `processor/rule/cron_scheduler.go:60` — `dispatchWake`
- `processor/rule/cron_scheduler.go:61` — `dispatchDone`
- `processor/rule/cron_scheduler.go:62` — `dispatchFence`
- `processor/rule/cron_scheduler.go:66` — `func(context.Context) error`
- `processor/rule/cron_scheduler.go:67` — `result chan error`
- `processor/rule/cron_scheduler.go:70` — `cronStopContext`

`cron` (the robfig handle, field `:42`): writer `NewCronScheduler` (`:158`, `cron: cronlib.New()`); callers
`Register` (`:201`), `Deregister` (`:226`), `Start` (`:272`), `Stop` (`:497`). No lock guards these four call sites
against each other beyond `s.mu` (Register/Deregister) or `s.lifecycleMu` (Start/Stop); robfig's own internal
`runningMu` is the only mutual exclusion between `Start()`/`Stop()` on the library side.

- `processor/rule/cron_scheduler.go:158` — `cron:     cronlib.New()`
- `processor/rule/cron_scheduler.go:201` — `entry.entryID = s.cron.Schedule(rule.Schedule(), job)`
- `processor/rule/cron_scheduler.go:226` — `s.cron.Remove(entry.entryID)`
- `processor/rule/cron_scheduler.go:272` — `s.cron.Start()`
- `processor/rule/cron_scheduler.go:497` — `nativeStop := s.cron.Stop()`

`lifecycleMu` guards every field above it; held at `Register` (`:176-204`), `Start` (`:254-266, :275-278`), `Stop`
(`:475-494`).

- `processor/rule/cron_scheduler.go:176` — `s.lifecycleMu.Lock()`
- `processor/rule/cron_scheduler.go:254` — `s.lifecycleMu.Lock()`

`lifecycleUsed`: written unconditionally `= true` at `Start` (`:259`) and again at `Stop` (`:487`, `Stop` does not
branch on its prior value — it only branches on `startDone`/`stopDone`). Read only at `Start` (`:255`) as the
already-started guard; `Stop` never reads it as a guard.

- `processor/rule/cron_scheduler.go:255` — `if s.lifecycleUsed {`
- `processor/rule/cron_scheduler.go:259` — `s.lifecycleUsed = true`
- `processor/rule/cron_scheduler.go:487` — `s.lifecycleUsed = true`

`startDone`: created `Start` (`:260`), closed `Start` (`:276`), reset to nil `Start` (`:277`). Read by `Stop`'s loop
(`:476-480`): if non-nil, `Stop` releases `lifecycleMu` and blocks on `<-startDone` with no timeout of its own
(`Stop` takes no ctx), then re-enters the loop.

- `processor/rule/cron_scheduler.go:260` — `s.startDone = make(chan struct{})`
- `processor/rule/cron_scheduler.go:276` — `close(s.startDone)`
- `processor/rule/cron_scheduler.go:277` — `s.startDone = nil`
- `processor/rule/cron_scheduler.go:476` — `if s.startDone != nil {`
- `processor/rule/cron_scheduler.go:479` — `<-startDone`

`stopDone`: created `Stop` (`:488`), closed inside the goroutine `Stop` spawns (`:507`). Read by `Stop`'s own loop
on a second/concurrent call (`:482-485`, returns the existing channel wrapped in `cronStopContext`).

- `processor/rule/cron_scheduler.go:482` — `if s.stopDone != nil {`
- `processor/rule/cron_scheduler.go:485` — `return cronStopContext{done: stopDone}`
- `processor/rule/cron_scheduler.go:488` — `s.stopDone = make(chan struct{})`
- `processor/rule/cron_scheduler.go:507` — `close(stopDone)`

`cancel`: written `Start` (`:262`, from `context.WithCancel(ctx)` at `:261`). Read by `Stop` (`:492`, captured
under lock) and invoked inside `Stop`'s goroutine (`:501-502`, only after both `<-nativeStop.Done()` and
`<-barrier` return).

- `processor/rule/cron_scheduler.go:261` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/rule/cron_scheduler.go:262` — `s.cancel = cancel`
- `processor/rule/cron_scheduler.go:492` — `cancel := s.cancel`
- `processor/rule/cron_scheduler.go:501` — `if cancel != nil {`
- `processor/rule/cron_scheduler.go:502` — `cancel()`

`registerFence`: written `Stop` (`:489`, `= true`, never reset). Read only by `Register` (`:177`); `Deregister`
never reads it or any lifecycle field.

- `processor/rule/cron_scheduler.go:489` — `s.registerFence = true`
- `processor/rule/cron_scheduler.go:177` — `if s.registerFence || s.stopDone != nil {`
- `processor/rule/cron_scheduler.go:218` — `func (s *CronScheduler) Deregister(ruleID string) {`

`dispatchMu` guards `dispatchQueue`, `dispatchWake` (assigned once at Start, read-only after), `dispatchDone`
(read-only after Start), `dispatchFence`. Held in `runDispatcher` (`:291-298`), `failDispatchQueue`
(`:307-310`), `submitDispatch` (`:319-332`), `fenceDispatch` (`:342-360`).

- `processor/rule/cron_scheduler.go:282` — `func (s *CronScheduler) runDispatcher(ctx context.Context) {`
- `processor/rule/cron_scheduler.go:283` — `defer close(s.dispatchDone)`
- `processor/rule/cron_scheduler.go:289` — `case <-s.dispatchWake:`
- `processor/rule/cron_scheduler.go:299` — `dispatch.result <- dispatch.run(ctx)`
- `processor/rule/cron_scheduler.go:306` — `func (s *CronScheduler) failDispatchQueue(err error) {`
- `processor/rule/cron_scheduler.go:317` — `func (s *CronScheduler) submitDispatch(run func(context.Context) error) error {`
- `processor/rule/cron_scheduler.go:320` — `if s.dispatchFence || s.dispatchWake == nil {`
- `processor/rule/cron_scheduler.go:325` — `case <-s.dispatchDone:`
- `processor/rule/cron_scheduler.go:330` — `s.dispatchQueue = append(s.dispatchQueue, dispatch)`
- `processor/rule/cron_scheduler.go:334` — `case wake <- struct{}{}:`
- `processor/rule/cron_scheduler.go:340` — `func (s *CronScheduler) fenceDispatch() <-chan error {`
- `processor/rule/cron_scheduler.go:343` — `s.dispatchFence = true`
- `processor/rule/cron_scheduler.go:344` — `if s.dispatchDone == nil {`
- `processor/rule/cron_scheduler.go:351` — `case <-s.dispatchDone:`
- `processor/rule/cron_scheduler.go:358` — `s.dispatchQueue = append(s.dispatchQueue, barrier)`
- `processor/rule/cron_scheduler.go:263` — `s.dispatchWake = make(chan struct{}, 1)`
- `processor/rule/cron_scheduler.go:264` — `s.dispatchDone = make(chan struct{})`
- `processor/rule/cron_scheduler.go:265` — `s.dispatchFence = false`
- `processor/rule/cron_scheduler.go:268` — `go s.runDispatcher(runCtx)`
- `processor/rule/cron_scheduler.go:493` — `dispatchDone := s.dispatchDone`
- `processor/rule/cron_scheduler.go:504` — `if dispatchDone != nil {`

`Stop`'s own body and spawned goroutine, in order:

- `processor/rule/cron_scheduler.go:472` — `func (s *CronScheduler) Stop() context.Context {`
- `processor/rule/cron_scheduler.go:496` — `barrier := s.fenceDispatch()`
- `processor/rule/cron_scheduler.go:498` — `go func() {`
- `processor/rule/cron_scheduler.go:499` — `<-nativeStop.Done()`
- `processor/rule/cron_scheduler.go:500` — `<-barrier`
- `processor/rule/cron_scheduler.go:505` — `<-dispatchDone`

`fire` and `dispatchAndRecord` (called by whichever goroutine robfig spawns for a tick — see Problem shape iv):

- `processor/rule/cron_scheduler.go:197` — `job := cronlib.FuncJob(func() {`
- `processor/rule/cron_scheduler.go:198` — `s.fire(ruleID)`
- `processor/rule/cron_scheduler.go:528` — `func (s *CronScheduler) fire(ruleID string) {`
- `processor/rule/cron_scheduler.go:594` — `if err := s.submitDispatch(func(ctx context.Context) error {`
- `processor/rule/cron_scheduler.go:618` — `func (s *CronScheduler) dispatchAndRecord(ctx context.Context, ruleID string, entry *cronEntry, previousFiredNanos int64) {`

`cronStopContext` (`:70-82`): `Deadline` always `(time.Time{}, false)`, `Done` returns the stored channel, `Err`
returns `context.Canceled` once `done` is closed else `nil`, `Value` always `nil`.

- `processor/rule/cron_scheduler.go:72` — `Deadline() (time.Time, bool)`
- `processor/rule/cron_scheduler.go:73` — `Done() <-chan struct{}`
- `processor/rule/cron_scheduler.go:82` — `func (cronStopContext) Value(any) any { return nil }`

Production has exactly one consumer of `Stop`'s returned context: `processor/rule/processor.go:1345` —
`cronScheduler.Stop().Done()` — reads only `.Done()`.

- `processor/rule/processor.go:1343` — `var cronDone <-chan struct{}`
- `processor/rule/processor.go:1345` — `cronDone = cronScheduler.Stop().Done()`
- `processor/rule/processor.go:1394` — `for _, done := range []<-chan struct{}{cronDone} {`

`gopls references` lists nine additional call sites of `Stop`, all in `cron_scheduler_test.go`. Whether any of
those reads `.Err()`/`.Deadline()`/`.Value()` beyond `.Done()` was not checked per-callsite (budget; see Searches
NOT RUN).

## Adjacent claims

- `docs/adr/031-time-trigger-primitive.md:275` — `core + lifecycle wiring`

That line is the decision-log table row: "`339773a` | `CronScheduler` core + lifecycle wiring
(`Register`/`Deregister`/`Start`/`Stop`, FireEveryN gate, cooldown gate, inflight CAS, panic backstop)." No line in
this ADR discusses Stop's boundedness or ownership beyond naming the methods that exist.

- `docs/adr/048-bounded-dispatcher-and-triples-substrate.md:183` — `NOT a workflow engine`
- `pkg/dispatch/doc.go:26` — `BoundedDispatcher is NOT`
- `pkg/lifecycle/doc.go:7` — `SUBSTRATE CONVENTION LAYER`
- `pkg/lifecycle/doc.go:16` — `does NOT provide`
- `pkg/lifecycle/doc.go:19` — `A process orchestrator`
- `pkg/lifecycle/doc.go:20` — `A replacement for components`
- `docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md:618` — `What this ADR is NOT`

That ADR-049 section is a scope-disclaimer about the ADR document itself (it does not claim the lifecycle-harness
concept is wrong, does not retroactively fault ADR-047, and is not a breaking change to consumers) — it does not
use the "NOT a workflow engine / NOT a process orchestrator" phrasing; that phrasing lives only in
`pkg/lifecycle/doc.go` and `pkg/dispatch/doc.go`.

- `openspec/specs/component-lifecycle/spec.md:9` — `MUST own continuing component work`
- `openspec/specs/component-lifecycle/spec.md:32` — `Running Stop has no shared-generation contract`
- `openspec/specs/component-lifecycle/spec.md:34` — `Stop MUST be caller-bounded`
- `processor/rule/readiness_integration_test.go:197` — `abort Stop must remain a synchronous bounded lifecycle call`

That assertion is over `processor.go`'s `Stop`, not `CronScheduler.Stop`.

- `git grep -n -i cron processor/rule/docs/*.md` → 0 — no doc under `processor/rule/docs/` mentions cron stop or the
  dispatcher.

Issue and PR context (not path:line pins):

- #1283 — "A bounded rule Stop can block forever on an orphaned fence barrier" — body scoped entirely to
  `processor.go`; see Claimed gap.
- #1273 / #1274 — "fix(rule): preserve runtime ownership through shutdown". The #1274 commit `40cb067d` touches
  only `processor/rule/lifecycle_runtime_test.go`, `processor/rule/processor.go`, `processor/rule/readiness.go` —
  zero hunks in `cron_scheduler.go` (`git show 40cb067d --stat`; `git show 40cb067d -- processor/rule/cron_scheduler.go`
  produced no diff). The commit message states: "Refs #1283 for the separate unchanged fence and ownership
  residuals; this merge does not claim to fix them." `git log -S runDispatcher -- processor/rule/cron_scheduler.go`
  → `c7ca5d0f` ("refactor(rule)!: restore lifecycle context ownership", 2026-08-20, 243 lines changed in this file)
  is the commit that introduced the current dispatch-queue/fence shape, three weeks before #1273/#1274.
- #1409 (our own draft PR, `Closes #1283`, branch `claude/gh1283-rule-stop-bounded-fence`) states verbatim: "The
  same drain→close window exists in `CronScheduler` (`cron_scheduler.go:496-500`), which is the frame the CI hang
  on PR #1404 (`ada5c46a`) actually reported," and records the owner's 2026-09-28 scope-widening to "inventory the
  rule processor and cron scheduler top-down against the framework's established lifecycle and dispatch idioms
  before choosing between a minimal patch and a consolidation."
- #1408 (Codex draft PR, `Closes #1397`) records the parallel-work split: "Codex takes #1397; Claude may claim
  #1283. Rule runtime-command and cron shutdown fixes remain Claude's production lane."
- `openspec list` → one in-flight change, `rule-bounded-stop-fence` (this one); `openspec/changes/rule-bounded-stop-fence/`
  holds only `.openspec.yaml` and `README.md` — no proposal/design/tasks yet.

## Consumers

- `processor/rule/cron_scheduler.go:317` — `func (s *CronScheduler) submitDispatch(run func(context.Context) error) error {`

`gopls references` on this line → 1 production call site: `cron_scheduler.go:594` (inside `fire`'s closure).

- `processor/rule/cron_scheduler.go:340` — `func (s *CronScheduler) fenceDispatch() <-chan error {`

`gopls references` on this line → 1 call site: `cron_scheduler.go:496` (inside `Stop`).

- `processor/rule/cron_scheduler.go:172` — `func (s *CronScheduler) Register(rule *CronRule) error {`

`gopls references` → production: `processor/rule/runtime_config.go:200`; 45 more hits are all
`cron_scheduler_test.go` (46 total).

- `processor/rule/runtime_config.go:200` — `rp.cronScheduler.Register(cronRule)`

- `processor/rule/cron_scheduler.go:218` — `func (s *CronScheduler) Deregister(ruleID string) {`

`gopls references` → production: `processor/rule/runtime_config.go:199, 260, 285`; test hits:
`cron_scheduler_test.go:126, 133, 140, 934` (7 total).

- `processor/rule/cron_scheduler.go:247` — `func (s *CronScheduler) Start(ctx context.Context) error {`

`gopls references` → production: `processor/rule/processor.go:1009`; 9 more hits are `cron_scheduler_test.go` (10
total).

- `processor/rule/cron_scheduler_test.go:250` — `func TestCronScheduler_StopOnNeverStartedIsSafe(t *testing.T) {`
- `processor/rule/cron_scheduler_test.go:259` — `func TestCronScheduler_StandaloneStartContextAndStopSettlement(t *testing.T) {`

This test blocks a `fire` mid-dispatch via a `blockingContextExecutor`, calls `Stop()`, and asserts settlement does
not close (`select { case <-settlement.Done(): t.Fatal(...) default: }`) until the blocked dispatch releases.

- `processor/rule/cron_scheduler_test.go:187` — `func TestCronScheduler_StopSerializesWithAdmittedRegister(t *testing.T) {`
- `processor/rule/cron_scheduler_test.go:174` — `func TestCronScheduler_RegisterAfterStopIsRejectedWithoutMutation(t *testing.T) {`

`git grep -n "dispatchDone\|dispatchFence\|dispatchWake\|dispatchQueue" processor/rule/cron_scheduler_test.go` → 0
— no test in this file touches the private dispatch-lane fields directly, or exercises the specific orphan window
named in PR #1409 (a barrier appended to the queue after `runDispatcher` has already exited via `ctx.Done()` but
before `fenceDispatch`'s nil-check/select observes that exit).

`git grep -l 'testing/synctest' -- '*.go'` → 4 files repo-wide, none is `cron_scheduler_test.go`:
`processor/graph-index/reconciliation_model_test.go`, `processor/graph-index/reconciliation_prop_test.go`,
`processor/rule/lifecycle_runtime_test.go`, `service/metrics_forwarder_test.go`.

- `processor/rule/lifecycle_runtime_test.go:9` — `testing/synctest`

That file's `synctest.Test`/`synctest.Wait` usage (`:26, :93, :120`) is over `processor.go`'s coordinator/fence
path, not over `CronScheduler`.

## Problem shape

(i) Serialized owner-lane shape (queue of `func(context.Context) error` + `result chan error` + wake + done +
fence/barrier) — searched repo-wide for a third instance and found none:

- `processor/rule/cron_scheduler.go:67` — `result chan error`
- `processor/rule/processor.go:576` — `result chan error`
- `processor/rule/cron_scheduler.go:60` — `dispatchWake`
- `processor/rule/processor.go:113` — `commandWake`

`git grep -rn "^\s*result\s\+chan error" -- '*.go' | grep -v _test.go` → exactly these 2, repo-wide.
`git grep -nE 'ake +chan struct\{\}' -- '*.go'` → exactly these 2 (`dispatchWake`, `commandWake`), repo-wide.
`git grep -nwE 'fence|Fence' -- '*.go' | grep -v processor/rule | grep -v _test.go` → 14 hits, none naming a
queue/wake/done/barrier lane (component admission fences, graph revision fences, markdown code-fence comments).
`git grep -nw barrier -- '*.go' | grep -v processor/rule | grep -v _test.go` → 24 hits, all the unrelated E2E
"process-replacement barrier" tool (`internal/e2eboot/options_process_barrier.go`,
`test/e2e/harness/processbarrier/*`, `test/e2e/scenarios/agentic/stage_a_process_replacement.go`) plus incidental
boot-barrier comments — not a dispatch-lane barrier. `git grep -nE 'chan error$' -- '*.go' | grep -v _test.go` → 7:
the 2 above plus 5 `serveDone chan error` fields (`gateway/graph-gateway/component.go:282,760`,
`input/websocket/websocket_input.go:45`, `metric/handler.go:31`, `output/websocket/websocket.go:143`) — a single
completion signal for a server goroutine, a different shape, not a per-item dispatch result. Confirmed: exactly two
instances of the full owner-lane shape in the repo (`processor/rule/cron_scheduler.go` and
`processor/rule/processor.go`).

(ii) Nearest framework-owned bounded stop/join shapes:

- `pkg/dispatch/dispatcher.go:196` — `func (d *BoundedDispatcher[W]) Stop(ctx context.Context) error {`
- `pkg/dispatch/dispatcher.go:231` — `poolErr := d.pool.Stop(timeout)`
- `pkg/dispatch/dispatcher.go:239` — `return errors.Join(poolErr, completionErr, ctx.Err())`
- `pkg/dispatch/keyed_pool.go:365` — `func (p *KeyedPool[W]) Stop(ctx context.Context) error {`
- `pkg/dispatch/keyed_pool.go:377` — `select {`
- `pkg/dispatch/keyed_pool.go:378` — `case <-stopDone:`
- `pkg/dispatch/keyed_pool.go:380` — `case <-ctx.Done():`
- `pkg/worker/pool.go:242` — `func (p *Pool[T]) Stop(timeout time.Duration) error {`
- `natsclient/client.go:796` — `func (s *Subscription) Drain(ctx context.Context) error {`

`BoundedDispatcher.Stop` requires a non-nil `ctx`, translates its deadline into a duration for the legacy
`pkg/worker.Pool.Stop(timeout)` API, joins an optional completion watcher against the same `ctx`, and returns
`errors.Join` of all three. `KeyedPool.Stop` rejects nil ctx, signals a `stopOnce`-guarded close, then selects on
`stopDone` vs `ctx.Done()` — the exact "select on caller ctx vs internal done" shape `CronScheduler.Stop`'s
goroutine (`:498-507`) does not use, because it has no ctx to select on. `func.*ConsumeContext.*Drain` /
`.*Closed` → 0 hits under that spelling; not found under any spelling tried (see Searches NOT RUN).

(iii) How sibling components marshal runtime mutations without retaining the Start ctx:

- `processor/rule/runtime_config.go:14` — `func (rp *Processor) ApplyConfigUpdate(changes map[string]any) error {`
- `processor/rule/kv_config_integration.go:43` — `processor  *Processor`
- `processor/rule/kv_config_integration.go:336` — `rcm.processor.ApplyConfigUpdate(changes)`

`git grep -n "RuntimeConfigurable"` finds only doc-comment/test-name uses, never a `type RuntimeConfigurable
interface` declaration. `gopls workspace_symbol ApplyConfigUpdate` → exactly one production declaration
(`runtime_config.go:14`), a concrete method on `*Processor`; `kv_config_integration.go`'s `processor` field is a
concrete `*Processor`, not an interface. The brief's "find the interface first" premise does not hold — there is
no such interface in this codebase under this name.

- `processor/rule/runtime_config.go:33` — `rp.mu.RLock()`
- `processor/rule/runtime_config.go:34` — `running := rp.running`
- `processor/rule/runtime_config.go:36` — `if running {`
- `processor/rule/runtime_config.go:37` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
- `processor/rule/runtime_config.go:41` — `return rp.applyConfigUpdate(nil, changes, buckets, hasBuckets)`

The running-processor path submits a closure into the same runtime-command queue named in #1283 (the `processor.go`
twin of `CronScheduler`'s dispatch queue), which supplies `ctx` from the processor's own run-context when the
coordinator dequeues it. The not-running path passes a literal `nil` ctx straight through.

- `processor/agentic-loop/component.go:741` — `func (c *Component) Stop(ctx context.Context) error {`
- `processor/agentic-loop/component.go:759` — `if c.startDone != nil {`
- `processor/agentic-loop/component.go:763` — `case <-done:`
- `processor/agentic-loop/component.go:765` — `case <-ctx.Done():`
- `processor/graph-ingest/component.go:1065` — `func (c *Component) Stop(ctx context.Context) error {`
- `processor/graph-ingest/component.go:1084` — `if c.startDone != nil {`
- `processor/graph-ingest/component.go:1090` — `case <-ctx.Done():`
- `processor/gated-dag/component.go:323` — `func (c *Component) Stop(ctx context.Context) error {`
- `processor/gated-dag/component.go:335` — `if c.exec.cancel == nil {`
- `processor/gated-dag/component.go:338` — `if err := c.exec.stop(ctx); err != nil {`

All three take `Stop(ctx context.Context) error` and select on `ctx.Done()` at every wait. `CronScheduler.Stop()`
matches none of them: no ctx parameter, a hand-rolled return type, and its waits (`:479, :499, :500, :505`) have no
`ctx.Done()` arm because there is no ctx available to select on.

(iv) Hand-rolled `context.Context` from Stop, or a goroutine spawned inside Stop that outlives Stop's return:

- `processor/rule/cron_scheduler.go:70` — `cronStopContext`
- `input/udp/udp_lifecycle_test.go:33` — `observedAfterFuncContext`

`git grep -nE '\) Deadline\(\) \(time\.Time, bool\)' -- '*.go'` → exactly these 2, repo-wide; the second is a
test-only helper, not a production Stop return.

An awk scope-tracked search for `go func()` literally inside a `func (...) Stop(` body, repo-wide
(`grep -v _test.go`), found 6 hits:

- `config/manager.go:459` — `func (cm *Manager) Stop(timeout time.Duration) error {`
- `config/manager.go:474` — `go func() {`
- `input/file/file.go:408` — `func (f *Input) Stop(ctx context.Context) error {`
- `input/file/file.go:421` — `go func() {`
- `input/http/http.go:297` — `func (h *Input) Stop(ctx context.Context) error {`
- `input/http/http.go:310` — `go func() {`
- `pkg/worker/pool.go:255` — `go func() {`
- `processor/agentic-tools/recording.go:128` — `func (r *RecordingExecutor) Stop(timeout time.Duration) error {`
- `processor/agentic-tools/recording.go:132` — `go func() {`
- `processor/rule/cron_scheduler.go:498` — `go func() {`

Whether each of the 5 non-`cron_scheduler.go` goroutines is joined before its own `Stop` returns, or outlives it
the way `cron_scheduler.go`'s does, was not checked (budget; see Searches NOT RUN) beyond confirming the enclosing
`Stop` signature and start line for each.

Robfig's own `Cron.Stop()` (`github.com/robfig/cron/v3@v3.0.1/cron.go:323`, outside this repo, not pinnable) builds
its returned context from the standard library — `ctx, cancel := context.WithCancel(context.Background())` then
`go func() { c.jobWaiter.Wait(); cancel() }()` — not a hand-rolled struct implementing the four `Context` methods
the way `cronStopContext` does. `Cron.Start()` (`cron.go:215`) spawns `go c.run()` (`:222`), the internal ticker
goroutine; each tick's job runs on its own fresh goroutine (`cron.go:310-312`, `go func() { ...; j.Run() }()`) with
no context parameter anywhere in the robfig API (`FuncJob.Run()` at `cron.go:136` is `func() { f() }`). This is the
goroutine that ultimately calls `CronScheduler.fire` (via the closure at `cron_scheduler.go:197-198`); it carries
no context from robfig — the ctx used inside `dispatchAndRecord` (`:618`) is the `runCtx` captured by
`runDispatcher` at `Start` (`:261, :268`), delivered across the dispatch-queue channel, not through robfig.

(v) Copy-pasted `lifecycleUsed`/`terminal`/`startDone`/`stopping`/`cleanupPending` idiom:

- `pkg/lifecycle/doc.go:7` — `SUBSTRATE CONVENTION LAYER`

`git grep -l 'lifecycleUsed' -- '*.go' | grep -v _test.go` → 30 files (matches the brief's starting count):
`examples/processors/{document,iot_sensor,weather_station}/component.go`, `gateway/graph-gateway/component.go`,
`input/websocket/websocket_input.go`, `output/{file,httppost,otel,websocket}/*.go`,
`processor/agentic-{dispatch,governance,loop,model,tools}/component.go`,
`processor/agentic-loop/trajectory_observability.go`,
`processor/graph-{clustering,embedding,index-spatial,index-temporal,ingest,query}/component.go`,
`processor/json_{filter,generic,map}.go`, `processor/rule/{cron_scheduler,kv_config_integration,processor}.go`,
`service/{component_manager,message_logger}.go`, `storage/objectstore/component.go`.
`git grep -l 'cleanupPending' -- '*.go' | grep -v _test.go | wc -l` → 34 (superset; the extra 4 files were not
individually re-enumerated — budget). `git grep -n 'lifecycleUsed' pkg/lifecycle` → 0 — `pkg/lifecycle`'s own
source does not use this field name at all; the canonical `Participant`/`Manager` harness (ADR-049) is not itself
built on this idiom, and the 30 files above copy-paste the pattern into their own component structs independently
of it. `git grep -n 'lifecycleUsed' docs openspec .agents` → 12 hits, all in `docs/proposals/*.md` or
`openspec/changes/archive/*` (design/inventory/recovery-ledger documents analyzing the pattern) — none in
`docs/adr/`, `openspec/specs/`, or `.agents/contracts/`. `git grep -n 'cleanupPending' docs openspec .agents` → 19
hits, same shape, plus one mention in `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md`
about the general concept of retained cleanup authority, not naming the field. No ADR, contract, or
`openspec/specs/*/spec.md` file names `lifecycleUsed` or `cleanupPending` as the sanctioned idiom for a
`LifecycleComponent`'s Stop guard.

## Searches

- `git rev-parse HEAD` → `7a91400ad0d97d4fd2796c57e5d6ca79d17ff8eb`
- `wc -l processor/rule/cron_scheduler.go processor/rule/processor.go` → 716 / 1770
- `gopls version` → `golang.org/x/tools/gopls v0.20.0`
- `cat -n processor/rule/cron_scheduler.go` (full read; the exact surface named in the brief) → 716 lines
- `sed -n '1335,1400p' processor/rule/processor.go` → located `:1343, :1345, :1394`
- `gopls workspace_symbol -matcher=fuzzy CronScheduler` → 49
- `grep -n cronDone processor/rule/processor.go` → 3
- `gopls references processor/rule/cron_scheduler.go:317:29` (submitDispatch) → 1
- `gopls references processor/rule/cron_scheduler.go:340:29` (fenceDispatch) → 1
- `gopls references processor/rule/cron_scheduler.go:172:29` (Register) → 46
- `gopls references processor/rule/cron_scheduler.go:218:29` (Deregister) → 7
- `gopls references processor/rule/cron_scheduler.go:247:29` (Start) → 10
- `gopls references processor/rule/cron_scheduler.go:472:29` (Stop) → 10
- `gopls workspace_symbol -matcher=fuzzy ApplyConfigUpdate` → 4
- `git grep -n "ApplyConfigUpdate" -- '*.go' | grep -v _test.go` → 9
- `sed -n '1,45p' processor/rule/runtime_config.go` → read
- `git grep -n "ApplyConfigUpdate(changes" -- '*.go' | grep -v processor/rule` → 0
- `git grep -n "RuntimeConfigurable" -- '*.go'` → 6 (doc-comment/test-name only, no interface decl)
- `sed -n '30,120p' processor/rule/runtime_config.go` → read
- `grep -n "processor " processor/rule/kv_config_integration.go` → confirms concrete field at `:43`
- `ls processor/ | grep -iE 'agentic-loop|graph-ingest|gated-dag'` → 3
- `grep -rn "func.*Stop(" processor/agentic-loop/*.go processor/graph-ingest*/*.go | grep -v _test.go` → 2
- `find . -iname '*gated*dag*'` → 8
- `sed -n '735,780p' processor/agentic-loop/component.go` → read
- `sed -n '1060,1110p' processor/graph-ingest/component.go` → read
- `grep -n "func.*Stop(" processor/gated-dag/*.go | grep -v _test.go` → 1 method decl (`component.go:323`;
  `executor.go:391` is a `defer watcher.Stop()` call site, not a decl)
- `sed -n '320,360p' processor/gated-dag/component.go` → read
- `git grep -l 'lifecycleUsed' -- '*.go' | grep -v _test.go | wc -l` → 30
- `git grep -l 'lifecycleUsed' -- '*.go' | grep -v _test.go` → 30 files listed (Problem shape v)
- `git grep -l 'cleanupPending' -- '*.go' | grep -v _test.go | wc -l` → 34
- `ls pkg/lifecycle/*.go | grep -v _test` → 13
- `git grep -n 'lifecycleUsed' docs openspec .agents` → 12
- `git grep -n 'cleanupPending' docs openspec .agents` → 19
- `git grep -n 'lifecycleUsed' pkg/lifecycle` → 0
- `grep -n "func.*Stop(\|type.*struct\|BoundedDispatcher\|KeyedPool" pkg/dispatch/*.go | grep -v _test.go` → 39
- `sed -n '196,244p' pkg/dispatch/dispatcher.go` → read
- `sed -n '365,389p' pkg/dispatch/keyed_pool.go` → read
- `grep -n "func.*Stop(\|func.*Close(\|func.*Shutdown(" pkg/worker/*.go natsclient/*.go | grep -v _test.go` → 6
- `git grep -n "func (.*Subscription) Drain\|func (.*Subscription) Closed\|func.*ConsumeContext.*Drain\|func.*ConsumeContext.*Closed" -- '*.go' | grep -v _test.go` → 1 (`Subscription.Drain`; 0 for `ConsumeContext` under this spelling)
- `git grep -rn "^\s*result\s\+chan error" -- '*.go' | grep -v _test.go` → 2
- `git grep -rnE '[wW]ake\s+chan struct\{\}'` → 0 (wrong regex escaping for git grep's default BRE — superseded by
  the corrected search below)
- `git grep -nw 'barrier' -- '*.go' | grep -v processor/rule | grep -v _test.go` → 24
- `git grep -nwE 'fence|Fence' -- '*.go' | grep -v processor/rule | grep -v _test.go` → 14
- `git grep -nE 'ake +chan struct\{\}' -- '*.go'` → 2 (corrected regex)
- `git grep -nE 'chan error$' -- '*.go' | grep -v _test.go` → 7
- `git grep -nE '\) Deadline\(\) \(time\.Time, bool\)' -- '*.go'` → 2
- `git grep -n synctest processor/rule/cron_scheduler_test.go` → 0
- `git grep -rl 'synctest' -- '*.go' | grep -v _test.go` → 0
- `git grep -l 'testing/synctest' -- '*.go'` → 4
- `git log -S runDispatcher --oneline -- processor/rule/cron_scheduler.go` → 1 (`c7ca5d0f`)
- `git show 40cb067d -- processor/rule/cron_scheduler.go` → 0 diff lines (no hunks touch this file)
- `git show c7ca5d0f --stat` → confirms `cron_scheduler.go | 243 +++++--`
- `git show 40cb067d --stat` → confirms 3 files changed, none `cron_scheduler.go`
- `grep -n -i "stop\|scheduler\|ownership\|goroutine" docs/adr/031-time-trigger-primitive.md` → 12
- `grep -n -i "not a workflow engine\|not a process orchestrator\|not a replacement for components\|NOT:" docs/adr/048*.md docs/adr/049*.md pkg/dispatch/doc.go pkg/lifecycle/doc.go` → 3
- `sed -n '180,195p' docs/adr/048-bounded-dispatcher-and-triples-substrate.md` → read
- `sed -n '1,20p' pkg/lifecycle/doc.go` → read
- `sed -n '20,35p' pkg/dispatch/doc.go` → read
- `grep -n -i "not a workflow engine\|not a process orchestrator\|not a replacement for components\|is not\|are not" docs/adr/049*.md` → 5
- `grep -n "NOT a workflow engine" docs/adr/048-bounded-dispatcher-and-triples-substrate.md pkg/dispatch/doc.go` → 2
- `grep -n "does NOT provide\|A process orchestrator\|A replacement for components" pkg/lifecycle/doc.go` → 3
- `grep -n "^## What this ADR is NOT" docs/adr/049*.md` → 1
- `sed -n '618,640p' docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md` → read
- `grep -n -i "stop\|shutdown\|bounded\|Stop-bound" openspec/specs/component-lifecycle/spec.md` → 15
- `sed -n '1,10p;32,36p' openspec/specs/component-lifecycle/spec.md` → read
- `grep -n -i "cron\|scheduler\|dispatch" processor/rule/docs/*.md` → 2 (both "dispatch", unrelated to CronScheduler)
- `ls processor/rule/docs/` → 9
- `git grep -n -i "cron" processor/rule/docs/*.md` → 0
- `gh issue list --state open --search "cron" --json number,title` → 0
- `gh issue list --state open --search "scheduler" --json number,title` → 5
- `gh issue view 1283 --json number,title,body` → read
- `gh issue view 1273 --json number,title` / `gh issue view 1274 --json number,title` → both "fix(rule): preserve
  runtime ownership through shutdown"
- `gh issue view 1283 --json body -q .body` → full body read
- `gh pr list --search "1283" --json number,title,body,isDraft` → 3 (#1404, #1409, #1408)
- `gh pr list --state open --json number,title,isDraft,headRefName` → 8
- `openspec list` → 1 (`rule-bounded-stop-fence`)
- `ls -la openspec/changes/rule-bounded-stop-fence/` → 2 files (`.openspec.yaml`, `README.md`)
- `sed -n '190,200p' processor/rule/readiness_integration_test.go` → located `:197`
- `grep -n "synctest" processor/rule/lifecycle_runtime_test.go` → 4
- `grep -n "func Test" processor/rule/cron_scheduler_test.go` → 47
- `sed -n '259,299p' processor/rule/cron_scheduler_test.go` → read
- `git grep -n "dispatchDone\|dispatchFence\|dispatchWake\|dispatchQueue" processor/rule/cron_scheduler_test.go` → 0
- `go list -m github.com/robfig/cron/v3` → `v3.0.1`
- `go env GOMODCACHE` → `/Users/coby/go/pkg/mod`
- `grep -n "func (c \*Cron) Start\|func (c \*Cron) run\|func FuncJob\|func (f FuncJob) Run" <modcache>/cron.go` → 4
- `sed -n '210,260p' <modcache>/cron.go` → read
- `grep -n "\.Run()\|go func\|WithChain\|Job.Run" <modcache>/cron.go` → 3
- `grep -n "func (c \*Cron) Stop" <modcache>/cron.go` + range read → read
- awk scope-tracked search for `go func()` inside `func (.*) Stop(` bodies, repo-wide, `grep -v _test.go` → 6
- `grep -n "^func" <each of 5 files>` cross-referenced against hit lines → confirmed enclosing `Stop(` signature
  and start line for the 5 non-`cron_scheduler.go` hits
- `grep -n "rp.mu.RLock()\|running := rp.running\|rp.mu.RUnlock()\|if running {\|return rp.submitRuntimeCommand\|return rp.applyConfigUpdate(nil" processor/rule/runtime_config.go` → 5
- `task inventory:verify -- openspec/changes/rule-bounded-stop-fence/inventory-cron-and-lanes.md` → run once against
  the first draft of this file (pre-rewrite); surfaced the grammar violations this revision corrects. Not re-run
  after the rewrite (budget).

### NOT RUN (budget)

- `ConsumeContext` `Drain`/`Closed` under any spelling other than the one regex tried.
- Per-call-site check of whether any of the 9 test references to `CronScheduler.Stop()`'s returned context read
  `.Err()`, `.Deadline()`, or `.Value()` beyond `.Done()`.
- Whether the 5 non-`cron_scheduler.go` `go func()`-inside-`Stop` instances join that goroutine before their own
  `Stop` returns, or let it outlive Stop the way `cron_scheduler.go`'s does.
- Individual file listing for the 34-file `cleanupPending` set beyond the count (the 30-file `lifecycleUsed` list
  above is a subset; 4 additional files not separately enumerated).
- Re-running `task inventory:verify` after this rewrite to confirm 0 malformed/unparsed/drift.
