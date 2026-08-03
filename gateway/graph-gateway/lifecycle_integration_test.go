//go:build integration

package graphgateway

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

var sharedLifecycleNATSClient *natsclient.TestClient

func TestMain(m *testing.M) {
	var err error
	sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(natsclient.WithKV())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shared lifecycle NATS client: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := sharedLifecycleNATSClient.Terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean up shared lifecycle NATS client: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	tc := sharedLifecycleNATSClient
	if tc == nil {
		panic("shared NATS client not initialized")
	}

	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: tc.Client,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	comp, err := CreateGraphGateway(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	return comp.(component.LifecycleComponent)
}

// TestGraphGateway_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestGraphGateway_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponentForLifecycle)
}
