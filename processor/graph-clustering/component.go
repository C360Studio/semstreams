// Package graphclustering provides the graph-clustering component for community detection.
package graphclustering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Per-capability LLM-call timeout defaults applied when neither
// endpoint.request_timeout nor capability.timeout is configured. Sized to
// the implicit 60s fallback that NewOpenAIClient previously applied — kept
// short so an unconfigured deployment doesn't silently extend its
// per-community blocking window. Operators that need 300s+ set
// capability.timeout explicitly; the precedence chain in
// model.ResolveCapabilityTimeout picks it up.
const (
	defaultCommunitySummaryTimeout = 60 * time.Second
	defaultAnomalyReviewTimeout    = 60 * time.Second
)

// Config holds configuration for graph-clustering component
type Config struct {
	Ports                *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	DetectionIntervalStr string                `json:"detection_interval" schema:"type:string,description:Interval between community detection runs (e.g. 30s or 5m),category:basic"`
	BatchSize            int                   `json:"batch_size" schema:"type:int,description:Event count threshold for triggering detection,category:basic"`
	EnableLLM            bool                  `json:"enable_llm" schema:"type:bool,description:Enable LLM-based community summarization (requires model registry with community_summary capability),category:advanced"`
	EnhancementWorkers   int                   `json:"enhancement_workers" schema:"type:int,description:Number of parallel workers for LLM enhancement (default 5),category:advanced"`
	MinCommunitySize     int                   `json:"min_community_size" schema:"type:int,description:Minimum number of entities to form a community,category:advanced"`
	MaxIterations        int                   `json:"max_iterations" schema:"type:int,description:Maximum iterations for LPA algorithm,category:advanced"`

	// Structural analysis (optional, enables anomaly detection)
	EnableStructural bool `json:"enable_structural" schema:"type:bool,description:Enable structural index computation (k-core and pivot distance),category:advanced"`
	PivotCount       int  `json:"pivot_count" schema:"type:int,description:Number of pivot nodes for distance indexing (default 16),category:advanced"`
	MaxHopDistance   int  `json:"max_hop_distance" schema:"type:int,description:Maximum BFS traversal depth (default 10),category:advanced"`

	// Anomaly detection (optional, requires EnableStructural)
	EnableAnomalyDetection bool             `json:"enable_anomaly_detection" schema:"type:bool,description:Enable anomaly detection after structural computation,category:advanced"`
	AnomalyConfig          inference.Config `json:"anomaly_config" schema:"type:object,description:Configuration for anomaly detection,category:advanced"`

	// Dependency startup configuration
	StartupAttempts int `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts to wait for dependencies at startup,category:advanced"`
	StartupInterval int `json:"startup_interval_ms,omitempty" schema:"type:int,description:Interval between startup attempts in milliseconds,category:advanced"`

	// Parsed duration (set by ApplyDefaults)
	detectionInterval time.Duration
}

// Validate implements component.Validatable interface
func (c *Config) Validate() error {
	if c.Ports == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "ports configuration required")
	}
	if len(c.Ports.Inputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one input port required")
	}
	if len(c.Ports.Outputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one output port required")
	}

	// Validate COMMUNITY_INDEX output exists
	hasCommunityIndex := false
	for _, output := range c.Ports.Outputs {
		if output.Subject == graph.BucketCommunityIndex {
			hasCommunityIndex = true
			break
		}
	}
	if !hasCommunityIndex {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", fmt.Sprintf("%s output required", graph.BucketCommunityIndex))
	}

	// Validate detection interval (parsed duration must be positive)
	if c.detectionInterval <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "detection_interval must be greater than 0")
	}

	// Validate min community size
	if c.MinCommunitySize <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "min_community_size must be greater than 0")
	}

	// Validate max iterations
	if c.MaxIterations <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "max_iterations must be greater than 0")
	}

	// Anomaly detection requires structural analysis
	if c.EnableAnomalyDetection && !c.EnableStructural {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "enable_anomaly_detection requires enable_structural to be true")
	}

	// Validate anomaly config if anomaly detection is enabled
	if c.EnableAnomalyDetection && c.AnomalyConfig.Enabled {
		if err := c.AnomalyConfig.Validate(); err != nil {
			return errs.Wrap(err, "Config", "Validate", "anomaly_config")
		}
	}

	return nil
}

