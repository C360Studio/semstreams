# GitHub #955 Bootstrap Logger Composition Inventory

Baseline: `8f7f87a462462294382ebc2d6dd60488226fa936`

Phase: `inventory-only`

No tests were run during the read-only inventory. The worktree was clean before this artifact was materialized.
Live GitHub state was verified: #955 is open with no comments.

## Claimed gap

GitHub #955 claims both primary binaries construct `natsclient.Client` before final logger and metrics composition,
so the client retains the bootstrap logger. The construction-order claim is confirmed, with one material correction:

- `cmd/semstreams` later builds stdout, NATS-forwarder, and WARN/ERROR counter handlers.
- `cmd/e2e-semstreams` later builds stdout only; its metrics registry is created afterward. It never constructs the
  NATS or counter handlers.

`natsclient.NewClient` stores the current `slog.Default()` pointer at construction and applies options afterward:
`natsclient/client.go:151-179`. `WithLogger` is construction-only and also resolves nil to the then-current default:
`natsclient/options.go:60-69`. No setter, swap, indirection, or post-connect logger seam exists.

Production composition:

- `cmd/semstreams` connects at `cmd/semstreams/main.go:113-124`, creates metrics at `:126-128`, then constructs and
  installs the full logger at `:130-132`. Its client constructor has no options: `:385-397`.
- `cmd/e2e-semstreams` connects and ensures streams at `cmd/e2e-semstreams/main.go:110-119`, installs its logger at
  `:121-122`, and only later creates metrics at `:128` and `:565-589`. Its constructor has no options: `:513-523`.
- Replacing the process default does not mutate the stored `*slog.Logger`; all `c.logger` sites continue using the
  original default.

Consequences:

- Production client logs retain the bootstrap default stderr/text/INFO+ output instead of configured stdout format,
  NATS forwarding, base `service/version/pid` attributes, or `semstreams_log_entries_total`.
- E2E client logs retain bootstrap stderr rather than the later stdout logger. There are no later NATS or counter
  consumers in that binary.
- Boot failures remain visible through the bootstrap default plus returned errors and spinner output.

Exact empty searches:

```text
rg -n 'natsclient\.(WithLogger|WithMetrics)' cmd/semstreams cmd/e2e-semstreams --glob '*.go'
# 0 matches

rg -n 'SetLogger|UpdateLogger|ReplaceLogger|SwapLogger' natsclient --glob '*.go'
# 0 matches

rg -n 'setupLogger|createNATSClient|connectToNATSWithSpinner' \
  cmd/semstreams cmd/e2e-semstreams --glob '*_test.go'
# 0 matches
```

## Every current spelling of logger identity and emission

### Client-captured logger

- Field and initial capture: `natsclient/client.go:67-73,151-179`.
- Client birth, connect, health, and metrics poller: `client.go:185,262,280-301,351-356,474-479,539-550`.
- Consumer and shutdown: `client.go:751,767-771,810-820`.
- Async PubAck and consumer/KV lifecycle: `client.go:1050-1062,1341,1411-1442`.
- Connection-wide async NATS error, including #950 attribution: `client.go:1705-1724`.
- Request/reply response-publication failures: `natsclient/request.go:386-419`.
- Stream divergence, consumer replacement/panic, duplicate-window clamp, and auto-create:
  `natsclient/stream.go:215-258,328,413,560,596`.
- `KVStore` copies the client logger at wrapper birth: `natsclient/kv.go:46-64`; its operation/retry logs are
  `:194-235,367-395,456-487`.
- Framework KV reconciliation reads and passes `c.logger`: `natsclient/kvspec.go:247-293`; helper warnings are
  `:328-367,388-428`.
- `WaitForBucket` passes the same pointer to `resource.Watcher`: `natsclient/client.go:1490-1519`.

No later default change reaches any of those stored or copied pointers.

### Dynamic process-default and independent captures

- Invalid request-timeout environment warnings execute during `NewClient` default initialization, before options:
  `natsclient/request.go:40-55`, `natsclient/client.go:168-176`.
