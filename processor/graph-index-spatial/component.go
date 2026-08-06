// Package graphindexspatial provides the graph-index-spatial component for spatial indexing.
package graphindexspatial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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

// Config holds configuration for graph-index-spatial component
type Config struct {
	Ports            *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	GeohashPrecision int                   `json:"geohash_precision" schema:"type:int,description:Geohash precision (1-12),category:basic"`
	Workers          int                   `json:"workers" schema:"type:int,description:Number of worker goroutines,category:basic"`
	BatchSize        int                   `json:"batch_size" schema:"type:int,description:Event batch size,category:basic"`

	// Dependency startup configuration
	StartupAttempts int `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts to wait for dependencies at startup,category:advanced"`
	StartupInterval int `json:"startup_interval_ms,omitempty" schema:"type:int,description:Interval between startup attempts in milliseconds,category:advanced"`
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

	// Validate SPATIAL_INDEX output exists
	hasSpatialIndex := false
	for _, output := range c.Ports.Outputs {
		if output.Subject == graph.BucketSpatialIndex {
			hasSpatialIndex = true
			break
		}
	}
	if !hasSpatialIndex {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", fmt.Sprintf("%s output required", graph.BucketSpatialIndex))
	}

	// Validate geohash precision
	if c.GeohashPrecision < 1 || c.GeohashPrecision > 12 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "geohash_precision must be between 1 and 12")
	}

	// Validate workers
	if c.Workers <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "workers must be greater than 0")
	}

	// Validate batch size
	if c.BatchSize <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "batch_size must be greater than 0")
	}

	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	if c.GeohashPrecision == 0 {
		c.GeohashPrecision = 6
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	if c.BatchSize == 0 {
		c.BatchSize = 100
	}

	// Dependency startup defaults
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 30 // ~15 seconds with 500ms interval
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 // milliseconds
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
					Name:    "spatial_index",
					Type:    "kv-write",
					Subject: graph.BucketSpatialIndex,
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
					Name:    "spatial_index",
					Type:    "kv-write",
					Subject: graph.BucketSpatialIndex,
				},
			},
		},
		GeohashPrecision: 6,
		Workers:          4,
		BatchSize:        100,
	}
}

// schema defines the configuration schema for graph-index-spatial component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

