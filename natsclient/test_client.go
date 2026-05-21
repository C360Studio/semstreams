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
	container    testcontainers.Container
	Client       *Client // Drop-in replacement for existing natsclient.Client
	URL          string
	BucketPrefix string // Prefix applied to all KV bucket names for test isolation
	cleanup      func()
}

// testConfig holds configuration for test client
type testConfig struct {
	jetstream    bool
	kv           bool
	kvBuckets    []string
	streams      []TestStreamConfig
	natsVersion  string
	timeout      time.Duration
	startTimeout time.Duration
	bucketPrefix string // Prefix for KV bucket names to enable test isolation
	fileStorage  bool   // Use file-backed JetStream storage instead of memory-only
}

// TestStreamConfig defines a stream to pre-create for testing
type TestStreamConfig struct {
	Name     string
	Subjects []string
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

// WithStreams pre-creates JetStream streams for testing
func WithStreams(streams ...TestStreamConfig) TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = true // Streams require JetStream
		cfg.streams = append(cfg.streams, streams...)
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

// WithFileStorage enables file-backed JetStream storage instead of the default
// memory-only store. Use this when tests create many KV buckets or write large
// volumes of data that would exceed the 256MB default memory limit.
func WithFileStorage() TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = true
		cfg.fileStorage = true
	}
}

// WithBucketPrefix sets a prefix for all KV bucket names to enable test isolation.
// When tests run in parallel, each test can use a unique prefix (e.g., test name)
// to avoid bucket name collisions.
func WithBucketPrefix(prefix string) TestOption {
	return func(cfg *testConfig) {
		cfg.bucketPrefix = prefix
	}
}

