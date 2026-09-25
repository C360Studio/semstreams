# Service listener behavioral evidence

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1120-service-listener-tests` (`codex/gh1120-service-listener-tests`). This is the Go implementation handoff; parent owns OpenSpec/docs/PR, full `task check:push`, and review. No commit or push was made here.

## Requirement, violation, observation

The accepted change's `Metrics listeners have explicit ownership` requirement says a caller-bound raw TCP listener transfers only on successful `Server.StartWithListener`, is served without release/reacquire, is released by `Stop`, and makes `Address` report its actual endpoint. Existing `freePort(t)`/`freeMetricsPort(t)`/`freeServerPort(t)` calls in service/metric tests released a port before the production bind and could not prove ownership of the exact listener. The new tests retain ephemeral bound listeners through startup, issue real HTTP requests, check the supplied listener accepted traffic, and check shutdown/rollback closes it. Native `Start` with an in-package port-zero setting separately proves the actual assigned address. Health and pprof tests use private seams to observe reservation and refusal without a same-address rebind assumption. The private Metrics bridge is exercised in both standalone and Manager startup.

PBT decision: this is a finite lifecycle/history and resource-ownership change with specific valid/refused states. Deterministic, synchronized histories and controlled mutation cover the named boundaries; random generation would not add a useful grammar boundary.

## TDD and focused verification

- Pre-TDD test backup: `cp metric/handler_test.go /private/tmp/semstreams-1386-handler-test.pre-tdd`; original and backup MD5 `af678d8e31b3d51956f5f5ffd78ce328`.
- Behavioral red: `go test ./metric -run '^TestServerNativeStartReportsOwnedEphemeralListener$' -count=1` exited 1. The test expected `http://localhost:52263/metrics` after native port-zero startup, but `Address` returned `http://localhost:0/metrics`. It passed after implementation.
- Focused pre-mutation race: `go test -race ./metric ./service -run 'TestServer|TestMetricsRollsBackBoundProviderWhenBaseCommitFails|TestMetricsStopWaitsForStartFinalizationBeforeProviderCleanup|TestStartAllBindsSharedAndMetricsBeforeBlockedService|TestManagerPrebindPublishesConcreteComponentCountsBeforeComponentManagerStart|TestConcreteMetricsLifecycleRetainsRegistrationAndReverseStopOrder|TestMetricsBindFailureClosesSharedAndStartsNoLaterService|TestStartHealthListener|TestHealthListener|TestMaybeStartPProf' -count=1 -timeout=4m` exited 0 (`metric` 1.561s; `service` 2.160s).
- The exact same race command after all restorations exited 0 (`metric` 1.317s; `service` 1.726s), output `/private/tmp/semstreams-1386-focused-race-final.log`.
- `git diff --check -- metric service` exited 0. `rg -n 'freePort\(t\)|freeMetricsPort\(t\)|freeServerPort\(t\)' service metric --glob '*_test.go'` exited 1 with no matches, which is the intended inventory result.
- Focused tests requiring loopback binds were run with approved escalation. One first baseline invocation without escalation failed solely with `listen tcp 127.0.0.1:0: bind: operation not permitted`; it was replaced by the successful escalated baseline in the table below.

## Controlled mutations

Each mutation changed only production source while tests stayed fixed. Before any mutation, `cp` created a backup in `/private/tmp`; after each mutation, `cp` restored it, MD5 matched the pre-mutation source, and the same test passed. All test commands used `-count=1` and `-timeout=1m` with approved loopback access. Exit 1 below means the named intended assertion rejected a valid compiling mutant, not a compile or timeout failure.

