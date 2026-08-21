# Metrics HTTP owner inventory

## Checkpoint authority

This is an inventory-only checkpoint for selecting one post-BaseService lifecycle owner. It records measured current
state and open findings; it contains no target-state option, recommendation, proposed API signature, binding ruling,
or implementation approval.

Baseline: clean merged `main` at `269e0ac94b28c6f6162d8f5d144ca545b393df85`.

The live production census at that baseline is:

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 36 |
| `lifecyclejoin.NewGeneration` | 38 |
| Calls on `Generation.Stop` | 43 |
| External `Generation.Cancel` | 4 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| Calls on lifecycle `Operation.Run` | 3 |
| External old-symbol rollback calls | 20 |

Implementation Gate A and task 2.3 remain incomplete. This inventory grants no owner-migrated, Gate, proof, release,
archive, or tag credit.

## Problem framing

The next owner must be selected from current repository evidence after the BaseService migration. The historical
S/F labels in `inventory.md` are hypotheses to retest, not authority for the current native-handle order. The likely
surface is the Metrics service and its concrete HTTP provider because the service wrapper cannot complete shutdown
independently of the provider's listener and Serve completion.

## Current Metrics service surface

- `service/metrics.go:18-35` embeds the now-one-shot `BaseService`. It retains a `metricsServer`, a generic
  `Generation`, `sync.Once`, and a retained teardown result.
- `service/metrics.go:37-53` declares the exported port/path configuration and validation.
- `service/metrics.go:55-104` exports the constructor, applies defaults, reads sealed security configuration, creates
  the BaseService, and installs the health check.
- `service/metrics.go:106-155` derives a second runtime lifetime, starts BaseService, synchronously constructs and
  binds `metric.Server`, and invokes bounded BaseService rollback after a bind failure.
- `service/metrics.go:157-189` delegates Stop to `Generation.Stop`. Its once-only cleanup calls the provider's
  context-free Stop while holding the service lifecycle mutex, retains any provider error, then stops BaseService.
- `service/metrics.go:191-247` exposes health, port, path, URL, and configuration-schema surfaces. These are adjacent
  endpoint/configuration facts, not lifecycle authority.

The wrapper's lifecycle mechanics come from two shared helpers:

- `internal/lifecyclejoin/generation.go:13-45,96-153` stores cancel, completion, once-only operations, and retained
  results. Stop permits a later caller to resume an expired operation and replay its result.
- `internal/lifecyclejoin/rollback.go:8-19` supplies the single bounded failed-Start rollback root.

## Current metric.Server provider surface

- `metric/handler.go:18-28` retains `*http.Server`, the exact `net.Listener`, a buffered `serveDone`, registry,
  security configuration, and a mutex over lifecycle fields.
- `metric/handler.go:30-45` exports `NewServer` and applies the provider's port/path defaults.
- `metric/handler.go:47-115` validates the provider, builds the Prometheus, health, and root handlers, and configures
  the HTTP/TLS server.
- `metric/handler.go:116-136` binds before publishing the listener and `serveDone`, then starts exactly one Serve
  goroutine. Start therefore reports listener ownership or bind failure synchronously.
- `metric/handler.go:138-167` holds the provider mutex, closes the exact listener and HTTP server, waits for the exact
  `serveDone`, translates terminal errors, and clears all three handles.
- `metric/handler.go:169-176` exposes the address derived from security, port, and path configuration.

`metric.Server` is the concrete owner of HTTP admission and Serve completion. `service.Metrics` is the lifecycle and
status wrapper that constructs it and is itself stopped by outer service composition.

## Composition and public catalog

- `service/register.go:6-27` registers `NewMetrics` under the built-in `metrics` service name.
- `service/service_configs.go:66-96` materializes metrics as default-on unless explicitly configured otherwise, with
  default path `/metrics` and port `9090`.
- `service/base.go:480-489` gives outer managers the public `Service.Stop(context.Context)` boundary and requires
  exact completed-repeat behavior rather than treating `StatusStopping` as completion.
- `service/metrics.go:205-247` makes port, path, URL, and schema visible to in-repository composition and config
  consumers.

## Current test and documentation claims

### Provider tests

- `metric/handler_test.go:14-46` proves synchronous listener ownership, listener closure before Stop returns, and
  same-provider-instance restart.
- `metric/handler_test.go:48-76` blocks listener Close and proves Start waits behind the provider mutex until Stop has
  closed the listener and joined Serve.

Neither provider test places a caller-owned time bound around the `serveDone` wait.

### Service tests

