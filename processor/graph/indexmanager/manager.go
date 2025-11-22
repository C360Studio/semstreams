// Package indexmanager provides index management for entity state tracking and queries.
package indexmanager

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/buffer"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/pkg/embedding"
	"github.com/c360/semstreams/pkg/worker"
)

// Manager implements the Indexer interface
type Manager struct {
	mu sync.RWMutex

	// Configuration
	config Config

	// NATS Client (for KVStore)
	natsClient *natsclient.Client

	// KV Buckets
	entityBucket    jetstream.KeyValue
	predicateBucket jetstream.KeyValue
	incomingBucket  jetstream.KeyValue
	aliasBucket     jetstream.KeyValue
	spatialBucket   jetstream.KeyValue
	temporalBucket  jetstream.KeyValue

	// Core components
	watcher     *KVWatcher
	eventBuffer buffer.Buffer[EntityChange]
	dedupCache  cache.Cache[bool]
	workers     *worker.Pool[func(context.Context)]
	indexes     map[string]Index

	// Simple LRU caches to reduce KV hits (one per index)
	predicateCache cache.Cache[[]string]       // Cache for predicate lookups
	spatialCache   cache.Cache[[]string]       // Cache for spatial lookups
	temporalCache  cache.Cache[[]string]       // Cache for temporal lookups
	incomingCache  cache.Cache[[]Relationship] // Cache for incoming relationships
	aliasCache     cache.Cache[string]         // Cache for alias resolution

	// Semantic search components (optional - nil when disabled)
	embedder         Embedder                     // Embedding generator (nil if disabled)
	vectorCache      cache.Cache[[]float32]       // TTL cache: entityID -> embedding vector (read hot-path)
	metadataCache    cache.Cache[*EntityMetadata] // TTL cache: entityID -> metadata
	embeddingStorage *embedding.Storage           // Persistent KV storage for embeddings
	embeddingWorker  *embedding.Worker            // Async worker for embedding generation

	// Event processing
	eventChan chan EntityChange
	batchChan chan []EntityChange

	// State
	wg sync.WaitGroup

	// Metrics
	metrics         *InternalMetrics
	promMetrics     *PrometheusMetrics
	metricsRegistry *metric.MetricsRegistry

	// Logger
	logger *slog.Logger
}

// Embedder interface from embedding package (re-declared here to avoid import cycle)
type Embedder interface {
	Generate(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	Model() string
	Close() error
}

// EntityMetadata stores entity information needed for semantic search
type EntityMetadata struct {
	EntityID   string
	EntityType string
	Properties map[string]interface{}
	Updated    time.Time
}

// NewManager creates a new Indexer implementation
func NewManager(
	config Config,
	buckets map[string]jetstream.KeyValue,
	natsClient *natsclient.Client,
	metricsRegistry *metric.MetricsRegistry,
	logger *slog.Logger,
) (Indexer, error) {
	// Ensure we have a logger
	if logger == nil {
		logger = slog.Default()
	}
	// Apply defaults and validate config
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errors.WrapInvalid(err, "IndexManager", "NewManager", "configuration validation failed")
	}

	// Create metrics
	metrics := NewInternalMetrics()
	var promMetrics *PrometheusMetrics
	if config.EventBuffer.Metrics && metricsRegistry != nil {
		promMetrics = NewPrometheusMetrics("indexengine", metricsRegistry)
	}

	// Create event buffer
	var eventBuffer buffer.Buffer[EntityChange]
	var err error
	if config.EventBuffer.OverflowPolicy == "drop_oldest" {
		eventBuffer, err = buffer.NewCircularBuffer[EntityChange](
			config.EventBuffer.Capacity,
			buffer.WithOverflowPolicy[EntityChange](buffer.DropOldest),
		)
	} else if config.EventBuffer.OverflowPolicy == "drop_newest" {
		eventBuffer, err = buffer.NewCircularBuffer[EntityChange](
			config.EventBuffer.Capacity,
			buffer.WithOverflowPolicy[EntityChange](buffer.DropNewest),
		)
	} else {
		eventBuffer, err = buffer.NewCircularBuffer[EntityChange](
			config.EventBuffer.Capacity,
			buffer.WithOverflowPolicy[EntityChange](buffer.Block),
		)
	}
	if err != nil {
		return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "event buffer creation")
	}

	// Create event channels
	eventChan := make(chan EntityChange, config.BatchProcessing.Size)
	batchChan := make(chan []EntityChange, 10) // Small buffer for batches

	// Create KV watcher
	entityBucket, ok := buckets[config.Buckets.EntityStates]
	if !ok {
		msg := fmt.Sprintf("bucket not found: %s", config.Buckets.EntityStates)
		return nil, errors.WrapInvalid(ErrBucketMissing, "IndexManager", "NewManager", msg)
	}
	watcher := NewKVWatcher(
		entityBucket, config.Buckets.EntityStates, eventChan, metrics, promMetrics, logger)

	// Create worker pool for processing
	workers := worker.NewPool[func(context.Context)](
		config.Workers, config.BatchProcessing.Size*2,
		func(ctx context.Context, task func(context.Context)) error {
			task(ctx)
			return nil
		})

	// Create LRU caches if enabled
	var predicateCache cache.Cache[[]string]
	var spatialCache cache.Cache[[]string]
	var temporalCache cache.Cache[[]string]
	var incomingCache cache.Cache[[]Relationship]
	var aliasCache cache.Cache[string]

	if config.Caches.Enabled {
		predicateCache, err = cache.NewLRU[[]string](config.Caches.PredicateSize)
		if err != nil {
			return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "predicate cache creation")
		}
		spatialCache, err = cache.NewLRU[[]string](config.Caches.SpatialSize)
		if err != nil {
			return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "spatial cache creation")
		}
		temporalCache, err = cache.NewLRU[[]string](config.Caches.TemporalSize)
		if err != nil {
			return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "temporal cache creation")
		}
		incomingCache, err = cache.NewLRU[[]Relationship](config.Caches.IncomingSize)
		if err != nil {
			return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "incoming cache creation")
		}
		aliasCache, err = cache.NewLRU[string](config.Caches.AliasSize)
		if err != nil {
			return nil, errors.WrapTransient(err, "IndexManager", "NewManager", "alias cache creation")
		}
	}

	engine := &Manager{
		config:          config,
		natsClient:      natsClient,
		entityBucket:    entityBucket,
		predicateBucket: buckets[config.Buckets.Predicate],
		incomingBucket:  buckets[config.Buckets.Incoming],
		aliasBucket:     buckets[config.Buckets.Alias],
		spatialBucket:   buckets[config.Buckets.Spatial],
		temporalBucket:  buckets[config.Buckets.Temporal],
		watcher:         watcher,
		eventBuffer:     eventBuffer,
		workers:         workers,
		eventChan:       eventChan,
		batchChan:       batchChan,
		predicateCache:  predicateCache,
		spatialCache:    spatialCache,
		temporalCache:   temporalCache,
		incomingCache:   incomingCache,
		aliasCache:      aliasCache,
		metrics:         metrics,
		promMetrics:     promMetrics,
		metricsRegistry: metricsRegistry,
		indexes:         make(map[string]Index),
		logger:          logger,
	}

	// Initialize enabled indexes
	if err := engine.initializeIndexes(); err != nil {
		return nil, errors.WrapInvalid(err, "IndexManager", "NewManager", "failed to initialize indexes")
	}

	// Initialize semantic search if enabled
	if err := engine.initializeSemanticSearch(buckets); err != nil {
		return nil, errors.WrapInvalid(err, "IndexManager", "NewManager", "failed to initialize semantic search")
	}

	return engine, nil
}

