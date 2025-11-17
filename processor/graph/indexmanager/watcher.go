package indexmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// KVWatcher manages KV watching for ENTITY_STATES changes
type KVWatcher struct {
	mu sync.RWMutex

	// Configuration
	bucket     jetstream.KeyValue
	bucketName string

	// State
	watcher  jetstream.KeyWatcher
	started  bool
	stopping bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // Track goroutines

	// Event processing
	eventChan chan EntityChange
	buffer    chan EntityChange // Buffered channel for event processing

	// Metrics
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics

	// Configuration
	bufferSize     int
	reconnectDelay time.Duration

	// Logger
	logger *slog.Logger
}

// NewKVWatcher creates a new KVWatcher instance
func NewKVWatcher(
	bucket jetstream.KeyValue,
	bucketName string,
	eventChan chan EntityChange,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *KVWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &KVWatcher{
		bucket:         bucket,
		bucketName:     bucketName,
		eventChan:      eventChan,
		metrics:        metrics,
		promMetrics:    promMetrics,
		bufferSize:     1000, // Internal buffer size
		reconnectDelay: 5 * time.Second,
		logger:         logger,
	}
}

// Start begins watching the KV bucket for changes
func (w *KVWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return gtypes.ErrAlreadyStarted
	}

	// Create context for the watcher
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Create internal buffer
	w.buffer = make(chan EntityChange, w.bufferSize)

	// Start the watcher
	if err := w.startWatcher(); err != nil {
		w.cancel()
		return errors.WrapTransient(
			err,
			"IndexManager",
			"Start",
			fmt.Sprintf("failed to start watcher for bucket: %s", w.bucketName),
		)
	}

	// Start the event processor
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.processEvents()
	}()

	// Start reconnection handler
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.handleReconnections()
	}()

	w.started = true
	w.metrics.UpdateWatchersActive(1)
	if w.promMetrics != nil {
		w.promMetrics.watchersActive.Set(1)
	}

	w.logger.Info("KV Watcher started", "bucket", w.bucketName)
	return nil
}

// Stop stops the KV watcher gracefully
func (w *KVWatcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return nil
	}

	w.stopping = true

	// Cancel context to signal all goroutines to stop
	if w.cancel != nil {
		w.cancel()
	}

	// Stop the watcher
	if w.watcher != nil {
		if err := w.watcher.Stop(); err != nil {
			w.logger.Warn("KV watcher stop error", "error", err)
		}
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
		w.logger.Debug("All watcher goroutines finished")
	case <-time.After(2 * time.Second):
		w.logger.Warn("Watcher shutdown timeout exceeded")
	}

	// Now safe to close buffer channel - all goroutines have exited
	if w.buffer != nil {
		close(w.buffer)
	}

	w.started = false
	w.metrics.UpdateWatchersActive(0)
	if w.promMetrics != nil {
		w.promMetrics.watchersActive.Set(0)
	}

	w.logger.Info("KV Watcher stopped", "bucket", w.bucketName)
	return nil
}

// startWatcher initializes the KV watcher
func (w *KVWatcher) startWatcher() error {
	// Watch all changes in the ENTITY_STATES bucket
	watcher, err := w.bucket.WatchAll(w.ctx)
	if err != nil {
		return errors.WrapFatal(err, "IndexManager", "startWatcher", "KV watcher creation failed")
	}

	w.watcher = watcher

	// Start consuming watch updates
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.consumeWatchUpdates()
	}()

	return nil
}

// consumeWatchUpdates processes incoming KV watch updates
func (w *KVWatcher) consumeWatchUpdates() {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("KV watcher panic recovered", "panic", r)
			w.metrics.RecordEventFailed(
				errors.WrapFatal(fmt.Errorf("panic: %v", r), "IndexManager", "runWatcher", "KV watcher panic"),
			)
			if w.promMetrics != nil {
				w.promMetrics.watchEventsFailed.Inc()
			}
		}
	}()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("KV watcher context cancelled")
			return

		case entry, ok := <-w.watcher.Updates():
			if !ok {
				w.logger.Debug("KV watcher updates channel closed")
				return
			}

			if entry == nil {
				continue // Skip nil entries
			}

			// Process the KV entry
			w.processKVEntry(entry)
		}
	}
}