- `service/metrics_lifecycle_test.go:13-29` proves that an occupied port is reported by Start and BaseService reaches
  stopped state after rollback.
- `service/lifecycle_context_contract_test.go:21-30,70-87` injects provider Stop failure and requires the current
  once-only teardown error to be retained and replayed.
- `service/lifecycle_integration_test.go:23-65` requires repeated Metrics Stop to be safe and double Start to fail.
- `service/lifecycle_integration_test.go:121-165` requires concurrent Stop result sharing. That is a repudiated generic
  lifecycle behavior under the recovery ledger, not positive target evidence.

### Adopter documentation

- `metric/doc.go:21-32,145-163,340-352` shows direct `metric.NewServer`, `Start()`, and `Stop()` use.
- `metric/README.md:40-55,177-188` publishes the same direct lifecycle API and says Stop closes the listener and waits
  for serving to exit.
- `docs/operations/migration-beta159-to-beta160.md:158-174` documents synchronous bind as a prior provider behavior
  change.

These examples make `metric.Server` an exported Go surface even though no current sister repository directly calls
its lifecycle methods.

## Historical classification recheck

### Metrics HTTP

`inventory.md:102` historically labels `service/metrics.go` as F with “base service and exporter cleanup.” Current
evidence makes that label incomplete:

- the provider has a concrete HTTP listener, HTTP server, and Serve completion at `metric/handler.go:116-166`;
- the service starts BaseService before bind and rolls it back after bind failure at `service/metrics.go:111-136`.

The measured current classification is P-primary for the concrete HTTP close/Serve protocol, with an F facet for
partial-Start rollback. This is a classification finding, not a migration design.

### Graph request providers

The graph query group retains exact NATS request-subscription handles and unsubscribes them during Stop. These are Q
owners, not alternatives for an owner-local S slice:

- graph query: `processor/graph-query/component.go:194,543-550` and
  `processor/graph-query/query.go:83-87`;
- clustering: `processor/graph-clustering/component.go:656,1083-1091` and
  `processor/graph-clustering/query.go:19-45`;
- embedding: `processor/graph-embedding/component.go:322,739-747` and
  `processor/graph-embedding/query.go:19-39`;
- spatial index: `processor/graph-index-spatial/component.go:199,534-541` and
  `processor/graph-index-spatial/query.go:22-34`;
- temporal index: `processor/graph-index-temporal/component.go:208,554-561` and
  `processor/graph-index-temporal/query.go:17-22`.

### Example, JSON, and research subscribers

These groups retain core or JetStream subscriptions and drain or stop them. Their measured class is Q with an F
facet. Cases that stop JetStream consumers only by error/name remain dependent on task 2.1 native-handle work.

- document example: `examples/processors/document/component.go:88-89,211-217,313,335-339,433`;
- IoT example: `examples/processors/iot_sensor/component.go:88-89,211-217,313,335-339,433`;
- weather example: `examples/processors/weather_station/component.go:71-72,176-182,229,242-248`;
- JSON filter: `processor/json_filter/json_filter.go:88-89,219-225,428,450-456`;
- JSON generic: `processor/json_generic/json_generic.go:76-77,196-202,385,407-413`;
- JSON map: `processor/json_map/json_map.go:95-96,237-243,430,452-458`;
- research assess: `processor/research-graph-assess/component.go:62-64,153-161,279-289`;
- research classify: `processor/research-graph-classify/component.go:72-75,216-224,352-362`;
- research execute: `processor/research-graph-execute/component.go:55-57,191-199,281-291`;
- research route: `processor/research-graph-route/component.go:61-64,159-167,301-311`;
- research synthesize: `processor/research-graph-synthesize/component.go:51-53,137-145,257-267`.

### Rule and service owners

- Rule retains core and JetStream subscriptions, KV watchers, a production context field, and failed-Start authority
  at `processor/rule/processor.go:143-164,918-983,1034-1144,1181-1260`. Its measured obligations are Q/M/F.
- MessageLogger owns dynamic subscriptions, retry, cancellation, and Start-finalization paths at
  `service/message_logger.go:192,229-233,434-475,544-623,628-706`. Its measured obligations are Q/M.
- Output file retains core and JetStream subscriptions at
  `output/file/file.go:117-118,265-267,313,367,412-433`. It is Q-primary with an F facet.
- HTTP post retains core and JetStream subscriptions at
  `output/httppost/httppost.go:111-112,268,306,360,409-429`. It is Q-primary with an F facet.
- `recovery-ledger.md:254-261` already records the output-owner correction and grants those owners no migration
  credit.

