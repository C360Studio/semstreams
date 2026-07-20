package agenticloop

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
)

// RegionType defines the type of context region
type RegionType string

// Context region types define different areas of the conversation context
const (
	RegionSystemPrompt     RegionType = "system_prompt"     // System prompt and instructions
	RegionCompactedHistory RegionType = "compacted_history" // Summarized old conversation
	RegionRecentHistory    RegionType = "recent_history"    // Recent uncompacted messages
	RegionToolResults      RegionType = "tool_results"      // Tool execution results
	RegionHydratedContext  RegionType = "hydrated_context"  // Retrieved context from memory
	RegionGraphEntities    RegionType = "graph_entities"    // Graph entity context (multi-agent)
)

// Region priorities (higher = more important, evict lower first)
// Graph-aware slicing adjusts these based on EntityPriority config
var regionPriorities = map[RegionType]int{
	RegionToolResults:      1, // Evict first
	RegionRecentHistory:    2,
	RegionHydratedContext:  3,
	RegionGraphEntities:    4, // Entity context (adjustable via EntityPriority)
	RegionCompactedHistory: 5,
	RegionSystemPrompt:     6, // Never evict
}

type contextMessage struct {
	Message   agentic.ChatMessage
	Tokens    int
	Iteration int // For GC - which iteration this was added
}

// ContextManager manages conversation context with memory optimization
type ContextManager struct {
	loopID           string
	model            string
	modelLimit       int
	config           ContextConfig
	modelRegistry    model.RegistryReader
	regions          map[RegionType][]contextMessage
	mu               sync.RWMutex
	currentIteration int
	logger           *slog.Logger
}

// ContextManagerOption is a functional option for configuring ContextManager
type ContextManagerOption func(*ContextManager)

// WithLogger sets the logger for the ContextManager
func WithLogger(logger *slog.Logger) ContextManagerOption {
	return func(cm *ContextManager) {
		cm.logger = logger
	}
}

// WithModelRegistry provides registry-based model limit resolution
func WithModelRegistry(reg model.RegistryReader) ContextManagerOption {
	return func(cm *ContextManager) {
		cm.modelRegistry = reg
	}
}

// NewContextManager creates a new context manager
func NewContextManager(loopID, model string, config ContextConfig, opts ...ContextManagerOption) *ContextManager {
	cm := &ContextManager{
		loopID:           loopID,
		model:            model,
		config:           config,
		regions:          make(map[RegionType][]contextMessage),
		currentIteration: 1,
		logger:           slog.Default(),
	}

	// Apply functional options
	for _, opt := range opts {
		opt(cm)
	}

	// Resolve model limit with logging
	cm.modelLimit = cm.resolveModelLimit(model)

	// Initialize empty regions
	for rt := range regionPriorities {
		cm.regions[rt] = []contextMessage{}
	}

	return cm
}

// resolveModelLimit looks up the model context window size from the model registry.
func (cm *ContextManager) resolveModelLimit(modelName string) int {
	if cm.modelRegistry != nil {
		if limit := cm.modelRegistry.GetMaxTokens(modelName); limit > 0 { // modelresolveaudit:allow GetMaxTokens resolves a capability to its endpoint internally (#594 fix)
			return limit
		}
	}

	cm.logger.Warn("model not in registry, using default context limit",
		"loop_id", cm.loopID,
		"model", modelName,
		"default_limit", DefaultContextLimit,
		"hint", "add model to model_registry for explicit limit",
	)
	return DefaultContextLimit
}

// Utilization returns the current context utilization (0.0 to 1.0).
// Reserves headroom so compaction triggers before the model limit,
// leaving room for the response. Headroom is resolved as the greater
// of (HeadroomRatio * modelLimit) and HeadroomTokens (floor).
func (cm *ContextManager) Utilization() float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	total := 0
	for _, messages := range cm.regions {
		for _, m := range messages {
			total += m.Tokens
		}
	}

	headroom := cm.resolveHeadroom()
	effectiveLimit := cm.modelLimit - headroom
	if effectiveLimit <= 0 {
		cm.logger.Warn("headroom >= model_limit, ignoring headroom",
			"loop_id", cm.loopID,
			"headroom", headroom,
			"model_limit", cm.modelLimit)
		effectiveLimit = cm.modelLimit
	}

	return float64(total) / float64(effectiveLimit)
}