| Mutant | Exact source replacement | Focused command | Baseline / mutant / restored | Mutant assertion and logs |
|---|---|---|---|---|
| Close and rebind accepted listener | In `metric/handler.go` after `listener := supplied`, insert `if provided { _ = listener.Close(); var err error; listener, err = net.Listen("tcp", "127.0.0.1:0"); if err != nil { return err } }` | `go test ./metric -run '^TestServerStartWithListenerKeepsOriginalOpenWhileServing$' -count=1 -timeout=1m` | 0 / 1 / 0 | `serving must retain the supplied listener`, `handler_test.go:140`; `/private/tmp/semstreams-1386-mut-accepted-{baseline,mutant,restored}.log` |
| Report configured Address while owning listener | Replace `if s.listener != nil {` with `if false && s.listener != nil {` in `metric/handler.go:Address` | `go test ./metric -run '^TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart$' -count=1 -timeout=1m` | 0 / 1 / 0 | Expected `http://127.0.0.1:<assigned>/metrics`, got `http://localhost:9090/metrics`; `handler_test.go:100`; `/private/tmp/semstreams-1386-mut-address-{baseline,mutant,restored}.log` |
| Omit failed-start shutdown release | Replace `if server != nil {` with `if false && server != nil {` in `service/metrics.go:cleanup` | `go test ./service -run '^TestMetricsRollsBackBoundProviderWhenBaseCommitFails$' -count=1 -timeout=1m` | 0 / 1 / 0 | `failed BaseService commit must release the bound metrics listener`, `metrics_owner_test.go:119`; `/private/tmp/semstreams-1386-mut-release-{baseline,mutant,restored}.log` |
| Bypass standalone private bridge | Replace `m.startServer(runCtx, server)` with `server.Start(runCtx)` in `service/metrics.go:Start` | Same `TestMetricsRollsBackBoundProviderWhenBaseCommitFails` command | 0 / 1 / 0 | Same supplied-listener rollback assertion; baseline is release baseline above, mutant/restored logs `/private/tmp/semstreams-1386-mut-standalone-bridge-{mutant,restored}.log` |
| Bypass Manager private bridge | Replace `service.startServer(ctx, server)` with `server.Start(ctx)` in `service/service_manager.go:startStartupMetricsServer` | `go test ./service -run '^TestStartAllBindsSharedAndMetricsBeforeBlockedService$' -count=1 -timeout=1m` | 0 / 1 / 0 | Expected supplied `127.0.0.1:<assigned>` URL, got `localhost:9090`; `startup_observability_test.go:591`; `/private/tmp/semstreams-1386-mut-manager-bridge-{baseline,mutant,restored}.log` |

Backups and matching original/restored MD5:

| Source | Backup | MD5 before and after |
|---|---|---|
| `metric/handler.go` | `/private/tmp/semstreams-1386-handler.before-mutants.go` | `18319da0f541175af03067f7622077d1` |
| `service/metrics.go` | `/private/tmp/semstreams-1386-metrics.before-mutants.go` | `13a8724ddebb044e56b73f43cd12c574` |
| `service/service_manager.go` | `/private/tmp/semstreams-1386-service-manager.before-mutants.go` | `9f471b42c1dd5903aca4f5668354d026` |

Historical first-run SHA-256 snapshot (superseded by the final-byte snapshot below):

```
50219ceaaa73542ba7c3f3ccb6482d680926b758a3f00a26879fd08bf6ed8f13  metric/handler.go
c580d751c76c951781e51df342864546c8531119b3e2b18d83cf83365aa3c7e0  metric/handler_test.go
912e1411cb8d641fab55303f856cce5ddf4b11ca62cca995bea6a816970ad6e2  service/metrics.go
8a6929843ab59feba3be5bae7bac06fa2b2daabf0dc5e0274c59f22b3184575a  service/metrics_owner_test.go
d6daf472145ff1e51f011fb834bec342b4b33fa3004914ed920d63f422c86eb9  service/pprof.go
d0669bcde76bfa8571366de2fc484e41af96d9b0aac38555bf6c7b2ce2fc4423  service/pprof_test.go
61f263462084783d1dd8baac87f99bb5254444a729849424ef5a35d25e26805d  service/service_manager.go
58128707fd51e1512f8f3308a9d33d6ca210adc3bab4aa348bafff6a62f216f0  service/service_manager_health_listener_test.go
4a06fa7252bf86153ae869cdd5f81e4132e2c6ea360096248a5bd95c1dd2a207  service/startup_observability_amendment_test.go
2dfefb96995505ef2d91cb3097dc778e52340d68f80b6e6babaf628b87447f60  service/startup_observability_test.go
```

Mutation logs often contain empty output for passing tests because stdout/stderr were redirected. Their command exit codes above were captured from `exec_command` results; no inferred pass from empty logs. The parent still owns the full gate and independent implementation review.