// DetectionInterval returns the parsed detection interval duration
func (c *Config) DetectionInterval() time.Duration {
	return c.detectionInterval
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	// Parse detection interval from string
	if c.DetectionIntervalStr != "" {
		if d, err := time.ParseDuration(c.DetectionIntervalStr); err == nil {
			c.detectionInterval = d
		}
	}
	if c.detectionInterval == 0 {
		c.detectionInterval = 30 * time.Second
	}

	if c.BatchSize == 0 {
		c.BatchSize = 100
	}
	// EnableLLM defaults to false (zero value)
	if c.MinCommunitySize == 0 {
		c.MinCommunitySize = 3
	}
	if c.MaxIterations == 0 {
		c.MaxIterations = 100
	}
	if c.EnhancementWorkers == 0 {
		c.EnhancementWorkers = 5 // Increased from default 3 for better parallelism
	}
	// Structural analysis defaults
	if c.EnableStructural {
		if c.PivotCount == 0 {
			c.PivotCount = 16 // Default from structural package
		}
		if c.MaxHopDistance == 0 {
			c.MaxHopDistance = 10 // Default maximum BFS depth
		}
	}
	// Anomaly detection defaults
	if c.EnableAnomalyDetection {
		c.AnomalyConfig.ApplyDefaults()
	}

	// Dependency startup defaults
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 30 // ~15 seconds with 500ms interval
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 // milliseconds
	}

	// Add optional output ports based on enabled features
	if c.Ports != nil {
		// Add STRUCTURAL_INDEX output when structural analysis is enabled
		if c.EnableStructural {
			hasStructural := false
			for _, o := range c.Ports.Outputs {
				if o.Subject == graph.BucketStructuralIndex {
					hasStructural = true
					break
				}
			}
			if !hasStructural {
				c.Ports.Outputs = append(c.Ports.Outputs, component.PortDefinition{
					Name:    "structural_index",
					Type:    "kv-write",
					Subject: graph.BucketStructuralIndex,
				})
			}
		}

		// Add ANOMALY_INDEX output when anomaly detection is enabled
		if c.EnableAnomalyDetection {
			hasAnomaly := false
			for _, o := range c.Ports.Outputs {
				if o.Subject == graph.BucketAnomalyIndex {
					hasAnomaly = true
					break
				}
			}
			if !hasAnomaly {
				c.Ports.Outputs = append(c.Ports.Outputs, component.PortDefinition{
					Name:    "anomaly_index",
					Type:    "kv-write",
					Subject: graph.BucketAnomalyIndex,
				})
			}
		}
	}

	if c.Ports == nil {
		// Apply full default port config
		defaultConf := DefaultConfig()
		c.Ports = defaultConf.Ports
	} else {
		// If ports exist but are empty, populate with defaults
		if len(c.Ports.Inputs) == 0 {
			c.Ports.Inputs = []component.PortDefinition{
				{
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			}
		}
		if len(c.Ports.Outputs) == 0 {
			c.Ports.Outputs = []component.PortDefinition{
				{
					Name:    "communities",
					Type:    "kv-write",
					Subject: graph.BucketCommunityIndex,
				},
			}
		}
	}
}

// DefaultConfig returns a valid default configuration
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:    "communities",
					Type:    "kv-write",
					Subject: graph.BucketCommunityIndex,
				},
			},
		},
		detectionInterval: 30 * time.Second,
		BatchSize:         100,
		EnableLLM:         false,
		MinCommunitySize:  3,
		MaxIterations:     100,
	}
}

// schema defines the configuration schema for graph-clustering component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the graph-clustering processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient    *natsclient.Client
	logger        *slog.Logger
	modelRegistry model.RegistryReader

	// Domain resources
	communityBucket jetstream.KeyValue
	entityBucket    jetstream.KeyValue
	outgoingBucket  jetstream.KeyValue
	incomingBucket  jetstream.KeyValue

	// Community detection
	detector clustering.CommunityDetector
	storage  *clustering.NATSCommunityStorage

	// Structural analysis (optional)
	structuralBucket  jetstream.KeyValue
	structuralStorage *structural.NATSStructuralIndexStorage
	graphProvider     clustering.Provider    // shared with detector
	previousKCore     *structural.KCoreIndex // for demotion detection

	// Anomaly detection (optional, requires structural)
	anomalyBucket       jetstream.KeyValue
	anomalyStorage      inference.Storage
	anomalyOrchestrator *inference.Orchestrator
	similarityFinder    inference.SimilarityFinder // for semantic gap detection

	// LLM enhancement (optional)
	enhancementWorker *clustering.EnhancementWorker
	llmClient         llm.Client

	// Review worker (optional, for anomaly approval workflow). The review
	// worker may use a dedicated LLM client when the operator binds
	// model.CapabilityAnomalyReview to a different endpoint than
	// community_summary; otherwise it shares llmClient.
	reviewWorker    *inference.ReviewWorker
	reviewLLMClient llm.Client // non-nil only when distinct from llmClient

	// Lifecycle state
	mu                sync.RWMutex
	running           bool
	initialized       bool
	startTime         time.Time
	wg                sync.WaitGroup
	cancel            context.CancelFunc
	lifecycleReporter component.LifecycleReporter

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