// Run starts KV watching and event processing. It blocks until error or context done and returns only fatal errors.
func (m *Manager) Run(ctx context.Context) error {
	// Create deduplication cache if enabled
	if err := m.createDedupCache(ctx); err != nil {
		return err
	}

	// Start worker pool - startup failure is fatal
	if err := m.workers.Start(ctx); err != nil {
		return errors.WrapFatal(err, "IndexManager", "Run", "worker_pool_start")
	}
	defer func() {
		if err := m.workers.Stop(5 * time.Second); err != nil {
			m.logger.Error("Failed to stop worker pool", "error", err)
		}
	}()

	// Start KV watcher - startup failure is fatal
	if err := m.watcher.Start(ctx); err != nil {
		return errors.WrapFatal(err, "IndexManager", "Run", "watcher_start")
	}
	defer func() {
		if err := m.watcher.Stop(); err != nil {
			m.logger.Warn("Watcher stop error", "error", err)
		}
	}()

	// Start embedding worker if configured
	if m.embeddingWorker != nil {
		if err := m.embeddingWorker.Start(ctx); err != nil {
			m.logger.Warn("Embedding worker start failed - continuing without async embeddings", "error", err)
			// Don't fail - async embeddings are optional
		} else {
			defer func() {
				if err := m.embeddingWorker.Stop(); err != nil {
					m.logger.Warn("Embedding worker stop error", "error", err)
				}
			}()
			m.logger.Info("Embedding worker started")
		}
	}

	// Start event and batch processing goroutines
	eventErr, batchErr := m.startProcessingGoroutines(ctx)

	m.logger.Info("IndexManager running", "workers", m.config.Workers)

	// Wait for context cancellation or fatal error
	if err := m.waitForShutdownSignal(ctx, eventErr, batchErr); err != nil {
		return err
	}

	// Perform graceful shutdown
	m.performGracefulShutdown()

	m.logger.Info("IndexManager stopped")
	return nil
}

// createDedupCache creates the deduplication cache if enabled
func (m *Manager) createDedupCache(ctx context.Context) error {
	if m.config.Deduplication.Enabled {
		var err error
		m.dedupCache, err = cache.NewTTL[bool](
			ctx,
			m.config.Deduplication.TTL,
			m.config.Deduplication.TTL/10,
		)
		if err != nil {
			return errors.WrapTransient(err, "IndexManager", "Run", "dedup cache creation")
		}
	}
	return nil
}

// startProcessingGoroutines starts event and batch processing goroutines with error handling
func (m *Manager) startProcessingGoroutines(ctx context.Context) (chan error, chan error) {
	// Channels for fatal errors from processors
	eventErr := make(chan error, 1)
	batchErr := make(chan error, 1)

	// Start event processing goroutines with error handling
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				// Panic in event processor is fatal
				eventErr <- errors.WrapFatal(fmt.Errorf("panic: %v", r), "IndexManager", "Run", "event_processor_panic")
			}
			close(eventErr)
		}()
		// Send error only if it's fatal
		if err := m.processEvents(ctx); err != nil {
			eventErr <- err
		}
	}()
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				// Panic in batch processor is fatal
				batchErr <- errors.WrapFatal(fmt.Errorf("panic: %v", r), "IndexManager", "Run", "batch_processor_panic")
			}
			close(batchErr)
		}()
		// Send error only if it's fatal
		if err := m.processBatches(ctx); err != nil {
			batchErr <- err
		}
	}()

	return eventErr, batchErr
}

// waitForShutdownSignal waits for context cancellation or fatal error from processors
func (m *Manager) waitForShutdownSignal(ctx context.Context, eventErr, batchErr chan error) error {
	select {
	case <-ctx.Done():
		// Normal shutdown
	case err := <-eventErr:
		if err != nil {
			m.logger.Error("Fatal error in event processor", "error", err)
			return err
		}
	case err := <-batchErr:
		if err != nil {
			m.logger.Error("Fatal error in batch processor", "error", err)
			return err
		}
	}
	return nil
}

