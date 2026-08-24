# GH-1022 LifecycleComponent / StandardLifecycleTests inventory

Baseline: `43dbf6fb72a9c346750b9c6b96fa8df8165f7bbe` (clean `main`).

## Problem statement

`component.LifecycleComponent` exposes `Initialize`, `Start(ctx)`, and `Stop(ctx)`; the exported shared suite has case names and messages for same-instance, concurrent sharing, rejoin, and replay behavior, while its measured assertions are enumerated below. Inventory asks which assertions exist, who consumes them, what current truth says, and which remaining adopters are already aligned.

## Surface inventory

### 1. Claimed gap

- Public interface: `component/lifecycle.go:43-53` says Initialize is setup-only, Start receives the running context, and Stop cancels Start lifetime then bounds join/cleanup. It does **not** say concurrent Initialize/Stop, Stop-result sharing/replay, deadline rejoin, reinitialization, or restartable instance.
- The production order is itself a collision: the interface comment says Stop cancels the Start lifetime and then bounds join/cleanup, while ADR-095 `:26-31` requires native admission Drain/Closed while callback authority remains live and only then cancellation; graph-index implements drain-before-cancel at `processor/graph-index/component.go:797-810`. The current authorities do not state one universal ordering.
- `openspec/specs/runtime-context-ownership/spec.md:6-19,48-59` currently specifies context-bearing component Stop, nil-before-action, and completed repeated Stop no-op; it does not specify the rest of the component lifecycle.
- `openspec/specs/service-shutdown/spec.md:3-14` explicitly excludes component lifecycle, while `:63-92` rejects concurrent Stop/rejoin/result replay only for services. It cannot be cited as component authority.
- `openspec/specs/lifecycle/spec.md:1-244` is the named graph-workflow Lifecycle harness, a distinct concept; it does not govern `LifecycleComponent`.
- `openspec/specs/component-runtime-config/spec.md` contains port/config truth, not the component lifecycle contract (search `rg -n 'LifecycleComponent|one-shot|concurrent Stop|repeated Stop|failed Start' openspec/specs/component-runtime-config/spec.md` => zero).
- ADR-095 `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:9-45` is the accepted broad running-owner decision: no measured production caller for concurrent executor election/deadline rejoin/result replay; completed repeated Stop is nil/no-op; concurrent Stop and result replay are not contracts; failed Start uniquely retains retryable cleanup authority.
- Landed guide `docs/operations/migration-restart-safe-nats-client.md:107-114` says all inventoried owners use direct private/native authority, completed repeated Stop is no-op, and no shared generation/result/rejoin/state-machine API remains.
- Archived simplify ledger still contains an unmet explicit exit gate: `openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/recovery-ledger.md:2661-2692,2738-2749` requires removing/rewriting every test for concurrent result sharing, deadline rejoin, or replay while retaining completed repeat, honest timeout, failed-Start cleanup, and owner-specific goroutine join evidence. Current suite proves that archive claim was mechanically incomplete.

Exact searches for plausible spellings found no current component lifecycle capability spec and no replacement helper/API: `rg -n 'Generation|Operation|StopWithQuiesce|lifecyclejoin|rejoin|retained.*result|result.*replay' component gateway/http input/udp processor/graph-index --glob '*.go'` finds only the shared-suite cases and UDP's explicit rejoin comment; no production `Generation`, `Operation`, `StopWithQuiesce`, or `lifecyclejoin`.

### 2. Every current spelling of the lifecycle fact