// CreateGraphClustering is the factory function for creating graph-clustering components
func CreateGraphClustering(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphClustering", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-clustering")

	// Create component
	comp := &Component{
		name:          "graph-clustering",
		config:        config,
		natsClient:    natsClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-clustering factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-clustering", &component.Registration{
		Name:        "graph-clustering",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Graph community detection and clustering processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphClustering,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-clustering",
		Type:        "processor",
		Description: "Graph community detection and clustering processor",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, 0, len(c.config.Ports.Inputs))
	for _, portDef := range c.config.Ports.Inputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionInput))
	}
	return ports
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, 0, len(c.config.Ports.Outputs))
	for _, portDef := range c.config.Ports.Outputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionOutput))
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}

	errorCount := int(atomic.LoadInt64(&c.errors))
	lastErr := ""
	status := "stopped"

	if c.running {
		status = "running"
		if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy:    c.running && errorCount == 0,
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
		LastError:  lastErr,
		Uptime:     uptime,
		Status:     status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	messages := atomic.LoadInt64(&c.messagesProcessed)
	bytes := atomic.LoadInt64(&c.bytesProcessed)
	errorCount := atomic.LoadInt64(&c.errors)

	c.mu.RLock()
	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}
	c.mu.RUnlock()

	// Calculate rates
	var messagesPerSec, bytesPerSec, errorRate float64
	if uptime > 0 {
		seconds := uptime.Seconds()
		messagesPerSec = float64(messages) / seconds
		bytesPerSec = float64(bytes) / seconds
		if messages > 0 {
			errorRate = float64(errorCount) / float64(messages)
		}
	}

	lastAct := time.Now()
	if stored := c.lastActivity.Load(); stored != nil {
		if t, ok := stored.(time.Time); ok {
			lastAct = t
		}
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSec,
		BytesPerSecond:    bytesPerSec,
		ErrorRate:         errorRate,
		LastActivity:      lastAct,
	}
}

// ============================================================================
// LifecycleComponent Interface (3 methods)
// ============================================================================

// Initialize validates configuration and sets up ports (no I/O)
func (c *Component) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil // Idempotent
	}

	// Validate configuration
	if err := c.config.Validate(); err != nil {
		return errs.Wrap(err, "Component", "Initialize", "config validation")
	}

	c.initialized = true
	c.logger.Info("component initialized", slog.String("component", "graph-clustering"))

	return nil
}

