# Readiness health-initialization flake inventory

Baseline: `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`.

## Problem statement

The integration suite intermittently fails `TestReadinessIncludesHealthyNonLifecycleDiscoverables` at
`service/startup_observability_test.go:220`: expected HTTP 200, observed HTTP 503.

The confirmed first-failure mechanism is a scheduling race between `BaseService.Start` returning and its initial
asynchronous health check. The fixture immediately commits startup and probes readiness without observing that initial
check.

This inventory distinguishes a proven deterministic test flake, a structurally possible production readiness window,
and what has not yet been demonstrated in a fully composed process. It makes no target-state or implementation ruling.

## Surface inventory

### Claimed gap and deterministic reproduction

The failing fixture:

- constructs a lifecycle component whose `Health` is always healthy at
  `service/component_manager_start_barrier_test.go:25-51`;
- constructs a non-lifecycle Discoverable whose health begins true at
  `service/startup_observability_test.go:74-91`;
- manually constructs a `ComponentManager` with a fresh `BaseService`, maps, component registry, and store registry at
  `service/startup_observability_test.go:190-204`;
- calls `ComponentManager.Start` synchronously at `:205`;
- manually admits the manager as the only service, records successful service Start accounting, and commits startup at
  `:208-216`;
- immediately invokes `/readyz` and expects 200 at `:218-220`;
- only afterward changes the non-lifecycle component to unhealthy and expects 503 at `:222-225`.

Measurements:

```text
go test -race ./service \
  -run '^TestReadinessIncludesHealthyNonLifecycleDiscoverables$' \
  -count=100
```

Result: passed 100/100.

```text
GOMAXPROCS=1 go test -race ./service \
  -run '^TestReadinessIncludesHealthyNonLifecycleDiscoverables$' \
  -count=100
```

Result: failed 100/100 at `startup_observability_test.go:220`, expected 200, actual 503.

The one-processor result makes the failure scheduler-controlled without repository mutation, arbitrary sleep, NATS,
Docker, test order, or an external dependency.

Exact issue search:

```text
gh issue list -R C360Studio/semstreams --state all \
  --search 'TestReadinessIncludesHealthyNonLifecycleDiscoverables'
```

Result: no existing issue names this test.

### Current spellings of health and readiness

`BaseService` owns service health:

- `healthy` is an `atomic.Bool` and therefore begins false: `service/base.go:82-105`.
- `IsHealthy` directly returns that atomic value: `service/base.go:196-199`.
- `Start` publishes `StatusRunning` before starting health monitoring: `service/base.go:249-275`.
- When health monitoring is enabled, `Start` launches `healthMonitor` as a goroutine and does not wait for its first
  check before returning: `service/base.go:271-280`.
- `healthMonitor` calls `performHealthCheck` immediately when scheduled, then on the ticker:
  `service/base.go:396-413`.
- With no custom health check and no NATS client, `performHealthCheck` computes success and stores `healthy=true`:
  `service/base.go:416-448`.

`ComponentManager.Start` preserves that asynchronous boundary:

- it completes all lifecycle component Starts first: `service/component_manager.go:338-382`;
- then calls `BaseService.Start`: `service/component_manager.go:384-387`;
- then launches its own health publisher supervisor, marks itself started, and returns:
  `service/component_manager.go:389-397`;
- there is no synchronization with `BaseService.healthMonitor`'s first `performHealthCheck`.

Production `ComponentManager` installs `cm.healthCheck` during construction at
`service/component_manager.go:119-225`. The failing fixture manually constructs the manager and does not install that
custom function. That distinction does not remove the race: either path still depends on the asynchronously scheduled
first `performHealthCheck`. The fixture's default check has no failure source and eventually sets healthy true.

`ComponentManager` component health is a separate direct projection:

- `startupSnapshot` counts admitted components, lifecycle participants, invoked Starts, completed Starts, and failures:
  `service/component_manager.go:596-625`;
- `GetComponentHealth` borrows the component set and invokes every admitted component's `Health`, including
  non-lifecycle Discoverables: `service/component_manager.go:1191-1205`;
- `healthCheck` treats not-yet-started ComponentManager state as healthy and later checks retained failed component
  state: `service/component_manager.go:1023-1071`.

`Manager` readiness joins those facts:

- `/readyz` returns 200 only when `currentStartupSnapshot().Status == "ready"`:
  `service/service_manager.go:1776-1786`;
- startup completion derives from sealed service outcomes and ComponentManager lifecycle participant counts:
  `service/service_manager.go:1788-1834`;
- after boot commitment, every admitted service must report `StatusRunning` and `IsHealthy()`:
  `service/service_manager.go:1837-1842`;
- every admitted component must have a health entry and report healthy:
  `service/service_manager.go:1843-1855`;