// NewSharedTestClient creates a new NATS test container for use in TestMain
// Unlike NewTestClient, this doesn't require testing.T and returns errors
func NewSharedTestClient(opts ...TestOption) (*TestClient, error) {
	// Default configuration
	cfg := &testConfig{
		natsVersion:  "2.12-alpine",
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
		if cfg.fileStorage {
			args = append(args, "--store_dir", "/tmp/nats-data")
		}
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:        "nats:" + cfg.natsVersion,
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          args,
		// Startup timeout applied at the ForAll level so it bounds BOTH
		// strategies. Prior pattern only set it on ForHTTP, leaving
		// ForListeningPort with its own (shorter) default — under Docker
		// API pressure the port-listening check would burn through retries
		// before the HTTP timeout could engage. Symptom:
		//   "retries: 532, port: '', last err: port '4222/tcp' not found,
		//   ctx err: context deadline exceeded"
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp"),
		).WithStartupTimeoutDefault(cfg.startTimeout),
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
		container:    container,
		Client:       client,
		URL:          url,
		BucketPrefix: cfg.bucketPrefix,
		cleanup: func() {
			// Use timeout context for drain to prevent hanging, then terminate container
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			_ = client.Close(closeCtx)                    // Wait for drain with timeout
			_ = container.Terminate(context.Background()) // Then terminate container
		},
	}

	// Setup KV buckets if requested
	if cfg.kv && len(cfg.kvBuckets) > 0 {
		if err := testClient.setupKVBuckets(ctx, cfg.kvBuckets); err != nil {
			testClient.cleanup()
			return nil, fmt.Errorf("failed to setup KV buckets: %w", err)
		}
	}

	// Setup streams if requested
	if len(cfg.streams) > 0 {
		if err := testClient.setupStreams(ctx, cfg.streams); err != nil {
			testClient.cleanup()
			return nil, fmt.Errorf("failed to setup streams: %w", err)
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
		natsVersion:  "2.12-alpine",
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
		if cfg.fileStorage {
			args = append(args, "--store_dir", "/tmp/nats-data")
		}
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:        "nats:" + cfg.natsVersion,
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          args,
		// Startup timeout applied at the ForAll level so it bounds BOTH
		// strategies. Prior pattern only set it on ForHTTP, leaving
		// ForListeningPort with its own (shorter) default — under Docker
		// API pressure the port-listening check would burn through retries
		// before the HTTP timeout could engage. Symptom:
		//   "retries: 532, port: '', last err: port '4222/tcp' not found,
		//   ctx err: context deadline exceeded"
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp"),
		).WithStartupTimeoutDefault(cfg.startTimeout),
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
		container:    container,
		Client:       client,
		URL:          url,
		BucketPrefix: cfg.bucketPrefix,
		cleanup: func() {
			// Use timeout context for drain to prevent hanging, then terminate container
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			_ = client.Close(closeCtx)                    // Wait for drain with timeout
			_ = container.Terminate(context.Background()) // Then terminate container
		},
	}

	// Setup KV buckets if requested
	if cfg.kv && len(cfg.kvBuckets) > 0 {
		if err := testClient.setupKVBuckets(ctx, cfg.kvBuckets); err != nil {
			testClient.cleanup()
			t.Fatalf("Failed to setup KV buckets: %v", err)
		}
	}

	// Setup streams if requested
	if len(cfg.streams) > 0 {
		if err := testClient.setupStreams(ctx, cfg.streams); err != nil {
			testClient.cleanup()
			t.Fatalf("Failed to setup streams: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(testClient.cleanup)

	return testClient
}

// setupKVBuckets creates the requested KV buckets
func (tc *TestClient) setupKVBuckets(ctx context.Context, buckets []string) error {
	for _, bucketName := range buckets {
		// Apply bucket prefix for test isolation
		fullName := tc.BucketPrefix + bucketName
		cfg := jetstream.KeyValueConfig{
			Bucket: fullName,
		}

		_, err := tc.Client.CreateKeyValueBucket(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create KV bucket %s: %w", fullName, err)
		}
	}
	return nil
}

// setupStreams creates the requested JetStream streams
func (tc *TestClient) setupStreams(ctx context.Context, streams []TestStreamConfig) error {
	for _, streamCfg := range streams {
		cfg := jetstream.StreamConfig{
			Name:     streamCfg.Name,
			Subjects: streamCfg.Subjects,
		}

		_, err := tc.Client.EnsureStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create stream %s: %w", streamCfg.Name, err)
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

// CreateKVBucket is a helper for creating KV buckets during tests.
// The bucket prefix is automatically applied if configured.
func (tc *TestClient) CreateKVBucket(ctx context.Context, name string) (jetstream.KeyValue, error) {
	fullName := tc.BucketPrefix + name
	cfg := jetstream.KeyValueConfig{
		Bucket: fullName,
	}
	return tc.Client.CreateKeyValueBucket(ctx, cfg)
}

// GetKVBucket is a helper for getting existing KV buckets during tests.
// The bucket prefix is automatically applied if configured.
func (tc *TestClient) GetKVBucket(ctx context.Context, name string) (jetstream.KeyValue, error) {
	fullName := tc.BucketPrefix + name
	return tc.Client.GetKeyValueBucket(ctx, fullName)
}

// PrefixedBucketName returns the full bucket name with prefix applied.
// Use this when you need to pass bucket names to components that create their own buckets.
func (tc *TestClient) PrefixedBucketName(name string) string {
	return tc.BucketPrefix + name
}

// CreateStream is a helper for creating JetStream streams during tests
func (tc *TestClient) CreateStream(ctx context.Context, name string, subjects []string) (jetstream.Stream, error) {
	cfg := jetstream.StreamConfig{
		Name:     name,
		Subjects: subjects,
	}
	return tc.Client.EnsureStream(ctx, cfg)
}

// GetStream is a helper for getting existing JetStream streams during tests
func (tc *TestClient) GetStream(ctx context.Context, name string) (jetstream.Stream, error) {
	js, err := tc.Client.JetStream()
	if err != nil {
		return nil, err
	}
	return js.Stream(ctx, name)
}