// Start begins processing (must be initialized first)
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	// Check initialization
	if !c.initialized {
		return errs.WrapFatal(errs.ErrInvalidConfig, "Component", "Start", "component not initialized")
	}

	// Idempotent - already running
	if c.running {
		return nil
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "context cancelled")
	}

	// Create COMMUNITY_INDEX bucket (we are the WRITER)
	communityBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketCommunityIndex,
		Description: "Community detection index",
	})
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during bucket creation")
		}
		return errs.Wrap(err, "Component", "Start", fmt.Sprintf("KV bucket creation: %s", graph.BucketCommunityIndex))
	}
	c.communityBucket = communityBucket

	// Create community storage for the detector
	c.storage = clustering.NewNATSCommunityStorage(communityBucket)

	// Set up query handlers
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Initialize lifecycle reporter and wait for dependencies
	c.initLifecycleReporter(ctx)

	if err := c.waitForDependencies(ctx); err != nil {
		cancel()
		return err
	}

	// Create graph provider and detector
	c.initProviderAndDetector()

	// Initialize structural analysis if enabled
	if c.config.EnableStructural {
		if err := c.initStructural(ctx); err != nil {
			c.logger.Warn("failed to initialize structural analysis, continuing without it",
				slog.Any("error", err))
		}
	}

	// Initialize anomaly detection if enabled (requires structural)
	if c.config.EnableAnomalyDetection && c.structuralStorage != nil {
		if err := c.initAnomalyDetection(ctx); err != nil {
			c.logger.Warn("failed to initialize anomaly detection, continuing without it",
				slog.Any("error", err))
		}
	}

	// Start LLM enhancement worker if enabled
	if c.config.EnableLLM {
		if err := c.startEnhancementWorker(ctx, c.graphProvider); err != nil {
			c.logger.Warn("failed to start enhancement worker, continuing without LLM",
				slog.Any("error", err))
		}
	}

	// Start review worker if enabled (for anomaly approval workflow)
	if c.config.AnomalyConfig.Review.Enabled && c.anomalyStorage != nil {
		if err := c.startReviewWorker(ctx); err != nil {
			c.logger.Warn("failed to start review worker, continuing without anomaly review",
				slog.Any("error", err))
		}
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Start detection loop goroutine
	c.wg.Add(1)
	go c.runDetectionLoop(ctx)

	c.logger.Info("component started",
		slog.String("component", "graph-clustering"),
		slog.Time("start_time", c.startTime),
		slog.Duration("detection_interval", c.config.DetectionInterval()),
		slog.Bool("enable_llm", c.config.EnableLLM),
		slog.Bool("enable_structural", c.config.EnableStructural),
		slog.Bool("enable_anomaly_detection", c.config.EnableAnomalyDetection))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}

	// Unsubscribe from query handlers
	for _, sub := range c.querySubscriptions {
		if sub != nil {
			if err := sub.Unsubscribe(); err != nil {
				c.logger.Warn("query subscription unsubscribe error", slog.Any("error", err))
			}
		}
	}
	c.querySubscriptions = nil

	// Stop review worker if running
	if c.reviewWorker != nil {
		if err := c.reviewWorker.Stop(); err != nil {
			c.logger.Warn("review worker stop error", slog.Any("error", err))
		}
	}

	// Stop enhancement worker if running
	if c.enhancementWorker != nil {
		if err := c.enhancementWorker.Stop(); err != nil {
			c.logger.Warn("enhancement worker stop error", slog.Any("error", err))
		}
	}

	// Close LLM client if present
	if c.llmClient != nil {
		if err := c.llmClient.Close(); err != nil {
			c.logger.Warn("LLM client close error", slog.Any("error", err))
		}
	}

	// Close the dedicated review LLM client if distinct from the shared one.
	if c.reviewLLMClient != nil {
		if err := c.reviewLLMClient.Close(); err != nil {
			c.logger.Warn("review LLM client close error", slog.Any("error", err))
		}
	}

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-clustering"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-clustering"))
		return errs.WrapTransient(errors.New("timeout"), "Component", "Stop", "graceful shutdown timeout")
	}
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketComponentStatus,
		Description: "Component lifecycle status",
	})
	if err != nil {
		c.logger.Warn("failed to create status bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewKVLifecycleReporter(statusBucket, "graph-clustering", c.logger)
}

// waitForDependencies waits for all required KV buckets and stores references.
func (c *Component) waitForDependencies(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.Wrap(err, "Component", "waitForDependencies", "JetStream connection")
	}

	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = time.Duration(c.config.StartupInterval) * time.Millisecond
	watcherCfg.Logger = c.logger

	// Wait for ENTITY_STATES bucket
	entityBucket, err := c.waitForBucket(ctx, js, graph.BucketEntityStates, watcherCfg)
	if err != nil {
		return err
	}
	c.entityBucket = entityBucket

	// Wait for OUTGOING_INDEX bucket
	outgoingBucket, err := c.waitForBucket(ctx, js, graph.BucketOutgoingIndex, watcherCfg)
	if err != nil {
		return err
	}
	c.outgoingBucket = outgoingBucket

	// Wait for INCOMING_INDEX bucket
	incomingBucket, err := c.waitForBucket(ctx, js, graph.BucketIncomingIndex, watcherCfg)
	if err != nil {
		return err
	}
	c.incomingBucket = incomingBucket

	return nil
}

// waitForBucket waits for a KV bucket to become available and returns it.
func (c *Component) waitForBucket(ctx context.Context, js jetstream.JetStream, bucketName string, cfg resource.Config) (jetstream.KeyValue, error) {
	if err := c.lifecycleReporter.ReportStage(ctx, "waiting_for_"+bucketName); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "waiting_for_"+bucketName), slog.Any("error", err))
	}

	watcher := resource.NewWatcher(
		bucketName,
		func(checkCtx context.Context) error {
			_, err := js.KeyValue(checkCtx, bucketName)
			return err
		},
		cfg,
	)

	if !watcher.WaitForStartup(ctx) {
		return nil, errs.WrapTransient(
			fmt.Errorf("bucket %s not available after %d attempts", bucketName, c.config.StartupAttempts),
			"Component", "waitForBucket", "dependency not available",
		)
	}

	bucket, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		return nil, errs.Wrap(err, "Component", "waitForBucket", fmt.Sprintf("get %s bucket", bucketName))
	}
	return bucket, nil
}