type entityStatesWatcher interface {
	WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// Component implements the graph-index-spatial processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Domain resources
	spatialBucket jetstream.KeyValue

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
	resetState        atomic.Pointer[graph.StateContractError]
	watchUnavailable  atomic.Bool
	bootstrapStarted  atomic.Bool
	bootstrapComplete atomic.Bool

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

// CreateGraphIndexSpatial is the factory function for creating graph-index-spatial components
func CreateGraphIndexSpatial(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphIndexSpatial", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphIndexSpatial", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphIndexSpatial", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-index-spatial")

	// Create component
	comp := &Component{
		name:       "graph-index-spatial",
		config:     config,
		natsClient: natsClient,
		logger:     logger,
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-index-spatial factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-index-spatial", &component.Registration{
		Name:        "graph-index-spatial",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Graph spatial indexing processor for geospatial queries",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphIndexSpatial,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-index-spatial",
		Type:        "processor",
		Description: "Graph spatial indexing processor for geospatial queries",
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
		if c.resetState.Load() != nil {
			status = graph.IndexStateResetRequired
			lastErr = graph.ErrorCodeGraphStateResetRequired + ": " + string(c.graphStateResetReason())
		} else if c.watchUnavailable.Load() {
			status = graph.IndexStateDegraded
			lastErr = graph.ErrorCodeIndexNotReady + ": ENTITY_STATES watcher unavailable"
		}
		if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy: c.running && errorCount == 0 && c.resetState.Load() == nil &&
			!c.watchUnavailable.Load(),
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
	c.logger.Info("component initialized", slog.String("component", "graph-index-spatial"))

	return nil
}

// waitForEntityBucket waits for the ENTITY_STATES bucket to be available and returns it
func (c *Component) waitForEntityBucket(ctx context.Context) (entityStatesWatcher, error) {
	// Report waiting stage
	if err := c.lifecycleReporter.ReportStage(ctx, "waiting_for_"+graph.BucketEntityStates); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "waiting_for_"+graph.BucketEntityStates), slog.Any("error", err))
	}

	// Configure resource watcher for bounded startup attempts
	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = time.Duration(c.config.StartupInterval) * time.Millisecond
	watcherCfg.Logger = c.logger

	entityWatcher := resource.NewWatcher(
		graph.BucketEntityStates,
		func(checkCtx context.Context) error {
			_, err := graph.OpenCatalogReader(checkCtx, c.natsClient, graph.BucketEntityStates)
			return err
		},
		watcherCfg,
	)

	if !entityWatcher.WaitForStartup(ctx) {
		return nil, errs.WrapTransient(
			errs.ErrStorageUnavailable,
			"Component", "waitForEntityBucket",
			fmt.Sprintf("bucket %s not available after %d attempts", graph.BucketEntityStates, c.config.StartupAttempts),
		)
	}

	reader, err := graph.OpenCatalogReader(ctx, c.natsClient, graph.BucketEntityStates)
	if err != nil {
		return nil, err
	}
	return reader, nil
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
		return errs.WrapFatal(errs.ErrNotStarted, "Component", "Start", "component not initialized")
	}

	// Idempotent - already running
	if c.running {
		return nil
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// SPATIAL_INDEX bucket (we are the WRITER) — acquired through the catalog
	// owner seam, which reconciles an adopted bucket to the declared policy.
	spatialBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketSpatialIndex)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during bucket creation")
		}
		return errs.Wrap(err, "Component", "Start", fmt.Sprintf("KV bucket creation: %s", graph.BucketSpatialIndex))
	}
	c.spatialBucket = spatialBucket

	// Initialize lifecycle reporter early for dependency waiting visibility
	c.initLifecycleReporter(ctx)

	// Queries must fail closed from the moment they are exposed until the
	// WatchAll bootstrap has been validated and fully projected.
	c.watchUnavailable.Store(false)
	c.bootstrapComplete.Store(false)
	c.bootstrapStarted.Store(true)

	// Set up query handlers
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Wait for entity states bucket
	entityBucket, err := c.waitForEntityBucket(ctx)
	if err != nil {
		cancel()
		return err
	}

	// Start entity watcher goroutine
	c.wg.Add(1)
	go c.watchEntityStates(ctx, entityBucket)

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("component started",
		slog.String("component", "graph-index-spatial"),
		slog.Time("start_time", c.startTime),
		slog.Int("geohash_precision", c.config.GeohashPrecision),
		slog.Int("workers", c.config.Workers),
		slog.Int("batch_size", c.config.BatchSize))

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
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-index-spatial"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-index-spatial"))
		return errs.WrapTransient(errs.ErrConnectionTimeout, "Component", "Stop", fmt.Sprintf("timeout after %v", timeout))
	}
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	c.lifecycleReporter = component.NewCatalogLifecycleReporter(ctx, c.natsClient, "graph-index-spatial", c.logger)
}

// ============================================================================
// Entity State Watcher
// ============================================================================

