package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthServeDoneDoesNotCompleteBeforeServeReturns(t *testing.T) {
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)
	listener := startHealthOnBoundListener(t, manager, t.Context())
	t.Cleanup(func() { _ = manager.StopHealthListener(context.Background()) })
	waitForListener(t, "http://"+listener.Addr().String()+"/healthz", 10*time.Second)

	select {
	case <-manager.healthServeDone:
		t.Fatal("health serveDone closed before Serve returned")
	default:
	}

	if err := manager.StopHealthListener(context.Background()); err != nil {
		t.Fatalf("StopHealthListener: %v", err)
	}
}

func TestManagerHTTPServeDoneDoesNotCompleteBeforeServeReturns(t *testing.T) {
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)
	require.NoError(t, manager.initializeHTTPInfrastructure())
	require.NoError(t, manager.startHTTPRuntime(t.Context()))
	t.Cleanup(func() { _ = manager.stopRuntimeServers(context.Background()) })
	waitForListener(t, observedManagerHTTPURL(t, manager)+"/healthz", 10*time.Second)

	select {
	case <-manager.httpServeDone:
		t.Fatal("manager HTTP serveDone closed before Serve returned")
	default:
	}

	require.NoError(t, manager.stopRuntimeServers(t.Context()))
}

func TestListenerBaseContextsPreserveExactStartValues(t *testing.T) {
	type contextKey string
	const key contextKey = "sm1"
	startCtx := context.WithValue(t.Context(), key, "exact-parent")

	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	require.NoError(t, manager.initializeHTTPInfrastructure())
	require.NoError(t, manager.startHTTPRuntime(startCtx))
	t.Cleanup(func() { _ = manager.stopRuntimeServers(context.Background()) })
	require.Equal(t, "exact-parent", manager.httpServer.BaseContext(manager.httpListener).Value(key))

	healthManager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	startHealthOnBoundListener(t, healthManager, startCtx)
	t.Cleanup(func() { _ = healthManager.StopHealthListener(context.Background()) })
	require.Equal(t, "exact-parent", healthManager.healthServer.BaseContext(healthManager.healthListener).Value(key))
}

func TestListenerStartsRejectCanceledContextBeforeAcquisition(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	require.NoError(t, manager.initializeHTTPInfrastructure())
	require.ErrorIs(t, manager.startHTTPRuntime(canceled), context.Canceled)
	require.False(t, manager.httpUsed)
	require.Nil(t, manager.httpListener)

	healthManager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	called := false
	require.ErrorIs(t, healthManager.startHealthListener(canceled, 1, func(string, string) (net.Listener, error) {
		called = true
		return nil, fmt.Errorf("unexpected acquisition")
	}), context.Canceled)
	require.False(t, called)
	require.False(t, healthManager.healthUsed)
	require.Nil(t, healthManager.healthListener)
	require.Error(t, healthManager.startHealthListener(nil, 1, func(string, string) (net.Listener, error) {
		called = true
		return nil, fmt.Errorf("unexpected acquisition")
	}))
	require.False(t, called)
}

// TestStartHealthListener_BindsHealthAndHealthz verifies that
// StartHealthListener binds /health and /healthz on the requested port
// and routes them to the Manager's existing handler functions. Closes
// the loop on #100 — the -health-port flag is no longer dead code.
func TestStartHealthListener_BindsHealthAndHealthz(t *testing.T) {
	// Pass nil NATS so handleSystemHealth skips the NATS branch — the
	// in-suite mockNATSClient creates an empty natsclient.Client whose
	// internal fields are nil and panic on GetStatus(). Health listener
	// behaviour is orthogonal to NATS readiness anyway; aggregating zero
	// sub-statuses returns a healthy aggregate by the health package's
	// rule (no sub-statuses = trivially healthy).
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)

	listener := startHealthOnBoundListener(t, manager, t.Context())
	t.Cleanup(func() {
		if err := manager.StopHealthListener(context.Background()); err != nil {
			t.Errorf("StopHealthListener cleanup error = %v", err)
		}
	})

	// Binding is synchronous, while Serve runs in a goroutine. Poll until
	// the bound listener serves requests to keep the test deterministic
	// without a wall-clock sleep.
	//
	// gh#209 / gh#220 — budget widened from 3s to 10s. The 3s budget
	// was empirically tight under parallel test load (race-detector
	// goroutine-scheduling overhead + Docker daemon contention from
	// sister tests). 10s is conservative per
	// [[feedback_substrate_flake_discipline]] (wall-clock assertions
	// need ≥3× tolerance over expected). Happy-path completion is
	// fast (test binary finishes in ~0.3s total for 3 health-listener
	// tests) so the wider budget doesn't slow the suite; the budget
	// is the timeout cap, not the expected wall-clock.
	addr := "http://" + listener.Addr().String()
	waitForListener(t, addr+"/healthz", 10*time.Second)

	// /healthz is the liveness probe — should always 200 once the
	// listener is up. No service state required.
	resp, err := http.Get(addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}

	// /health aggregates service + NATS status. The mock NATS reports
	// connected, no services registered, so the aggregate is healthy.
	resp, err = http.Get(addr + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health status = %d, want 200", resp.StatusCode)
	}
}

