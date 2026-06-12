// Package graphingest provides the graph-ingest component for entity and triple ingestion.
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Package-level prometheus metric (registered once to avoid duplicate registration errors)
var (
	metricsOnce         sync.Once
	entitiesUpdatedOnce prometheus.Counter

	indexingProfileOnce       sync.Once
	indexingProfileDefaultVec *prometheus.CounterVec
)

// entityIDRegex validates entity ID format: org.platform.domain.system.type.instance
// Example: c360.ops.robotics.gcs.drone.001 or c360.logistics.environmental.sensor.humidity.humid-sensor-001
// Each part must start with alphanumeric and can contain alphanumeric, hyphens, or underscores
var entityIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func getEntitiesUpdatedMetric(registry *metric.MetricsRegistry) prometheus.Counter {
	metricsOnce.Do(func() {
		entitiesUpdatedOnce = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "entities_updated_total",
			Help:      "Total entities updated",
		})
		// Register with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounter("graph-ingest", "entities_updated_total", entitiesUpdatedOnce)
		} else {
			// Fallback to default prometheus registry for testing
			// Ignore error if already registered (can happen across tests)
			_ = prometheus.DefaultRegisterer.Register(entitiesUpdatedOnce)
		}
	})
	return entitiesUpdatedOnce
}

// getIndexingProfileDefaultMetric returns the process-wide counter that fires
// whenever graph-ingest falls back to the indexing-profile floor at entity
// creation — i.e. neither a Graphable IndexingProfiler nor a mutation-envelope
// indexing_profile field declared a profile (ADR-054 §5). Labeled by
// message_type so operators can see WHICH producers omit a declaration: a new
// "content" type nobody declared would otherwise silently default to "control"
// and never be embedded. message_type is the low-cardinality registry key; the
// full entity subject is intentionally NOT a label (cardinality bomb).
func getIndexingProfileDefaultMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	indexingProfileOnce.Do(func() {
		indexingProfileDefaultVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "indexing_profile_default_total",
			Help:      "Entities whose message type was unclassified (no producer declaration AND not in the indexing-profile registry) and defaulted to control — a registry gap",
		}, []string{"message_type"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "indexing_profile_default_total", indexingProfileDefaultVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(indexingProfileDefaultVec)
		}
	})
	return indexingProfileDefaultVec
}

// Config holds configuration for graph-ingest component
type Config struct {
	Ports              *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	EnableHierarchy    bool                  `json:"enable_hierarchy" schema:"type:bool,description:Enable hierarchy inference,default:false,category:advanced"`
	EnableTypeSiblings *bool                 `json:"enable_type_siblings" schema:"type:bool,description:Enable sibling edges between same-type entities (default true when hierarchy enabled),category:advanced"`
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
	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	// EnableHierarchy defaults to false
	if c.Ports == nil {
		c.Ports = &component.PortConfig{}
	}
}

// DefaultConfig returns a valid default configuration
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:    "entity_stream",
					Type:    "jetstream",
					Subject: "entity.>",
					Config: component.JetStreamPort{
						DeliverPolicy: "all", // Idempotent: catch up on historical entities
					},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:    "entity_states",
					Type:    "kv-write",
					Subject: graph.BucketEntityStates,
				},
			},
		},
		EnableHierarchy: false,
	}
}

// schema defines the configuration schema for graph-ingest component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// entityManagerAdapter adapts Component to implement inference.EntityManager interface
type entityManagerAdapter struct {
	component *Component
}

func (a *entityManagerAdapter) ExistsEntity(ctx context.Context, id string) (bool, error) {
	_, err := a.component.entityBucket.Get(ctx, id)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *entityManagerAdapter) CreateEntity(ctx context.Context, entity *graph.EntityState) (*graph.EntityState, error) {
	err := a.component.CreateEntity(ctx, entity)
	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (a *entityManagerAdapter) ListWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	// Use server-side prefix filtering (prefix + "." to ensure we match the exact level)
	return a.component.entityBucket.KeysByPrefix(ctx, prefix+".")
}

// tripleAdderAdapter adapts Component to implement inference.TripleAdder interface
type tripleAdderAdapter struct {
	component *Component
}

func (a *tripleAdderAdapter) AddTriple(ctx context.Context, triple message.Triple) error {
	return a.component.AddTriple(ctx, triple)
}

// Component implements the graph-ingest processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	decoder    *message.Decoder
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Domain resources
	entityBucket *natsclient.KVStore            // KV operations with CAS support
	entityCache  cache.Cache[graph.EntityState] // Read-through cache for query handlers
	suffixBucket *natsclient.KVStore            // KV suffix index: suffix → fullID
	suffixCache  cache.Cache[string]            // TTL cache for suffix resolution

	// Inference components
	hierarchyInference *inference.HierarchyInference

	// Lifecycle state
	mu          sync.RWMutex
	running     bool
	initialized bool
	startTime   time.Time
	wg          sync.WaitGroup
	cancel      context.CancelFunc

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time

	// Prometheus metrics (for e2e test compatibility with datamanager metrics)
	entitiesUpdated        prometheus.Counter
	indexingProfileDefault *prometheus.CounterVec
	metricsRegistry        *metric.MetricsRegistry

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Query and mutation subscriptions (for cleanup)
	subscriptions []*natsclient.Subscription
}

