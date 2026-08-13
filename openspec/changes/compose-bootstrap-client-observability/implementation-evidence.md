# Implementation evidence

This table records implementation evidence for the owner-accepted rulings. No design deviation was taken.

| Ruling | Evidence |
|---|---|
| R1 | `internal/bootstrapobservability/bootstrap.go:132-145` constructs through existing `natsclient.WithLogger` and `natsclient.WithMetrics`; there is no setter, proxy, or deferred handler. |
| R2 | The production profile composes metrics, the caller-supplied local handler, and the WARN/ERROR counter at `internal/bootstrapobservability/bootstrap.go:34-54`; the non-forwarding client graph is derived at `internal/bootstrapobservability/bootstrap.go:100-120`. Exact-one stdout and `natsclient` WARN counter evidence is `internal/bootstrapobservability/bootstrap_test.go:77-97`; active-forwarding non-recursion evidence remains `cmd/semstreams/bootstrap_observability_integration_test.go:130-218`. |
| R3 | The E2E profile explicitly passes no counter at `internal/bootstrapobservability/bootstrap.go:57-73`, and its root selects that profile at `cmd/e2e-semstreams/main.go:111-122`. Its completion helper retains `Steady(nil)` at `cmd/e2e-semstreams/main.go:276-312`. No-counter plus pre-connect metrics evidence is `internal/bootstrapobservability/bootstrap_test.go:100-119`; the actual E2E client entry is covered at `cmd/e2e-semstreams/bootstrap_observability_test.go:15-33`. |
| R4 | Client and config-manager identities are bound at `internal/bootstrapobservability/bootstrap.go:114-119`; production exact `natsclient` identity/counter evidence is `internal/bootstrapobservability/bootstrap_test.go:77-97`, and all common attributes/child identities are asserted at `internal/bootstrapobservability/bootstrap_test.go:122-145`. |
| R5 | Each profile creates metrics before its local graph at `internal/bootstrapobservability/bootstrap.go:34-73`; roots call those profiles before client connection at `cmd/semstreams/main.go:114-131` and `cmd/e2e-semstreams/main.go:111-125`. `NewClient` applies metrics before return at `internal/bootstrapobservability/bootstrap.go:132-145`; metric registration is asserted at `internal/bootstrapobservability/bootstrap_test.go:97` and `internal/bootstrapobservability/bootstrap_test.go:119`. |
| R6 | Shared arbitration returns `Manager.GetConfig().Get()` and shared validation completes at `internal/bootstrapobservability/bootstrap.go:168-210`; effective validation and stream provisioning are `internal/bootstrapobservability/bootstrap.go:213-256`. Production orders validated arbitration, provisioning, then forwarding at `cmd/semstreams/main.go:137-156`; E2E completion orders the same gates before `Steady(nil)` at `cmd/e2e-semstreams/main.go:285-303`. The cross-root AST guard follows these wrapper calls and their selected profile/client/config/logger values through effective validation and the real stream manager, and proves that the arbitrated effective value is both validated and returned, at `internal/maxdelivery/boot_order_test.go:22-123` and `internal/maxdelivery/boot_order_test.go:278-311`. Real KV selection and absent-before/present-before-forwarder LOGS evidence is `cmd/semstreams/bootstrap_observability_integration_test.go:32-98`. |
| R7 | Outer absence/disable returns nil before logger/publisher/policy access at `internal/bootstrapobservability/bootstrap.go:259-285`. The absent and disabled-malformed cases are `internal/bootstrapobservability/bootstrap_test.go:157-160`, with handler-nil assertion at `internal/bootstrapobservability/bootstrap_test.go:189-192`. |
| R8 | CLI/global local level is applied at `internal/bootstrapobservability/bootstrap.go:76-97`; forwarding gets the independently resolved policy level at `internal/bootstrapobservability/bootstrap.go:276-285`; counter presence is isolated to the production profile at `internal/bootstrapobservability/bootstrap.go:34-54`. Default INFO and explicit WARN routing evidence is `internal/bootstrapobservability/bootstrap_test.go:161-169`. |
| R9 | The mandatory exclusion and union/dedup normalization remain `internal/logforwarderpolicy/policy.go:14` and `internal/logforwarderpolicy/policy.go:72-91`. Default, configured union, and empty-list retention evidence is `internal/logforwarderpolicy/policy_test.go:12-46` plus `internal/bootstrapobservability/bootstrap_test.go:170-175`. |
| R10 | Both mains install configured Phase-A logging before client construction at `cmd/semstreams/main.go:114-131` and `cmd/e2e-semstreams/main.go:111-125`. Owned failures are recorded and returned through `internal/bootstrapobservability/bootstrap.go:132-290`; exact-one/cause unit evidence is `internal/bootstrapobservability/bootstrap_test.go:21-75`, root client-create evidence is `cmd/semstreams/bootstrap_observability_test.go:35-49` and `cmd/e2e-semstreams/bootstrap_observability_test.go:35-49`, and real config-manager Start evidence remains `cmd/semstreams/bootstrap_observability_integration_test.go:100-128`. |
| R11 | Both mains call the same plain profile/config helpers: production at `cmd/semstreams/main.go:114-156`; E2E at `cmd/e2e-semstreams/main.go:111-125` and `cmd/e2e-semstreams/main.go:276-312`. Both actual profile-to-client entries are covered by their command-package `bootstrap_observability_test.go:15-33`. The sister-binary guard proves each profile/client/config/stream chain reaches `maxdelivery.Start` before `Manager.StartAll`, excludes the opposite profile, binds the selected E2E metrics into completion, and binds effective services/shared client/selected process logger into the production forwarder before its exact result reaches `Steady`, at `internal/maxdelivery/boot_order_test.go:22-123`. |
| R12 | Client/config-manager loggers derive only from the Phase-A base graph at `internal/bootstrapobservability/bootstrap.go:100-120`; a forwarding destination is added only by `Steady` at `internal/bootstrapobservability/bootstrap.go:123-130`. The real-NATS proof first observes an application sentinel through same-client forwarding, then proves the client diagnostic is local/counted and absent from `logs.>` at `cmd/semstreams/bootstrap_observability_integration_test.go:130-218`. |
| R13 | The new helper packages are repository-internal (`internal/bootstrapobservability/bootstrap.go:1-3`, `internal/logforwarderpolicy/policy.go:1-3`); the existing named public type remains declared at `service/log_forwarder.go:39-55`. No new public framework-package symbol was added. |
| R14 | Capability deltas remain in `specs/application-logging/spec.md` and `specs/service-composition/spec.md`; the change contains no ADR. |
| R15 | The sole decode/default/normalize/validate owner remains `internal/logforwarderpolicy/policy.go:27-91`. Service construction delegates at `service/log_forwarder.go:17-25`, public field validation delegates at `service/log_forwarder.go:57-60`, and boot delegates only after outer activation at `internal/bootstrapobservability/bootstrap.go:259-285`. Resolver behavior is covered at `internal/logforwarderpolicy/policy_test.go:12-53`. |
| R16 | Each explicit binary profile creates exactly one configured local handler and passes it with the common base-attribute slice into `NewPhaseALogging` at `internal/bootstrapobservability/bootstrap.go:34-73`; that constructor derives process/client/config-manager and reuses the same base graph for steady destinations at `internal/bootstrapobservability/bootstrap.go:100-130`. Common service/version/pid attributes and only the required child component identities are asserted at `internal/bootstrapobservability/bootstrap_test.go:122-145`. |

