# GitHub #954 Assembled Slow-Consumer Attribution Inventory

Baseline: `ecd8a2e1c75d39ed283deed530776ecc1c1f3dd5`

Phase: `inventory-only`

No files were changed and no tests were run during the read-only inventory. The worktree was clean. Live GitHub state
was verified: #954 and #586 are open with no comments.

## Claimed gap

GitHub #954's claim is accurate: no product E2E scenario deliberately overflows a `cmd/semstreams` core-NATS async
subscription and asserts the emitted structured attribution plus cumulative drops.

```text
pattern='slow[ _-]?consumer|errslowconsumer|setpendinglimits|pendinglimits|\.dropped\('
pattern="$pattern|\"dropped\"|dropped_available|nats error"
rg -n -i "$pattern" \
  cmd/e2e test/e2e taskfiles/e2e docker/compose configs
# no relevant E2E occurrence
```

The matches are unrelated Grafana/throughput wording and an ops timeout mentioning its last NATS error. Deliberate
overflows exist only in package/integration proofs, not product E2E.

## Every current spelling of the behavior

- The production connection installs one async error callback, `nats.ErrorHandler(m.handleError)`, at
  `natsclient/client.go:405-417`.
- `handleError` emits ERROR, message `NATS error`, `error`, `subject`, optional `queue`, and for
  `nats.ErrSlowConsumer`, either `dropped` or `dropped_available=false`: `natsclient/client.go:1705-1724`.
- The public subscription wrapper exposes only `Unsubscribe`; raw subscription pending controls stay hidden:
  `natsclient/client.go:841-852`.
- Framework handlers execute synchronously inside the NATS async callback: `natsclient/client.go:867-878`.
- The #950 real-NATS proof gates the callback, sets `SetPendingLimits(1,-1)`, publishes exactly eight excess messages,
  polls for the exact cumulative dropped value, then releases and asserts the structured record without sleeps:
  `natsclient/client_async_error_integration_test.go:24-164`.
- The #955/#961 production-bootstrap integration composes the production logger/client, proves ordinary application
  forwarding to `logs.>`, overflows a gated raw subscription, and proves the diagnostic is local/counted but not
  self-forwarded: `cmd/semstreams/bootstrap_observability_integration_test.go:130-218`. It asserts
  level/message/component/counter, but not error, subject, queue, or dropped count.
- Production Phase A supplies configured local output plus the WARN/ERROR counter; E2E Phase A is local-only:
  `internal/bootstrapobservability/bootstrap.go:34-73`.
- `cmd/semstreams` creates the client from the non-forwarding `component=natsclient` child and later adds forwarding
  only to the process logger: `cmd/semstreams/main.go:114-156`.
- `cmd/e2e-semstreams` explicitly calls `Steady(nil)`: `cmd/e2e-semstreams/main.go:276-312`.

## Current E2E home and topology

- `task e2e:core` launches `docker/compose/e2e.yml` and runs `./e2e --scenario all`:
  `taskfiles/e2e/core.yml:4-34`.
- The compose application uses Docker target `production`, therefore `cmd/semstreams`:
  `docker/compose/e2e.yml:43-80`.
- It exposes NATS client `34222`, NATS monitor `38222`, HTTP `38080`, metrics `39090`, UDP, and WebSocket:
  `docker/compose/e2e.yml:16-31,70-78`.
- Core runs exactly health, dataflow, and graph-roundtrip, counting scenarios rather than assertions:
  `cmd/e2e/main.go:568-607`.
- The per-PR statistical tier builds Docker target `e2e`, meaning `cmd/e2e-semstreams`, despite workflow prose saying
  full stack: `.github/workflows/e2e-ladder.yml:11-24`; `docker/compose/tiered.yml:195-236`.
- Agentic/research/deep-research/crud stacks add unrelated services and flows. Structural/statistical/semantic and
  throughput use the E2E root. Core is the current general production-root tier; inventory makes no binding tier
  ruling.

## Control feasibility

An exact assembled overflow is not feasible through the current external controller alone:

- The host can publish through exposed NATS but cannot create or configure a subscription on the production process's
  connection.
- `natsclient.Subscription` hides `SetPendingLimits`, `PendingLimits`, and the raw subscription.
- The assembled protocol flow enables `message-logger`, which creates production-client core-NATS subscriptions:
  `configs/protocol-flow.json:26-32`; `service/message_logger.go:449-468`. Their handlers are not externally gateable,
  their pending limits are not configurable, and the wrapper exposes neither limits nor drop counts.
- Flow-runtime log streaming can also create raw production-connection subscriptions:
  `service/flow_runtime_logs.go:152-217`. Their callbacks use a non-blocking channel send, so they do not provide a
  controllably blocked handler; the surface exposes neither pending-limit nor drop-count control.
- No `cmd/semstreams` E2E flag or control subject exists. Searches of `cmd/semstreams`, configs, compose, and the E2E
  harness find `SetPendingLimits` only in the bootstrap integration test.
- Protocol-flow HTTP output consumes JetStream, not core NATS: `configs/protocol-flow.json:246-263`. HTTPPost can
  consume core NATS and synchronously block in HTTP delivery (`output/httppost/httppost.go:282-307,463-498`), but no
  configuration can lower, inspect, or gate the subscription pending limits.
- The pinned nats.go v1.52.0 defaults are 500,000 messages and 64 MiB:
  `go.mod:12`; `$GOPATH/pkg/mod/github.com/nats-io/nats.go@v1.52.0/nats.go:5637-5642`. External overflow would be
  costly and timing-dependent.
- NATS schedules its error callback when entering slow state, while `handleError` reads cumulative `Dropped()` when
  the callback runs: `$GOPATH/pkg/mod/github.com/nats-io/nats.go@v1.52.0/nats.go:3880-3903`;
  `natsclient/client.go:1705-1724`. An external publisher cannot freeze the exact observed cumulative value. The #950
  proof obtains exactness by gating the callback.
- `/varz` is reachable and existing graph-index integration tests inspect aggregate `slow_consumers`, but that signal
  neither identifies the client subscription nor gives the attributed cumulative count.

Measured conclusion: production-client subscriptions exist, but no current one exposes the deterministic handler
blocking, pending-limit reduction, callback gating, or drop-count inspection needed for an exact assembled proof. The
external controller also has no current local-output capture path in the core harness. Inventory does not decide which
surface, if any, should supply those controls.

## Observation feasibility

- The required diagnostic cannot be observed through `logs.>` or runtime WebSocket. #961 deliberately keeps the NATS
  client's Phase-A logger outside its own NATS forwarder:
  `openspec/changes/compose-bootstrap-client-observability/specs/application-logging/spec.md:29-39`.
- `NATSLogHandler` publishes process records as `logs.{LEVEL}.{source}`: `pkg/logging/nats_handler.go:19-26,86-98`.
  Client diagnostics remain container-local.
- Core defaults to structured JSON stdout because compose does not set `SEMSTREAMS_LOG_FORMAT` and the binary default
  is JSON: `docker/compose/e2e.yml:60-68`; `cmd/semstreams/flags.go:41-43`.
- Production exposes `semstreams_log_entries_total{component="natsclient",level="error"}` through the core metrics
  port: `metric/core.go:20-23,107-115`; `docker/compose/e2e.yml:70-73`. That can corroborate exact-one emission but
  cannot prove the subject, queue, error, or dropped fields.
- Core's normal harness has no programmatic container-stdout client. Within the core task, Docker logs appear only in
  the manual debug task: `taskfiles/e2e/core.yml:36-44`.
- Ops and CRUD scenarios independently shell out to `docker logs`, establishing repository precedent but no shared
  observation abstraction: `test/e2e/scenarios/ops/scenario.go:276-290`;
  `test/e2e/scenarios/crud-tools/scenario.go:356-367`.
- WebSocket tests inspect flow-status/log envelopes, not process-local Phase-A diagnostics:
  `test/e2e/scenarios/core_dataflow.go:297-385`.

