// Package natsclient provides testcontainers-based NATS infrastructure for testing.
package natsclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// requiredPortObservationBudget bounds how long required-port observation keeps
// asking Docker for one coherent port-map snapshot.
//
// Deliberately SHORT. This covers a mapping that is momentarily unresolvable
// under Docker API pressure, not a container that failed to start — the wait
// strategy already owns start-up, with its own 30s default budget. A long budget here
// would convert "this container is dead" into a slow, confusing timeout
// instead of a fast, accurate failure.
const (
	requiredPortObservationBudget    = 10 * time.Second
	requiredPortObservationInterval  = 50 * time.Millisecond
	defaultTestContainerStartTimeout = 30 * time.Second
	// Docker container deletion can exceed five seconds when the integration
	// runner tears down many packages concurrently. This is a failure ceiling,
	// not a delay on the healthy path: Terminate returns as soon as Docker has
	// removed the container.
	testInfrastructureCleanupTimeout = 15 * time.Second
	testContainerHostLookupTimeout   = 5 * time.Second
)

const (
	requiredClientPort     = "4222/tcp"
	requiredMonitoringPort = "8222/tcp"
)

type requiredPortObservation struct {
	mappedPorts  map[string]string
	missingPorts []string
}

type requiredPortObserver func(context.Context) (requiredPortObservation, error)

type requiredPortSet []string

func requiredPortsForConfig(cfg *testConfig) requiredPortSet {
	ports := requiredPortSet{requiredClientPort}
	if cfg.monitoring {
		ports = append(ports, requiredMonitoringPort)
	}
	return ports
}

func requiredPortsFromSnapshot(
	requiredPorts requiredPortSet,
	hostNetwork bool,
	ports nat.PortMap,
) requiredPortObservation {
	observation := requiredPortObservation{
		mappedPorts: make(map[string]string, len(requiredPorts)),
	}
	for _, requiredPort := range requiredPorts {
		mappedPort := firstNonemptyHostPort(ports[nat.Port(requiredPort)])
		if hostNetwork {
			mappedPort = nat.Port(requiredPort).Port()
		}
		if mappedPort == "" {
			observation.missingPorts = append(observation.missingPorts, requiredPort)
			continue
		}
		observation.mappedPorts[requiredPort] = mappedPort
	}
	return observation
}

func firstNonemptyHostPort(bindings []nat.PortBinding) string {
	for _, binding := range bindings {
		if binding.HostPort != "" {
			return binding.HostPort
		}
	}
	return ""
}

// containerRequiredPortObserver reads every configured required mapping from
// one Docker Inspect snapshot. No values from different revisions are combined.
func containerRequiredPortObserver(
	container testcontainers.Container,
	requiredPorts requiredPortSet,
) requiredPortObserver {
	return func(ctx context.Context) (requiredPortObservation, error) {
		inspected, err := container.Inspect(ctx)
		if err != nil {
			return requiredPortObservation{}, fmt.Errorf("inspect container port snapshot: %w", err)
		}

		hostNetwork := inspected != nil && inspected.ContainerJSONBase != nil &&
			inspected.ContainerJSONBase.HostConfig != nil &&
			string(inspected.ContainerJSONBase.HostConfig.NetworkMode) == "host"
		var ports nat.PortMap
		if inspected != nil && inspected.NetworkSettings != nil {
			ports = inspected.NetworkSettings.Ports
		}
		return requiredPortsFromSnapshot(requiredPorts, hostNetwork, ports), nil
	}
}

type successfulRequiredPortObservation struct {
	attempt      int
	missingPorts []string
}

func missingRequiredPorts(
	requiredPorts requiredPortSet,
	observation requiredPortObservation,
) []string {
	missingPorts := make([]string, 0, len(requiredPorts))
	for _, requiredPort := range requiredPorts {
		if observation.mappedPorts[requiredPort] == "" {
			missingPorts = append(missingPorts, requiredPort)
		}
	}
	return missingPorts
}

type requiredPortResolutionError struct {
	attempts                  int
	budget                    time.Duration
	elapsed                   time.Duration
	lastSuccessfulObservation *successfulRequiredPortObservation
	lastInspectErrAttempt     int
	lastInspectErr            error
	terminalErr               error
}

