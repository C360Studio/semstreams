//go:build integration

package graphingest

import (
	"encoding/json"
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

	// Setup shared NATS client for all integration tests
	t := &testing.T{}
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	sharedLifecycleNATSClient = natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// Run tests
	code := m.Run()

	// Cleanup
	if sharedLifecycleNATSClient != nil {
		sharedLifecycleNATSClient.Terminate()
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