- only then is status `ready`: `service/service_manager.go:1856-1861`.

In the failing schedule, the first false predicate is the service check at
`service/service_manager.go:1837-1840`: ComponentManager status is running, but its inherited BaseService health remains
false. Component health is not yet consulted.

### Test order, globals, and CI execution

The failing test:

- does not call `t.Parallel`;
- uses fresh maps, `component.NewRegistry`, and `storeregistry.New`;
- uses no NATS client, port, environment variable, package registry, random source, wall-clock deadline, or shared
  fixture;
- has no dependency on another test's mutation.

The package's integration-tag `TestMain` owns a shared NATS test client at `service/main_test.go:13-59`, but the failing
test never reads it. The test is compiled in ordinary and integration runs because its file has no build constraint.

Other `service` tests use `t.Parallel`, but Go pauses parallel tests while sequential tests run. No package-global
writer was found that can change this fixture's `BaseService`, ComponentManager maps, component atomics, or Manager.

Package globals found by:

```text
rg -n '^var \(|^var [A-Za-z_]|^func TestMain|init\(\)' service --glob '*.go'
```

Relevant results are limited to integration shared NATS handles in `service/main_test.go:14-17`, immutable/read-only
catalogs and metric label slices, OpenAPI handler registration, and `mandatoryServices` at
`service/service_manager.go:385`. None is read by the failing predicate in a mutable way.

CI runs:

```text
go test -race -failfast -tags=integration -timeout=20m -count=1 ./...
```

through `.github/workflows/ci.yml:91-106` and `scripts/run-integration-tests.sh:304-312`. Package parallelism is
explicitly uncapped. This supplies cross-package CPU contention even though the failing service test itself is
sequential. The `GOMAXPROCS=1` reproduction confirms that scheduler availability is sufficient to expose the failure.

### Sibling tests and existing synchronization conventions

Sibling readiness tests that use synthetic services initialize health synchronously:

- `TestStartupReadinessAndAtomicPromotion` uses `MockService{status: StatusRunning, healthy: true}`:
  `service/startup_observability_test.go:150-188`;
- `TestReadinessRequiresCommitAndClearsBeforeChildStop` does the same:
  `service/startup_observability_amendment_test.go:152-196`;
- `TestPreparedFullMuxStaysInvisibleUntilCommit` also uses a synchronously healthy mock:
  `service/startup_observability_amendment_test.go:198-240`.

The full StartAll startup test waits on causal service-entry channels and performs substantial listener, metrics, and
route work before its post-StartAll readiness assertion: `service/startup_observability_test.go:390-464`. It does not
isolate the BaseService first-health-check boundary.

Existing BaseService integration tests acknowledge asynchronous initial health:

- `waitForHealthy` polls until `IsHealthy` becomes true: `service/base_test.go:29-39`;
- lifecycle health tests wait before relying on the initial check: `service/base_test.go:111-141`;
- custom-check tests wait for an observed invocation: `service/base_test.go:241-253`.

The lifecycle ownership test uses explicit channels to observe that the first health check entered before proceeding:
`service/base_lifecycle_test.go:48-67`.

Storage observability tests directly invoke `performHealthCheck` before readiness assertions:
`service/storage_observability_test.go:284-322` and
`service/storage_observability_integration_test.go:253-280`.

The failing fixture is therefore the sibling exception: it uses real asynchronous BaseService health but treats
`ComponentManager.Start` return as proof that the initial health observation completed.

### Specifications, accepted design, history, and active changes

Current framework truth requires readiness to remain 503 until boot commitment and every admitted service and component
is currently healthy: `openspec/specs/framework-composition/spec.md:152-173`.

The current service-composition spec requires separate admitted/invoked/completed/failed counts and distinguishes
non-lifecycle Discoverables: `openspec/specs/service-composition/spec.md:244-257`.

The accepted #867 design amendment states the same readiness predicate: commitment true, all service and component
Starts complete, no Stop begun, and every admitted service and component currently healthy. Evidence:
`docs/proposals/gh867-startup-observability-design-amendment.md:105-134`.

The #867 conformance record says focused race, repository race, integration race, and core E2E were green:
`docs/proposals/gh867-startup-observability-conformance.md:53-97`. Those were single proof runs and did not include a
scheduler-constrained repetition of this exact test.

Operational documentation says TCP reachability is not readiness and callers must consume `/readyz` status:
`service/doc.go:112-149` and `docs/operations/migration-startup-observability.md:1-31`.

History:

- BaseService's asynchronous first health check dates to the initial commit `3361a8dc`; later lifecycle commits
  `61fbd48ff` and `269e0ac94` changed context and one-shot ownership but retained the asynchronous check.
