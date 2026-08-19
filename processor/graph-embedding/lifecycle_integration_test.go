//go:build integration

package graphembedding

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

var sharedLifecycleNATSClient *natsclient.TestClient

func TestMain(m *testing.M) {
	var err error
	sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(
		natsclient.WithKV(),
		natsclient.WithKVBuckets(graph.BucketEntityStates),
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

	comp, err := CreateGraphEmbedding(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	return comp.(component.LifecycleComponent)
}
