# GitHub #954 Assembled Slow-Consumer Attribution Design

Baseline: `ecd8a2e1c75d39ed283deed530776ecc1c1f3dd5`

Phase: `owner-accepted`

Accepted inventory: `docs/proposals/gh954-slow-consumer-e2e-inventory.md`, SHA-256
`0bcd335683770294919f8b1e8129985badb2247be9c4053a43e9779d297b5e76`.

Owner accepted rulings R1-R15 on 2026-08-13. The owner noted that the design is somewhat over-engineered for a logger
proof and approved it on the condition that implementation remain within the narrow ruled shape.

## Problem boundary

#950 proves the real nats.go mechanism and production handler. #961 proves the production composition supplies that
handler with the explicitly configured local logger graph. Neither proof runs an assembled `cmd/semstreams` process
in Docker and observes the operator-facing JSON record from outside the process.

#954 closes only that last gap. It does not decide #586's production pending-limit policy and must not create a public
tuning surface merely to make an E2E deterministic.

## Options

- **Keep integration coverage only:** cheap and no adopter cost, but leaves the assembled proof absent. Reject.
- **Externally overflow nats.go defaults:** uses the exact production image, but requires the 500,000-message/64-MiB
  threshold and cannot freeze the callback count. Reject.
- **Implement #586 first:** could tune an ordinary subscription, but ships public policy for a test and still lacks
  callback gating/drop inspection. Reject as a sequencing dependency.
- **Add production control:** a flag, config, endpoint, or subject is deterministic but charges every adopter for a
  backdoor-shaped surface. Reject.
- **Reuse a production subscriber:** `message-logger` and flow-log handlers cannot be gated and hide limits/drops.
  Reject.
- **Replace ordinary core with a tagged image:** keeps the same root but weakens existing exact-image coverage. Reject.
- **Add a separate tagged `cmd/semstreams` gate:** keeps the same root and logging code while ordinary core remains
  exact; it is small and CI-affordable. Choose.

## Recommended architecture

Keep `task e2e:core` and `docker/compose/e2e.yml` unchanged as the exact production-image proof. Add a separate,
disposable slow-consumer stack whose application is built from `./cmd/semstreams` with one narrow build tag. The tag
replaces a default no-op hook; it does not select a different main, bootstrap path, client constructor, logger helper,
configuration manager, service manager, or runtime configuration.

The normal source file calls the hook immediately after the production client connects and before configuration
arbitration or forwarding composition. The ordinary build compiles a no-op implementation. The tagged command hook
calls a tagged `internal/e2eslowconsumer` probe using the already-constructed production `*natsclient.Client`. The
fixture uses the existing `Client.GetConnection()` plus nats.go's concurrency-safe `Conn.ErrorHandler()` and
`SetErrorHandler()` APIs; it adds no natsclient symbol. It creates or defaults no logger or handler. The sole target
record therefore traverses the captured #961 graph: configured local JSON output, common base attributes,
`component=natsclient`, and the WARN/ERROR counter, with no same-client NATS forwarder in that graph.

The tagged probe auto-runs in its fresh stack; there is no control message path. This follows the `kv-or-stream`
decision: a trigger would be a request to do work and would require JetStream work semantics, which is unnecessary for
a disposable one-shot fixture. No subject, stream, bucket, endpoint, flag, or config schema is added for control.

### Deterministic probe

The tagged hook:

1. Captures the connection's installed production async-error callback and installs a temporary wrapper.
2. Creates a raw queue subscription on a fixed E2E-only diagnostic subject, with a fixed queue, and sets the message
   pending limit to one.
3. Flushes, publishes one message, and waits on a channel until its handler is blocked.
4. Publishes exactly eight additional messages and flushes.
5. The temporary callback wrapper gates only the matching subscription's `ErrSlowConsumer`; all unrelated callbacks
   immediately delegate to the captured production callback.
6. It bounded-polls `Subscription.Dropped()` until it equals eight, failing if it exceeds eight or the context expires.
7. It releases the matching callback into the captured production callback and waits for that callback to return.
   Because the configured local handler writes synchronously, return bounds visibility of the JSON record.
8. It restores the original callback and releases/unsubscribes the message handler. Any probe failure returns as a
   tagged-build boot error; it does not manufacture another WARN/ERROR record.

Every wait uses channels, `context.Context`, NATS flushes, or a ticker-driven bounded condition loop. There is no
`time.Sleep`.

The fixture owns its fixed subject, queue, limit, and publish count. The external scenario imports the same internal
fixture contract rather than predicting those values.

### External observation

A dedicated host-side scenario waits for the fresh stack to become ready, then bounded-polls `docker logs` and parses
JSON lines rather than matching prose. It selects the unique `NATS error` record for the probe subject and asserts:

- level is `ERROR` and message is `NATS error`;
- `component=natsclient`;
- `error` identifies `nats: slow consumer, messages dropped`;
- subject and queue equal the fixture contract;
- `dropped` equals the fixture-reported known cumulative value, exactly eight;
- exactly one matching diagnostic exists;
- no `dropped_available=false` fallback is present;
- `semstreams_log_entries_total{component="natsclient",level="error"}` is exactly one in the fresh stack.

The metric corroborates exact-one routing; it does not substitute for the structured-field proof. The scenario is RED
if attribution is removed, any asserted field is falsified, the callback bypasses the production handler, the logger
loses its stable component identity, or the output no longer reaches configured JSON stdout.