## Review correction and final-byte recheck (2026-09-25)

The shared Manager HTTP listener binds wildcard `:0`, so `observedManagerHTTPURL` now reads its assigned TCP port and builds `net.JoinHostPort("127.0.0.1", port)`. `TestManagerHTTPServeDoneDoesNotCompleteBeforeServeReturns` uses that helper. `TestMetricsBindFailureClosesSharedAndStartsNoLaterService` retains the listener and server under `manager.mu.RLock` before deriving the loopback dial address. Newly edited metric tests now issue requests with `http.NewRequestWithContext(t.Context(), ...)` and a two-second `http.Client.Timeout`; their terminal cleanup uses a five-second bounded Background context. The two collector-blocked metric tests release the collector gate before cleanup joins; the blocked standalone Metrics Start test releases its gate before joining Start and stopping. `beginObservedManagerStart` already released its gate before joining.

Focused correction check: `go test -race ./service -run '^(TestStartAllBindsSharedAndMetricsBeforeBlockedService|TestMetricsBindFailureClosesSharedAndStartsNoLaterService|TestManagerPrebindPublishesConcreteComponentCountsBeforeComponentManagerStart|TestConcreteMetricsLifecycleRetainsRegistrationAndReverseStopOrder|TestManagerHTTPServeDoneDoesNotCompleteBeforeServeReturns)$' -count=1 -timeout=4m` exited 0; `/private/tmp/semstreams-1386-shared-url-race.log`.

The same five valid mutation operations in the table above were repeated against the corrected test bytes. For each command, `-count=1 -timeout=1m` and the same named test regex from the original table were used, with `-v2-{baseline,mutant,restored}.log` suffixes. For standalone bridge, the release-v2 baseline was reused as the identical test/source baseline, and its own mutant/restored logs were captured. Results and intended assertions:

| Mutation | Final-byte baseline / mutant / restored | Intended failure |
|---|---|---|
| Accepted listener close/rebind | 0 / 1 / 0 | `serving must retain the supplied listener`; `/private/tmp/semstreams-1386-mut-accepted-v2-{baseline,mutant,restored}.log` |
| Configured instead of observed Address | 0 / 1 / 0 | Expected assigned loopback port, got `localhost:9090`; `/private/tmp/semstreams-1386-mut-address-v2-{baseline,mutant,restored}.log` |
| Omitted failed-start release | 0 / 1 / 0 | `failed BaseService commit must release the bound metrics listener`; `/private/tmp/semstreams-1386-mut-release-v2-{baseline,mutant,restored}.log` |
| Standalone bridge bypass | 0 / 1 / 0 | Same supplied-listener rollback assertion; release-v2 baseline and `/private/tmp/semstreams-1386-mut-standalone-bridge-v2-{mutant,restored}.log` |
| Manager bridge bypass | 0 / 1 / 0 | Expected supplied loopback metrics endpoint, got `localhost:9090`; `/private/tmp/semstreams-1386-mut-manager-bridge-v2-{baseline,mutant,restored}.log` |

`metric/handler.go`, `service/metrics.go`, and `service/service_manager.go` were restored with `cp` from the original backup paths above after each operation; their final MD5 values still match `18319da0f541175af03067f7622077d1`, `13a8724ddebb044e56b73f43cd12c574`, and `9f471b42c1dd5903aca4f5668354d026`, respectively. The original ten-file snapshot above is historical for the first mutation run; the final-byte snapshot is:

```
50219ceaaa73542ba7c3f3ccb6482d680926b758a3f00a26879fd08bf6ed8f13  metric/handler.go
078d52a95935cdfbbdccf24023f577fb87aa0f97b00c21b905290417907ccdf4  metric/handler_test.go
912e1411cb8d641fab55303f856cce5ddf4b11ca62cca995bea6a816970ad6e2  service/metrics.go
76df015c5b141edb08d24322e4c3e41b48aea9091e5598fbeff09b2a412f86f1  service/metrics_owner_test.go
d6daf472145ff1e51f011fb834bec342b4b33fa3004914ed920d63f422c86eb9  service/pprof.go
d0669bcde76bfa8571366de2fc484e41af96d9b0aac38555bf6c7b2ce2fc4423  service/pprof_test.go
61f263462084783d1dd8baac87f99bb5254444a729849424ef5a35d25e26805d  service/service_manager.go
1b26a965b5af7074be8d8ede96ba1422ddc83a84c634c24b93b999c253f0b13f  service/service_manager_health_listener_test.go
4a06fa7252bf86153ae869cdd5f81e4132e2c6ea360096248a5bd95c1dd2a207  service/startup_observability_amendment_test.go
3666efe40ac5eb156acdfaf2472b087ca77042b030bcf7f272fe2be5ce383294  service/startup_observability_test.go
```

