// Package graphquery implements the query coordinator component for the graph subsystem.
// It orchestrates queries across graph-ingest and graph-index components and provides
// PathRAG traversal capabilities.
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/nats-io/nats.go/jetstream"
)

// Note: jetstream import is used for KV types (jetstream.KeyValue, jetstream.KeyWatcher).
// This is the standard pattern across all processor components.
// natsclient wraps NATS operations but doesn't abstract jetstream types.

// natsRequester is a local interface for NATS request/reply and KV operations.
// *natsclient.Client satisfies this interface, and tests can provide mocks.
type natsRequester interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
	SubscribeForRequests(ctx context.Context, subject string, handler func(ctx context.Context, data []byte) ([]byte, error)) (*natsclient.Subscription, error)
	Status() natsclient.ConnectionStatus
	Connect(ctx context.Context) error
	WaitForConnection(ctx context.Context) error
	JetStream() (jetstream.JetStream, error)
	GetKeyValueBucket(ctx context.Context, name string) (jetstream.KeyValue, error)
}

// Config defines the configuration for the graph-query coordinator component
type Config struct {
	Ports                *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
	QueryTimeout         time.Duration         `json:"query_timeout,omitempty" schema:"type:duration,description:Timeout for query operations,default:5s,category:basic"`
	MaxDepth             int                   `json:"max_depth,omitempty" schema:"type:int,description:Maximum traversal depth for path search,default:10,min:1,max:100,category:basic"`
	StartupAttempts      int                   `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts for startup dependency checks,default:10,min:1,category:advanced"`
	StartupInterval      time.Duration         `json:"startup_interval,omitempty" schema:"type:duration,description:Interval between startup attempts,default:500ms,category:advanced"`
	RecheckInterval      time.Duration         `json:"recheck_interval,omitempty" schema:"type:duration,description:Interval for rechecking missing buckets,default:5s,category:advanced"`
	MinSemanticRelevance float64               `json:"min_semantic_relevance,omitempty" schema:"type:float,description:Minimum neural embedding similarity score (0.0-1.0),default:0.5,min:0,max:1,category:advanced"`
	MinBM25Relevance     float64               `json:"min_bm25_relevance,omitempty" schema:"type:float,description:Minimum BM25 embedding similarity score (0.0-1.0),default:0.4,min:0,max:1,category:advanced"`
	MinTextRelevance     float64               `json:"min_text_relevance,omitempty" schema:"type:float,description:Minimum text match score (0.0-1.0),default:0.3,min:0,max:1,category:advanced"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Ports == nil || len(c.Ports.Inputs) == 0 {
		return errors.New("ports configuration with at least one input port is required")
	}
	return nil
}

// ApplyDefaults applies default values to the configuration
func (c *Config) ApplyDefaults() {
	if c.QueryTimeout == 0 {
		c.QueryTimeout = 5 * time.Second
	}
	if c.MaxDepth == 0 {
		c.MaxDepth = 10
	}
	// Resource startup defaults match resource.DefaultConfig()
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 10
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 * time.Millisecond
	}
	// Use shorter recheck interval than resource.DefaultConfig (60s) for faster recovery
	if c.RecheckInterval == 0 {
		c.RecheckInterval = 5 * time.Second
	}
}

// DefaultConfig returns a default configuration for the graph-query coordinator
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "query_entity", Type: "nats-request", Subject: "graph.query.entity"},
				{Name: "query_relationships", Type: "nats-request", Subject: "graph.query.relationships"},
				{Name: "query_path_search", Type: "nats-request", Subject: "graph.query.pathSearch"},
			},
			Outputs: []component.PortDefinition{},
		},
		QueryTimeout: 5 * time.Second,
		MaxDepth:     10,
	}
}

// Component implements the graph query coordinator
type Component struct {
	config        Config
	natsClient    natsRequester
	pathSearcher  *PathSearcher
	router        *StaticRouter
	logger        *slog.Logger
	modelRegistry model.RegistryReader
	llmClient     llm.Client
	classifier    *query.ClassifierChain

	// Answer synthesis for globalSearch
	answerSynthesizer AnswerSynthesizer

	// Resolved relevance thresholds (from config or defaults)
	minSemanticRelevance float64
	minBM25Relevance     float64
	minTextRelevance     float64

	// Community cache for GraphRAG (consumer-owned, KV watch based)
	communityCache   *CommunityCache
	communityWatcher *resource.Watcher

	// Lifecycle state
	mu          sync.RWMutex
	wg          sync.WaitGroup
	initialized bool
	started     bool
	cancel      context.CancelFunc

	// Health tracking
	healthMu   sync.RWMutex
	errorCount int
	lastError  error

	// Metrics tracking
	metricsMu         sync.RWMutex
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastMetricsReset  time.Time

	// Prometheus metrics for observability
	promMetrics *queryMetrics

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// CreateGraphQuery creates a new graph query coordinator component
func CreateGraphQuery(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Parse configuration
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	config.ApplyDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Validate dependencies - deps.NATSClient is typed as *natsclient.Client
	// which satisfies our natsRequester interface
	if deps.NATSClient == nil {
		return nil, errors.New("NATSClient dependency is required")
	}

	logger := deps.GetLoggerWithComponent("graph-query")

	// Create component with keyword-only classifier; LLM classifier wired in Start()
	comp := &Component{
		config:               config,
		natsClient:           deps.NATSClient, // Assign to interface field
		pathSearcher:         NewPathSearcher(deps.NATSClient, config.QueryTimeout, config.MaxDepth, logger),
		logger:               logger,
		modelRegistry:        deps.ModelRegistry,
		classifier:           query.NewClassifierChain(query.NewKeywordClassifier(), nil, nil),
		lastMetricsReset:     time.Now(),
		promMetrics:          getMetrics(deps.MetricsRegistry),
		minSemanticRelevance: MinSemanticRelevance,
		minBM25Relevance:     MinBM25Relevance,
		minTextRelevance:     MinTextRelevance,
	}

	// Apply config overrides for relevance thresholds
	if config.MinSemanticRelevance > 0 {
		comp.minSemanticRelevance = config.MinSemanticRelevance
	}
	if config.MinBM25Relevance > 0 {
		comp.minBM25Relevance = config.MinBM25Relevance
	}
	if config.MinTextRelevance > 0 {
		comp.minTextRelevance = config.MinTextRelevance
	}

	return comp, nil
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Type:        "processor",
		Name:        "graph-query",
		Description: "Query coordinator for graph subsystem - orchestrates queries across graph-ingest and graph-index",
		Version:     "1.0.0",
	}
}

// InputPorts returns the component's input ports
func (c *Component) InputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}

	// Build ports from configuration
	ports := make([]component.Port, 0, len(c.config.Ports.Inputs))
	for _, portDef := range c.config.Ports.Inputs {
		port := component.BuildPortFromDefinition(portDef, component.DirectionInput)
		ports = append(ports, port)
	}

	return ports
}

// OutputPorts returns the component's output ports (none for query coordinator)
func (c *Component) OutputPorts() []component.Port {
	// Query coordinator has no output ports - it returns data via request/reply
	return []component.Port{}
}

// ConfigSchema returns the JSON schema for the component's configuration
func (c *Component) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"ports": {
				Type:        "object",
				Description: "Port configuration for input and output connections",
			},
			"query_timeout": {
				Type:        "string",
				Description: "Timeout for query operations (e.g., '5s', '10s')",
			},
			"max_depth": {
				Type:        "integer",
				Description: "Maximum traversal depth for path search queries",
			},
		},
		Required: []string{"ports"},
	}
}

// Health returns the component's health status
func (c *Component) Health() component.HealthStatus {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()

	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	healthy := started && c.natsClient.Status() == natsclient.StatusConnected

	var lastErrorStr string
	if c.lastError != nil {
		lastErrorStr = c.lastError.Error()
	}

	return component.HealthStatus{
		Healthy:    healthy,
		ErrorCount: c.errorCount,
		LastError:  lastErrorStr,
		Status:     c.getHealthMessage(healthy),
	}
}

func (c *Component) getHealthMessage(healthy bool) string {
	if !healthy {
		if c.lastError != nil {
			return c.lastError.Error()
		}
		return "not started or NATS disconnected"
	}
	return "ok"
}

// DataFlow returns the component's data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	elapsed := time.Since(c.lastMetricsReset).Seconds()
	if elapsed == 0 {
		elapsed = 1
	}

	return component.FlowMetrics{
		MessagesPerSecond: float64(c.messagesProcessed) / elapsed,
		BytesPerSecond:    float64(c.bytesProcessed) / elapsed,
		ErrorRate:         float64(c.errors) / elapsed,
	}
}

// Initialize initializes the component
func (c *Component) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	// Validate configuration
	if err := c.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	c.initialized = true
	c.logger.Info("graph-query coordinator initialized")
	return nil
}

// initLifecycleReporter initializes the lifecycle reporter for status tracking
func (c *Component) initLifecycleReporter(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Failed to get JetStream, lifecycle reporting disabled", slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}

	statusBucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
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
		ComponentName:    "graph-query",
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// initLLMClassifier wires the LLM query classifier if the model registry
// has a query_classification capability configured. Gracefully degrades to
// keyword-only classification on any error.
func (c *Component) initLLMClassifier() {
	if c.modelRegistry == nil {
		return
	}
	resolved, err := model.ResolveEndpoint(c.modelRegistry, model.CapabilityQueryClassification)
	if err != nil {
		// No query_classification capability configured — keyword-only is fine
		return
	}
	client, err := llm.NewOpenAIClient(llm.OpenAIConfig{
		BaseURL: resolved.URL,
		Model:   resolved.Model,
		APIKey:  resolved.APIKey,
		Logger:  c.logger,
	})
	if err != nil {
		c.logger.Warn("failed to create LLM query classifier, using keyword-only",
			slog.Any("error", err))
		return
	}
	c.llmClient = client
	adapter := query.NewLLMClientAdapter(client)
	llmClassifier := query.NewLLMClassifier(adapter, nil)
	c.classifier = query.NewClassifierChain(query.NewKeywordClassifier(), nil, llmClassifier)
	c.logger.Info("LLM query classifier enabled",
		slog.String("model", resolved.Model))
}

// initAnswerSynthesizer wires the LLM answer synthesizer if the model registry
// has an answer_synthesis capability configured. Falls back to template-based
// synthesis (no LLM) when unconfigured.
func (c *Component) initAnswerSynthesizer() {
	// Template fallback is always the default
	c.answerSynthesizer = &TemplateAnswerSynthesizer{}

	if c.modelRegistry == nil {
		return
	}
	resolved, err := model.ResolveEndpoint(c.modelRegistry, model.CapabilityAnswerSynthesis)
	if err != nil {
		c.logger.Debug("no answer_synthesis endpoint configured, using template fallback")
		return
	}
	client, err := llm.NewOpenAIClient(llm.OpenAIConfig{
		BaseURL: resolved.URL,
		Model:   resolved.Model,
		APIKey:  resolved.APIKey,
		Logger:  c.logger,
	})
	if err != nil {
		c.logger.Warn("failed to create answer synthesis LLM client, using template fallback",
			slog.Any("error", err))
		return
	}
	c.answerSynthesizer = NewLLMAnswerSynthesizer(client, resolved.Model, c.logger)
	c.logger.Info("LLM answer synthesis enabled",
		slog.String("model", resolved.Model))
}

// Start starts the component
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "graph-query", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "graph-query", "Start", "context already cancelled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return errors.New("component not initialized")
	}

	if c.started {
		return nil // Already started - idempotent
	}

	// Create component context for lifecycle management
	componentCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Wait for NATS connection
	if err := c.natsClient.WaitForConnection(componentCtx); err != nil {
		return fmt.Errorf("wait for NATS connection: %w", err)
	}

	// Wire LLM classifier if model registry has query_classification capability
	c.initLLMClassifier()

	// Wire answer synthesizer (LLM if available, template fallback otherwise)
	c.initAnswerSynthesizer()

	// Initialize lifecycle reporter (throttled for high-throughput queries)
	c.initLifecycleReporter(componentCtx)

	// Create router for static routing
	c.router = NewStaticRouter(c.logger)

	// Subscribe to query subjects
	if err := c.setupQueryHandlers(componentCtx); err != nil {
		return fmt.Errorf("subscribe to queries: %w", err)
	}

	// Initialize community cache for GraphRAG
	c.communityCache = NewCommunityCache(c.logger)

	// Set up resource watcher for COMMUNITY_INDEX bucket
	// This handles graceful startup and recovery if the bucket appears later
	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = c.config.StartupInterval
	watcherCfg.RecheckInterval = c.config.RecheckInterval
	watcherCfg.Logger = c.logger
	watcherCfg.OnAvailable = func() {
		c.enableGraphRAG(componentCtx)
		// Report recovery from degraded state
		if err := c.lifecycleReporter.ReportStage(componentCtx, "idle"); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
		}
	}
	watcherCfg.OnLost = func() {
		c.disableGraphRAG()
		// Report degraded state due to missing dependency
		if err := c.lifecycleReporter.ReportStage(componentCtx, "degraded_missing_"+graph.BucketCommunityIndex); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "degraded_missing_"+graph.BucketCommunityIndex), slog.Any("error", err))
		}
	}

	c.communityWatcher = resource.NewWatcher(
		"COMMUNITY_INDEX",
		func(ctx context.Context) error {
			_, err := c.natsClient.GetKeyValueBucket(ctx, graph.BucketCommunityIndex)
			return err
		},
		watcherCfg,
	)

	// Report waiting stage before dependency check (COMMUNITY_INDEX is optional but we report waiting)
	if err := c.lifecycleReporter.ReportStage(componentCtx, "waiting_for_"+graph.BucketCommunityIndex); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "waiting_for_"+graph.BucketCommunityIndex), slog.Any("error", err))
	}

	// Try to get bucket during startup
	if c.communityWatcher.WaitForStartup(componentCtx) {
		// Bucket available - enable GraphRAG immediately
		if err := c.startGraphRAGWatcher(componentCtx); err != nil {
			return fmt.Errorf("start GraphRAG watcher: %w", err)
		}
	} else {
		// Bucket not available - start background checking
		c.logger.Info("COMMUNITY_INDEX bucket not available at startup, GraphRAG disabled (will retry)")
		c.communityWatcher.StartBackgroundCheck(componentCtx)
		// Report degraded state due to missing optional dependency
		if err := c.lifecycleReporter.ReportStage(componentCtx, "degraded_missing_"+graph.BucketCommunityIndex); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "degraded_missing_"+graph.BucketCommunityIndex), slog.Any("error", err))
		}
	}

	c.started = true

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(componentCtx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("graph-query coordinator started")
	return nil
}

// Stop stops the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil // Not started - safe to stop
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

	// Close LLM client if present
	if c.llmClient != nil {
		if err := c.llmClient.Close(); err != nil {
			c.logger.Warn("LLM client close error", slog.Any("error", err))
		}
	}

	// Close answer synthesizer (releases its LLM client if LLM-backed)
	if c.answerSynthesizer != nil {
		if err := c.answerSynthesizer.Close(); err != nil {
			c.logger.Warn("answer synthesizer close error", slog.Any("error", err))
		}
	}

	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	// Wait for background goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(timeout):
		c.logger.Warn("stop timeout waiting for goroutines")
	}

	// Stop community watcher (background check goroutine)
	if c.communityWatcher != nil {
		c.communityWatcher.Stop()
	}

	// Stop community cache watcher
	if c.communityCache != nil {
		c.communityCache.Stop()
	}

	c.mu.Lock()
	c.started = false
	c.mu.Unlock()

	c.logger.Info("graph-query coordinator stopped")
	return nil
}

// startGraphRAGWatcher initializes and starts the community cache KV watcher.
// Called when COMMUNITY_INDEX bucket is available at startup.
func (c *Component) startGraphRAGWatcher(ctx context.Context) error {
	communityBucket, err := c.natsClient.GetKeyValueBucket(ctx, graph.BucketCommunityIndex)
	if err != nil {
		return fmt.Errorf("get COMMUNITY_INDEX bucket: %w", err)
	}

	// Start community cache watcher in background
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.communityCache.WatchAndSync(ctx, communityBucket); err != nil {
			if ctx.Err() == nil {
				c.logger.Error("community cache watcher failed", "error", err)
			}
		}
	}()

	// Register GraphRAG handlers
	if err := c.setupGraphRAGHandlers(ctx); err != nil {
		return fmt.Errorf("setup GraphRAG handlers: %w", err)
	}

	c.logger.Info("GraphRAG enabled")
	return nil
}

// enableGraphRAG is called when COMMUNITY_INDEX bucket becomes available after being unavailable.
// This is the OnAvailable callback for the resource watcher.
func (c *Component) enableGraphRAG(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ctx.Err() != nil {
		return // Component shutting down
	}

	if err := c.startGraphRAGWatcher(ctx); err != nil {
		c.logger.Error("failed to enable GraphRAG after bucket became available", "error", err)
	}
}

// disableGraphRAG is called when COMMUNITY_INDEX bucket is lost.
// This is the OnLost callback for the resource watcher.
func (c *Component) disableGraphRAG() {
	c.logger.Warn("COMMUNITY_INDEX bucket lost, GraphRAG queries will fail until recovered")
	// Note: We don't stop the community cache watcher here because:
	// 1. The watcher will handle the bucket disappearing gracefully
	// 2. When bucket returns, we'll get the OnAvailable callback
	// The communityCache.IsAvailable() check in handlers will prevent queries
}

// reportQuerying reports the querying stage (throttled to avoid KV spam)
func (c *Component) reportQuerying(ctx context.Context) {
	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, "querying"); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "querying"), slog.Any("error", err))
		}
	}
}

// recordSuccess records successful query metrics
func (c *Component) recordSuccess(bytesIn, bytesOut int) {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()

	c.messagesProcessed++
	c.bytesProcessed += int64(bytesIn + bytesOut)
}

// recordError records error metrics and updates health
func (c *Component) recordError(err error) {
	c.metricsMu.Lock()
	c.errors++
	c.metricsMu.Unlock()

	c.healthMu.Lock()
	c.errorCount++
	c.lastError = err
	c.healthMu.Unlock()

	c.logger.Error("query failed", "error", err)
}

// Register registers the graph-query component factory with the registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-query", &component.Registration{
		Name:         "graph-query",
		Type:         "processor",
		Protocol:     "nats",
		Domain:       "graph",
		Description:  "Query coordinator for graph subsystem",
		Version:      "1.0.0",
		Factory:      CreateGraphQuery,
		Schema:       DefaultConfig().Schema(),
		Dependencies: []string{component.DepModelRegistry},
	})
}

// Schema returns the configuration schema for the component
func (c Config) Schema() component.ConfigSchema {
	return component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"ports": {
				Type:        "object",
				Description: "Port configuration for input and output connections",
			},
			"query_timeout": {
				Type:        "string",
				Description: "Timeout for query operations (e.g., '5s', '10s')",
			},
			"max_depth": {
				Type:        "integer",
				Description: "Maximum traversal depth for path search queries",
			},
		},
		Required: []string{"ports"},
	}
}