- Durable-handler and heartbeat warnings call package-level `slog` at emission time and therefore use the current
  default then: `natsclient/consume_durable.go:43-54`; `natsclient/heartbeat.go:67-78`.
- `ReconcileNoLifecycleRetention` defaults a nil logger at call time: `natsclient/kv_retention.go:62-67`, logging at
  `:99-108`.
- Storage inventory, publisher, and consumer capture config/default at their own construction:
  `natsclient/storage_inventory.go:135-150,183-223,312-361`;
  `natsclient/storage_report.go:145-164,184-211,235-236,619-670`;
  `natsclient/storage_report_consumer.go:107-126,137-165,205-206,265-268`.
- Production storage observability explicitly supplies its already-composed service logger to those three:
  `service/storage_observability.go:200-230,440-442`; they are not affected by the client capture.

Client birth, connect, and stream bootstrap use the bootstrap logger. The quiet logger at
`cmd/semstreams/main.go:333-335` and E2E `cmd/e2e-semstreams/main.go:525-529` belongs only to
`config.StreamsManager`; it does not replace `natsclient.Client.logger`. Every later client, request, stream, KV,
KVSPEC, async callback, and shutdown log listed above remains on the captured bootstrap pointer. Only package-level
`slog` sites and independently constructed storage surfaces observe the later default.

## Handler construction and ownership

Production `cmd/semstreams` owns:

- stdout JSON/text handler: `cmd/semstreams/logging.go:37-54`;
- NATS handler using the same client as publisher: `:56-63`;
- counter handler over `CoreMetrics().LogEntriesTotal`: `:65`;
- one `MultiHandler(stdout, nats, counter)`: `:67-74`.

`CounterHandler` accepts WARN+ and labels by `component`, defaulting to `unknown`:
`pkg/logging/counter_handler.go:10-59`. `MultiHandler` dispatches enabled handlers and suppresses handler errors:
`pkg/logging/multi_handler.go:21-40`. `NATSLogHandler` asynchronously publishes `logs.{level}.{source}` and resolves
source by `source`, then `component`, then `service`, then `system`: `pkg/logging/nats_handler.go:19-26,61-98,143-184`.
The LOGS stream owns `logs.>` with one-hour and 100 MiB bounds: `config/streams.go:108-118`.

E2E `setupLogger` ignores its NATS/config parameters and returns only stdout JSON/text:
`cmd/e2e-semstreams/main.go:540-563`. Metrics are created afterward at `:565-572`. An exact search for
`NewNATSLogHandler`, `NewCounterHandler`, `NewMultiHandler`, or `pkg/logging` in E2E composition returned zero.

Configuration ownership is already split:

- `LogForwarder` says outer `Enabled` owns activation and `min_level` / `exclude_sources` configure the handler:
  `service/log_forwarder.go:50-65`.
- Actual forwarding is composition-root-owned; the service only validates and reports config:
  `service/log_forwarder.go:12-15,85-130`.
- `setupLogger` always constructs the NATS handler even if log-forwarder is omitted or disabled; it consults that
  service entry only for exclusions: `cmd/semstreams/logging.go:56-68,77-99`.
- Handler `MinLevel` comes from global CLI log level, not `LogForwarderConfig.MinLevel`:
  `cmd/semstreams/logging.go:37-43,60-63`. The config field has no production handler reader.
- `getExcludeSources` reads file-loaded config before config-manager arbitration. Service construction later reads
  effective post-Start config: `cmd/semstreams/main.go:257-260`; `cmd/e2e-semstreams/main.go:242-245`.
- Empty configured exclusions cannot clear the default because replacement requires `len > 0`:
  `cmd/semstreams/logging.go:79-98`.

The code subject grammar is `logs.{level}.{source}` (`pkg/logging/nats_handler.go:86-95`;
`config/streams.go:108-112`), while `service/README.md:448-452` says `logs.{source}.{level}`.

## Feedback-loop and forwarding-failure behavior

- Default exclusion is only `flow-service.websocket`; configured exclusions use exact-or-dotted-prefix matching:
  `cmd/semstreams/logging.go:77-99`; `pkg/logging/nats_handler.go:131-140`.