// CreateGraphIngest is the factory function for creating graph-ingest components
func CreateGraphIngest(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphIngest", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphIngest", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphIngest", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-ingest")

	// Create component
	comp := &Component{
		name:                   "graph-ingest",
		config:                 config,
		decoder:                message.NewDecoder(deps.PayloadRegistry),
		natsClient:             natsClient,
		logger:                 logger,
		entitiesUpdated:        getEntitiesUpdatedMetric(deps.MetricsRegistry),
		indexingProfileDefault: getIndexingProfileDefaultMetric(deps.MetricsRegistry),
		metricsRegistry:        deps.MetricsRegistry,
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-ingest factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-ingest", &component.Registration{
		Name:        "graph-ingest",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Entity and triple ingestion processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphIngest,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-ingest",
		Type:        "processor",
		Description: "Entity and triple ingestion processor for graph system",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions.
// Reads directly from config so ports are available before Initialize().
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

// OutputPorts returns output port definitions.
// Reads directly from config so ports are available before Initialize().
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
	c.logger.Info("component initialized", slog.String("component", "graph-ingest"))

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
		return errs.WrapFatal(fmt.Errorf("component not initialized"), "Component", "Start", "initialization check")
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

	// Ensure NATS client is connected
	if c.natsClient.Status() != natsclient.StatusConnected {
		if err := c.natsClient.Connect(ctx); err != nil {
			cancel()
			// Check if this is a context-related error
			if ctx.Err() != nil {
				return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during NATS connection")
			}
			return errs.Wrap(err, "Component", "Start", "NATS connection failed")
		}
		if err := c.natsClient.WaitForConnection(ctx); err != nil {
			cancel()
			if ctx.Err() != nil {
				return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled waiting for NATS")
			}
			return errs.Wrap(err, "Component", "Start", "wait for NATS connection")
		}
	}

	// Initialize storage buckets and query caches
	if err := c.initStorage(ctx); err != nil {
		cancel()
		return err
	}

	// Initialize lifecycle reporter (throttled for high-throughput ingestion)
	c.initLifecycleReporter(ctx)

	// Initialize hierarchy inference if enabled (synchronous - no Start/Stop)
	c.initHierarchyInference()

	// Set up subscriptions for input ports
	if err := c.setupSubscriptions(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "subscription setup")
	}

	// Set up query handler subscriptions
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "query handler setup")
	}

	// Set up mutation handler subscriptions (for rule processor actions)
	if err := c.setupMutationHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "mutation handler setup")
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("component started",
		slog.String("component", "graph-ingest"),
		slog.Time("start_time", c.startTime))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}

	// Unsubscribe from query and mutation handlers
	for _, sub := range c.subscriptions {
		if sub != nil {
			if err := sub.Unsubscribe(); err != nil {
				c.logger.Warn("subscription unsubscribe error", slog.Any("error", err))
			}
		}
	}
	c.subscriptions = nil

	// Close caches
	if c.entityCache != nil {
		if err := c.entityCache.Close(); err != nil {
			c.logger.Warn("entity cache close error", slog.Any("error", err))
		}
	}
	if c.suffixCache != nil {
		if err := c.suffixCache.Close(); err != nil {
			c.logger.Warn("suffix cache close error", slog.Any("error", err))
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
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-ingest"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-ingest"))
		return fmt.Errorf("stop timeout after %v", timeout)
	}
}