// processKVEntry converts a KV entry to an EntityChange and buffers it
func (w *KVWatcher) processKVEntry(entry jetstream.KeyValueEntry) {
	if w.promMetrics != nil {
		w.promMetrics.watchEventsTotal.Inc()
	}
	w.metrics.RecordEventReceived()

	// Determine operation type
	operation := w.detectOperation(entry)
	if operation == "" {
		w.logger.Warn("Unknown KV operation", "key", entry.Key())
		if w.promMetrics != nil {
			w.promMetrics.watchEventsFailed.Inc()
		}
		return
	}

	// Create entity change event
	change := EntityChange{
		Key:       entry.Key(),
		Operation: operation,
		Value:     entry.Value(),
		Revision:  entry.Revision(),
		Timestamp: time.Now(),
	}

	// Buffer the change for processing
	select {
	case w.buffer <- change:
		// Successfully buffered
	case <-w.ctx.Done():
		return
	default:
		// Buffer full - drop oldest or log warning
		w.logger.Warn("KV watcher buffer full, dropping event", "entity", entry.Key())
		w.metrics.RecordEventDropped()
		if w.promMetrics != nil {
			w.promMetrics.eventsDropped.Inc()
		}
	}
}

// detectOperation determines the operation type from a KV entry
func (w *KVWatcher) detectOperation(entry jetstream.KeyValueEntry) Operation {
	switch entry.Operation() {
	case jetstream.KeyValuePut:
		if entry.Revision() == 1 {
			return OperationCreate
		}
		return OperationUpdate
	case jetstream.KeyValueDelete:
		return OperationDelete
	default:
		return "" // Unknown operation
	}
}

// processEvents forwards buffered events to the main event channel
func (w *KVWatcher) processEvents() {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Event processor panic recovered", "panic", r)
		}
	}()

	for {
		select {
		case <-w.ctx.Done():
			return

		case change, ok := <-w.buffer:
			if !ok {
				return // Buffer closed
			}

			// Forward to main event processing
			select {
			case w.eventChan <- change:
				// Successfully forwarded
			case <-w.ctx.Done():
				return
			default:
				// Main event channel full - this is a backpressure situation
				w.logger.Warn("Main event channel full, dropping event", "entity", change.Key)
				w.metrics.RecordEventDropped()
				if w.promMetrics != nil {
					w.promMetrics.eventsDropped.Inc()
				}
			}
		}
	}
}

// handleReconnections manages automatic reconnection on watch failures
func (w *KVWatcher) handleReconnections() {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Reconnection handler panic recovered", "panic", r)
		}
	}()

	reconnectTimer := time.NewTicker(w.reconnectDelay)
	defer reconnectTimer.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return

		case <-reconnectTimer.C:
			if w.shouldReconnect() {
				w.attemptReconnection()
			}
		}
	}
}

// shouldReconnect determines if reconnection is needed
func (w *KVWatcher) shouldReconnect() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.started && !w.stopping && (w.watcher == nil)
}

// attemptReconnection tries to reconnect the KV watcher
func (w *KVWatcher) attemptReconnection() {
	w.logger.Info("Attempting KV watcher reconnection", "bucket", w.bucketName)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopping {
		return
	}

	// Try to create new watcher
	if err := w.startWatcher(); err != nil {
		w.logger.Error("KV watcher reconnection failed", "error", err)
		w.metrics.RecordEventFailed(err)
		if w.promMetrics != nil {
			w.promMetrics.watchEventsFailed.Inc()
		}
		return
	}

	if w.promMetrics != nil {
		w.promMetrics.watchReconnections.Inc()
	}
	w.logger.Info("KV watcher reconnected successfully", "bucket", w.bucketName)
}

// GetStatus returns the current status of the watcher
func (w *KVWatcher) GetStatus() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"started":     w.started,
		"stopping":    w.stopping,
		"bucket_name": w.bucketName,
		"buffer_size": len(w.buffer),
		"has_watcher": w.watcher != nil,
	}
}

// ValidateEntityState validates that the entity state is properly formatted
func (w *KVWatcher) ValidateEntityState(data []byte) (*gtypes.EntityState, error) {
	if len(data) == 0 {
		return nil, errors.WrapInvalid(
			errors.ErrInvalidData,
			"IndexManager",
			"parseEntityState",
			"empty entity state data",
		)
	}

	var state gtypes.EntityState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, errors.WrapInvalid(err, "IndexManager", "parseEntityState", "entity state JSON unmarshal failed")
	}

	// Basic validation
	if state.Node.ID == "" {
		return nil, errors.WrapInvalid(
			errors.ErrInvalidData,
			"IndexManager",
			"parseEntityState",
			"entity state missing ID",
		)
	}

	if state.Node.Type == "" {
		return nil, errors.WrapInvalid(
			errors.ErrInvalidData,
			"IndexManager",
			"parseEntityState",
			"entity state missing type",
		)
	}

	return &state, nil
}
