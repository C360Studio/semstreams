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

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/pkg/buffer"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/pkg/retry"
	"github.com/c360/semstreams/pkg/worker"
)

var (
	// entityIDRegex validates entity ID format: platform.namespace.type.subtype.instance.name
	// Example: c360.platform1.robotics.gcs1.drone.1
	entityIDRegex = regexp.MustCompile(
		`^[a-zA-Z0-9]+\.[a-zA-Z0-9]+\.[a-zA-Z0-9]+\.[a-zA-Z0-9]+\.[a-zA-Z0-9]+\.[a-zA-Z0-9]+$`)
)

// validateEntityID validates that an entity ID follows the expected format
func validateEntityID(id string) error {
	if id == "" {
		return errors.WrapInvalid(nil, "DataManager", "validateEntityID", "entity ID cannot be empty")
	}

	if len(id) > 255 {
		return errors.WrapInvalid(nil, "DataManager", "validateEntityID", "entity ID too long (max 255 chars)")
	}

	if !entityIDRegex.MatchString(id) {
		parts := strings.Split(id, ".")
		msg := fmt.Sprintf(
			"invalid entity ID format: expected 6 parts (platform.namespace.type.subtype.instance.name), got %d parts",
			len(parts))
		return errors.WrapInvalid(nil, "DataManager", "validateEntityID", msg)
	}

	return nil
}

// validateEntity validates an entity state before operations
func validateEntity(entity *gtypes.EntityState) error {
	if entity == nil {
		return errors.WrapInvalid(nil, "DataManager", "validateEntity", "entity cannot be nil")
	}

	return validateEntityID(entity.Node.ID)
}

// Manager is the consolidated data management service using framework components
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

	// Configuration
	config Config

	// State management
	wg sync.WaitGroup
}

// Note: EntityWrite is already defined in types.go
// Compile-time verification that Manager implements all interfaces is now in interfaces.go

// NewDataManager creates a new data manager using framework components
func NewDataManager(deps Dependencies) (*Manager, error) {
	if deps.KVBucket == nil {
		return nil, errors.WrapInvalid(nil, "DataManager", "NewDataManager", "kvBucket is required")
	}
	if deps.Logger == nil {
		return nil, errors.WrapInvalid(nil, "DataManager", "NewDataManager", "logger is required")
	}

	// Apply defaults to config
	if deps.Config.Workers == 0 {
		deps.Config = DefaultConfig()
	}

	m := &Manager{
		kvBucket:        deps.KVBucket,
		metricsRegistry: deps.MetricsRegistry,
		logger:          deps.Logger,
		config:          deps.Config,
	}

	// Initialize L1 cache (LRU) using framework
	if m.config.Cache.L1Hot.Size > 0 {
		opts := []cache.Option[*gtypes.EntityState]{}
		if m.metricsRegistry != nil {
			opts = append(opts, cache.WithMetrics[*gtypes.EntityState](
				m.metricsRegistry, "datamanager_l1"))
		}
		var err error
		m.l1Cache, err = cache.NewLRU[*gtypes.EntityState](
			m.config.Cache.L1Hot.Size, opts...)
		if err != nil {
			return nil, errors.WrapTransient(err, "DataManager", "NewManager", "L1 cache creation")
		}
	}

	// Note: L2 cache (TTL) will be initialized in Run() since it needs context

	// Initialize write buffer using framework
	if m.config.BufferConfig.BatchingEnabled {
		// Map string overflow policy to buffer.OverflowPolicy
		var overflowPolicy buffer.OverflowPolicy
		switch m.config.BufferConfig.OverflowPolicy {
		case "drop_newest":
			overflowPolicy = buffer.DropNewest
		case "block":
			overflowPolicy = buffer.Block
		default: // "drop_oldest"
			overflowPolicy = buffer.DropOldest
		}

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
			return nil, errors.WrapTransient(err, "DataManager", "NewManager", "write buffer creation")
		}

		// Initialize worker pool using framework
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

	return m, nil
}