## Reproducible focused evidence

- RED: `go test ./internal/logforwarderpolicy ./internal/bootstrapobservability` failed because `Resolve`,
  `ValidateFields`, `NewLocalHandler`, `NewPhaseALogging`, `NewClient`, and `NewForwardingHandler` were undefined.
- RED mutation check: returning the stale initial config from `StartConfigManager` made
  `TestIntegrationProductionBootstrapObservability/KV-selected_policy_and_effective_streams_precede_forwarding`
  fail at `bootstrap_observability_integration_test.go:64` because the effective forwarder remained disabled. The
  source backup and restored file both had MD5 `7dafa919deda10c0a67db9a1a5288cd6`.
- GREEN: `go test ./internal/logforwarderpolicy ./internal/bootstrapobservability ./cmd/semstreams
  ./cmd/e2e-semstreams` passed.
- GREEN: `go test ./service -run 'TestLogForwarder'` passed.
- GREEN: the same focused package sets passed with `go test -race`.
- GREEN: `go test -tags=integration ./cmd/semstreams -run TestIntegrationProductionBootstrapObservability
  -count=1 -v` passed all three real-NATS subtests.
- GREEN: the same focused integration test passed with `go test -race -tags=integration`.
- GREEN after the Phase-A extraction: `task lint` passed, including `go vet`, `go fmt`, `revive`, fixed-port, and
  request-guard stages.
