# GH-867 local startup observability design

> **Superseded in part:**
> `gh867-startup-observability-design-amendment.md` replaces this document's
> Metrics-first lifecycle ordering, metric ownership, readiness commitment,
> and related test dispositions. The amendment is authoritative for those
> surfaces; unaffected rulings here remain active.

Baseline: `8494a8e882e95c581f78d6baf059be56b998dcad`. Accepted inventory: `docs/proposals/gh867-startup-observability-inventory.md`, SHA-256 `d594173ab170cb53d8496f048e50cbe2ec35357d61d63ccfe5955cc5fbd1e100` (`INVENTORY PASS`).

## Constraints and skill result

Runtime service/component lifecycle truth remains ephemeral, process-local, new each boot, and manager-owned. ServiceManager records actual Service Start/Stop outcomes; ComponentManager records actual Initialize/Start/Stop outcomes. Health is current child observation. Readiness derives from both and is not stored.

Keep the fail-closed component barriers. Add no timeout, degraded continuation, async supervisor, readiness service, lifecycle framework/state machine, KV/JetStream/payload/NATS round trip, recovery, service-shutdown component claim, or agent/LLM/persona/role. `StartAll` keeps the live non-signal-canceled `runtimeCtx`; #867 makes a blocked Start observable, not interruptible.

`orchestration-check` applies because startup sequencing changes. Result: existing Manager lifecycle composition owns it, not a rule, Lifecycle-harness entity, workflow, or component. No state enters `ENTITY_STATES`. Other decision skills do not trigger.

## Options

| Option | Result | Disposition |
|---|---|---|
| Do nothing | Keeps both ports dark. | Reject. |
| Move/extend dedicated health listener | Optional second port; existing clients use shared `/readyz`; metrics stays dark. | Reject. |
| Bind shared HTTP early only | Corrects HTTP but violates owner’s explicit HTTP-plus-metrics PR4. | Reject unless owner narrows scope. |
| **Recommended: early shared diagnostics + early built-in Metrics + manager snapshots** | Both measured ports bind before another Service/component can block; full routes promote only after success. | Smallest complete vertical. |
| Timeout/fire-and-forget/supervisor | Weakens fail-closed ownership and invents orchestration. | Reject. |

A two-PR HTTP/metrics split knowingly leaves half the accepted issue unresolved. Recommend one cohesive #867 PR.

## Target contract

### Private observations

No exported lifecycle API. ServiceManager keeps one private record per sealed service: Start invoked/completed/error and Stop invoked/completed/error. Records are written immediately around actual calls under its mutex; no context/cancel/deadline/goroutine is stored.

ComponentManager extends its private runtime observation with the actual Start result before `startDone` closes. Its snapshot counts admitted Discoverables, lifecycle participants, Starts invoked, completed, and failed. A valid non-lifecycle Discoverable stays `StateCreated`, contributes Health/DataFlow, and is never counted as a lifecycle participant or missing Start. Disabled/failed-construction optionals are absent.

ServiceManager snapshots copy records/references under locks, then release locks before child Status/Health calls. ComponentManager retains its existing borrow fence. No stored phase machine is added. Per-read status is derived: `failed` if a Start failed; `starting` while a participant is incomplete; `ready` when the predicate is true; otherwise `not_ready` after Starts complete but health is false.

### Readiness and exact compatibility

`/readyz` is never vacuously ready. Before a sealed nonempty composition containing mandatory `component-manager`, it returns 503 exact `NOT READY`.

It returns 200 exact `READY` only when all admitted Service Starts completed successfully, all ComponentManager lifecycle participant Starts completed successfully, no Start failed, all admitted Services currently report running/healthy, and every admitted component—including non-lifecycle Discoverables—currently reports healthy. If completion is already false, it returns 503 without calling component Health to rediscover false; after completion it observes Health directly. It never reads retained data.

Keep default text/plain bodies exactly `READY` / `NOT READY`; add no default JSON, query mode, content negotiation, Retry-After, or predicted duration. Add diagnostic facts to existing `/services`:

```json
"startup": {
  "status": "starting",
  "services": {"admitted": 5, "starts_invoked": 2, "starts_completed": 1, "starts_failed": 0},
  "components": {"admitted": 8, "lifecycle_participants": 7, "starts_invoked": 7, "starts_completed": 6, "starts_failed": 0}
}
```

This is additive/count-only; no raw error/context/authority is exposed.

### Shared HTTP early bind and atomic promotion

After mandatory creation and seal, before any Service Start, bind a startup mux on the existing shared port. It serves only `/health`, `/healthz`, `/readyz`, `/services`, `/services/health`, and—when the concrete framework ComponentManager exists—`/components/health`, `/components/list`, `/components/status/{name}`. Other paths return 503 `NOT READY`; graph/OpenAPI/docs/config/type/flowgraph/product/gateway routes do not run early.