- Interface and casts: `component/lifecycle.go:43-53,78-87`.
- Production composition consumer: ComponentManager initializes once during fixed boot at `service/component_manager.go:904-928`, creates one child Start context and `startDone` before launch at `:470-528`, calls Start once and records completion at `:531-570`, and rejects a concurrent component Stop at `:789-830`; failed-Start cleanup can retry through caller Stop at `:827-855`. No in-process restart is present.
- Test authority: `component/lifecycle_test_suite.go:16-34` exports `LifecycleFactory` and `StandardLifecycleTests`; `:36-165` holds compliance assertions; `:167-247` error paths; `:249-416` concurrent/stress assertions; `:418-490` fresh-instance leak check. Adjacent exported helpers in the same file are `BenchmarkLifecycleMethods` at `:492-540` and `ErrorInjectingComponent`/`TestErrorInjection` at `:542-667`.
- Documentation: `docs/operations/migration-restore-go-lifecycle-ownership.md:13-16,30-34,38-81,98-120` states Start owns runtime lifetime, Stop gets a fresh independent bound, no stored context, and specifically declines to infer concurrent Stop/result/replay/restart. `docs/basics/05-first-processor.md:736-954` is stale adjacent tutorial code using reusable shutdown/done channels, no nil validation, and an older port grammar; it is not reliable current lifecycle authority and is broader documentation debt.
- Concrete owner-local one-shot proof already exists across migrated owners. Representative exact current anchors: `input/file/file_lifecycle_test.go`; `input/http/http_lifecycle_test.go`; `processor/graph-{clustering,embedding,index-spatial,index-temporal,query}/lifecycle_owner_test.go`; `output/{file,httppost,otel}/...lifecycle...test.go`; `storage/objectstore/lifecycle_owner_test.go`; `processor/rule/lifecycle_owner_test.go`; and `service/base_lifecycle_test.go`. The archive records the first input migration and replacement of `StandardLifecycleTests` with focused Start/Stop, parent-cancel, blocked-join deadline, and completed-repeat tests at recovery ledger `:2076-2105`; base service's one-shot/no-rejoin proof is recorded at `:2368-2383` but does not govern components.
- Migrated `input/file/file.go:378-438` and `input/http/http.go:249-314` consume Stop cancellation authority once and have focused no-rejoin tests, yet do not universally reject later Start; this proves absence of a restart contract must not be restated as a universal rejection contract.

### 3. Exact StandardLifecycleTests assertion classification

- **Normal fresh Initialize**: `component/lifecycle_test_suite.go:42,63-66` constructs a fresh component, calls Initialize, and asserts nil.
- **Normal Initialize→Start→Stop**: `:43,68-81` calls the three methods in order and asserts nil for each; Stop uses unbounded Background and supplies no finite caller bound.
- **Stop before Start**: `:44,83-87` and duplicate `:50,144-147` call Stop and assert nil; neither case attempts a later Start.
- **Repeated Start while running**: `:45,89-105` calls Start twice and ignores the second result.
- **Completed repeated Stop**: `:46,107-122` asserts both Stop calls return nil; no side-effect hook observes whether teardown occurs once.
- **Nil Start/Stop**: `:47-48,124-131` calls Start and Stop with nil and asserts both return errors.
- **Start without Initialize**: `:49,133-142` and `:202-208` accepts either nil or an error whose text contains the expected initialization wording.
- **Initialize after Stop**: `:51,149-165` asserts only that a second Initialize returns nil; it never attempts Start or otherwise proves instance reuse. Graph-index Initialize is idempotent at `processor/graph-index/component.go:565-567`, while a later Start rejects at `:608-610`.
- **Rejected canceled/expired Start**: `:176-201` passes pre-canceled and pre-expired contexts and asserts Start returns errors; it does not cancel an accepted running Start context.
- **Cleanup after failed Start**: all error-table cases call Stop at `:242-245`, so Stop after the suite's pre-action Start rejections is covered. The factory cannot induce a partial-acquisition Start failure, so retained exact-handle cleanup is not observed.
- **Concurrent Initialize**: `:249-266,330-361` launches 20 Initialize calls and asserts at least one returns nil.
- **Concurrent Stop generation/result sharing and completed-result replay**: `:254-256,298-328` asserts eight concurrent Stop results are string-equal and a later Stop result is string-equal. It does not observe executor identity or a shared generation; its case names/messages claim sharing and replay beyond the measured result equality.
- **Canceled/expired Stop rejoin**: `:257-262,268-296` asserts the first Stop satisfies `ErrorIs` for the caller context error and a later Stop returns nil; it does not observe cleanup or rejoin. Graph-index's explicit no-rejoin behavior at `processor/graph-index/lifecycle_order_test.go:83-89` satisfies those assertions.
- **Stress**: `:263-265,363-416` creates a fresh component per worker iteration; it does not invoke concurrent methods on one instance and asserts no lifecycle result beyond nonnil factory.
- **NoLeaks**: `:418-490` repeats a fresh normal lifecycle and checks coarse process memory/goroutine deltas; it does not observe an individual owner's join handles.
- **Benchmarks**: `:503-527` benchmark Start repeatedly on one instance and Stop repeatedly on one instance. They record performance under those call patterns and make no lifecycle assertion.
- **Start-lifetime cancellation**: no shared assertion cancels an accepted Start context and then observes Stop.
- **Bounded Stop**: no normal shared assertion supplies a finite Stop context and verifies return; `LifecycleFactory` exposes no blocking seam.
- **Failed-Start cleanup**: partial-acquisition cleanup is absent and cannot be induced through the current factory.