- WebSocket workers receive that excluded source at `service/flow_runtime_stream.go:540-554`; they consume `logs.>`
  at `:413-433,579-584`.
- No default or configured exclusion names `natsclient`, the application service, or log-forwarder itself.
- A final logger bound to the client would carry base `service=semstreams`, not `component=natsclient`:
  `cmd/semstreams/logging.go:70-74`; its counter label would be `unknown`.
- JSON marshal failures and asynchronous publish errors are silently dropped to avoid recursive logging:
  `pkg/logging/nats_handler.go:76-98`.
- No forwarder-failure counter, health, status, retry, drain, or completion owner exists.

Measured inference: handing the client a logger containing a `NATSLogHandler` backed by that same client creates an
unguarded cycle for forwarded-publish failures that reach `recordFailure` or `recordStreamPublishFailure`, including
JetStream access and non-capacity publish failures: `natsclient/client.go:1010-1038`. `recordFailure` emits Debug and
periodic Info through the same client logger: `natsclient/client.go:253-305`. At DEBUG those paths become recursively
asynchronous. `ErrCircuitOpen` and `ErrNotConnected` return directly without logging or failure accounting at
`natsclient/client.go:1000-1008`, so they do not enter this cycle. The current bootstrap capture prevents the affected
cycle accidentally; no explicit recursion exclusion or invariant exists.

No current NATS-handler test injects a publish failure:

```text
rg -n 'publishFunc\s*=' pkg/logging/nats_handler_test.go
# 0 matches
```

## Metrics paths

Client-local JetStream metrics:

- `WithMetrics` is the only enablement path: `natsclient/options.go:207-222`.
- It registers stream gauges, consumer gauges/counters, and an operation-error counter:
  `natsclient/jetstream_metrics.go:13-34,36-132`.
- Resources are tracked at `natsclient/client.go:958-965,1052-1053,1293-1308,1367-1385` and
  `natsclient/stream.go:190-194,379-386,650-653`.
- The poller starts during `Connect` only if metrics already exist: `natsclient/client.go:547-550`; it stops on
  `Close`: `:578-581`.
- The poller updates tracked handles every interval: `natsclient/jetstream_metrics.go:164-238`. Stream gauges are
  set; consumer cumulative values are added on every poll at `:204-210`.

Without `WithMetrics` in either primary binary, `jsMetrics` remains nil, no JetStream families register, no poller
starts, and nil-receiver tracking/error calls are no-ops.

Platform/core metrics:

- The registry always registers log-entry and core NATS connected/RTT/reconnect/circuit metrics:
  `metric/registry.go:33-52,260-275`; declarations are `metric/core.go:20-30,107-152`.
- `LogEntriesTotal` is updated only by `CounterHandler`: `pkg/logging/counter_handler.go:35-59`.
- Core `RecordNATS*` methods exist at `metric/core.go:195-216`, but no production caller exists outside that file.
- Metrics-forwarder gathers whatever is in the registry: `service/metrics_forwarder.go:147-185`.
- E2E's registry reaches services but not its client or logger.

Exact negative measurement:

```text
rg -n 'RecordNATS(Status|RTT|Reconnect)|RecordCircuitBreakerState' . \
  --glob '*.go' --glob '!**/*_test.go' | rg -v '^./metric/core.go:'
# 0 matches
```

## Binary and shared-client census

Production `cmd/*` imports of `natsclient` are exactly `cmd/semstreams/main.go` and
`cmd/e2e-semstreams/main.go`.

Other non-test production-shaped constructors are test tooling:

- E2E validation wrapper: `test/e2e/client/nats.go:58-82`.
- Disposable E2E scenario clients: `test/e2e/scenarios/core_objectstore_raw.go:55-73,219-237`.
- Shared/testcontainer factory: `natsclient/test_client.go:759-763,803-845`; it supplies timeout, reconnect, and health
  options only, not logger or metrics.
- Both public entry points delegate to that option-less factory: `NewSharedTestClient` at
  `natsclient/test_client.go:846-850` and `NewTestClient` at `:852-869`.
