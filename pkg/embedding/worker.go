package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
)

// Worker processes pending embedding requests asynchronously
type Worker struct {
	mu sync.RWMutex

	// Dependencies
	storage  *Storage
	embedder Embedder // HTTP or BM25 embedder

	// KV watching
	indexBucket jetstream.KeyValue
	watcher     jetstream.KeyWatcher

	// State
	started  bool
	stopping bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Configuration
	workers int // Number of concurrent workers

	// Logger
	logger *slog.Logger
}

// NewWorker creates a new async embedding worker
func NewWorker(
	storage *Storage,
	embedder Embedder,
	indexBucket jetstream.KeyValue,
	logger *slog.Logger,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		storage:     storage,
		embedder:    embedder,
		indexBucket: indexBucket,
		workers:     5, // Default concurrent workers
		logger:      logger,
	}
}

// WithWorkers sets the number of concurrent workers
func (w *Worker) WithWorkers(n int) *Worker {
	w.workers = n
	return w
}

// Start begins watching for pending embeddings and processing them
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("embedding worker already started")
	}

	// Create context for the worker
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Start KV watcher for EMBEDDING_INDEX
	watcher, err := w.indexBucket.WatchAll(w.ctx)
	if err != nil {
		w.cancel()
		return errors.WrapTransient(err, "Worker", "Start", "failed to create KV watcher")
	}
	w.watcher = watcher

	// Start worker goroutines
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func(workerID int) {
			defer w.wg.Done()
			w.processEmbeddings(workerID)
		}(i)
	}

	w.started = true
	w.logger.Info("Embedding worker started", "workers", w.workers)
	return nil
}

// Stop stops the embedding worker gracefully
func (w *Worker) Stop() error {
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

	// Wait for all goroutines to finish
	w.wg.Wait()

	w.started = false
	w.logger.Info("Embedding worker stopped")
	return nil
}

// processEmbeddings watches for KV changes and processes pending embeddings
func (w *Worker) processEmbeddings(workerID int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Embedding worker panic recovered", "worker_id", workerID, "panic", r)
		}
	}()

	w.logger.Debug("Embedding worker goroutine started", "worker_id", workerID)

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Embedding worker context cancelled", "worker_id", workerID)
			return

		case entry, ok := <-w.watcher.Updates():
			if !ok {
				w.logger.Debug("KV watcher updates channel closed", "worker_id", workerID)
				return
			}

			if entry == nil {
				continue
			}

			// Process if this is a new pending record or update to existing pending
			if entry.Operation() == jetstream.KeyValuePut {
				w.handleKVEntry(entry, workerID)
			}
		}
	}
}

// handleKVEntry processes a KV entry to check if it needs embedding generation
func (w *Worker) handleKVEntry(entry jetstream.KeyValueEntry, workerID int) {
	// Parse the record to check status
	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		w.logger.Warn("Failed to unmarshal embedding record", "key", entry.Key(), "error", err)
		return
	}

	// Only process pending records
	if record.Status != StatusPending {
		return
	}

	entityID := entry.Key()
	w.logger.Debug("Processing pending embedding", "worker_id", workerID, "entity_id", entityID)

	// Check deduplication first
	dedupRecord, err := w.storage.GetByContentHash(w.ctx, record.ContentHash)
	if err != nil {
		w.logger.Error("Failed to check dedup", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("dedup check failed: %v", err))
		return
	}

	var vector []float32

	if dedupRecord != nil {
		// Reuse existing embedding
		w.logger.Debug("Deduplicating embedding", "entity_id", entityID, "content_hash", record.ContentHash)
		vector = dedupRecord.Vector
	} else {
		// Generate new embedding
		w.logger.Debug("Generating new embedding", "entity_id", entityID)

		vectors, err := w.embedder.Generate(w.ctx, []string{record.SourceText})
		if err != nil {
			w.logger.Error("Failed to generate embedding", "entity_id", entityID, "error", err)
			w.markFailed(entityID, fmt.Sprintf("generation failed: %v", err))
			return
		}

		if len(vectors) == 0 {
			w.logger.Error("No embedding generated", "entity_id", entityID)
			w.markFailed(entityID, "no embedding returned")
			return
		}

		vector = vectors[0]

		// Save to dedup bucket
		if err := w.storage.SaveDedup(w.ctx, record.ContentHash, vector, entityID); err != nil {
			w.logger.Warn("Failed to save dedup record", "entity_id", entityID, "error", err)
			// Continue anyway - not critical
		}
	}

	// Save generated embedding
	dimensions := len(vector)
	model := w.embedder.Model()
	if err := w.storage.SaveGenerated(w.ctx, entityID, vector, model, dimensions); err != nil {
		w.logger.Error("Failed to save generated embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("save failed: %v", err))
		return
	}

	w.logger.Info("Embedding generated successfully", "entity_id", entityID, "dimensions", dimensions)
}

// markFailed marks an embedding as failed
func (w *Worker) markFailed(entityID, errorMsg string) {
	if err := w.storage.SaveFailed(w.ctx, entityID, errorMsg); err != nil {
		w.logger.Error("Failed to mark embedding as failed", "entity_id", entityID, "error", err)
	}
}