- The failing test and current startup-readiness aggregation entered together in commit
  `451a06397a309426025d7ca06f27691e014462fa` (`feat(service): expose startup diagnostics before lifecycle`,
  2026-08-23).
- That commit is `v1.0.0-beta.161-99-g451a0639`, so the flake is post-beta.161 next-tag work.
- Issue #867 is closed and records direct in-memory manager readiness ownership. No existing issue names this specific
  health-initialization race.

Active OpenSpec changes do not claim this surface. Exact active-file enumeration found readiness mentions only as
explicit exclusions or unrelated graph/agentic readiness; none owns BaseService first-health observation.

## Same-class collision table

| Dimension | Existing owners and evidence |
|---|---|
| Semantic class | Process-local service/component startup completion plus current health, aggregated into `/readyz`. |
| Owners | BaseService owns service status/health (`service/base.go:82-105,191-228`); ComponentManager owns component lifecycle observations and direct child health (`service/component_manager.go:596-625,1191-1205`); Manager owns boot commitment and readiness aggregation (`service/service_manager.go:1776-1861`). |
| Catalogs | `Manager.serviceOutcomes` and sealed service identities (`service/service_manager.go:99-132,502-576`); `ComponentManager.components` and private runtimes (`service/component_manager.go:31-115`); registry declarations explicitly do not own runtime health/readiness (`component/registry.go:72-90`). |
| Status | BaseService `Status` and `healthy`; ComponentManager startup counts and component `HealthStatus`; Manager `startupSnapshot.Status`; `/readyz`, `/services.startup`, `/components/health`. |
| Lifecycle | BaseService publishes running, starts health monitoring asynchronously, and clears health on joined shutdown (`service/base.go:232-293,396-471`); ComponentManager completes child Start barrier before BaseService Start (`service/component_manager.go:338-397`); Manager commits only after fallible boot work (`service/service_manager.go:390-478`). |
| Ownership | Service and component health remain child observations; managers observe method outcomes and aggregate them. Runtime lifecycle truth is process-local and is not stored in NATS. |
| Readers | `/readyz` and `/services` (`service/service_manager.go:1776-1913`); component health HTTP (`service/component_manager_http.go:284-320`); health publishers and heartbeat; tests and external probes. |
| Writers | `BaseService.performHealthCheck` writes service health (`service/base.go:416-448`); concrete components write their own `HealthStatus`; managers write startup invocation/completion and commitment facts. |
| Recovery | BaseService repeats health checks at its configured interval after the immediate asynchronous check (`service/base.go:396-448`). A later successful check changes readiness without lifecycle mutation. There is no persisted readiness recovery record or replay. |

No new durable primitive, communication primitive, payload, subject, bucket, configuration key, or exported lifecycle
surface is present in this inventory.

## Consumer at birth

No new symbol or surface is proposed, so there is no new consumer-at-birth entry.

Current consumers are operators and orchestrators polling the existing `/readyz`, `Manager.currentStartupSnapshot`,
`/services.startup`, and tests proving startup commitment and health inclusion. Exact issue and repository searches
found no consumer of an initial-health-completed signal because no such signal exists.

## Context ownership audit

Touched production structs retain no `context.Context`:

- `BaseService` retains a private synchronized `context.CancelFunc` and join state only:
  `service/base.go:110-114`;
- ComponentManager retains private cancellation and join state only:
  `service/component_manager.go:82-115`;
- Manager retains private listener/publisher cancellation and join state only:
  `service/service_manager.go:54-77`.

`BaseService.Start` derives `runtimeCtx` from the exact caller context and passes it directly to both owned goroutines:
`service/base.go:232-281`. ComponentManager similarly derives its runtime child and passes it into components and
supervisors: `service/component_manager.go:338-397`.

Manager's HTTP `BaseContext` captures the exact derived server context: `service/service_manager.go:1159-1215`.

Exact touched-surface searches found no stored `context.Context`, no `context.TODO`, no production
`context.Background`, no `context.WithoutCancel`, and nil rejection before Start/Stop action at
`service/base.go:30-34,232-243`, `service/component_manager.go:338-347,646-650`, and
`service/service_manager.go:390-396`.

The test cleanup uses `context.Background` at `service/startup_observability_test.go:206`. It is test-only terminal
cleanup and is not part of the first-failure path.

## Adopter seam inventory

Specific adopter: an operator or product composition developer using `/readyz` without reading service internals.

1. **What must they know today?** `/readyz` is 200 only after boot commitment, completed Starts, no Stop observation,
   and current health for every admitted service and component. BaseService health initially remains false until its
   asynchronous first check runs, although service status is already running.