func (e *requiredPortResolutionError) observedRequiredPortAbsence(requiredPorts requiredPortSet) bool {
	if e.lastSuccessfulObservation == nil {
		return false
	}
	for _, missingPort := range e.lastSuccessfulObservation.missingPorts {
		for _, requiredPort := range requiredPorts {
			if missingPort == requiredPort {
				return true
			}
		}
	}
	return false
}

func (e *requiredPortResolutionError) Error() string {
	parts := []string{fmt.Sprintf(
		"resolve required mapped ports: %d observation attempt(s), mapping budget %s, elapsed %s",
		e.attempts,
		e.budget,
		e.elapsed,
	)}
	if e.lastSuccessfulObservation == nil {
		parts = append(parts, "no successful port observation")
	} else {
		parts = append(parts, fmt.Sprintf(
			"last successful observation attempt %d missing [%s]",
			e.lastSuccessfulObservation.attempt,
			strings.Join(e.lastSuccessfulObservation.missingPorts, ", "),
		))
	}
	if e.lastInspectErr != nil {
		parts = append(parts, fmt.Sprintf(
			"last inspect error attempt %d: %v",
			e.lastInspectErrAttempt,
			e.lastInspectErr,
		))
	}
	if e.terminalErr != nil {
		parts = append(parts, "context: "+e.terminalErr.Error())
	}
	return strings.Join(parts, "; ")
}

func (e *requiredPortResolutionError) Unwrap() []error {
	causes := make([]error, 0, 2)
	if e.terminalErr != nil {
		causes = append(causes, e.terminalErr)
	}
	if e.lastInspectErr != nil {
		causes = append(causes, e.lastInspectErr)
	}
	return causes
}

func resolveRequiredPorts(
	ctx context.Context,
	requiredPorts requiredPortSet,
	observe requiredPortObserver,
) (requiredPortObservation, error) {
	return resolveRequiredPortsWithin(
		ctx,
		requiredPorts,
		observe,
		requiredPortObservationBudget,
		requiredPortObservationInterval,
	)
}

func resolveRequiredPortsWithin(
	ctx context.Context,
	requiredPorts requiredPortSet,
	observe requiredPortObserver,
	budget time.Duration,
	retryInterval time.Duration,
) (requiredPortObservation, error) {
	started := time.Now()
	budgetCtx, budgetCancel := context.WithTimeout(ctx, budget)
	defer budgetCancel()
	deadline, _ := budgetCtx.Deadline()

	var (
		attempts                  int
		lastSuccessfulObservation *successfulRequiredPortObservation
		lastInspectErrAttempt     int
		lastInspectErr            error
	)
	resolutionError := func(terminalErr error) error {
		return &requiredPortResolutionError{
			attempts:                  attempts,
			budget:                    budget,
			elapsed:                   time.Since(started),
			lastSuccessfulObservation: lastSuccessfulObservation,
			lastInspectErrAttempt:     lastInspectErrAttempt,
			lastInspectErr:            lastInspectErr,
			terminalErr:               terminalErr,
		}
	}

	for {
		if ctxErr := budgetCtx.Err(); ctxErr != nil {
			return requiredPortObservation{}, resolutionError(ctxErr)
		}

		remaining := time.Until(deadline)
		attemptCtx, attemptCancel := context.WithTimeout(budgetCtx, remaining)
		observation, err := observe(attemptCtx)
		attemptCancel()
		attempts++
		if err != nil {
			lastInspectErrAttempt = attempts
			lastInspectErr = err
		} else {
			missingPorts := missingRequiredPorts(requiredPorts, observation)
			if len(missingPorts) == 0 {
				return observation, nil
			}
			lastSuccessfulObservation = &successfulRequiredPortObservation{
				attempt:      attempts,
				missingPorts: missingPorts,
			}
		}

		if ctxErr := budgetCtx.Err(); ctxErr != nil {
			return requiredPortObservation{}, resolutionError(ctxErr)
		}
		remaining = time.Until(deadline)
		if remaining <= 0 {
			return requiredPortObservation{}, resolutionError(context.DeadlineExceeded)
		}
		pause := min(retryInterval, remaining)
		if pause < 0 {
			pause = 0
		}
		timer := time.NewTimer(pause)
		select {
		case <-budgetCtx.Done():
			timer.Stop()
			return requiredPortObservation{}, resolutionError(budgetCtx.Err())
		case <-timer.C:
		}
	}
}

