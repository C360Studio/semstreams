package graphclustering

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/processor/graph/querymanager"
	"github.com/nats-io/nats.go/jetstream"
)

// EnhancementWorker handles asynchronous LLM enhancement of community summaries via KV watch
type EnhancementWorker struct {
	mu sync.RWMutex

	// Dependencies
	storage       CommunityStorage
	llm           *HTTPLLMSummarizer
	provider      GraphProvider
	querier       querymanager.Querier
	communityBucket jetstream.KeyValue

	// KV watching
	watcher jetstream.KeyWatcher

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

// EnhancementWorkerConfig holds configuration for the enhancement worker
type EnhancementWorkerConfig struct {
	LLMSummarizer   *HTTPLLMSummarizer
	Storage         CommunityStorage
	GraphProvider   GraphProvider
	Querier         querymanager.Querier
	CommunityBucket jetstream.KeyValue
	Logger          *slog.Logger
}

// NewEnhancementWorker creates a new enhancement worker
func NewEnhancementWorker(config *EnhancementWorkerConfig) (*EnhancementWorker, error) {
	if config.LLMSummarizer == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "EnhancementWorker",
			"New", "LLM summarizer is required")
	}
	if config.Storage == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "EnhancementWorker",
			"New", "storage is required")
	}
	if config.GraphProvider == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "EnhancementWorker",
			"New", "graph provider is required")
	}
	if config.Querier == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "EnhancementWorker",
			"New", "querier is required")
	}
	if config.CommunityBucket == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "EnhancementWorker",
			"New", "community bucket is required")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &EnhancementWorker{
		storage:         config.Storage,
		llm:             config.LLMSummarizer,
		provider:        config.GraphProvider,
		querier:         config.Querier,
		communityBucket: config.CommunityBucket,
		workers:         3, // Default concurrent workers
		logger:          logger,
	}, nil
}

// WithWorkers sets the number of concurrent workers
func (w *EnhancementWorker) WithWorkers(n int) *EnhancementWorker {
	w.workers = n
	return w
}

// Start begins watching for communities needing LLM enhancement
func (w *EnhancementWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("enhancement worker already started")
	}

	// Create context for the worker
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Start KV watcher for COMMUNITY_INDEX
	watcher, err := w.communityBucket.WatchAll(w.ctx)
	if err != nil {
		w.cancel()
		return errors.WrapTransient(err, "EnhancementWorker", "Start", "failed to create KV watcher")
	}
	w.watcher = watcher

	// Start worker goroutines
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func(workerID int) {
			defer w.wg.Done()
			w.processCommunities(workerID)
		}(i)
	}

	w.started = true
	w.logger.Info("Enhancement worker started", "workers", w.workers)
	return nil
}

// processCommunities watches for KV changes and processes communities needing enhancement
func (w *EnhancementWorker) processCommunities(workerID int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Enhancement worker panic recovered", "worker_id", workerID, "panic", r)
		}
	}()

	w.logger.Debug("Enhancement worker goroutine started", "worker_id", workerID)

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Enhancement worker context cancelled", "worker_id", workerID)
			return

		case entry, ok := <-w.watcher.Updates():
			if !ok {
				w.logger.Debug("KV watcher updates channel closed", "worker_id", workerID)
				return
			}

			if entry == nil {
				continue
			}

			// Process if this is a community record (not entity mapping)
			if entry.Operation() == jetstream.KeyValuePut {
				w.handleKVEntry(entry, workerID)
			}
		}
	}
}

// handleKVEntry processes a KV entry to check if it needs LLM enhancement
func (w *EnhancementWorker) handleKVEntry(entry jetstream.KeyValueEntry, workerID int) {
	// Skip entity mapping keys (graph.community.entity.*)
	if len(entry.Key()) > 0 && entry.Key()[0:len("graph.community.entity.")] == "graph.community.entity." {
		return
	}

	// Parse the community record
	var community Community
	if err := json.Unmarshal(entry.Value(), &community); err != nil {
		w.logger.Warn("Failed to unmarshal community record", "key", entry.Key(), "error", err)
		return
	}

	// Only process communities with status="statistical" (need LLM enhancement)
	if community.SummaryStatus != "statistical" {
		return
	}

	// Use community.ID from the record, not entry.Key() (which is the full KV path)
	communityID := community.ID
	w.logger.Debug("Processing community for LLM enhancement",
		"worker_id", workerID,
		"community_id", communityID,
		"kv_key", entry.Key(),
		"member_count", len(community.Members))

	// Fetch entities for enhancement
	entities, err := w.fetchEntities(w.ctx, community.Members)
	if err != nil {
		w.logger.Error("Failed to fetch entities", "community_id", communityID, "error", err)
		w.markFailed(communityID, fmt.Sprintf("fetch entities failed: %v", err))
		return
	}

	// Generate LLM summary
	enhanced, err := w.llm.SummarizeCommunity(w.ctx, &community, entities)
	if err != nil {
		w.logger.Error("Failed to generate LLM summary", "community_id", communityID, "error", err)
		w.markFailed(communityID, fmt.Sprintf("LLM generation failed: %v", err))
		return
	}

	// Preserve statistical summary, add LLM summary
	community.LLMSummary = enhanced.LLMSummary
	community.SummaryStatus = "llm-enhanced"

	// Save enhanced community
	if err := w.storage.SaveCommunity(w.ctx, &community); err != nil {
		w.logger.Error("Failed to save enhanced community", "community_id", communityID, "error", err)
		w.markFailed(communityID, fmt.Sprintf("save failed: %v", err))
		return
	}

	w.logger.Info("Community enhanced with LLM summary",
		"community_id", communityID,
		"statistical_len", len(community.StatisticalSummary),
		"llm_len", len(community.LLMSummary))
}

// markFailed marks a community enhancement as failed
func (w *EnhancementWorker) markFailed(communityID, errorMsg string) {
	community, err := w.storage.GetCommunity(w.ctx, communityID)
	if err != nil || community == nil {
		w.logger.Error("Failed to fetch community for failed status", "community_id", communityID, "error", err)
		return
	}

	community.SummaryStatus = "llm-failed"
	if err := w.storage.SaveCommunity(w.ctx, community); err != nil {
		w.logger.Error("Failed to save llm-failed status", "community_id", communityID, "error", err)
	}
}

// fetchEntities retrieves entities from the graph provider using QueryManager
func (w *EnhancementWorker) fetchEntities(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error) {
	entities, err := w.querier.GetEntities(ctx, entityIDs)
	if err != nil {
		return nil, errors.WrapTransient(err, "EnhancementWorker",
			"fetchEntities", "failed to fetch entities from QueryManager")
	}
	return entities, nil
}

// Stop gracefully stops the enhancement worker
func (w *EnhancementWorker) Stop() error {
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
	w.logger.Info("Enhancement worker stopped")
	return nil
}
