# GH-867 startup observability design amendment

Authority: accepted inventory SHA `d594173ab170cb53d8496f048e50cbe2ec35357d61d63ccfe5955cc5fbd1e100`; prior design SHA `f5dae1b44f0f5aa26235d8c2136ec8f839048ebfd715293b10c25e009285ff8d`.

This amendment supersedes the prior design’s “Metrics-first service” exception. All unaffected inventory, compatibility, and fail-closed rulings remain in force.

## Inventory delta

The accepted problem inventory remains valid. The uncommitted implementation adds four facts that the amendment must reconcile before defining target state:

1. `service/service_manager.go` adds `lifecycleOrder` and moves concrete built-in `Metrics` ahead of registration order. This collides with reverse-registration shutdown required by `openspec/specs/service-shutdown/spec.md:19`, `openspec/specs/runtime-context-ownership/spec.md:13-25`, `service/doc.go:191`, and `service/README.md:233`.
2. The implementation records final service Start completion before full-route construction and health-publisher acquisition. `/readyz` can therefore report ready while the startup mux is active. Conversely, full routes are promoted before the fallible publisher start. Stop invocation is recorded but readiness ignores it, allowing teardown to remain ready while a child Stop blocks.
3. `ComponentManager` creates runtime records before launch and derives `starts_invoked` from runtime existence. This predicts invocation before the child call. Component counts are not published before the early scrape listener binds, and concurrent completion paths can publish an older snapshot after a newer one.
4. The current concurrency test permits either startup or final responses but does not causally prove the commit boundary. The shutdown test manually injects `lifecycleOrder`, so it does not exercise production planning.

The existing Metrics surface is already separable without a new public abstraction:

- `metric.MetricsRegistry` exists before service composition in `cmd/semstreams/main.go:119`.
- `metric.Server` already owns synchronous Prometheus bind, request `BaseContext`, listener, and bounded Stop/join in `metric/handler.go`.
- `service.Metrics` currently combines configured port/path/security, standalone service lifecycle, and creation/ownership of `metric.Server` in `service/metrics.go`.
- `Manager` already receives the same registry and constructs the configured built-in Metrics instance before seal.

No additional adopter or sister-repository surface was found. The existing `services.metrics` spelling, `NewMetrics`, `Port`, `Path`, `URL`, registry, and exact readiness text remain compatibility constraints.

## Decision-skill result

`orchestration-check` applies because startup, commit, and rollback ordering are changing. The result is process composition owned by the existing managers. This is not a rule, workflow, Lifecycle-harness entity, component, supervisor, or durable phase model. State ownership stays exclusive:

- ServiceManager observes service calls and owns boot commitment and diagnostic listeners.
- ComponentManager observes component calls.
- Children own current Health.
- Readiness derives those facts without storing a second authority.

No `kv-or-stream`, `new-payload`, or `query-pattern` decision is triggered. There is no new communication primitive, payload, or query front door.

## Options

| Option | Cost and result | Disposition |
|---|---|---|
| Do nothing | Leaves shared HTTP and Prometheus dark during a blocked Start. | Reject. |
| Keep Metrics-first lifecycle ordering | Makes observability a lifecycle dependency and changes legitimate start/stop order. | Rejected by owner. |
| Remove `metrics` from the service catalog and add new Manager config | Clean ownership but breaks existing config, runtime rows, direct construction, and schemas. | Reject for #867. |
| Bind a second hard-coded Prometheus server | Duplicates config and risks two owners of one port/registry. | Reject. |
| **Manager privately claims the configured built-in Metrics listener while preserving the ordinary Metrics compatibility service** | No new public/config surface; listener availability is outside lifecycle; service registration order remains authoritative. | Recommend. |

## Amended target contract

### Manager-owned boot diagnostic plane

After mandatory construction and seal, but before any Service `Start`, ServiceManager performs these steps:

1. Freeze the startup middleware chain and construct the complete diagnostic mux.
2. Initialize all nine startup metric series from the sealed service set and the already-initialized concrete ComponentManager admitted set.
3. Bind the shared HTTP listener.
4. If the configured built-in Metrics service exists, bind its Prometheus listener using its existing port, path, registry, security, and `metric.Server`.
5. Only after both required binds succeed may the ordinary service loop begin.

The diagnostic plane is private Manager infrastructure. It is not a Service, is absent from registration order, and adds no priority, phase, lifecycle interface, timeout, or configuration knob. Disabled or absent Metrics remains absent; #867 does not make Prometheus mandatory.

Binding is transactional. Shared-bind failure starts no child. Prometheus-bind failure closes the already-bound shared listener and starts no child. A successfully bound diagnostic listener remains observable through later Start failure rollback and is closed only after child cleanup has been attempted.

