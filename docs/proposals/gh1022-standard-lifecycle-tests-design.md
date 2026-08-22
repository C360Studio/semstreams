# GH-1022 StandardLifecycleTests design checkpoint

Baseline: `43dbf6fb72a9c346750b9c6b96fa8df8165f7bbe`.

Accepted inventory: `docs/proposals/gh1022-standard-lifecycle-tests-inventory.md`, SHA-256
`8a9b788c07396710d3540e0330e4bbe93b5b8a74c402cbde43c6e8f50747fe7d` (`INVENTORY PASS`).

## Accepted inventory (verbatim)

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

## Options considered

### Option A — do nothing

Leave `StandardLifecycleTests`, the `LifecycleComponent` comment, and UDP unchanged. Cost: stale sharing/rejoin/replay case names remain exported guidance; accepted-Start cancellation, finite normal Stop, and partial failed-Start behavior remain indistinguishable; the archived mechanical test-zero claim remains false; the interface's cancel-before-cleanup wording continues to conflict with ADR-095's native-drain-before-cancel protocol.

### Option B — correct only the shared suite

Keep `LifecycleFactory` unchanged; delete the cases that require or advertise concurrent Initialize, result equality/replay, later-Stop rejoin, and post-Stop Initialize; add only portable tests expressible through the existing factory. Cost: this removes the misleading exported test contract without adding a harness, but leaves the interface ordering collision and UDP's explicit later-Stop rejoin behavior untouched.

### Option C — correct the portable contract and the one measured adopter conflict

Choose Option B, clarify `LifecycleComponent` GoDoc as an owner-specific terminal fence/cancel/join contract, and migrate UDP from retained later-Stop rejoin state to direct private cancellation plus a private Start-owned completion observation channel used by the first caller-bounded Stop. Add focused UDP owner tests and a new component-lifecycle OpenSpec capability delta. Keep gateway/http and graph-index production unchanged. Cost: one bounded production owner migration and a new current-spec home; benefit: code, tests, GoDoc, ADR-095, and portable adopter guidance converge without a replacement lifecycle framework.

### Option D — expand the exported test factory into a lifecycle fault harness

Add options/hooks that let `StandardLifecycleTests` block joins and inject partial Start acquisition so the shared suite can prove every resource-specific condition. Cost: a larger exported testing API that external authors must understand; fake hooks cannot model native NATS, UDP, HTTP, worker, or callback admission ordering faithfully; it generalizes the exact machinery ADR-095 assigned to concrete owners.

## Recommendation

Recommend Option C. It is the smallest option that corrects both misleading shared authority and the one measured production rejoin path. It adds no lifecycle framework, runtime coordinator, communication path, payload, query surface, config, wire format, persistent state, agent, LLM, persona, or role.

## Measurable premises

1. The public signature stays unchanged: `component/lifecycle.go:48-53`.
2. Exactly three in-repo `StandardLifecycleTests` callers exist: `gateway/http/http_lifecycle_test.go:57`, `input/udp/udp_lifecycle_test.go:41`, and `processor/graph-index/lifecycle_integration_test.go:78`; the exact `rg` census is in the accepted inventory.
3. Zero sibling repositories directly call `StandardLifecycleTests`, `BenchmarkLifecycleMethods`, or `TestErrorInjection`; the time-bounded command and result are in the accepted inventory.
4. The current factory cannot induce a blocked join or partial-acquisition failure: `component/lifecycle_test_suite.go:16-17` exposes only `func() LifecycleComponent`.
5. UDP alone among the three shared-suite adopters explicitly retains later-Stop rejoin: `input/udp/udp.go:507-582`, especially `:532-533`. Gateway owns no running goroutine/rejoin state at `gateway/http/http.go:49-70,102-147`. Graph-index explicitly rejects later running-generation rejoin at `processor/graph-index/lifecycle_order_test.go:49-105`.
6. The interface order and concrete protocol order collide: `component/lifecycle.go:47` says cancel then join/cleanup; ADR-095 `:26-31` and graph-index `processor/graph-index/component.go:797-810` drain admission while callback authority remains live, then cancel.
7. Partial failed-Start cleanup is resource-specific and already proven by graph-index at `processor/graph-index/failed_start_subscription_test.go:48-199`; the shared suite observes only Stop after pre-action rejection at `component/lifecycle_test_suite.go:176-245`.
8. `service-shutdown` cannot be the component spec home because its purpose explicitly excludes component lifecycle at `openspec/specs/service-shutdown/spec.md:3-14`.
9. Touched production types currently retain no `context.Context`, no root/provider, and no exported cancel; graph-index's private `context.CancelFunc` is allowed. The accepted inventory records the exact search.
10. Already-migrated file/HTTP inputs consume Stop authority once but do not establish a universal Start-after-Stop rejection: `input/file/file.go:378-438`, `input/http/http.go:249-314`. The target therefore removes any portable reuse promise without requiring every implementation to reject an optional later Start.