This comparison closes the apparent “simple adjacent owner” gap: every compared remaining group has a concrete
protocol, native-handle, Start-finalization, or failed-Start obligation that must be inventoried with its owner.

## Context and root inventory

The production search over `service/metrics.go` and `metric/handler.go` found:

- operation context parameters only on `Metrics.Start` and `Metrics.Stop`;
- one `context.WithCancel(ctx)` derivation in `Metrics.Start` at `service/metrics.go:107-112`;
- no `context.Context` field in either `Metrics` or `metric.Server`;
- no `context.Background`, `context.TODO`, or `context.WithoutCancel` in either file;
- no context use at all in `metric.Server`.

The only relevant invented root is the already-approved, bounded failed-Start helper at
`internal/lifecyclejoin/rollback.go:13-19`. The inventory records it as existing framework authority, not precedent
for another root or for unbounded cleanup.

## Adopter seam inventory

The adopter is either a config author/Prometheus reader or a Go developer who directly uses the exported
`metric.Server`.

### What must they know now?

- Config authors supply only port and path; defaults are `9090` and `/metrics`.
- Prometheus readers use the configured metrics path; health readers use `/health`.
- Direct Go users call `NewServer`, `Start`, and `Stop`, and current Stop has no caller-provided bound.

The architect's sister-repository scan found zero direct calls to `metric.NewServer`, `service.NewMetrics`, or a
metrics-server Stop. Sister imports of `semstreams/metric` use registries and collectors only. In-repository direct
provider consumers are the Metrics service, provider tests, and the cited documentation examples.

### What happens if they do nothing?

Current config and scrape behavior is unchanged by this inventory. The direct Go API remains the only seam that a
later provider lifecycle design could change.

Conditionally, if a later approved design makes exact provider Stop context-taking, a direct Go adopter that does
nothing would receive a compile error and would supply its existing shutdown context. This inventory does not propose
that signature; it records the adopter effect of that possible design choice.

### Where do they find out?

For the conditional Go surface change, the compiler is the first discovery point and the metric package docs are the
second. Config and HTTP readers continue to discover port/path through service configuration and the published docs.

### What should they have to know?

A direct Go adopter should know only its shutdown budget. It should not predict Serve completion, listener identity,
framework status, a generation, a retained result, or any internal lifecycle state. Config authors and Prometheus
readers should learn no new lifecycle fact.

## Same-class collision table

| Dimension | Current evidence |
|---|---|
| Semantic class | Metrics HTTP listener admission and Serve completion. |
| Owners | Provider: `metric/handler.go:18-167`. |
| Owners | Service wrapper: `service/metrics.go:18-189`. |
| Owners | Outer composition calls the wrapper through `Service.Stop(ctx)`. |
| Catalogs | Built-in service entry: `service/register.go:6-27`. |
| Catalogs | Default activation/config: `service/service_configs.go:66-96`. |
| Status | BaseService lifecycle/health plus provider `/health`. |
| Lifecycle | Wrapper Generation surrounds provider Start/Stop and BaseService. |
| Lifecycle | Provider binds, serves, closes exact handles, then awaits `serveDone`. |
| Ownership | One Metrics instance retains one Server; one Server retains one listener. |
| Ownership | OS bind rejects duplicate ownership of the configured port. |
| Readers | Prometheus and health clients; existing E2E readers are unchanged. |
| Writers | Config authors and `NewMetrics`/`NewServer` constructors only. |
| Recovery | No durable state; a fresh service/process rebinds the listener. |

No new communication path, orchestration behavior, payload type, or query operation is proposed. The shared
`kv-or-stream`, `orchestration-check`, `new-payload`, and `query-pattern` skills therefore do not trigger.

## Open evidence finding

`metric.Server.Stop` closes its exact handles and then waits on `<-serveDone` with no caller context at
`metric/handler.go:138-166`. If Serve completion does not arrive, this wait is unbounded. The outer
`service.Metrics.Stop(ctx)` invokes that provider wait inside its cleanup, but its caller context cannot interrupt the
provider's context-free wait.

The active shutdown requirement at `specs/restart-safe-shutdown/spec.md:3-19` requires native shutdown and exact
completion to remain bounded by the shutdown context. The provider seam therefore must be handled in the same
coherent owner slice or as its first prerequisite. A service-only migration would leave the required bound unproven
and is incomplete.

This finding defines a proof gap, not a target API. The inventory leaves the provider method shape, owner-local state,
test changes, and migration order for post-inventory design review.

## Review gate

This exact inventory requires independent `INVENTORY PASS` before any target state, options, recommendation, artifact
delta, implementation task, or runtime change is proposed or approved.