// performGracefulShutdown stops the watcher, waits for goroutines, and cleans up resources
func (m *Manager) performGracefulShutdown() {
	// Stop the watcher first to prevent new events
	if m.watcher != nil {
		if err := m.watcher.Stop(); err != nil {
			m.logger.Warn("KV watcher stop error", "error", err)
		}
		m.logger.Info("KV Watcher stopped", "bucket", "ENTITY_STATES")
	}

	// Wait for goroutines to finish processing BEFORE closing channels
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		// Goroutines finished, now safe to close channels
		m.logger.Debug("All goroutines finished, closing channels")
	case <-time.After(5 * time.Second):
		m.logger.Warn("Shutdown timeout exceeded, forcing channel closure")
	}

	// Now safe to close channels - all goroutines have exited
	close(m.eventChan)
	close(m.batchChan)

	// Close caches
	if m.dedupCache != nil {
		m.dedupCache.Close()
	}
}

// initializeIndexes creates and initializes all enabled indexes
func (m *Manager) initializeIndexes() error {
	if m.config.Indexes.Predicate && m.predicateBucket != nil {
		index := NewPredicateIndex(m.predicateBucket, m.natsClient, m.metrics, m.promMetrics, m.logger)
		m.indexes["predicate"] = index
	}

	if m.config.Indexes.Incoming && m.incomingBucket != nil {
		index := NewIncomingIndex(m.incomingBucket, m.metrics, m.promMetrics, m.logger)
		m.indexes["incoming"] = index
	}

	if m.config.Indexes.Alias && m.aliasBucket != nil {
		index := NewAliasIndex(m.aliasBucket, m.natsClient, m.metrics, m.promMetrics, m.logger)
		m.indexes["alias"] = index
	}

	if m.config.Indexes.Spatial && m.spatialBucket != nil {
		index := NewSpatialIndex(m.spatialBucket, m.natsClient, m.metrics, m.promMetrics, m.logger)
		m.indexes["spatial"] = index
	}

	if m.config.Indexes.Temporal && m.temporalBucket != nil {
		index := NewTemporalIndex(m.temporalBucket, m.natsClient, m.metrics, m.promMetrics, m.logger)
		m.indexes["temporal"] = index
	}

	m.logger.Info("IndexManager initialized indexes", "count", len(m.indexes), "enabled", m.config.GetEnabledIndexes())
	return nil
}

// processEvents collects events into batches for processing
// Returns only fatal errors - transient errors are logged and processing continues
func (m *Manager) processEvents(ctx context.Context) error {
	batch := make([]EntityChange, 0, m.config.BatchProcessing.Size)
	ticker := time.NewTicker(m.config.BatchProcessing.Interval)
	defer ticker.Stop()

	// Track consecutive batch drops to detect systemic issues
	var consecutiveDrops int
	const maxConsecutiveDrops = 10

	for {
		select {
		case <-ctx.Done():
			// Process any remaining batch
			if len(batch) > 0 {
				// Best effort on shutdown, ignore errors
				_ = m.submitBatch(ctx, batch)
			}
			return nil

		case event, ok := <-m.eventChan:
			if !ok {
				return nil // Channel closed
			}

			// Add to batch
			batch = append(batch, event)

			// Process batch if full
			if len(batch) >= m.config.BatchProcessing.Size {
				if err := m.submitBatch(ctx, batch); err != nil {
					if errors.IsFatal(err) {
						return err
					}
					// Batch was dropped due to backpressure
					consecutiveDrops++
					m.logger.Debug("Batch dropped due to backpressure",
						"error", err,
						"consecutive_drops", consecutiveDrops)

					// Too many consecutive drops indicates systemic problem
					if consecutiveDrops >= maxConsecutiveDrops {
						return errors.WrapFatal(
							fmt.Errorf("batch channel persistently full: %d consecutive drops", consecutiveDrops),
							"IndexManager", "processEvents", "backpressure_failure")
					}
				} else {
					// Reset counter on success
					consecutiveDrops = 0
				}
				batch = batch[:0] // Reset slice
			}

		case <-ticker.C:
			// Process batch on timer
			if len(batch) > 0 {
				if err := m.submitBatch(ctx, batch); err != nil {
					if errors.IsFatal(err) {
						return err
					}
					// Batch was dropped due to backpressure
					consecutiveDrops++
					m.logger.Debug("Batch dropped due to backpressure on timer",
						"error", err,
						"consecutive_drops", consecutiveDrops)

					// Too many consecutive drops indicates systemic problem
					if consecutiveDrops >= maxConsecutiveDrops {
						return errors.WrapFatal(
							fmt.Errorf("batch channel persistently full: %d consecutive drops", consecutiveDrops),
							"IndexManager", "processEvents", "backpressure_failure")
					}
				} else {
					// Reset counter on success
					consecutiveDrops = 0
				}
				batch = batch[:0] // Reset slice
			}
		}
	}
}

// submitBatch submits a batch of events for processing
// Returns error if batch cannot be submitted
func (m *Manager) submitBatch(ctx context.Context, batch []EntityChange) error {
	// Copy batch to avoid race conditions
	batchCopy := make([]EntityChange, len(batch))
	copy(batchCopy, batch)

	select {
	case m.batchChan <- batchCopy:
		// Successfully submitted
		return nil
	case <-ctx.Done():
		// Context cancelled, clean shutdown
		return nil
	default:
		// Batch channel full - this is a backpressure issue
		m.logger.Warn("Batch channel full, dropping batch", "events", len(batch))
		m.metrics.UpdateBacklogSize(m.metrics.backlogSize + len(batch))

		// Return transient error so caller knows the batch was dropped
		return errors.WrapTransient(
			fmt.Errorf("batch channel full, dropped %d events", len(batch)),
			"IndexManager", "submitBatch", "channel_full")
	}
}

