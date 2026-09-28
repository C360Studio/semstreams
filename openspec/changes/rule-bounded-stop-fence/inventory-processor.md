# Inventory: processor/rule Processor Stop path (rule-bounded-stop-fence, #1283)
base: 7a91400ad0d97d4fd2796c57e5d6ca79d17ff8eb

Scope note: this file covers `processor/rule/processor.go`, `kv_config_integration.go`, `entity_watcher.go`,
`message_handler.go`, `runtime_config.go` only. `CronScheduler` and the repo-wide "same shape elsewhere" question
are out of scope here (parallel explorer).

## Claimed gap

- `processor/rule/processor.go:588` — `defer close(rp.coordinatorDone)`
- `processor/rule/processor.go:615` — `rp.commandMu.Unlock()`
  (ends `failQueuedRuntimeCommands`, runs before the deferred close above returns control)
- `processor/rule/processor.go:651` — `if rp.coordinatorDone != nil {`
  (fenceRuntimeCommands self-settle guard: only fires when coordinatorDone is ALREADY closed)
- `processor/rule/processor.go:658` — `default:`
  (non-blocking check falls through when coordinatorDone is open)
- `processor/rule/processor.go:661` — `rp.commands = append(rp.commands, barrier)`
  (the orphan append: reached unconditionally when `rp.coordinatorDone == nil`, no self-settle branch exists for that case, unlike `CronScheduler.fenceDispatch`)
- `processor/rule/processor.go:644` — `return <-command.result`
  (submitRuntimeCommand; no ctx parameter, no select, unbounded)
- `processor/rule/processor.go:1492` — `case barrierErr := <-barrier:`
- `processor/rule/processor.go:1494` — `case <-ctx.Done():`
  the deadline arm; the two receives below follow it with no further select against ctx
- `processor/rule/processor.go:1502` — `barrierErr := <-barrier`
- `processor/rule/processor.go:1504` — `<-coordinatorDone`
  (settleRuntimeCommandFence deadline-arm select)
`processor/rule/processor.go:1504` (`sed` line for `case <-ctx.Done():`) — the ctx.Done() branch that follows still performs two further unbounded receives before returning:
  `processor/rule/processor.go:1502` (second occurrence, inside the `ctx.Done()` case) — `barrierErr := <-barrier` — unbounded, no further select against ctx
  - `processor/rule/processor.go:1504` — `<-coordinatorDone`
    unbounded, no further select against ctx
- `processor/rule/kv_config_integration.go:208` — `<-startDone`
  (ConfigManager.Stop; function takes no `ctx` argument at all — unbounded by any caller deadline)
- `processor/rule/kv_config_integration.go:229` — `<-done`
  (ConfigManager.Stop; same — unbounded)