// resolveHeadroom returns the effective headroom in tokens.
// Uses max(ratio * modelLimit, floor) so headroom scales with the model
// but never drops below the configured floor.
func (cm *ContextManager) resolveHeadroom() int {
	ratioHeadroom := int(cm.config.HeadroomRatio * float64(cm.modelLimit))
	if ratioHeadroom > cm.config.HeadroomTokens {
		return ratioHeadroom
	}
	return cm.config.HeadroomTokens
}

// TotalTokens returns the sum of all region tokens.
func (cm *ContextManager) TotalTokens() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.getTotalTokensLocked()
}

// ModelLimit returns the resolved model context limit.
func (cm *ContextManager) ModelLimit() int {
	return cm.modelLimit
}

// ShouldCompact returns true if compaction should be triggered
func (cm *ContextManager) ShouldCompact() bool {
	return cm.Utilization() >= cm.config.CompactThreshold
}

// GetContext returns all messages in region priority order
func (cm *ContextManager) GetContext() []agentic.ChatMessage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var messages []agentic.ChatMessage

	// Order by region priority: System -> Compacted -> Graph Entities -> Hydrated -> Recent -> Tools
	order := []RegionType{
		RegionSystemPrompt,
		RegionCompactedHistory,
		RegionGraphEntities,
		RegionHydratedContext,
		RegionRecentHistory,
		RegionToolResults,
	}

	for _, rt := range order {
		for _, entry := range cm.regions[rt] {
			messages = append(messages, entry.Message)
		}
	}

	return messages
}

// AddMessage adds a message to a specific region
func (cm *ContextManager) AddMessage(region RegionType, msg agentic.ChatMessage) error {
	if _, valid := regionPriorities[region]; !valid {
		return errs.WrapInvalid(fmt.Errorf("invalid region type: %s", region), "ContextManager", "AddMessage", "validate region type")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	tokens := estimateTokens(msg.Content) + estimateTokens(msg.ReasoningContent)
	cm.regions[region] = append(cm.regions[region], contextMessage{
		Message:   msg,
		Tokens:    tokens,
		Iteration: cm.currentIteration,
	})

	return nil
}

// AdvanceIteration moves to the next iteration. Call this after processing
// each loop iteration to ensure proper age tracking for GC.
func (cm *ContextManager) AdvanceIteration() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.currentIteration++
}

// GetRegionTokens returns the total token count for a region
func (cm *ContextManager) GetRegionTokens(region RegionType) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	total := 0
	for _, m := range cm.regions[region] {
		total += m.Tokens
	}
	return total
}

// RepairToolPairs is the public entry point for the tool-pair
// integrity audit. Locks cm.mu and delegates to repairToolPairsLocked.
//
// Beta.25 hoisted this from compaction-only (it was previously called
// only by SliceForBudget) to a general-purpose pre-request safety
// net. Beta.25 C1 wired synth-result emission at every known
// terminal-transition path (failLoop, max-iter, cancel signal,
// dispatch failure) so most orphan tool_calls get matched results
// before they ever reach context. RepairToolPairs is the belt-and-
// suspenders for cases the synth-result path missed:
//   - loops restored from KV with corrupt context written by an
//     older binary version,
//   - any future failure path that adds an assistant tool_call to
//     context without ensuring matched results,
//   - third-party state mutations (e.g., manual KV edits during
//     debugging).
//
// Returns the number of messages removed; zero on the hot path
// (well-formed contexts iterate the recent-history slice once and
// return without allocations).
func (cm *ContextManager) RepairToolPairs() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.repairToolPairsLocked()
}