// processBatches processes batches of events using worker pool
// Returns only fatal errors - transient errors are logged and processing continues
func (m *Manager) processBatches(ctx context.Context) error {
	// Counter for consecutive submit failures
	var submitFailures int
	const maxConsecutiveFailures = 10

	for {
		select {
		case <-ctx.Done():
			return nil // Clean shutdown

		case batch, ok := <-m.batchChan:
			if !ok {
				return nil // Channel closed
			}

			// Submit batch to worker pool
			batchCopy := batch // capture batch for closure
			err := m.workers.Submit(func(ctx context.Context) {
				m.processBatch(ctx, batchCopy)
			})

			if err != nil {
				// Check error type to determine severity
				if stderrors.Is(err, worker.ErrPoolStopped) || stderrors.Is(err, worker.ErrPoolNotStarted) {
					// Worker pool stopped unexpectedly - this is fatal
					return errors.WrapFatal(err, "IndexManager", "processBatches", "worker pool no longer running")
				}
				if stderrors.Is(err, worker.ErrQueueFull) {
					// Queue full is transient - log and continue
					submitFailures++
					m.logger.Warn("Worker pool queue full, batch dropped",
						"error", err,
						"batch_size", len(batch),
						"consecutive_failures", submitFailures)
				}
				// Too many consecutive failures might indicate a problem
				if submitFailures >= maxConsecutiveFailures {
					return errors.WrapFatal(
						fmt.Errorf("worker pool unresponsive: %d consecutive submit failures", submitFailures),
						"IndexManager",
						"processBatches",
						"queue_stuck",
					)
				}
			} else {
				// Reset counter on success
				submitFailures = 0
			}
		}
	}
}

// processBatch processes a single batch of events
func (m *Manager) processBatch(ctx context.Context, batch []EntityChange) {
	startTime := time.Now()
	processed := 0
	failed := 0

	for _, event := range batch {
		if err := m.processEntityChange(ctx, event); err != nil {
			m.logger.Error("Event processing failed", "entity", event.Key, "error", err)
			m.metrics.RecordEventFailed(err)
			if m.promMetrics != nil {
				m.promMetrics.eventsFailed.Inc()
			}
			failed++
		} else {
			m.metrics.RecordEventProcessed()
			if m.promMetrics != nil {
				m.promMetrics.eventsProcessed.Inc()
			}
			processed++
		}
	}

	// Update metrics
	duration := time.Since(startTime)
	if m.promMetrics != nil {
		m.promMetrics.processLatency.Observe(duration.Seconds())
	}
	m.metrics.UpdateProcessingLag(duration)

	if processed+failed > 0 {
		m.logger.Debug("Processed batch", "successful", processed, "failed", failed, "duration", duration)
	}
}

// processEntityChange processes a single entity change event
func (m *Manager) processEntityChange(ctx context.Context, event EntityChange) error {
	// Deduplication check
	if m.dedupCache != nil {
		dedupKey := m.createDedupKey(event)
		if _, exists := m.dedupCache.Get(dedupKey); exists {
			m.metrics.RecordDuplicateEvent()
			if m.promMetrics != nil {
				m.promMetrics.duplicateEvents.Inc()
			}
			return nil // Skip duplicate
		}
		m.dedupCache.Set(dedupKey, true)
	}

	// Validate entity state for create/update operations
	var entityState interface{}
	if event.Operation == OperationCreate || event.Operation == OperationUpdate {
		state, err := m.watcher.ValidateEntityState(event.Value)
		if err != nil {
			msg := fmt.Sprintf("failed to process entity: %s", event.Key)
			return errors.WrapTransient(err, "IndexManager", "processEvent", msg)
		}
		entityState = state
	}

	// Update all enabled indexes
	for indexType, index := range m.indexes {
		if err := m.updateIndex(ctx, index, indexType, event, entityState); err != nil {
			// Log error but continue with other indexes
			m.logger.Error("Index update failed", "index_type", indexType, "entity", event.Key, "error", err)
			if m.promMetrics != nil {
				m.promMetrics.indexUpdatesFailed.WithLabelValues(indexType, string(event.Operation)).Inc()
			}
		} else {
			if m.promMetrics != nil {
				m.promMetrics.indexUpdatesTotal.WithLabelValues(indexType, string(event.Operation)).Inc()
			}
		}
	}

	// Queue embeddings asynchronously if storage is available
	if m.embeddingStorage != nil &&
		(event.Operation == OperationCreate || event.Operation == OperationUpdate) {
		if err := m.queueEmbeddingGeneration(ctx, event.Key, entityState); err != nil {
			// Log error but don't fail the entire operation
			m.logger.Error("Failed to queue embedding", "entity_id", event.Key, "error", err)
		}
	}

	// Delete embeddings if this is a delete operation
	if m.vectorCache != nil && m.metadataCache != nil && event.Operation == OperationDelete {
		m.vectorCache.Delete(event.Key)
		m.metadataCache.Delete(event.Key)
	}

	return nil
}

