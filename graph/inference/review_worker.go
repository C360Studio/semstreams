// Package inference provides structural anomaly detection for missing relationships.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/pkg/errs"
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

// ReviewWorker watches ANOMALY_INDEX and processes pending anomalies.
// Follows enhancement_worker.go patterns for lifecycle management.
type ReviewWorker struct {
	// Lifecycle methods take mu before pauseMu whenever both are needed. Worker
	// loops use pauseMu alone, so Stop may hold mu while cancelling and joining.
	mu sync.RWMutex

	// Dependencies
	storage   Storage
	llmClient llm.Client          // optional - nil if LLM disabled
	applier   RelationshipApplier // writes approved suggestions to graph
	config    ReviewConfig
	logger    *slog.Logger

	// KV watching
	anomalyBucket jetstream.KeyValue
	watcher       jetstream.KeyWatcher

	// Lifecycle
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  bool
	stopping bool

	// Pause/resume coordination (follows enhancement_worker pattern)
	pauseMu  sync.RWMutex // Guards paused state; lock order is mu -> pauseMu
	paused   bool
	pauseCh  chan struct{}
	resumeCh chan struct{}

	// Metrics
	metrics *ReviewMetrics
}

// ReviewWorkerConfig holds configuration for the review worker.
type ReviewWorkerConfig struct {
	AnomalyBucket jetstream.KeyValue
	Storage       Storage
	LLMClient     llm.Client // optional
	Applier       RelationshipApplier
	Config        ReviewConfig
	Metrics       *ReviewMetrics // optional - nil disables metrics
	Logger        *slog.Logger
}

// NewReviewWorker creates a new review worker.
func NewReviewWorker(cfg *ReviewWorkerConfig) (*ReviewWorker, error) {
	if cfg.AnomalyBucket == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "ReviewWorker", "New",
			"anomaly bucket is required")
	}
	if cfg.Storage == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "ReviewWorker", "New",
			"storage is required")
	}
	if cfg.Applier == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "ReviewWorker", "New",
			"applier is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Default workers if not set
	workers := cfg.Config.Workers
	if workers <= 0 {
		workers = 1
	}

	return &ReviewWorker{
		storage:       cfg.Storage,
		llmClient:     cfg.LLMClient, // May be nil
		applier:       cfg.Applier,
		config:        cfg.Config,
		anomalyBucket: cfg.AnomalyBucket,
		logger:        logger,
		metrics:       cfg.Metrics, // May be nil
	}, nil
}

// Start begins watching ANOMALY_INDEX for pending anomalies.
func (w *ReviewWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ReviewWorker", "Start", "context cannot be nil")
	}
	if w.started {
		return nil // Idempotent
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.pauseMu.Lock()
	w.paused = false
	w.pauseCh = nil
	w.resumeCh = make(chan struct{})
	w.pauseMu.Unlock()
	w.stopping = false

	// Create KV watcher
	watcher, err := w.anomalyBucket.WatchAll(runCtx)
	if err != nil {
		cancel()
		return errs.WrapTransient(err, "ReviewWorker", "Start",
			"failed to create KV watcher")
	}
	w.watcher = watcher
	w.cancel = cancel

	// Determine worker count
	workers := w.config.Workers
	if workers <= 0 {
		workers = 1
	}

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.processAnomalies(runCtx, i)
	}

	w.started = true
	w.logger.Info("Review worker started", "workers", workers)
	return nil
}

// Stop gracefully shuts down the worker.
func (w *ReviewWorker) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return nil // Idempotent
	}

	w.stopping = true

	if w.cancel != nil {
		w.cancel()
	}

	if w.watcher != nil {
		if err := w.watcher.Stop(); err != nil {
			if !isExpectedShutdownError(err) {
				w.logger.Warn("KV watcher stop error", "error", err)
			}
		}
	}

	w.wg.Wait()

	w.pauseMu.Lock()
	w.paused = false
	w.pauseCh = nil
	w.resumeCh = nil
	w.pauseMu.Unlock()
	w.cancel = nil
	w.started = false
	w.logger.Info("Review worker stopped")
	return nil
}

