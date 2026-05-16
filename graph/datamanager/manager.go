// Package datamanager consolidates entity and edge operations into a unified data management service.
// This implementation properly uses semstreams framework components without reimplementing them.
package datamanager

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/buffer"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/c360studio/semstreams/pkg/worker"
)

var (
	// entityIDRegex validates entity ID format: org.platform.domain.system.type.instance
	// Each segment must start with [a-zA-Z0-9] and may continue with [a-zA-Z0-9_-].
	// Kept in sync with processor/graph-ingest/component.go entityIDRegex —
	// realistic instance segments like "sensor-042" or "warehouse-7" need hyphens.
	// Example: c360.platform1.robotics.gcs1.drone.unit-1
	entityIDRegex = regexp.MustCompile(
		`^[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*\.[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
)

// Metrics holds Prometheus metrics for DataManager KV operations
type Metrics struct {
	writesTotal     *prometheus.CounterVec   // Total KV write operations by status and operation
	writeLatency    *prometheus.HistogramVec // KV write latency by operation
	queueDepth      prometheus.Gauge         // Current write queue depth
	batchSize       prometheus.Histogram     // Size of write batches
	droppedWrites   prometheus.Counter       // Total writes dropped due to queue overflow
	casRetries      prometheus.Counter       // CAS operation retries
	cacheHits       *prometheus.CounterVec   // Cache hits by level (l1, l2)
	cacheMisses     prometheus.Counter       // Cache misses (KV fetch required)
	entitiesCreated prometheus.Counter       // Total entities created
	entitiesUpdated prometheus.Counter       // Total entities updated
	entitiesDeleted prometheus.Counter       // Total entities deleted
}

// newMetrics creates and registers DataManager metrics
func newMetrics(registry *metric.MetricsRegistry) *Metrics {
	if registry == nil {
		return nil
	}

	m := &Metrics{
		writesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "writes_total",
			Help:      "Total KV write operations",
		}, []string{"status", "operation"}),

		writeLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "write_latency_seconds",
			Help:      "KV write latency distribution",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}, []string{"operation"}),

		queueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "queue_depth",
			Help:      "Current write queue depth",
		}),

		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "batch_size",
			Help:      "Size of write batches",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 200},
		}),

		droppedWrites: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "dropped_writes_total",
			Help:      "Total writes dropped due to queue overflow",
		}),

		casRetries: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "cas_retries_total",
			Help:      "Total CAS operation retries",
		}),

		cacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "cache_hits_total",
			Help:      "Total cache hits by level",
		}, []string{"level"}),

		cacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "cache_misses_total",
			Help:      "Total cache misses (KV fetch required)",
		}),

		entitiesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "entities_created_total",
			Help:      "Total entities created",
		}),

		entitiesUpdated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "entities_updated_total",
			Help:      "Total entities updated",
		}),

		entitiesDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "entities_deleted_total",
			Help:      "Total entities deleted",
		}),
	}

	// Register all metrics with the registry
	serviceName := "datamanager"
	registry.RegisterCounterVec(serviceName, "writes_total", m.writesTotal)
	registry.RegisterHistogramVec(serviceName, "write_latency_seconds", m.writeLatency)
	registry.RegisterGauge(serviceName, "queue_depth", m.queueDepth)
	registry.RegisterHistogram(serviceName, "batch_size", m.batchSize)
	registry.RegisterCounter(serviceName, "dropped_writes_total", m.droppedWrites)
	registry.RegisterCounter(serviceName, "cas_retries_total", m.casRetries)
	registry.RegisterCounterVec(serviceName, "cache_hits_total", m.cacheHits)
	registry.RegisterCounter(serviceName, "cache_misses_total", m.cacheMisses)
	registry.RegisterCounter(serviceName, "entities_created_total", m.entitiesCreated)
	registry.RegisterCounter(serviceName, "entities_updated_total", m.entitiesUpdated)
	registry.RegisterCounter(serviceName, "entities_deleted_total", m.entitiesDeleted)

	return m
}

// validateEntityID validates that an entity ID follows the expected format
func validateEntityID(id string) error {
	if id == "" {
		return errs.WrapInvalid(nil, "DataManager", "validateEntityID", "entity ID cannot be empty")
	}

	if len(id) > 255 {
		return errs.WrapInvalid(nil, "DataManager", "validateEntityID", "entity ID too long (max 255 chars)")
	}

	if !entityIDRegex.MatchString(id) {
		parts := strings.Split(id, ".")
		msg := fmt.Sprintf(
			"invalid entity ID format: expected 6 parts (platform.namespace.type.subtype.instance.name), got %d parts",
			len(parts))
		return errs.WrapInvalid(nil, "DataManager", "validateEntityID", msg)
	}

	return nil
}

// validateEntity validates an entity state before operations
func validateEntity(entity *gtypes.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(nil, "DataManager", "validateEntity", "entity cannot be nil")
	}

	return validateEntityID(entity.ID)
}

// EntityCreatedCallback is invoked when a new entity is created in the data store.
// The callback receives the entity ID and should be non-blocking to avoid
// impacting write performance. Used by the graph processor for adaptive clustering.
type EntityCreatedCallback func(entityID string)

// Manager is the consolidated data management service using framework components.
type Manager struct {
	// Framework components - no custom implementations!
	l1Cache     cache.Cache[*gtypes.EntityState] // LRU cache from pkg/cache
	l2Cache     cache.Cache[*gtypes.EntityState] // TTL cache from pkg/cache
	writeBuffer buffer.Buffer[*EntityWrite]      // CircularBuffer from pkg/buffer
	workers     *worker.Pool[*EntityWrite]       // Worker pool from pkg/worker

	// Core dependencies
	kvBucket        jetstream.KeyValue
	metricsRegistry *metric.MetricsRegistry
	logger          *slog.Logger

	// Prometheus metrics for observability
	metrics *Metrics

	// Configuration
	config Config

	// State management
	wg sync.WaitGroup

	// Callbacks for event-driven processing
	onEntityCreated EntityCreatedCallback
	callbackMu      sync.RWMutex
}

// Note: EntityWrite is already defined in types.go
// Compile-time verification that Manager implements all interfaces is now in interfaces.go

// NewDataManager creates a new data manager using framework components
func NewDataManager(deps Dependencies) (*Manager, error) {
	if err := validateDependencies(deps); err != nil {
		return nil, err
	}

	applyConfigDefaults(&deps)

	m := &Manager{
		kvBucket:        deps.KVBucket,
		metricsRegistry: deps.MetricsRegistry,
		logger:          deps.Logger,
		config:          deps.Config,
	}

	m.metrics = newMetrics(deps.MetricsRegistry)

	if err := m.initializeL1Cache(); err != nil {
		return nil, err
	}

	// Note: L2 cache (TTL) will be initialized in Run() since it needs context

	if err := m.initializeBufferAndWorkers(); err != nil {
		return nil, err
	}

	return m, nil
}

// validateDependencies checks that required dependencies are provided.
func validateDependencies(deps Dependencies) error {
	if deps.KVBucket == nil {
		return errs.WrapInvalid(nil, "DataManager", "NewDataManager", "kvBucket is required")
	}
	if deps.Logger == nil {
		return errs.WrapInvalid(nil, "DataManager", "NewDataManager", "logger is required")
	}
	return nil
}

// applyConfigDefaults applies default values to zero-value config fields.
func applyConfigDefaults(deps *Dependencies) {
	defaults := DefaultConfig()

	// Core config defaults
	if deps.Config.Workers == 0 {
		deps.Config.Workers = defaults.Workers
	}
	if deps.Config.WriteTimeout == 0 {
		deps.Config.WriteTimeout = defaults.WriteTimeout
	}
	if deps.Config.ReadTimeout == 0 {
		deps.Config.ReadTimeout = defaults.ReadTimeout
	}
	if deps.Config.MaxRetries == 0 {
		deps.Config.MaxRetries = defaults.MaxRetries
	}
	if deps.Config.RetryDelay == 0 {
		deps.Config.RetryDelay = defaults.RetryDelay
	}

	applyBufferConfigDefaults(&deps.Config, defaults)
	applyCacheConfigDefaults(&deps.Config, defaults)
	applyBucketConfigDefaults(&deps.Config, defaults)
}

// applyBufferConfigDefaults applies buffer configuration defaults.
func applyBufferConfigDefaults(config *Config, defaults Config) {
	// If BufferConfig appears completely unset, apply full defaults
	bufferConfigIsEmpty := config.BufferConfig.Capacity == 0 &&
		config.BufferConfig.FlushInterval == 0 &&
		config.BufferConfig.MaxBatchSize == 0 &&
		config.BufferConfig.MaxBatchAge == 0 &&
		config.BufferConfig.OverflowPolicy == "" &&
		!config.BufferConfig.BatchingEnabled

	if bufferConfigIsEmpty {
		config.BufferConfig = defaults.BufferConfig
		return
	}

	// Apply individual defaults while preserving explicit settings
	if config.BufferConfig.Capacity == 0 {
		config.BufferConfig.Capacity = defaults.BufferConfig.Capacity
	}
	if config.BufferConfig.FlushInterval == 0 {
		config.BufferConfig.FlushInterval = defaults.BufferConfig.FlushInterval
	}
	if config.BufferConfig.MaxBatchSize == 0 {
		config.BufferConfig.MaxBatchSize = defaults.BufferConfig.MaxBatchSize
	}
	if config.BufferConfig.MaxBatchAge == 0 {
		config.BufferConfig.MaxBatchAge = defaults.BufferConfig.MaxBatchAge
	}
	if config.BufferConfig.OverflowPolicy == "" {
		config.BufferConfig.OverflowPolicy = defaults.BufferConfig.OverflowPolicy
	}
	// Note: BatchingEnabled is NOT defaulted - if user set it explicitly (even to false), respect it
}

// applyCacheConfigDefaults applies cache configuration defaults.
func applyCacheConfigDefaults(config *Config, defaults Config) {
	if config.Cache.L1Hot.Size == 0 {
		config.Cache.L1Hot = defaults.Cache.L1Hot
	}
	if config.Cache.L2Warm.Size == 0 {
		config.Cache.L2Warm = defaults.Cache.L2Warm
	}
}

// applyBucketConfigDefaults applies bucket configuration defaults.
func applyBucketConfigDefaults(config *Config, defaults Config) {
	if config.BucketConfig.Name == "" {
		config.BucketConfig.Name = defaults.BucketConfig.Name
	}
	if config.BucketConfig.History == 0 {
		config.BucketConfig.History = defaults.BucketConfig.History
	}
	if config.BucketConfig.Replicas == 0 {
		config.BucketConfig.Replicas = defaults.BucketConfig.Replicas
	}
}

// initializeL1Cache creates the L1 LRU cache if configured.
func (m *Manager) initializeL1Cache() error {
	if m.config.Cache.L1Hot.Size == 0 {
		return nil
	}

	opts := []cache.Option[*gtypes.EntityState]{}
	if m.metricsRegistry != nil {
		opts = append(opts, cache.WithMetrics[*gtypes.EntityState](
			m.metricsRegistry, "datamanager_l1"))
	}

	var err error
	m.l1Cache, err = cache.NewLRU[*gtypes.EntityState](m.config.Cache.L1Hot.Size, opts...)
	if err != nil {
		return errs.WrapTransient(err, "DataManager", "initializeL1Cache", "L1 cache creation")
	}
	return nil
}

// initializeBufferAndWorkers creates the write buffer and worker pool if batching is enabled.
func (m *Manager) initializeBufferAndWorkers() error {
	if !m.config.BufferConfig.BatchingEnabled {
		return nil
	}

	if err := m.initializeWriteBuffer(); err != nil {
		return err
	}

	m.initializeWorkerPool()
	return nil
}

// initializeWriteBuffer creates the circular write buffer.
func (m *Manager) initializeWriteBuffer() error {
	overflowPolicy := m.mapOverflowPolicy()

	bufOpts := []buffer.Option[*EntityWrite]{
		buffer.WithOverflowPolicy[*EntityWrite](overflowPolicy),
	}
	if m.metricsRegistry != nil {
		bufOpts = append(bufOpts, buffer.WithMetrics[*EntityWrite](
			m.metricsRegistry, "datamanager_buffer"))
	}

	var err error
	m.writeBuffer, err = buffer.NewCircularBuffer[*EntityWrite](
		m.config.BufferConfig.Capacity, bufOpts...)
	if err != nil {
		return errs.WrapTransient(err, "DataManager", "initializeWriteBuffer", "write buffer creation")
	}
	return nil
}

// mapOverflowPolicy converts string overflow policy to buffer.OverflowPolicy.
func (m *Manager) mapOverflowPolicy() buffer.OverflowPolicy {
	switch m.config.BufferConfig.OverflowPolicy {
	case "drop_newest":
		return buffer.DropNewest
	case "block":
		return buffer.Block
	default:
		return buffer.DropOldest
	}
}

// initializeWorkerPool creates the worker pool for processing writes.
func (m *Manager) initializeWorkerPool() {
	opts := []worker.Option[*EntityWrite]{}
	if m.metricsRegistry != nil {
		opts = append(opts, worker.WithMetricsRegistry[*EntityWrite](m.metricsRegistry, "datamanager_workers"))
	}
	m.workers = worker.NewPool[*EntityWrite](
		m.config.Workers,
		m.config.BufferConfig.MaxBatchSize,
		m.processWrite,
		opts...)
}

// Run starts the DataManager and blocks until context is cancelled or fatal error occurs.
// If onReady is provided, it is called once initialization completes successfully.
func (m *Manager) Run(ctx context.Context, onReady func()) error {
	// Initialize L2 cache (TTL) using framework - needs context
	if err := m.initializeL2Cache(ctx); err != nil {
		return err
	}

	// Start worker pool and buffer processing if batching enabled
	bufferErr := m.startWorkerPoolAndBufferProcessing(ctx)

	// Ensure worker pool stops on function exit
	defer func() {
		if m.config.BufferConfig.BatchingEnabled && m.workers != nil {
			// Give workers 5 seconds to drain on shutdown
			if err := m.workers.Stop(5 * time.Second); err != nil {
				m.logger.Error("Failed to stop worker pool", "error", err)
			}
		}
	}()

	m.logger.Info("DataManager running",
		"workers", m.config.Workers,
		"buffer_capacity", m.config.BufferConfig.Capacity,
		"cache_l1_size", m.config.Cache.L1Hot.Size,
		"cache_l2_size", m.config.Cache.L2Warm.Size,
		"batching_enabled", m.config.BufferConfig.BatchingEnabled,
	)

	// Signal ready - worker pool started, caches initialized
	if onReady != nil {
		onReady()
	}

	// Wait for context cancellation or fatal error
	if err := m.waitForShutdown(ctx, bufferErr); err != nil {
		return err
	}

	m.logger.Info("DataManager shutting down")

	// Wait for background tasks to complete with timeout
	m.waitForBackgroundTasks()

	// Clean up caches
	m.cleanupCaches()

	return nil
}

// initializeL2Cache creates and configures the L2 cache if enabled
func (m *Manager) initializeL2Cache(ctx context.Context) error {
	if m.config.Cache.L2Warm.Size > 0 {
		opts := []cache.Option[*gtypes.EntityState]{}
		if m.metricsRegistry != nil {
			opts = append(opts, cache.WithMetrics[*gtypes.EntityState](
				m.metricsRegistry, "datamanager_l2"))
		}
		var err error
		m.l2Cache, err = cache.NewTTL[*gtypes.EntityState](
			ctx,
			m.config.Cache.L2Warm.TTL,
			m.config.Cache.L2Warm.CleanupInterval,
			opts...)
		if err != nil {
			return errs.WrapTransient(err, "DataManager", "Run", "L2 cache creation")
		}
	}
	return nil
}

// startWorkerPoolAndBufferProcessing starts the worker pool and buffer processing goroutine
func (m *Manager) startWorkerPoolAndBufferProcessing(ctx context.Context) chan error {
	// Channel to receive fatal errors from buffer processing
	var bufferErr chan error

	// Start worker pool if batching enabled
	if m.config.BufferConfig.BatchingEnabled && m.workers != nil {
		if err := m.workers.Start(ctx); err != nil {
			// Worker pool startup failure is fatal
			m.logger.Error("Failed to start worker pool", "error", err)
			return nil
		}

		// Start buffer processing with error handling
		bufferErr = make(chan error, 1)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					m.logger.Error("Panic in buffer processing goroutine", "panic", r)
					panicErr := fmt.Errorf("panic: %v", r)
					bufferErr <- errs.WrapFatal(panicErr, "DataManager",
						"processBufferedWrites", "goroutine panicked")
				}
				close(bufferErr)
			}()
			// Only send error if it's fatal
			if err := m.processBufferedWrites(ctx); err != nil {
				bufferErr <- err
			}
		}()
	}

	return bufferErr
}

// waitForShutdown waits for context cancellation or fatal error from buffer processing
func (m *Manager) waitForShutdown(ctx context.Context, bufferErr chan error) error {
	if bufferErr != nil {
		select {
		case <-ctx.Done():
			// Normal shutdown
		case err := <-bufferErr:
			if err != nil {
				// Fatal error from buffer processing
				m.logger.Error("Fatal error in buffer processing", "error", err)
				return err // Return to stop the system
			}
		}
	} else {
		// No buffer processing, just wait for context
		<-ctx.Done()
	}
	return nil
}

// waitForBackgroundTasks waits for background tasks to complete with a timeout
func (m *Manager) waitForBackgroundTasks() {
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("Panic in shutdown wait goroutine", "panic", r)
			}
			close(done)
		}()
		m.wg.Wait()
	}()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		m.logger.Info("DataManager shutdown complete")
	case <-timer.C:
		m.logger.Warn("DataManager shutdown timeout")
	}
}

// cleanupCaches clears and closes all caches
func (m *Manager) cleanupCaches() {
	if m.l1Cache != nil {
		m.l1Cache.Clear()
		m.l1Cache.Close()
	}
	if m.l2Cache != nil {
		m.l2Cache.Clear()
		m.l2Cache.Close()
	}
}

// processBufferedWrites handles batched writes from the buffer
// Returns only fatal errors - transient errors are logged and processing continues
func (m *Manager) processBufferedWrites(ctx context.Context) error {
	ticker := time.NewTicker(m.config.BufferConfig.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush any remaining writes before shutdown
			// Log flush errors but don't fail shutdown (best effort)
			if err := m.flushBufferWithContext(ctx); err != nil {
				m.logger.Warn("failed to flush buffer during shutdown", "error", err)
			}
			return nil // Clean shutdown is not an error
		case <-ticker.C:
			// Flush buffer periodically
			if err := m.flushBufferWithContext(ctx); err != nil {
				// Check if error is fatal
				if errs.IsFatal(err) {
					return err // Return fatal errors to stop the system
				}
				// Transient error - log and continue
				m.logger.Warn("Transient flush error, continuing",
					"error", err,
					"retry_in", m.config.BufferConfig.FlushInterval)
			}
		}
	}
}

// flushBuffer processes pending writes from the buffer with coalescing
// func (m *Manager) flushBuffer() error {
// 	return m.flushBufferWithContext(context.Background())
// }

// flushBufferWithContext processes pending writes from the buffer
// Returns error only if fatal - transient submit errors are handled internally
func (m *Manager) flushBufferWithContext(ctx context.Context) error {
	if m.writeBuffer == nil || m.workers == nil {
		return nil
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil // Context cancellation is not an error
	default:
	}

	// Record queue depth before reading
	if m.metrics != nil {
		m.metrics.queueDepth.Set(float64(m.writeBuffer.Size()))
	}

	// Read batch from buffer
	batch := m.writeBuffer.ReadBatch(m.config.BufferConfig.MaxBatchSize)
	if len(batch) == 0 {
		return nil
	}

	// Record batch size
	if m.metrics != nil {
		m.metrics.batchSize.Observe(float64(len(batch)))
	}

	// Submit writes to workers
	var submitErrors int
	for _, write := range batch {
		// Check context before each submit
		select {
		case <-ctx.Done():
			// Context cancelled, stop submitting
			if write.Callback != nil {
				write.Callback(errs.Wrap(ctx.Err(), "DataManager", "flushBufferWithContext", "context cancelled"))
			}
			return nil // Clean shutdown
		default:
		}

		if err := m.workers.Submit(write); err != nil {
			// Check error type to determine if it's fatal or transient
			if stderrors.Is(err, worker.ErrPoolStopped) || stderrors.Is(err, worker.ErrPoolNotStarted) {
				// Pool stopped unexpectedly - this is fatal
				return errs.WrapFatal(err, "DataManager", "flushBufferWithContext", "worker pool no longer running")
			}
			if stderrors.Is(err, worker.ErrQueueFull) {
				// Queue full is transient - notify callback and continue
				// CRITICAL: Track dropped writes for diagnosing data loss
				if m.metrics != nil {
					m.metrics.droppedWrites.Inc()
				}
				m.logger.Warn("Write dropped due to queue full",
					"entity_id", write.Entity.ID,
					"operation", write.Operation)
				if write.Callback != nil {
					queueErr := errs.WrapTransient(err, "DataManager",
						"flushBufferWithContext", "worker queue full")
					write.Callback(queueErr)
				}
				submitErrors++
				continue
			}
			// Unknown error - treat as fatal
			return errs.WrapFatal(err, "DataManager", "flushBufferWithContext", "unexpected worker pool error")
		}
	}

	// If ALL submits failed, might indicate a problem
	if submitErrors > 0 && submitErrors == len(batch) {
		m.logger.Warn("All writes in batch failed to submit",
			"batch_size", len(batch),
			"errors", submitErrors)
		// Still transient - queue is just full
	}

	return nil
}

// processWrite processes a single write operation (called by worker pool)
func (m *Manager) processWrite(ctx context.Context, write *EntityWrite) error {
	switch write.Operation {
	case OperationCreate:
		_, err := m.createEntityDirect(ctx, write.Entity)
		if write.Callback != nil {
			write.Callback(err)
		}
		return err

	case OperationUpdate:
		_, err := m.updateEntityDirect(ctx, write.Entity, write.Strategy)
		if write.Callback != nil {
			write.Callback(err)
		}
		return err

	case OperationDelete:
		var err error
		if write.Entity != nil {
			err = m.deleteEntityDirect(ctx, write.Entity.ID)
		}
		if write.Callback != nil {
			write.Callback(err)
		}
		return err

	default:
		msg := fmt.Sprintf("invalid operation: %s", write.Operation)
		err := errs.WrapInvalid(nil, "DataManager", "processWrite", msg)
		if write.Callback != nil {
			write.Callback(err)
		}
		return err
	}
}

// Entity CRUD Operations

// CreateEntity creates a new entity in the store
func (m *Manager) CreateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	// Validate entity before processing
	if err := validateEntity(entity); err != nil {
		return nil, err
	}

	// Use buffered write if available
	if m.writeBuffer != nil && m.config.BufferConfig.BatchingEnabled {
		write := &EntityWrite{
			Operation: OperationCreate,
			Entity:    entity,
			Timestamp: time.Now(),
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return nil, errs.Wrap(err, "DataManager", "CreateEntity", "buffer write")
		}

		// For creates, we need to wait synchronously
		// In production, this would use a result channel
		return entity, nil
	}

	// Direct write
	return m.createEntityDirect(ctx, entity)
}

// createEntityDirect performs immediate entity creation
func (m *Manager) createEntityDirect(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	startTime := time.Now()

	if entity == nil {
		err := errs.WrapInvalid(nil, "DataManager", "createEntityDirect", "entity cannot be nil")
		return nil, err
	}

	if entity.ID == "" {
		err := errs.WrapInvalid(nil, "DataManager", "createEntityDirect", "entity ID cannot be empty")
		return nil, err
	}

	// Deep copy entity to avoid race conditions when workers process concurrent writes
	entity = entity.Clone()

	// Set version and timestamps
	entity.Version = 1
	entity.UpdatedAt = time.Now()

	// Serialize entity
	data, err := json.Marshal(entity)
	if err != nil {
		err = errs.Wrap(err, "DataManager", "createEntityDirect", "marshal entity")
		return nil, err
	}

	// Create in KV bucket
	if _, err := m.kvBucket.Create(ctx, entity.ID, data); err != nil {
		// Record write failure
		if m.metrics != nil {
			m.metrics.writesTotal.WithLabelValues("failure", "create").Inc()
			m.metrics.writeLatency.WithLabelValues("create").Observe(time.Since(startTime).Seconds())
		}
		if err == jetstream.ErrKeyExists {
			m.logger.Debug("Create failed: entity already exists",
				"entity_id", entity.ID)
			err = errs.WrapInvalid(err, "DataManager", "createEntityDirect", "entity already exists")
		} else {
			m.logger.Warn("Create failed: KV error",
				"entity_id", entity.ID,
				"error", err)
			err = errs.Wrap(err, "DataManager", "createEntityDirect", "KV create")
		}
		return nil, err
	}

	// Record write success
	if m.metrics != nil {
		m.metrics.writesTotal.WithLabelValues("success", "create").Inc()
		m.metrics.writeLatency.WithLabelValues("create").Observe(time.Since(startTime).Seconds())
		m.metrics.entitiesCreated.Inc()
	}

	// Update caches
	m.updateCaches(entity)

	// Notify callback of new entity creation (non-blocking)
	m.callbackMu.RLock()
	cb := m.onEntityCreated
	m.callbackMu.RUnlock()
	if cb != nil {
		cb(entity.ID)
	}

	return entity, nil
}

// UpdateEntity updates an existing entity
func (m *Manager) UpdateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	// Validate entity before processing
	if err := validateEntity(entity); err != nil {
		return nil, err
	}

	// Use buffered write if available
	if m.writeBuffer != nil && m.config.BufferConfig.BatchingEnabled {
		write := &EntityWrite{
			Operation: OperationUpdate,
			Entity:    entity,
			Timestamp: time.Now(),
			Strategy:  WriteStrategyPut, // Async buffered writes use Put (last-write-wins)
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return nil, errs.Wrap(err, "DataManager", "UpdateEntity", "buffer write")
		}

		// For updates, we need to wait synchronously
		// In production, this would use a result channel
		return entity, nil
	}

	// Direct sync write uses CAS (caller can handle version conflicts)
	return m.updateEntityDirect(ctx, entity, WriteStrategyCAS)
}

// UpsertEntity atomically creates or updates an entity using Put semantics.
// This is the preferred method for streaming data where idempotency is required.
// Unlike the GetEntity → Create/Update pattern, this avoids TOCTOU race conditions.
func (m *Manager) UpsertEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	// Validate entity before processing
	if err := validateEntity(entity); err != nil {
		return nil, err
	}

	// Use buffered write if available
	if m.writeBuffer != nil && m.config.BufferConfig.BatchingEnabled {
		write := &EntityWrite{
			Operation: OperationUpdate, // Update with Put strategy is effectively upsert
			Entity:    entity,
			Timestamp: time.Now(),
			Strategy:  WriteStrategyPut, // Put() creates if not exists, updates if exists
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return nil, errs.Wrap(err, "DataManager", "UpsertEntity", "buffer write")
		}

		return entity, nil
	}

	// Direct write using Put (upsert semantics)
	return m.updateEntityPut(ctx, entity)
}

// updateEntityDirect performs immediate entity update using the specified strategy
func (m *Manager) updateEntityDirect(ctx context.Context, entity *gtypes.EntityState, strategy WriteStrategy) (*gtypes.EntityState, error) {
	if entity == nil {
		return nil, errs.WrapInvalid(nil, "DataManager", "updateEntityDirect", "entity cannot be nil")
	}

	if strategy == WriteStrategyPut {
		return m.updateEntityPut(ctx, entity)
	}
	return m.updateEntityCAS(ctx, entity)
}

// updateEntityPut performs entity update using Put (last-write-wins, no CAS)
// Best for async streaming data where concurrent writes are expected
func (m *Manager) updateEntityPut(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	startTime := time.Now()

	// Deep copy entity to avoid race conditions when workers process concurrent writes
	entity = entity.Clone()

	// Update timestamp (version not meaningful for Put)
	entity.UpdatedAt = time.Now()

	// Serialize entity
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, errs.Wrap(err, "DataManager", "updateEntityPut", "marshal entity")
	}

	// Put() overwrites regardless of version - no CAS conflict possible
	if _, err := m.kvBucket.Put(ctx, entity.ID, data); err != nil {
		if m.metrics != nil {
			m.metrics.writesTotal.WithLabelValues("failure", "update").Inc()
			m.metrics.writeLatency.WithLabelValues("update").Observe(time.Since(startTime).Seconds())
		}
		m.logger.Warn("Put update failed",
			"entity_id", entity.ID,
			"error", err)
		return nil, errs.Wrap(err, "DataManager", "updateEntityPut", "KV put")
	}

	// Record write success
	if m.metrics != nil {
		m.metrics.writesTotal.WithLabelValues("success", "update").Inc()
		m.metrics.writeLatency.WithLabelValues("update").Observe(time.Since(startTime).Seconds())
		m.metrics.entitiesUpdated.Inc()
	}

	// Update caches
	m.updateCaches(entity)

	return entity, nil
}

// updateEntityCAS performs entity update with Compare-And-Swap semantics
// Best for synchronous mutations where caller can handle version conflicts
func (m *Manager) updateEntityCAS(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	startTime := time.Now()

	// Deep copy entity to avoid race conditions when workers process concurrent writes
	entity = entity.Clone()

	// Retry logic for CAS operations
	retryConfig := retry.DefaultConfig()
	retryConfig.MaxAttempts = m.config.MaxRetries

	var updatedEntity *gtypes.EntityState
	var retryCount int
	err := retry.Do(ctx, retryConfig, func() error {
		// Get current version
		current, err := m.GetEntity(ctx, entity.ID)
		if err != nil {
			return errs.Wrap(err, "DataManager", "updateEntityCAS", "get current version")
		}

		// Update version and timestamp
		entity.Version = current.Version + 1
		entity.UpdatedAt = time.Now()

		// Serialize entity
		data, err := json.Marshal(entity)
		if err != nil {
			return errs.Wrap(err, "DataManager", "updateEntityCAS", "marshal entity")
		}

		// CAS update
		if _, err := m.kvBucket.Update(ctx, entity.ID, data, uint64(current.Version)); err != nil {
			if err == jetstream.ErrKeyNotFound {
				return errs.WrapInvalid(err, "DataManager", "updateEntityCAS", "entity not found")
			}
			// CAS conflict is transient, will retry
			retryCount++
			if m.metrics != nil {
				m.metrics.casRetries.Inc()
			}
			m.logger.Debug("CAS conflict, retrying",
				"entity_id", entity.ID,
				"version", current.Version,
				"retry_count", retryCount)
			return errs.WrapTransient(err, "DataManager", "updateEntityCAS", "CAS conflict")
		}

		updatedEntity = entity
		return nil
	})

	if err != nil {
		// Record write failure after all retries exhausted
		if m.metrics != nil {
			m.metrics.writesTotal.WithLabelValues("failure", "update").Inc()
			m.metrics.writeLatency.WithLabelValues("update").Observe(time.Since(startTime).Seconds())
		}
		m.logger.Warn("CAS update failed after retries",
			"entity_id", entity.ID,
			"retries", retryCount,
			"error", err)
		return nil, err
	}

	// Record write success
	if m.metrics != nil {
		m.metrics.writesTotal.WithLabelValues("success", "update").Inc()
		m.metrics.writeLatency.WithLabelValues("update").Observe(time.Since(startTime).Seconds())
		m.metrics.entitiesUpdated.Inc()
	}

	// Update caches
	m.updateCaches(updatedEntity)

	return updatedEntity, nil
}

// DeleteEntity deletes an entity from the store
func (m *Manager) DeleteEntity(ctx context.Context, id string) error {
	// Validate entity ID before processing
	if err := validateEntityID(id); err != nil {
		return err
	}

	// Use buffered write if available
	if m.writeBuffer != nil && m.config.BufferConfig.BatchingEnabled {
		write := &EntityWrite{
			Operation: OperationDelete,
			Entity:    &gtypes.EntityState{ID: id},
			Timestamp: time.Now(),
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return errs.Wrap(err, "DataManager", "DeleteEntity", "buffer write")
		}

		return nil
	}

	// Direct delete
	return m.deleteEntityDirect(ctx, id)
}

// deleteEntityDirect performs immediate entity deletion
func (m *Manager) deleteEntityDirect(ctx context.Context, id string) error {
	startTime := time.Now()

	// Get entity for cleanup
	entity, err := m.GetEntity(ctx, id)
	if err != nil {
		err = errs.Wrap(err, "DataManager", "deleteEntityDirect", "get entity for cleanup")
		return err
	}

	// Cleanup incoming references if entity has relationship triples
	if len(entity.Triples) > 0 {
		if err := m.CleanupIncomingReferences(ctx, id, entity.Triples); err != nil {
			m.logger.Warn("Failed to cleanup incoming references", "entity", id, "error", err)
			// Don't fail the delete operation for cleanup errors
		}
	}

	// Delete from KV
	if err := m.kvBucket.Delete(ctx, id); err != nil {
		// Record delete failure
		if m.metrics != nil {
			m.metrics.writesTotal.WithLabelValues("failure", "delete").Inc()
			m.metrics.writeLatency.WithLabelValues("delete").Observe(time.Since(startTime).Seconds())
		}
		if err == jetstream.ErrKeyNotFound {
			m.logger.Debug("Delete failed: entity not found", "entity_id", id)
			err = errs.WrapInvalid(err, "DataManager", "deleteEntityDirect", "entity not found")
		} else {
			m.logger.Warn("Delete failed: KV error", "entity_id", id, "error", err)
			err = errs.Wrap(err, "DataManager", "deleteEntityDirect", "KV delete")
		}
		return err
	}

	// Record delete success
	if m.metrics != nil {
		m.metrics.writesTotal.WithLabelValues("success", "delete").Inc()
		m.metrics.writeLatency.WithLabelValues("delete").Observe(time.Since(startTime).Seconds())
		m.metrics.entitiesDeleted.Inc()
	}

	// Invalidate caches
	m.invalidateCaches(id)

	return nil
}

// GetEntity retrieves an entity from the store
func (m *Manager) GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error) {
	// Check L1 cache
	if m.l1Cache != nil {
		if entity, found := m.l1Cache.Get(id); found {
			if m.metrics != nil {
				m.metrics.cacheHits.WithLabelValues("l1").Inc()
			}
			return entity, nil
		}
	}

	// Check L2 cache
	if m.l2Cache != nil {
		if entity, found := m.l2Cache.Get(id); found {
			if m.metrics != nil {
				m.metrics.cacheHits.WithLabelValues("l2").Inc()
			}
			// Promote to L1
			if m.l1Cache != nil {
				m.l1Cache.Set(id, entity)
			}
			return entity, nil
		}
	}

	// Cache miss - must fetch from KV
	if m.metrics != nil {
		m.metrics.cacheMisses.Inc()
	}

	// Fetch from KV
	entry, err := m.kvBucket.Get(ctx, id)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			// Not found is often expected, don't record as error
			return nil, errs.WrapInvalid(err, "DataManager", "GetEntity", "entity not found")
		}
		// Record actual KV errors
		err = errs.Wrap(err, "DataManager", "GetEntity", "KV get")
		return nil, err
	}

	// Deserialize entity
	var entity gtypes.EntityState
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		err = errs.Wrap(err, "DataManager", "GetEntity", "unmarshal entity")
		return nil, err
	}

	// Update caches
	m.updateCaches(&entity)

	return &entity, nil
}

// ExistsEntity checks if an entity exists
func (m *Manager) ExistsEntity(ctx context.Context, id string) (bool, error) {

	_, err := m.kvBucket.Get(ctx, id)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return false, nil
		}
		return false, errs.Wrap(err, "DataManager", "ExistsEntity", "KV get")
	}
	return true, nil
}

// Atomic Entity+Triple Operations

// CreateEntityWithTriples creates an entity with triples atomically
func (m *Manager) CreateEntityWithTriples(
	ctx context.Context,
	entity *gtypes.EntityState,
	triples []message.Triple,
) (*gtypes.EntityState, error) {
	if entity == nil {
		return nil, errs.WrapInvalid(nil, "DataManager",
			"CreateEntityWithTriples", "entity cannot be nil")
	}

	// Add triples to entity before creating
	entity.Triples = append(entity.Triples, triples...)

	// Create entity with triples
	return m.CreateEntity(ctx, entity)
}

// UpdateEntityWithTriples updates an entity and modifies triples atomically
func (m *Manager) UpdateEntityWithTriples(
	ctx context.Context,
	entity *gtypes.EntityState,
	addTriples []message.Triple,
	removePredicates []string,
) (*gtypes.EntityState, error) {
	if entity == nil {
		return nil, errs.WrapInvalid(nil, "DataManager",
			"UpdateEntityWithTriples", "entity cannot be nil")
	}

	// Get current entity state
	current, err := m.GetEntity(ctx, entity.ID)
	if err != nil {
		return nil, err
	}

	// Start with current triples
	entity.Triples = current.Triples

	// Remove triples by predicate
	newTriples := []message.Triple{}
	for _, triple := range entity.Triples {
		shouldRemove := false
		for _, predicate := range removePredicates {
			if triple.Predicate == predicate {
				shouldRemove = true
				break
			}
		}
		if !shouldRemove {
			newTriples = append(newTriples, triple)
		}
	}
	entity.Triples = newTriples

	// Add new triples (using MergeTriples to handle duplicates)
	entity.Triples = gtypes.MergeTriples(entity.Triples, addTriples)

	// Update entity with modified triples
	return m.UpdateEntity(ctx, entity)
}

// Additional required methods...

// Helper methods

// updateCaches updates both L1 and L2 caches
func (m *Manager) updateCaches(entity *gtypes.EntityState) {
	if m.l1Cache != nil {
		m.l1Cache.Set(entity.ID, entity)
	}
	if m.l2Cache != nil {
		m.l2Cache.Set(entity.ID, entity)
	}
}

// invalidateCaches removes entity from both caches
func (m *Manager) invalidateCaches(id string) {
	if m.l1Cache != nil {
		m.l1Cache.Delete(id)
	}
	if m.l2Cache != nil {
		m.l2Cache.Delete(id)
	}
}

// FlushPendingWrites forces all buffered writes to be processed immediately.
// This is primarily for testing and graceful shutdown scenarios.
func (m *Manager) FlushPendingWrites(ctx context.Context) error {
	if !m.config.BufferConfig.BatchingEnabled {
		return nil // Nothing to flush if batching is disabled
	}

	// Send a flush signal and wait for completion
	return m.flushBufferWithContext(ctx)
}

// GetPendingWriteCount returns the number of writes currently in the buffer.
// This is useful for monitoring and testing async operations.
func (m *Manager) GetPendingWriteCount() int {
	if !m.config.BufferConfig.BatchingEnabled || m.writeBuffer == nil {
		return 0
	}

	// Get the current buffer size
	// The buffer tracks pending items internally
	return m.writeBuffer.Size()
}

// SetEntityCreatedCallback sets a callback function that is invoked when a new entity is created.
// This is used by the graph processor to track entity changes for adaptive clustering.
// The callback is invoked synchronously, so it should be non-blocking.
func (m *Manager) SetEntityCreatedCallback(callback EntityCreatedCallback) {
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	m.onEntityCreated = callback
}