// queueEmbeddingGeneration queues an entity for async embedding generation
func (m *Manager) queueEmbeddingGeneration(ctx context.Context, entityID string, entityState interface{}) error {
	// Cast to EntityState
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapTransient(
			fmt.Errorf("invalid entity state type: %T", entityState),
			"IndexManager", "generateEmbedding", "entity state must be *gtypes.EntityState")
	}

	// Check if this message type should be embedded using shouldEmbed filter
	if state.MessageType != "" {
		// Parse message type string (format: "domain.category.version")
		parts := strings.Split(state.MessageType, ".")
		if len(parts) != 3 {
			m.logger.Debug("Invalid message type format, skipping embedding",
				"entity_id", entityID, "message_type", state.MessageType)
			return nil // Skip if format is invalid
		}

		msgType := message.Type{
			Domain:   parts[0],
			Category: parts[1],
			Version:  parts[2],
		}

		if !shouldEmbed(msgType, &m.config.Embedding) {
			m.logger.Debug("Message type filtered out, skipping embedding",
				"entity_id", entityID, "message_type", state.MessageType)
			return nil // Filtered out by type
		}
	}

	// Extract text from properties (use helper from semantic.go)
	properties := state.Node.Properties
	text := m.extractText(properties)
	if text == "" {
		m.logger.Debug("No text content found, skipping embedding", "entity_id", entityID)
		return nil // No text to embed
	}

	// Record text extraction
	if m.promMetrics != nil {
		m.promMetrics.embeddingTextExtractions.Inc()
	}

	// Calculate content hash for deduplication
	contentHash := embedding.ContentHash(text)

	// Write pending record to KV - worker will pick it up asynchronously
	if err := m.embeddingStorage.SavePending(ctx, entityID, contentHash, text); err != nil {
		return errors.WrapTransient(err, "IndexManager", "queueEmbeddingGeneration", "failed to queue embedding")
	}

	m.logger.Debug("Queued embedding for async generation",
		"entity_id", entityID,
		"message_type", state.MessageType,
		"text_length", len(text),
		"content_hash", contentHash[:8]) // Log first 8 chars of hash

	return nil
}

// updateIndex updates a specific index with an entity change
func (m *Manager) updateIndex(
	ctx context.Context, index Index, indexType string,
	event EntityChange, entityState interface{},
) error {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.indexUpdateLatency.WithLabelValues(indexType).Observe(duration.Seconds())
		}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, m.config.ProcessTimeout)
	defer cancel()

	switch event.Operation {
	case OperationCreate:
		return index.HandleCreate(timeoutCtx, event.Key, entityState)
	case OperationUpdate:
		return index.HandleUpdate(timeoutCtx, event.Key, entityState)
	case OperationDelete:
		return index.HandleDelete(timeoutCtx, event.Key)
	default:
		return errors.WrapTransient(
			ErrInvalidEvent,
			"IndexManager",
			"processEvent",
			fmt.Sprintf("invalid event for entity: %s", event.Key),
		)
	}
}

// createDedupKey creates a deduplication key for an event
func (m *Manager) createDedupKey(event EntityChange) string {
	return fmt.Sprintf("%s.%d.%s", event.Key, event.Revision, event.Operation)
}

// Update methods implementation

// updateIndexes updates all relevant indexes for an entity using triples (internal use only)
func (m *Manager) updateIndexes(ctx context.Context, entityState *gtypes.EntityState) error {
	if entityState == nil {
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "updateIndexes", "entity state cannot be nil")
	}

	entityID := entityState.Node.ID

	// Update predicate index with full entity state
	if err := m.UpdatePredicateIndex(ctx, entityID, entityState); err != nil {
		return errors.WrapTransient(err, "IndexManager", "updateIndexes", "predicate index update failed")
	}

	// Update spatial index if position data is available
	if position := extractPositionFromTriples(entityState.Triples); position != nil {
		if err := m.UpdateSpatialIndex(ctx, entityID, position); err != nil {
			return errors.WrapTransient(err, "IndexManager", "updateIndexes", "spatial index update failed")
		}
	}

	// Update temporal index with full entity state
	if err := m.UpdateTemporalIndex(ctx, entityID, entityState); err != nil {
		return errors.WrapTransient(err, "IndexManager", "updateIndexes", "temporal index update failed")
	}

	// Update incoming index for all edges
	for _, edge := range entityState.Edges {
		if err := m.UpdateIncomingIndex(ctx, edge.ToEntityID, entityID); err != nil {
			return errors.WrapTransient(
				err,
				"IndexManager",
				"updateIndexes",
				fmt.Sprintf("incoming index update failed for edge to %s", edge.ToEntityID),
			)
		}
	}

	return nil
}

// updateIndexesAsync updates indexes asynchronously (internal use only)
func (m *Manager) updateIndexesAsync(ctx context.Context, entityState *gtypes.EntityState, errorCallback func(error)) {
	// Check for cancellation before submitting work
	select {
	case <-ctx.Done():
		if errorCallback != nil {
			errorCallback(ctx.Err())
		}
		return
	default:
	}

	if m.workers == nil {
		if errorCallback != nil {
			errorCallback(fmt.Errorf("worker pool not initialized"))
		}
		return
	}

	if err := m.workers.Submit(func(workerCtx context.Context) {
		if err := m.updateIndexes(workerCtx, entityState); err != nil {
			if errorCallback != nil {
				errorCallback(err)
			}
		}
	}); err != nil {
		// Check if it's a fatal error (pool stopped)
		if stderrors.Is(err, worker.ErrPoolStopped) || stderrors.Is(err, worker.ErrPoolNotStarted) {
			// Pool stopped - this is critical, notify via callback
			if errorCallback != nil {
				errorCallback(errors.WrapFatal(err, "IndexManager", "UpdateEntityAsync", "worker pool not running"))
			}
			return
		}
		// Queue full - log but don't fail the entire operation
		if stderrors.Is(err, worker.ErrQueueFull) {
			m.logger.Debug("Worker queue full, update deferred", "entity_id", entityState.Node.ID)
			if errorCallback != nil {
				errorCallback(errors.WrapTransient(err, "IndexManager", "UpdateEntityAsync", "queue full"))
			}
		}
	}
}