// Pause stops processing new anomalies while allowing in-flight work to complete.
func (w *ReviewWorker) Pause() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()

	if w.paused || !w.started {
		return
	}

	w.paused = true
	w.pauseCh = make(chan struct{})
	close(w.pauseCh)
	w.logger.Debug("Review worker paused")
}

// Resume allows processing to continue after a Pause.
func (w *ReviewWorker) Resume() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()

	if !w.started || !w.paused {
		return
	}

	w.paused = false
	if w.resumeCh != nil {
		close(w.resumeCh)
	}
	w.resumeCh = make(chan struct{})
	w.logger.Debug("Review worker resumed")
}

// IsPaused returns whether the worker is currently paused.
func (w *ReviewWorker) IsPaused() bool {
	w.pauseMu.RLock()
	defer w.pauseMu.RUnlock()
	return w.paused
}

// waitForResume blocks until Resume() is called or context is cancelled.
// Returns true if resumed, false if context cancelled.
func (w *ReviewWorker) waitForResume(ctx context.Context) bool {
	w.pauseMu.RLock()
	resumeCh := w.resumeCh
	w.pauseMu.RUnlock()

	select {
	case <-ctx.Done():
		return false
	case <-resumeCh:
		return true
	}
}

// processAnomalies is the main worker loop.
func (w *ReviewWorker) processAnomalies(ctx context.Context, workerID int) {
	defer w.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Review worker panic recovered",
				"worker_id", workerID, "panic", r)
		}
	}()

	w.logger.Debug("Review worker goroutine started", "worker_id", workerID)

	for {
		// Check for pause
		if w.IsPaused() {
			w.logger.Debug("Worker waiting for resume", "worker_id", workerID)
			if !w.waitForResume(ctx) {
				return
			}
		}

		select {
		case <-ctx.Done():
			w.logger.Debug("Review worker context cancelled", "worker_id", workerID)
			return

		case entry, ok := <-w.watcher.Updates():
			if !ok {
				w.logger.Debug("KV watcher updates channel closed", "worker_id", workerID)
				return
			}

			if entry == nil {
				continue
			}

			// Only process put operations (new or updated anomalies)
			if entry.Operation() == jetstream.KeyValuePut {
				w.handleKVEntry(ctx, entry, workerID)
			}
		}
	}
}

// handleKVEntry processes a single anomaly from KV.
func (w *ReviewWorker) handleKVEntry(ctx context.Context, entry jetstream.KeyValueEntry, workerID int) {
	// Parse anomaly from KV entry
	var anomaly StructuralAnomaly
	if err := json.Unmarshal(entry.Value(), &anomaly); err != nil {
		w.logger.Warn("Failed to parse anomaly", "key", entry.Key(), "error", err)
		return // Skip malformed entries
	}

	// Only process pending anomalies
	if anomaly.Status != StatusPending {
		return
	}

	// Get revision for optimistic locking
	revision := entry.Revision()

	startTime := time.Now()

	w.logger.Debug("Processing anomaly",
		"worker_id", workerID,
		"anomaly_id", anomaly.ID,
		"type", anomaly.Type,
		"confidence", anomaly.Confidence,
		"revision", revision)

	// Decision flow with confidence thresholds
	decision, reason := w.makeDecision(ctx, &anomaly)

	switch decision {
	case DecisionApprove:
		if err := w.applyAndMarkApprovedWithRevision(ctx, &anomaly, reason, revision); err != nil {
			if err == ErrConcurrentModification {
				w.logger.Debug("Anomaly already processed by another worker",
					"anomaly_id", anomaly.ID, "worker_id", workerID)
				return // Skip - another worker handled it
			}
			w.logger.Error("Failed to apply approved anomaly",
				"anomaly_id", anomaly.ID, "error", err)
			w.markFailed(ctx, &anomaly, fmt.Sprintf("apply failed: %v", err))
			w.recordMetric("failed", time.Since(startTime))
			return
		}
		w.recordMetric("approved", time.Since(startTime))

	case DecisionReject:
		if err := w.markRejectedWithRevision(ctx, &anomaly, reason, revision); err != nil {
			if err == ErrConcurrentModification {
				w.logger.Debug("Anomaly already processed by another worker",
					"anomaly_id", anomaly.ID, "worker_id", workerID)
				return
			}
			w.logger.Error("Failed to mark anomaly rejected",
				"anomaly_id", anomaly.ID, "error", err)
		}
		w.recordMetric("rejected", time.Since(startTime))

	case DecisionHumanReview:
		if err := w.markForHumanReviewWithRevision(ctx, &anomaly, reason, revision); err != nil {
			if err == ErrConcurrentModification {
				w.logger.Debug("Anomaly already processed by another worker",
					"anomaly_id", anomaly.ID, "worker_id", workerID)
				return
			}
			w.logger.Error("Failed to mark anomaly for human review",
				"anomaly_id", anomaly.ID, "error", err)
		}
		w.recordMetric("deferred", time.Since(startTime))
	}
}