### 4. Every in-repo shared-helper adopter/call site

`rg -n 'StandardLifecycleTests' --glob '*.go'` yields exactly three calls plus the definition:

1. `gateway/http/http_lifecycle_test.go:12-57` factory + call. Current production `gateway/http/http.go:49-70,102-147` only toggles `running`; after successful or failed Stop, a later Start is accepted. It has no running goroutine or rejoin/result state; completed Stop is an immediate no-op on repeat.
2. `input/udp/udp_lifecycle_test.go:11-41` factory + call. Current `input/udp/udp.go:113-148,401-477,507-582` recreates shutdown/done channels on each Start, explicitly says a later Stop may rejoin at `:532-533`, clears handles, and allows restart.
3. `processor/graph-index/lifecycle_integration_test.go:16-78` real-NATS shared fixture + call. Graph-index is one-shot in `processor/graph-index/component.go:243-253,581-621,729-784`; focused tests reject restart, reject rejoin after failed running Stop, prove completed repeated Stop, and retain retryable failed-Start cleanup at `processor/graph-index/lifecycle_order_test.go:49-125` and `failed_start_subscription_test.go:48-199`.

Adjacent exported helper uses: `output/websocket/websocket_test.go:1117,1122` calls `TestErrorInjection` and `BenchmarkLifecycleMethods`; no in-repo caller uses `NewErrorInjectingComponent` directly. Those helpers are not StandardLifecycleTests call sites.

Focused current suite measurement: `go test ./gateway/http ./input/udp -run ComprehensiveLifecycle -count=1` is green. That proves current hidden requirements are accepted by those two legacy adopters, not that they satisfy ADR-095.

### 5. External consumer-at-birth census

A time-bounded sibling search found **zero** direct uses of `StandardLifecycleTests`, `BenchmarkLifecycleMethods`, or `TestErrorInjection`: `find /Users/coby/Code/c360 -maxdepth 1 -mindepth 1 -type d ! -name semstreams ! -name 'semstreams-*' -print0 | xargs -0 rg -n 'StandardLifecycleTests|BenchmarkLifecycleMethods|TestErrorInjection' --glob '*.go'` => empty.

The interface is genuinely external: examples include semconnect `gateway/cs-api/component.go:148,194`, semdev `internal/station/station.go:212,254`, semsage `processor/ui-api/component.go:48,118`, semsource `processor/mcp-gateway/component.go:44`, semlink `internal/semstreams/runtime.go:41,335-351`, and numerous semdragon components. These are read-only impact sources. No signature change is established by the measured gap, and no downstream validation was performed.

## Same-class collision table (runtime lifecycle-contract authority)