// extractPosition extracts position data from entity properties
func extractPosition(properties map[string]any) interface{} {
	// Look for common position property patterns
	if lat, hasLat := properties["latitude"]; hasLat {
		if lon, hasLon := properties["longitude"]; hasLon {
			return map[string]interface{}{
				"latitude":  lat,
				"longitude": lon,
			}
		}
	}

	if pos, hasPos := properties["position"]; hasPos {
		return pos
	}

	if loc, hasLoc := properties["location"]; hasLoc {
		return loc
	}

	return nil
}

// extractPositionFromTriples extracts position data from entity triples using semantic predicates
func extractPositionFromTriples(triples []message.Triple) interface{} {
	var latitude, longitude interface{}
	var hasLat, hasLon bool

	// Look for geo.location.* predicates (standard vocabulary)
	for _, triple := range triples {
		switch triple.Predicate {
		case "geo.location.latitude", "latitude":
			latitude = triple.Object
			hasLat = true
		case "geo.location.longitude", "longitude":
			longitude = triple.Object
			hasLon = true
		case "geo.location.position", "position":
			return triple.Object
		case "geo.location.coordinates", "location":
			return triple.Object
		}
	}

	// If we found both lat/lon, combine them
	if hasLat && hasLon {
		return map[string]interface{}{
			"latitude":  latitude,
			"longitude": longitude,
		}
	}

	return nil
}