- The standalone E2E runner installs stdout before creating validation/scenario clients:
  `cmd/e2e/main.go:265-278`; those clients capture the installed logger.

Repository docs reproduce connect-first/logger-later composition: `pkg/logging/README.md:287-320`.

## Adjacent claims on the territory

- Project boundary admits shared runtime logging and metrics substrate: `openspec/project.md:3-38`.
- ADR-058 classifies NATS, streams, metrics, and logger as deterministic Phase-A composition and requires shared
  helpers across both mains: `docs/adr/058-boot-lifecycle-phases.md:58-79,100-112`. Its claim that current wiring is
  eager and correct at `:104` predates this measured capture gap.
- Current `nats-streaming` is publish-only: `openspec/specs/nats-streaming/spec.md:3-20`.
- Current `message-logger` explicitly excludes application log-level behavior:
  `openspec/specs/message-logger/spec.md:3-14`.
- `service-composition` requires boot composition to use post-arbitration `SafeConfig`, never the stale file object,
  and says log-forwarder/metrics knobs are next-boot: `openspec/specs/service-composition/spec.md:28-37,136-145`.
  Production currently conflicts with that requirement: it builds the logger from original file-loaded `cfg` before
  `config.Manager.Start` at `cmd/semstreams/main.go:126-145`, and `getExcludeSources` directly reads `cfg.Services` at
  `cmd/semstreams/logging.go:77-98`. The #955 design must either resolve this adjacent spec violation or explicitly
  bound and track it; application logger lifetime itself remains unspecified.
- Active `attribute-nats-subscription-errors` owns `Client.handleError` attribution and explicitly excludes logger
  wiring to #955: proposal `:19-26`; design `:11-13,45-60`; diagnostics delta `:5-51`; tasks `:36-39`.
- Active `stream-capacity-rejection-is-circuit-neutral` preserves existing metrics/logging and adds no surface:
  proposal `:9-19`; design `:52-72`.
- The accepted #950 inventory already measured capture at
  `docs/proposals/gh950-slow-consumer-attribution-inventory.md:42-55,87-99`; its design separated logger composition
  because the full logger depends on the connected client and creates a feedback loop:
  `docs/proposals/gh950-slow-consumer-attribution-design.md:167-172`.
- #950 is closed; #955 is open; #954 remains the assembled-product E2E gap; #586 remains pending-limit policy.
- ADR-081 splits slow-consumer attribution and pending-limit configuration from graph-view fan-out:
  `docs/adr/081-graph-view-subscription.md:212-225`.

No current capability spec governs application logger lifetime, NATS-forwarder failure, or bootstrap-to-steady-state
logger identity. An exact application-logger search returned only unrelated message-logger/service references.

## Current tests

Existing evidence:

- `natsclient/client_async_error_test.go:69-177` and the real-NATS proof at
  `natsclient/client_async_error_integration_test.go:24-120` explicitly inject `WithLogger`; they do not test default
  replacement.
- JetStream metric integration explicitly injects `WithMetrics`: `natsclient/integration_test.go:296-382`;
  storage inventory has another registry path at `natsclient/storage_inventory_integration_test.go:80-89`.
- NATS handler tests cover levels, subject/payload, source priority, exclusions, attrs/groups, and concurrency:
  `pkg/logging/nats_handler_test.go:44-442`.
- MultiHandler tests cover fan-out and error continuation: `pkg/logging/multi_handler_test.go:67-185`.
- Counter tests cover WARN+, labels, chaining, pass-through, and nil counter:
  `pkg/logging/counter_handler_test.go:44-181`.
- Service integration drives `NATSLogHandler` directly, not the binary sequence:
  `service/flow_runtime_stream_integration_test.go:82-146,320-380`.

Missing evidence:

- No test in either primary command exercises `setupLogger`, `createNATSClient`, or connect-before-`SetDefault`.
- No test proves one post-bootstrap client ERROR reaches configured stdout/NATS/counter consumers.
- No test proves forwarding failure is non-recursive.
- No test proves WARN/ERROR counting exactly once across client logging.
- #954 records the absence of an assembled E2E attribution test.

