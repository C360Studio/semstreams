package graphquery

import (
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	// The mock client returns StatusConnected, which allows Start() to proceed
	// through initialization without requiring real NATS infrastructure.
	config := DefaultConfig()
	config.ApplyDefaults()
	config.StartupAttempts = 1
	config.StartupInterval = time.Millisecond
	config.RecheckInterval = time.Hour

	return &Component{
		natsClient:       newMockNATSClient(),
		config:           config,
		logger:           slog.Default(),
		lastMetricsReset: time.Now(),
	}
}

// TestGraphQuery_ComprehensiveLifecycle runs the complete lifecycle test suite.
// This test uses a mock NATS client that satisfies the interface without
// requiring actual NATS connectivity, making it suitable for unit testing.
//
// Current status: Most tests pass. The component successfully handles:
// - Initialize/Start/Stop lifecycle
// - Idempotent operations (double Start, double Stop)
// - Restart after Stop
// - Error cases (Start without Initialize)
//
// Known issues revealed by tests:
// - Component should fail when given a cancelled context (currently succeeds)
// - Component should fail when given a timeout context (currently succeeds)
// - Component should check context validity before proceeding with Start
func TestGraphQuery_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponentForLifecycle)
}