// UpdatePredicateIndex updates the predicate index for an entity
func (m *Manager) UpdatePredicateIndex(ctx context.Context, entityID string, entityState interface{}) error {
	if !m.config.Indexes.Predicate {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["predicate"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	return index.HandleUpdate(ctx, entityID, entityState)
}

// UpdateSpatialIndex updates the spatial index for an entity
func (m *Manager) UpdateSpatialIndex(ctx context.Context, entityID string, position interface{}) error {
	if !m.config.Indexes.Spatial {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["spatial"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	// Create a minimal entity state with position
	entityState := map[string]interface{}{
		"position": position,
	}

	return index.HandleUpdate(ctx, entityID, entityState)
}

// UpdateTemporalIndex updates the temporal index for an entity
func (m *Manager) UpdateTemporalIndex(ctx context.Context, entityID string, entityState *gtypes.EntityState) error {
	if !m.config.Indexes.Temporal {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["temporal"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	// Pass the full entity state to the temporal index
	return index.HandleUpdate(ctx, entityID, entityState)
}

// UpdateIncomingIndex updates the incoming index for a relationship
func (m *Manager) UpdateIncomingIndex(ctx context.Context, targetEntityID, sourceEntityID string) error {
	if !m.config.Indexes.Incoming {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["incoming"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	// The IncomingIndex needs to add a reference from source to target
	// We'll call its internal method directly since it has special logic
	if incomingIndex, ok := index.(*IncomingIndex); ok {
		return incomingIndex.AddIncomingReference(ctx, targetEntityID, sourceEntityID)
	}

	return gtypes.ErrIndexNotFound
}

// UpdateAliasIndex updates the alias index for an entity
func (m *Manager) UpdateAliasIndex(ctx context.Context, alias, entityID string) error {
	if !m.config.Indexes.Alias {
		return ErrIndexDisabled
	}

	// Store alias in KV bucket with consistent key format
	// Sanitize alias and add prefix (matches ResolveAlias lookup format)
	sanitizedAlias := sanitizeNATSKey(alias)
	key := fmt.Sprintf("alias--%s", sanitizedAlias)

	_, err := m.aliasBucket.PutString(ctx, key, entityID)
	if err != nil {
		return errors.WrapTransient(err, "IndexManager", "updateAliases", "alias index update failed")
	}

	return nil
}

// DeleteFromIndexes deletes an entity from all indexes
func (m *Manager) DeleteFromIndexes(ctx context.Context, entityID string) error {
	var lastErr error

	for indexType, index := range m.indexes {
		if err := index.HandleDelete(ctx, entityID); err != nil {
			m.logger.Error("Failed to delete from index", "index", indexType, "entity", entityID, "error", err)
			lastErr = err
		}
	}

	return lastErr
}

// DeleteFromPredicateIndex deletes an entity from the predicate index
func (m *Manager) DeleteFromPredicateIndex(ctx context.Context, entityID string) error {
	if !m.config.Indexes.Predicate {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["predicate"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	return index.HandleDelete(ctx, entityID)
}

// DeleteFromSpatialIndex deletes an entity from the spatial index
func (m *Manager) DeleteFromSpatialIndex(ctx context.Context, entityID string) error {
	if !m.config.Indexes.Spatial {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["spatial"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	return index.HandleDelete(ctx, entityID)
}

// DeleteFromTemporalIndex deletes an entity from the temporal index
func (m *Manager) DeleteFromTemporalIndex(ctx context.Context, entityID string) error {
	if !m.config.Indexes.Temporal {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["temporal"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	return index.HandleDelete(ctx, entityID)
}

// DeleteFromIncomingIndex deletes an entity from the incoming index
func (m *Manager) DeleteFromIncomingIndex(ctx context.Context, entityID string) error {
	if !m.config.Indexes.Incoming {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["incoming"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	return index.HandleDelete(ctx, entityID)
}

// RemoveFromIncomingIndex removes a specific incoming relationship
func (m *Manager) RemoveFromIncomingIndex(ctx context.Context, targetEntityID, sourceEntityID string) error {
	if !m.config.Indexes.Incoming {
		return ErrIndexDisabled
	}

	index, exists := m.indexes["incoming"]
	if !exists {
		return gtypes.ErrIndexNotFound
	}

	// Remove the specific relationship from target to source
	// This is more granular than DeleteFromIncomingIndex which removes all relationships for an entity
	relationshipKey := fmt.Sprintf("%s:%s", targetEntityID, sourceEntityID)
	return index.HandleDelete(ctx, relationshipKey)
}

// DeleteFromAliasIndex deletes an alias from the alias index
func (m *Manager) DeleteFromAliasIndex(ctx context.Context, alias string) error {
	if !m.config.Indexes.Alias {
		return ErrIndexDisabled
	}

	// Delete alias from KV bucket with consistent key format
	// Sanitize alias and add prefix (matches ResolveAlias lookup format)
	sanitizedAlias := sanitizeNATSKey(alias)
	key := fmt.Sprintf("alias--%s", sanitizedAlias)

	err := m.aliasBucket.Delete(ctx, key)
	if err != nil && err != jetstream.ErrKeyNotFound {
		return errors.WrapTransient(err, "IndexManager", "removeAliases", "alias index deletion failed")
	}

	return nil
}

// Query methods implementation

// GetPredicateIndex gets entity IDs that have a specific predicate (single KV Get)
func (m *Manager) GetPredicateIndex(ctx context.Context, predicate string) ([]string, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.queryLatency.WithLabelValues("predicate").Observe(duration.Seconds())
			m.promMetrics.queriesTotal.WithLabelValues("predicate").Inc()
		}
	}()

	if !m.config.Indexes.Predicate {
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("predicate").Inc()
		}
		return nil, ErrIndexDisabled
	}

	// Check cache first if enabled
	if m.predicateCache != nil {
		if cached, ok := m.predicateCache.Get(predicate); ok {
			m.metrics.RecordQuery(false)
			// TODO: Add cache hit metric
			return cached, nil
		}
		// Cache miss - continue to KV lookup
	}

	queryCtx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	entry, err := m.predicateBucket.Get(queryCtx, predicate)
	if err != nil {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("predicate").Inc()
		}
		return []string{}, nil // No entities found
	}

	// Try simple format first ([]string)
	var entities []string
	if err := json.Unmarshal(entry.Value(), &entities); err == nil {
		m.metrics.RecordQuery(false)
		// Cache the result if cache is enabled
		if m.predicateCache != nil {
			m.predicateCache.Set(predicate, entities)
		}
		return entities, nil
	}

	// Fallback to legacy format for migration compatibility
	var predicateData map[string]interface{}
	if err := json.Unmarshal(entry.Value(), &predicateData); err != nil {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("predicate").Inc()
		}
		return nil, errors.WrapInvalid(
			err,
			"IndexManager",
			"GetPredicateIndex",
			fmt.Sprintf("failed to query predicate: %s", predicate),
		)
	}

	entitiesInterface, exists := predicateData["entities"]
	if !exists {
		m.metrics.RecordQuery(false)
		return []string{}, nil
	}

	// Convert interface{} to []string
	if entitiesArray, ok := entitiesInterface.([]interface{}); ok {
		entities = make([]string, 0, len(entitiesArray))
		for _, e := range entitiesArray {
			if entityStr, ok := e.(string); ok {
				entities = append(entities, entityStr)
			}
		}
	}

	m.metrics.RecordQuery(false)
	// Cache the result if cache is enabled
	if m.predicateCache != nil {
		m.predicateCache.Set(predicate, entities)
	}
	return entities, nil
}

// QuerySpatial queries entities within spatial bounds
func (m *Manager) QuerySpatial(ctx context.Context, bounds Bounds) ([]string, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.queryLatency.WithLabelValues("spatial").Observe(duration.Seconds())
			m.promMetrics.queriesTotal.WithLabelValues("spatial").Inc()
		}
	}()

	if !m.config.Indexes.Spatial {
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("spatial").Inc()
		}
		return nil, ErrIndexDisabled
	}

	// Validate bounds
	if bounds.North < bounds.South || bounds.East < bounds.West {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("spatial").Inc()
		}
		return nil, ErrInvalidBounds
	}

	// For Phase 1, implement a simple spatial query
	// In production, this would use proper geospatial indexing
	queryCtx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	var allEntities []string

	// Simple approach: check multiple geohash cells within bounds
	// This is inefficient but functional for Phase 1
	for lat := bounds.South; lat <= bounds.North; lat += 0.1 {
		for lon := bounds.West; lon <= bounds.East; lon += 0.1 {
			geohash := m.calculateGeohash(lat, lon, 7)
			entry, err := m.spatialBucket.Get(queryCtx, geohash)
			if err != nil {
				continue // No entities in this cell
			}

			var spatialData map[string]interface{}
			if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
				continue // Invalid data
			}

			if entities, ok := spatialData["entities"].(map[string]interface{}); ok {
				for entityID := range entities {
					allEntities = append(allEntities, entityID)
				}
			}
		}
	}

	m.metrics.RecordQuery(false)
	return allEntities, nil
}

// QueryTemporal queries entities within time range
func (m *Manager) QueryTemporal(ctx context.Context, start, end time.Time) ([]string, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.queryLatency.WithLabelValues("temporal").Observe(duration.Seconds())
			m.promMetrics.queriesTotal.WithLabelValues("temporal").Inc()
		}
	}()

	if !m.config.Indexes.Temporal {
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("temporal").Inc()
		}
		return nil, ErrIndexDisabled
	}

	// Validate time range
	if start.After(end) {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("temporal").Inc()
		}
		return nil, ErrInvalidTimeRange
	}

	queryCtx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	var allEntities []string

	// Query hourly buckets within time range
	current := start.Truncate(time.Hour)
	endHour := end.Truncate(time.Hour)

	for current.Before(endHour.Add(time.Hour)) {
		timeKey := fmt.Sprintf("%04d.%02d.%02d.%02d",
			current.Year(),
			current.Month(),
			current.Day(),
			current.Hour())

		entry, err := m.temporalBucket.Get(queryCtx, timeKey)
		if err != nil {
			current = current.Add(time.Hour)
			continue // No entities in this hour
		}

		var temporalData map[string]interface{}
		if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
			current = current.Add(time.Hour)
			continue // Invalid data
		}

		if events, ok := temporalData["events"].([]interface{}); ok {
			for _, event := range events {
				if eventMap, ok := event.(map[string]interface{}); ok {
					if entityID, ok := eventMap["entity"].(string); ok {
						allEntities = append(allEntities, entityID)
					}
				}
			}
		}

		current = current.Add(time.Hour)
	}

	m.metrics.RecordQuery(false)
	return allEntities, nil
}

// GetIncomingRelationships gets incoming relationships for a target entity (single KV Get)
func (m *Manager) GetIncomingRelationships(ctx context.Context, targetEntityID string) ([]Relationship, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.queryLatency.WithLabelValues("incoming").Observe(duration.Seconds())
			m.promMetrics.queriesTotal.WithLabelValues("incoming").Inc()
		}
	}()

	if !m.config.Indexes.Incoming {
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("incoming").Inc()
		}
		return nil, ErrIndexDisabled
	}

	// Check cache first if enabled
	if m.incomingCache != nil {
		if cached, ok := m.incomingCache.Get(targetEntityID); ok {
			m.metrics.RecordQuery(false)
			// TODO: Add cache hit metric
			return cached, nil
		}
		// Cache miss - continue to KV lookup
	}

	queryCtx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	entry, err := m.incomingBucket.Get(queryCtx, targetEntityID)
	if err != nil {
		m.metrics.RecordQuery(false)
		return []Relationship{}, nil // No incoming relationships
	}

	var incomingIDs []string
	if err := json.Unmarshal(entry.Value(), &incomingIDs); err != nil {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("incoming").Inc()
		}
		return nil, errors.WrapInvalid(
			err,
			"IndexManager",
			"GetIncomingRelationships",
			fmt.Sprintf("failed to query incoming relationships for: %s", targetEntityID),
		)
	}

	// Convert to Relationship objects
	// For Phase 1, we'll create simple relationships without full edge data
	relationships := make([]Relationship, len(incomingIDs))
	for i, fromID := range incomingIDs {
		relationships[i] = Relationship{
			FromEntityID: fromID,
			EdgeType:     "references", // Simplified edge type
			Weight:       1.0,
			Properties:   make(map[string]interface{}),
			CreatedAt:    time.Now(), // Placeholder timestamp
		}
	}

	m.metrics.RecordQuery(false)
	// Cache the result if cache is enabled
	if m.incomingCache != nil {
		m.incomingCache.Set(targetEntityID, relationships)
	}
	return relationships, nil
}