### Private Metrics split and compatibility

`metric.Server` remains unchanged and continues to own its listener and serving goroutine. ServiceManager owns the production instance used by the boot diagnostic plane.

`service.Metrics` gains only a private Manager-claim/adoption seam:

- Manager claims one freshly constructed server from the concrete built-in Metrics instance before lifecycle begins.
- A claimed Metrics service does not bind or stop that server from its ordinary `Start` or `Stop`; those methods retain BaseService compatibility and execute in original registration order.
- Metrics health reads a private process-local availability witness owned by Manager; it receives no context, cancel function, or listener authority.
- Manager alone stops the claimed server after service rollback/shutdown.
- A directly constructed Metrics service not claimed by a Manager retains its existing standalone Start/Stop behavior.

Thus the configured `metrics` identity, runtime row, schema, `Port`/`Path`/`URL`, and direct constructor behavior remain compatible, while production scrape availability no longer depends on or orders service lifecycle.

### Observed outcomes and metrics

ServiceManager retains one private current-boot outcome per sealed service:

- Start invoked, completed, and error;
- Stop invoked, completed, and error.

It records invocation immediately before the actual call and completion immediately after return. It starts services in original registration order and stops them in exact reverse registration order. Delete `lifecycleOrder`; no concrete service receives ordering privilege.

ComponentManager adds `startInvoked` to each private runtime. Creating a runtime or scheduling a goroutine does not count as invocation. The launch goroutine records invocation at the immediate child-call boundary, then records completion/error after return.

Component startup snapshot and gauge publication use one unexported `service.startupMetricWriter`, constructed and owned by ServiceManager. Its constructor creates a fresh private `prometheus.GaugeVec` and registers that exact collector once through the existing `metric.MetricsRegistry.PrometheusRegistry().Register` boundary. It does not use `RegisterOrGetGaugeVec`, does not enter `registeredMetrics`, and never adopts or shares a compatible preexisting collector. Any registration error—including `prometheus.AlreadyRegisteredError` or descriptor conflict from a same-name collector with different labels—is fatal before any diagnostic listener binds or child Start runs.

The successful writer retains the sole private GaugeVec pointer. ServiceManager privately supplies that writer only to the concrete ComponentManager; no component, ordinary service, or other package receives the collector. The writer mutex encloses invocation of the owning manager’s snapshot function and all corresponding gauge writes. ComponentManager remains sole owner of component observations, ServiceManager remains sole owner of service observations, and an older concurrent snapshot cannot overwrite a newer one.

Before Prometheus binds, the writer initializes and writes exactly four service and five component pairs:

`semstreams_startup_units{owner,stage}`

The component values come from the already-initialized concrete ComponentManager, so the first scrape reports actual admitted/participant counts with invoked/completed zero. The private writer accepts only the fixed owner/stage vocabulary and emits no unit-name label. Remove the uncommitted exported `metric.Metrics.RecordStartupUnits`; `/readyz` never scrapes Prometheus.

### Boot commitment and readiness

ServiceManager owns one private atomic boot-commit fact and a private stopping observation. Commitment begins false. A private `beginStopping(firstService)` operation holds the Manager lock while it marks stopping, clears commitment, and—when a first service exists—records that first Stop invocation; it then releases the lock before calling child code. Subsequent Stop observations use the same Manager-owned outcome records. Readiness therefore cannot observe commitment after teardown has become observable.

The shared dispatcher retains a diagnostic mux and a separately prepared full mux. While commitment is false it always dispatches through the diagnostic mux, regardless of whether a full mux has been constructed. Ordinary routes therefore cannot leak before commit.

Successful boot order is:

1. bind both diagnostic listeners;
2. invoke all Services in original registration order;
3. build the complete mux off-path;
4. start every remaining fallible Manager-owned runtime needed for successful boot, including the health publisher;
5. store the complete mux;
6. atomically set boot commitment true as the final non-failing transition;
7. return success.

No fallible boot operation follows commitment. A publisher or route-build failure leaves commitment false, exposes no ordinary route, and enters rollback.

`/readyz` returns 200 exact `READY` only if:

- boot commitment is true;
- every admitted Service Start completed successfully;
- every admitted ComponentManager lifecycle-participant Start completed successfully;
- no Service Stop has been invoked;
- no ComponentManager Stop has begun;
- every admitted service and component currently reports healthy.

It returns 503 exact `NOT READY` otherwise. Commitment prevents vacuous pre-Start readiness and closes the windows between final child completion, route construction, publisher acquisition, rollback, and teardown. A boot with zero lifecycle components may be ready after successful commitment; mandatory service composition remains nonempty.

`currentStartupSnapshot` reports:

- `failed` after any Start error;
- `stopping` after commitment is cleared for Stop or a Stop is invoked;
- `starting` while invocations are outstanding;
- `not_ready` when starts completed but commitment/current health is false;
- `ready` only when the readiness predicate is true.

When completion or commitment is already false, readiness short-circuits without calling child Health merely to rediscover false.

### Route and middleware concurrency

Startup and full muxes are each fully constructed before they become request-visible. No handler is registered on a mux after that mux is served.

The single server Handler remains the preconfigured middleware chain around the Manager dispatcher. `UseHTTPMiddleware` remains pre-StartAll and is rejected after listener acquisition. Product middleware therefore applies consistently to startup diagnostics and committed routes.

The dispatcher’s commitment check is the route gate:

- false: diagnostic mux only; unknown/ordinary paths return 503 `NOT READY`;
- true: complete mux only.

During Stop, `beginStopping` pairs commitment clearing with the first Stop observation under the Manager lock before any child call. During rollback, commitment is already false and the same stopping observation is established before cleanup calls. Requests immediately use the diagnostic-only view, `/readyz` is not-ready, and listeners remain available until owner cleanup reaches them.

### Failure, cleanup, retry, and context

Provider-first and consumer-second component barriers remain unchanged. #867 adds no timeout, detached Start, degraded continuation, or production-root path that calls `Manager.StopAll` concurrently with an in-progress `Manager.StartAll`. Existing owner-local ComponentManager and standalone Metrics Start/Stop fencing, waiting, and race behavior remain unchanged.

Failed boot cleanup order is:

1. clear commitment;
2. stop registered Services in reverse registration order under existing failed-Start rules;
3. stop/join the health publisher if acquired;
4. stop/join shared HTTP and the Manager-owned Prometheus server.

Normal Stop uses the same ownership order after first clearing commitment. Diagnostics are owner resources acquired before children and therefore released after children.

Each diagnostic listener is one-shot, and failed bind or failed Start never rebinds the same Manager. Shared HTTP retains the existing Manager cleanup-mode contract: failed-start cleanup that cannot finish retains its exact listener/server authority for a later `StopAll` retry. The Manager-owned Prometheus listener follows the existing unchanged `metric.Server.Stop` contract instead: `Stop` consumes and clears that server’s authority even when it returns an error, so Manager joins and reports the error but does not retry that same `metric.Server`. Repeated cleanup after either owner reaches terminal state is idempotent.

Both HTTP servers receive the exact `StartAll` context through `http.Server.BaseContext`. Production structs retain no `context.Context`; only private cancellation and join authority is retained. Existing caller-bounded shutdown and `metric.Server` forced-join behavior remain unchanged. No new timeout is introduced.

#1020 remains unchanged: the production process passes live `runtimeCtx` to `StartAll`. Signal observation does not cancel a blocked runtime Start or cause concurrent Stop.

## Specification and documentation correction

Owner ruling authorizes direct current-truth correction with no new OpenSpec change directory, lifecycle capability, or ADR.

Amend `framework-composition` to state:

- the Manager-owned shared HTTP and configured Prometheus diagnostic plane binds after seal and before lifecycle Starts;
- diagnostics remain not-ready until boot commitment;
- ordinary routes remain gated until the final commit;
- failed boot and Stop clear commitment before cleanup;
- diagnostic binding is not service lifecycle participation.

Retain the additive `/services.startup` projection in `service-composition`. Clarify that startup diagnostic binding is Manager infrastructure, not a service-contributed route or service-order exception.

Do not modify the reverse-registration requirements in `service-shutdown` or `runtime-context-ownership`; implementation must conform to them.

Correct `service/doc.go`, `service/README.md`, metric docs, middleware/local-monitoring docs, and the migration note to remove every claim that Metrics starts first or owns the production listener. State that the configured scrape listener is Manager-owned during composed production boot and that TCP reachability is not readiness.

The current conformance document is invalid because it claims Metrics-first conformance and no deviations. It must be regenerated only after implementation and review against this amendment.

## Adopter seam

Product composition developers receive no new method, config, ordering rule, priority, timeout, subject, bucket, or callback.

If they do nothing:

- existing `services.metrics` enablement, port, path, security, service identity, and standalone constructor behavior remain;
- production shared HTTP and configured Prometheus ports bind earlier;
- middleware still must be installed before `StartAll`;
- service Start order and reverse-registration Stop order remain unchanged;
- ordinary routes remain unavailable until successful commitment.

Operators must use `/readyz` status, not TCP reachability. Exact bodies remain `READY` and `NOT READY`. Startup counts remain additive on `/services` and Prometheus.