// makeDecision determines how to handle an anomaly.
func (w *ReviewWorker) makeDecision(ctx context.Context, anomaly *StructuralAnomaly) (Decision, string) {
	// Auto-approve high confidence
	if anomaly.CanAutoApprove(w.config.AutoApproveThreshold) {
		return DecisionApprove, fmt.Sprintf("auto-approved: confidence %.2f >= %.2f",
			anomaly.Confidence, w.config.AutoApproveThreshold)
	}

	// Auto-reject low confidence
	if anomaly.CanAutoReject(w.config.AutoRejectThreshold) {
		return DecisionReject, fmt.Sprintf("auto-rejected: confidence %.2f <= %.2f",
			anomaly.Confidence, w.config.AutoRejectThreshold)
	}

	// LLM review if configured
	if w.llmClient != nil {
		decision, reason, err := w.llmReview(ctx, anomaly)
		if err != nil {
			w.logger.Warn("LLM review failed, falling back",
				"anomaly_id", anomaly.ID, "error", err)
			// Fall through to human review
		} else {
			return decision, reason
		}
	}

	// Fallback to human review
	if w.config.FallbackToHuman {
		return DecisionHumanReview, "confidence between thresholds, queued for human review"
	}

	// No fallback configured - auto-reject uncertain cases
	return DecisionReject, "confidence between thresholds, no LLM or human review configured"
}

// llmReview asks the LLM to review an anomaly.
func (w *ReviewWorker) llmReview(ctx context.Context, anomaly *StructuralAnomaly) (Decision, string, error) {
	// Check if context already cancelled before starting LLM call
	select {
	case <-ctx.Done():
		return DecisionHumanReview, "", ctx.Err()
	default:
	}

	timeout := w.config.ReviewTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build prompt with entity context
	prompt := w.buildReviewPrompt(anomaly)

	// Low temperature for consistent decisions
	temp := 0.1

	// LLM client already has retry built in (openai_client.go pattern)
	response, err := w.llmClient.ChatCompletion(ctx, llm.ChatRequest{
		SystemPrompt: reviewSystemPrompt,
		UserPrompt:   prompt,
		Temperature:  &temp,
	})
	if err != nil {
		return DecisionHumanReview, "", err
	}

	// Parse LLM response
	return w.parseLLMDecision(response.Content)
}