type containerTerminator interface {
	Terminate(context.Context, ...testcontainers.TerminateOption) error
}

type testClientCloser interface {
	Close(context.Context) error
}

func cleanupTestInfrastructure(client testClientCloser, container containerTerminator) error {
	return cleanupTestInfrastructureWithin(client, container, testInfrastructureCleanupTimeout)
}

// cleanupTestInfrastructureWithin closes the client and terminates its
// container even when either operation fails. Each operation receives a fresh
// deadline so a blocked client drain cannot consume the container's cleanup
// budget and leave Docker resources behind.
func cleanupTestInfrastructureWithin(
	client testClientCloser,
	container containerTerminator,
	timeout time.Duration,
) error {
	var cleanupErr error
	if client != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)
		if err := client.Close(closeCtx); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close NATS test client: %w", err))
		}
		closeCancel()
	}
	if container != nil {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)
		if err := container.Terminate(terminateCtx); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate NATS test container: %w", err))
		}
		terminateCancel()
	}
	return cleanupErr
}

func cleanupAfterTestClientSetupFailure(
	client testClientCloser,
	container containerTerminator,
	setupErr error,
) error {
	return errors.Join(setupErr, cleanupTestInfrastructure(client, container))
}

func cleanupContainerAfterStartFailure(container containerTerminator, startErr error) error {
	return cleanupContainerAfterStartFailureWithin(container, startErr, testInfrastructureCleanupTimeout)
}

// cleanupContainerAfterStartFailureWithin removes a container returned with a
// failed GenericContainer call. Cleanup gets an independent bounded context
// because the startup context may already be expired.
func cleanupContainerAfterStartFailureWithin(
	container containerTerminator,
	startErr error,
	timeout time.Duration,
) error {
	if container == nil {
		return startErr
	}

	return errors.Join(startErr, cleanupTestInfrastructureWithin(nil, container, timeout))
}

// TestClient provides testcontainers-based NATS for testing
type TestClient struct {
	container     testcontainers.Container
	Client        *Client // Drop-in replacement for existing natsclient.Client
	URL           string
	MonitoringURL string // Empty unless WithMonitoring requests the mapped monitoring endpoint
	BucketPrefix  string // Prefix applied to all KV bucket names for test isolation
	cleanup       func() error
	cleanupOnce   sync.Once
	cleanupErr    error
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
	monitoring   bool   // Expose the NATS monitoring endpoint when explicitly requested
	maxPayload   int64  // Override the broker limit for response-bound integration tests
}

// natsReadyWaitStrategy observes readiness in container logs. LogStrategy still
// checks container state through Docker Inspect, but unlike HTTP and listening-
// port strategies it never asks Docker to resolve a host-side port mapping. It
// can accept the ready log without waiting for a successful state inspect.
func natsReadyWaitStrategy(startupTimeout time.Duration) *wait.LogStrategy {
	return wait.ForLog("Server is ready").WithStartupTimeout(startupTimeout)
}

// newTestContainerRequest is the single container shape consumed by both
// NewTestClient and NewSharedTestClient. Keeping readiness here prevents the
// TestMain and testing.T entry paths from silently drifting apart.
func newTestContainerRequest(cfg *testConfig) testcontainers.GenericContainerRequest {
	args := []string{
		"--port", "4222",
	}
	var files []testcontainers.ContainerFile
	if cfg.maxPayload > 0 {
		const configPath = "/etc/nats/semstreams-test.conf"
		args = append(args, "--config", configPath)
		files = append(files, testcontainers.ContainerFile{
			Reader:            strings.NewReader(fmt.Sprintf("max_payload: %d\n", cfg.maxPayload)),
			ContainerFilePath: configPath,
			FileMode:          0o644,
		})
	}
	if cfg.monitoring {
		args = append(args, "--http_port", "8222")
	}
	if cfg.jetstream {
		args = append(args, "--js")
		if cfg.fileStorage {
			args = append(args, "--store_dir", "/tmp/nats-data")
		}
	}

	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:" + cfg.natsVersion,
			ExposedPorts: append([]string(nil), requiredPortsForConfig(cfg)...),
			Cmd:          args,
			Files:        files,
			// LogStrategy still checks container state through Inspect, but it
			// does not call MappedPort and can accept this exact readiness log
			// without waiting for a successful state inspect (gh#736).
			WaitingFor: natsReadyWaitStrategy(cfg.startTimeout),
		},
		Started: true,
	}
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