// watchEntityStates watches the ENTITY_STATES KV bucket and indexes entities with spatial data
func (c *Component) watchEntityStates(ctx context.Context, bucket entityStatesWatcher) {
	defer c.wg.Done()
	c.bootstrapStarted.Store(true)

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		// Context cancellation during shutdown is expected, not an error
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("entity watcher stopped due to context cancellation",
				slog.String("bucket", graph.BucketEntityStates))
			return
		}
		c.logger.Error("failed to start entity watcher",
			slog.String("bucket", graph.BucketEntityStates),
			slog.Any("error", err))
		c.watchUnavailable.Store(true)
		return
	}
	// NOTE: watcher.Stop() is called explicitly before each return, not via defer.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine when using defer.

	c.logger.Info("entity watcher started", slog.String("bucket", graph.BucketEntityStates))

	// WatchAll delivers the current snapshot followed by nil, then continues with
	// live updates. Build the private projection incrementally in constant space,
	// but keep every query gated until nil proves the complete snapshot valid.
	// The same watcher remains active, eliminating a list-then-watch gap.
	validating := true
	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("entity watcher stopping", slog.String("reason", "context cancelled"))
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				// A KeyWatcher closes only after Stop or connection loss. Context
				// cancellation is handled above; every other closure fails closed.
				watcher.Stop()
				if ctx.Err() == nil {
					c.watchUnavailable.Store(true)
				}
				return
			}
			if entry == nil {
				if !validating {
					continue
				}
				validating = false
				if c.resetState.Load() != nil {
					c.logger.Error("entity watcher bootstrap rejected",
						slog.String("reason", string(c.graphStateResetReason())))
					continue
				}
				c.bootstrapComplete.Store(true)
				c.logger.Debug("entity watcher initial sync complete")
				continue
			}

			if validating {
				if !graph.IsKVTombstone(entry.Operation()) {
					c.validateEntityStateEntry(entry)
				}
				if c.resetState.Load() == nil {
					c.applyEntityWatchEntry(ctx, entry)
				}
				continue
			}
			if c.resetState.Load() == nil {
				c.applyEntityWatchEntry(ctx, entry)
			}
		}
	}
}

func (c *Component) validateEntityStateEntry(entry jetstream.KeyValueEntry) {
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.latchGraphStateReset(stateErr.Reason)
			return
		}
		c.latchGraphStateReset(graph.GraphStateReasonUnreadableEntity)
	}
}

func (c *Component) applyEntityWatchEntry(ctx context.Context, entry jetstream.KeyValueEntry) {
	if graph.IsKVTombstone(entry.Operation()) {
		c.handleEntityDelete(ctx, entry.Key())
		return
	}
	c.processEntityUpdate(ctx, entry)
}

func (c *Component) latchGraphStateReset(reason graph.StateResetReason) {
	c.resetState.CompareAndSwap(nil, &graph.StateContractError{Reason: reason})
}

// processEntityUpdate indexes an entity's spatial data if it has coordinates
func (c *Component) processEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	// Report indexing stage (throttled to avoid KV spam)
	if err := c.lifecycleReporter.ReportStage(ctx, "indexing"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "indexing"), slog.Any("error", err))
	}

	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.latchGraphStateReset(stateErr.Reason)
		}
		c.logger.Warn("failed to unmarshal entity state",
			slog.String("entity", entry.Key()),
			slog.Any("error", err))
		return
	}

	// Determine entity ID
	entityID := state.ID
	if entityID == "" {
		entityID = entry.Key()
	}
	if entityID == "" && len(state.Triples) > 0 {
		entityID = state.Triples[0].Subject
	}

	// Extract coordinates from triples
	lat, lon, alt := c.extractGeoCoordinates(state.Triples)

	// Skip if no coordinates found
	if lat == nil || lon == nil {
		return
	}

	// Calculate geohash
	geohashStr := c.calculateGeohash(*lat, *lon, c.config.GeohashPrecision)

	// Update spatial index
	altValue := 0.0
	if alt != nil {
		altValue = *alt
	}

	if err := c.updateSpatialIndex(ctx, geohashStr, entityID, *lat, *lon, altValue); err != nil {
		c.logger.Warn("failed to update spatial index",
			slog.String("entity", entityID),
			slog.String("geohash", geohashStr),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("indexed entity spatial data",
		slog.String("entity", entityID),
		slog.String("geohash", geohashStr),
		slog.Float64("lat", *lat),
		slog.Float64("lon", *lon))

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())
}

func (c *Component) graphStateResetReason() graph.StateResetReason {
	if state := c.resetState.Load(); state != nil {
		return state.Reason
	}
	return graph.GraphStateReasonUnreadableEntity
}

func (c *Component) graphStateResetError() error {
	state := c.resetState.Load()
	if state == nil {
		state = &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity}
	}
	return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		state)
}

