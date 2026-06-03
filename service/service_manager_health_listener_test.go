package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

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

	port := freePort(t)
	if err := manager.StartHealthListener(port); err != nil {
		t.Fatalf("StartHealthListener(%d) error = %v", port, err)
	}
	t.Cleanup(func() {
		if err := manager.StopHealthListener(); err != nil {
			t.Errorf("StopHealthListener cleanup error = %v", err)
		}
	})

	// Wait briefly for the listener to come up. ListenAndServe is async
	// in a goroutine; we poll until the port is reachable to keep the
	// test deterministic without a wall-clock sleep.
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
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
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

	if err := manager.StartHealthListener(0); err != nil {
		t.Errorf("StartHealthListener(0) error = %v, want nil (no-op)", err)
	}
	if manager.healthServer != nil {
		t.Error("healthServer should remain nil when port is 0")
	}
	// Stop should also be a no-op when no listener was started.
	if err := manager.StopHealthListener(); err != nil {
		t.Errorf("StopHealthListener with no listener error = %v, want nil", err)
	}
}

// TestStartHealthListener_DoubleStartErrors verifies the idempotency
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

	port := freePort(t)
	if err := manager.StartHealthListener(port); err != nil {
		t.Fatalf("first StartHealthListener error = %v", err)
	}
	t.Cleanup(func() { _ = manager.StopHealthListener() })

	if err := manager.StartHealthListener(port); err == nil {
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

	port := freePort(t)
	if err := manager.StartHealthListener(port); err != nil {
		t.Fatalf("StartHealthListener(%d) error = %v", port, err)
	}

	// Sanity: listener is up before shutdown.
	// gh#209/gh#220: 3s → 10s for the same reason as the sister test.
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForListener(t, addr+"/healthz", 10*time.Second)

	// StopAll is the production shutdown entry point. It must tear
	// down the dedicated health listener as part of its sequence.
	if err := manager.StopAll(5 * time.Second); err != nil {
		t.Fatalf("StopAll error = %v", err)
	}

	// Port should be free now — re-binding it succeeds. Poll briefly
	// since Shutdown returns once the listener stops accepting but the
	// OS may take a moment to release the port (TIME_WAIT etc.).
	// gh#209/gh#220: 3s → 10s. The TIME_WAIT-release window under
	// heavy parallel load can stretch — 10s gives enough headroom.
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		// gh#220:allow-fixed-port — rebind to the previously-allocated
		// ephemeral port is the test's load-bearing semantic (verifying
		// the OS released it). The port itself came from freePort(t),
		// so this is NOT a static-range collision risk.
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)) // gh#220:allow-fixed-port
		if err == nil {
			_ = l.Close()
			return
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d never freed after StopAll within 10s; last bind error: %v", port, lastErr)
}

// freePort asks the kernel for an unused TCP port. Used by tests that
// need a real port without colliding under parallel test runs.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen for free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
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
