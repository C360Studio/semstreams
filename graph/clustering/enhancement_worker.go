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

// defaultSummaryFailedRetryBackoff is how long a community whose enhancement
// failed (an llm-failed record) waits before a subsequent COMMUNITY_INDEX trigger
// re-attempts it. It bounds retry pressure on a struggling LLM endpoint without a
// dedicated backoff queue: the membership hash is the identity, so the failed
// record self-serves as the retry timer.
const defaultSummaryFailedRetryBackoff = 5 * time.Minute

// EnhancementWorker consumes COMMUNITY_INDEX changes as a TRIGGER ONLY and writes
// LLM summaries to the worker-exclusive, content-addressed COMMUNITY_SUMMARIES
// store. It holds NO CommunityStorage — it structurally cannot write the partition
// bucket, which is the single-writer invariant that closes #607/#617 (ADR-087).
type EnhancementWorker struct {
	mu sync.RWMutex

	// Dependencies
	llm             *LLMSummarizer
	querier         EntityQuerier
	communityBucket jetstream.KeyValue // COMMUNITY_INDEX — watched as a TRIGGER only, never written
	summaries       SummaryStore       // COMMUNITY_SUMMARIES — the worker is its SOLE writer

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
	workers            int           // Number of concurrent workers
	llmTimeout         time.Duration // Per-request LLM timeout (default 30s)
	failedRetryBackoff time.Duration // Minimum age of an llm-failed record before retry

	// Metrics
	metrics *EnhancementMetrics

	// Logger
	logger *slog.Logger
}

// EnhancementWorkerConfig holds configuration for the enhancement worker
type EnhancementWorkerConfig struct {
	LLMSummarizer *LLMSummarizer
	Querier       EntityQuerier
	// CommunityBucket is the COMMUNITY_INDEX bucket, watched as a trigger ONLY.
	CommunityBucket jetstream.KeyValue
	// SummaryBucket is the COMMUNITY_SUMMARIES bucket the worker owns and writes.
	SummaryBucket jetstream.KeyValue
	Logger        *slog.Logger
	Registry      *metric.MetricsRegistry // Optional: for summary-worker metrics
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
	if config.Querier == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "querier is required")
	}
	if config.CommunityBucket == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "community bucket is required")
	}
	if config.SummaryBucket == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EnhancementWorker",
			"New", "summary bucket is required")
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
		llm:                config.LLMSummarizer,
		querier:            config.Querier,
		communityBucket:    config.CommunityBucket,
		summaries:          NewNATSSummaryStore(config.SummaryBucket),
		workers:            3, // Default concurrent workers
		llmTimeout:         llmTimeout,
		failedRetryBackoff: defaultSummaryFailedRetryBackoff,
		metrics:            metrics,
		logger:             logger,
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

	// Initialize the summaries-size gauge from the store's current count. Without
	// this, updateSizeGauge only runs after a write, so a restart onto a populated
	// store where every trigger is a cache hit (no writes) would leave the gauge
	// pinned at 0 forever — violating the "summary-store volume is observable"
	// requirement. Seeding at Start makes the gauge reflect the real count on the
	// no-write path.
	w.updateSizeGauge()

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

// handleKVEntry decodes a COMMUNITY_INDEX change and dispatches it to the
// content-addressed enhancement decision. The partition bucket is a TRIGGER only:
// what work happens is decided entirely by the summary store, not by the
// COMMUNITY_INDEX record's status.
func (w *EnhancementWorker) handleKVEntry(entry jetstream.KeyValueEntry, workerID int) {
	// Skip entity mapping keys (format: entity.{level}.{entityID}) — they carry a
	// bare community ID string, not a Community record.
	if strings.HasPrefix(entry.Key(), "entity.") {
		return
	}

	// Parse the community record
	var community Community
	if err := json.Unmarshal(entry.Value(), &community); err != nil {
		w.logger.Warn("Failed to unmarshal community record", "key", entry.Key(), "error", err)
		return
	}

	// A memberless community has no content to summarize (and no membership hash
	// worth caching).
	if len(community.Members) == 0 {
		return
	}

	w.logger.Debug("Community trigger received",
		"worker_id", workerID,
		"community_id", community.ID,
		"kv_key", entry.Key(),
		"member_count", len(community.Members))

	w.enhanceCommunity(&community)
}