// repairToolPairsLocked removes orphaned tool pair messages from RegionRecentHistory.
// An assistant message with ToolCalls is orphaned if any of its tool results are missing.
// A tool result message is orphaned if its corresponding assistant message is missing.
// When an orphan is found, the entire group (assistant + all its tool results) is removed.
// Caller must hold cm.mu.
func (cm *ContextManager) repairToolPairsLocked() int {
	recent := cm.regions[RegionRecentHistory]
	if len(recent) == 0 {
		return 0
	}

	// Collect all tool call IDs offered by present assistant messages
	assistantCallIDs := make(map[string]bool)
	for _, m := range recent {
		for _, tc := range m.Message.ToolCalls {
			assistantCallIDs[tc.ID] = true
		}
	}

	// Collect all tool result IDs present
	resultIDs := make(map[string]bool)
	for _, m := range recent {
		if m.Message.Role == "tool" && m.Message.ToolCallID != "" {
			resultIDs[m.Message.ToolCallID] = true
		}
	}

	// Find assistant messages with incomplete tool results and mark their IDs as broken.
	// Any group missing even one result is broken — partial groups cause 400 errors
	// from provider APIs that require every tool_call to have a matching tool result.
	// Error results survive GC (IsError exempt from age-based eviction), so complete
	// groups with errors are naturally preserved without special-casing here.
	brokenCallIDs := make(map[string]bool)
	for _, m := range recent {
		if len(m.Message.ToolCalls) == 0 {
			continue
		}

		for _, tc := range m.Message.ToolCalls {
			if !resultIDs[tc.ID] {
				// Missing a result — mark entire group as broken
				for _, sibling := range m.Message.ToolCalls {
					brokenCallIDs[sibling.ID] = true
				}
				break
			}
		}
	}

	// Filter out orphaned and broken messages
	removed := 0
	remaining := make([]contextMessage, 0, len(recent))
	for _, m := range recent {
		drop := false

		// Drop tool results whose assistant is gone or whose group is broken
		if m.Message.Role == "tool" && m.Message.ToolCallID != "" {
			if !assistantCallIDs[m.Message.ToolCallID] || brokenCallIDs[m.Message.ToolCallID] {
				drop = true
			}
		}

		// Drop assistant messages with broken tool pairs
		if len(m.Message.ToolCalls) > 0 {
			for _, tc := range m.Message.ToolCalls {
				if brokenCallIDs[tc.ID] {
					drop = true
					break
				}
			}
		}

		if drop {
			removed++
		} else {
			remaining = append(remaining, m)
		}
	}

	if removed > 0 {
		cm.regions[RegionRecentHistory] = remaining
		cm.logger.Debug("Repaired orphaned tool pairs",
			"loop_id", cm.loopID,
			"removed", removed,
			"remaining", len(remaining))
	}
	return removed
}

// estimateTokens estimates the token count for a string
// Uses a simple heuristic of ~4 characters per token
func estimateTokens(content string) int {
	return (len(content) + 3) / 4 // Round up
}

// ContextSlice defines which regions to include when slicing context
type ContextSlice struct {
	IncludeRegions   []RegionType // Regions to include
	ExcludeRegions   []RegionType // Regions to exclude (takes precedence)
	PreserveEntities []string     // Entity IDs to always keep
}

// CheckBudget checks if current context is within the token budget.
// Returns (withinBudget, currentTokens).
func (cm *ContextManager) CheckBudget(budgetTokens int) (bool, int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if budgetTokens <= 0 {
		// No budget limit
		return true, cm.getTotalTokensLocked()
	}

	current := cm.getTotalTokensLocked()
	return current <= budgetTokens, current
}

// getTotalTokensLocked returns total tokens (must hold read lock)
func (cm *ContextManager) getTotalTokensLocked() int {
	total := 0
	for _, messages := range cm.regions {
		for _, m := range messages {
			total += m.Tokens
		}
	}
	return total
}