func (c *Component) ensureBootstrapReady() error {
	if c.resetState.Load() != nil {
		return c.graphStateResetError()
	}
	if c.watchUnavailable.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("spatial index not ready: ENTITY_STATES watcher is unavailable"))
	}
	if c.bootstrapStarted.Load() && !c.bootstrapComplete.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("spatial index not ready: ENTITY_STATES bootstrap is still validating"))
	}
	return nil
}

// extractGeoCoordinates extracts latitude, longitude, and altitude from entity triples
func (c *Component) extractGeoCoordinates(triples []message.Triple) (lat, lon, alt *float64) {
	for _, triple := range triples {
		switch triple.Predicate {
		case "geo.location.latitude":
			if latVal, ok := triple.Object.(float64); ok {
				lat = &latVal
			}
		case "geo.location.longitude":
			if lonVal, ok := triple.Object.(float64); ok {
				lon = &lonVal
			}
		case "geo.location.altitude":
			if altVal, ok := triple.Object.(float64); ok {
				alt = &altVal
			}
		}
	}
	return lat, lon, alt
}

// updateSpatialIndex updates the spatial index bucket for a geohash cell
func (c *Component) updateSpatialIndex(ctx context.Context, geohashStr, entityID string, lat, lon, alt float64) error {
	// Get current data for this geohash cell
	entry, err := c.spatialBucket.Get(ctx, geohashStr)

	var spatialData map[string]interface{}
	if err == nil {
		if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
			spatialData = map[string]interface{}{
				"entities":    map[string]interface{}{},
				"last_update": time.Now().Unix(),
			}
		}
	} else {
		spatialData = map[string]interface{}{
			"entities":    map[string]interface{}{},
			"last_update": time.Now().Unix(),
		}
	}

	// Get or create entities map
	entities, ok := spatialData["entities"].(map[string]interface{})
	if !ok {
		entities = map[string]interface{}{}
	}

	// Add/update entity position
	entities[entityID] = map[string]interface{}{
		"lat":     lat,
		"lon":     lon,
		"alt":     alt,
		"updated": time.Now().Unix(),
	}
	spatialData["entities"] = entities
	spatialData["last_update"] = time.Now().Unix()

	// Serialize and write
	data, err := json.Marshal(spatialData)
	if err != nil {
		return errs.Wrap(err, "Component", "updateSpatialIndex", "marshal spatial data")
	}

	if entry != nil {
		_, err = c.spatialBucket.Update(ctx, geohashStr, data, entry.Revision())
	} else {
		_, err = c.spatialBucket.Create(ctx, geohashStr, data)
	}

	if err != nil {
		return errs.Wrap(err, "Component", "updateSpatialIndex", "write spatial data")
	}

	return nil
}

// handleEntityDelete handles entity deletion from the spatial index
func (c *Component) handleEntityDelete(_ context.Context, entityID string) {
	c.logger.Debug("entity deleted - spatial cleanup not fully implemented",
		slog.String("entity", entityID))
}

// calculateGeohash calculates a configurable-precision geohash for spatial indexing
// This matches the algorithm used in graph/indexmanager/indexes.go
func (c *Component) calculateGeohash(lat, lon float64, precision int) string {
	// Configurable spatial binning based on precision:
	// precision=4: ~2.5km bins  (multiplier=10)
	// precision=5: ~600m bins   (multiplier=50)
	// precision=6: ~120m bins   (multiplier=100)
	// precision=7: ~30m bins    (multiplier=300)  <- Default
	// precision=8: ~5m bins     (multiplier=1000)

	var multiplier float64
	switch precision {
	case 4:
		multiplier = 10.0 // ~2.5km resolution
	case 5:
		multiplier = 50.0 // ~600m resolution
	case 6:
		multiplier = 100.0 // ~120m resolution
	case 7:
		multiplier = 300.0 // ~30m resolution (default)
	case 8:
		multiplier = 1000.0 // ~5m resolution
	default:
		multiplier = 300.0 // Default to precision 7
	}

	// Normalize coordinates to positive integers with precision-based binning
	latInt := int(math.Floor((lat + 90.0) * multiplier))
	lonInt := int(math.Floor((lon + 180.0) * multiplier))
	return fmt.Sprintf("geo_%d_%d_%d", precision, latInt, lonInt)
}