2. **What happens if they do nothing?** A wait-for-HTTP-200 probe remains safe and continues polling. A direct,
   immediate readiness read can observe 503 until the first health observation occurs. The deterministic proof shows
   this in the fixture; it has not yet shown a fully composed production process returning 503 after `StartAll`
   returns.
3. **Where do they find out?** The exact `READY`/`NOT READY` contract and current-health predicate are documented in
   current specs and operator docs. The asynchronous first-health timing is only visible in BaseService code and
   historical tests. There is no typed or operator-visible "initial health check completed" fact.
4. **What SHOULD they have to know?** Only the `/readyz` status. They should not need to predict goroutine scheduling,
   health-monitor startup latency, service count, or how much post-Start work gives an initial health goroutine time
   to run.
5. **Observation versus prediction:** the framework owns both health-check invocation and readiness aggregation. An
   adopter cannot reliably predict when the initial goroutine ran.

## Evidence-based first-failure hypotheses

### H1 — confirmed for the failing test

`BaseService.healthMonitor` has not executed its first `performHealthCheck` before the fixture probes readiness.

Evidence: health defaults false; status becomes running before the goroutine is launched; Start does not wait for the
first check; readiness reads `IsHealthy`; the non-lifecycle and lifecycle components are independently healthy; and
constraining scheduling with `GOMAXPROCS=1` changes the exact test from 100/100 passing to 100/100 failing.

### H2 — unsupported by current evidence: non-lifecycle Discoverable health is wrong

The plain Discoverable explicitly initializes health true at `service/startup_observability_test.go:76-79`. Its only
false write occurs after the first readiness assertion at `:222`. The lifecycle component's health is always true.

### H3 — unsupported by current evidence: startup counts or boot commitment are incomplete

The fixture synchronously waits for `ComponentManager.Start`, manually records the sole service Start as invoked and
completed, and commits before probing. The failure reaches the post-commit current-health portion of
`currentStartupSnapshot`.

### H4 — unsupported by current evidence: package-global contamination or test-order dependence

The fixture uses fresh owners and no shared mutable state. It is sequential. The deterministic `GOMAXPROCS` result
changes only scheduling opportunity.

### H5 — structurally possible but not yet demonstrated end-to-end: production post-StartAll readiness window

The same BaseService asynchronous-health mechanism exists in production and no happens-before edge connects initial
health completion to Manager commitment. Therefore a transient production 503 after commitment is structurally
possible.

The current reproduction manually assembles ComponentManager and manually commits Manager startup; it does not execute
the complete production `Manager.StartAll` path. The real path performs listener setup, service sequencing, route
construction, and health-publisher launch between child Starts and commitment, which may provide scheduling
opportunities but is not synchronization. Existing core E2E passed and supplies no negative proof because it waits for
readiness.

Classification at this checkpoint:

- **Proven:** test flake caused by an unobserved asynchronous initial health check.
- **Possible from code topology:** a production transient readiness window.
- **Not yet measured:** `/readyz` returning 503 after a real `Manager.StartAll` success on a minimal production-shaped
  composition.

## Searches closing empty categories

```text
rg -n 'TestReadinessIncludesHealthyNonLifecycleDiscoverables' .
```

One defining test only.

```text
gh issue list -R C360Studio/semstreams --state all \
  --search 'TestReadinessIncludesHealthyNonLifecycleDiscoverables'
```

No issue.

```text
rg -n 'context\.(Background|TODO|WithoutCancel)|context\.Context|CancelFunc' \
  service/base.go service/component_manager.go service/service_manager.go
```

Only operation parameters, allowed `http.Server.BaseContext` closures, and private `CancelFunc` fields; no retained
production context or invented root.

```text
rg -n 't\.Parallel\(|Setenv\(|slog\.SetDefault|prometheus\.Default|GOMAXPROCS' \
  service/startup_observability_test.go
```

No hit affecting the target test.

```text
rg -n 'readiness|startup|BaseService|health monitor|initial health' \
  openspec/changes --glob '!archive/**' --glob '*.md'
```

No active change claims the BaseService initial-health/readiness seam.

## Open evidence questions

- Does a minimal production-shaped `Manager.StartAll` composition expose the same post-success 503 under deterministic
  scheduler constraint, or does some existing production acquisition create an incidental scheduling point?
- Is BaseService Start intended to promise only lifecycle acquisition, or also one completed current-health
  observation? Current specs constrain `/readyz`, not this exact boundary.
- Is the inventory scope test-only once the production-shaped measurement is made, or does it reveal an outward
  readiness timing defect?
- Which exact caller should own any future deterministic proof: BaseService lifecycle, ComponentManager startup, or
  Manager readiness? That is a design-phase question and is not ruled here.

This is the mandatory inventory-only checkpoint. It contains no target state, options, recommendation, or artifact
delta.
