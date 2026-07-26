// Package clustering provides graph clustering algorithms and community detection.
package clustering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// isExpectedShutdownError returns true if the error is expected during component shutdown.
func isExpectedShutdownError(err error) bool {
	if errors.Is(err, nats.ErrBadSubscription) || errors.Is(err, jetstream.ErrConsumerNotFound) {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "invalid subscription") ||
		strings.Contains(errStr, "consumer not found")
}

// EntityQuerier provides minimal interface for querying entities.
// This interface exists to avoid import cycle with querymanager package.
type EntityQuerier interface {
	GetEntities(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)
}

// EnhancementWorker handles asynchronous LLM enhancement of community summaries via KV watch
type EnhancementWorker struct {
	mu sync.RWMutex

	// Dependencies
	storage         CommunityStorage
	llm             *LLMSummarizer
	provider        Provider
	querier         EntityQuerier
	communityBucket jetstream.KeyValue

	// KV watching
	watcher jetstream.KeyWatcher

	// State
	started  bool
	stopping bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Pause/resume coordination
	pauseMu  sync.RWMutex  // Guards paused state
	paused   bool          // Whether worker is paused
	pauseCh  chan struct{} // Closed when pausing requested
	resumeCh chan struct{} // Closed when resume requested

	// Configuration
	workers    int           // Number of concurrent workers
	llmTimeout time.Duration // Per-request LLM timeout (default 30s)

	// Metrics
	metrics *EnhancementMetrics

	// Logger
	logger *slog.Logger
}

// EnhancementWorkerConfig holds configuration for the enhancement worker
type EnhancementWorkerConfig struct {
	LLMSummarizer   *LLMSummarizer
	Storage         CommunityStorage
	Provider        Provider
	Querier         EntityQuerier
	CommunityBucket jetstream.KeyValue
	Logger          *slog.Logger
	Registry        *metric.MetricsRegistry // Optional: for LLM enhancement metrics
	// LLMTimeout caps the per-call inner sub-context that wraps each LLM
	// summarization round-trip. Zero means use the 30s default. The HTTP
	// client's transport-level timeout (set via the OpenAIClient) is the
	// outer ceiling; this is the inner ctx.WithTimeout that fires first
	// for slow upstreams. Both must be at least the operator-intended
	// ceiling — see processor/graph-clustering for the wiring.
	LLMTimeout time.Duration
}

// NewEnhancementWorker creates a new enhancement worker
func NewEnhancementWorker(config *EnhancementWorkerConfig) (*EnhancementWorker, error) {
	if config.LLMSummarizer == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "LLM summarizer is required")
	}
	if config.Storage == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "storage is required")
	}
	if config.Provider == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "graph provider is required")
	}
	if config.Querier == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "querier is required")
	}
	if config.CommunityBucket == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "community bucket is required")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Initialize metrics if registry provided
	var metrics *EnhancementMetrics
	if config.Registry != nil {
		metrics = NewEnhancementMetrics("enhancement_worker", config.Registry)
	}

	llmTimeout := config.LLMTimeout
	if llmTimeout <= 0 {
		llmTimeout = 30 * time.Second
	}

	return &EnhancementWorker{
		storage:         config.Storage,
		llm:             config.LLMSummarizer,
		provider:        config.Provider,
		querier:         config.Querier,
		communityBucket: config.CommunityBucket,
		workers:         3, // Default concurrent workers
		llmTimeout:      llmTimeout,
		metrics:         metrics,
		logger:          logger,
	}, nil
}

// WithWorkers sets the number of concurrent workers.
// Must be called before Start(). Has no effect if worker is already started.
func (w *EnhancementWorker) WithWorkers(n int) *EnhancementWorker {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		w.logger.Warn("Cannot change worker count while running", "requested", n, "current", w.workers)
		return w
	}
	w.workers = n
	return w
}

// Pause stops processing new communities while allowing in-flight work to complete.
// Safe to call multiple times. Returns immediately after signaling workers to pause.
func (w *EnhancementWorker) Pause() {
	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()

	if w.paused || !w.started {
		return
	}

	w.paused = true
	w.pauseCh = make(chan struct{})
	close(w.pauseCh) // Signal workers to pause
	w.logger.Debug("Enhancement worker paused")
}

// Resume allows processing to continue after a Pause.
// Safe to call multiple times.
func (w *EnhancementWorker) Resume() {
	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()

	if !w.paused {
		return
	}

	w.paused = false
	if w.resumeCh != nil {
		close(w.resumeCh) // Signal workers to resume
	}
	w.resumeCh = make(chan struct{}) // Reset for next pause cycle
	w.logger.Debug("Enhancement worker resumed")
}