// initStorage initializes KV buckets and query caches.
func (c *Component) initStorage(ctx context.Context) error {
	// Entity states KV bucket (create if not exists) - we are the WRITER
	bucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Entity state storage for graph-ingest",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "KV bucket creation")
	}
	c.entityBucket = c.natsClient.NewKVStore(bucket)

	// Entity query cache (HybridCache: LRU capacity + TTL freshness)
	entityCache, err := cache.NewFromConfig[graph.EntityState](ctx, cache.Config{
		Enabled:         true,
		Strategy:        cache.StrategyHybrid,
		MaxSize:         5000,
		TTL:             30 * time.Second,
		CleanupInterval: 10 * time.Second,
	}, cache.WithMetrics[graph.EntityState](c.metricsRegistry, "entity_query_cache"))
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "entity cache creation")
	}
	c.entityCache = entityCache

	// Suffix index KV bucket for fast suffix→fullID resolution
	suffixBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "ENTITY_SUFFIX_INDEX",
		Description: "Suffix-to-full-ID reverse index for partial entity ID resolution",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "suffix index bucket creation")
	}
	c.suffixBucket = c.natsClient.NewKVStore(suffixBucket)

	// Suffix resolution cache (stable mappings, long TTL)
	suffixCacheInst, err := cache.NewFromConfig[string](ctx, cache.Config{
		Enabled:         true,
		Strategy:        cache.StrategyTTL,
		MaxSize:         500,
		TTL:             5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, cache.WithMetrics[string](c.metricsRegistry, "suffix_resolution_cache"))
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "suffix cache creation")
	}
	c.suffixCache = suffixCacheInst

	return nil
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMPONENT_STATUS",
		Description: "Component lifecycle status tracking",
	})
	if err != nil {
		c.logger.Warn("Failed to create COMPONENT_STATUS bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
		KV:               statusBucket,
		ComponentName:    "graph-ingest",
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// initHierarchyInference initializes hierarchy inference if enabled.
func (c *Component) initHierarchyInference() {
	if !c.config.EnableHierarchy {
		return
	}

	// Enable sibling edges by default, can be disabled via config
	enableTypeSiblings := true
	if c.config.EnableTypeSiblings != nil {
		enableTypeSiblings = *c.config.EnableTypeSiblings
	}

	hierarchyConfig := inference.HierarchyConfig{
		Enabled:            true,
		CreateTypeEdges:    true,
		CreateSystemEdges:  true,
		CreateDomainEdges:  true,
		CreateTypeSiblings: enableTypeSiblings,
	}

	c.hierarchyInference = inference.NewHierarchyInference(
		&entityManagerAdapter{component: c},
		&tripleAdderAdapter{component: c},
		hierarchyConfig,
		c.logger,
	)
}

// ============================================================================
// Subscription Management
// ============================================================================

// setupSubscriptions sets up JetStream consumers for input ports
func (c *Component) setupSubscriptions(ctx context.Context) error {
	for _, port := range c.config.Ports.Inputs {
		if port.Type != "jetstream" {
			c.logger.Debug("skipping non-jetstream port", slog.String("port", port.Name), slog.String("type", port.Type))
			continue
		}

		if err := c.setupJetStreamConsumer(ctx, port); err != nil {
			return errs.Wrap(err, "Component", "setupSubscriptions",
				fmt.Sprintf("JetStream consumer for %s", port.Subject))
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	// Derive stream name from subject
	streamName := port.StreamName
	if streamName == "" {
		streamName = c.deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return fmt.Errorf("could not derive stream name for subject %s", port.Subject)
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(port.Subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("graph-ingest-%s", sanitizedSubject)

	c.logger.Debug("Setting up JetStream consumer",
		slog.String("stream", streamName),
		slog.String("consumer", consumerName),
		slog.String("filter_subject", port.Subject))

	// Get consumer config from port definition (allows user configuration)
	// graph-ingest defaults to "all" since it's idempotent (KV overwrites)
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: port.Subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	subject := port.Subject // capture for closure
	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleMessage(msgCtx, subject, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack JetStream message", slog.Any("error", ackErr))
		}
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}

	c.logger.Debug("graph-ingest subscribed (JetStream)",
		slog.String("subject", subject),
		slog.String("stream", streamName))
	return nil
}

// deriveStreamName derives a stream name from a subject pattern
func (c *Component) deriveStreamName(subject string) string {
	// Common mappings based on subject prefix
	prefixToStream := map[string]string{
		"sensor.":      "SENSOR",
		"objectstore.": "OBJECTSTORE",
		"entity.":      "ENTITY",
		"events.":      "EVENTS",
	}

	for prefix, stream := range prefixToStream {
		if strings.HasPrefix(subject, prefix) {
			return stream
		}
	}

	// Default: use first segment uppercased
	parts := strings.Split(subject, ".")
	if len(parts) > 0 {
		return strings.ToUpper(parts[0])
	}
	return ""
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			c.logger.Debug("Stream available", slog.String("stream", streamName))
			return nil
		}

		// Exponential backoff
		c.logger.Debug("Waiting for stream",
			slog.String("stream", streamName),
			slog.Int("attempt", i+1),
			slog.Duration("interval", retryInterval))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
			retryInterval = time.Duration(float64(retryInterval) * 1.5)
			if retryInterval > maxInterval {
				retryInterval = maxInterval
			}
		}
	}

	return fmt.Errorf("stream %s not available after %d retries", streamName, maxRetries)
}

// handleMessage processes an incoming message and creates/updates entity state
func (c *Component) handleMessage(ctx context.Context, subject string, data []byte) {
	// Report processing stage (throttled to avoid KV spam)
	if err := c.lifecycleReporter.ReportStage(ctx, "processing"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "processing"), slog.Any("error", err))
	}

	c.logger.Debug("Received message",
		slog.String("subject", subject),
		slog.Int("size", len(data)))

	// Try to unmarshal as a BaseMessage containing a Graphable payload
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Warn("Failed to unmarshal base message",
			slog.String("subject", subject),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Extract entity from BaseMessage payload
	entity, err := c.extractEntityFromMessage(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to extract entity from message",
			slog.String("subject", subject),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Store entity in KV bucket — MERGE semantics (gh#177). Earlier code
	// used CreateEntity (Put = full-replace) here, which clobbered any
	// pre-existing triples on the entity. The atomic mutation handlers
	// (create_with_triples, update_with_triples, triple.add) all merge;
	// the jetstream consumer path was the lone outlier and silently
	// erased lifecycle-managed entity state on every subsequent
	// Graphable arrival.
	if err := c.MergeEntity(ctx, entity); err != nil {
		c.logger.Error("Failed to merge entity",
			slog.String("entity_id", entity.ID),
			slog.Any("error", err))
		return
	}

	c.logger.Debug("Entity ingested",
		slog.String("entity_id", entity.ID),
		slog.Int("triples", len(entity.Triples)))
}

// extractEntityFromMessage extracts an EntityState from a BaseMessage
func (c *Component) extractEntityFromMessage(msg *message.BaseMessage) (*graph.EntityState, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}

	payload := msg.Payload()
	if payload == nil {
		return nil, fmt.Errorf("message has no payload")
	}

	// Check if payload implements Graphable
	graphable, ok := payload.(graph.Graphable)
	if !ok {
		return nil, fmt.Errorf("payload does not implement Graphable interface")
	}

	// Get entity ID and triples from Graphable
	entityID := graphable.EntityID()
	if entityID == "" {
		return nil, fmt.Errorf("graphable payload returned empty entity ID")
	}

	triples := graphable.Triples()

	// Build EntityState
	entity := &graph.EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: msg.Type(),
		Version:     1,
	}

	// ADR-054 channel (a): a Graphable payload MAY also declare its indexing
	// profile via the optional IndexingProfiler interface. Stamp it explicitly
	// here so it's present on entity.Triples by the time MergeEntity reaches
	// its create seam; absence (or an invalid value) falls through to the
	// fallback floor there.
	if profiler, ok := payload.(message.IndexingProfiler); ok {
		stampExplicitIndexingProfile(entity, profiler.IndexingProfile())
	}

	return entity, nil
}

// removeIndexingProfileTriples drops every entity.indexing.profile triple from
// the entity (the single-valued predicate is replace-on-write, never appended).
func removeIndexingProfileTriples(entity *graph.EntityState) {
	filtered := entity.Triples[:0]
	for _, t := range entity.Triples {
		if t.Predicate != vocabulary.EntityIndexingProfile {
			filtered = append(filtered, t)
		}
	}
	entity.Triples = filtered
}

// appendIndexingProfileTriple appends one entity.indexing.profile triple.
// Callers are responsible for having removed any prior value first.
func appendIndexingProfileTriple(entity *graph.EntityState, profile string) {
	entity.Triples = append(entity.Triples, message.Triple{
		Subject:    entity.ID,
		Predicate:  vocabulary.EntityIndexingProfile,
		Object:     profile,
		Source:     "graph-ingest-indexing-profile",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	})
}

// stampExplicitIndexingProfile sets entity.indexing.profile to an explicitly
// declared profile (replace-on-write, single-valued). Empty or unrecognized
// values are ignored — the entity then falls through to the fallback floor at
// its create seam rather than failing (lenient Phase 1 semantics). Used by the
// Graphable IndexingProfiler channel (extractEntityFromMessage) and the
// mutation-envelope channel (handleEntityCreateWithTriples).
func stampExplicitIndexingProfile(entity *graph.EntityState, profile string) {
	if entity == nil || !vocabulary.IsValidIndexingProfile(profile) {
		return
	}
	removeIndexingProfileTriples(entity)
	appendIndexingProfileTriple(entity, profile)
}

// reconcileIndexingProfile is the entity-CREATION-seam stamp (ADR-054 §5). It
// runs at every place an entity is born — createEntity, MergeEntity's
// first-write branch, and the stub→real upgrade in MergeEntity's merge branch —
// and never on a plain update of an already-profiled entity, so a profile is
// immutable once set. It enforces the single-valued invariant and applies the
// fallback floor when nothing was declared:
//
//   - ≥1 profile triple present (an explicit declaration was stamped upstream,
//     or a real producer is upgrading a profile-less stub): keep the FIRST and
//     drop any duplicates. No floor, no metric.
//   - 0 profile triples present: apply the registry floor
//     (indexingProfileFloorFor) and append it; increment
//     indexing_profile_default_total{message_type} ONLY when the type was not in
//     the registry (an unclassified gap, not a deliberate registered floor).
func (c *Component) reconcileIndexingProfile(entity *graph.EntityState) {
	if entity == nil {
		return
	}
	kept := false
	filtered := entity.Triples[:0]
	for _, t := range entity.Triples {
		if t.Predicate == vocabulary.EntityIndexingProfile {
			if kept {
				continue // single-valued: drop duplicates, keep the first
			}
			kept = true
		}
		filtered = append(filtered, t)
	}
	entity.Triples = filtered
	if kept {
		return
	}
	// No explicit declaration → apply the registry floor (ADR-054 channel c).
	// The default-fallback metric fires ONLY on a registry MISS — a type that is
	// neither producer-declared nor classified in the seed and silently took the
	// control default. A registered floor (e.g. agentic.request → trace) is a
	// deliberate classification, not an operator gap.
	profile, registered := indexingProfileFloorFor(entity.MessageType)
	appendIndexingProfileTriple(entity, profile)
	if !registered && c.indexingProfileDefault != nil {
		c.indexingProfileDefault.WithLabelValues(indexingProfileMetricLabel(entity.MessageType)).Inc()
	}
}

// indexingProfileMetricLabel renders a message.Type as a stable, low-cardinality
// metric label, or "unknown" for any incomplete Type. The IsValid guard (all
// three of Domain/Category/Version present) prevents a partial Type from
// producing a non-semantic label like ".widget.v1" or "test.widget." — a
// malformed producer surfaces as "unknown" rather than a junk label key.
func indexingProfileMetricLabel(mt message.Type) string {
	if !mt.IsValid() {
		return "unknown"
	}
	return mt.Key()
}

// ============================================================================
// Entity Operations
// ============================================================================

// validateEntityID validates that an entity ID follows the expected format
func validateEntityID(id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "validateEntityID", "entity ID cannot be empty")
	}

	if len(id) > 255 {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "validateEntityID", "entity ID too long (max 255 chars)")
	}

	if !entityIDRegex.MatchString(id) {
		parts := strings.Split(id, ".")
		msg := fmt.Sprintf(
			"invalid entity ID format: expected 6 ASCII alphanumeric parts (org.platform.domain.system.type.instance), got %d parts or non-ASCII characters",
			len(parts))
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "validateEntityID", msg)
	}

	return nil
}

