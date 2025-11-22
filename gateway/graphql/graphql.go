// Package graphql provides a GraphQL gateway for SemStreams.
// Phase 1: Generic infrastructure for schema-driven GraphQL API
// Phase 2: Code generation from GraphQL schema
package graphql

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/gateway"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/processor/graph/querymanager"
)

// graphqlGatewaySchema defines the configuration schema
var graphqlGatewaySchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Gateway implements the Gateway interface for GraphQL protocol
type Gateway struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Components
	client   *NATSClient
	resolver *BaseResolver
	server   *Server

	// Lifecycle state (atomic operations, no mutex needed for running flag)
	running atomic.Bool

	// Protects metrics and startTime for concurrent reads
	mu        sync.RWMutex
	startTime time.Time
	shutdown  chan struct{}
	done      chan struct{}

	// Metrics (atomic operations)
	requestsTotal   atomic.Uint64
	requestsSuccess atomic.Uint64
	requestsFailed  atomic.Uint64
	lastActivity    time.Time
}

// NewGraphQLGateway creates a new GraphQL gateway from configuration
func NewGraphQLGateway(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "GraphQLGateway", "NewGraphQLGateway", "config unmarshal")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errors.WrapInvalid(err, "GraphQLGateway", "NewGraphQLGateway", "config validation")
	}

	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errors.WrapFatal(errors.ErrMissingConfig, "GraphQLGateway", "NewGraphQLGateway",
			"NATS client is required")
	}

	logger := deps.GetLogger().With("component", "graphql-gateway")

	// Create NATS client wrapper
	natsClient := NewNATSClient(deps.NATSClient, config.NATSSubjects, config.Timeout())

	// Create gateway instance
	gateway := &Gateway{
		name:       "graphql-gateway",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		client:     natsClient,
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
	}

	// Create base resolver with preferred backend
	// Prefer QueryManager (direct access with caching) over NATS (remote queries)
	var resolver *BaseResolver
	if deps.QueryManager != nil {
		// Type assert QueryManager to the expected interface
		if qm, ok := deps.QueryManager.(querymanager.Querier); ok {
			// Use QueryManager for optimized, cached queries
			resolver = NewBaseResolver(qm, gateway)
			logger.Info("GraphQL gateway using QueryManager backend")
		} else {
			logger.Warn("QueryManager type assertion failed, falling back to NATS")
			resolver = NewBaseResolverWithNATS(natsClient, gateway)
		}
	} else {
		// Fall back to NATS backend
		resolver = NewBaseResolverWithNATS(natsClient, gateway)
		logger.Info("GraphQL gateway using NATS backend (QueryManager not available)")
	}

	// Create HTTP server
	server, err := NewServer(config, resolver, logger)
	if err != nil {
		return nil, errors.WrapFatal(err, "GraphQLGateway", "NewGraphQLGateway", "create server")
	}

	gateway.resolver = resolver
	gateway.server = server

	return gateway, nil
}

// Initialize prepares the GraphQL gateway
func (g *Gateway) Initialize() error {
	g.logger.Info("Initializing GraphQL gateway")

	// Setup HTTP server
	if err := g.server.Setup(); err != nil {
		return errors.WrapFatal(err, "GraphQLGateway", "Initialize", "server setup")
	}

	g.logger.Info("GraphQL gateway initialized",
		"address", g.config.BindAddress,
		"path", g.config.Path)

	return nil
}

// Start begins the GraphQL gateway operation
func (g *Gateway) Start(ctx context.Context) error {
	// ComponentManager already serializes Start/Stop calls
	// Check if already running (atomic read, no lock needed)
	if g.running.Load() {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "GraphQLGateway", "Start",
			"gateway already running")
	}

	// Set running state
	g.mu.Lock()
	g.running.Store(true)
	g.startTime = time.Now()
	g.mu.Unlock()

	g.logger.Info("GraphQL gateway starting")

	// Create ready channel
	ready := make(chan struct{})
	errChan := make(chan error, 1)

	// Start HTTP server in background
	go func() {
		if err := g.server.Start(ctx, ready); err != nil {
			select {
			case errChan <- err:
			default:
			}
			g.mu.Lock()
			g.running.Store(false)
			g.mu.Unlock()
		}
	}()

	// Wait for server to be ready or error
	select {
	case <-ready:
		g.logger.Info("GraphQL gateway started successfully")
	case err := <-errChan:
		return err
	case <-time.After(5 * time.Second):
		return errors.WrapFatal(errors.ErrConnectionTimeout, "GraphQLGateway", "Start",
			"server failed to start within timeout")
	}

	// Wait for context cancellation
	<-ctx.Done()
	g.logger.Info("GraphQL gateway context cancelled")

	return g.Stop(30 * time.Second)
}