## Result and assertion accounting

- `Scenario.Result` has success/error, metrics/details, errors/warnings, and optional tiered structured results, but
  no assertion counter: `test/e2e/scenarios/scenario.go:30-58`.
- `runScenario` saves structured output only when `outputDir` is set and `Structured` is nonnil:
  `cmd/e2e/main.go:520-558`.
- Core `--scenario all` supplies neither and reports only three scenario pass/fail totals.
- Tiered execution prints `[i/N]` stages and fails fast but does not count individual assertions:
  `test/e2e/scenarios/tiered.go:423-499`.

```text
rg -n 'assertion_count|assertions_run|checks_run|assert count' cmd/e2e test/e2e
# 0 matches
```

Therefore #954's requirement to report the assertion count has no generic executable field to inherit.

## Synchronization

The #950 and #955 proofs use channel barriers, contexts, NATS flushes, and bounded condition polling. Neither uses
`time.Sleep`. Core task sleeps occur only in its manual debug task, not its scenario. Existing scenario APIs accept
`context.Context`; explicit readiness and condition signaling is the current convention.

## Adjacent claims

- The accepted #950 OpenSpec requires exact attribution and excludes pending-limit control:
  `openspec/changes/attribute-nats-subscription-errors/specs/nats-client-diagnostics/spec.md:5-17`;
  `openspec/changes/attribute-nats-subscription-errors/proposal.md:14-23`.
- The #961 OpenSpec requires client/config diagnostics to remain local and forbids same-client NATS forwarding:
  `openspec/changes/compose-bootstrap-client-observability/specs/application-logging/spec.md:29-39`.
- ADR-081 records attribution and watcher pending-limit configuration as split natsclient ergonomics:
  `docs/adr/081-graph-view-subscription.md:223-225`.
- Graph-view slow-subscriber behavior is bounded, process-local fan-out rather than this core-NATS callback:
  `openspec/specs/graph-view-subscription/spec.md:188-205`.
- #586 is half stale: #950 delivered its attribution half. Its remaining pending-limit surface is a separate adopter
  prediction/policy question.
- No new subject, bucket, stream, or lifecycle control exists in the current surface. Collision inventory and
  `kv-or-stream` classification do not trigger during inventory.

## Consumer at birth

- Present consumer: the operator of an assembled SemStreams deployment diagnosing which production subscription
  dropped messages and how many known drops accumulated.
- Present test consumer: the release gate that must fail when attribution is removed or falsified.
- No present production consumer exists for a new pending-limit or E2E-control API.

## Adopter seam inventory

Specific adopter: a developer outside this repository implementing a component and calling
`natsclient.Client.Subscribe`.

1. **What must they know now?** Only the subject and handler. The framework owns connection error handling and records
   slow-consumer attribution; it exposes no raw subscription pending controls.
2. **What happens if they do nothing?** NATS defaults apply. A synchronous handler that falls behind can overflow;
   the framework emits a local structured `NATS error` while health remains unchanged.
3. **Where do they find out?** The public wrapper and diagnostics spec define behavior. Operationally the record is in
   process-local output, not `logs.>` or flow WebSocket.
4. **What should they have to know?** Nothing about pending capacity, callback timing, an internal test subject, or a
   readiness deadline. Whether the missing proof can preserve that zero-knowledge seam is a design question.

## Measured premises and open evidence

- No product E2E overflow/assertion exists; exact search above.
- Core is the current production-root E2E tier; statistical and related tiers use the E2E binary.
- Exact cumulative-drop proof requires in-process callback gating; external NATS publishing cannot freeze it.
- The diagnostic is intentionally local after #961; `logs.>` silence is a safety invariant, not an observation bug.
- Existing harness result types do not count assertions.
- No existing surface combines deterministic blocking, pending-limit reduction, callback gating, drop inspection, and
  local-output observation. Inventory does not decide whether to add, adapt, or sequence any surface to provide them.

**INVENTORY READY**
