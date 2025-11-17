package component

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go"
)

var (
	sharedNATSClient *nats.Conn
)

func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		fmt.Println("Skipping integration tests. Set INTEGRATION_TESTS=1 to run.")
		return
	}

	// Setup: Create shared NATS test client with testcontainers
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
	)
	if err != nil {
		log.Fatalf("Failed to create shared test client: %v", err)
	}

	sharedNATSClient = testClient.Client.GetConnection()

	// Run tests
	exitCode := m.Run()

	// Cleanup
	testClient.Terminate()

	os.Exit(exitCode)
}

// getSharedNATSClient returns the shared NATS connection for integration tests
func getSharedNATSClient(t *testing.T) *nats.Conn {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}