Construct one `http.Server`. Its Handler is the preconfigured product middleware chain around a private Manager dispatcher loading one `atomic.Pointer[http.ServeMux]`. Store the diagnostic mux initially. After every Service Start succeeds, build a separate full mux off-path; refactor route helpers to accept that mux. Register every system/service/gateway/OpenAPI route there, then publish it with one atomic store. Requests see diagnostic or complete routes, never partial registration. Middleware applies to both. `UseHTTPMiddleware` remains pre-StartAll and rejects late calls. The optional dedicated health listener remains post-StartAll and unchanged.

### Early Metrics

Shared HTTP binds first. At seal, derive one private lifecycle order: admitted service `metrics`, when configured, first; all others retain current registration order. StartAll uses it; StopAll uses its reverse. Add no generic priority/DAG/group/phase interface or knob. Built-in Metrics keeps its current synchronous bind and exact Stop ownership. Disabled metrics remains absent. A metrics bind failure is briefly observable via shared not-ready, then fails boot and closes shared HTTP; no later Service starts.

Add core low-cardinality `semstreams_startup_units{owner,stage}`. Fixed pairs: services × `admitted|starts_invoked|starts_completed|starts_failed`; components × `admitted|lifecycle_participants|starts_invoked|starts_completed|starts_failed`. No unit-name label. Managers update from their own records. Gauges distribute lifecycle progress only; `/readyz` still reads current health directly.

### Failure, rollback, and signal

Keep provider-first/consumer-second barriers and `batch.Wait`. Component failure still fails ComponentManager.Start; Service failure still fails StartAll. Never promote or return ready after failure.

Every failure after early bind uses existing bounded synchronous rollback: record failure; Stop admitted Services in reverse planned lifecycle order (including early Metrics under its current idempotent/failed-Start contract); stop publisher if present; shut down/join Manager listeners. Shared bind failure starts no child. Metrics bind failure closes shared HTTP and starts no later Service. Full-mux build failure rolls back and never publishes partial routes.

Early HTTP retains only listener/private CancelFunc/done and uses exact StartAll context through `http.Server.BaseContext`; no Context is stored.

#1020 is unchanged. An in-progress Start receives live `runtimeCtx`; SIGINT/SIGTERM does not cancel it or cause concurrent StopAll. Diagnostics stay available, but this PR adds no unwinding supervisor. Normal successful boot followed by SIGTERM must still complete bounded ordered Stop/Close and release both ports.

## Specification workflow collision — explicit owner fork

Current truth conflicts:

- `framework-composition/spec.md:159-170` forbids HTTP/service-health exposure while component Starts are outstanding/failed and says HTTP never comes up.
- `service-composition/spec.md:244-264` says readiness is unchanged although its Purpose excludes readiness.

Owner says “No new lifecycle framework or OpenSpec.” Coherent choices:

1. **Recommended explicit exception:** owner confirms the #867 comment authorizes direct correction of those current spec clauses in the implementation PR, with no `openspec/changes` directory. Keep fail-closed barrier; permit diagnostics/not-ready before completion; forbid full-route promotion/ready on failure; remove service-composition’s readiness claim. PR cites owner comment and accepted design hash.
2. Owner narrows “no OpenSpec” to no new capability/framework but requires the normal minimal OpenSpec delta.
3. Do not implement until the conflict is removed.

Leaving current specs false is not an option. Draft recommends 1, but implementation is blocked until owner accepts one fork.

## Adopter seam

Product composition developer:

1. **Must know:** no new method/config/port/subject/bucket/callback/priority/timeout. Middleware remains pre-StartAll. Built-in Metrics starts before other Services.
2. **Do nothing:** existing shared/Prometheus ports bind earlier. Diagnostics return facts/not-ready; other routes return 503 until promotion; post-boot routes and readiness bodies remain. Disabled metrics remains absent.
3. **Find out:** update `service/doc.go`, HTTP middleware ops docs, `/services` docs, metric docs, release/migration note. No compile error because signatures do not change.
4. **SHOULD know:** TCP reachability is not readiness; 200 `/readyz` is the gate. Nothing about internal records/participant detection/order mechanics.
5. **Observation:** managers observe sealed/admitted sets and actual calls; adopter predicts no counts/budget/readiness.

Operator:

- wait-for-200 probes remain correct: refusal becomes 503 during startup, then exact 200 `READY`;
- TCP-is-ready consumers must switch to status code—this timing change is necessary;
- counts are on `/services` and fixed metrics; current health stays on existing diagnostic routes;
- product middleware applies during startup, so product owns probe auth policy.

Component author:

- no interface/semantic change; slow Start stays fail-closed but visible, never timed out/detached;
- non-lifecycle Discoverables remain valid and have no Start completion.

No sister writes. SemStreams documents timing/additive migration effect; SemSource/SemDev/SemTeams/SemSpec/SemDragon/SemBoids/SemSage/SemMem owners validate their own roots.

## Exact scope

Production:

- `service/service_manager.go`: private outcomes/snapshot, metrics-first order, early listener, atomic mux, readiness, `/services.startup`, no lock across child calls, rollback.
- `service/component_manager.go`: private Start-result observation/count snapshot.
- `service/component_manager_http.go`: private registration of three read-only early component diagnostics.
- `metric/core.go`, `metric/registry.go`: startup GaugeVec/update/registration.
- `cmd/semstreams/main.go`: replace premature pre-manager `SemStreams ready` with non-readiness wording; keep post-StartAll success spelling.

Current truth/docs, only under fork 1:

- `openspec/specs/framework-composition/spec.md`, `openspec/specs/service-composition/spec.md`: direct correction; no new change directory.
- `service/doc.go`, `docs/operations/09-http-middleware.md`, relevant existing metric/service docs: timing, promotion, body compatibility, `/services.startup`, metric, TCP warning.

Tests:

- new `service/startup_observability_test.go`;
- update `component_manager_start_barrier_test.go`, `service_manager_test.go`, `service_manager_stopall_test.go`, `framework_owned_bucket_guards_integration_test.go`, `middleware_test.go`;
- update `metrics_lifecycle_test.go` or cover real early bind in vertical test;
- update `metric/registry_test.go`, `metric/integration_test.go`;
- touch `cmd/semstreams/signal_shutdown_test.go` only if log/start seam needs it; signal semantics unchanged.

Out: payload/NATS/config streams, ENTITY_STATES, service-shutdown capability, dedicated-listener contract, component interfaces, sister repos, agentic packages.

## TDD RED plan

1. Admit one healthy non-lifecycle Discoverable and one gated LifecycleComponent. While held: admitted=2, participants=1, invoked=1, completed=0, failed=0; after release completed=1. Channels only.
2. Compose real built-in Metrics, gated component-manager Service, and later Service. When gate-entry closes, assert shared and metrics listeners are bound and later Start not entered. Release; prove success. StopAll order must be reverse of actual metrics-first planned order.
3. Readiness: direct pre-StartAll is 503 exact `NOT READY`; real shared endpoint during gate is same; successful completion/current health gives 200 exact `READY`; later unhealthy gives 503 without lifecycle mutation.
4. `/services.startup` and startup metrics distinguish admitted from lifecycle participants/completions and omit absent optionals. Scrape real Metrics while gate held.
5. During gate, diagnostics traverse middleware; non-diagnostic route returns 503 and handler is not called. After release atomic promotion yields 200 through same middleware. Concurrent promotion accepts only startup 503 or final response, never partial/404/panic; run race.
6. Failure: occupied shared port invokes no Service; occupied metrics port closes early shared and invokes no later Service; child failure records failed, never promotes/ready, rolls back and releases both ports. Replace old “HTTP never created” integration assertion with diagnostic-bind/fail-closed/cleanup truth.
7. Context/cleanup: nil/pre-canceled StartAll acts on nothing; BaseContext derives from exact StartAll context; no stored Context; normal StopAll joins Serve goroutines/releases ports. Existing #1020 tests remain green without claiming blocked Start cancellation.

Use entry/release/done channels, exact listeners, bounded request contexts, and owner join signals. No arbitrary sleeps or polling as causality.

## Verification and e2e ruling

Focused:

```bash
go test -race ./service ./metric ./cmd/semstreams
go test -race -tags=integration ./service -run 'StartAll|Startup|Readiness|Metrics|BootFailsClosed'
```

Repository:

```bash
task lint
go test -race ./...
task schema:generate
git diff --exit-code -- schemas/ specs/
go test ./test/contract/...
```

`task e2e:core` is mandatory before integration because both default listeners, readiness timing, middleware exposure, startup counts, and teardown change. Unit tests prove held-start behavior; core proves normal real-process HTTP/metrics/dataflow and teardown. No semantic/agentic/statistical tier: graph inference, payloads, rules, and loops are unchanged. If core lacks a normal-boot assertion for new metric labels, add it rather than run an unrelated tier.

## Owner acceptance gates

Before implementation, owner must bind:

1. recommended one-PR option;
2. exact `/readyz` body compatibility with additive `/services`/Prometheus facts;
3. configured built-in Metrics as the only special early Service, no generic priority API;
4. spec fork 1 or an explicit alternative removing contradiction.

No implementation begins until independent SemStreams design review passes and owner binds these rulings.
