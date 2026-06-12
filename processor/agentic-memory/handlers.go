package agenticmemory

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// ContextEvent is the compaction event shape this handler decodes. It is a
// type alias for agentic.ContextEvent (the canonical definition publishers
// produce) so any future field added on the agentic side propagates here
// automatically — no more silent drift between mirror and original.
//
// Local-package call sites and integration tests that reference
// `agenticmemory.ContextEvent{...}` keep working since the alias is the
// same underlying type.
type ContextEvent = agentic.ContextEvent

// HydrateRequest represents an explicit hydration request message
type HydrateRequest struct {
	LoopID          string `json:"loop_id"`
	TaskDescription string `json:"task_description,omitempty"`
	Type            string `json:"type"` // "pre_task" or "post_compaction"
}

// handleCompactionEvent processes compaction-related events from the agent loop
func (c *Component) handleCompactionEvent(ctx context.Context, data []byte) {
	// Parse event
	var event ContextEvent
	if err := json.Unmarshal(data, &event); err != nil {
		c.logger.Error("Failed to unmarshal compaction event", "error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Validate event
	if event.LoopID == "" {
		c.logger.Error("Compaction event missing loop_id")
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Check context cancellation
	if ctx.Err() != nil {
		c.logger.Debug("Context cancelled during compaction event processing", "loop_id", event.LoopID)
		return
	}

	c.logger.Debug("Processing compaction event",
		"type", event.Type,
		"loop_id", event.LoopID,
		"iteration", event.Iteration)

	// Handle based on event type
	switch event.Type {
	case "compaction_complete":
		c.handleCompactionComplete(ctx, event)
	case "compaction_starting":
		c.handleCompactionStarting(ctx, event)
	default:
		// Unknown event types are logged but not counted as processed
		c.logger.Debug("Unknown compaction event type", "type", event.Type, "loop_id", event.LoopID)
		return
	}

	atomic.AddInt64(&c.eventsProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// handleCompactionComplete processes compaction completion events
func (c *Component) handleCompactionComplete(ctx context.Context, event ContextEvent) {
	c.logger.Debug("Compaction complete",
		"loop_id", event.LoopID,
		"iteration", event.Iteration,
		"tokens_saved", event.TokensSaved,
		"summary", event.Summary)

	// Trigger post-compaction hydration if enabled
	if !c.config.Hydration.PostCompaction.Enabled {
		c.logger.Debug("Post-compaction hydration disabled", "loop_id", event.LoopID)
		return
	}

	// Perform hydration
	hydrated, err := c.hydrator.HydratePostCompaction(ctx, event.LoopID)
	if err != nil {
		c.logger.Error("Post-compaction hydration failed",
			"loop_id", event.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Publish injected context
	if err := c.publishInjectedContext(ctx, event.LoopID, "post_compaction", hydrated); err != nil {
		c.logger.Error("Failed to publish injected context",
			"loop_id", event.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("Post-compaction context injected",
		"loop_id", event.LoopID,
		"token_count", hydrated.TokenCount)
}

// handleCompactionStarting processes compaction start events
func (c *Component) handleCompactionStarting(ctx context.Context, event ContextEvent) {
	c.logger.Debug("Compaction starting",
		"loop_id", event.LoopID,
		"iteration", event.Iteration,
		"utilization", event.Utilization)

	// Trigger fact extraction if enabled
	if !c.config.Extraction.LLMAssisted.Enabled {
		c.logger.Debug("LLM-assisted extraction disabled", "loop_id", event.LoopID)
		return
	}

	// Extract facts from context (using summary as content proxy)
	content := event.Summary
	if content == "" {
		c.logger.Debug("No content to extract from", "loop_id", event.LoopID)
		return
	}

	triples, err := c.extractor.ExtractFacts(ctx, event.LoopID, content)
	if err != nil {
		c.logger.Error("Fact extraction failed",
			"loop_id", event.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	if len(triples) == 0 {
		c.logger.Debug("No facts extracted", "loop_id", event.LoopID)
		return
	}

	// Publish graph mutations
	if err := c.publishGraphMutations(ctx, event.LoopID, "add_triples", triples); err != nil {
		c.logger.Error("Failed to publish graph mutations",
			"loop_id", event.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("Facts extracted and published",
		"loop_id", event.LoopID,
		"triple_count", len(triples))
}

// handleHydrateRequest processes explicit hydration requests
func (c *Component) handleHydrateRequest(ctx context.Context, data []byte) {
	// Parse request
	var request HydrateRequest
	if err := json.Unmarshal(data, &request); err != nil {
		c.logger.Error("Failed to unmarshal hydrate request", "error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Validate request
	if request.LoopID == "" {
		c.logger.Error("Hydrate request missing loop_id")
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Check context cancellation
	if ctx.Err() != nil {
		c.logger.Debug("Context cancelled during hydrate request processing", "loop_id", request.LoopID)
		return
	}

	c.logger.Debug("Processing hydrate request",
		"type", request.Type,
		"loop_id", request.LoopID)

	// Handle based on request type
	switch request.Type {
	case "pre_task":
		c.handlePreTaskHydration(ctx, request)
	case "post_compaction":
		c.handlePostCompactionHydration(ctx, request)
	default:
		// Unknown request types are errors and not counted as processed
		c.logger.Error("Unknown hydrate request type",
			"type", request.Type,
			"loop_id", request.LoopID)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	atomic.AddInt64(&c.eventsProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// handlePreTaskHydration processes pre-task hydration requests
func (c *Component) handlePreTaskHydration(ctx context.Context, request HydrateRequest) {
	// Validate task description is provided for pre-task requests
	if request.TaskDescription == "" {
		c.logger.Error("Pre-task hydration request missing task_description",
			"loop_id", request.LoopID)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Check if pre-task hydration is enabled
	if !c.config.Hydration.PreTask.Enabled {
		c.logger.Debug("Pre-task hydration disabled", "loop_id", request.LoopID)
		return
	}

	// Perform hydration
	hydrated, err := c.hydrator.HydratePreTask(ctx, request.LoopID, request.TaskDescription)
	if err != nil {
		c.logger.Error("Pre-task hydration failed",
			"loop_id", request.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Publish injected context
	if err := c.publishInjectedContext(ctx, request.LoopID, "pre_task", hydrated); err != nil {
		c.logger.Error("Failed to publish injected context",
			"loop_id", request.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("Pre-task context injected",
		"loop_id", request.LoopID,
		"token_count", hydrated.TokenCount)
}

// handlePostCompactionHydration processes post-compaction hydration requests
func (c *Component) handlePostCompactionHydration(ctx context.Context, request HydrateRequest) {
	// Check if post-compaction hydration is enabled
	if !c.config.Hydration.PostCompaction.Enabled {
		c.logger.Debug("Post-compaction hydration disabled", "loop_id", request.LoopID)
		return
	}

	// Perform hydration
	hydrated, err := c.hydrator.HydratePostCompaction(ctx, request.LoopID)
	if err != nil {
		c.logger.Error("Post-compaction hydration failed",
			"loop_id", request.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Publish injected context
	if err := c.publishInjectedContext(ctx, request.LoopID, "post_compaction", hydrated); err != nil {
		c.logger.Error("Failed to publish injected context",
			"loop_id", request.LoopID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("Post-compaction context injected",
		"loop_id", request.LoopID,
		"token_count", hydrated.TokenCount)
}