// IsPaused returns whether the worker is currently paused.
func (w *EnhancementWorker) IsPaused() bool {
	w.pauseMu.RLock()
	defer w.pauseMu.RUnlock()
	return w.paused
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

	// Initialize pause/resume channels
	w.resumeCh = make(chan struct{})

	// Start KV watcher for COMMUNITY_INDEX
	watcher, err := w.communityBucket.WatchAll(w.ctx)
	if err != nil {
		w.cancel()
		return errs.WrapTransient(err, "EnhancementWorker", "Start", "failed to create KV watcher")
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
		// Check if paused before processing
		if w.IsPaused() {
			w.logger.Debug("Worker waiting for resume", "worker_id", workerID)
			if !w.waitForResume() {
				return // Context cancelled while waiting
			}
			w.logger.Debug("Worker resumed", "worker_id", workerID)
		}

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

// waitForResume blocks until Resume() is called or context is cancelled.
// Returns true if resumed, false if context cancelled.
func (w *EnhancementWorker) waitForResume() bool {
	w.pauseMu.RLock()
	resumeCh := w.resumeCh
	w.pauseMu.RUnlock()

	select {
	case <-w.ctx.Done():
		return false
	case <-resumeCh:
		return true
	}
}

// handleKVEntry processes a KV entry to check if it needs LLM enhancement
func (w *EnhancementWorker) handleKVEntry(entry jetstream.KeyValueEntry, workerID int) {
	// Skip entity mapping keys (format: entity.{level}.{entityID})
	if strings.HasPrefix(entry.Key(), "entity.") {
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

	// Track metrics: start enhancement attempt
	startTime := time.Now()
	w.metrics.RecordEnhancementStart()
	w.metrics.IncQueueDepth()
	defer w.metrics.DecQueueDepth()

	// Fetch entities for enhancement
	entities, err := w.fetchEntities(w.ctx, community.Members)
	if err != nil {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordEnhancementFailed(latency)
		w.logger.Error("Failed to fetch entities", "community_id", communityID, "error", err, "latency_s", latency)
		w.markFailed(communityID, fmt.Sprintf("fetch entities failed: %v", err))
		return
	}

	// Generate LLM summary with per-request timeout (content fetching happens internally via ContentFetcher)
	llmCtx, llmCancel := context.WithTimeout(w.ctx, w.llmTimeout)
	enhanced, err := w.llm.SummarizeCommunity(llmCtx, &community, entities)
	llmCancel()
	if err != nil {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordEnhancementFailed(latency)
		w.logger.Error("Failed to generate LLM summary", "community_id", communityID, "error", err, "latency_s", latency)
		w.markFailed(communityID, fmt.Sprintf("LLM generation failed: %v", err))
		return
	}

	// Preserve statistical summary, add LLM summary
	community.LLMSummary = enhanced.LLMSummary
	community.SummaryStatus = "llm-enhanced"
	// Carry the truncation flag back from the summarizer's result — this worker
	// saves its own `community`, not `enhanced`, so a field set only on `enhanced`
	// would be dropped on save (B2 §8 dilution channel).
	community.SummaryTruncated = enhanced.SummaryTruncated

	// Save enhanced community
	if err := w.storage.SaveCommunity(w.ctx, &community); err != nil {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordEnhancementFailed(latency)
		w.logger.Error("Failed to save enhanced community", "community_id", communityID, "error", err, "latency_s", latency)
		w.markFailed(communityID, fmt.Sprintf("save failed: %v", err))
		return
	}

	// Record successful enhancement
	latency := time.Since(startTime).Seconds()
	w.metrics.RecordEnhancementSuccess(latency)

	w.logger.Debug("Community enhanced with LLM summary",
		"community_id", communityID,
		"statistical_len", len(community.StatisticalSummary),
		"llm_len", len(community.LLMSummary),
		"latency_s", latency)
}

// markFailed marks a community enhancement as failed
func (w *EnhancementWorker) markFailed(communityID, _ string) {
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
		return nil, errs.WrapTransient(err, "EnhancementWorker",
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

	// Wait for all goroutines to finish BEFORE stopping watcher.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine if workers are still reading.
	w.wg.Wait()

	// Stop the watcher after workers have exited
	if w.watcher != nil {
		if err := w.watcher.Stop(); err != nil {
			if !isExpectedShutdownError(err) {
				w.logger.Warn("KV watcher stop error", "error", err)
			}
		}
	}

	w.started = false
	w.logger.Info("Enhancement worker stopped")
	return nil
}