| Dimension | Existing owners/evidence | Inventory finding |
|---|---|---|
| Semantic class | `LifecycleComponent` Go interface (`component/lifecycle.go:43-53`); Standard suite (`component/lifecycle_test_suite.go:16-490`); ADR-095 (`:9-45`); ComponentManager (`service/component_manager.go:470-570,789-855`) | Four partial authorities; suite case names include sharing/rejoin/replay absent from the interface and rejected as contracts by ADR-095. |
| Catalogs | Registry holds immutable declarations, not handles/lifecycle (`openspec/specs/component-discovery/spec.md:192-256`); ComponentManager retains handles (`service/component_manager.go:904-932`) | No lifecycle catalog exists. |
| Status | `ManagedComponent.State/LastError` at `component/lifecycle.go:55-76`; manager state updates at `service/component_manager.go:548-569,843-855` | Observation only; not restart authority. |
| Lifecycle | Interface comment says cancel then bound join/cleanup; ADR-095 says native admission drain/Closed while callback authority remains live, then cancel; manager performs one boot Initialize/Start/Stop; concrete owners have local methods/tests | The interface and ADR state different ordering for owners with admitted callbacks; shared suite does not observe the ordering. |
| Ownership | ComponentManager sole concrete-handle owner (`component-discovery` spec `:192-200`); exact owner-local native handles per ADR-095 | Shared-suite case names/messages refer to executor sharing/rejoin, while measured assertions observe only results. |
| Readers | ComponentManager casts; three shared-suite callers; direct tests; external component authors | External suite consumption currently zero, interface consumption broad. |
| Writers | ComponentManager invokes Initialize/Start/Stop; direct composition/test callers also invoke methods | No separate lifecycle writer/API was found. |
| Recovery | ADR-095 separates failed-Start retained cleanup from terminal running Stop; graph-index proves it owner-locally | The shared factory cannot induce partial acquisition. |

## Context-ownership inventory on likely touched production owners

`rg -n 'context.Context|context.CancelFunc|context.Background\(|context.TODO\(|context.WithoutCancel\(' gateway/http/http.go input/udp/udp.go processor/graph-index/component.go component/lifecycle.go` found no stored `context.Context`, no Background/TODO/WithoutCancel roots, and one allowed private `context.CancelFunc` on graph-index at `component.go:249`. Gateway and UDP retain no context. No exported cancel exists.

## Adopter seam inventory — external component author

Specific adopter: a developer in semconnect/semdev/semdragon implementing `component.LifecycleComponent`, without reading SemStreams internals.

1. **What must they know today?** They must implement Discoverable + Initialize/Start/Stop, pass the accepted Start context through running work, reject nil context before action, and use caller Stop authority for bounded cleanup. GoDoc says Stop cancels the Start lifetime and then bounds join/cleanup, while ADR-095's concrete admission protocol says native drain/Closed occurs while callback authority remains live and only then cancellation. An external author cannot derive one universal resource-cleanup ordering from current authority. Current prose also says callers cannot rely on same-instance reinitialization/restart, completed repeated Stop is harmless, and failed Start can retain exact cleanup authority.
2. **What happens if they do nothing?** Their code still compiles because signatures do not change. A restartable/rejoinable implementation can satisfy the current suite's measured assertions; no typed failure exposes the semantic difference.
3. **Where do they find out?** Signatures/compile errors cover only method shape. Nil/context ownership is in GoDoc plus migration/spec; one-shot/no-rejoin is in ADR/migration prose; failed-Start distinction is ADR prose/owner examples. The shared suite currently gives contradictory runtime-test feedback. Correctness facts are therefore at docs/test level, a finding.
4. **What SHOULD they know?** Framework/package documentation should expose a small portable lifecycle contract, while the concrete owner observes the resources it acquired and their actual completion. The exact artifact and ordering boundary remain design questions.
5. **Observation vs prediction:** the concrete owner observes exact resources and completion; ComponentManager observes exact Start completion and calls owner Stop. Current evidence identifies no caller use of a generation or retained result.

## Open evidence questions for review

- The current `LifecycleFactory` cannot induce a blocked join or partial Start. Later design must choose between an unchanged portable floor and an expanded seam, with the costs of each measured explicitly.
- Whether UDP's explicit later-Stop rejoin belongs in #1022 production scope is an owner ruling.
- The current component lifecycle contract has no dedicated OpenSpec home, and `service-shutdown` explicitly excludes it. The spec-home ruling remains pending.

No decision skill triggers: no new communication path, orchestration, payload, or query access; `kv-or-stream`, `orchestration-check`, `new-payload`, and `query-pattern` are not applicable.