// CreateEntity creates a new entity in the graph using upsert (Put)
// semantics. Existing callers that need last-writer-wins behavior
// (graph/datamanager edge ops) stay on this path. New atomic-create
// callers (the NATS mutation handlers' POST 409 path) use
// CreateEntityStrict, which fails fast with natsclient.ErrKVKeyExists
// when the ID is already present.
func (c *Component) CreateEntity(ctx context.Context, entity *graph.EntityState) error {
	return c.createEntity(ctx, entity, false)
}

// MergeEntity ingests a streaming-consumer EntityState (typically built
// by extractEntityFromMessage from a Graphable arriving on the
// JetStream input) WITHOUT clobbering pre-existing triples on the
// entity. First write behaves like CreateEntity (the entity didn't
// exist; its fields land verbatim); subsequent writes append the
// incoming triples to the existing slice and refresh latest-wins
// metadata (MessageType, StorageRef, UpdatedAt) while monotonically
// bumping Version.
//
// Closes gh#177: the prior code called CreateEntity (Put = full-
// replace) from handleMessage, which erased any triples written via
// the atomic mutation handlers (create_with_triples, update_with_
// triples, triple.add) — all of which merge. The jetstream consumer
// path was the lone outlier. Lifecycle-managed entities surfaced this
// most loudly: Manager.Create stamped the phase triple, then the
// first Graphable arrival via a downstream processor wiped it.
//
// Uses entityBucket.UpdateWithRetry for atomic CAS read-modify-write,
// so concurrent arrivals on the same Subject converge without racing.
func (c *Component) MergeEntity(ctx context.Context, entity *graph.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "MergeEntity", "entity cannot be nil")
	}
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "MergeEntity", "context cancelled")
	}

	// Hierarchy inference is deterministic per entityID — calling it
	// on every merge would APPEND the same hierarchy triples on each
	// arrival (50 mission-command Graphables → 50 copies of
	// type.container / system.container / etc. on the same entity).
	// Gate to first-write only, applied inside the CAS callback so the
	// fetch happens once per genuine create. See go-reviewer concern 5
	// on the gh#177 fix.
	var hierarchyTriples []message.Triple
	if c.config.EnableHierarchy && c.hierarchyInference != nil {
		// Probe-then-fetch: cheap pre-check avoids the inference cost
		// on guaranteed-second-write paths. Even if the entity is
		// concurrently created between probe and CAS, the callback's
		// len(current) == 0 branch is the gate that actually applies
		// the hierarchy triples — the probe is an optimization, not
		// correctness.
		if _, err := c.entityBucket.Get(ctx, entity.ID); err != nil && natsclient.IsKVNotFoundError(err) {
			triples, herr := c.hierarchyInference.GetHierarchyTriples(ctx, entity.ID)
			if herr != nil {
				c.logger.Warn("Failed to get hierarchy triples",
					slog.String("entity_id", entity.ID),
					slog.Any("error", herr))
			} else {
				hierarchyTriples = triples
			}
		}
	}

	var bytesWritten int
	err := c.entityBucket.UpdateWithRetry(ctx, entity.ID, func(current []byte) ([]byte, error) {
		// First write: entity didn't exist. Apply hierarchy triples
		// (deterministic-per-ID so safe to apply once on create),
		// then store verbatim.
		if len(current) == 0 {
			if len(hierarchyTriples) > 0 {
				entity.Triples = append(entity.Triples, hierarchyTriples...)
			}
			// ADR-054: first write is entity birth — stamp the profile
			// (explicit-if-declared via IndexingProfiler, else floor).
			c.reconcileIndexingProfile(entity)
			data, err := json.Marshal(entity)
			if err == nil {
				bytesWritten = len(data)
			}
			return data, err
		}
		// Existing entity: merge triples + refresh latest-wins metadata.
		// Hierarchy triples are NOT re-applied — they landed on the
		// original create and would only produce duplicates here.
		var existing graph.EntityState
		if err := json.Unmarshal(current, &existing); err != nil {
			return nil, err // non-retryable
		}
		existing.Triples = append(existing.Triples, entity.Triples...)
		existing.MessageType = entity.MessageType
		if entity.StorageRef != nil {
			existing.StorageRef = entity.StorageRef
		}
		// ADR-054: a real producer merging into a profile-less referential-
		// integrity stub is that entity's true birth — reconcile stamps the
		// profile (kept from the incoming declaration, else floor). For an
		// already-profiled entity this is a no-op (keep-first preserves the
		// create-time value), so a re-arrival never re-profiles.
		c.reconcileIndexingProfile(&existing)
		existing.Version++
		existing.UpdatedAt = time.Now()
		data, err := json.Marshal(&existing)
		if err == nil {
			bytesWritten = len(data)
		}
		return data, err
	})
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "MergeEntity", "CAS update")
	}

	// Cache invalidation matches createEntity — readers must see the
	// merged state on next Get.
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	c.updateSuffixIndex(ctx, entity.ID)

	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(bytesWritten))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity merged",
		slog.String("entity_id", entity.ID),
		slog.Int("triples_in", len(entity.Triples)))

	c.ensureRelationshipTargetsExist(ctx, entity)

	return nil
}