// WithMonitoring enables and exposes the NATS HTTP monitoring endpoint. The
// resulting TestClient.MonitoringURL is empty unless this option is supplied.
func WithMonitoring() TestOption {
	return func(cfg *testConfig) {
		cfg.monitoring = true
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

type testClientSetupPhase string

const (
	testClientSetupPhaseStart      testClientSetupPhase = "start"
	testClientSetupPhaseHost       testClientSetupPhase = "host"
	testClientSetupPhaseMappedPort testClientSetupPhase = "mapped-port"
	testClientSetupPhaseClient     testClientSetupPhase = "client"
	testClientSetupPhaseConnect    testClientSetupPhase = "connect"
	testClientSetupPhaseReadiness  testClientSetupPhase = "readiness"
	testClientSetupPhaseResources  testClientSetupPhase = "resources"
	maxTestClientSetupAttempts                          = 2
)

// testClientSetupError retains the exact attempt and setup phase that failed,
// plus every setup and cleanup cause. The factory uses these fields to make the
// one narrow replacement decision; callers can still use errors.Is/As against
// the original Docker, context, NATS, and cleanup errors.
type testClientSetupError struct {
	attempt          int
	phase            testClientSetupPhase
	missingPorts     []string
	containerID      string
	parentErr        error
	parentStateKnown bool
	cause            error
	cleanupErr       error
}

func (e *testClientSetupError) Error() string {
	location := fmt.Sprintf("test client attempt %d phase %s", e.attempt, e.phase)
	if len(e.missingPorts) > 0 {
		location += " missing ports [" + strings.Join(e.missingPorts, ", ") + "]"
	}
	if e.containerID != "" {
		location += " container " + e.containerID
	}
	if e.parentStateKnown {
		if e.parentErr == nil {
			location += " parent context live"
		} else {
			location += " parent context " + e.parentErr.Error()
		}
	}
	if e.cleanupErr != nil {
		return fmt.Sprintf("%s: %v (cleanup: %v)", location, e.cause, e.cleanupErr)
	}
	return fmt.Sprintf("%s: %v", location, e.cause)
}

func (e *testClientSetupError) Unwrap() []error {
	causes := make([]error, 0, 2)
	if e.cause != nil {
		causes = append(causes, e.cause)
	}
	if e.cleanupErr != nil {
		causes = append(causes, e.cleanupErr)
	}
	return causes
}

type testClientFactoryDeps struct {
	attempt func(context.Context, *testConfig, int) (*TestClient, error)
}

var productionTestClientFactoryDeps = testClientFactoryDeps{
	attempt: runTestClientAttempt,
}

func defaultTestConfig() *testConfig {
	return &testConfig{
		natsVersion: "2.14-alpine",
		timeout:     5 * time.Second,
		// This bounds the whole Docker create/start/readiness operation.
		// NATS normally emits readiness in milliseconds; callers with a
		// concrete slower environment can widen it with WithStartTimeout.
		startTimeout: defaultTestContainerStartTimeout,
	}
}

// newTestClient is the single error-returning full factory consumed by both
// public constructors. It permits at most one replacement, and only for the
// mapped-port phase when a successful Inspect snapshot observed a required
// port absent. Each failed attempt has completed cleanup before it returns.
func newTestClient(
	ctx context.Context,
	deps testClientFactoryDeps,
	opts ...TestOption,
) (*TestClient, error) {
	cfg := defaultTestConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	failures := make([]error, 0, maxTestClientSetupAttempts)
	for attempt := 1; attempt <= maxTestClientSetupAttempts; attempt++ {
		testClient, err := deps.attempt(ctx, cfg, attempt)
		if err == nil {
			return testClient, nil
		}
		var setupErr *testClientSetupError
		if errors.As(err, &setupErr) {
			setupErr.parentErr = ctx.Err()
			setupErr.parentStateKnown = true
		}
		failures = append(failures, err)
		if attempt == maxTestClientSetupAttempts ||
			!eligibleForMappedPortReplacement(ctx, requiredPortsForConfig(cfg), err) {
			return nil, errors.Join(failures...)
		}
	}

	panic("unreachable test client attempt bound")
}

func eligibleForMappedPortReplacement(
	ctx context.Context,
	requiredPorts requiredPortSet,
	err error,
) bool {
	if ctx.Err() != nil {
		return false
	}
	var setupErr *testClientSetupError
	if !errors.As(err, &setupErr) || setupErr.phase != testClientSetupPhaseMappedPort {
		return false
	}
	var resolutionErr *requiredPortResolutionError
	return setupErr.cleanupErr == nil &&
		errors.As(setupErr.cause, &resolutionErr) &&
		resolutionErr.observedRequiredPortAbsence(requiredPorts)
}

func failedTestClientAttempt(
	ctx context.Context,
	attempt int,
	phase testClientSetupPhase,
	missingPorts []string,
	cause error,
	client testClientCloser,
	container containerTerminator,
) error {
	containerID := ""
	if identified, ok := container.(interface{ GetContainerID() string }); ok {
		containerID = identified.GetContainerID()
	}
	return &testClientSetupError{
		attempt:          attempt,
		phase:            phase,
		missingPorts:     missingPorts,
		containerID:      containerID,
		parentErr:        ctx.Err(),
		parentStateKnown: true,
		cause:            cause,
		cleanupErr:       cleanupTestInfrastructure(client, container),
	}
}

func monitoringURLForConfig(
	cfg *testConfig,
	host string,
	resolvedPorts requiredPortObservation,
) string {
	if !cfg.monitoring {
		return ""
	}
	return fmt.Sprintf(
		"http://%s:%s",
		host,
		resolvedPorts.mappedPorts[requiredMonitoringPort],
	)
}

func runTestClientAttempt(ctx context.Context, cfg *testConfig, attempt int) (*TestClient, error) {
	startCtx, startCancel := context.WithTimeout(ctx, cfg.startTimeout)
	container, err := testcontainers.GenericContainer(startCtx, newTestContainerRequest(cfg))
	startCancel()
	if err != nil {
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseStart,
			nil,
			fmt.Errorf("start NATS container: %w", err),
			nil,
			container,
		)
	}

	hostCtx, hostCancel := context.WithTimeout(ctx, testContainerHostLookupTimeout)
	host, err := container.Host(hostCtx)
	hostCancel()
	if err != nil {
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseHost,
			nil,
			fmt.Errorf("get container host: %w", err),
			nil,
			container,
		)
	}

	requiredPorts := requiredPortsForConfig(cfg)
	resolvedPorts, err := resolveRequiredPorts(
		ctx,
		requiredPorts,
		containerRequiredPortObserver(container, requiredPorts),
	)
	if err != nil {
		var resolutionErr *requiredPortResolutionError
		errors.As(err, &resolutionErr)
		var missingPorts []string
		if resolutionErr != nil && resolutionErr.lastSuccessfulObservation != nil {
			missingPorts = append(
				missingPorts,
				resolutionErr.lastSuccessfulObservation.missingPorts...,
			)
		}
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseMappedPort,
			missingPorts,
			err,
			nil,
			container,
		)
	}

	url := fmt.Sprintf("nats://%s:%s", host, resolvedPorts.mappedPorts[requiredClientPort])
	client, err := NewClient(url,
		WithTimeout(cfg.timeout),
		WithMaxReconnects(0),
		WithHealthInterval(0),
	)
	if err != nil {
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseClient,
			nil,
			fmt.Errorf("create NATS client: %w", err),
			nil,
			container,
		)
	}

	connectCtx, connectCancel := context.WithTimeout(ctx, cfg.timeout)
	if err := client.Connect(connectCtx); err != nil {
		connectCancel()
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseConnect,
			nil,
			fmt.Errorf("connect to NATS: %w", err),
			client,
			container,
		)
	}
	if err := client.WaitForConnection(connectCtx); err != nil {
		connectCancel()
		return nil, failedTestClientAttempt(
			ctx,
			attempt,
			testClientSetupPhaseReadiness,
			nil,
			fmt.Errorf("NATS connection not ready: %w", err),
			client,
			container,
		)
	}
	connectCancel()

	testClient := &TestClient{
		container:     container,
		Client:        client,
		URL:           url,
		MonitoringURL: monitoringURLForConfig(cfg, host, resolvedPorts),
		BucketPrefix:  cfg.bucketPrefix,
		cleanup: func() error {
			return cleanupTestInfrastructure(client, container)
		},
	}
	resourceSetupCtx, resourceSetupCancel := context.WithTimeout(ctx, cfg.timeout)
	defer resourceSetupCancel()

	if cfg.kv && len(cfg.kvBuckets) > 0 {
		if err := testClient.setupKVBuckets(resourceSetupCtx, cfg.kvBuckets); err != nil {
			return nil, &testClientSetupError{
				attempt:          attempt,
				phase:            testClientSetupPhaseResources,
				containerID:      container.GetContainerID(),
				parentErr:        ctx.Err(),
				parentStateKnown: true,
				cause:            fmt.Errorf("setup KV buckets: %w", err),
				cleanupErr:       testClient.Terminate(),
			}
		}
	}
	if len(cfg.streams) > 0 {
		if err := testClient.setupStreams(resourceSetupCtx, cfg.streams); err != nil {
			return nil, &testClientSetupError{
				attempt:          attempt,
				phase:            testClientSetupPhaseResources,
				containerID:      container.GetContainerID(),
				parentErr:        ctx.Err(),
				parentStateKnown: true,
				cause:            fmt.Errorf("setup streams: %w", err),
				cleanupErr:       testClient.Terminate(),
			}
		}
	}

	return testClient, nil
}

