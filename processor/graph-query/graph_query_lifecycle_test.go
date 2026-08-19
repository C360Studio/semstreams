package graphquery

import (
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/component"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	// The mock client returns StatusConnected, which allows Start() to proceed
	// through initialization without requiring real NATS infrastructure.
	config := DefaultConfig()
	config.ApplyDefaults()
	config.RecheckInterval = time.Hour

	return &Component{
		natsClient:       newMockNATSClient(),
		config:           config,
		queryFamily:      graphQuerySubjectFamily,
		logger:           slog.Default(),
		lastMetricsReset: time.Now(),
	}
}