- GREEN after updating the sister-binary guard for the shared helper chain: `go test -race ./internal/maxdelivery`
  passed while checking actual client connection, effective-config validation, real stream provisioning, steady
  logger composition, MaxDeliver startup, and `Manager.StartAll` ordering in both roots.
- GREEN after the guard dataflow tightening: `TestBootOrderDataflowRejectsMutations` rejects wrong client/config
  logger selection, stale stream config, a different stream client, and a disconnected steady destination at
  `internal/maxdelivery/boot_order_test.go:104-149`.
- GREEN final guard closure: `TestRemainingBootDataflowRejectsMutations` rejects a different E2E metrics registry,
  validation or return of a non-arbitrated config, and wrong services/client/logger inputs to the production
  forwarder at `internal/maxdelivery/boot_order_test.go:173-257`.

Reviewer H1 correction evidence:

- `go test -race ./internal/bootstrapobservability ./cmd/semstreams ./cmd/e2e-semstreams
  ./internal/logforwarderpolicy` passed.
- The final real-NATS suite, including the canceled-context config-manager Start failure, passed normally in 1.151s
  and with `-race` in 2.201s.
- `VerifyJetStreamLimits` is intentionally best-effort and currently returns nil on every observed server failure
  (`config/streams.go:296-353`). `EnsureEffectiveStreams` owns and logs its defensive non-nil return branch, but no
  production limit-verification failure can be induced without adding a test seam or changing that established
  contract. No such seam or behavior change was added.

The deterministic real async diagnostic is NATS's slow-consumer callback, which emits the production `NATS error`
record at ERROR and therefore exercises the WARN+ counter. The narrower `recordFailure` method emits DEBUG and INFO,
so it cannot itself prove the accepted WARN/ERROR counter condition without changing production levels or adding a
test-only seam. No such seam or behavior change was added.

## Final verification and review

- `task lint` passed after the Phase-A extraction brought both `run` functions below the revive statement limit.
- `go test -race ./...` passed after the sister-binary guard was updated to follow and falsify the shared helper
  dataflow rather than stale direct-call names.
- `task test:integration` passed with the integration tag and race detector, including all three production bootstrap
  observability subtests and the repository's Docker-backed packages.
- `task build` passed and produced `bin/semstreams`.
- `task schema:generate` passed twice; `git status --short schemas specs` and `git diff -- schemas specs` remained
  empty after generation, proving deterministic no-drift output.
- `go test ./test/contract/...` passed.
- `openspec validate compose-bootstrap-client-observability --strict` passed.
- `git diff --check` passed.
- `task e2e:core` passed 3/3: `core-health`, `core-dataflow`, and `core-graph-roundtrip`. The assembled dataflow
  observed 36 WebSocket log entries, and teardown removed both containers, the network, and the volume.
- Independent SemStreams implementation review returned `REVIEW PASS`. The reviewer required and verified exact-once
  configured-local boot-failure logging, active-forwarder proof before the no-self-publication assertion, accurate
  R1-R16 citations, and AST dataflow/mutation coverage for both primary binaries.