// initProviderAndDetector creates the graph provider and community detector.
func (c *Component) initProviderAndDetector() {
	provider := newKVProvider(c.entityBucket, c.outgoingBucket, c.incomingBucket, c.logger)

	entityIDProvider := clustering.NewEntityIDProvider(
		provider,
		clustering.DefaultEntityIDProviderConfig(),
		c.logger,
	)

	entityQuerier := &kvEntityQuerier{entityBucket: c.entityBucket, logger: c.logger}
	summarizer := clustering.NewStatisticalSummarizer()

	detector := clustering.NewLPADetector(entityIDProvider, c.storage).
		WithLogger(c.logger).
		WithMaxIterations(c.config.MaxIterations).
		WithLevels(3).
		WithSummarizer(summarizer)

	detector.SetEntityProvider(entityQuerier)
	c.detector = detector
	c.graphProvider = entityIDProvider
}

// reportStage safely reports a lifecycle stage change.
// Errors are logged but do not interrupt processing.
func (c *Component) reportStage(ctx context.Context, stage string) {
	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, stage); err != nil {
			c.logger.Debug("failed to report lifecycle stage",
				slog.String("stage", stage),
				slog.Any("error", err))
		}
	}
}

// runDetectionLoop runs community detection on a timer
func (c *Component) runDetectionLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.DetectionInterval())
	defer ticker.Stop()

	// Report initial idle state
	c.reportStage(ctx, "idle")

	c.logger.Info("detection loop started",
		slog.Duration("interval", c.config.DetectionInterval()))

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("detection loop stopping")
			return
		case <-ticker.C:
			// Double-check context before starting new detection
			// This prevents starting a new cycle if shutdown just began
			if ctx.Err() != nil {
				c.logger.Debug("detection loop stopping - context cancelled")
				return
			}
			c.runCommunityDetection(ctx)
		}
	}
}

// handleDetectionError handles errors during detection, returning true if the error was handled as shutdown.
func (c *Component) handleDetectionError(ctx context.Context, err error, operation string) bool {
	if errors.Is(err, context.Canceled) {
		c.logger.Debug(operation + " interrupted by shutdown")
		return true
	}
	c.logger.Error(operation+" failed", slog.Any("error", err))
	atomic.AddInt64(&c.errors, 1)
	if c.lifecycleReporter != nil {
		if repErr := c.lifecycleReporter.ReportCycleError(ctx, err); repErr != nil {
			c.logger.Debug("failed to report cycle error", slog.Any("error", repErr))
		}
	}
	return false
}

// runStructuralAndAnomalyDetection runs structural computation and anomaly detection if enabled.
func (c *Component) runStructuralAndAnomalyDetection(ctx context.Context) bool {
	if !c.config.EnableStructural {
		return true
	}
	if ctx.Err() != nil {
		c.logger.Debug("skipping structural computation - shutdown in progress")
		return false
	}

	c.reportStage(ctx, "structural_computation")
	kcoreIndex, pivotIndex, err := c.runStructuralComputation(ctx)
	if err != nil {
		c.handleDetectionError(ctx, err, "structural computation")
		return false
	}

	if c.config.EnableAnomalyDetection && kcoreIndex != nil {
		if ctx.Err() != nil {
			c.logger.Debug("skipping anomaly detection - shutdown in progress")
			return false
		}
		c.reportStage(ctx, "anomaly_detection")
		if err := c.runAnomalyDetection(ctx, kcoreIndex, pivotIndex); err != nil {
			c.handleDetectionError(ctx, err, "anomaly detection")
			return false
		}
	}
	return true
}

// runCommunityDetection executes the community detection algorithm
func (c *Component) runCommunityDetection(ctx context.Context) {
	if ctx.Err() != nil {
		c.logger.Debug("skipping detection - shutdown in progress")
		return
	}

	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportCycleStart(ctx); err != nil {
			c.logger.Debug("failed to report cycle start", slog.Any("error", err))
		}
	}

	c.logger.Debug("running community detection")
	start := time.Now()
	c.reportStage(ctx, "community_detection")

	communities, err := c.detector.DetectCommunities(ctx)
	if err != nil {
		c.handleDetectionError(ctx, err, "detection")
		return
	}

	totalCommunities := 0
	for _, levelCommunities := range communities {
		totalCommunities += len(levelCommunities)
	}

	atomic.AddInt64(&c.messagesProcessed, int64(totalCommunities))
	c.lastActivity.Store(time.Now())

	c.logger.Debug("community detection complete",
		slog.Int("communities_found", totalCommunities),
		slog.Int("levels", len(communities)),
		slog.Duration("duration", time.Since(start)))

	if !c.runStructuralAndAnomalyDetection(ctx) {
		return
	}

	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportCycleComplete(ctx); err != nil {
			c.logger.Debug("failed to report cycle complete", slog.Any("error", err))
		}
	}
}

// ============================================================================
// KV-based Graph Provider for Community Detection
// ============================================================================

