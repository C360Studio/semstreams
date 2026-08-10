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
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
)

// Note: jetstream import is used for KV types (jetstream.KeyValue, jetstream.KeyWatcher).
// This is the standard pattern across all processor components.
// natsclient wraps NATS operations but doesn't abstract jetstream types.

// natsRequester is a local interface for NATS request/reply and JetStream access.
// *natsclient.Client satisfies this interface, and tests can provide mocks.
type natsRequester interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
	// RequestClassified is the gh#93 caller-side path: handler errors
	// surface via the err return as classified errors, transport
	// errors remain on err too. Prefer over Request+body-sniff for
	// new callers.
	RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
	SubscribeForRequests(ctx context.Context, subject string, handler func(ctx context.Context, data []byte) ([]byte, error)) (*natsclient.Subscription, error)
	Status() natsclient.ConnectionStatus
	Connect(ctx context.Context) error
	WaitForConnection(ctx context.Context) error
	JetStream() (jetstream.JetStream, error)
}

// Config defines the configuration for the graph-query coordinator component
type Config struct {
	Ports                *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
	QueryTimeout         time.Duration         `json:"query_timeout,omitempty" schema:"type:duration,description:Timeout for query operations,default:5s,category:basic"`
	MaxDepth             int                   `json:"max_depth,omitempty" schema:"type:int,description:Maximum traversal depth for path search,default:10,min:1,max:100,category:basic"`
	RecheckInterval      time.Duration         `json:"recheck_interval,omitempty" schema:"type:duration,description:Interval for rechecking missing buckets,default:5s,category:advanced"`
	MinSemanticRelevance float64               `json:"min_semantic_relevance,omitempty" schema:"type:float,description:Minimum neural embedding similarity score (0.0-1.0),default:0.5,min:0,max:1,category:advanced"`
	MinBM25Relevance     float64               `json:"min_bm25_relevance,omitempty" schema:"type:float,description:Minimum BM25 embedding similarity score (0.0-1.0),default:0.4,min:0,max:1,category:advanced"`
	MinTextRelevance     float64               `json:"min_text_relevance,omitempty" schema:"type:float,description:Minimum text match score (0.0-1.0),default:0.3,min:0,max:1,category:advanced"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Ports == nil {
		return errors.New("ports configuration is required")
	}
	if len(c.Ports.Inputs) != 1 {
		return fmt.Errorf("exactly one graph_queries input port is required, got %d", len(c.Ports.Inputs))
	}
	if len(c.Ports.Outputs) != 0 {
		return fmt.Errorf("graph-query declares no output ports, got %d", len(c.Ports.Outputs))
	}
	definition := c.Ports.Inputs[0]
	if definition.Name != graphQueriesPortName || !definition.Required {
		return errors.New("graph-query input must be the required graph_queries port")
	}
	port, err := definition.Resolve(component.DirectionInput)
	if err != nil {
		return fmt.Errorf("resolve graph_queries input: %w", err)
	}
	facts, err := port.Facts()
	if err != nil {
		return fmt.Errorf("inspect graph_queries input: %w", err)
	}
	contract, hasContract := facts.Interface()
	if facts.Kind() != component.PortKindNATSRequest || !hasContract ||
		contract.Type != graphQueryInterfaceType || contract.Version != graphQueryInterfaceVersion ||
		len(facts.NATSSubjects()) != 1 || facts.NATSSubjects()[0] != graphQuerySubjectFamily {
		return fmt.Errorf("graph_queries must be required nats-request %s %s on %s",
			graphQueryInterfaceType, graphQueryInterfaceVersion, graphQuerySubjectFamily)
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
				{
					Name:     graphQueriesPortName,
					Required: true,
					Config: component.NATSRequestPort{
						Subject:   graphQuerySubjectFamily,
						Interface: graphQueryInterface(),
					},
				},
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
	inputs        []component.Port
	queryFamily   string
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
	communityCache       *communityCache
	openCommunityReader  func(context.Context) (graph.CatalogReader, error)
	waitCommunityRetry   func(context.Context, time.Duration) bool
	communityPublished   func(uint64)
	communityUnpublished func(uint64)
	// Private synchronization seams for proving final lease validation in tests.
	searchGraphBeforeFinalize  func()
	globalSearchBeforeFinalize func()
	localSearchBeforeFinalize  func()

	// Optional COMMUNITY_SUMMARIES serving view. The supervisor is the sole
	// lifecycle owner; readers copy this synchronized pointer and call Get outside
	// the mutex so view loss and Stop remain fail-closed.
	summaryViewMu          sync.RWMutex
	summaryView            *graphview.View[clustering.CommunitySummaryRecord]
	openSummaryReader      func(context.Context) (graph.CatalogReader, error)
	waitSummaryRetry       func(context.Context, time.Duration) bool
	summaryViewConstructed func(*graphview.View[clustering.CommunitySummaryRecord])
	summaryViewChanged     func(*graphview.View[clustering.CommunitySummaryRecord])
	summaryViewApplied     func(string, uint64)
	summaryViewStopped     func(*graphview.View[clustering.CommunitySummaryRecord])

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
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, fmt.Errorf("resolve input port: %w", err)
		}
		inputs = append(inputs, port)
	}
	queryFacts, err := inputs[0].Facts()
	if err != nil {
		return nil, fmt.Errorf("inspect graph query subject family: %w", err)
	}
	queryFamily := queryFacts.NATSSubjects()[0]

	// Create component with keyword-only classifier; LLM classifier wired in Start()
	comp := &Component{
		config:               config,
		inputs:               inputs,
		queryFamily:          queryFamily,
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
		openCommunityReader: func(ctx context.Context) (graph.CatalogReader, error) {
			return graph.OpenCatalogReader(ctx, deps.NATSClient, graph.BucketCommunityIndex)
		},
		openSummaryReader: func(ctx context.Context) (graph.CatalogReader, error) {
			return graph.OpenCatalogReader(ctx, deps.NATSClient, graph.BucketCommunitySummaries)
		},
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

	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns the component's output ports (none for query coordinator)
func (c *Component) OutputPorts() []component.Port {
	// Query coordinator has no output ports - it returns data via request/reply
	return nil
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

// initLLMClassifier wires the LLM query classifier if the model registry
// has a query_classification capability configured. Gracefully degrades to
// keyword-only classification on any error.
func (c *Component) initLLMClassifier() {
	if c.modelRegistry == nil {
		return
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityQueryClassification)
	if err != nil {
		// No query_classification capability configured — keyword-only is fine
		return
	}
	timeout := model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityQueryClassification, query.DefaultClassificationTimeout, c.logger)
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = timeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		c.logger.Warn("failed to create LLM query classifier, using keyword-only",
			slog.Any("error", err))
		return
	}
	c.llmClient = client
	adapter := query.NewLLMClientAdapter(client, timeout)
	llmClassifier := query.NewLLMClassifier(adapter, nil)
	c.classifier = query.NewClassifierChain(query.NewKeywordClassifier(), nil, llmClassifier)
	c.logger.Info("LLM query classifier enabled",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", timeout))
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
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityAnswerSynthesis)
	if err != nil {
		c.logger.Debug("no answer_synthesis endpoint configured, using template fallback")
		return
	}
	timeout := model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityAnswerSynthesis, DefaultAnswerSynthesisTimeout, c.logger)
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = timeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		c.logger.Warn("failed to create answer synthesis LLM client, using template fallback",
			slog.Any("error", err))
		return
	}
	c.answerSynthesizer = NewLLMAnswerSynthesizer(client, resolved.Model, c.logger, timeout)
	c.logger.Info("LLM answer synthesis enabled",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", timeout))
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

	// Create router for static routing
	c.router = NewStaticRouter(c.logger)

	// Construct the cache before responders are installed. localSearch is stable
	// at every successful Start and may receive a request as soon as its
	// subscription exists; publishing the pointer first avoids a startup race.
	c.communityCache = newCommunityCache(c.logger)
	c.communityCache.onPublished = c.communityPublished
	c.communityCache.onUnpublished = c.communityUnpublished

	// Subscribe to query subjects
	if err := c.setupQueryHandlers(componentCtx); err != nil {
		return fmt.Errorf("subscribe to queries: %w", err)
	}

	// COMMUNITY_INDEX is optional at component start. One component-lifetime
	// supervisor repeatedly performs the catalog's must-exist reader open and a
	// fresh WatchAll generation; responders remain installed throughout.
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.superviseCommunityGenerations(componentCtx)
	}()

	// Independently supervise the optional, content-addressed summary serving
	// view. Absence and bootstrap never delay component startup or gate queries.
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.superviseSummaryView(componentCtx)
	}()

	c.started = true

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

	c.mu.Lock()
	c.started = false
	c.mu.Unlock()

	c.logger.Info("graph-query coordinator stopped")
	return nil
}

func (c *Component) superviseCommunityGenerations(ctx context.Context) {
	if c.openCommunityReader == nil {
		c.logger.Error("community generation supervisor has no catalog reader opener")
		return
	}
	waitRetry := c.waitCommunityRetry
	if waitRetry == nil {
		waitRetry = func(ctx context.Context, interval time.Duration) bool {
			timer := time.NewTimer(interval)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			}
		}
	}

	var generationID uint64
	for ctx.Err() == nil {
		generationID++
		generation := newCommunityGeneration(generationID)
		reader, err := c.openCommunityReader(ctx)
		if err == nil {
			err = c.communityCache.watchGeneration(ctx, reader, generation)
		}
		if ctx.Err() != nil {
			return
		}
		c.logger.Warn("community generation unavailable; retrying",
			"generation", generationID, "error", err,
			"retry_interval", c.config.RecheckInterval)
		if !waitRetry(ctx, c.config.RecheckInterval) {
			return
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