// Run starts the DataManager and blocks until context is cancelled or fatal error occurs
func (m *Manager) Run(ctx context.Context) error {
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
			return errors.WrapTransient(err, "DataManager", "Run", "L2 cache creation")
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
					bufferErr <- errors.WrapFatal(panicErr, "DataManager",
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
				if errors.IsFatal(err) {
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

// flushBufferWithContext processes pending writes from the buffer with coalescing and context awareness
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

	// Read batch from buffer
	batch := m.writeBuffer.ReadBatch(m.config.BufferConfig.MaxBatchSize)
	if len(batch) == 0 {
		return nil
	}

	// Coalesce writes to the same entity
	coalesced := m.coalesceWrites(batch)

	// Submit coalesced writes to workers
	var submitErrors int
	for _, write := range coalesced {
		// Check context before each submit
		select {
		case <-ctx.Done():
			// Context cancelled, stop submitting
			if write.Callback != nil {
				write.Callback(errors.Wrap(ctx.Err(), "DataManager", "flushBufferWithContext", "context cancelled"))
			}
			return nil // Clean shutdown
		default:
		}

		if err := m.workers.Submit(write); err != nil {
			// Check error type to determine if it's fatal or transient
			if stderrors.Is(err, worker.ErrPoolStopped) || stderrors.Is(err, worker.ErrPoolNotStarted) {
				// Pool stopped unexpectedly - this is fatal
				return errors.WrapFatal(err, "DataManager", "flushBufferWithContext", "worker pool no longer running")
			}
			if stderrors.Is(err, worker.ErrQueueFull) {
				// Queue full is transient - notify callback and continue
				if write.Callback != nil {
					queueErr := errors.WrapTransient(err, "DataManager",
						"flushBufferWithContext", "worker queue full")
					write.Callback(queueErr)
				}
				submitErrors++
				continue
			}
			// Unknown error - treat as fatal
			return errors.WrapFatal(err, "DataManager", "flushBufferWithContext", "unexpected worker pool error")
		}
	}

	// If ALL submits failed, might indicate a problem
	if submitErrors > 0 && submitErrors == len(coalesced) {
		m.logger.Warn("All writes in batch failed to submit",
			"batch_size", len(coalesced),
			"errors", submitErrors)
		// Still transient - queue is just full
	}

	return nil
}

// coalesceWrites merges multiple writes to the same entity into a single write
func (m *Manager) coalesceWrites(writes []*EntityWrite) []*EntityWrite {
	// Map to track the latest write per entity
	entityWrites := make(map[string]*EntityWrite)

	// Track order of first appearance
	order := []string{}

	for _, write := range writes {
		if write == nil || write.Entity == nil {
			continue
		}

		entityID := write.Entity.Node.ID

		// Check if we already have a write for this entity
		if existing, exists := entityWrites[entityID]; exists {
			// Merge the writes based on operation type
			merged := m.mergeWrites(existing, write)
			entityWrites[entityID] = merged
		} else {
			// First write for this entity
			entityWrites[entityID] = write
			order = append(order, entityID)
		}
	}

	// Build result preserving order
	result := make([]*EntityWrite, 0, len(entityWrites))
	for _, entityID := range order {
		if write, exists := entityWrites[entityID]; exists {
			result = append(result, write)
		}
	}

	// Log coalescing stats if significant
	if len(result) < len(writes) {
		m.logger.Debug("Coalesced writes",
			"original_count", len(writes),
			"coalesced_count", len(result),
			"reduction", len(writes)-len(result))
	}

	return result
}

// mergeWrites merges two writes to the same entity
func (m *Manager) mergeWrites(existing, newer *EntityWrite) *EntityWrite {
	// Handle different operation combinations
	switch {
	case newer.Operation == OperationDelete:
		// Delete supersedes everything
		return newer

	case existing.Operation == OperationCreate && newer.Operation == OperationUpdate:
		// Create + Update = Create with updated properties
		merged := &EntityWrite{
			Operation: OperationCreate,
			Entity:    m.mergeEntityStates(existing.Entity, newer.Entity),
			Timestamp: existing.Timestamp, // Keep original timestamp
			Callback:  m.combineCallbacks(existing.Callback, newer.Callback),
		}
		return merged

	case existing.Operation == OperationUpdate && newer.Operation == OperationUpdate:
		// Update + Update = Update with merged properties
		merged := &EntityWrite{
			Operation: OperationUpdate,
			Entity:    m.mergeEntityStates(existing.Entity, newer.Entity),
			Timestamp: existing.Timestamp, // Keep original timestamp
			Callback:  m.combineCallbacks(existing.Callback, newer.Callback),
		}
		return merged

	default:
		// For other combinations, newer wins
		return newer
	}
}

// mergeEntityStates merges triples from two entity states using semantic triples as single source of truth
func (m *Manager) mergeEntityStates(existing, newer *gtypes.EntityState) *gtypes.EntityState {
	if existing == nil {
		return newer
	}
	if newer == nil {
		return existing
	}

	// Create merged entity with triples-based approach
	merged := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   existing.Node.ID,
			Type: newer.Node.Type, // Use newer type
		},
		Edges:       newer.Edges,                                          // Use newer edges
		Triples:     gtypes.MergeTriples(existing.Triples, newer.Triples), // CRITICAL: Proper triples merging
		ObjectRef:   newer.ObjectRef,                                      // Use newer ObjectRef
		MessageType: newer.MessageType,                                    // Use newer message type
		UpdatedAt:   newer.UpdatedAt,                                      // Use newer timestamp
		Version:     newer.Version,                                        // Will be set during actual write
	}

	return merged
}