Component authors have no new obligation. Slow Start remains fail-closed and untimed; managers observe the actual call rather than requiring components to publish lifecycle facts.

No sister-repository write is authorized. Sister owners validate the earlier listener timing in their own roots.

## Exact implementation and test scope

Production:

- `service/service_manager.go`: delete special lifecycle order; add commitment gate, Manager-owned Prometheus handle/claim, original-order observations, final commit, and cleanup ordering.
- `service/metrics.go`: private Manager claim/adoption and availability witness; preserve standalone behavior.
- `service/component_manager.go`: actual invocation fact, serialized snapshot/publication, pre-bind initial publication, and stop-started observation.
- `service/component_manager_http.go`: retain read-only startup route subset.
- `service/startup_metrics.go` (private): Manager-owned GaugeVec construction, strict one-time registration through the existing Prometheus registry accessor, fatal preclaim rejection, exactly nine initialized pairs, and serialized snapshot-plus-write operations.
- `metric/core.go`, `metric/registry.go`: remove the uncommitted startup collector and exported `RecordStartupUnits`; retain existing registry primitives unchanged.
- `cmd/semstreams/main.go`: retain corrected non-readiness bootstrap log.

Deterministic TDD:

1. Before any Service Start, scrape real configured Prometheus and prove nonzero initialized component admitted/participant counts with invoked/completed zero.
2. Separate component preparation from invocation deterministically; prove a prepared but not-called child is not invoked, then prove entry into its actual `Start` increments invoked.
3. Force reverse completion order and prove serialized publication never regresses completed/failed gauges.
4. Gather immediately after private writer initialization and prove exactly nine fixed series. Pre-register same-name collectors in two subtests—one descriptor-compatible and one with an extra label—then prove writer construction fails fatally, no foreign collector is adopted, no diagnostic listener binds, and no child Start runs. Add a source/package-boundary assertion that `metric.Metrics` exposes no `RecordStartupUnits` method and the only production holder/writer of the startup GaugeVec is the unexported Manager-owned writer in `service`.
5. Gate full-mux construction and force health-publisher failure; in both cases prove commitment false, `/readyz` 503, ordinary routes unavailable, and synchronous cleanup.
6. Complete all fallible work, commit, and prove exact `READY` plus full routes. Use causal channels around commit rather than an unconstrained request loop.
7. Block a child Stop before it changes self-status; prove commitment is already false and `/readyz` is 503 while diagnostics remain reachable.
8. Register services including built-in Metrics in a non-first position; prove lifecycle Starts follow registration order and Stops derive reverse registration order from production behavior. Do not inject an order field.
9. Hold the first ordinary service Start and prove both listeners are already bound, a later service has not started, and Prometheus reports the held progress.
10. Occupy the metrics port; prove no Service starts and the partially acquired shared listener is released.
11. Run focused `-race` service/metric/cmd tests and the existing integration failure path.

`task e2e:core` remains the one relevant final tier because it exercises the real production composition, both ports, readiness, SIGTERM cleanup, and normal startup metric values. It complements but does not replace the deterministic held-start and ordering tests. No agentic, semantic, or other E2E tier is relevant.

## Disposition of current uncommitted work

Keep as the basis:

- corrected bootstrap log in `cmd/semstreams/main.go`;
- early diagnostic route subset and off-path route-helper changes;
- manager/component outcome records and additive `/services.startup` shape;
- the fixed low-cardinality metric contract, reworked from `metric.Metrics` into the private Manager-owned `service.startupMetricWriter`;
- early-bind listener lifecycle/context tests;
- integration expectation that failed boot briefly has diagnostics but never promotes routes;
- middleware timing/TCP warning;
- normal-boot `e2e:core` metric assertion;
- accepted inventory unchanged.

Surgically revert:

- `lifecycleOrder`;
- concrete Metrics detection/reordering in `sealComposition`;
- every “Metrics starts first,” “only special early service,” and “reverse planned lifecycle order” claim;
- tests that manually inject `lifecycleOrder` or assert Metrics-first sequencing.

Rework:

- ServiceManager readiness, promotion, publisher ordering, rollback, and Stop gating around explicit commitment;
- ComponentManager invocation accounting and serialized metric publication;
- held-start test so Prometheus is Manager-bound rather than Metrics-Start-bound;
- promotion concurrency test into a causal commit test;
- storage-readiness test to establish commitment rather than only synthetic successful outcomes;
- framework/service specs and all service/metric/migration documentation;
- `docs/proposals/gh867-startup-observability-design.md` to reference this superseding amendment;
- `docs/proposals/gh867-startup-observability-conformance.md` completely after implementation.

No whole-file discard is required. No production, documentation, GitHub, or sister-repository mutation was performed by the architect.