// NewSharedTestClient creates a new NATS test container for use in TestMain
// Unlike NewTestClient, this doesn't require testing.T and returns errors
func NewSharedTestClient(opts ...TestOption) (*TestClient, error) {
	return newTestClient(context.Background(), productionTestClientFactoryDeps, opts...)
}

// NewTestClient creates a new NATS test container
// Accepts testing.TB so it works with both *testing.T and *testing.B
func NewTestClient(t testing.TB, opts ...TestOption) *TestClient {
	t.Helper()
	testClient, err := newTestClient(t.Context(), productionTestClientFactoryDeps, opts...)
	if err != nil {
		t.Fatal(err)
		return nil
	}

	// Register cleanup
	t.Cleanup(func() {
		if err := testClient.Terminate(); err != nil {
			t.Errorf("clean up NATS test infrastructure: %v", err)
		}
	})

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
		cfg := testStreamConfig(streamCfg.Name, streamCfg.Subjects)

		_, err := tc.Client.EnsureStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create stream %s: %w", streamCfg.Name, err)
		}
	}
	return nil
}

// Test-stream bounds. Every stream these helpers create declares them, rather
// than the helpers taking an exemption from the bounds requirement.
//
// That is the point. A test path that could create an unbounded stream would let
// the production contract go unexercised by the entire suite that uses these
// helpers, and the suite is where the contract's own tests live — a guard nothing
// routine drives is a guard nobody notices breaking. The values are small on
// purpose: a test stream that outlives its container is a leak either way, and a
// tight ceiling makes a runaway test fail loudly instead of filling the account.
const (
	testStreamMaxAge   = time.Hour
	testStreamMaxBytes = 64 << 20
)