Final focused race command: `go test -race ./metric ./service -run 'TestServer|TestMetricsRollsBackBoundProviderWhenBaseCommitFails|TestMetricsStopWaitsForStartFinalizationBeforeProviderCleanup|TestStartAllBindsSharedAndMetricsBeforeBlockedService|TestManagerPrebindPublishesConcreteComponentCountsBeforeComponentManagerStart|TestConcreteMetricsLifecycleRetainsRegistrationAndReverseStopOrder|TestMetricsBindFailureClosesSharedAndStartsNoLaterService|TestManagerHTTPServeDoneDoesNotCompleteBeforeServeReturns' -count=1 -timeout=4m` exited 0 (`metric` 1.320s, `service` 1.700s); `/private/tmp/semstreams-1386-focused-race-final-v2.log`. `git diff --check -- metric service` exited 0.

## Retained final-run output

Environment: Go 1.26.4, darwin/arm64, macOS 26.5.2, Task 3.51.1, OpenSpec 1.7.0.
Source baseline: `b7970702dc49c0fc028d3c500ffe39fe3b14d65d` plus the ten-file final snapshot above.
The implementation commit containing this record retains those exact source and test bytes.
Local scratch paths above identify execution artifacts; the decisive final-run output is retained here durably.
Machine-specific source prefixes are removed, tabs expanded, and trailing whitespace trimmed for Markdown.

### accepted

baseline (exit 0):

```text
ok      github.com/c360studio/semstreams/metric 0.369s
```

mutant (exit 1):

```text
--- FAIL: TestServerStartWithListenerKeepsOriginalOpenWhileServing (0.00s)
    handler_test.go:163:
            Error Trace:    metric/handler_test.go:163
            Error:          Should be false
            Test:           TestServerStartWithListenerKeepsOriginalOpenWhileServing
            Messages:       serving must retain the supplied listener
FAIL
FAIL    github.com/c360studio/semstreams/metric 0.359s
FAIL
```

restored (exit 0):

```text
ok      github.com/c360studio/semstreams/metric 0.234s
```

### address

baseline (exit 0):

```text
ok      github.com/c360studio/semstreams/metric 0.241s
```

mutant (exit 1):

```text
--- FAIL: TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart (0.00s)
    handler_test.go:122:
            Error Trace:    metric/handler_test.go:122
            Error:          Not equal:
                            expected: "http://127.0.0.1:57157/metrics"
                            actual  : "http://localhost:9090/metrics"

                            Diff:
                            --- Expected
                            +++ Actual
                            @@ -1 +1 @@
                            -http://127.0.0.1:57157/metrics
                            +http://localhost:9090/metrics
            Test:           TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart
FAIL
FAIL    github.com/c360studio/semstreams/metric 0.354s
FAIL
```

restored (exit 0):

```text
ok      github.com/c360studio/semstreams/metric 0.249s
```

### release