- `processor/rule/processor.go:1392` — `stopErrors = append(stopErrors, hotReloadMgr.Stop())`
  (the call site inside `cleanup(ctx)`; `ConfigManager.Stop()` receives no part of `ctx`, so the two unbounded receives above are entirely outside the Stop caller's deadline)
- `processor/rule/message_handler.go:88` — `if rp.messageCache != nil {`
  (read with no mutex held, contrasts with the guarded read at processor.go:1426 and the guarded nil at processor.go:1524)
- `processor/rule/processor.go:970` — `rp.messageCache = msgCache`
  (write in `Start`, no mutex held at this line; `rp.mu.Lock()` is only taken later at processor.go:1035)
- `processor/rule/entity_watcher.go:457` — `wg := rp.runtimeWG`
  (read under `lifecycleMu`; when nil, the `go func(){...}()` below is still spawned unconditionally — only the `wg.Add`/`wg.Done` bracketing is skipped)

## Spellings of the fact

Four guards found in `processor/rule/processor.go`: `lifecycleMu sync.Mutex` (:101), `commandMu sync.Mutex` (:110),
`mu sync.RWMutex` (:123), `entityDispatchGate sync.RWMutex` (:170). `ConfigManager` (kv_config_integration.go) has
its own separate `lifecycleMu sync.Mutex` (:52) guarding its own state, unrelated to `Processor`'s.

The eleven fields `clearLifecycleHandles` (processor.go:1513-1524) nils, plus five more named in the brief. Guard
column is the lock held at that exact line; "none" means the line takes no lock of its own (it may still run inside
a caller's critical section already noted).

### cancel (chan/func field, decl processor.go:107)
write `processor.go:1076` — `rp.cancel = cancel` — guard: lifecycleMu (inside `beginStartAuthority`)
read `processor.go:1325` — `cancel := rp.cancel` — guard: lifecycleMu (inside `cleanup`)
write(nil) `processor.go:1514` — `rp.cancel = nil` — guard: lifecycleMu (`clearLifecycleHandles`)
read (test) `lifecycle_owner_test.go:119,195,399,457` — direct field access, no guard shown at call site (test-internal)

### runtimeDone (chan struct{}, decl :108)
write `processor.go:1078` — `rp.runtimeDone = make(chan struct{})` — guard: lifecycleMu
read `processor.go:944` — `}(runtimeWG, rp.runtimeDone)` (goroutine closes it via `wg.Wait(); close(done)`) — guard: none (read once at Start, pre-concurrency)
read `processor.go:1338` — `runtimeDone := rp.runtimeDone` — guard: lifecycleMu (`cleanup` snapshot block, :1330-1339)
write(nil) `processor.go:1515` — guard: lifecycleMu
read (test) `lifecycle_owner_test.go:118,126,198,219,398,411,456,469`; `lifecycle_runtime_test.go:38,115` — direct, test-internal

### runtimeWG (*sync.WaitGroup, decl :109)
write `processor.go:1077` — `rp.runtimeWG = &sync.WaitGroup{}` — guard: lifecycleMu
read `processor.go:930` — `runtimeWG := rp.runtimeWG` — guard: none (Start, pre-concurrency, single goroutine at this point)
read `entity_watcher.go:457` — `wg := rp.runtimeWG` — guard: lifecycleMu
write(nil) `processor.go:1516` — guard: lifecycleMu

### commandFenced (bool, decl :111)
write `processor.go:625` — `if rp.commandFenced ...` (read, not write) — guard: commandMu (`submitRuntimeCommand`)
write `processor.go:650` — `rp.commandFenced = true` — guard: commandMu (`fenceRuntimeCommands`)
write `processor.go:1081` — `rp.commandFenced = false` — guard: lifecycleMu (`beginStartAuthority`)
read (test) `lifecycle_owner_test.go:240,426` — direct, test-internal

### commands ([]ruleRuntimeCommand, decl :112)
read/write `processor.go:597,601,602(x2)` — guard: commandMu (`runRuntimeCoordinator` drain loop)
read/write `processor.go:613,614` — guard: commandMu (`failQueuedRuntimeCommands`)
write `processor.go:637(x2)` — guard: commandMu (`submitRuntimeCommand` append)
write `processor.go:661(x2)` — guard: commandMu (`fenceRuntimeCommands` append — the orphan site)
read (test) `lifecycle_owner_test.go:241` — direct, test-internal

### commandWake (chan struct{}, decl :113)
write `processor.go:1079` — guard: lifecycleMu (`beginStartAuthority`)
read `processor.go:587` — `wake := rp.commandWake` — guard: none (`runRuntimeCoordinator`, captured once at goroutine start, comment explicitly says this capture happens "before acknowledging any shutdown fence")
read `processor.go:625,638,662` — guard: commandMu (submit/fence paths)
write(nil) `processor.go:1517` — guard: lifecycleMu
read (test) `lifecycle_owner_test.go:116,156,196,396,454` — direct, test-internal

### coordinatorDone (chan struct{}, decl :114)
write `processor.go:1080` — guard: lifecycleMu (`beginStartAuthority`)
read `processor.go:588` — `defer close(rp.coordinatorDone)` — guard: none (captured at defer-registration in the coordinator goroutine)
read `processor.go:629,631,651,653` — guard: commandMu (submit/fence nil-checks)
read `processor.go:1326` — `coordinatorDone := rp.coordinatorDone` — guard: lifecycleMu (`cleanup` snapshot)
write(nil) `processor.go:1518` — guard: lifecycleMu
read (test) `lifecycle_owner_test.go:117,156,197,397,455,488`; `lifecycle_runtime_test.go:116` — direct, test-internal

### statusLoopDone (chan struct{}, decl :145)
write `processor.go:1016` — guard: none directly on the line (inside the `else` branch of the `createStatusBucket` check in `Start`, pre-concurrency for this field)
read `processor.go:1017` — `go rp.statusMetricsLoop(runCtx, rp.statusLoopDone)` — guard: none
read `processor.go:1339` — guard: lifecycleMu (`cleanup` snapshot)
write(nil) `processor.go:1523` — guard: lifecycleMu
read (test) `lifecycle_runtime_test.go:44,62` — direct, test-internal

### statusFenced (atomic.Bool, decl :148)
write `processor.go:1038` — `rp.statusFenced.Store(false)` — guard: none needed (atomic), inside `rp.mu.Lock()` block per surrounding code (:1035-1042 in Start)
write `processor.go:1321` — `rp.statusFenced.Store(true)` — guard: none needed (atomic), first line of `cleanup`
read `readiness.go:151` — `if rp.statusFenced.Load() {` — guard: none needed (atomic)
read `readiness.go:162` — same, second check after the readiness compute (comment: "the check that actually closes the window")
read (test) `readiness_test.go:339` — direct

### isSubscribed (bool, decl :158)
write `processor.go:328` — `isSubscribed: false,` — struct literal in `NewProcessor`
write `processor.go:1037` — `rp.isSubscribed = true` — guard: rp.mu (Lock, Start :1035-1042 block)
write `processor.go:1311` — `rp.isSubscribed = false` — guard: rp.mu (Lock, end of `Stop`)
read `runtime_config.go:340` — `"is_running": rp.isSubscribed,` — guard: rp.mu.RLock (`GetRuntimeConfig`, :323-324)
read (test) `runtime_config_test.go:260` — direct

### subscriptions ([]*natsclient.Subscription, decl :161)
write `processor.go:1157` — `rp.subscriptions = append(rp.subscriptions, sub)` — guard: none shown at this exact line (inside `setupSubscriptions`, called from `Start` before `rp.mu.Lock()` is taken for this field — no lock wraps this append)
read `processor.go:1337` — `subscriptions := append([]*natsclient.Subscription(nil), rp.subscriptions...)` — guard: lifecycleMu (`cleanup` snapshot)
write(nil) `processor.go:1521` — guard: lifecycleMu

### cronScheduler (*CronScheduler, decl :214)
write `processor.go:911` — guard: none shown directly (inside `initializeCronScheduler`, called from `Start` pre-concurrency)
read `processor.go:1008,1009` — `if rp.cronScheduler != nil { if err := rp.cronScheduler.Start(runCtx)` — guard: none (Start, sequential)
read `processor.go:1334` — guard: lifecycleMu (`cleanup` snapshot)
write(nil) `processor.go:1522` — guard: lifecycleMu
read `runtime_config.go:193,199,200,259,260,284,285` — guard: rp.mu (comment at :275-276 "Caller holds rp.mu.Lock")

### kvConfigManager (*ConfigManager, decl :248)
write `processor.go:1026` — `rp.kvConfigManager = rcm` — guard: none shown directly (Start, sequential, inside `if rp.natsClient != nil` block)
read `processor.go:1335` — `hotReloadMgr := rp.kvConfigManager` — guard: lifecycleMu (`cleanup` snapshot)
write(nil) `processor.go:1520` — guard: lifecycleMu

### streamConsumers ([]ruleStreamConsumer, decl :249)
write `processor.go:1224` — `rp.streamConsumers = append(rp.streamConsumers, ruleStreamConsumer{handle: handle})` — guard: lifecycleMu (explicit Lock/Unlock at :1223/1225)
read `processor.go:1336` — guard: lifecycleMu (`cleanup` snapshot)
write `processor.go:1434` — `rp.streamConsumers = consumers` — guard: lifecycleMu (end of `cleanup`, writes back post-drain state)
write(nil) `processor.go:1519` — guard: lifecycleMu
read (test) `lifecycle_owner_test.go:115` — direct

### messageCache (cache.Cache[message.Message], decl :69)
write `processor.go:306` — `messageCache: msgCache,` — struct literal in `NewProcessor` — guard: none (pre-Start)
write `processor.go:970` — `rp.messageCache = msgCache` — guard: none (inside `Start`, before `rp.mu.Lock()` at :1035)
read `message_handler.go:88,90` — guard: none (`evaluateRulesForMessage`)
read `processor.go:1426` — `messageCache := rp.messageCache` — guard: rp.mu.RLock (`cleanup`, :1425/1427)
write(nil) `processor.go:1524` — guard: lifecycleMu (`clearLifecycleHandles`)

## Adjacent claims

- `openspec/specs/component-lifecycle/spec.md:9` — `MUST own continuing component work and MUST NOT be retained on a production struct. `Stop(ctx)` MUST reject nil before`
  quoted as: "MUST own continuing component work and MUST NOT be retained on a production struct. `Stop(ctx)` MUST reject nil before"
- `openspec/specs/component-lifecycle/spec.md:32` — `### Requirement: Running Stop has no shared-generation contract`
  quoted as: "### Requirement: Running Stop has no shared-generation contract"
- `openspec/specs/component-lifecycle/spec.md:34` — `A successfully running component's Stop MUST be caller-bounded. Completed repeated Stop with a valid context MUST`
  quoted as: "A successfully running component's Stop MUST be caller-bounded. Completed repeated Stop with a valid context MUST"
- `openspec/specs/component-lifecycle/spec.md:40` — `#### Scenario: Stop bound wins`
  quoted as: "#### Scenario: Stop bound wins"
- `openspec/specs/component-lifecycle/spec.md:19` — `- **AND** a separately bounded Stop makes synchronous best-effort terminal progress under its exact caller authority`
  quoted as: "**AND** a separately bounded Stop makes synchronous best-effort terminal progress under its exact caller authority"
- `openspec/specs/runtime-context-ownership/spec.md:8` — ``service.Service.Stop`, `component.LifecycleComponent.Stop`, and `service.Manager.StopAll` MUST accept`
  quoted as: "`service.Service.Stop`, `component.LifecycleComponent.Stop`, and `service.Manager.StopAll` MUST accept"
- `openspec/specs/runtime-context-ownership/spec.md:61` — `### Requirement: Lifecycle composition distinguishes controlled shutdown from abort cancellation`
  quoted as: "### Requirement: Lifecycle composition distinguishes controlled shutdown from abort cancellation"
- `.agents/contracts/semstreams-developer.md:194` — `### Context ownership`
  quoted as: "### Context ownership"
- `.agents/contracts/semstreams-developer.md:213` — `parent contract may use `context.WithTimeout(context.Background(), budget)`. Complete synchronously or join all`
  quoted as: "parent contract may use `context.WithTimeout(context.Background(), budget)`. Complete synchronously or join all"
- `component/lifecycle.go:45` — `// that owns continuing work. Stop uses its exact caller context to bound the`
  quoted as: "// that owns continuing work. Stop uses its exact caller context to bound the" (part of the :43-62 `LifecycleComponent` doc comment)
- `openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/base-service-owner-slice.md:185` — `9. Add no generic lifecycle abstraction, operation coordinator, native-handle protocol, exported lifecycle surface,`
  quoted as: "9. Add no generic lifecycle abstraction, operation coordinator, native-handle protocol, exported lifecycle surface," (generic prose, not specific to rule's coordinator)
- `openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/recovery-ledger.md:2638` — `No generic generation, operation election, retained result, rejoin channel, concurrent-Stop coordinator, detached`
  quoted as: "No generic generation, operation election, retained result, rejoin channel, concurrent-Stop coordinator, detached" (generic prose, not specific)
- No `design.md` in `2026-08-21-restore-go-lifecycle-ownership`, `2026-08-21-simplify-one-shot-lifecycle-ownership`, or `2026-08-24-gh1062-rule-lifecycle-cleanup` mentions `coordinator`, `command lane`, `commandWake`, `commandMu`, `runRuntimeCoordinator`, `fenceRuntimeCommands`, or `submitRuntimeCommand` (zero hits, see Searches) — no recorded design-level decision for this shape in those three archives.
- #1283 — "fix(rule): a bounded Stop can block forever on an orphaned fence barrier, violating component-lifecycle spec:34" (OPEN, this issue)
- #1273 — "fix(rule): preserve runtime ownership through shutdown" (CLOSED)
- #1274 — "fix(rule): preserve runtime ownership through shutdown" (MERGED, commit `40cb067d`)
- #1012 — "lifecycle: retire invented production context roots by bounded owner" (OPEN)
- #1064 — "test lifecycle: audit and guard unbounded Stop cleanup roots" (OPEN)
- #1041 — "rule: an entity-watcher start failure permanently disables message-path rule evaluation too" (OPEN)
- #1042 — "rule: per-rule entity.watch_buckets is parsed and validated but never drives a watcher" (OPEN)
- #1170 — "rule: Start warn-swallows a state-tracker failure that leaves NO action executor — the processor boots healthy and dispatches nothing" (OPEN)
- Commit `40cb067d` message — "Refs #1283 for the separate unchanged fence and ownership residuals; this merge does not claim to fix them." (direct provenance: #1274 explicitly did not fix the orphan-fence class)
- #1409 — "fix(rule): a bounded Stop must not block on an orphaned fence barrier" (OPEN, draft, `Closes #1283`, branch `claude/gh1283-rule-stop-bounded-fence` — this is the claim PR for the present work; no other open draft PR references #1283 or these functions)

## Consumers

- `processor/rule/entity_watcher.go:314` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
  (inside `UpdateWatchBuckets`)
- `processor/rule/runtime_config.go:37` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
  (inside `ApplyConfigUpdate`)
- `processor/rule/lifecycle_owner_test.go:159` — `if err := processor.submitRuntimeCommand(func(commandCtx context.Context) error {`
  (test)
- `processor/rule/lifecycle_owner_test.go:171` — `if err := processor.submitRuntimeCommand(func(context.Context) error { return nil }); err == nil {`
  (test)
- `processor/rule/lifecycle_owner_test.go:167` — `barrier := processor.fenceRuntimeCommands()`
  (test, direct call to the fence)
- `processor/rule/processor.go:1322` — `barrier := rp.fenceRuntimeCommands()`
  (production caller, inside `cleanup`)
`ApplyConfigUpdate` (`runtime_config.go:14`) — `gopls implementation` on its declaration returns zero results: it does not satisfy any interface method in the workspace. Production caller: `processor/rule/kv_config_integration.go:336` — `if err := rcm.processor.ApplyConfigUpdate(changes); err != nil {` inside `(rcm *ConfigManager) reconcileFromKV`, itself invoked from `processKVUpdates` (the KV-watcher goroutine started at `kv_config_integration.go:173` `go rcm.processKVUpdates(runCtx, watcher, done)`). No caller in `config/` or `internal/boot`.
`Processor.Stop` production caller: `service/component_manager.go:996` and `:1039` — `lifecycle.Stop(ctx)` through the generic `component.LifecycleComponent` interface (no rule-specific call site in `service/` or `internal/boot`).
`Processor.Stop` test callers passing `context.Background()`: 33 sites across `processor/rule/*_test.go` (see Searches for the full `git grep` list); representative: `lifecycle_owner_test.go:519`, `lifecycle_runtime_test.go:139`, `rule_lifecycle_test.go:23,26`, `kv_hot_reload_integration_test.go:111,143,184,235,273,304`, `rule_integration_test.go:147,247,447,541,681,831`. (`actions_run_scope_integration_test.go:130` and `triple_mutator_revision_integration_test.go:74` call `ingest.Stop(context.Background())` — `ingest` there is a different processor type, not confirmed as `rule.Processor` without further reading; listed for completeness, not counted as a rule.Processor call.)
`component.StandardLifecycleTests` (`component/lifecycle_test_suite.go:21`) is used by `gateway/http/http_lifecycle_test.go:57`, `input/udp/udp_lifecycle_test.go:88`, `processor/graph-index/lifecycle_integration_test.go:78`. **No file under `processor/rule/` calls `component.StandardLifecycleTests` or references `component.LifecycleFactory`** — `rule.Processor` is not run through the portable lifecycle test suite (see Searches, zero hits in `processor/rule/`).

## Problem shape

(none — see Searches; the brief reserves this category to the parallel CronScheduler/repo-wide explorer's surface. The one same-shape instance found on this surface, `CronScheduler.fenceDispatch`'s `dispatchDone == nil` self-settle branch at `processor/rule/cron_scheduler.go:344`, sits outside this file's named surface and is left for that pass to record under its own inventory.)

## Searches

- `gopls workspace_symbol -matcher=fuzzy Processor` — not run (surface already fully named by brief; struct located via `grep -n "type Processor struct"` instead)
- `grep -n "^## " openspec/project.md` → 4 (Purpose, Product Boundary, How we spec, Standing Technical Conventions)
- `wc -l processor/rule/{processor,kv_config_integration,entity_watcher,message_handler,runtime_config}.go` → 5 files, 4297 total lines
- `grep -n "func settleRuntimeCommandFence\|func (rp \*Processor) clearLifecycleHandles\|barrierErr := <-barrier\|<-coordinatorDone\|func (rp \*Processor) Stop\|func (rp \*Processor) cleanup\|func (rp \*Processor) runRuntimeCoordinator\|func (rp \*Processor) failQueuedRuntimeCommands\|func (rp \*Processor) submitRuntimeCommand\|func (rp \*Processor) fenceRuntimeCommands\|close(rp.coordinatorDone)\|return <-command.result" processor/rule/processor.go` → 12
- `grep -n "select {\|case <-\|case <-ctx.Done()\|case barrierErr\|cronDone = \|watcher.Stop()\|\.Drain(\|closeEntityEvaluationQueue\|messageCache.Close\|cancel()" processor/rule/processor.go` (filtered 1266-1440) → 20
- `grep -n "^func\|struct {" processor/rule/kv_config_integration.go` → 15
- `grep -n "^func\|struct {" processor/rule/entity_watcher.go` → 47
- `grep -n "func (rcm \*ConfigManager) Stop\|<-startDone\|<-done\b\|func (rcm \*ConfigManager) Start" processor/rule/kv_config_integration.go` → 4
- `grep -n "hotReloadMgr.Stop()" processor/rule/processor.go` → 1
- `grep -n "submitRuntimeCommand" processor/rule/runtime_config.go processor/rule/entity_watcher.go processor/rule/processor.go processor/rule/message_handler.go processor/rule/kv_config_integration.go` → 3
- `git grep -n "submitRuntimeCommand" -- '*.go'` → 5
- `git grep -n "fenceRuntimeCommands" -- '*.go'` → 3
- `git grep -n "ApplyConfigUpdate" -- '*.go'` → 30
- `git grep -n "ApplyConfigUpdate" -- 'config/*' 'internal/boot/*'` → 0
- `git grep -n "ApplyConfigUpdate(changes map\[string\]any) error" -- '*.go'` → 1 (single declaration, no separate interface signature)
- `git grep -n "RuntimeConfigurable" -- '*.go'` → 8 (all comments/test names; no `type RuntimeConfigurable interface` declaration found)
- `gopls implementation processor/rule/runtime_config.go:14:22` (ApplyConfigUpdate) → 0
- `gopls references processor/rule/runtime_config.go:14:22` → 7 (cross-checked against `git grep`; the 3 `rule_integration_test.go` call sites are invisible to this because that file carries `//go:build integration` and gopls's default view excludes it — confirmed via `head -5 rule_integration_test.go`)
- `gopls references` over 16 struct fields (`messageCache, running, cancel, runtimeDone, runtimeWG, commandFenced, commands, commandWake, coordinatorDone, statusLoopDone, statusFenced, isSubscribed, subscriptions, cronScheduler, kvConfigManager, streamConsumers`), one call each via a batch script → 6, 6, 7, 14, 4, 5, 11, 11, 15, 6, 5, 5, 4, 12, 3, 6 references respectively (raw output retained in this session's scratchpad, not committed)
- `git grep -n "\.Stop(context\." -- 'processor/rule/*.go' ':!processor/rule/*_test.go'` → 0
- `git grep -n "Stop(context.Background())" -- 'processor/rule/*_test.go'` → 36
- `git grep -n "LifecycleFactory" -- '*.go'` → 9
- `git grep -n "StandardLifecycleTests" -- '*.go'` → 5 (declaration + 3 non-rule users + itself)
- `git grep -ln "rule.NewProcessor\|rule\.Processor{" -- '*.go' | grep -v processor/rule/` → 1 (`processor/gated-dag/fullstack_integration_test.go`)
- `git grep -n "rp.Stop(\|ruleProcessor.Stop(\|\.Stop(ctx)" -- 'service/*.go' 'internal/boot/*.go'` → 21 (generic `LifecycleComponent`/`BaseService` call sites, none naming `rule.Processor` specifically)
- `ls openspec/specs/ | grep -i lifecycle` → 3 (`component-lifecycle`, `lifecycle`, `nats-subscription-lifecycle`)
- `ls openspec/specs/ | grep -i runtime-context` → 1 (`runtime-context-ownership`)
- `grep -n "Stop\|bound" openspec/specs/component-lifecycle/spec.md` → 18 (filtered to head -40)
- `grep -n "^#\|Stop\|bound" openspec/specs/runtime-context-ownership/spec.md` → 19
- `grep -n "^## Context ownership\|^## " .agents/contracts/semstreams-developer.md` → 9 headings, none literally "Context ownership" at `##` level
- `grep -n "Context ownership\|context.Context\|nil context\|Background()" .agents/contracts/semstreams-developer.md` → 4 (heading is `### Context ownership` at :194)
- `find . -type d -iname "*restore-go-lifecycle*" -o -type d -iname "*simplify-one-shot*" -o -type d -iname "*gh1062*"` → 3 (all found under `openspec/changes/archive/`)
- `grep -n "coordinator\|command lane\|commandWake\|commandMu\|runRuntimeCoordinator\|fenceRuntimeCommands\|submitRuntimeCommand" <each archive's design.md>` → 0 for all three
- `grep -rln "coordinator\|command lane\|commandWake\|commandMu\|runRuntimeCoordinator\|fenceRuntimeCommands\|submitRuntimeCommand" <all three archive dirs>` → 2 files (both generic prose, see Adjacent claims)
- `git log --format='%h %ad %s' --date=short -S runRuntimeCoordinator -- processor/rule/processor.go` → 1 (`c7ca5d0f 2026-08-20`, matches brief's expectation)
- `git show --stat 40cb067d` → 1 commit, 3 files touched
- `git show 40cb067d -- processor/rule/processor.go` → 1 diff (hunks pinned above)
- `gh issue view <n> --json title,state` for #1273 #1274 #1283 #1012 #1064 #1041 #1042 #1170 → 8/8 found
- `gh pr list --state open --json number,title,body --limit 100` filtered for `#1283`/coordinator-fence symbols → 3 (`#1409, #1408, #1404`)
- `gh pr view 1409 --json ...` → 1 (this task's own claim PR, branch matches this worktree)
- `git grep -n "synctest.Test\|testing/synctest" -- 'processor/rule/*.go'` → 3 (all in `lifecycle_runtime_test.go`)
- `find internal/boot -iname "*rule_hot_reload*"` → 0 (file named in brief does not exist)
- `sed -n '190,200p' processor/rule/readiness_integration_test.go` → confirms the :197 quote (wall-clock `context.WithTimeout(context.Background(), 5*time.Second)`, not synctest)
- `grep -n "synctest\|context.WithTimeout\|time.Sleep" processor/rule/lifecycle_owner_test.go` → 0 (no synctest, no explicit timeout/sleep pattern found by this filter)
- `grep -n "synctest\|context.WithTimeout\|time.Sleep" processor/rule/kv_hot_reload_integration_test.go` → 10 (wall-clock: `time.Sleep`, `context.WithTimeout(..., 30*time.Second)`)
- `grep -n "commandMu\s\+sync\|lifecycleMu\s\+sync\|^\s*mu\s\+sync\|entityDispatchGate\s\+sync" processor/rule/processor.go` → 4
- `sed -n '498,502p' processor/rule/cron_scheduler.go` → confirms `<-barrier` at :500 (brief's pin, CronScheduler surface — not swept further, out of scope)
- `grep -n "func (s \*CronScheduler) fenceDispatch\|dispatchDone == nil" processor/rule/cron_scheduler.go` → 2 (confirms :344 self-settle branch exists there, contrasting with `fenceRuntimeCommands`'s absence of one)

### NOT RUN
- `gopls call_hierarchy` on any symbol in this file — not exercised; `gopls references` and `git grep` covered every consumer question the brief asked.
- Full read of `actions_run_scope_integration_test.go:130` and `triple_mutator_revision_integration_test.go:74` to confirm whether `ingest` there is `rule.Processor` or a different type — flagged as unconfirmed in Consumers, not resolved.
- `entityBorrowDone`, `entityBorrowFenced`, `entityBorrowCount`, `entityDispatchRecords` — read/write sites not enumerated; brief did not name them among the sixteen fields, and `fenceEntityBorrowsLocked`/`awaitEntityBorrowSettlement` were only pinned for their own receive/select, not their backing fields.
- Sister-repo asks — none searched; brief scoped this file to `processor/rule` only and named no cross-repo contract on this surface.