// Stop gracefully stops the GraphQL gateway
func (g *Gateway) Stop(timeout time.Duration) error {
	if !g.running.Load() {
		return nil // Already stopped
	}

	g.logger.Info("GraphQL gateway stopping")

	// Signal shutdown
	close(g.shutdown)

	// Stop HTTP server
	if err := g.server.Stop(timeout); err != nil {
		g.logger.Error("Failed to stop server", "error", err)
		return err
	}

	// Update state
	g.mu.Lock()
	g.running.Store(false)
	close(g.done)
	g.mu.Unlock()

	g.logger.Info("GraphQL gateway stopped")

	return nil
}

// RegisterHTTPHandlers registers gateway routes with the HTTP mux
// Note: For Phase 1, we're running our own HTTP server
// Phase 2 may integrate with ServiceManager's central server
func (g *Gateway) RegisterHTTPHandlers(prefix string, _ *http.ServeMux) {
	// Phase 1: We run our own HTTP server, so this is a no-op
	// Phase 2: May register with central server if needed
	g.logger.Info("RegisterHTTPHandlers called",
		"prefix", prefix,
		"note", "GraphQL gateway runs its own HTTP server")
}

// Component metadata implementation

// Meta returns component metadata
func (g *Gateway) Meta() component.Metadata {
	return component.Metadata{
		Name:        g.name,
		Type:        "gateway",
		Description: "GraphQL gateway for schema-driven NATS queries (Phase 1: Infrastructure)",
		Version:     "0.1.0",
	}
}

// InputPorts returns no input ports (gateway is request-driven)
func (g *Gateway) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns no output ports (gateway uses request/reply)
func (g *Gateway) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema
func (g *Gateway) ConfigSchema() component.ConfigSchema {
	return graphqlGatewaySchema
}

// Health returns the current health status
func (g *Gateway) Health() component.HealthStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()

	healthy := g.running.Load() && g.server.IsRunning()

	return component.HealthStatus{
		Healthy:    healthy,
		LastCheck:  time.Now(),
		ErrorCount: int(g.requestsFailed.Load()),
		Uptime:     time.Since(g.startTime),
	}
}

// DataFlow returns current data flow metrics
func (g *Gateway) DataFlow() component.FlowMetrics {
	g.mu.RLock()
	defer g.mu.RUnlock()

	total := g.requestsTotal.Load()
	failed := g.requestsFailed.Load()

	var errorRate float64
	if total > 0 {
		errorRate = float64(failed) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      g.lastActivity,
	}
}

// RecordMetrics wraps a GraphQL operation to record metrics
func (g *Gateway) RecordMetrics(_ context.Context, operation string, fn func() error) error {
	start := time.Now()

	g.requestsTotal.Add(1)

	err := fn()
	duration := time.Since(start)

	if err != nil {
		g.requestsFailed.Add(1)
		g.logger.Warn("GraphQL operation failed",
			"operation", operation,
			"duration", duration,
			"error", err)
	} else {
		g.requestsSuccess.Add(1)
		g.logger.Debug("GraphQL operation succeeded",
			"operation", operation,
			"duration", duration)
	}

	g.mu.Lock()
	g.lastActivity = time.Now()
	g.mu.Unlock()

	return err
}

// recordRequest records request metrics
func (g *Gateway) recordRequest(success bool) {
	g.requestsTotal.Add(1)
	if success {
		g.requestsSuccess.Add(1)
	} else {
		g.requestsFailed.Add(1)
	}

	g.mu.Lock()
	g.lastActivity = time.Now()
	g.mu.Unlock()
}

// Register registers the GraphQL gateway with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "graphql",
		Factory:     NewGraphQLGateway,
		Schema:      graphqlGatewaySchema,
		Type:        "gateway",
		Protocol:    "graphql",
		Domain:      "network",
		Description: "GraphQL gateway for schema-driven NATS queries",
		Version:     "0.1.0",
	})
}

// GetResolver returns the base resolver (for testing)
func (g *Gateway) GetResolver() *BaseResolver {
	return g.resolver
}

// Ensure Gateway implements required interfaces
var (
	_ component.Discoverable = (*Gateway)(nil)
	_ gateway.Gateway        = (*Gateway)(nil)
)
