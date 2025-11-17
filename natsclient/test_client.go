// Package natsclient provides testcontainers-based NATS infrastructure for testing.
package natsclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestClient provides testcontainers-based NATS for testing
type TestClient struct {
	container testcontainers.Container
	Client    *Client // Drop-in replacement for existing natsclient.Client
	URL       string
	cleanup   func()
}

// testConfig holds configuration for test client
type testConfig struct {
	jetstream    bool
	kv           bool
	kvBuckets    []string
	natsVersion  string
	timeout      time.Duration
	startTimeout time.Duration
}

// TestOption for configuring test client
type TestOption func(*testConfig)

// WithJetStream enables JetStream for tests that need it
func WithJetStream() TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = true
	}
}

// WithKV enables KV store for tests that need it
func WithKV() TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = true // KV requires JetStream
		cfg.kv = true
	}
}

// WithKVBuckets pre-creates specific KV buckets
func WithKVBuckets(buckets ...string) TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = true // KV requires JetStream
		cfg.kv = true
		cfg.kvBuckets = append(cfg.kvBuckets, buckets...)
	}
}

// WithNATSVersion specifies a specific NATS server version to use
func WithNATSVersion(version string) TestOption {
	return func(cfg *testConfig) {
		cfg.natsVersion = version
	}
}

// WithTestTimeout sets the connection timeout for test client
func WithTestTimeout(timeout time.Duration) TestOption {
	return func(cfg *testConfig) {
		cfg.timeout = timeout
	}
}

// WithStartTimeout sets the container startup timeout
func WithStartTimeout(timeout time.Duration) TestOption {
	return func(cfg *testConfig) {
		cfg.startTimeout = timeout
	}
}

// NewSharedTestClient creates a new NATS test container for use in TestMain
// Unlike NewTestClient, this doesn't require testing.T and returns errors
func NewSharedTestClient(opts ...TestOption) (*TestClient, error) {
	// Default configuration
	cfg := &testConfig{
		natsVersion:  "2.11.7-alpine",
		timeout:      5 * time.Second,
		startTimeout: 30 * time.Second,
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	ctx := context.Background()

	// Build NATS arguments
	args := []string{
		"--port", "4222",
		"--http_port", "8222",
	}

	if cfg.jetstream {
		args = append(args, "--js")
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:        "nats:" + cfg.natsVersion,
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          args,
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp").WithStartupTimeout(cfg.startTimeout),
		),
	}

	// Start container
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start NATS container: %w", err)
	}

	// Get connection details
	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "4222")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	// Build connection URL
	url := fmt.Sprintf("nats://%s:%s", host, port.Port())

	// Create NATS client with appropriate timeout
	client, err := NewClient(url,
		WithTimeout(cfg.timeout),
		WithMaxReconnects(0),  // No reconnects in tests
		WithHealthInterval(0), // Disable health monitoring
	)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create NATS client: %w", err)
	}

	// Connect to NATS
	connectCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Wait for connection to be ready
	if err := client.WaitForConnection(connectCtx); err != nil {
		container.Terminate(ctx)
		_ = client.Close(ctx) // Best effort cleanup on error path
		return nil, fmt.Errorf("NATS connection not ready: %w", err)
	}

	testClient := &TestClient{
		container: container,
		Client:    client,
		URL:       url,
		cleanup: func() {
			_ = client.Close(context.Background())        // Best effort test cleanup
			_ = container.Terminate(context.Background()) // Best effort test cleanup
		},
	}

	// Setup KV buckets if requested
	if cfg.kv && len(cfg.kvBuckets) > 0 {
		if err := testClient.setupKVBuckets(ctx, cfg.kvBuckets); err != nil {
			testClient.cleanup()
			return nil, fmt.Errorf("failed to setup KV buckets: %w", err)
		}
	}

	return testClient, nil
}

// NewTestClient creates a new NATS test container
// Accepts testing.TB so it works with both *testing.T and *testing.B
func NewTestClient(t testing.TB, opts ...TestOption) *TestClient {
	t.Helper()

	// Default configuration
	cfg := &testConfig{
		natsVersion:  "2.11.7-alpine",
		timeout:      5 * time.Second,
		startTimeout: 30 * time.Second,
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	ctx := context.Background()

	// Build NATS arguments
	args := []string{
		"--port", "4222",
		"--http_port", "8222",
	}

	if cfg.jetstream {
		args = append(args, "--js")
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:        "nats:" + cfg.natsVersion,
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          args,
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp").WithStartupTimeout(cfg.startTimeout),
		),
	}

	// Start container
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start NATS container: %v", err)
	}

	// Get connection details
	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "4222")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get mapped port: %v", err)
	}

	// Build connection URL
	url := fmt.Sprintf("nats://%s:%s", host, port.Port())

	// Create NATS client with appropriate timeout
	client, err := NewClient(url,
		WithTimeout(cfg.timeout),
		WithMaxReconnects(0),  // No reconnects in tests
		WithHealthInterval(0), // Disable health monitoring
	)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to create NATS client: %v", err)
	}

	// Connect to NATS
	connectCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to connect to NATS: %v", err)
	}

	// Wait for connection to be ready
	if err := client.WaitForConnection(connectCtx); err != nil {
		container.Terminate(ctx)
		_ = client.Close(ctx)
		t.Fatalf("NATS connection not ready: %v", err)
	}

	testClient := &TestClient{
		container: container,
		Client:    client,
		URL:       url,
		cleanup: func() {
			_ = client.Close(context.Background())        // Best effort test cleanup
			_ = container.Terminate(context.Background()) // Best effort test cleanup
		},
	}

	// Setup KV buckets if requested
	if cfg.kv && len(cfg.kvBuckets) > 0 {
		if err := testClient.setupKVBuckets(ctx, cfg.kvBuckets); err != nil {
			testClient.cleanup()
			t.Fatalf("Failed to setup KV buckets: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(testClient.cleanup)

	return testClient
}

// setupKVBuckets creates the requested KV buckets
func (tc *TestClient) setupKVBuckets(ctx context.Context, buckets []string) error {
	for _, bucketName := range buckets {
		cfg := jetstream.KeyValueConfig{
			Bucket: bucketName,
		}

		_, err := tc.Client.CreateKeyValueBucket(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create KV bucket %s: %w", bucketName, err)
		}
	}
	return nil
}

// Terminate manually terminates the container and client (usually handled by t.Cleanup)
func (tc *TestClient) Terminate() error {
	if tc.cleanup != nil {
		tc.cleanup()
		tc.cleanup = nil
	}
	return nil
}

// IsReady checks if the NATS connection is ready for use
func (tc *TestClient) IsReady() bool {
	return tc.Client.IsHealthy()
}

// GetNativeConnection returns the underlying NATS connection for direct access
func (tc *TestClient) GetNativeConnection() *gonats.Conn {
	return tc.Client.GetConnection()
}

// CreateKVBucket is a helper for creating KV buckets during tests
func (tc *TestClient) CreateKVBucket(ctx context.Context, name string) (jetstream.KeyValue, error) {
	cfg := jetstream.KeyValueConfig{
		Bucket: name,
	}
	return tc.Client.CreateKeyValueBucket(ctx, cfg)
}

// GetKVBucket is a helper for getting existing KV buckets during tests
func (tc *TestClient) GetKVBucket(ctx context.Context, name string) (jetstream.KeyValue, error) {
	return tc.Client.GetKeyValueBucket(ctx, name)
}