## Consumer at birth

Present consumers already exist; no new exported surface is assumed:

- stdout/stderr operators;
- WebSocket clients consuming LOGS through `logs.>`;
- Prometheus and metrics-forwarder consumers of `semstreams_log_entries_total`;
- the external SemSource operator whose scale run motivated #950;
- E2E operators expecting the configured stdout logger.

No present consumer was found for a new logger setter, mutable global logger, or new metric. This inventory introduces
none.

## Adopter seam inventory

Specific adopter: an external component author using `natsclient.NewClient` or receiving the shared client without
reading either composition root.

1. **What must they know today?** `NewClient` snapshots the logger pointer; later `slog.SetDefault` does not reach the
   client or derived `KVStore`; logger and metrics options must be supplied at construction; metrics must exist before
   `Connect` for the poller; a logger containing the same client's NATS handler has no explicit recursive-failure
   guard; and log-forwarder activation/min-level configuration does not actually own handler construction.
2. **What happens if they do nothing?** The client silently keeps its birth logger. Later format, destinations,
   attributes, counters, and registry are missed. Production consumers disagree about which logs exist; E2E's
   configured stdout disagrees with stderr. Forwarding failures stay silent.
3. **Where do they find out?** `WithLogger`/`WithMetrics` comments and repository archaeology. There is no compile,
   boot, typed-runtime, health, status, or warning signal. Rank: documentation/source only.
4. **What should they have to know?** Nothing about boot order, handler cycles, pointer capture, poller timing, or
   recursion exclusions. The framework should own the real composition sequence.

Current callers must predict the eventual handler graph and metrics registry before the NATS-dependent logger can be
constructed. The framework can observe connection state and final composition; requiring callers to predict/order it
is the seam gap.

## Collision-table disposition

No same-class collision table is triggered during inventory: no durable, communication, or runtime-coordination
primitive is proposed. Existing owners of logger identity, forwarding, counters, client metrics, configuration, and
lifecycle are enumerated above.

## Measured premises and open evidence

- `HEAD == origin/main == 8f7f87a462462294382ebc2d6dd60488226fa936` at inventory time.
- Only the two primary binaries construct production `cmd/*` clients; cited census and searches above.
- Every client-captured logger path stays on one birth pointer; dynamic and independent exceptions are enumerated.
- The production full logger creates a same-client forwarding cycle on the cited publish-failure paths that reach
  failure accounting if assigned wholesale to the client; this is a code-path inference, not yet a runtime experiment.
- The E2E issue claim is corrected: its later logger is stdout-only and its metrics registry is later still.
- No present consumer exists for a new setter, mutable global logger, or metric.
- Sister-repository explicit `WithLogger`/`WithMetrics` composition was outside this repository inventory.
- The active #950 change remains under `openspec/changes` even though implementation and review are complete.

**INVENTORY READY**

# GitHub #955 Bootstrap Logger Composition Design

Status: `owner-accepted`

The accepted inventory above is preserved verbatim. Its independently reviewed body SHA-256 is
`a78e4c894a1c2c1b3819296e561964381e95235402e051ea8ad0a2ad5940a5ba`.

## Options considered

- **O0 — do nothing.** No implementation cost. Client logs remain on bootstrap stderr, both binaries omit client
  metrics, production misses configured destinations and its counter, and stale file config still selects forwarding.
- **O1 — add `Client.SetLogger` after connect.** Directly replaces the client field, but adds a public mutable
  lifecycle surface, races with callbacks and pollers, does not update pointers copied into `KVStore`, and makes
  adopters own boot order.
- **O2 — add a mutable/deferred handler.** One stable logger pointer can reach copied loggers, but the approach adds
  synchronization and a partially initialized lifecycle, obscures attribute ownership, and makes same-client safety
  depend on runtime mutation.
- **O3 — construct a non-forwarding client logger and metrics before `Connect` using existing options.** This fixes
  capture and metrics timing without a new API and makes recursion exclusion structural. The cost is two intentional
  logger graphs and a shared, explicit boot sequence.

Recommendation: **O3**.