## Proposed owner rulings

- **R1 — Portable floor, unchanged factory.** `LifecycleFactory` remains `func() LifecycleComponent`. `StandardLifecycleTests` is a portable minimum, not proof of resource-specific drain order, blocked joins, or partial-acquisition rollback.
- **R2 — Retained shared assertions.** The suite retains fresh Initialize; a normal Initialize→Start→Stop in which the accepted Start context remains live while a separate finite Stop context bounds the owner's controlled terminal sequence; a distinct fresh-instance case in which an accepted Start parent is canceled before a separately bounded Stop observes owner completion; nil Start/Stop rejection; pre-canceled/pre-expired Start rejection followed by safe Stop; safe Stop before Start; completed repeated Stop nil; and parallel fresh-instance/leak smoke only if it remains deterministic. Keeping the normal Stop case's Start authority live preserves owner-specific drain-before-cancel protocols. The suite removes ambiguous or unsupported cases for repeated Start result, Start without Initialize, post-Stop Initialize, concurrent Initialize, concurrent Stop result equality/replay, and canceled/expired Stop followed by a later Stop.
- **R3 — No fake universal failed-Start proof.** The shared suite's failed-Start floor is only safe Stop after Start rejects before acquisition. Components with fallible acquisition retain focused owner-local tests for exact handle rollback, retained cleanup authority, and caller Stop retry. No exported hook or fault harness is added.
- **R4 — Owner-specific Stop order.** GoDoc says Start's context owns runtime work and Stop uses its caller context only to bound the owner's terminal admission-fence/cancel/join/cleanup sequence. It does not prescribe cancel before every protocol fence. Nil is rejected before action; completed repeated Stop is nil/no-op; concurrent execution, result replay, later running-generation rejoin, reinitialization, and restart are not portable promises.
- **R5 — UDP is the only production delta.** UDP replaces retained later-Stop rejoin state with a private synchronized cancel function, a private Start-owned completion observation channel, and the existing WaitGroup as owner/test proof. Start derives a child of the exact caller context and publishes both cancellation authority and the new completion channel before launching the read loop. The Start-owned read goroutine owns a synchronous exit defer/finalizer that publishes terminal runtime state and finalizes its exact socket and buffer/resource state only when that goroutine actually exits: `running=false`, `conn=nil`, terminal buffer/resource state closed, and the existing WaitGroup completed. Only after that synchronous finalization does the Start-owned goroutine close its completion channel. Stop launches no waiter or detached cleanup goroutine: the first valid Stop consumes cancellation authority once, snapshots the Start-owned completion observation, closes the exact UDP socket to fence/unblock reads, cancels runtime work, and selects that completion against the exact caller context. If the bound wins, the error is returned honestly. Because cancellation authority has already been consumed, a later Stop ignores the completion channel and returns immediate nil/no-op; it neither observes nor rejoins that running generation. When a blocked Start-owned goroutine is later released, its own defer performs the eventual finalization and closes completion as natural Start-owner completion, not later-Stop rejoin or result replay. While that completion remains unobserved, health is derived from observed resource state and cannot remain falsely healthy merely because `conn` is a nonnil closed pointer. No context is retained, no root is invented, no generic finalizer framework is added, and same-instance restart remains unspecified rather than newly guaranteed or universally rejected.
- **R6 — Existing aligned adopters stay production-stable.** Gateway/http and graph-index receive no production changes. Their existing shared-suite call sites remain and must pass the corrected floor. Graph-index owner-specific failed-Start and no-rejoin tests remain authoritative.
- **R7 — Dedicated current spec.** Add a new `component-lifecycle` capability delta. Do not extend `service-shutdown`, the named workflow `lifecycle` capability, or `component-runtime-config` with unrelated mechanics.
- **R8 — No adjacent claims.** This change makes no claim that service shutdown governs components, no universal service-manager/process proof, no resolution of #867, #1012, or #1013, no restartable-instance contract, and no tag-readiness claim.