// kvProvider implements clustering.Provider using NATS KV buckets
type kvProvider struct {
	entityBucket   jetstream.KeyValue
	outgoingBucket jetstream.KeyValue
	incomingBucket jetstream.KeyValue
	logger         *slog.Logger
}

// newKVProvider creates a graph provider that reads from KV buckets
func newKVProvider(
	entityBucket jetstream.KeyValue,
	outgoingBucket jetstream.KeyValue,
	incomingBucket jetstream.KeyValue,
	logger *slog.Logger,
) *kvProvider {
	return &kvProvider{
		entityBucket:   entityBucket,
		outgoingBucket: outgoingBucket,
		incomingBucket: incomingBucket,
		logger:         logger,
	}
}

// GetAllEntityIDs returns all entity IDs from the ENTITY_STATES bucket
func (p *kvProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	keys, err := p.entityBucket.Keys(ctx)
	if err != nil {
		// Empty bucket returns an error in some cases
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "kvProvider", "GetAllEntityIDs", "list keys")
	}
	return keys, nil
}

// GetNeighbors returns entity IDs connected to the given entity
func (p *kvProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if entityID == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "kvProvider", "GetNeighbors", "entityID is empty")
	}

	neighbors := make(map[string]bool)

	// Get outgoing neighbors
	if direction == "outgoing" || direction == "both" {
		outgoing, err := p.getNeighborsFromBucket(ctx, p.outgoingBucket, entityID)
		if err != nil {
			p.logger.Debug("failed to get outgoing neighbors", slog.String("entity", entityID), slog.Any("error", err))
		}
		for _, n := range outgoing {
			neighbors[n] = true
		}
	}

	// Get incoming neighbors
	if direction == "incoming" || direction == "both" {
		incoming, err := p.getNeighborsFromBucket(ctx, p.incomingBucket, entityID)
		if err != nil {
			p.logger.Debug("failed to get incoming neighbors", slog.String("entity", entityID), slog.Any("error", err))
		}
		for _, n := range incoming {
			neighbors[n] = true
		}
	}

	result := make([]string, 0, len(neighbors))
	for n := range neighbors {
		result = append(result, n)
	}
	return result, nil
}

// relationshipEntry represents a relationship in the index buckets
type relationshipEntry struct {
	Predicate    string `json:"predicate"`
	ToEntityID   string `json:"to_entity_id,omitempty"`   // For OUTGOING_INDEX
	FromEntityID string `json:"from_entity_id,omitempty"` // For INCOMING_INDEX
}

// getNeighborsFromBucket reads neighbor entity IDs from a relationship index bucket
func (p *kvProvider) getNeighborsFromBucket(ctx context.Context, bucket jetstream.KeyValue, entityID string) ([]string, error) {
	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // No neighbors found
		}
		return nil, err
	}

	// Parse the index entry - format is a list of relationship entries
	var relationships []relationshipEntry
	if err := json.Unmarshal(entry.Value(), &relationships); err != nil {
		return nil, err
	}

	neighbors := make([]string, 0, len(relationships))
	for _, rel := range relationships {
		// Use whichever ID field is populated
		if rel.ToEntityID != "" {
			neighbors = append(neighbors, rel.ToEntityID)
		} else if rel.FromEntityID != "" {
			neighbors = append(neighbors, rel.FromEntityID)
		}
	}
	return neighbors, nil
}

// GetEdgeWeight returns the weight of the edge between two entities
func (p *kvProvider) GetEdgeWeight(_ context.Context, _, _ string) (float64, error) {
	// For now, return 1.0 for all edges (equal weight)
	// Could be enhanced to read confidence from the relationship data
	return 1.0, nil
}

// ============================================================================
// LLM Enhancement Support
// ============================================================================