baseline (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.566s
```

mutant (exit 1):

```text
2026/09/25 18:55:23 INFO Starting metrics server port=9090 path=/metrics
--- FAIL: TestMetricsRollsBackBoundProviderWhenBaseCommitFails (0.00s)
    metrics_owner_test.go:119:
            Error Trace:    service/metrics_owner_test.go:119
            Error:          An error is expected but got nil.
            Test:           TestMetricsRollsBackBoundProviderWhenBaseCommitFails
            Messages:       failed BaseService commit must release the bound metrics listener
FAIL
FAIL    github.com/c360studio/semstreams/service    0.567s
FAIL
```

restored (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.455s
```

### standalone-bridge

baseline (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.566s
```

mutant (exit 1):

```text
2026/09/25 18:55:56 INFO Starting metrics server port=9090 path=/metrics
--- FAIL: TestMetricsRollsBackBoundProviderWhenBaseCommitFails (0.00s)
    metrics_owner_test.go:119:
            Error Trace:    service/metrics_owner_test.go:119
            Error:          An error is expected but got nil.
            Test:           TestMetricsRollsBackBoundProviderWhenBaseCommitFails
            Messages:       failed BaseService commit must release the bound metrics listener
FAIL
FAIL    github.com/c360studio/semstreams/service    0.560s
FAIL
```

restored (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.447s
```

### manager-bridge

baseline (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.577s
```

mutant (exit 1):

```text
2026/09/25 18:51:00 ERROR Manager.StartAll: Failed to start service name=component-manager error="context canceled"
--- FAIL: TestStartAllBindsSharedAndMetricsBeforeBlockedService (0.00s)
    startup_observability_test.go:594:
            Error Trace:    service/startup_observability_test.go:594
            Error:          Not equal:
                            expected: "http://127.0.0.1:56992/metrics"
                            actual  : "http://localhost:9090/metrics"

                            Diff:
                            --- Expected
                            +++ Actual
                            @@ -1 +1 @@
                            -http://127.0.0.1:56992/metrics
                            +http://localhost:9090/metrics
            Test:           TestStartAllBindsSharedAndMetricsBeforeBlockedService
FAIL
FAIL    github.com/c360studio/semstreams/service    0.575s
FAIL
```

restored (exit 0):

```text
ok      github.com/c360studio/semstreams/service    0.439s
```

### Final focused race

```text
ok      github.com/c360studio/semstreams/metric 1.320s
ok      github.com/c360studio/semstreams/service    1.700s
```

## Lint argument-order correction

The full gate's revive pass found `context-as-argument` in the test-only `startHealthOnBoundListener` helper. Its signature is now `(ctx context.Context, t *testing.T, manager *Manager)` and its six call sites pass context first. Only `service/service_manager_health_listener_test.go` changed in this correction; mutation-target source and test bytes above are unchanged. New SHA-256 for this file is `8f9209c2680d4a121a7b0ed2bf3b5326ef40b720158e11db22ae7e1996c758e3` (replacing the one entry in the final-byte snapshot above). `go test -race ./service -run '^(TestHealthServeDoneDoesNotCompleteBeforeServeReturns|TestListenerBaseContextsPreserveExactStartValues|TestStartHealthListener_BindsHealthAndHealthz|TestHealthListenerCannotRebindAfterCompletedStop|TestStartHealthListener_DoubleStartErrors|TestStopAll_TearsDownHealthListener)$' -count=1 -timeout=3m` exited 0 (`service` 1.756s); `/private/tmp/semstreams-1386-health-arg-order-race.log`. `git diff --check -- service/service_manager_health_listener_test.go` exited 0. Full gate retry remains parent-owned.

## Scoped IPv6 Address review correction

The independent reviewer found that concatenating a raw `TCPAddr.Zone` into a URL host yielded e.g. `http://[fe80::1%en0]:port/metrics`, which Go's URL parser rejects. The new deterministic test uses a real loopback-bound listener with a wrapper reporting a `TCPAddr` containing `fe80::1`, its actually assigned port, and zone `en0`; it does not require a host link-local interface. `TestServerAddressEscapesScopedIPv6Zone` checks exact `%25` URL escaping, parsed hostname/port, and standard HTTP request construction. `Server.Address` now serializes `url.URL{Scheme, Host: net.JoinHostPort(...), Path}` so the zone is escaped while ordinary scheme/path/fallback tests continue to pass.

- TDD backup: `cp metric/handler_test.go /private/tmp/semstreams-1386-handler-test.pre-ipv6.go` (original and backup MD5 `862a602a2739badd076036aba3e8dcf5`), and `cp metric/handler.go /private/tmp/semstreams-1386-handler.pre-ipv6.go` (MD5 `18319da0f541175af03067f7622077d1`).
- Red: `go test ./metric -run '^TestServerAddressEscapesScopedIPv6Zone$' -count=1 -timeout=1m` exited 1 at the exact URL assertion: expected `%25en0`, actual `%en0`; `/private/tmp/semstreams-1386-ipv6-red.log`.
- Green: the same command exited 0 after `net/url.URL` serialization; `/private/tmp/semstreams-1386-ipv6-green.log`.
- Focused race: `go test -race ./metric -run '^TestServer' -count=1 -timeout=3m` exited 0 both before and after mutation restoration; `/private/tmp/semstreams-1386-ipv6-race.log`, `/private/tmp/semstreams-1386-ipv6-race-restored.log` (restored run `metric` 1.342s).
- Address mutation post-fix: `cp metric/handler.go /private/tmp/semstreams-1386-handler.post-ipv6.before-address-mutant.go` with equal MD5 `402a77ca372271a60309ed73a61082c6`. Replaced `if s.listener != nil {` with `if false && s.listener != nil {`. `go test ./metric -run '^TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart$|^TestServerAddressEscapesScopedIPv6Zone$' -count=1 -timeout=1m` exited baseline 0, mutant 1 (both scoped and ordinary assigned-endpoint assertions failed against `localhost:9090`), restored 0. Logs: `/private/tmp/semstreams-1386-mut-address-v3-{baseline,mutant,restored}.log`. Restored using `cp` from the post-fix backup; MD5 returned to `402a77ca372271a60309ed73a61082c6`.
- Final source SHA-256: `metric/handler.go` `9df65e3bc6798937f9f32f572807b3bd5d6386f6c45e2d229d7535dd9b0b7b83`; test SHA-256: `metric/handler_test.go` `3469360c62b4baae9fbd35a1805da0509eeedb698fe6c58972141d0890006252`. These replace the two metric entries in the previous snapshot; other files and earlier unrelated mutation comparisons are unchanged. `git diff --check -- metric/handler.go metric/handler_test.go` exited 0.

### Retained scoped-IPv6 correction output

ipv6-red:

```text
--- FAIL: TestServerAddressEscapesScopedIPv6Zone (0.00s)
    handler_test.go:136:
            Error Trace:    metric/handler_test.go:136
            Error:          Not equal:
                            expected: "http://[fe80::1%25en0]:58623/metrics"
                            actual  : "http://[fe80::1%en0]:58623/metrics"

                            Diff:
                            --- Expected
                            +++ Actual
                            @@ -1 +1 @@
                            -http://[fe80::1%25en0]:58623/metrics
                            +http://[fe80::1%en0]:58623/metrics
            Test:           TestServerAddressEscapesScopedIPv6Zone
FAIL
FAIL    github.com/c360studio/semstreams/metric 0.435s
FAIL
```

ipv6-green:

```text
ok      github.com/c360studio/semstreams/metric 0.348s
```

mut-address-v3-baseline:

```text
ok      github.com/c360studio/semstreams/metric 0.245s
```

mut-address-v3-mutant:

```text
--- FAIL: TestServerAddressEscapesScopedIPv6Zone (0.00s)
    handler_test.go:136:
            Error Trace:    metric/handler_test.go:136
            Error:          Not equal:
                            expected: "http://[fe80::1%25en0]:58701/metrics"
                            actual  : "http://localhost:9090/metrics"

                            Diff:
                            --- Expected
                            +++ Actual
                            @@ -1 +1 @@
                            -http://[fe80::1%25en0]:58701/metrics
                            +http://localhost:9090/metrics
            Test:           TestServerAddressEscapesScopedIPv6Zone
--- FAIL: TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart (0.00s)
    handler_test.go:153:
            Error Trace:    metric/handler_test.go:153
            Error:          Not equal:
                            expected: "http://127.0.0.1:58702/metrics"
                            actual  : "http://localhost:9090/metrics"

                            Diff:
                            --- Expected
                            +++ Actual
                            @@ -1 +1 @@
                            -http://127.0.0.1:58702/metrics
                            +http://localhost:9090/metrics
            Test:           TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart
FAIL
FAIL    github.com/c360studio/semstreams/metric 0.361s
FAIL
```

mut-address-v3-restored:

```text
ok      github.com/c360studio/semstreams/metric 0.247s
```

ipv6-race-restored:

```text
ok      github.com/c360studio/semstreams/metric 1.342s
```
