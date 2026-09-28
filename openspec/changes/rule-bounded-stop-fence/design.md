# Design: a bounded rule Stop never blocks on an orphaned lane barrier (#1283)

base: `3dc4ccbe` (origin/main). Inventories pinned at `7a91400a`; every line below re-read with `sed -n Np` at `756a5689`.
Decision skills: `kv-or-stream`, `orchestration-check`, `new-payload`, `query-pattern` — none triggers (no new path, orchestration, payload, or query).

Authorship: § 0–§ 6 are the `semstreams-architect` handoff of 2026-09-28, materialized verbatim (read-only pass, 31 tool calls, no tests run). § 7 is the coordinating session's read of it, with the amendments the docket carries.

## § 0 Words that bind (verbatim)

- #1283: "Add no supervisor, state machine, durable state, or public API. This is a scoping constraint carried over from #1273." · "Prove each with a deterministic test." · "Prefer whichever preserves the existing ordering guarantee in `cleanup` (fence `:1322` → settle `:1329` → … → join `:1413`)." · "Decide `:644`'s contract: it takes no context, so either it gets one or `ApplyConfigUpdate` must document that it can block indefinitely." · "Independent review, and the CI-faithful integration gate (`scripts/run-integration-tests.sh ./processor/rule/...`), before merge."
- Owner, 2026-09-28: "they find 1283 to be troubling and have cleared us to claim it and work the fix" · "let's note that rules and cron might be pretty rough when we start looking. let's look at the processor top and down and ensure it's playing well in the framework using established patterns and idioms. we have a lot of debt we are clearing from early work where agents were very willing to roll their own".
- `.agents/contracts/semstreams-developer.md:194-223` § Context ownership: no invented roots; "every spawned task joins `Stop`"; detach only terminal finalization under `context.WithTimeout`; exported lifecycle records expose no `CancelFunc`.
- `openspec/specs/component-lifecycle/spec.md:34` — "A successfully running component's Stop MUST be caller-bounded."; `:40-45` Scenario "Stop bound wins"; `openspec/specs/runtime-context-ownership/spec.md:68-70` — abort cleanup "MUST NOT detach or create a second-rejoin contract, and when the bound wins it makes no complete-join or leak-freedom claim"; `component/lifecycle.go:45-46` — "Stop uses its exact caller context to bound the component's terminal admission fence, cancellation, join, and cleanup".
- Owner standing rules: greenfield (no shims, migration note for an exported change); simple over edge-case (doc-sentence row first); a design pass never ratchets complexity up; exported-vs-unexported before has-caller.
- Owner, 2026-09-28, docket 1 (in-session): "i approve as recommended as long as we remember that sister projects downstream can migrate. they might enform our use cases but should not prevent us from breaking things and fixing them right - propper patterns and idoms - when we need to. no dperecated code needed. we make it right and note it, they migrate to it as required" → Q0 (a) inventory review waived; Q-A (2) one unexported owner lane; Q-C (3) `CronScheduler.Stop(ctx context.Context) error` called at cleanup step 2 (§ 7 A1); Q-B (1), Q-D (1), Q-E as designed.

## § 1 Premises (each `path:line` — `text`)