// startEnhancementWorker initializes and starts the LLM enhancement worker
func (c *Component) startEnhancementWorker(ctx context.Context, provider clustering.Provider) error {
	// Resolve endpoint AND full config — direct ResolveEndpoint silently strips
	// the connection-hygiene fields (DisableKeepAlives, IdleConnTimeout,
	// ResponseHeaderTimeout) and per-endpoint RequestTimeout that the LLM
	// client builder needs to honour the operator's capability config.
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityCommunitySummary)
	if err != nil {
		return errs.Wrap(err, "Component", "startEnhancementWorker", "resolve community_summary endpoint")
	}

	// Probe LLM endpoint before committing workers — a fast health check
	// prevents per-community blocking when the endpoint is unreachable
	// (e.g., seminstruct unhealthy, LLM disabled in deployment).
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := probeLLMEndpoint(probeCtx, resolved.URL); err != nil {
		probeCancel()
		return errs.WrapTransient(err, "Component", "startEnhancementWorker",
			fmt.Sprintf("LLM endpoint unreachable at %s", resolved.URL))
	}
	probeCancel()

	// Create LLM client. OpenAIConfigFromEndpoint plumbs connection-hygiene
	// fields; ResolveCapabilityTimeout applies the endpoint > capability >
	// default precedence chain so configured 300s reaches the HTTP client.
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityCommunitySummary, defaultCommunitySummaryTimeout, c.logger)
	llmClient, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create LLM client")
	}
	c.llmClient = llmClient

	// Create LLM summarizer
	llmSummarizer, err := clustering.NewLLMSummarizer(clustering.LLMSummarizerConfig{
		Client:    llmClient,
		MaxTokens: 200,
	})
	if err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create LLM summarizer")
	}

	// Create entity querier from entity bucket
	querier := newKVEntityQuerier(c.entityBucket, c.logger)

	// Create enhancement worker. LLMTimeout matches cfg.Timeout so the
	// inner ctx.WithTimeout that wraps each LLM round-trip respects the
	// resolved capability timeout — without this the inner default (30s)
	// caps the effective ceiling regardless of HTTP-client configuration.
	worker, err := clustering.NewEnhancementWorker(&clustering.EnhancementWorkerConfig{
		LLMSummarizer:   llmSummarizer,
		Storage:         c.storage,
		Provider:        provider,
		Querier:         querier,
		CommunityBucket: c.communityBucket,
		Logger:          c.logger,
		LLMTimeout:      cfg.Timeout,
	})
	if err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create enhancement worker")
	}

	// Configure worker parallelism
	worker.WithWorkers(c.config.EnhancementWorkers)

	// Start the worker
	if err := worker.Start(ctx); err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "start enhancement worker")
	}

	c.enhancementWorker = worker
	c.logger.Info("LLM enhancement worker started",
		slog.String("endpoint", resolved.URL),
		slog.String("model", resolved.Model),
		slog.Int("workers", c.config.EnhancementWorkers))

	// Start background health monitor that pauses/resumes the worker based
	// on LLM endpoint availability. Checks every 30s so recovery is automatic
	// when the endpoint comes back.
	c.wg.Add(1)
	go c.monitorLLMHealth(ctx, resolved.URL, worker)

	return nil
}

// probeLLMEndpoint performs a fast connectivity check against the LLM service.
// Returns nil if the endpoint responds (any status), error if unreachable.
func probeLLMEndpoint(ctx context.Context, baseURL string) error {
	probeURL := strings.TrimSuffix(baseURL, "/v1") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// monitorLLMHealth periodically probes the LLM endpoint and pauses/resumes
// the enhancement worker based on availability. This prevents workers from
// blocking on 30s timeouts when the endpoint goes down mid-operation, and
// automatically resumes when it recovers.
func (c *Component) monitorLLMHealth(ctx context.Context, endpointURL string, worker *clustering.EnhancementWorker) {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := probeLLMEndpoint(probeCtx, endpointURL)
			cancel()

			if err != nil && !worker.IsPaused() {
				c.logger.Warn("LLM endpoint unreachable, pausing enhancement worker",
					slog.String("endpoint", endpointURL),
					slog.Any("error", err))
				worker.Pause()
			} else if err == nil && worker.IsPaused() {
				c.logger.Info("LLM endpoint recovered, resuming enhancement worker",
					slog.String("endpoint", endpointURL))
				worker.Resume()
			}
		}
	}
}