## Exact target behavior

### Shared suite

1. A fresh factory result is nonnil and Initialize returns nil.
2. The normal controlled case initializes, starts with a live nonnil Start context, and invokes Stop with a separate finite caller context while Start authority remains live; Stop returns nil before its bound. This preserves owner-specific admission-drain-before-cancel ordering.
3. A distinct fresh instance initializes and starts with a cancellable context; after that accepted parent is canceled, a separately bounded Stop observes owner completion before its bound.
4. Nil Start and nil Stop return errors.
5. Pre-canceled and pre-expired Start return errors; a later valid Stop is safe.
6. Stop before Start is safe. No subsequent Start assertion is made.
7. After one completed normal Stop, a valid repeated Stop returns nil. The generic suite does not claim to observe teardown side effects.
8. No same-instance method is invoked concurrently. No result-sharing, replay, rejoin, reinitialize, or restart assertion remains.
9. Any retained stress/leak loop creates a fresh component for each lifecycle and uses finite Stop contexts. It is not named or documented as same-instance concurrency proof.

### UDP owner

1. Start derives runtime cancellation from the exact accepted caller context, retains no context, invents no root, and publishes both private cancellation authority and a private Start-owned completion observation channel before launching the read loop.
2. The Start-owned read goroutine owns a synchronous exit defer/finalizer. Only when that goroutine actually exits does it publish `running=false`, clear `conn=nil`, close terminal buffer/resource state, complete the existing WaitGroup, and then close its completion observation channel; no generic finalizer framework is introduced.
3. Accepted Start-parent cancellation terminates the read loop; a separate finite Stop observes the join.
4. Normal Stop consumes cancellation authority and snapshots the corresponding Start-owned completion channel once, fences admission by closing the exact socket, cancels runtime work, and selects completion against the exact caller context. A completed repeat is nil without repeating teardown.
5. With the owner join deterministically blocked, a canceled/deadline Stop returns the caller error. Because that call consumed cancellation authority, a second valid Stop ignores the completion channel and returns nil before release. Stop creates no waiter or detached cleanup goroutine, and the second Stop does not observe or rejoin completion.
6. After the blocker is released, the Start-owned goroutine's own defer performs eventual finalization, completes the existing WaitGroup, and closes its completion channel. Exact channel/WaitGroup synchronization then proves `running=false`, `conn=nil`, nonhealthy health, finalized resources, and no second teardown.
7. While owner completion remains unobserved, health is derived from observed resource state and does not report healthy solely because `conn` still points to a closed socket.
8. Start validation/bind failure leaves no running goroutine or leaked socket; Stop is safe. No generalized retained failed-Start cleanup state is introduced because UDP has no measured post-acquisition fallible step after a successful bind.
9. Tests use channels/WaitGroups, not sleeps, for ordering; timeouts are failure bounds only.

## Test migration

