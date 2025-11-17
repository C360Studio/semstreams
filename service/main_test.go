package service

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

// TestMain sets up a single shared NATS container for all service package tests
func TestMain(m *testing.M) {
	// Run unit tests even without INTEGRATION_TESTS set
	// Integration tests will skip themselves if INTEGRATION_TESTS is not set
	if os.Getenv("INTEGRATION_TESTS") == "" {
		log.Println("Running unit tests only. Set INTEGRATION_TESTS=1 to run integration tests.")
		exitCode := m.Run()
		os.Exit(exitCode)
	}

	// Create a single shared test client for integration tests
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithTestTimeout(5*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to create shared test client: %v", err)
	}

	sharedTestClient = testClient
	sharedNATSClient = testClient.Client

	// Run all tests
	exitCode := m.Run()

	// Cleanup
	testClient.Terminate()

	os.Exit(exitCode)
}

// getSharedTestClient returns the shared test client for tests that need it
func getSharedTestClient(t *testing.T) *natsclient.TestClient {
	if sharedTestClient == nil {
		t.Fatal("Shared test client not initialized - TestMain should have created it")
	}
	return sharedTestClient
}

// getSharedNATSClient returns the shared NATS client for tests that need it
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}