// startReviewWorker initializes and starts the anomaly review worker.
// The review worker processes pending anomalies and can auto-approve/reject
// based on confidence thresholds, optionally using LLM for uncertain cases.
//
// LLM client selection precedence:
//  1. If model.CapabilityAnomalyReview is bound in the registry, create a
//     dedicated client for that endpoint. Stored in c.reviewLLMClient for
//     cleanup.
//  2. Otherwise fall back to c.llmClient (the community_summary endpoint),
//     preserving the pre-capability piggyback behavior. May be nil for
//     human-only mode.
func (c *Component) startReviewWorker(ctx context.Context) error {
	// Create relationship applier for approved anomalies
	// Uses the mutation API to go through graph-ingest for proper indexing
	applier := inference.NewMutationRelationshipApplier(c.natsClient, c.logger)

	reviewClient := c.resolveReviewLLMClient()

	// Resolve the capability timeout into ReviewConfig.ReviewTimeout so the
	// inner ctx.WithTimeout that wraps each review LLM call respects the
	// model-registry config. AnomalyConfig.Review.ReviewTimeout becomes the
	// fall-through default — endpoint.request_timeout > capability.timeout >
	// AnomalyConfig.Review.ReviewTimeout. Operators previously had to set
	// the inference-config knob; now anomaly_review.timeout in the model
	// registry suffices.
	reviewConfig := c.config.AnomalyConfig.Review
	if c.modelRegistry != nil && c.modelRegistry.GetCapability(model.CapabilityAnomalyReview) != nil {
		reviewConfig.ReviewTimeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityAnomalyReview, reviewConfig.ReviewTimeout, c.logger)
	}

	// Create review worker - reviewClient may be nil for human-only mode
	reviewWorker, err := inference.NewReviewWorker(&inference.ReviewWorkerConfig{
		AnomalyBucket: c.anomalyBucket,
		Storage:       c.anomalyStorage,
		LLMClient:     reviewClient,
		Applier:       applier,
		Config:        reviewConfig,
		Logger:        c.logger,
	})
	if err != nil {
		return errs.Wrap(err, "Component", "startReviewWorker", "create review worker")
	}
	c.reviewWorker = reviewWorker

	// Start the worker
	if err := c.reviewWorker.Start(ctx); err != nil {
		c.reviewWorker = nil
		return errs.Wrap(err, "Component", "startReviewWorker", "start review worker")
	}

	c.logger.Info("review worker started",
		slog.Int("workers", c.config.AnomalyConfig.Review.Workers),
		slog.Bool("llm_enabled", reviewClient != nil),
		slog.Bool("llm_dedicated", c.reviewLLMClient != nil),
		slog.Float64("auto_approve_threshold", c.config.AnomalyConfig.Review.AutoApproveThreshold),
		slog.Float64("auto_reject_threshold", c.config.AnomalyConfig.Review.AutoRejectThreshold))

	return nil
}

// resolveReviewLLMClient returns the LLM client the review worker should
// use. Creates a dedicated client only when CapabilityAnomalyReview is
// *explicitly* bound in the registry — otherwise (capability not present,
// or any client construction error) falls back to c.llmClient, preserving
// the legacy piggyback on the community_summary endpoint.
//
// We check explicit binding via GetCapability rather than ResolveEndpoint
// because Resolve falls through to defaults.model for unknown capabilities;
// using ResolveEndpoint would create a dedicated client for every
// deployment even when no operator opted in, which doubles the connection
// pool against the same endpoint and defeats the point of the fallback.
func (c *Component) resolveReviewLLMClient() llm.Client {
	if c.modelRegistry == nil {
		return c.llmClient
	}
	if c.modelRegistry.GetCapability(model.CapabilityAnomalyReview) == nil {
		// Capability not explicitly bound — preserve legacy piggyback.
		return c.llmClient
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityAnomalyReview)
	if err != nil {
		c.logger.Warn("anomaly_review capability bound but endpoint resolution failed; falling back to community_summary client",
			slog.Any("error", err))
		return c.llmClient
	}
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityAnomalyReview, defaultAnomalyReviewTimeout, c.logger)
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		c.logger.Warn("failed to create dedicated anomaly review LLM client; falling back to community_summary client",
			slog.Any("error", err),
			slog.String("endpoint", resolved.URL))
		return c.llmClient
	}
	c.reviewLLMClient = client
	c.logger.Info("anomaly review using dedicated LLM endpoint",
		slog.String("endpoint", resolved.URL),
		slog.String("model", resolved.Model))
	return client
}

// ============================================================================
// KV-based Entity Querier for Enhancement Worker
// ============================================================================

// kvEntityQuerier implements clustering.EntityQuerier using NATS KV
type kvEntityQuerier struct {
	entityBucket jetstream.KeyValue
	logger       *slog.Logger
}

// newKVEntityQuerier creates an entity querier that reads from ENTITY_STATES
func newKVEntityQuerier(entityBucket jetstream.KeyValue, logger *slog.Logger) *kvEntityQuerier {
	return &kvEntityQuerier{
		entityBucket: entityBucket,
		logger:       logger,
	}
}

// GetEntities retrieves entities by their IDs from ENTITY_STATES bucket
func (q *kvEntityQuerier) GetEntities(ctx context.Context, ids []string) ([]*graph.EntityState, error) {
	entities := make([]*graph.EntityState, 0, len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		entry, err := q.entityBucket.Get(ctx, id)
		if err != nil {
			if err == jetstream.ErrKeyNotFound {
				q.logger.Debug("entity not found", slog.String("id", id))
				continue
			}
			return nil, errs.WrapTransient(err, "kvEntityQuerier", "GetEntities", "get entity")
		}

		var entity graph.EntityState
		if err := json.Unmarshal(entry.Value(), &entity); err != nil {
			q.logger.Warn("failed to unmarshal entity", slog.String("id", id), slog.Any("error", err))
			continue
		}

		entities = append(entities, &entity)
	}

	return entities, nil
}