// CreateEntityStrict creates a new entity atomically — if the ID is
// already present, returns natsclient.ErrKVKeyExists without
// overwriting. Closes the concurrent-create TOCTOU window the
// graph.mutation.entity.create handler used to have when it relied
// on exists-check + Put.
func (c *Component) CreateEntityStrict(ctx context.Context, entity *graph.EntityState) error {
	return c.createEntity(ctx, entity, true)
}

// createEntity is the shared body for CreateEntity / CreateEntityStrict.
// atomicCreate=true switches the KV write from Put (upsert) to
// Create (atomic key-create); everything else (hierarchy inference,
// referential integrity stubs, cache invalidation, metrics) is
// identical.
func (c *Component) createEntity(ctx context.Context, entity *graph.EntityState, atomicCreate bool) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "CreateEntity", "entity cannot be nil")
	}

	// Validate entity ID format
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "CreateEntity", "context cancelled")
	}

	// SYNCHRONOUS HIERARCHY INFERENCE:
	// Get hierarchy triples BEFORE writing entity to storage
	// This ensures entity is written once with all triples included (no cascade)
	if c.config.EnableHierarchy && c.hierarchyInference != nil {
		hierarchyTriples, err := c.hierarchyInference.GetHierarchyTriples(ctx, entity.ID)
		if err != nil {
			c.logger.Warn("Failed to get hierarchy triples",
				slog.String("entity_id", entity.ID),
				slog.Any("error", err))
			// Don't fail entity creation if hierarchy fails - just log warning
		} else if len(hierarchyTriples) > 0 {
			// Add hierarchy triples to entity before writing
			entity.Triples = append(entity.Triples, hierarchyTriples...)
		}
	}

	// ADR-054: stamp the indexing profile at the creation seam — keeps an
	// explicit declaration (envelope/Graphable, already on entity.Triples) and
	// otherwise applies the fallback floor + default metric.
	c.reconcileIndexingProfile(entity)

	// Serialize entity (now includes hierarchy triples if enabled)
	data, err := json.Marshal(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "CreateEntity", "entity serialization")
	}

	// Store in KV bucket. Put is upsert (last-writer-wins); Create is
	// atomic create-or-fail (returns natsclient.ErrKVKeyExists on
	// conflict). The ErrKVKeyExists sentinel is bubbled verbatim so
	// handlers can branch with errors.Is.
	var writeErr error
	if atomicCreate {
		_, writeErr = c.entityBucket.Create(ctx, entity.ID, data)
		if writeErr != nil && errors.Is(writeErr, natsclient.ErrKVKeyExists) {
			// Expected conflict shape — don't count as a component
			// error and don't wrap (preserves sentinel identity).
			return writeErr
		}
	} else {
		_, writeErr = c.entityBucket.Put(ctx, entity.ID, data)
	}
	if writeErr != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(writeErr, "Component", "CreateEntity", "KV store")
	}

	// Invalidate cache on write (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	// Update suffix index (best-effort, don't fail entity creation)
	c.updateSuffixIndex(ctx, entity.ID)

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity created",
		slog.String("entity_id", entity.ID),
		slog.Int("triples", len(entity.Triples)))

	c.ensureRelationshipTargetsExist(ctx, entity)

	return nil
}

