package indexmanager

import (
	"log"
	"os"
	"testing"

	"github.com/c360/semstreams/natsclient"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all indexmanager tests
func TestMain(m *testing.M) {
	// Unit tests always run (no env var needed)
	// Integration tests require INTEGRATION_TESTS=1

	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithKV(),
		)
		if err != nil {
			log.Fatalf("Failed to create shared test client: %v", err)
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(code)
}

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}
