# Service listener inventory (#1120)
base: 50ff16eb8bc89bac520713531c8c70df1676cf2d

Inventory-only checkpoint; independent INVENTORY PASS pending. No target, recommendation, or scope ruling.
Started from `/private/tmp/semstreams-1120-service-port-inventory.md` at353b82a2.
`service/` and `metric/` have no diff between that baseline and this checkpoint.
The original18 calls are confirmed; the same listener owners have9 additional calls through two helpers.
This inventory includes all27 calls for repair assessment. Adjacent transport helpers are recorded separately.
Read the project, current framework-composition/service-shutdown/runtime-context-ownership specs, ADR-058,
ADR-094, and the complete active proposal. No tests or repository mutations were performed.

## Claimed gap and complete consumer census

Each helper binds loopback port0, observes its number, closes its reservation, and returns the number.
The caller cannot rely on that number remaining available. Paired allocations need not return distinct numbers.

- `service/service_manager_health_listener_test.go:277` — `func freePort(t *testing.T) int {`
- `service/service_manager_health_listener_test.go:279` — `l, err := net.Listen("tcp", "127.0.0.1:0")`
- `service/service_manager_health_listener_test.go:284` — `_ = l.Close()`
- `service/metrics_owner_test.go:216` — `func freeMetricsPort(t *testing.T) int {`
- `service/metrics_owner_test.go:218` — `listener, err := net.Listen("tcp", "127.0.0.1:0")`
- `service/metrics_owner_test.go:221` — `require.NoError(t, listener.Close())`
- `metric/handler_test.go:190` — `func freeServerPort(t *testing.T) int {`
- `metric/handler_test.go:192` — `listener, err := net.Listen("tcp", "127.0.0.1:0")`
- `metric/handler_test.go:195` — `require.NoError(t, listener.Close())`

The18 original calls cover health, shared HTTP, startup metrics, pprof, and refusal/no-op assertions:

- `service/pprof_test.go:19` — `port := freePort(t)`
- `service/pprof_test.go:36` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:17` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:37` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:65` — `require.NoError(t, healthManager.StartHealthListener(startCtx, freePort(t)))`
- `service/service_manager_health_listener_test.go:81` — `require.ErrorIs(t, healthManager.StartHealthListener(canceled, freePort(t)), context.Canceled)`
- `service/service_manager_health_listener_test.go:100` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:202` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:206` — `err := manager.StartHealthListener(t.Context(), freePort(t))`
- `service/service_manager_health_listener_test.go:225` — `port := freePort(t)`
- `service/service_manager_health_listener_test.go:247` — `port := freePort(t)`
- `service/startup_observability_amendment_test.go:316` — `httpPort := freePort(t)`
- `service/startup_observability_amendment_test.go:317` — `metricsPort := freePort(t)`
- `service/startup_observability_amendment_test.go:370` — `httpPort := freePort(t)`
- `service/startup_observability_amendment_test.go:371` — `metricsPort := freePort(t)`
- `service/startup_observability_test.go:465` — `httpPort := freePort(t)`
- `service/startup_observability_test.go:466` — `metricsPort := freePort(t)`
- `service/startup_observability_test.go:595` — `httpPort := freePort(t)`

The9 additional calls exercise standalone Metrics/provider ownership and concrete metric.Server lifecycle:

- `service/metrics_owner_test.go:101` — `port := freeMetricsPort(t)`
- `service/metrics_owner_test.go:125` — `port := freeMetricsPort(t)`
- `service/metrics_owner_test.go:197` — `raw, err := json.Marshal(MetricsConfig{Port: freeMetricsPort(t), Path: "/metrics"})`
- `metric/handler_test.go:44` — `port := freeServerPort(t)`
- `metric/handler_test.go:74` — `server := NewServer(freeServerPort(t), "/metrics", registry, security.Config{})`
- `metric/handler_test.go:112` — `server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})`
- `metric/handler_test.go:120` — `server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})`
- `metric/handler_test.go:130` — `server := NewServer(freeServerPort(t), "/metrics", registry, security.Config{})`
- `metric/handler_test.go:174` — `server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})`

## Current spellings, usable seams, and owners

| Owner | Acquisition/current seam | Serving and release | Defaults/limitations |
|---|---|---|---|
| Manager shared HTTP | Direct net.Listen; test constructor assigns config without normalization; retained httpListener observable in-package | Same listener passed to Serve; stopHTTPRuntimeMode closes/joins exact owner | ConfigureFromServices changes0→8080; test constructor can preserve0 |
| Manager health | Public StartHealthListener takes context and integer port; no listener argument | Retains healthListener/cancel/serveDone; StopHealthListener and StopAll release it | 0 disables; nil/canceled Start rejected; one-shot |
| metric.Server | NewServer plus Start; retained private listener; optional TLS wrapping | Serve exact listener; Stop graceful/forced close and bounded join | NewServer maps0→9090; Address reports configuration |
| Standalone Metrics | Start constructs metric.Server; server interface retains runtime Start/Stop authority | Service cleanup invokes retained provider Stop | NewMetrics maps0→9090; interface field is not factory injection |
| Manager startup metrics | claimManagerServer constructs concrete *metric.Server; Manager binds before child Starts | Manager owns diagnostic Stop after child cleanup; Metrics participates in service order/health | Existing standalone fake-provider seam does not replace this acquisition |
| Pprof | Async ListenAndServe; only debug/port gate | No returned listener/server/join handle; process-exit cleanup | debug false or port<=0 disables; default mux caller-owned; failure stdout |

- `service/service_manager.go:153` — `func (m *Manager) ConfigureFromServices(services map[string]types.ServiceConfig, deps *Dependencies) error {`
- `service/service_manager.go:182` — `cfg.HTTPPort = 8080`
- `service/service_manager_test.go:610` — `serviceManager.config = config`
- `service/service_manager.go:1176` — `listener, err := net.Listen("tcp", ":"+strconv.Itoa(m.config.HTTPPort))`
- `service/service_manager.go:1200` — `m.httpListener = listener`
- `service/service_manager.go:1287` — `if port == 0 {`
- `service/service_manager.go:1296` — `listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))`
- `service/service_manager.go:1321` — `m.healthListener = listener`
- `service/service_manager.go:1386` — `stopErr := shutdownManagerHTTPRuntime(ctx, "service-manager/health-listener", server, listener, cancel, serveDone, mode)`
- `service/service_manager.go:1419` — `stopErr := shutdownManagerHTTPRuntime(ctx, "service-manager/http-listener", server, listener, cancel, serveDone, mode)`
- `service/service_manager.go:87` — `testSharedHTTPBound    chan<- struct{}`
- `service/service_manager.go:88` — `testMetricsBindRelease <-chan struct{}`
- `service/service_manager.go:81` — `startupMetricsServer    *metric.Server`
- `service/service_manager.go:620` — `server, err := metricsService.claimManagerServer()`
- `service/service_manager.go:646` — `func (m *Manager) stopStartupMetricsServer(ctx context.Context) error {`
- `service/metrics.go:45` — `type metricsServer interface {`
- `service/metrics.go:78` — `cfg.Port = 9090`
- `service/metrics.go:142` — `server := metric.NewServer(m.config.Port, m.config.Path, m.registry, m.security)`
- `service/metrics.go:315` — `return metric.NewServer(m.config.Port, m.config.Path, m.registry, m.security), nil`
- `service/metrics.go:326` — `return m.config.Port`
- `metric/handler.go:42` — `port = 9090`
- `metric/handler.go:130` — `listener, err := net.Listen("tcp", httpServer.Addr)`
- `metric/handler.go:137` — `listener = tls.NewListener(listener, httpServer.TLSConfig)`
- `metric/handler.go:144` — `serveDone <- httpServer.Serve(listener)`
- `metric/handler.go:234` — `func (s *Server) Address() string {`
- `service/pprof.go:28` — `if !debug || port <= 0 {`
- `service/pprof.go:35` — `if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {`

Inspected owners and embedded BaseService retain private cancellation/join state, not direct context fields.
Manager BaseContext captures a Start-derived child; metric BaseContext captures the supplied Start context.
Metric's detached join is immediately bounded terminal cleanup. This is not a repository-wide context audit.

- `metric/handler.go:116` — `BaseContext: func(net.Listener) context.Context { return ctx },`
- `metric/handler.go:206` — `joinCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), forcedServeJoinTimeout)`

## Adjacent claims, consumer at birth, and problem shape

Current claim is proposal-only PR1386, per caller's refreshed claim state; agentic-loop/E2E claims stay separate.
Explorer's earlier “no active changes” finding is superseded. GitHub state was not independently refreshed here.
No new exported symbol/config field is proposed; no external demand for listener injection is established.

- `openspec/specs/framework-composition/spec.md:152` — `### Requirement: Component starts form a fail-closed boot barrier`
- `openspec/specs/framework-composition/spec.md:173` — `routes MUST remain gated while commitment is false. Diagnostic listeners MUST be released after child cleanup.`
- `openspec/specs/service-shutdown/spec.md:16` — `### Requirement: Coordinated shutdown treats an already-stopped service as clean success`
- `openspec/specs/runtime-context-ownership/spec.md:28` — `### Requirement: Production structs do not retain context authority`

ADR-058 rollout4 preserves early, best-effort, process-lifetime pprof; ADR-094 constrains terminal ownership.
Problem shape already exists: bind once, retain exact handle, observe assigned address, release through owner.

- `service/service_manager_test.go:57` — `listener, err := net.Listen("tcp", "127.0.0.1:0")`
- `service/service_manager_test.go:77` — `_ = server.Serve(listener)`
- `service/service_manager_test.go:81` — `response, requestErr := http.Get("http://" + listener.Addr().String() + "/block")`
- `gateway/graph-gateway/component.go:727` — `listener, err = net.Listen("tcp", c.config.BindAddress)`
- `gateway/graph-gateway/lifecycle_owner_test.go:81` — `c.config.BindAddress = "127.0.0.1:0"`
- `gateway/graph-gateway/lifecycle_owner_test.go:99` — `resp, err := http.Get("http://" + c.listener.Addr().String() + "/graphql")`
- `service/startup_observability_test.go:591` — `occupied, err := net.Listen("tcp", ":0")`

The last pin is intentional collision evidence: its listener remains held, so it is not a free-port defect.
Adjacent helpers below belong to separate transport owners; their enumeration makes no scope ruling.

- `output/websocket/websocket_test.go:1172` — `func findAvailablePort(t *testing.T) int {`
- `input/udp/udp_test.go:478` — `func findAvailablePort(t *testing.T) int {`
- `internal/maxdelivery/runtime_integration_test.go:401` — `func reserveLoopbackPorts(t *testing.T, count int) []int {`

## Bounded collision and adopter inventories

No shared primitive is proposed. Collision dimensions describe existing owners, not a lifecycle redesign.

| Dimension | Evidence/ownership |
|---|---|
| Semantic class/owners | Exclusive TCP bind, address observation, Serve completion/release; six owner rows above |
| Catalogs | Manager HTTPPort, MetricsConfig Port/Path, health CLI flag; no listener catalog surfaced |
| Status | Shared readiness/startup diagnostics; Metrics managerHealthy; health endpoints; pprof stdout |
| Lifecycle/ownership | Manager/metric locks and one-shot flags; exclusive claimManagerServer; native bind host exclusivity |
| Readers/writers |27 test calls, composition/probes, configured URL accessors; owner bind sites above |
| Recovery | Synchronous bind errors and owner cleanup; pprof best-effort failure; no replay/store/lease introduced |

| Adopter surface | Required knowledge/default path/discovery | Gap |
|---|---|---|
| Shared HTTP | ConfigureFromServices0→8080; bindable address; Start/Stop ownership; bind error fails boot | Test-only direct config permits observed ephemeral ownership |
| Health |0 disables, context and positive port, one-shot; composition logs failure and continues | Caller's predicted free port is not reservation |
| Metrics |0→9090, registry/context, TLS, exclusive Manager/standalone ownership; Start errors | Address/Port/URL observe config, not actual listener |
| Pprof |debug and positive port, blank import/default mux, early async startup, process lifetime; stdout failure | No acquisition/completion handle; positive test cannot release owner |

More than two knowledge facts exist for health/metrics/pprof: recorded existing adopter debt, not redesign authority.
The bounded caller should not have to predict continued availability after releasing its reservation.
Production pprof callers are both mains; health startup is cmd/semstreams. Downstream injection demand remains unproven.

## Searches

`git rev-parse HEAD`→50ff16eb; `git status --short`→empty.
`git diff --name-only 353b82a2..HEAD -- service metric`→empty.
`git grep -n -E 'freePort\(t\)' -- service`→18 calls.
`git grep -n -E 'freeMetricsPort\(t\)|freeServerPort\(t\)' -- service metric`→3+6 calls.
`git ls-files openspec/changes | grep -v /archive/`→this proposal only at inspection.
`gopls workspace_symbol -matcher=fuzzy Listener` and references at service_manager.go:1280:19/handler.go:37:6→empty, unusable: known declarations/callers exist. No complete typed-reference census claimed.
`git grep -n -E 'Listener|ListenFunc|listenFunc|listenerFactory|ListenerFactory|StartWithListener|ServeListener' -- service/metrics.go service/pprof.go metric/handler.go`→retained listener/BaseContext/TLS wrapping; no listener injection entry.
`git grep -n -E 'managerMetrics|metricsServer|metric.NewServer|claimManager' -- service`→standalone and Manager paths above.
`git grep -n '127.0.0.1:0' -- '*.go'`→service/metric/gateway/websocket and other tests; retained gateway precedent inspected.
`git grep -n -E 'StartHealthListener|MaybeStartPProf|health-port|pprof-port' -- cmd`→composition/flags consumers.
`git grep -n -E 'HTTPPort.*json|http_port|9090|8080' -- service schemas`→config/default spellings.
`git grep -n -E 'context.Context|context.Background|context.TODO|context.WithoutCancel|context.CancelFunc' -- service/pprof.go service/metrics.go service/service_manager.go metric/handler.go`→signatures/private cancels/BaseContext/bounded join; no Background/TODO roots.
`git grep -n -i -E 'startup|listener|pprof|metrics server|health endpoint' -- openspec/specs docs/adr`→applicable specs/ADRs read.
`git ls-files openspec/specs | grep -E 'lifecycle|shutdown|restart|runtime'`→actual lifecycle capability paths.
Failed unquoted glob searches were discarded and rerun against explicit tracked directories.

## Findings awaiting independent review

1. Shared HTTP already has sufficient test acquisition/observation seams: direct zero config plus retained listener.
2. The existing seams do not mechanically cover all27 calls: health disables0, metrics normalizes0 and crosses package ownership, pprof hides acquisition and cleanup.
3. Retained listener fields are not missing; acquisition/observation access is the unresolved surface.
4. Preserve distinct zero semantics and distinguish intentional collision tests from successful binds.
5. Typed-reference completeness and any downstream need for new exported surface remain unproven.
6. This checkpoint chooses no repair, primitive, or additional-owner scope.