## Target composition

Both primary binaries use the same plain helpers from a Go `internal/` package, consistent with ADR-058. The helper
boundary owns construction mechanics, not policy-bearing public API.

1. Load and locally validate file configuration, preserving current `--validate` behavior.
2. Create the process metrics registry.
3. Create the configured stdout handler.
4. Compose and install the non-forwarding bootstrap process logger. Production includes stdout plus the WARN/ERROR
   counter; E2E includes stdout only.
5. Derive the client logger with `component=natsclient`.
6. Construct `natsclient.Client` with `WithLogger(clientLogger)` and `WithMetrics(metricsRegistry)`.
7. Connect the client.
8. Construct and start `config.Manager` with a non-forwarding `component=config-manager` logger.
9. Read `effectiveCfg := configManager.GetConfig().Get()` after arbitration.
10. Run `effectiveCfg.Validate()`, `rulepackcap.ValidateConfig(effectiveCfg)`, and
    `graphresearch.ValidateConfig(effectiveCfg)`.
11. Run `StreamsManager.VerifyJetStreamLimits(ctx, effectiveCfg)` and
    `StreamsManager.EnsureStreams(ctx, effectiveCfg)` to completion. This creates LOGS and every other effective
    stream before a NATS forwarding handler exists.
12. Resolve enabled log-forwarder policy from effective service config, then compose and install the steady-state
    process logger:
    - production: stdout, WARN/ERROR counter, and an optional NATS forwarder;
    - E2E: stdout only.
13. Continue all remaining boot composition from `effectiveCfg`.

The client and derived KV stores intentionally retain a non-forwarding logger. The config manager also remains on a
non-forwarding logger because it arbitrates the configuration that decides whether forwarding exists. Production's
client logger includes stdout and the WARN/ERROR counter, so its records reach both exactly once. E2E's client logger
includes stdout only. Its metrics registry still registers `LogEntriesTotal`, but no `CounterHandler` is installed and
client/application records do not increment that family.

### One owner for log-forwarder inner policy

Create one repository-internal `internal/logforwarderpolicy` package. Its private policy value and one resolver own
decode, INFO defaulting, normalization, and validation. `service.LogForwarderConfig` remains the same named public type
declared in package `service`, preserving both source and runtime type identity. `service.NewLogForwarderService`
translates the internal resolved value into that public type, while `LogForwarderConfig.Validate` delegates its field
semantics to the internal owner. The internal boot-composition helper consumes the internal resolved value directly.

The outer service resolver remains structural and does not decode inner JSON. The boot helper calls the inner policy
resolver only after the effective outer map says `log-forwarder.Enabled == true`; absent and disabled entries invoke
no inner decoder or validator. The service constructor receives only enabled entries and delegates to that same
resolver. The `service-composition` delta explicitly replaces constructor-exclusive ownership for this one Phase-A
policy with single-internal-resolver ownership; it does not grant the outer resolver service-specific semantics.

The design does not make every boot log NATS-forwardable. Connection, stream, and config-arbitration records precede
the effective forwarding decision and remain operator-visible through configured stdout and returned boot errors.

## Proposed owner rulings

- **R1 — construction.** Use existing `WithLogger` and `WithMetrics`. Add no setter, mutable proxy, or deferred
  handler.
- **R2 — production client routing.** A client WARN/ERROR reaches configured stdout and
  `semstreams_log_entries_total` exactly once. A client record never enters a `NATSLogHandler` backed by that client.
- **R3 — E2E routing.** Preserve stdout-only application logging. E2E gains pre-connect client metrics, but installs
  no `NATSLogHandler` or `CounterHandler`; the registered `LogEntriesTotal` family receives no client/application
  increments.
- **R4 — identity.** Bind `component=natsclient` on both client loggers. Production counter labels use `natsclient`,
  not `unknown`; service/version/pid attributes remain where available.
- **R5 — metrics timing.** Both registries exist before client construction, and both clients receive `WithMetrics`
  before `Connect`, so the JetStream poller starts.