// ensureRelationshipTargetsExist walks the entity's relationship triples
// and, for each referenced entity that doesn't yet exist, creates a
// stub. Bounded to 5 concurrent KV ops. Errors are best-effort —
// referential integrity is a fallback, not a hard contract — so a
// failure is logged but does not propagate.
//
// Extracted from createEntity to keep that function under the
// revive.toml function-length cap (50 statements).
func (c *Component) ensureRelationshipTargetsExist(ctx context.Context, entity *graph.EntityState) {
	// Deduplicate target IDs — multiple triples may reference the same entity.
	uniqueTargets := make(map[string]struct{})
	for _, triple := range entity.Triples {
		if triple.IsRelationship() {
			targetID, ok := triple.Object.(string)
			if ok && targetID != "" && targetID != entity.ID {
				uniqueTargets[targetID] = struct{}{}
			}
		}
	}

	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for targetID := range uniqueTargets {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			// Acquire semaphore with context cancellation support.
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			if ctx.Err() != nil {
				return
			}

			if err := c.ensureReferencedEntityExists(ctx, id, entity.ID); err != nil {
				c.logger.Debug("failed to ensure referenced entity exists",
					slog.String("target", id),
					slog.String("referenced_by", entity.ID),
					slog.Any("error", err))
			}
		}(targetID)
	}

	wg.Wait()
}

// ensureReferencedEntityExists creates a stub entity if the referenced entity doesn't exist.
// This is a fallback mechanism to guarantee referential integrity - if an entity references
// another entity by ID, that entity must exist in the graph.
func (c *Component) ensureReferencedEntityExists(ctx context.Context, entityID, referencedBy string) error {
	// Check if entity already exists
	_, err := c.entityBucket.Get(ctx, entityID)
	if err == nil {
		return nil // Entity exists, nothing to do
	}

	// Entity doesn't exist - create a stub.
	// Version is set to 1 to match the invariant held by every other
	// EntityState write path (see component.go:872, messagemanager/
	// processor.go:276, datamanager/manager.go:792). When the real entity
	// later arrives, the merge path increments Version to 2.
	now := time.Now()
	stub := &graph.EntityState{
		ID:        entityID,
		Version:   1,
		UpdatedAt: now,
		Triples: []message.Triple{
			{
				Subject:    entityID,
				Predicate:  "core.identity.stub",
				Object:     true,
				Source:     "graph-ingest-referential-integrity",
				Timestamp:  now,
				Confidence: 1.0,
			},
			{
				Subject:    entityID,
				Predicate:  "core.identity.referenced_by",
				Object:     referencedBy,
				Source:     "graph-ingest-referential-integrity",
				Timestamp:  now,
				Confidence: 1.0,
			},
		},
	}

	data, err := json.Marshal(stub)
	if err != nil {
		return fmt.Errorf("marshal stub entity: %w", err)
	}

	if _, err := c.entityBucket.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("store stub entity: %w", err)
	}

	c.logger.Debug("created stub entity for referential integrity",
		slog.String("entity_id", entityID),
		slog.String("referenced_by", referencedBy))

	return nil
}

// UpdateEntity updates an existing entity
func (c *Component) UpdateEntity(ctx context.Context, entity *graph.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateEntity", "entity cannot be nil")
	}

	// Validate entity ID format
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateEntity", "context cancelled")
	}

	// Serialize entity
	data, err := json.Marshal(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateEntity", "entity serialization")
	}

	// Update in KV bucket
	if _, err := c.entityBucket.Put(ctx, entity.ID, data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateEntity", "KV store")
	}

	// Invalidate cache on write (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity updated",
		slog.String("entity_id", entity.ID),
		slog.Uint64("version", entity.Version))

	return nil
}

