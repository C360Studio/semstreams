//go:build integration

package graphingest

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/vocabulary"
)

var sharedLifecycleNATSClient *natsclient.TestClient

func TestMain(m *testing.M) {
	// Declare the test-only predicates used by projection/ownership contracts.
	// Runtime graph writes require canonical syntax, while authoring surfaces
	// additionally require explicit vocabulary declaration.
	vocabulary.Register("mission.state.phase")
	vocabulary.Register("sensorml.component.is-hosted-by")
	vocabulary.Register("test.anyproducer.hosted-by")
	vocabulary.Register("test.strict.hosted-by")

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	var err error
	sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(
		natsclient.WithKV(),
		natsclient.WithStreams(streams...),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shared lifecycle NATS client: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if err := sharedLifecycleNATSClient.Terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean up shared lifecycle NATS client: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func getSharedNATSClient(t *testing.T) *natsclient.TestClient {
	if sharedLifecycleNATSClient == nil {
		t.Fatal("shared NATS client not initialized")
	}
	return sharedLifecycleNATSClient
}

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	tc := sharedLifecycleNATSClient
	if tc == nil {
		panic("shared NATS client not initialized - run with -tags=integration")
	}

	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: tc.Client,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	comp, err := CreateGraphIngest(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	return comp.(component.LifecycleComponent)
}

// TestGraphIngest_ComprehensiveLifecycle runs the complete lifecycle test suite.
func TestGraphIngest_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponentForLifecycle)
}