- `component/lifecycle_test_suite.go`: delete stale cases/functions and `fmt` if unused; add a normal controlled lifecycle that calls finite Stop while the accepted Start context remains live, plus a separate fresh-instance accepted-parent-cancellation case followed by finite Stop; make every cleanup Stop finite; retain deterministic fresh-instance leak/stress coverage only.
- `input/udp/udp_lifecycle_test.go`: continue calling `StandardLifecycleTests`; add focused normal live-parent Stop, accepted-parent cancellation, normal completed repeat, blocked-join canceled/deadline no-rejoin with post-release owner-finalization/resource/health proof, and failed-bind cleanup tests. The no-rejoin test must be RED against current UDP because the second Stop observes the current retained `done` channel. The corrected private completion channel remains Start-owned: it is published before launch and closed by the read goroutine after synchronous finalization, while only the first Stop that consumes cancellation authority may select it. The blocked case uses that exact completion observation and the WaitGroup for post-release proof, observes one teardown, proves a later Stop ignores completion, and proves Stop launches no waiter or detached cleanup goroutine.
- `gateway/http/http_lifecycle_test.go`: no source change expected; run corrected suite.
- `processor/graph-index/lifecycle_integration_test.go`: no source change expected; run corrected suite with real NATS.
- Preserve `output/websocket/websocket_test.go:1117,1122` compatibility with `TestErrorInjection` and `BenchmarkLifecycleMethods`. If Start benchmark retains repeated same-instance calls, label it performance-only; preferred smallest correction is fresh factory + lifecycle per iteration so the benchmark does not imply reuse.

## Exact file scope

Expected production changes:
- `component/lifecycle.go`
- `input/udp/udp.go`

Expected test changes:
- `component/lifecycle_test_suite.go`
- `input/udp/udp_lifecycle_test.go`

Expected OpenSpec/design materialization:
- `openspec/changes/align-standard-lifecycle-tests/proposal.md`
- `openspec/changes/align-standard-lifecycle-tests/design.md`
- `openspec/changes/align-standard-lifecycle-tests/tasks.md`
- `openspec/changes/align-standard-lifecycle-tests/specs/component-lifecycle/spec.md`
- `docs/proposals/gh1022-standard-lifecycle-tests-inventory.md`
- `docs/proposals/gh1022-standard-lifecycle-tests-design.md`

Verification-only, no expected source change:
- `gateway/http/http_lifecycle_test.go`
- `processor/graph-index/lifecycle_integration_test.go`
- `processor/graph-index/lifecycle_order_test.go`
- `processor/graph-index/failed_start_subscription_test.go`
- `output/websocket/websocket_test.go`

Explicitly out of scope: `service/*` behavior/specs, `docs/basics/05-first-processor.md`, ADR edits, sister-repo edits, #867/#1012/#1013, E2E scenario changes, configs, schemas, NATS subjects/buckets/streams, agents/LLMs/personas/roles.

## Draft OpenSpec artifacts

### proposal.md

# Change: Align StandardLifecycleTests with component lifecycle authority

## Why

The exported shared suite advertises concurrent Initialize, concurrent Stop result sharing/replay, later-Stop rejoin, and post-Stop reinitialization that the public interface does not promise and ADR-095 rejects for running-owner authority. It also omits portable accepted-Start cancellation and finite normal Stop coverage. UDP retains the one measured later-Stop rejoin path among current suite adopters.

## What changes

- Narrow `StandardLifecycleTests` to the portable LifecycleComponent floor.
- Clarify owner-specific terminal ordering in LifecycleComponent GoDoc.
- Replace UDP's later-Stop rejoin state with direct one-shot cancellation and a private Start-owned completion channel selected only by the first caller-bounded Stop.
- Add focused UDP owner tests and seed current `component-lifecycle` truth.

## What does not change

No method signature, exported test-factory shape, config, schema, wire contract, NATS primitive, persistent state, lifecycle framework, service-shutdown claim, restartable-instance contract, agent, LLM, persona, or role.

### component-lifecycle spec delta

# component-lifecycle Specification

## ADDED Requirements

### Requirement: Component runtime lifetime and terminal authority are caller-owned

`LifecycleComponent.Start(ctx)` MUST reject nil or already-ended context before action. The accepted Start context MUST own continuing component work and MUST NOT be retained on a production struct. `Stop(ctx)` MUST reject nil before action and use the exact caller context only to bound the concrete owner's terminal admission-fence, cancellation, join, and cleanup sequence. The exact resource-specific ordering MUST preserve admitted callback authority where its native drain protocol requires it; no universal cancel-before-drain order is implied.

#### Scenario: Accepted Start context ends

- **GIVEN** a fresh component accepted Start with a cancellable nonnil context
- **WHEN** that accepted parent context is canceled
- **THEN** continuing work derived from it exits
- **AND** a separately bounded Stop can observe owner completion

#### Scenario: Controlled Stop while Start authority remains live