// updateEntityAtRevision is the CAS-protected variant of UpdateEntity.
// It only commits if the entity's KV revision still matches expectedRev,
// otherwise returns natsclient.ErrKVRevisionMismatch. This closes the
// resurrect-after-delete window the plain UpdateEntity → Put path has:
// a concurrent DeleteEntity that lands between the caller's read and
// this write would otherwise turn an update into a silent re-create.
//
// Used by the mutation handlers' must-exist contract
// (graph.mutation.entity.update / .update_with_triples). UpdateEntity
// itself stays Put-based for callers (e.g. datamanager edge ops) that
// want last-writer-wins.
func (c *Component) updateEntityAtRevision(ctx context.Context, entity *graph.EntityState, expectedRev uint64) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateEntityAtRevision", "entity cannot be nil")
	}

	if err := validateEntityID(entity.ID); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "updateEntityAtRevision", "context cancelled")
	}

	data, err := json.Marshal(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "updateEntityAtRevision", "entity serialization")
	}

	if _, err := c.entityBucket.Update(ctx, entity.ID, data, expectedRev); err != nil {
		// ErrKVRevisionMismatch and other KV errors propagate verbatim so
		// callers can branch with errors.Is(..., natsclient.ErrKVRevisionMismatch).
		atomic.AddInt64(&c.errors, 1)
		return err
	}

	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity updated (CAS)",
		slog.String("entity_id", entity.ID),
		slog.Uint64("expected_revision", expectedRev),
		slog.Uint64("version", entity.Version))

	return nil
}

// DeleteEntity removes an entity from the graph
func (c *Component) DeleteEntity(ctx context.Context, entityID string) error {
	// Validate entity ID format
	if err := validateEntityID(entityID); err != nil {
		return err
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteEntity", "context cancelled")
	}

	// Delete from KV bucket
	if err := c.entityBucket.Delete(ctx, entityID); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "DeleteEntity", "KV delete")
	}

	// Invalidate cache on delete (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entityID) //nolint:errcheck
	}

	// Remove suffix index entries (best-effort)
	c.removeSuffixIndex(ctx, entityID)

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	c.logger.Debug("entity deleted", slog.String("entity_id", entityID))

	return nil
}

// ============================================================================
// Triple Operations
// ============================================================================

// AddTriple adds a triple to an entity using CAS for concurrency safety
func (c *Component) AddTriple(ctx context.Context, triple message.Triple) error {
	if triple.Subject == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "AddTriple", "triple subject cannot be empty")
	}
	if triple.Predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "AddTriple", "triple predicate cannot be empty")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "AddTriple", "context cancelled")
	}

	// Use UpdateWithRetry for atomic read-modify-write with CAS
	err := c.entityBucket.UpdateWithRetry(ctx, triple.Subject, func(current []byte) ([]byte, error) {
		var entity graph.EntityState

		if len(current) > 0 {
			// Deserialize existing entity
			if err := json.Unmarshal(current, &entity); err != nil {
				return nil, err // Non-retryable
			}
		} else {
			// Create new entity if doesn't exist
			entity = graph.EntityState{
				ID:        triple.Subject,
				Version:   0,
				UpdatedAt: time.Now(),
			}
		}

		// Add triple
		entity.Triples = append(entity.Triples, triple)
		entity.Version++
		entity.UpdatedAt = time.Now()

		return json.Marshal(&entity)
	})

	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "AddTriple", "CAS update")
	}

	return nil
}

// AddTriples adds many triples in one call, batching per entity. Triples
// sharing the same Subject collapse to a single CAS read-modify-write on
// that entity's KV record — N triples on the same loop entity become
// 1 round-trip to graph-ingest, not N. Triples spanning multiple
// entities issue one CAS per entity in deterministic subject order.
//
// Atomicity scope: per-entity, not cross-entity. If two entities are
// in the batch and the first commits but the second exhausts CAS
// retries, the first stays committed. This trade-off is acceptable
// for the primary use case (write_todos, ADR-036) where every triple
// shares one Subject (the loop entity). Multi-subject callers that
// need cross-entity atomicity should use UpdateEntityWithTriples
// instead.
//
// Returns nil on full success. On partial failure returns an error
// whose message names the failing subjects; the per-subject error
// detail surfaces via the handler's FailedSubjects response field.
func (c *Component) AddTriples(ctx context.Context, triples []message.Triple) (writtenCount int, failedSubjects map[string]string, err error) {
	if len(triples) == 0 {
		return 0, nil, nil
	}

	// Validate before any write. Reject the whole batch on the first
	// malformed triple — partial validation would be surprising.
	for i, t := range triples {
		if t.Subject == "" {
			return 0, nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "AddTriples", fmt.Sprintf("triple[%d] subject cannot be empty", i))
		}
		if t.Predicate == "" {
			return 0, nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "AddTriples", fmt.Sprintf("triple[%d] predicate cannot be empty", i))
		}
	}

	if err := ctx.Err(); err != nil {
		return 0, nil, errs.Wrap(err, "Component", "AddTriples", "context cancelled")
	}

	// Group triples by Subject so each entity sees a single CAS.
	// Map iteration is non-deterministic; we sort subject keys before
	// committing so retries replay in stable order (helpful for tests
	// and for any operator reading a partial-failure trace).
	bySubject := make(map[string][]message.Triple, len(triples))
	for _, t := range triples {
		bySubject[t.Subject] = append(bySubject[t.Subject], t)
	}
	subjects := make([]string, 0, len(bySubject))
	for s := range bySubject {
		subjects = append(subjects, s)
	}
	sort.Strings(subjects)

	failedSubjects = make(map[string]string)
	for i, subject := range subjects {
		// Short-circuit on context cancellation: don't burn the retry
		// budget for every remaining subject when the caller has
		// already given up. Mark the rest as failed-due-to-cancel so
		// the response shape is honest about what didn't commit, but
		// only count one operational error event for the cancellation
		// rather than N (one per remaining subject).
		if ctxErr := ctx.Err(); ctxErr != nil {
			for _, s := range subjects[i:] {
				failedSubjects[s] = ctxErr.Error()
			}
			atomic.AddInt64(&c.errors, 1)
			break
		}
		group := bySubject[subject]
		casErr := c.entityBucket.UpdateWithRetry(ctx, subject, func(current []byte) ([]byte, error) {
			var entity graph.EntityState

			if len(current) > 0 {
				if err := json.Unmarshal(current, &entity); err != nil {
					return nil, err // Non-retryable
				}
			} else {
				entity = graph.EntityState{
					ID:        subject,
					Version:   0,
					UpdatedAt: time.Now(),
				}
			}

			entity.Triples = append(entity.Triples, group...)
			entity.Version++
			entity.UpdatedAt = time.Now()

			return json.Marshal(&entity)
		})

		if casErr != nil {
			atomic.AddInt64(&c.errors, 1)
			failedSubjects[subject] = casErr.Error()
			continue
		}
		writtenCount += len(group)
	}

	if len(failedSubjects) == 0 {
		return writtenCount, nil, nil
	}

	// Sort failed-subject names for stable error messages.
	failed := make([]string, 0, len(failedSubjects))
	for s := range failedSubjects {
		failed = append(failed, s)
	}
	sort.Strings(failed)
	return writtenCount, failedSubjects, errs.Wrap(
		fmt.Errorf("CAS update failed for %d/%d subjects: %v", len(failedSubjects), len(subjects), failed),
		"Component", "AddTriples", "batch CAS partial failure")
}