// TestStartHealthListener_ZeroIsNoOp verifies the default disabled
// path: port 0 (the flag's "0 to disable" sentinel) is a no-op, no
// listener bound, no error returned. Back-compat — operators with the
// flag unset see no change.
func TestStartHealthListener_ZeroIsNoOp(t *testing.T) {
	// Pass nil NATS so handleSystemHealth skips the NATS branch — the
	// in-suite mockNATSClient creates an empty natsclient.Client whose
	// internal fields are nil and panic on GetStatus(). Health listener
	// behaviour is orthogonal to NATS readiness anyway; aggregating zero
	// sub-statuses returns a healthy aggregate by the health package's
	// rule (no sub-statuses = trivially healthy).
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)

	called := false
	if err := manager.startHealthListener(t.Context(), 0, func(string, string) (net.Listener, error) {
		called = true
		return nil, fmt.Errorf("unexpected acquisition")
	}); err != nil {
		t.Errorf("startHealthListener(port 0) error = %v, want nil (no-op)", err)
	}
	require.False(t, called)
	if manager.healthServer != nil {
		t.Error("healthServer should remain nil when port is 0")
	}
	// Stop should also be a no-op when no listener was started.
	if err := manager.StopHealthListener(context.Background()); err != nil {
		t.Errorf("StopHealthListener with no listener error = %v, want nil", err)
	}
}

func TestStartHealthListenerReportsBindFailureSynchronously(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	err = manager.StartHealthListener(t.Context(), port)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bind")
}

func TestStartHTTPRuntimeReportsBindFailureSynchronously(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	manager := createTestServiceManager(ManagerConfig{HTTPPort: port}, createTestServiceDependencies(nil))
	require.NoError(t, manager.initializeHTTPInfrastructure())
	err = manager.startHTTPRuntime(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bind")
}

func TestHealthListenerCannotRebindAfterCompletedStop(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, createTestServiceDependencies(nil))
	startHealthOnBoundListener(t, manager, t.Context())
	require.NoError(t, manager.StopHealthListener(t.Context()))

	called := false
	err := manager.startHealthListener(t.Context(), 1, func(string, string) (net.Listener, error) {
		called = true
		return nil, fmt.Errorf("unexpected acquisition")
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already used")
	require.False(t, called)
}

// TestStartHealthListener_DoubleStartErrors verifies the one-shot
// guard: calling twice with a non-zero port returns an error rather
// than silently re-binding (which would either fail at bind time with
// a confusing OS error or leak the first listener).
func TestStartHealthListener_DoubleStartErrors(t *testing.T) {
	// Pass nil NATS so handleSystemHealth skips the NATS branch — the
	// in-suite mockNATSClient creates an empty natsclient.Client whose
	// internal fields are nil and panic on GetStatus(). Health listener
	// behaviour is orthogonal to NATS readiness anyway; aggregating zero
	// sub-statuses returns a healthy aggregate by the health package's
	// rule (no sub-statuses = trivially healthy).
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)

	startHealthOnBoundListener(t, manager, t.Context())
	t.Cleanup(func() { _ = manager.StopHealthListener(context.Background()) })

	if err := manager.StartHealthListener(context.Background(), 1); err == nil {
		t.Error("second StartHealthListener should error; got nil")
	}
}

// TestStopAll_TearsDownHealthListener verifies the #100 production
// shutdown contract: cmd/semstreams/main.go's shutdown() calls
// manager.StopAll, NOT manager.Stop, so the dedicated health-port
// listener must be torn down by StopAll (otherwise the port stays
// bound until process exit and graceful-drain semantics are broken).
// Reviewer-found blocker on the original PR — keeps regression
// coverage on the path the reviewer was right to flag.
func TestStopAll_TearsDownHealthListener(t *testing.T) {
	deps := createTestServiceDependencies(nil)
	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, deps)

	listener := startHealthOnBoundListener(t, manager, t.Context())

	// Sanity: listener is up before shutdown.
	// gh#209/gh#220: 3s → 10s for the same reason as the sister test.
	addr := "http://" + listener.Addr().String()
	waitForListener(t, addr+"/healthz", 10*time.Second)

	// StopAll is the production shutdown entry point. It must tear
	// down the dedicated health listener as part of its sequence.
	if err := manager.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll error = %v", err)
	}

	// The health listener must be torn down — that is StopAll's contract
	// (#100), and what this test guards. Assert the SIGNAL (the server
	// stopped serving /healthz), NOT OS port release. Re-binding the port
	// is TIME_WAIT-bound — the kernel can hold it for the MSL window (~15s
	// on macOS, up to 120s on some Linux configs), which is not provably
	// ≥3× the old 10s budget and flaked under parallel load — and OS port
	// release is not this test's concern (gh#316). "Listener no longer
	// accepts" is bounded by how fast Shutdown closes the listener, so it
	// is deterministic.
	waitForListenerGone(t, addr+"/healthz", 2*time.Second)
}

func startHealthOnBoundListener(t *testing.T, manager *Manager, ctx context.Context) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	require.NoError(t, manager.startHealthListener(ctx, 1, func(string, string) (net.Listener, error) {
		return listener, nil
	}))
	return listener
}

// waitForListener polls url with short HTTP GETs until one succeeds or
// the budget expires. Avoids brittle sleep-based readiness in tests.
func waitForListener(t *testing.T, url string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		// Short, bounded backoff under the overall budget — not a
		// wall-clock sleep that scales with system pressure.
		select {
		case <-time.After(20 * time.Millisecond):
		case <-context.Background().Done():
			return
		}
	}
	t.Fatalf("listener at %s never came up within %s", url, budget)
}

// waitForListenerGone polls url until a GET FAILS — i.e. the listener stopped
// accepting — or the budget expires. Signal-bound: bounded by how fast the
// server closes its listener on Shutdown, NOT by OS port release (which is
// TIME_WAIT-bound and flaky, gh#316). The complement of waitForListener.
func waitForListenerGone(t *testing.T, url string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			return // no longer serving — teardown confirmed
		}
		_ = resp.Body.Close()
		select {
		case <-time.After(20 * time.Millisecond):
		case <-context.Background().Done():
			return
		}
	}
	t.Fatalf("health listener at %s still serving after StopAll within %s (StopAll did not tear it down)", url, budget)
}