// buildReviewPrompt constructs the prompt for LLM review.
func (w *ReviewWorker) buildReviewPrompt(anomaly *StructuralAnomaly) string {
	var prompt string

	prompt += fmt.Sprintf("Anomaly Type: %s\n", anomaly.Type)
	prompt += fmt.Sprintf("Confidence: %.2f\n", anomaly.Confidence)
	prompt += fmt.Sprintf("Entity A: %s\n", anomaly.EntityA)

	if anomaly.EntityB != "" {
		prompt += fmt.Sprintf("Entity B: %s\n", anomaly.EntityB)
	}

	if anomaly.EntityAContext != "" {
		prompt += fmt.Sprintf("\nEntity A Context:\n%s\n", anomaly.EntityAContext)
	}
	if anomaly.EntityBContext != "" {
		prompt += fmt.Sprintf("\nEntity B Context:\n%s\n", anomaly.EntityBContext)
	}

	// Add evidence
	prompt += "\nEvidence:\n"
	if anomaly.Evidence.Similarity > 0 {
		prompt += fmt.Sprintf("- Semantic Similarity: %.2f\n", anomaly.Evidence.Similarity)
	}
	if anomaly.Evidence.StructuralDistance > 0 {
		prompt += fmt.Sprintf("- Structural Distance: %d hops\n", anomaly.Evidence.StructuralDistance)
	}
	if anomaly.Evidence.CoreLevel > 0 {
		prompt += fmt.Sprintf("- Core Level: %d\n", anomaly.Evidence.CoreLevel)
	}

	// Add suggestion if present
	if anomaly.Suggestion != nil {
		prompt += fmt.Sprintf("\nSuggested Relationship:\n")
		prompt += fmt.Sprintf("- From: %s\n", anomaly.Suggestion.FromEntity)
		prompt += fmt.Sprintf("- Predicate: %s\n", anomaly.Suggestion.Predicate)
		prompt += fmt.Sprintf("- To: %s\n", anomaly.Suggestion.ToEntity)
		if anomaly.Suggestion.Reasoning != "" {
			prompt += fmt.Sprintf("- Reasoning: %s\n", anomaly.Suggestion.Reasoning)
		}
	}

	prompt += "\nShould this relationship be added to the graph? Reply with APPROVE or REJECT followed by your reasoning."

	return prompt
}

// parseLLMDecision extracts the decision from LLM response.
func (w *ReviewWorker) parseLLMDecision(response string) (Decision, string, error) {
	// Simple parsing - look for APPROVE or REJECT at the start
	if len(response) == 0 {
		return DecisionHumanReview, "empty LLM response", nil
	}

	// Check for approval
	if len(response) >= 7 && (response[:7] == "APPROVE" || response[:7] == "approve") {
		reason := "LLM approved"
		if len(response) > 8 {
			reason = response[8:] // Everything after "APPROVE "
		}
		return DecisionApprove, reason, nil
	}

	// Check for rejection
	if len(response) >= 6 && (response[:6] == "REJECT" || response[:6] == "reject") {
		reason := "LLM rejected"
		if len(response) > 7 {
			reason = response[7:] // Everything after "REJECT "
		}
		return DecisionReject, reason, nil
	}

	// Unclear response - defer to human
	return DecisionHumanReview, fmt.Sprintf("unclear LLM response: %s", truncate(response, 100)), nil
}

// applyAndMarkApprovedWithRevision applies the suggestion with optimistic locking.
func (w *ReviewWorker) applyAndMarkApprovedWithRevision(ctx context.Context, anomaly *StructuralAnomaly, reason string, revision uint64) error {
	if anomaly.Suggestion == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "ReviewWorker", "applyAndMarkApproved",
			"anomaly has no suggestion to apply")
	}

	// Update status first with optimistic locking to claim this anomaly
	now := time.Now()
	anomaly.Status = StatusApplied
	anomaly.ReviewedAt = &now
	anomaly.ReviewedBy = "review_worker"
	anomaly.LLMReasoning = reason

	if err := w.storage.SaveWithRevision(ctx, anomaly, revision); err != nil {
		if err == ErrConcurrentModification {
			return err // Let caller handle
		}
		return errs.WrapTransient(err, "ReviewWorker", "applyAndMarkApproved",
			fmt.Sprintf("failed to save status for %s", anomaly.ID))
	}

	// Apply the relationship after claiming
	if err := w.applier.ApplyRelationship(ctx, anomaly.Suggestion); err != nil {
		// Relationship application failed - revert status
		anomaly.Status = StatusHumanReview
		anomaly.ReviewNotes = fmt.Sprintf("apply failed after claim: %v", err)
		_ = w.storage.Save(ctx, anomaly) // Best effort revert
		return err
	}

	w.logger.Info("Anomaly approved and applied",
		"anomaly_id", anomaly.ID,
		"type", anomaly.Type,
		"from", anomaly.Suggestion.FromEntity,
		"to", anomaly.Suggestion.ToEntity)

	return nil
}

