# Readiness health-initialization flake design

Status: **ACCEPTED — owner approved 2026-08-23**

Production baseline: `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`

Accepted inventory: `docs/proposals/readiness-health-initialization-flake-inventory.md`, SHA-256
`dcd3b6ddc814134d8a18d4bb3ff8aaec98e43e7bee71b66f570b7dd23551f5c7`, independent `INVENTORY PASS`.

Owner acceptance: all five rulings in this artifact were explicitly approved on 2026-08-23. The recommended test-only
causal synchronization and mandatory production-shaped companion contract test are binding implementation scope.

## Additional production-shaped measurement

An ephemeral Go overlay added a temporary `package service` test outside the repository. The harness used a real
`Manager`, concrete `ComponentManager`, production `cm.SetHealthCheck(cm.healthCheck)`, real `Manager.StartAll`, no NATS
dependency/components/metrics, an immediate `manager.currentStartupSnapshot()` read, 100 independent start/stop
attempts, and the race detector. The repository was not modified.

```text
go test -race -overlay=<temporary-overlay> ./service \
  -run '^TestProductionStartAllReadinessWindowMeasurement$' -count=1 -v

immediate post-StartAll not-ready: 0/100
```

```text
GOMAXPROCS=1 go test -race -overlay=<temporary-overlay> ./service \
  -run '^TestProductionStartAllReadinessWindowMeasurement$' -count=1 -v

immediate post-StartAll not-ready: 100/100
```

The ordering window crosses the real composition root and is scheduler-dependent. The measurement does not establish
that the 503 is incorrect. Current truth makes boot commitment necessary but not sufficient; readiness also requires
current health for every admitted service and component (`openspec/specs/framework-composition/spec.md:152-173`,
`service/service_manager.go:1776-1861`).

## Measured premises

1. `BaseService` begins unhealthy and does not claim health before a check completes
   (`service/base.go:82-105,196-199`).
2. `BaseService.Start` publishes running and launches the health monitor without waiting for its first evaluation
   (`service/base.go:249-280,395-448`).
3. `ComponentManager.Start` returns after child and base-service startup without a first-health barrier
   (`service/component_manager.go:338-397`).
4. `Manager.StartAll` can therefore commit while a service's first asynchronous health observation is pending.
5. `/readyz` correctly renders 503 whenever the committed snapshot contains an unhealthy service
   (`service/service_manager.go:1776-1861`).
6. Exported `HealthCheckFunc` is `func() error`, with no context/cancellation contract
   (`service/base.go:76-77,165-170`).
7. External authors can install arbitrary health functions through `WithHealthCheck` and `SetHealthCheck`; the
   framework cannot assume they are prompt or non-blocking (`service/base.go:165-170,352-359`).
8. Documentation distinguishes TCP reachability from readiness and requires current health
   (`service/README.md:297-306`, `service/doc.go:124-150`).
9. The failing test intends to prove healthy non-lifecycle Discoverable aggregation but does not establish the
   enclosing `ComponentManager` service-health precondition (`service/startup_observability_test.go:190-225`).

## Options

### Option 1: Do nothing

No compatibility risk, but the test remains scheduler-sensitive and fails deterministically under `GOMAXPROCS=1`.
Not recommended.

### Option 2: Test-only causal synchronization

Preserve runtime behavior. Amend the test to observe `ComponentManager` becoming healthy before asserting readiness:

1. Install `OnHealthChange` before `cm.Start`.
2. Close a `sync.Once`-guarded channel when the callback reports `healthy == true`.
3. Start the component manager.
4. Wait on that channel with a finite two-second test-safety bound. The bound only fails a wedged test; it does not
   determine readiness or introduce runtime polling.
5. Assert `cm.IsHealthy()`.
6. Record service completion and commitment.
7. Retain the child-healthy and child-unhealthy readiness assertions.

`performHealthCheck` stores health before invoking the callback (`service/base.go:415-448`), so this is causal proof.

Add a companion deterministic production-shaped contract test:

- wrap real `cm.healthCheck` with a channel gate;
- before `StartAll`, register cleanup that first invokes a `sync.Once`-guarded release and then calls
  `Manager.StopAll` with a bounded terminal context;
- register a post-store `OnHealthChange(true)` signal;
- call and await real `Manager.StartAll`, then assert `manager.bootCommitted.Load()`;
- await the health-check-entered signal with the finite test-safety bound;
- prove readiness is 503 while the observation remains blocked;
- call the guarded release, await the post-store healthy callback with the finite bound, assert `cm.IsHealthy()`, and
  prove readiness transitions to 200.