// testStreamConfig is the declared configuration the test helpers create streams
// with. A test needing different bounds builds its own config and calls
// EnsureStream directly — which is also how it would look in production.
func testStreamConfig(name string, subjects []string) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:     name,
		Subjects: subjects,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
		Discard:  jetstream.DiscardOld,
	}
}

// Terminate closes the client and terminates the container, returning any
// cleanup errors. Repeated calls return the first cleanup result without
// running cleanup again; NewTestClient normally invokes it through t.Cleanup.
func (tc *TestClient) Terminate() error {
	tc.cleanupOnce.Do(func() {
		if tc.cleanup != nil {
			tc.cleanupErr = tc.cleanup()
		}
	})
	return tc.cleanupErr
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

// CreateStream is a helper for creating JetStream streams during tests. The
// stream carries declared bounds; see testStreamConfig.
func (tc *TestClient) CreateStream(ctx context.Context, name string, subjects []string) (jetstream.Stream, error) {
	return tc.Client.EnsureStream(ctx, testStreamConfig(name, subjects))
}

// GetStream is a helper for getting existing JetStream streams during tests
func (tc *TestClient) GetStream(ctx context.Context, name string) (jetstream.Stream, error) {
	js, err := tc.Client.JetStream()
	if err != nil {
		return nil, err
	}
	return js.Stream(ctx, name)
}