- **R6 — effective configuration.** Start `config.Manager` before final forwarding composition. Every subsequent
  logger/service decision, effective validation, and stream provisioning reads post-arbitration `SafeConfig`, never
  the original file object. Effective streams exist before the forwarding handler is installed.
- **R7 — activation.** An absent or outer-disabled `log-forwarder` creates no NATS handler. Outer `Enabled` remains
  the sole activation input.
- **R8 — levels.** CLI/global level governs stdout. `LogForwarderConfig.MinLevel`, defaulting to INFO, governs only
  NATS forwarding. The counter remains WARN+.
- **R9 — exclusions.** `flow-service.websocket` is a mandatory framework safety exclusion. Effective exclusions are
  its union with configured exact/dotted-prefix exclusions; omitted or empty config cannot remove it.
- **R10 — failure visibility.** Configured stdout is installed before client construction. Client/config boot failures
  remain visible there and through returned errors and spinner failure output.
- **R11 — shared implementation.** Both mains call the same internal plain logging/client/config-arbitration helpers.
  Add no boot state machine, service, DI container, or per-binary copy.
- **R12 — recursion invariant.** Same-client prevention is structural: the client's logger graph contains no NATS
  handler. It does not depend on source exclusions or runtime failure detection.
- **R13 — public surface.** Add no public `natsclient`, `service`, `pkg/logging`, config, metric, status, or health
  symbol. Preserve `service.LogForwarderConfig` as the named package-`service` type, including its runtime identity.
  Internal exported Go names remain repository-internal.
- **R14 — specification.** Specify bootstrap logging and amend service composition's effective-config behavior. Do
  not add an ADR.
- **R15 — policy ownership.** One internal log-forwarder policy resolver owns decode, INFO defaulting, normalization,
  and validation for both the service constructor and boot composition. Outer resolution remains structural; absent
  or disabled entries never invoke the policy resolver.
- **R16 — logger identity.** Bootstrap, client, config-manager, and steady-state loggers reuse the same configured
  stdout handler and common base attributes. Child loggers add only their required component identity and destinations.
  The shared composition path SHALL NOT silently create or fall back to a different logger or handler instance; an
  independent logger exists only when an explicit call-site requirement names it.

## Adopter seam target

Specific adopter: a developer outside this repository writing a component that receives the shared client.

- **What must they know?** Nothing new. The composition root supplies logger and metrics dependencies before the
  client is usable.
- **What happens if they do nothing?** Their component logs and uses the shared client normally. Client diagnostics
  appear on configured local output and production WARN/ERROR counts; no same-client forwarding cycle exists.
- **Where do they find out?** Normal runtime output and the application-logging capability spec. No correctness fact
  is left at documentation-only discovery.
- **What should they have to know?** Nothing about handler graphs, pointer capture, metrics-poller timing, config
  arbitration, or recursion exclusions.

No new communication path, orchestration behavior, payload, or query access is introduced. Therefore none of the
canonical `kv-or-stream`, `orchestration-check`, `new-payload`, or `query-pattern` decision skills triggers.

## Test contract

Tests SHALL invoke the same Phase-A construction helpers called by both real `run` paths; handler-only replicas do not
satisfy the contract.

1. Production construction: emit through the constructed client logger and assert one stdout record, one
   `component=natsclient` WARN/ERROR counter increment, and zero forwarding-handler calls.
2. Production steady state: ordinary application records reach stdout and counter, and reach NATS only when effective
   `log-forwarder` is enabled.
3. E2E construction: client and application records use configured stdout, no NATS or counter handler exists,
   `LogEntriesTotal` remains unchanged, and client metric families register before connection.
4. Effective configuration: arrange file/KV disagreement, synchronously complete `config.Manager.Start`, and prove
   enabled state, minimum level, and exclusions come from `SafeConfig`.
5. Level/exclusion table: disabled, default INFO, explicit DEBUG/WARN/ERROR, empty exclusions, exact exclusions, and
   dotted-prefix exclusions.
6. Boot failure: force client creation, connection, or config-manager failure and assert configured stdout plus
   returned-error visibility.