// markRejectedWithRevision updates status with optimistic locking.
func (w *ReviewWorker) markRejectedWithRevision(ctx context.Context, anomaly *StructuralAnomaly, reason string, revision uint64) error {
	now := time.Now()
	anomaly.Status = StatusRejected
	anomaly.ReviewedAt = &now
	anomaly.ReviewedBy = "review_worker"
	anomaly.LLMReasoning = reason

	if err := w.storage.SaveWithRevision(ctx, anomaly, revision); err != nil {
		if err == ErrConcurrentModification {
			return err
		}
		return errs.WrapTransient(err, "ReviewWorker", "markRejected",
			fmt.Sprintf("failed to save rejected status for %s", anomaly.ID))
	}

	w.logger.Debug("Anomaly rejected",
		"anomaly_id", anomaly.ID,
		"type", anomaly.Type,
		"reason", reason)

	return nil
}

// markForHumanReviewWithRevision updates status with optimistic locking.
func (w *ReviewWorker) markForHumanReviewWithRevision(ctx context.Context, anomaly *StructuralAnomaly, reason string, revision uint64) error {
	anomaly.Status = StatusHumanReview
	anomaly.ReviewNotes = reason

	if err := w.storage.SaveWithRevision(ctx, anomaly, revision); err != nil {
		if err == ErrConcurrentModification {
			return err
		}
		return errs.WrapTransient(err, "ReviewWorker", "markForHumanReview",
			fmt.Sprintf("failed to save human_review status for %s", anomaly.ID))
	}

	w.logger.Debug("Anomaly queued for human review",
		"anomaly_id", anomaly.ID,
		"type", anomaly.Type,
		"reason", reason)

	return nil
}

// markFailed handles anomalies that failed to process.
func (w *ReviewWorker) markFailed(ctx context.Context, anomaly *StructuralAnomaly, reason string) {
	// For failed anomalies, mark as needing human review
	anomaly.Status = StatusHumanReview
	anomaly.ReviewNotes = fmt.Sprintf("Processing failed: %s", reason)

	if err := w.storage.Save(ctx, anomaly); err != nil {
		w.logger.Error("Failed to save failed status",
			"anomaly_id", anomaly.ID, "error", err)
	}
}

// recordMetric records a review metric if metrics are configured.
func (w *ReviewWorker) recordMetric(outcome string, duration time.Duration) {
	if w.metrics == nil {
		return
	}

	switch outcome {
	case "approved":
		w.metrics.RecordApproved(duration.Seconds())
	case "rejected":
		w.metrics.RecordRejected(duration.Seconds())
	case "deferred":
		w.metrics.RecordDeferred(duration.Seconds())
	case "failed":
		w.metrics.RecordFailed(duration.Seconds())
	}
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// reviewSystemPrompt is the system prompt for LLM review.
const reviewSystemPrompt = `You are a graph relationship reviewer. Your task is to evaluate whether a suggested relationship should be added to a knowledge graph.

Consider:
1. Does the evidence support the relationship?
2. Would this relationship add meaningful value to the graph?
3. Is the confidence level appropriate for the evidence?

Reply with either:
- APPROVE followed by your reasoning
- REJECT followed by your reasoning

Be concise but thorough in your reasoning.`