### Assertion accounting

Add `AssertionsRun int` to the E2E `scenarios.Result` and report it from `runScenario` on both success and failure when
a result exists. The new scenario increments the count only after an assertion actually executes. Its terminal result
contains the dynamic count and a stable expected-total check; early failure therefore reports the smaller actual
count rather than a planned constant. Existing scenarios may leave the field zero.

## Binding owner rulings

- **R1:** #954 is independent and does not wait for or define #586.
- **R2:** Ordinary `e2e:core` keeps the untagged Docker `production` target.
- **R3:** The new gate builds `./cmd/semstreams` with one E2E-only tag, never `cmd/e2e-semstreams`.
- **R4:** The only production-root seam is one hook call plus mutually exclusive tagged/default implementations.
- **R5:** Untagged behavior is a no-op and recognizes no E2E environment, config, subject, or endpoint.
- **R6:** The tagged probe auto-runs in a disposable stack and adds no communication or durable-storage primitive.
- **R7:** The probe receives only the existing client and creates/defaults no client, logger, or handler.
- **R8:** The diagnostic uses the client-captured #961 logger: same configured local handler/common attributes,
  `component=natsclient`, one counter increment, and no same-client forwarding.
- **R9:** The probe gates then delegates the installed callback; it does not duplicate `handleError`.
- **R10:** Fixed pending limits/raw subscription access exist only in the tag, never the public wrapper/config.
- **R11:** Structured stdout proves attribution; the existing counter corroborates exact-one emission.
- **R12:** The scenario reports assertions actually executed on success and failure.
- **R13:** No arbitrary sleep is permitted in fixture, scenario, task, or tests.
- **R14:** The gate runs per PR as a separate short E2E Ladder job, parallel with statistical E2E.
- **R15:** Add an OpenSpec E2E-proof change; no ADR is warranted because production contracts do not change.

## Artifact delta

- `cmd/semstreams/slow_consumer_probe_disabled.go`: untagged no-op hook.
- `cmd/semstreams/slow_consumer_probe_e2e.go`: tagged activation of the internal probe.
- `internal/e2eslowconsumer/contract.go`: private subject, queue, and expected-drop fixture constants.
- `internal/e2eslowconsumer/probe_e2e.go`: tagged one-shot fixture using existing connection callback APIs.
- `cmd/semstreams/main.go`: one explicit hook call using the existing client immediately after connection.
- `test/e2e/scenarios/core_slow_consumer.go`: external JSON-log/metric scenario and dynamic assertion recorder.
- `test/e2e/scenarios/scenario.go`: optional `AssertionsRun` result field.
- `cmd/e2e/main.go`: scenario registration and success/failure assertion-count reporting.
- `docker/Dockerfile`: tagged `cmd/semstreams` builder/final target without altering `production`.
- `docker/compose/e2e-slow-consumer.yml`: isolated NATS plus tagged application stack and unique ports/names.
- `taskfiles/e2e/slow-consumer.yml` and `Taskfile.yml`: build, run, bounded teardown, and direct task registration.
- `.github/workflows/e2e-ladder.yml`: separate short per-PR job.
- `openspec/changes/prove-slow-consumer-attribution-e2e/`: proposal, design, spec delta, tasks, and evidence.

The developer may consolidate file names where repository conventions demand it, but may not change the rulings or
move fixture behavior into an untagged/public surface.

## TDD and mutation plan

1. Add scenario/result tests proving actual assertion counts survive both success and partial failure.
2. Add compile/AST contract tests proving the production target is untagged, the probe target builds
   `cmd/semstreams` with the one tag, and the ordinary hook is a no-op.
3. Add fixture unit tests with explicit channel synchronization for exact drops, unrelated-callback delegation,
   callback restoration, cleanup, and timeout/failure markers.
4. Add JSON-log parser tests with extra unrelated records, missing/falsified attribution, duplicate matches, malformed
   lines, fallback count, and counter mismatch.
5. Run the new E2E green.
6. Mutation proof: remove or falsify subject, queue, error, or dropped attribution in `handleError` and demonstrate the
   new E2E fails before restoring production code.
7. Run focused `-race`, `task lint`, `go test -race ./...`, integration tests, build, schema twice/no drift, contract
   tests, strict OpenSpec validation, ordinary `task e2e:core`, new E2E, and statistical E2E if CI/config was touched.

## Adopter seam result

The outside component developer learns nothing new. Doing nothing retains normal nats.go limits and the existing
attributed local diagnostic. The E2E tag, fixed fixture contract, raw subscription, callback gate, and assertion count
do not exist in the release binary or configuration surface. The production logger remains explicitly constructed and
passed; no hidden default logger instance is introduced.

## OpenSpec target

Change ID: `prove-slow-consumer-attribution-e2e`.

Add an E2E-proof requirement under `nats-client-diagnostics`: a tagged assembly of `cmd/semstreams` SHALL deliberately
overflow a known core-NATS subscription, drive the installed production async-error handler, and externally observe
one configured-local JSON record with exact error, subject, queue, and cumulative dropped count. The proof SHALL be
mutation-sensitive, SHALL report actual assertions executed, and SHALL use bounded explicit synchronization. The
untagged release binary SHALL expose no fixture behavior or control surface.

No ADR: this is reversible test composition and evidence for contracts already fixed by #950 and #961.

## Accepted inventory (verbatim)

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