7. Real-NATS integration: connect through the production construction helper, trigger a real client diagnostic path,
   and verify stdout/counter delivery with no `logs.>` publication for that client record.
8. Metrics integration: connect through each binary construction path and reuse existing JetStream metrics assertions
   to prove the poller is active.
9. Half-migration guard: command-package tests exercise each main's actual construction entry and assert the shared
   helper behavior.

Synchronization uses handler channels, NATS acknowledgements/flushes, context deadlines, and Prometheus gathers.
Tests contain no arbitrary `time.Sleep`.

## OpenSpec draft

Change ID: `compose-bootstrap-client-observability`

### Proposal

The two primary binaries construct `natsclient.Client` before final logger/metrics composition. The client snapshots
the bootstrap logger, and neither client receives metrics before `Connect`. Production's final logger cannot simply be
assigned wholesale because it contains a NATS handler backed by the same client. Production also selects forwarding
policy from stale pre-arbitration file config.

Add shared internal Phase-A construction helpers that give the client a configured, component-attributed,
non-forwarding logger and metrics registry before connection. After config arbitration, compose the steady-state
process logger from effective service config. Preserve production and E2E destination differences. Add no public API,
config schema, subject, metric, or durable state.

### Application-logging spec delta

#### Requirement: Client observability exists before connection

Each primary binary SHALL construct its metrics registry and non-forwarding client logger before client construction,
and SHALL pass both through the existing client options before `Connect`.

#### Requirement: Client records have stable local identity

Client records SHALL use configured stdout and `component=natsclient`. Production WARN/ERROR records SHALL increment
the existing log counter exactly once. E2E SHALL remain stdout-only.

#### Requirement: Client logging cannot forward through itself

A client's logger graph SHALL NOT contain a NATS log handler backed by that client. This invariant SHALL hold for all
client publish failure paths without relying on exclusion strings.

#### Requirement: Boot failures remain visible

Configured local output SHALL exist before client construction. Connection and config-arbitration failure SHALL remain
visible locally and through the returned boot error.

### Service-composition spec delta

#### Requirement: Effective service state selects log forwarding

After `config.Manager.Start` completes arbitration, final log-forwarder composition SHALL use its effective
`SafeConfig`. Outer enabled state SHALL solely activate forwarding. Forwarder minimum level SHALL govern only NATS
delivery. Effective exclusions SHALL retain the framework WebSocket safety exclusion and union configured exclusions.

Effective configuration validation and stream-limit verification/provisioning SHALL complete before the forwarding
handler is installed. LOGS and all other declared streams SHALL be derived from effective configuration, not the
original file object.

#### Requirement: One inner-policy owner serves service construction and Phase-A forwarding

The constructor-exclusive inner-semantics rule SHALL be narrowed for `log-forwarder`: one repository-internal policy
resolver SHALL solely decode, default, normalize, and validate its inner config for both service construction and
Phase-A handler composition. The structural outer resolver SHALL remain service-agnostic and SHALL NOT call this
resolver. An absent or outer-disabled entry SHALL invoke no inner decoder or validator.

### Tasks

1. Add failing shared-construction routing, identity, exact-counter, and metrics tests.
2. Add failing `SafeConfig` arbitration and forwarding-policy tests.
3. Introduce internal plain bootstrap logging/client helpers using existing options.
4. Move log-forwarder inner semantics to one internal resolver, preserve the existing named public
   `service.LogForwarderConfig` type and delegate/translate at the service boundary, and prove absent/disabled entries
   invoke no decoder or validator.
5. Rewire production and E2E mains through the same helpers.
6. Add real-NATS integration and half-migration regression tests without arbitrary sleeps.
7. Add the application-logging spec delta and amend service composition.
8. Correct the stale documented log subject order.
9. Run focused race tests, repository race tests, lint, schema generation/drift, contract tests, and `task e2e:core`.

## Decision-record disposition

No ADR is warranted. This is reversible composition mechanics that applies ADR-058 and existing constructor options;
it creates no irreversible or cross-repository contract.

## Open owner questions

The owner accepted R1-R15 and added R16 on 2026-08-13. No evidence or owner question remains open.