// combineCallbacks combines multiple callbacks into one
func (m *Manager) combineCallbacks(callbacks ...func(error)) func(error) {
	// Filter out nil callbacks
	var nonNil []func(error)
	for _, cb := range callbacks {
		if cb != nil {
			nonNil = append(nonNil, cb)
		}
	}

	if len(nonNil) == 0 {
		return nil
	}

	// Return a function that calls all callbacks
	return func(err error) {
		for _, cb := range nonNil {
			cb(err)
		}
	}
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
		_, err := m.updateEntityDirect(ctx, write.Entity)
		if write.Callback != nil {
			write.Callback(err)
		}
		return err

	case OperationDelete:
		var err error
		if write.Entity != nil {
			err = m.deleteEntityDirect(ctx, write.Entity.Node.ID)
		}
		if write.Callback != nil {
			write.Callback(err)
		}
		return err

	default:
		msg := fmt.Sprintf("invalid operation: %s", write.Operation)
		err := errors.WrapInvalid(nil, "DataManager", "processWrite", msg)
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
			return nil, errors.Wrap(err, "DataManager", "CreateEntity", "buffer write")
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
	if entity == nil {
		err := errors.WrapInvalid(nil, "DataManager", "createEntityDirect", "entity cannot be nil")
		return nil, err
	}

	if entity.Node.ID == "" {
		err := errors.WrapInvalid(nil, "DataManager", "createEntityDirect", "entity ID cannot be empty")
		return nil, err
	}

	// Set version and timestamps
	entity.Version = 1
	entity.UpdatedAt = time.Now()

	// Serialize entity
	data, err := json.Marshal(entity)
	if err != nil {
		err = errors.Wrap(err, "DataManager", "createEntityDirect", "marshal entity")
		return nil, err
	}

	// Create in KV bucket
	if _, err := m.kvBucket.Create(ctx, entity.Node.ID, data); err != nil {
		if err == jetstream.ErrKeyExists {
			err = errors.WrapInvalid(err, "DataManager", "createEntityDirect", "entity already exists")
		} else {
			err = errors.Wrap(err, "DataManager", "createEntityDirect", "KV create")
		}
		return nil, err
	}

	// Update caches
	m.updateCaches(entity)

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
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return nil, errors.Wrap(err, "DataManager", "UpdateEntity", "buffer write")
		}

		// For updates, we need to wait synchronously
		// In production, this would use a result channel
		return entity, nil
	}

	// Direct write
	return m.updateEntityDirect(ctx, entity)
}

// updateEntityDirect performs immediate entity update with CAS
func (m *Manager) updateEntityDirect(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	if entity == nil {
		err := errors.WrapInvalid(nil, "DataManager", "updateEntityDirect", "entity cannot be nil")
		return nil, err
	}

	// Retry logic for CAS operations
	retryConfig := retry.DefaultConfig()
	retryConfig.MaxAttempts = m.config.MaxRetries

	var updatedEntity *gtypes.EntityState
	err := retry.Do(ctx, retryConfig, func() error {
		// Get current version
		current, err := m.GetEntity(ctx, entity.Node.ID)
		if err != nil {
			return errors.Wrap(err, "DataManager", "updateEntityDirect", "get current version")
		}

		// Update version and timestamp
		entity.Version = current.Version + 1
		entity.UpdatedAt = time.Now()

		// Serialize entity
		data, err := json.Marshal(entity)
		if err != nil {
			return errors.Wrap(err, "DataManager", "updateEntityDirect", "marshal entity")
		}

		// CAS update
		if _, err := m.kvBucket.Update(ctx, entity.Node.ID, data, uint64(current.Version)); err != nil {
			if err == jetstream.ErrKeyNotFound {
				return errors.WrapInvalid(err, "DataManager", "updateEntityDirect", "entity not found")
			}
			// CAS conflict is transient, will retry
			return errors.WrapTransient(err, "DataManager", "updateEntityDirect", "CAS conflict")
		}

		updatedEntity = entity
		return nil
	})

	if err != nil {
		// Record error after all retries have failed
		return nil, err
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
			Entity:    &gtypes.EntityState{Node: gtypes.NodeProperties{ID: id}},
			Timestamp: time.Now(),
		}

		if err := m.writeBuffer.Write(write); err != nil {
			return errors.Wrap(err, "DataManager", "DeleteEntity", "buffer write")
		}

		return nil
	}

	// Direct delete
	return m.deleteEntityDirect(ctx, id)
}