P1 `processor/rule/processor.go:588` — `defer close(rp.coordinatorDone)`; `:615` — `rp.commandMu.Unlock()` (end of `failQueuedRuntimeCommands`); `:651` — `if rp.coordinatorDone != nil {`; `:661` — `rp.commands = append(rp.commands, barrier)`. The close is outside the mutex; an append under the mutex between `:615` and the deferred close is consumed by nothing.
P2 `processor.go:644` — `return <-command.result`; `:625` — `if rp.commandFenced || rp.commandWake == nil {` (never-started is refused here, so only `fenceRuntimeCommands` lacks a nil-done branch).
P3 `processor.go:1494` — `case <-ctx.Done():`; `:1502` — `barrierErr := <-barrier`; `:1504` — `<-coordinatorDone` — two receives after the bound already won.
P4 `processor/rule/cron_scheduler.go:283` — `defer close(s.dispatchDone)`; `:310` — `s.dispatchMu.Unlock()` (end of `failDispatchQueue`); `:344` — `if s.dispatchDone == nil {` (the nil-done self-settle the processor fence lacks); `:358` — `s.dispatchQueue = append(s.dispatchQueue, barrier)`; `:337` — `return <-dispatch.result`. Same window, second copy.
P5 `cron_scheduler.go:472` — `func (s *CronScheduler) Stop() context.Context {`; `:496-507` fence, `nativeStop := s.cron.Stop()`, `go func(){ <-nativeStop.Done(); <-barrier; cancel(); <-dispatchDone; close(stopDone) }()`; `:513` — `return cronStopContext{done: settlement}`; `:70` — `type cronStopContext struct{ done <-chan struct{} }`. Sole production consumer `processor.go:1345` — `cronDone = cronScheduler.Stop().Done()`; waited at `:1394` under `ctx`. PR #1404's hang: `:500` `<-barrier` never returns, so `stopDone` never closes.
P6 `cron_scheduler.go:529` — `if s.ready != nil && !s.ready() {` wired to `processor.go:896` — `Ready:    rp.graphRuleEvaluationReady,` — not the `statusFenced` flag (`readiness.go:151`), so a later cron fence admits ticks that an earlier one refused.
P7 `cleanup` order (`processor.go`): `:1322` fence → `:1329` settle → `:1345` cron fence → drains → `:1392` — `stopErrors = append(stopErrors, hotReloadMgr.Stop())` → `:1394` cron join (ctx-bounded) → `:1411` `cancel()` → `:1413` — `for _, done := range []<-chan struct{}{statusLoopDone, runtimeDone} {`.
P8 `processor/rule/kv_config_integration.go:185` — `func (rcm *ConfigManager) Stop() error {`; `:208` — `<-startDone`; `:229` — `<-done`; the joined goroutine selects `:272` — `case <-ctx.Done():` and its only other blocking path is `:292` — `if err := rcm.reconcileFromKV(ctx); err != nil {` → `:336` — `if err := rcm.processor.ApplyConfigUpdate(changes); err != nil {` → `runtime_config.go:37` submit. Stop cancels its own child (`:212-215`) before `<-done`.
P9 Sibling settle helper `processor.go:1465-1481` (`awaitEntityBorrowSettlement`) cancels and returns `ctx.Err()` at `:1481` with no post-deadline join; the command-fence helper joins because its comment (`:1496-1499`) needs the teardown snapshot final.
P10 Guards: `message_handler.go:88` — `if rp.messageCache != nil {` (no lock); `processor.go:970` — `rp.messageCache = msgCache` (no lock); `:1426` under `rp.mu.RLock`; `:1524` — `rp.messageCache = nil` under `lifecycleMu`. `entity_watcher.go:457` — `wg := rp.runtimeWG`; `:459-461` `if wg != nil { wg.Add(1) }`; `:462` — `go func() {` unconditional. Callers `:168` (Start path) and `:393` (runtime command) both run before the fence settles; no production path reaches it post-clear was found.
P11 Established idiom: `processor/agentic-loop/component.go:741`, `processor/graph-ingest/component.go:1065`, `processor/gated-dag/component.go:323` — `Stop(ctx context.Context) error` selecting on `ctx.Done()` at every wait. Owner-lane shape: exactly two instances repo-wide, both `processor/rule` (inventory-cron-and-lanes § Problem shape i). Hand-rolled Context: one, `cronStopContext` (§ iv).
P12 Tests: `lifecycle_runtime_test.go:26` — `synctest.Test(t, func(t *testing.T) {`; `:34` — `runCtx, startDone, err := processor.beginStartAuthority(startCtx)` (NATS-free accepted-Start seam); `:56` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)` (virtual deadline). `readiness_integration_test.go:197` — wall-clock 5 s abort-lane witness. `cron_scheduler_test.go:250/:259/:187`; zero references to the dispatch-lane fields in that file. `lifecycle_owner_test.go:167` — `barrier := processor.fenceRuntimeCommands()` (direct-fence seam); 16 direct lane-field reads in that file.
P13 #1404 (`git diff origin/main...origin/claude/gh1188-config-bucket-authority-namespace -- processor/rule/` → 7 files, +550 −806): rewrites `kv_config_integration.go` (626 lines), moves `ConfigManager` to the composition root, adds `type HotReloadTarget interface { …; ApplyConfigUpdate(changes map[string]any) error }` and `var _ HotReloadTarget = (*Processor)(nil)`, deletes `Processor.kvConfigManager` and the `cleanup` call at `:1392`; its new `Stop()` (branch file `:158-170`) is `cancel(); <-done`, still contextless; adds `kv_config_stop_fence_test.go` (187 lines) that detects parked Stops from goroutine dumps.

### § 1b Adopter seam (surfaces reached from outside; symbol-precise read-only probe of 21 `/Users/coby/Code/c360/sem*/` checkouts, 2026-09-28)

| Surface | Outside consumer | Must know today | Do nothing | Find out | After this design |
|---|---|---|---|---|---|
| `rule.Processor.Stop(ctx)` via `component.LifecycleComponent` (`service/component_manager.go:996,:1039`) | every binary that composes a rule processor (semboids, semdragon, semmachina, semspec, semteams, semdev import `processor/rule`) | Stop may outlive its ctx (the defect) | a bounded `StopAll` hangs (PR #1404: 20 min) | nowhere | class removed (Q-A) |
| `Processor.ApplyConfigUpdate(changes)` | `/Users/coby/Code/c360/semboids/internal/api/service.go:51` — `ApplyConfigUpdate(changes map[string]any) error` in `type ruleReconfigurer interface`, called `:400`; #1404 `HotReloadTarget` | blocks while a running processor applies; no caller deadline | caller goroutine parks until the runtime ends | nowhere → doc | doc (Q-B); a ctx signature would break semboids and #1404 |
| `CronScheduler.Stop() context.Context` | none found (`CronScheduler|NewCronScheduler|cronStopContext|UpdateWatchBuckets` → 0 sister hits) | returned Context has no Deadline; select `.Done()` under your own bound (`:468-471`) | `<-Stop().Done()` (9 of 10 in-repo test sites) blocks on the orphan | doc | compile error + the bound is the caller's (Q-C) |
| `rule.ConfigManager` | `/Users/coby/Code/c360/semteams/cmd/semteams/main.go:550` constructs it; no `Stop` call | — | — | — | unchanged (Q-D) |

Gap between "must know" and "should know" (nothing): closed for rows 1 and 3; row 2 stays a doc-level fact by choice (see Q-B).

## § 2 Options and recommendation

**Q-A Orphaned-fence class, both lanes.** cronDone (P5) closes iff `<-barrier` and `fire`'s `<-dispatch.result` always return.
- (0) Do nothing: violates spec `:34`; #1404 blocked. Rejected.
- (1) Fix in place per lane. `processor.go`: capture `done := rp.coordinatorDone` beside `wake` at `:587`; delete the defer `:588`; `failQueuedRuntimeCommands` → `closeRuntimeCommandLane(done, err)` that does `close(done)` inside the `commandMu` section before `:615`; `fenceRuntimeCommands` gains the nil-done self-settle mirror of `cron_scheduler.go:344`. `cron_scheduler.go`: same move (`:283` → inside `failDispatchQueue` before `:310`). Cost: 2 files, ≈ +14 −8, exported N, tests 3 (§ 4 T1 T2 T3) plus a 6-line nil-done parity test. cronDone: guaranteed — under close-under-mutex, "done observed open under the mutex" implies the append lands in the exit snapshot, so every barrier/dispatch is drained or failed; `fire` returns; robfig's job completes; `nativeStop` settles; the only remaining wait is an in-flight action ignoring `runCtx` (the action's defect; the processor's wait at `:1394` is ctx-bounded regardless).
- (2) One unexported `ownerLane` in `processor/rule/owner_lane.go` (mutex, queue, cap-1 wake, done, fenced; `run(ctx)`, `submit(run) error`, `fence() <-chan error`, `done()`), closing `done` under its mutex on exit; `Processor` replaces 5 fields (`:110-114`) with `commandLane`, `CronScheduler` replaces 5 (`:58-62`) with `dispatch`; owners wrap two lane sentinels into their existing error texts. Cost: 3 files, ≈ +107 −177 (net ≈ −70), exported N, tests: ONE lane test (T1) covers the class for both owners, T2/T3 stay as wiring proofs; 16 + 2 test-site edits (P12). cronDone: guaranteed, by one code path.
- (3) `pkg/*` primitive: excluded by #1283 ("no … public API"), by the contract's owner-review rule for new `pkg/*` surface, and by the adoption sweep it would owe with zero third instances (P11). Not now.
- Recommendation: **(2)**. The two copies already drifted (P4 has the nil-done branch, P2's twin does not); the owner asked for exactly this look; the diff is net-negative and unexported. (1) is the cheaper row and fully sufficient for #1283 as filed.

**Q-B Contextless submits (`:644`, `:337`).**
- (1) Doc sentence, three sites (`runtime_config.go:14` `ApplyConfigUpdate`, `entity_watcher.go:297` `UpdateWatchBuckets`, the lane `submit`): "Blocks while a running processor executes the update on its runtime; returns the update's result, a refusal once Stop has fenced admission, or the runtime's end error. It carries no caller deadline — a bounded Stop cancels the runtime and releases it." True only with Q-A (the end always delivers). Cost: +6 doc lines, exported N.
- (2) `submit(ctx, run)` with dequeue-on-abandon under the mutex; `ApplyConfigUpdate(ctx, changes)` and `UpdateWatchBuckets(ctx, …)`; `kv_config_integration.go:336` passes its `ctx`. An already-dequeued command cannot be abandoned: it finishes on the coordinator under `runCtx`, its result is dropped (cap-1 channel, no leak), and the caller is told "canceled" for an update that then applies. Cost: ≈ +25, exported Y — breaks semboids `service.go:51/:400` and #1404's `HotReloadTarget`; 30 in-repo references.
- "Satisfies no interface" (inventory) holds in-repo only; semboids and #1404 both pin the current signature. Recommendation: **(1)**.

**Q-C `CronScheduler.Stop() context.Context`.**
- (1) Keep; +1 doc line. Leaves the one hand-rolled Context and the one goroutine-outlives-Stop (P11).
- (2) `Stop() <-chan struct{}`: deletes `:70-82`; 1 production + 10 test edits; exported Y; keeps the settlement goroutine (needed for cleanup's fence-early/join-late split).
- (3) `Stop(ctx context.Context) error` (P11 idiom): nil-ctx rejected; `:479` and every wait become `select … case <-ctx.Done()`; `stopDone`/goroutine/`cronStopContext` deleted; repeated completed Stop returns nil; concurrent Stop errors like `processor.go:1292`. In `cleanup`, delete `:1343-1345` and `:1394-1403`, add `if cronScheduler != nil { stopErrors = append(stopErrors, cronScheduler.Stop(ctx)) }` where `:1394` was — join order (cron before `cancel()` `:1411`) preserved; the cron *fence* moves from step 2 to step 5, so ticks during steps 3–4 are admitted (P6) and joined under `ctx` there. Cost: `cron_scheduler.go` ≈ +18 −30, `processor.go` +3 −11, tests 10 sites (2 rewritten with synctest), exported Y, 0 adopters found → one migration row.
- Recommendation: **(3)**. Rule call-out: #1283's "preserve the existing ordering guarantee" names the command-lane order (fence→settle→join), which (3) does not touch; read as pinning the cron step it would force (1)/(2), keeping a Stop the caller cannot bound. The developer contract's "every spawned task joins Stop" is what makes (2) insufficient. I am not sure the lost overlap (cron settlement no longer runs concurrently with consumer drains) is measurable; it is bounded by `ctx` either way.

**Q-D `ConfigManager.Stop()` inside `cleanup(ctx)` (`:1392`).**
- (1) Leave; one comment at `:1392`: bounded in practice — after `:1322` the KV goroutine's only lane entry is refused at `:625`, its other waits are ctx-aware (P8), and Q-A makes any admitted command deliver. Cost: +1, exported N.
- (2) `Stop(ctx) error`: ≈ +8 −2, 1 production + 6 test sites, exported Y (semteams constructs, never stops). Conflicts head-on with #1404's rewrite (P13).
- Recommendation: **(1)** on main. #1404 hunk for the owner to sequence: it deletes `processor.go` field `:243-248`, Start block `:1019-1028`, snapshot `:1335`, call `:1391-1393`, nil `:1520`, and keeps a contextless `cancel(); <-done` Stop at the composition root — that root's boundedness is #1404's question, not this change's.

**Q-E Residuals.**
| Item | Disposition | Reason |
|---|---|---|
| `messageCache` three guards (P10) | fix here | in #1283 scope; one guard = `rp.mu`: RLock around `message_handler.go:88-91`, Lock around `:970`, move the nil write from `:1524` into Stop's `rp.mu` block `:1308-1312`. ≈ +6 −1. (Deleting the nil write instead was not chosen: post-Stop `Set` on a closed `pkg/cache` was not verified safe.) |
| `entity_watcher.go:457` nil `wg` | fix here | refuse: `if wg == nil { close(done) if non-nil; _ = watcher.Stop(); logger.Warn(...); return }` ≈ +7. Add-after-Wait (non-nil `wg`, `runtimeDone` closed) is not closable through `WaitGroup`; recorded, not fixed. |
| `:1504` post-deadline `<-coordinatorDone` | leave, document | join-after-cancel whose only blocker is a command ignoring its ctx; P9 shows the sibling deliberately differs. No issue. |
| `StandardLifecycleTests` absent for `rule.Processor` | file | test-only, NATS-backed (~30 lines per `processor/graph-index/lifecycle_integration_test.go:78`), independent of the fix; `CronScheduler` is not a `LifecycleComponent` (no `Initialize`) and is not forced into it. |
| `lifecycleUsed`/`cleanupPending` copy-paste (30 / 34 files; 0 in `pkg/lifecycle`; no ADR/spec sanctions it) | file | framework idiom decision, never fixed here. |
| Abort-lane Stop returns `context.Canceled` from a barrier failed by the coordinator's own exit (first arm `:1492` has no normalization; `:1506-1508` does) | leave | pre-existing, accurate per spec `:44`; noted for the reviewer. |

**Q-F Test plan** — § 4.

## § 3 Docket (cheaper row first; one recommendation each)

| Q | Cheaper | Recommended | One line |
|---|---|---|---|
| Q-A | (1) in place, ≈ 22 lines, 4 tests | (2) unexported `ownerLane`, net ≈ −70, 1 class test | Take (2): the copies already drifted; (1) if unblocking #1404 today outweighs clearing the duplicate. |
| Q-C | (1) keep + doc | (3) `Stop(ctx) error`, exported break, 0 adopters | Take (3): the sibling idiom; deletes the only hand-rolled Context; migration row "no adopter found". |

Q-B, Q-D, Q-E: cheaper row == recommended; no exported change. Q-C is the only exported-surface change → `docs/operations/migration-rule-stop-bounded.md` (rows: `CronScheduler.Stop` signature — 0 adopters; `ApplyConfigUpdate`, `ConfigManager` unchanged).

## § 4 Acceptance

Invariants (spec home: ADDED requirement in `openspec/specs/rule-engine/spec.md`, "Runtime lanes settle every admitted barrier"): I1 every command or barrier appended to a lane receives exactly one result (its run's, or the lane's end error); I2 an appender that observes the lane's done closed under the lane mutex never appends; I3 a barrier settles after every command admitted before it, or at the lane's end. PBT decision (`docs/contributing/01-testing.md` § When to use PBT): named examples — the only interleaving that matters is forced deterministically below; a random-interleaving property cannot force it and would reconstruct the implementation.

Seam for (a): a queued command whose cap-1 `result` is **pre-filled** parks the lane's exit inside the drain→close window at `command.result <- err` — no sleep, no production hook.

| Test (all `synctest.Test`, `-race`) | Forces | Mutation that fails it (wiring) |
|---|---|---|
| T1 `TestRuleRuntimeLaneFenceAfterLastDrainSettles` (on `ownerLane`; or per lane under Q-A(1)) | `beginStartAuthority` seam (P12); append pre-filled command under the lane mutex; cancel; `synctest.Wait()`; `fence()`; drain the pre-filled channel; `synctest.Wait()`; `select { case <-barrier: default: t.Fatal }`; done closed | move `close(done)` from inside the mutex section to after `Unlock()` → barrier orphaned |
| T1b `TestRuleRuntimeFenceOnUnstartedLaneSelfSettles` | `fenceRuntimeCommands()` with nil done (`lifecycle_owner_test.go:167` idiom) | delete the nil-done branch → receive blocks; synctest reports deadlock |
| T2 `TestRuleStopDeadlineArmCancelsAndJoinsCoordinator` | submit a command whose run blocks on `<-ctx.Done()`; `synctest.Wait()`; `Stop(WithTimeout(ctx, time.Second))` → deadline arm `:1494` at zero wall-clock; assert `DeadlineExceeded`, coordinator done, submitter got `Canceled` | delete `cancel()` at `:1500` → deadlock |
| T3 `TestCronScheduler_StopSettlesWhenBarrierRacesDispatcherExit` | `newUnstartedSchedulerForTest`; `Start(ctx)`; pre-filled dispatch under `dispatchMu`; cancel; `synctest.Wait()`; `Stop` (signature per Q-C); release; assert settled | same move in `failDispatchQueue` → never settles |
| T4 `TestRuleMessageCacheOneGuard` (`-race`) | `Stop` racing `evaluateRulesForMessage` | remove the RLock at `:88` → race report |
| T5 `TestRuleManagedWatcherSpawnRefusedAfterRuntimeEnd` | `clearLifecycleHandles()` then `startManagedEntityWatcher` with a fake watcher; assert `done` closed, Warn logged, bubble exits | delete the refuse branch → goroutine blocks; bubble does not exit |
| T6 (Q-C(3)) rewrite `cron_scheduler_test.go:259` and `:187` with `Stop(ctx)` in a goroutine + `synctest.Wait()` | settlement waits for in-flight action; Register serialization | — (rewrites, not new coverage) |

Existing coverage: `readiness_integration_test.go:197` remains the wall-clock abort-lane witness (it wedged in #1283's reproduction; it is not deterministic for the window); `lifecycle_runtime_test.go:25` enters the deadline lane at `:1413`, never at `:1494`; `:92` covers failed-Start rollback; `cron_scheduler_test.go:250/:259/:187` never touch the dispatch lane. Gates: `go test -race ./processor/rule -run 'TestRuleRuntime|TestRuleStop|TestRuleMessageCache|TestRuleManaged|TestCronScheduler' -count=20`; `task check:push`; `scripts/run-integration-tests.sh ./processor/rule/...` at the final revision; `task inventory:verify` on both inventories after each commit; `task spec:properties`. Mutation evidence via `cp` backup + checksum, recorded in the PR body.

## § 5 Residuals to file

1. "test(rule): run `rule.Processor` through `component.StandardLifecycleTests`" — body: only gateway/http, input/udp, processor/graph-index use the portable suite (`component/lifecycle_test_suite.go:21`); rule needs a NATS-backed factory. Milestone: v1.0.0-beta.165.
2. "lifecycle: `lifecycleUsed`/`cleanupPending` is a 30-file copy-paste with no sanctioned home" — body: 30 non-test files carry the idiom, 34 carry `cleanupPending`, `pkg/lifecycle` uses neither, no ADR/spec/contract names it; decide one owner-lifecycle guard (or record that the idiom is the decision). Milestone: v1.0.0-rc.1.

## § 6 tasks.md rows (replace § 3 Implementation)

- [ ] 3.1 `processor/rule/owner_lane.go` (Q-A(2)) — lane with close-under-mutex; `Processor` and `CronScheduler` adopt it; `lifecycle_owner_test.go` / `lifecycle_runtime_test.go` field reads updated (if the owner rules Q-A(1): the two in-place moves + fence nil-done parity instead)
- [ ] 3.2 `CronScheduler.Stop(ctx context.Context) error` (Q-C(3)); `cleanup` calls it where `:1394` was; delete `cronStopContext`; 10 test sites
- [ ] 3.3 Doc sentences: `ApplyConfigUpdate`, `UpdateWatchBuckets`, lane `submit` (Q-B(1)); one comment at the `hotReloadMgr.Stop()` call (Q-D(1))
- [ ] 3.4 `messageCache` under `rp.mu` at all four sites; `startManagedEntityWatcher` refuses on nil `wg` (Q-E)
- [ ] 3.5 Tests T1–T6 with mutation rows in the PR body (`cp` + checksum)
- [ ] 3.6 Spec delta: `openspec/specs/rule-engine/spec.md` ADDED "Runtime lanes settle every admitted barrier" (I1–I3, three scenarios); `docs/operations/migration-rule-stop-bounded.md`
- [ ] 3.7 File § 5 residuals (two issues, milestones as named)
- [ ] 3.8 `semstreams-reviewer` round; `task check:push`; `scripts/run-integration-tests.sh ./processor/rule/...` at the final code revision; `task inventory:verify` both files; archive as the last content commit

## § 7 Coordinator read (2026-09-28)

Concur with the recommendations on Q-A (2), Q-B (1), Q-D (1), Q-E, and the § 4 test plan. Three amendments, carried into the docket and into § 6 as implemented:

- **A0 — gate not run.** `.agents/contracts/semstreams-architect.md:25` requires an independent inventory review to `INVENTORY PASS` before design; none was run, so § 2–§ 6 are a pre-review draft. Not self-certified: docket Q0 asks the owner to waive it (the pins are mechanically verified, 35/35 and 154/154, and the implementation reviewer reads both inventories in its round) or to run it as its own reviewer spawn before code.
- **A1 — Q-C (3) is called at cleanup step 2, not step 5.** Today step 2 is `cronDone = cronScheduler.Stop().Done()` (`processor.go:1343-1345`): the cron admission fence and the native stop happen there, and `fire` on a fenced lane is refused (`cron_scheduler.go:594-599`). Placing `cronScheduler.Stop(ctx)` at step 2 keeps that admission point exactly where it is, so the P6 concern (ticks admitted during steps 3–4) does not arise and #1283's ordering sentence is honored on its plain reading, not argued around. The join still precedes `cancel()` at `:1411`. The only cost is that cron settlement no longer overlaps the consumer drains; it is bounded by `ctx` either way. § 6 row 3.2 reads "where `:1343-1345` is" accordingly.
- **A2 — the spec delta is phrased as the adopter-visible contract, not as lane internals.** `openspec/specs/rule-engine/spec.md` has no runtime-update section today. The ADDED requirement states behavior an adopter can observe: a runtime configuration update (`ApplyConfigUpdate`, `UpdateWatchBuckets`) blocks until the running processor applies it or its runtime ends, and never blocks past a bounded Stop; the cron scheduler's Stop is bounded by its caller context. I1–I3 stay in this design and in the test file's doc comment as the mechanism the tests force; they do not enter the spec.

Docket as posted on #1283 (2026-09-28): Q0 inventory-review gate — (a) waive, recommended; (b) run. Q-A — (a) in place; (b) one unexported lane, recommended. Q-C — (a) keep + doc; (b) `Stop(ctx) error` at step 2, recommended. Q-B, Q-D, Q-E: cheaper row = recommended; no owner question.