// SliceForBudget removes content to fit within the budget.
// Uses graph-aware slicing that preserves entity context when EntityPriority is set.
func (cm *ContextManager) SliceForBudget(budget int, slice ContextSlice) error {
	if budget <= 0 {
		return nil // No budget limit
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	current := cm.getTotalTokensLocked()
	if current <= budget {
		return nil // Already within budget
	}

	toEvict := current - budget
	evicted := 0

	// Get effective priorities (adjusted for entity priority if set)
	priorities := cm.getEffectivePriorities()

	// Build eviction order (lowest priority first)
	order := cm.buildEvictionOrder(priorities, slice)

	// Evict messages until budget is met
	for _, region := range order {
		if evicted >= toEvict {
			break
		}

		messages := cm.regions[region]
		if len(messages) == 0 {
			continue
		}

		// Evict from the end (oldest first for most regions)
		newMessages := []contextMessage{}
		for i := len(messages) - 1; i >= 0 && evicted < toEvict; i-- {
			msg := messages[i]

			// Check if this message is for a preserved entity
			if cm.shouldPreserve(msg, slice.PreserveEntities) {
				newMessages = append([]contextMessage{msg}, newMessages...)
				continue
			}

			evicted += msg.Tokens
		}

		// Keep non-evicted messages
		for i := 0; i < len(messages)-len(newMessages); i++ {
			if i < len(messages) {
				msg := messages[i]
				if cm.shouldPreserve(msg, slice.PreserveEntities) {
					newMessages = append([]contextMessage{msg}, newMessages...)
				}
			}
		}

		cm.regions[region] = newMessages
	}

	// Repair orphaned tool pairs created by the eviction above
	repaired := cm.repairToolPairsLocked()

	cm.logger.Debug("Context sliced for budget",
		"loop_id", cm.loopID,
		"budget", budget,
		"evicted_tokens", evicted,
		"repaired_orphans", repaired,
		"new_total", cm.getTotalTokensLocked())

	return nil
}

// getEffectivePriorities returns region priorities adjusted for entity priority
func (cm *ContextManager) getEffectivePriorities() map[RegionType]int {
	priorities := make(map[RegionType]int)
	for k, v := range regionPriorities {
		priorities[k] = v
	}

	// Adjust entity priority based on config
	if cm.config.EntityPriority > 0 {
		// Higher EntityPriority = higher priority for graph entities
		// Scale: 1-10 maps to priority adjustment of 0-5
		adjustment := cm.config.EntityPriority / 2
		priorities[RegionGraphEntities] += adjustment
	}

	return priorities
}

// buildEvictionOrder builds the order in which to evict regions
func (cm *ContextManager) buildEvictionOrder(priorities map[RegionType]int, slice ContextSlice) []RegionType {
	// Collect regions that can be evicted
	var evictable []RegionType
	excludeSet := make(map[RegionType]bool)
	for _, r := range slice.ExcludeRegions {
		excludeSet[r] = true
	}

	for region := range cm.regions {
		// Never evict system prompt
		if region == RegionSystemPrompt {
			continue
		}
		// Skip excluded regions
		if excludeSet[region] {
			continue
		}
		evictable = append(evictable, region)
	}

	// Sort by priority (lowest first)
	for i := 0; i < len(evictable)-1; i++ {
		for j := i + 1; j < len(evictable); j++ {
			if priorities[evictable[i]] > priorities[evictable[j]] {
				evictable[i], evictable[j] = evictable[j], evictable[i]
			}
		}
	}

	return evictable
}

// shouldPreserve checks if a message should be preserved based on entity IDs
func (cm *ContextManager) shouldPreserve(msg contextMessage, preserveEntities []string) bool {
	if len(preserveEntities) == 0 {
		return false
	}

	// Check if message content contains any preserved entity ID
	content := msg.Message.Content
	for _, entityID := range preserveEntities {
		if strings.Contains(content, entityID) {
			return true
		}
	}

	return false
}

// AddGraphEntityContext adds context from graph entities to the dedicated region
func (cm *ContextManager) AddGraphEntityContext(entityID string, content string) error {
	msg := agentic.ChatMessage{
		Role:    "system",
		Content: fmt.Sprintf("[Entity: %s]\n%s", entityID, content),
	}
	return cm.AddMessage(RegionGraphEntities, msg)
}

// GetGraphEntityTokens returns the total tokens in the graph entities region
func (cm *ContextManager) GetGraphEntityTokens() int {
	return cm.GetRegionTokens(RegionGraphEntities)
}

// ClearGraphEntities clears the graph entities region
func (cm *ContextManager) ClearGraphEntities() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.regions[RegionGraphEntities] = []contextMessage{}
}