Use no sleeps, polling, or arbitrary readiness deadline.

The cleanup release is registered before `StartAll`, so an earlier failure cannot strand the health monitor and hang
joined cleanup. `t.Context()` alone is not a failed-wait bound because it is canceled only when the test is ending.

Benefits: directly fixes the missing precondition, preserves fail-closed readiness and asynchronous monitoring, and
adds deterministic proof for the production window without new public/production surface. Cost: an immediate caller
can still observe transient 503, which the owner must explicitly accept as intended behavior.

This is the recommended option.

### Option 3: Make the first BaseService check synchronous

This would make successful Start imply one observation, but a blocking adopter callback could make Start unbounded and
uncancellable because `HealthCheckFunc` has no context. Calling `performHealthCheck` under current startup locking can
deadlock, concrete services would check earlier relative to acquisition, every external adopter would inherit startup
latency, and monitor ownership/specification would change. Not recommended for a test whose expected runtime state is
already compatible with current truth.

### Option 4: Delay Manager commitment until first health observation

`Service` exposes current health but not initial-observation completion. Adding an interface bills adopters; polling
violates the no-guessed-readiness rule; waiting for healthy conflates startup and recovery; and deadlines predict
adopter-owned duration. Not recommended.

### Option 5: Special-case ComponentManager

This is smaller than a global BaseService change but leaves other services exposed, predicts health if seeded,
duplicates callbacks/checks if evaluated directly, and creates a special case in shared ownership. Not recommended.

## Proposed target contract

Subject to owner approval:

1. `StartAll` success means admitted startup calls and Manager acquisitions completed and boot committed.
2. It does not promise that every asynchronous health monitor completed its first observation.
3. `/readyz` remains fail-closed at 503 until every admitted service and component is currently healthy.
4. A health state not yet observed remains not healthy; no optimistic initial health is added.
5. Tests requiring healthy state explicitly and causally establish it.
6. Tests use signals/callbacks, never sleeps, polling loops, or scheduler assumptions.

This preserves current OpenSpec and documented runtime semantics.

## Adopter seam inventory

Specific adopter: a developer outside this repository embedding `BaseService` and providing a custom health check.

- **What must they know?** Nothing new. TCP reachability or successful startup does not replace `/readyz`; the health
  check remains asynchronous and does not lengthen Start.
- **What happens if they do nothing?** Existing behavior remains. Readiness is 503 until successful current health is
  observed, and standard probes naturally retry.
- **Where do they find out?** `service/README.md:297-306`, `service/doc.go:124-150`, and
  `openspec/specs/framework-composition/spec.md:152-173`.
- **What should they know?** Ideally only that `/readyz` is authoritative. They do not predict scheduling or sleep
  after startup.
- **How is silent failure surfaced?** An honest 503 plus `/services`, `/health`, and startup metrics—not optimistic
  200. The deterministic contract test preserves that distinction.

Compatibility: no exported symbol, constructor, interface, configuration, context, or sister-repository change.

## Context ownership audit

The recommended option changes no production context flow, stores no context, creates no production root, and
introduces no detached work. Callback synchronization uses channels and `sync.Once`; finite test-only safety bounds
fail wedged signals rather than determine readiness.

Cleanup `Stop` uses `context.WithTimeout(context.Background(), 2*time.Second)` as a bounded terminal context. Cleanup
must not feed a canceled `t.Context()` into Stop or use unbounded `context.WithoutCancel`.

The rejected synchronous options are especially problematic because exported `HealthCheckFunc` cannot receive
cancellation.

## Exact artifacts

Recommended implementation:

- Modify `service/startup_observability_test.go` to causally observe initial ComponentManager health in
  `TestReadinessIncludesHealthyNonLifecycleDiscoverables` and add the deterministic real-`StartAll`
  initial-health-lag contract test.
- Add `docs/proposals/readiness-health-initialization-flake-conformance.md` after implementation.
- Production files: none.
- OpenSpec/ADR: none if the owner confirms this interpretation. If `StartAll` must imply immediate 200, this design is
  insufficient and a new lifecycle design is required.

## Verification

```bash
GOMAXPROCS=1 go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

go test -race ./service
go test -race ./...
task test:integration
task lint
task schema:generate
git diff --exit-code -- schemas/ specs/
go test ./test/contract/...
```

