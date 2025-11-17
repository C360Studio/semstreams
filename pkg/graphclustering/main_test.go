package graphclustering

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/c360/semstreams/natsclient"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all graphclustering tests
func TestMain(m *testing.M) {
	// Unit tests always run (no env var needed)
	// Integration tests require INTEGRATION_TESTS=1

	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithKV(),
			natsclient.WithKVBuckets(
				"COMMUNITY_INDEX",
			),
			natsclient.WithTestTimeout(5*time.Second),
			natsclient.WithStartTimeout(30*time.Second),
		)
		if err != nil {
			log.Fatalf("Failed to create shared test client: %v", err)
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources if they were created
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(exitCode)
}

// getSharedTestClient returns the shared test client for integration tests
func getSharedTestClient(t *testing.T) *natsclient.TestClient {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedTestClient == nil {
		t.Fatal("Shared test client not initialized - TestMain should have created it")
	}
	return sharedTestClient
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