// ResolveAlias resolves an alias to an entity ID
func (m *Manager) ResolveAlias(ctx context.Context, alias string) (string, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if m.promMetrics != nil {
			m.promMetrics.queryLatency.WithLabelValues("alias").Observe(duration.Seconds())
			m.promMetrics.queriesTotal.WithLabelValues("alias").Inc()
		}
	}()

	if !m.config.Indexes.Alias {
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("alias").Inc()
		}
		return "", ErrIndexDisabled
	}

	// Check cache first if enabled
	if m.aliasCache != nil {
		if cached, ok := m.aliasCache.Get(alias); ok {
			m.metrics.RecordQuery(false)
			// TODO: Add cache hit metric
			return cached, nil
		}
		// Cache miss - continue to KV lookup
	}

	queryCtx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	// Sanitize alias and add prefix (matches AliasIndex storage format)
	sanitizedAlias := sanitizeNATSKey(alias)
	key := fmt.Sprintf("alias--%s", sanitizedAlias)

	entry, err := m.aliasBucket.Get(queryCtx, key)
	if err != nil {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("alias").Inc()
		}
		return "", gtypes.ErrAliasNotFound
	}

	entityID := string(entry.Value())
	if entityID == "" {
		m.metrics.RecordQuery(true)
		if m.promMetrics != nil {
			m.promMetrics.queriesFailed.WithLabelValues("alias").Inc()
		}
		return "", gtypes.ErrAliasNotFound
	}

	m.metrics.RecordQuery(false)
	// Cache the result if cache is enabled (cache by original alias, not sanitized key)
	if m.aliasCache != nil {
		m.aliasCache.Set(alias, entityID)
	}
	return entityID, nil
}

// Batch query operations

// GetPredicateIndexes performs batch predicate Gets (multiple single KV Gets)
func (m *Manager) GetPredicateIndexes(ctx context.Context, predicates []string) (map[string][]string, error) {
	results := make(map[string][]string)

	for _, predicate := range predicates {
		entities, err := m.GetPredicateIndex(ctx, predicate)
		if err != nil {
			return nil, err
		}
		results[predicate] = entities
	}

	return results, nil
}

// ResolveAliases resolves multiple aliases in a single operation
func (m *Manager) ResolveAliases(ctx context.Context, aliases []string) (map[string]string, error) {
	results := make(map[string]string)

	for _, alias := range aliases {
		entityID, err := m.ResolveAlias(ctx, alias)
		if err != nil && err != gtypes.ErrAliasNotFound {
			return nil, err
		}
		if err == nil {
			results[alias] = entityID
		}
	}

	return results, nil
}

// calculateGeohash calculates a simple geohash for spatial queries
func (m *Manager) calculateGeohash(lat, lon float64, _ int) string {
	// Simplified geohash for Phase 1
	latInt := int(math.Floor((lat + 90.0) * 100))
	lonInt := int(math.Floor((lon + 180.0) * 100))
	return fmt.Sprintf("geo_%d_%d", latInt, lonInt)
}

// GetBacklog returns the number of pending events in the processing queue.
func (m *Manager) GetBacklog() int {
	return len(m.eventChan) + len(m.batchChan)*m.config.BatchProcessing.Size
}

// GetDeduplicationStats returns the current deduplication statistics.
func (m *Manager) GetDeduplicationStats() DeduplicationStats {
	return m.metrics.GetDeduplicationStats()
}