- **GIVEN** a component accepted Start and its context remains live
- **WHEN** Stop is called with a separate finite context
- **THEN** the owner completes its terminal sequence before the Stop bound
- **AND** resource-specific admission drain may precede cancellation of Start authority

### Requirement: Running Stop has no shared-generation contract

A successfully running component's Stop MUST be caller-bounded. Completed repeated Stop with a valid context MUST return nil without repeating teardown. The portable contract MUST NOT promise concurrent Stop executor election, shared results, retained-result replay, later rejoin after a Stop bound wins, concurrent Initialize, post-Stop reinitialization, or same-instance restart. Implementations MAY reject or tolerate unsupported extra calls, but callers and shared tests MUST NOT rely on them.

#### Scenario: Stop bound wins

- **GIVEN** terminal owner work has not joined before the Stop context ends
- **WHEN** Stop returns the caller context error
- **THEN** the call reports the failed exit honestly
- **AND** the portable contract grants no later caller authority to rejoin that running generation

#### Scenario: Completed Stop is repeated

- **GIVEN** Stop completed
- **WHEN** Stop is called again with a valid context
- **THEN** it returns nil
- **AND** it performs no teardown side effect

### Requirement: Failed Start cleanup remains owner-specific

A component that returns from Start after acquiring resources MUST synchronously attempt bounded rollback and retain exact cleanup authority only when rollback does not complete. A later caller Stop MAY retry that retained failed-Start cleanup. This exception MUST NOT create running-generation rejoin, result replay, a shared lifecycle wrapper, or an exported test fault harness. Components with fallible acquisition MUST prove this behavior through owner-local deterministic tests.

#### Scenario: Partial Start rollback expires

- **GIVEN** Start acquired an exact resource and its bounded rollback did not complete
- **WHEN** another Start is attempted
- **THEN** it is rejected while cleanup remains pending
- **AND** a later caller Stop may retry the retained exact cleanup

### design.md summary

Record accepted inventory hash, Option C owner rulings R1-R8, exact shared-suite/UDP behavior, adopter seam, file scope, and verification plan. Record that UDP publishes a private Start-owned completion channel before launch, closes it from the read goroutine only after synchronous finalization, and permits only the first Stop that consumes cancellation authority to select it against the exact caller context. State explicitly that service-shutdown does not govern components and that optional same-instance reuse is neither promised nor universally prohibited.

### tasks.md

1. Materialize and independently review inventory and design; record owner rulings.
2. Add RED shared-suite/UDP tests for distinct live-parent controlled Stop and accepted-parent-cancellation paths, plus UDP no-rejoin and post-release Start-owner finalization behavior.
3. Correct LifecycleComponent GoDoc and StandardLifecycleTests without changing signatures/factory shape.
4. Migrate UDP to private cancel + socket fence + a private Start-owned completion observation selected by the first caller-bounded Stop; close completion only after Start-goroutine-owned synchronous exit finalization, and remove later-Stop rejoin behavior without a Stop-launched waiter or detached cleanup.
5. Run focused unit/race tests for component, gateway/http, input/udp, output/websocket; run graph-index lifecycle integration.
6. Run repository race, integration, lint, build, schema, contract, strict OpenSpec, and relevant CI gates.
7. Obtain independent implementation review; integrate atomically and close #1022 without adjacent lifecycle/tag claims.

## Verification plan

Focused:
- `go test -race ./component ./gateway/http ./input/udp ./output/websocket -count=1`
- graph-index real-NATS lifecycle integration with the repository integration task/filter
- repeated deterministic UDP completion-channel no-rejoin and post-release owner-finalization test under `-race -count=10`

Repository gates:
- `task lint`
- `go test -race ./...`
- `task test:integration`
- `go build ./...`
- `task schema:generate` plus no `schemas/`/`specs/` drift
- `go test ./test/contract/...`
- `openspec validate align-standard-lifecycle-tests --strict --no-interactive`
- hosted CI on the exact candidate

No E2E tier is required by the measured change: there is no wire/config/persisted-state behavior and no BREAKING commit claim. If implementation expands into process shutdown, #867, transport Close, or another assembled path, stop for a separate design/evidence ruling.