// enhanceCommunity applies the content-addressed enhancement decision for one
// community: compute the membership hash, read the summary store, and skip on an
// llm-enhanced hit / back off on a fresh llm-failed record / summarize on a miss.
// It NEVER writes COMMUNITY_INDEX.
func (w *EnhancementWorker) enhanceCommunity(community *Community) {
	hash := MembershipHash(community.Members)
	// Key the summary by the payload's level. The detector writes key-level ==
	// payload-level, and the graph-query cache resolves by key-normalized level,
	// so the two agree; if a future writer diverged them the join would miss and
	// fall back to the statistical floor (degraded, never corrupt).
	level := community.Level

	existing, err := w.summaries.GetSummary(w.ctx, level, hash)
	if err != nil {
		// A transient read error skips this trigger rather than stampeding the LLM:
		// the next COMMUNITY_INDEX change re-triggers and self-heals.
		w.logger.Warn("summary store read failed; skipping trigger",
			"community_id", community.ID, "level", level, "error", err)
		return
	}

	if existing != nil {
		switch existing.Status {
		case SummaryStatusEnhanced:
			// Same membership already summarized → serve it, no LLM call (#607).
			w.metrics.RecordCacheHit()
			return
		case SummaryStatusFailed:
			if time.Since(existing.GeneratedAt) < w.failedRetryBackoff {
				// Recent failure: do not hammer the endpoint before the backoff.
				return
			}
			// Backoff elapsed → fall through and retry.
		}
	}

	w.summarizeAndStore(community, hash, level)
}

// summarizeAndStore performs the LLM work for a membership miss (or a failed
// record past its backoff) and writes the result to the summary store. On any
// failure it records an llm-failed record so the backoff timer starts.
func (w *EnhancementWorker) summarizeAndStore(community *Community, hash string, level int) {
	startTime := time.Now()

	entities, err := w.fetchEntities(w.ctx, community.Members)
	if err != nil {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordFailed(latency)
		w.logger.Error("Failed to fetch entities", "community_id", community.ID, "error", err, "latency_s", latency)
		w.markFailed(community, hash, level)
		return
	}

	// Generate LLM summary with per-request timeout (content fetching happens internally via ContentFetcher)
	llmCtx, llmCancel := context.WithTimeout(w.ctx, w.llmTimeout)
	enhanced, err := w.llm.SummarizeCommunity(llmCtx, community, entities)
	llmCancel()
	// Treat an error, a nil result, an empty summary, or a graceful
	// statistical-fallback (SummaryStatus != "llm-enhanced") all as a failure: only
	// real LLM prose is worth caching as an llm-enhanced record.
	if err != nil || enhanced == nil || enhanced.LLMSummary == "" || enhanced.SummaryStatus != "llm-enhanced" {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordFailed(latency)
		w.logger.Error("Failed to generate LLM summary", "community_id", community.ID, "error", err, "latency_s", latency)
		w.markFailed(community, hash, level)
		return
	}

	rec := &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          level,
		LLMSummary:     enhanced.LLMSummary,
		Model:          w.llm.Client.Model(),
		Status:         SummaryStatusEnhanced,
		Truncated:      enhanced.SummaryTruncated,
		MemberCount:    len(community.Members),
		GeneratedAt:    time.Now(),
	}
	if err := w.summaries.PutSummary(w.ctx, rec); err != nil {
		latency := time.Since(startTime).Seconds()
		w.metrics.RecordFailed(latency)
		w.logger.Error("Failed to write summary record", "community_id", community.ID, "error", err, "latency_s", latency)
		// The write itself failed — nothing to record; the next trigger retries as a miss.
		return
	}

	latency := time.Since(startTime).Seconds()
	w.metrics.RecordGenerated(latency)
	w.updateSizeGauge()

	w.logger.Debug("Community summary generated",
		"community_id", community.ID,
		"level", level,
		"statistical_len", len(community.StatisticalSummary),
		"llm_len", len(enhanced.LLMSummary),
		"latency_s", latency)
}

// markFailed writes an llm-failed record to the summary store so the failed
// membership is retried only after the backoff. It targets ONLY the summary
// store — never COMMUNITY_INDEX.
//
// It goes through PutFailedUnlessEnhanced (NOT a blind PutSummary): the worker pool
// is concurrent and COMMUNITY_INDEX re-Puts unchanged records, so two triggers for
// the same membership hash can both miss the pre-work read and do LLM work; if one
// succeeds and the other fails later, a blind failed write would overwrite the
// llm-enhanced record. The CAS write guarantees a failure can never downgrade a
// success (ADR-087 idempotency).
func (w *EnhancementWorker) markFailed(community *Community, hash string, level int) {
	rec := &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          level,
		Status:         SummaryStatusFailed,
		MemberCount:    len(community.Members),
		GeneratedAt:    time.Now(),
	}
	if err := w.summaries.PutFailedUnlessEnhanced(w.ctx, rec); err != nil {
		w.logger.Error("Failed to write llm-failed record", "community_id", community.ID, "error", err)
		return
	}
	w.updateSizeGauge()
}

// updateSizeGauge refreshes the summaries-size gauge from the store. Called after
// each write (writes are rare — only membership misses cost anything), so a steady
// graph leaves the gauge at its last value, which is correct.
func (w *EnhancementWorker) updateSizeGauge() {
	if w.metrics == nil {
		return
	}
	n, err := w.summaries.CountSummaries(w.ctx)
	if err != nil {
		w.logger.Debug("failed to count summaries for size gauge", "error", err)
		return
	}
	w.metrics.SetSummariesSize(n)
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