This changes no product behavior, payload, persistence, or cross-component dataflow, so it creates no new E2E
obligation. The next tag's exact-candidate E2E proof remains a separate release gate.

## Owner decisions required

1. Confirm successful `Manager.StartAll` does not guarantee immediate `/readyz` 200 while initial health is pending.
2. Confirm 503 during that interval is honest fail-closed behavior, not a production defect.
3. Approve test-only causal synchronization.
4. Decide whether the companion production-shaped contract test is mandatory. Recommendation: yes.
5. Confirm no OpenSpec delta or new E2E tier is required for this test-only correction.

No shared decision skill triggers: this adds no communication path, payload, remote operation, or orchestration owner.

## Appendix: durable production measurement

Measured checkout:

```text
HEAD: 35a64ee19ad86f14bd2a1fc6fe0b39984e169a35
production baseline: 774c85dcf75bdce242f1f15ee2a5a310991ecf0d
git diff --quiet 774c85dc..35a64ee1 -- service
exit status: 0
Go: go1.26.4 darwin/arm64
```

The measured `service/` production tree was identical to baseline.

Overlay mapping:

```json
{
  "Replace": {
    "/Users/coby/Code/c360/semstreams/service/readiness_measure_test.go": "/private/tmp/semstreams-readiness-measure-v3/readiness_measure_test.go"
  }
}
```

Complete harness:

```go
package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/stretchr/testify/require"
)

func TestProductionStartAllReadinessWindowMeasurement(t *testing.T) {
	const attempts = 100
	const attemptSafetyBound = 5 * time.Second
	notReady := 0
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for range attempts {
		func() {
			cm := &ComponentManager{
				BaseService: NewBaseServiceWithOptions(
					"component-manager", nil, WithLogger(logger),
				),
				components:    make(map[string]*component.ManagedComponent),
				registry:      component.NewRegistry(),
				storeRegistry: storeregistry.New(),
				storeProvided: make(map[string][]string),
			}
			cm.initialized.Store(true)
			cm.SetHealthCheck(cm.healthCheck)

			manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, nil)
			manager.BaseService = NewBaseServiceWithOptions(
				"service-manager", nil, WithLogger(logger),
			)
			require.NoError(t, manager.RegisterInstance("component-manager", cm))

			startCtx, startCancel := context.WithTimeout(
				t.Context(), attemptSafetyBound,
			)
			defer startCancel()
			require.NoError(t, manager.StartAll(startCtx))

			if manager.currentStartupSnapshot().Status != "ready" {
				notReady++
			}

			stopCtx, stopCancel := context.WithTimeout(
				context.Background(), attemptSafetyBound,
			)
			stopErr := manager.StopAll(stopCtx)
			stopCancel()
			require.NoError(t, stopErr)
		}()
	}

	t.Logf(
		"immediate post-StartAll not-ready observations: %d/%d",
		notReady,
		attempts,
	)
}
```

Commands and corrected-harness results:

```text
go test -race \
  -overlay=/private/tmp/semstreams-readiness-measure-v3/overlay.json \
  ./service \
  -run '^TestProductionStartAllReadinessWindowMeasurement$' \
  -count=1 -v

=== RUN   TestProductionStartAllReadinessWindowMeasurement
    readiness_measure_test.go:49: immediate post-StartAll not-ready observations: 0/100
--- PASS: TestProductionStartAllReadinessWindowMeasurement (0.04s)
PASS
ok github.com/c360studio/semstreams/service 1.544s
```

```text
GOMAXPROCS=1 go test -race \
  -overlay=/private/tmp/semstreams-readiness-measure-v3/overlay.json \
  ./service \
  -run '^TestProductionStartAllReadinessWindowMeasurement$' \
  -count=1 -v

=== RUN   TestProductionStartAllReadinessWindowMeasurement
    readiness_measure_test.go:49: immediate post-StartAll not-ready observations: 100/100
--- PASS: TestProductionStartAllReadinessWindowMeasurement (0.04s)
PASS
ok github.com/c360studio/semstreams/service 1.482s
```

The per-attempt closure keeps the StartAll parent live through the immediate snapshot and bounded StopAll; deferred
cancellation runs only after StopAll returns. Results are 0/100 under the default scheduler and deterministic 100/100
under `GOMAXPROCS=1`.

The permanent companion test likewise retains the exact StartAll parent through commitment assertion, blocked-health
503, health release, post-store callback, healthy 200, and bounded StopAll. Its pre-StartAll cleanup releases the gate,
performs bounded StopAll, and only then cancels the lifecycle parent.