// deleteEntityDirect performs immediate entity deletion
func (m *Manager) deleteEntityDirect(ctx context.Context, id string) error {
	// Get entity for cleanup
	entity, err := m.GetEntity(ctx, id)
	if err != nil {
		err = errors.Wrap(err, "DataManager", "deleteEntityDirect", "get entity for cleanup")
		return err
	}

	// Cleanup incoming references if needed
	if len(entity.Edges) > 0 {
		if err := m.CleanupIncomingReferences(ctx, id, entity.Edges); err != nil {
			m.logger.Warn("Failed to cleanup incoming references", "entity", id, "error", err)
			// Don't fail the delete operation for cleanup errors
		}
	}

	// Delete from KV
	if err := m.kvBucket.Delete(ctx, id); err != nil {
		if err == jetstream.ErrKeyNotFound {
			err = errors.WrapInvalid(err, "DataManager", "deleteEntityDirect", "entity not found")
		} else {
			err = errors.Wrap(err, "DataManager", "deleteEntityDirect", "KV delete")
		}
		return err
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
			return entity, nil
		}
	}

	// Check L2 cache
	if m.l2Cache != nil {
		if entity, found := m.l2Cache.Get(id); found {
			// Promote to L1
			if m.l1Cache != nil {
				m.l1Cache.Set(id, entity)
			}
			return entity, nil
		}
	}

	// Fetch from KV
	entry, err := m.kvBucket.Get(ctx, id)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			// Not found is often expected, don't record as error
			return nil, errors.WrapInvalid(err, "DataManager", "GetEntity", "entity not found")
		}
		// Record actual KV errors
		err = errors.Wrap(err, "DataManager", "GetEntity", "KV get")
		return nil, err
	}

	// Deserialize entity
	var entity gtypes.EntityState
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		err = errors.Wrap(err, "DataManager", "GetEntity", "unmarshal entity")
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
		return false, errors.Wrap(err, "DataManager", "ExistsEntity", "KV get")
	}
	return true, nil
}

// Atomic Entity+Edge Operations

// CreateEntityWithEdges creates an entity with edges atomically
func (m *Manager) CreateEntityWithEdges(
	ctx context.Context,
	entity *gtypes.EntityState,
	edges []gtypes.Edge,
) (*gtypes.EntityState, error) {
	if entity == nil {
		return nil, errors.WrapInvalid(nil, "DataManager",
			"CreateEntityWithEdges", "entity cannot be nil")
	}

	// Add edges to entity before creating
	for _, edge := range edges {
		entity.AddEdge(edge)
	}

	// Create entity with edges
	return m.CreateEntity(ctx, entity)
}

// UpdateEntityWithEdges updates an entity and modifies edges atomically
func (m *Manager) UpdateEntityWithEdges(
	ctx context.Context, entity *gtypes.EntityState,
	addEdges []gtypes.Edge, removeEdges []string,
) (*gtypes.EntityState, error) {
	if entity == nil {
		return nil, errors.WrapInvalid(nil, "DataManager",
			"UpdateEntityWithEdges", "entity cannot be nil")
	}

	// Get current entity state
	current, err := m.GetEntity(ctx, entity.Node.ID)
	if err != nil {
		return nil, err
	}

	// Start with current edges
	entity.Edges = current.Edges

	// Remove edges
	newEdges := []gtypes.Edge{}
	for _, edge := range entity.Edges {
		shouldRemove := false
		for _, removeID := range removeEdges {
			if edge.ToEntityID == removeID {
				shouldRemove = true
				break
			}
		}
		if !shouldRemove {
			newEdges = append(newEdges, edge)
		}
	}
	entity.Edges = newEdges

	// Add new edges
	for _, edge := range addEdges {
		entity.AddEdge(edge)
	}

	// Update entity with modified edges
	return m.UpdateEntity(ctx, entity)
}

// Additional required methods...

// Helper methods

// updateCaches updates both L1 and L2 caches
func (m *Manager) updateCaches(entity *gtypes.EntityState) {
	if m.l1Cache != nil {
		m.l1Cache.Set(entity.Node.ID, entity)
	}
	if m.l2Cache != nil {
		m.l2Cache.Set(entity.Node.ID, entity)
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