// RemoveTriple removes a triple from an entity using CAS for concurrency safety
func (c *Component) RemoveTriple(ctx context.Context, subject, predicate string) error {
	if subject == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "RemoveTriple", "subject cannot be empty")
	}
	if predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "RemoveTriple", "predicate cannot be empty")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "RemoveTriple", "context cancelled")
	}

	// Check if entity exists first - if not, nothing to remove
	_, err := c.entityBucket.Get(ctx, subject)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return nil // Entity doesn't exist, nothing to remove
		}
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "RemoveTriple", "entity lookup")
	}

	// Use UpdateWithRetry for atomic read-modify-write with CAS
	err = c.entityBucket.UpdateWithRetry(ctx, subject, func(current []byte) ([]byte, error) {
		if len(current) == 0 {
			// Entity was deleted between our check and update - nothing to do
			return nil, natsclient.ErrKVKeyNotFound
		}

		// Deserialize existing entity
		var entity graph.EntityState
		if err := json.Unmarshal(current, &entity); err != nil {
			return nil, err // Non-retryable
		}

		// Remove matching triples
		filtered := make([]message.Triple, 0, len(entity.Triples))
		for _, t := range entity.Triples {
			if t.Predicate != predicate {
				filtered = append(filtered, t)
			}
		}

		// If nothing changed, return input unchanged to avoid unnecessary write
		if len(filtered) == len(entity.Triples) {
			return current, nil
		}

		entity.Triples = filtered
		entity.Version++
		entity.UpdatedAt = time.Now()

		return json.Marshal(&entity)
	})

	// Handle errors - ErrKVKeyNotFound means entity was deleted, which is fine
	if err != nil {
		// Check if it's a wrapped "not found" error
		if natsclient.IsKVNotFoundError(err) {
			return nil // Entity was deleted, nothing to remove
		}
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "RemoveTriple", "CAS update")
	}

	return nil
}

// ============================================================================
// Suffix Index Operations
// ============================================================================

// entitySuffixKeys returns the suffix index keys for a given entity ID.
// Entity ID format: org.platform.domain.system.type.instance
// Returns two keys: instance part and type.instance part.
func entitySuffixKeys(entityID string) (instance, typeInstance string) {
	parts := strings.Split(entityID, ".")
	if len(parts) < 2 {
		return entityID, ""
	}
	instance = parts[len(parts)-1]
	typeInstance = parts[len(parts)-2] + "." + parts[len(parts)-1]
	return instance, typeInstance
}

// updateSuffixIndex writes suffix→fullID mappings to the KV suffix index.
// Best-effort: errors are logged but don't fail the caller.
func (c *Component) updateSuffixIndex(ctx context.Context, entityID string) {
	if c.suffixBucket == nil {
		return
	}

	instance, typeInstance := entitySuffixKeys(entityID)
	indexValue := []byte(`{"id":"` + entityID + `"}`)

	if instance != "" {
		if _, err := c.suffixBucket.Put(ctx, instance, indexValue); err != nil {
			c.logger.Debug("suffix index write failed",
				slog.String("key", instance), slog.Any("error", err))
		}
	}
	if typeInstance != "" {
		if _, err := c.suffixBucket.Put(ctx, typeInstance, indexValue); err != nil {
			c.logger.Debug("suffix index write failed",
				slog.String("key", typeInstance), slog.Any("error", err))
		}
	}
}

// removeSuffixIndex removes suffix→fullID mappings from the KV suffix index and cache.
// Best-effort: errors are logged but don't fail the caller.
func (c *Component) removeSuffixIndex(ctx context.Context, entityID string) {
	if c.suffixBucket == nil {
		return
	}

	instance, typeInstance := entitySuffixKeys(entityID)

	if instance != "" {
		_ = c.suffixBucket.Delete(ctx, instance)
		if c.suffixCache != nil {
			c.suffixCache.Delete(instance) //nolint:errcheck
		}
	}
	if typeInstance != "" {
		_ = c.suffixBucket.Delete(ctx, typeInstance)
		if c.suffixCache != nil {
			c.suffixCache.Delete(typeInstance) //nolint:errcheck
		}
	}
}
