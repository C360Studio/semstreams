package http

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/gateway"
	"github.com/c360studio/semstreams/natsclient"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	// Create a minimal valid configuration
	config := gateway.Config{
		Routes: []gateway.RouteMapping{
			{
				Path:        "/test",
				Method:      "POST",
				NATSSubject: "test.subject",
			},
		},
		EnableCORS:     false,
		CORSOrigins:    []string{},
		MaxRequestSize: 1024 * 1024,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal lifecycle config: " + err.Error())
	}

	// Create unconnected NATS client (won't actually connect)
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		panic("failed to create lifecycle NATS client: " + err.Error())
	}

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := NewGateway(configJSON, deps)
	if err != nil {
		panic("failed to create lifecycle component: " + err.Error())
	}

	lifecycleComponent, ok := comp.(component.LifecycleComponent)
	if !ok {
		panic("HTTP gateway does not implement component.LifecycleComponent")
	}
	return lifecycleComponent
}

// TestHTTPGateway_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestHTTPGateway_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponentForLifecycle)
}
