package agenticloop

import (
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
)

// LoopManager manages loop entity lifecycle and state
type LoopManager struct {
	loops                map[string]*agentic.LoopEntity
	contextManagers      map[string]*ContextManager          // loopID -> ContextManager
	pendingTools         map[string]map[string]bool          // loopID -> map[callID]bool
	queuedToolCalls      map[string][]agentic.ToolCall       // loopID -> remaining calls to dispatch serially
	cachedTools          map[string][]agentic.ToolDefinition // loopID -> tools (runtime cache, not persisted)
	cachedToolChoice     map[string]*agentic.ToolChoice      // loopID -> tool choice (runtime cache, not persisted)
	cachedMetadata       map[string]map[string]any           // loopID -> metadata (domain context, not persisted)
	cachedRequestTimeout map[string]string                   // loopID -> request timeout (from TaskMessage.Timeout, not persisted)
	taskPrompts          map[string]string                   // loopID -> original task prompt (for context recovery)
	requestToLoop        map[string]string                   // requestID -> loopID
	toolCallToLoop       map[string]string                   // callID -> loopID
	callIDToName         map[string]string                   // callID -> function name (for Gemini tool result name field)
	callIDToArguments    map[string]map[string]any           // callID -> tool arguments (for trajectory audit)
	requestStartTimes    map[string]time.Time                // requestID -> start time (for duration measurement)
	toolStartTimes       map[string]time.Time                // callID -> start time (for duration measurement)
	// truncationRetryAttempts counts consecutive within-iteration retries
	// driven by length-truncation responses. Reset to 0 whenever the loop
	// makes forward progress (StatusComplete or StatusToolCall response).
	// Capped at 1 in the handler — second truncation in a row falls
	// through to a hard fail with diagnostic so a structurally-too-small
	// model doesn't burn iterations indefinitely. Runtime-only; a
	// process restart mid-retry resets to 0, which is the desired
	// behavior (the parent sees a generic loop failure and decides).
	truncationRetryAttempts map[string]int
	contextConfig           ContextConfig        // shared context config
	modelRegistry           model.RegistryReader // model registry for context managers
	logger                  *slog.Logger         // logger for context managers
	mu                      sync.RWMutex
}

// LoopManagerOption is a functional option for configuring LoopManager
type LoopManagerOption func(*LoopManager)

// WithLoopManagerLogger sets the logger for the LoopManager and its context managers
func WithLoopManagerLogger(logger *slog.Logger) LoopManagerOption {
	return func(lm *LoopManager) {
		lm.logger = logger
	}
}

// WithLoopManagerModelRegistry sets the model registry for context managers
func WithLoopManagerModelRegistry(reg model.RegistryReader) LoopManagerOption {
	return func(lm *LoopManager) {
		lm.modelRegistry = reg
	}
}

// NewLoopManager creates a new LoopManager
func NewLoopManager(opts ...LoopManagerOption) *LoopManager {
	lm := &LoopManager{
		loops:                   make(map[string]*agentic.LoopEntity),
		contextManagers:         make(map[string]*ContextManager),
		pendingTools:            make(map[string]map[string]bool),
		queuedToolCalls:         make(map[string][]agentic.ToolCall),
		cachedTools:             make(map[string][]agentic.ToolDefinition),
		cachedToolChoice:        make(map[string]*agentic.ToolChoice),
		cachedMetadata:          make(map[string]map[string]any),
		cachedRequestTimeout:    make(map[string]string),
		taskPrompts:             make(map[string]string),
		requestToLoop:           make(map[string]string),
		toolCallToLoop:          make(map[string]string),
		callIDToName:            make(map[string]string),
		callIDToArguments:       make(map[string]map[string]any),
		requestStartTimes:       make(map[string]time.Time),
		toolStartTimes:          make(map[string]time.Time),
		truncationRetryAttempts: make(map[string]int),
		contextConfig:           DefaultContextConfig(),
		logger:                  slog.Default(),
	}
	for _, opt := range opts {
		opt(lm)
	}
	return lm
}

// NewLoopManagerWithConfig creates a new LoopManager with custom context config
func NewLoopManagerWithConfig(contextConfig ContextConfig, opts ...LoopManagerOption) *LoopManager {
	lm := &LoopManager{
		loops:                   make(map[string]*agentic.LoopEntity),
		contextManagers:         make(map[string]*ContextManager),
		pendingTools:            make(map[string]map[string]bool),
		queuedToolCalls:         make(map[string][]agentic.ToolCall),
		cachedTools:             make(map[string][]agentic.ToolDefinition),
		cachedToolChoice:        make(map[string]*agentic.ToolChoice),
		cachedMetadata:          make(map[string]map[string]any),
		cachedRequestTimeout:    make(map[string]string),
		taskPrompts:             make(map[string]string),
		requestToLoop:           make(map[string]string),
		toolCallToLoop:          make(map[string]string),
		callIDToName:            make(map[string]string),
		callIDToArguments:       make(map[string]map[string]any),
		requestStartTimes:       make(map[string]time.Time),
		toolStartTimes:          make(map[string]time.Time),
		truncationRetryAttempts: make(map[string]int),
		contextConfig:           contextConfig,
		logger:                  slog.Default(),
	}
	for _, opt := range opts {
		opt(lm)
	}
	return lm
}

// CreateLoop creates a new loop entity with a generated UUID
func (m *LoopManager) CreateLoop(taskID, role, model string, maxIterations ...int) (string, error) {
	loopID := uuid.New().String()
	return m.CreateLoopWithID(loopID, taskID, role, model, maxIterations...)
}

// CreateLoopWithID creates a new loop entity with a specific ID
func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Determine max iterations
	maxIter := 20 // default
	if len(maxIterations) > 0 && maxIterations[0] > 0 {
		maxIter = maxIterations[0]
	}

	entity := agentic.NewLoopEntity(loopID, taskID, role, model, maxIter)

	m.loops[loopID] = &entity
	m.pendingTools[loopID] = make(map[string]bool)

	// Always create context manager — full conversation history is required
	// for providers like Gemini that need the assistant tool_call message
	// paired with every tool result.
	opts := []ContextManagerOption{WithLogger(m.logger)}
	if m.modelRegistry != nil {
		opts = append(opts, WithModelRegistry(m.modelRegistry))
	}
	m.contextManagers[loopID] = NewContextManager(loopID, model, m.contextConfig, opts...)

	return loopID, nil
}

// HasActiveLoopForTask returns true if a non-terminal loop already exists for the
// given task ID. This prevents duplicate loop creation on JetStream redelivery.
func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entity := range m.loops {
		if entity.TaskID == taskID && !entity.State.IsTerminal() {
			return entity.ID, true
		}
	}
	return "", false
}

// GetLoop retrieves a loop entity by ID
func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if loopID == "" {
		return agentic.LoopEntity{}, errs.WrapInvalid(fmt.Errorf("loop ID cannot be empty"), "LoopManager", "GetLoop", "validate loop ID")
	}

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "GetLoop", "find loop")
	}

	return *entity, nil
}

// UpdateLoop updates an existing loop entity
func (m *LoopManager) UpdateLoop(entity agentic.LoopEntity) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[entity.ID]; !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", entity.ID), "LoopManager", "UpdateLoop", "find loop")
	}

	m.loops[entity.ID] = &entity
	return nil
}

// IncrementTruncationRetry bumps the within-loop truncation retry
// counter and returns the new value. Caller branches on the return
// to decide between "first retry — compact and try again" (==1) and
// "already retried — fail loud" (>1). The counter is cleared by
// ResetTruncationRetry whenever the loop makes forward progress.
func (m *LoopManager) IncrementTruncationRetry(loopID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.truncationRetryAttempts[loopID]++
	return m.truncationRetryAttempts[loopID]
}

// ResetTruncationRetry clears the within-loop truncation retry
// counter. Called when the loop makes forward progress (a normal
// StatusComplete or StatusToolCall response arrives) so a future
// truncation can self-heal once.
func (m *LoopManager) ResetTruncationRetry(loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.truncationRetryAttempts, loopID)
}

// ResolveApprovalIfPending atomically transitions the loop out of
// LoopStateAwaitingApproval if and only if the supplied call_id
// matches the currently pinned PendingApproval. Returns a snapshot
// of the pending state (so the caller has the original tool name +
// arguments + trace context for re-dispatch) plus a bool indicating
// whether the resolve actually happened. A false return is the
// idempotent drop case: the loop is no longer awaiting approval, or
// the response targets a different call_id (typical when a
// duplicate UI click races with an automated reject scheduler).
//
// This is the only path that should mutate PendingApproval +
// State out of awaiting_approval after BeginAwaitingApproval. The
// previous load → mutate → UpdateLoop pattern in
// HandleApprovalResponse let two concurrent responses both pass
// the awaiting-state check and both dispatch — for a safety
// feature, that double-execution risk is unacceptable.
func (m *LoopManager) ResolveApprovalIfPending(loopID, callID string) (agentic.PendingApprovalState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.PendingApprovalState{}, false, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "ResolveApprovalIfPending", "find loop")
	}
	if entity.State != agentic.LoopStateAwaitingApproval {
		return agentic.PendingApprovalState{}, false, nil
	}
	if entity.PendingApproval == nil || entity.PendingApproval.CallID != callID {
		return agentic.PendingApprovalState{}, false, nil
	}

	pending := *entity.PendingApproval
	if err := entity.ResolveApproval(); err != nil {
		return agentic.PendingApprovalState{}, false, errs.Wrap(err, "LoopManager", "ResolveApprovalIfPending", "resolve approval")
	}
	return pending, true, nil
}

// DeleteLoop deletes a loop entity and all associated tracking data.
func (m *LoopManager) DeleteLoop(loopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.loops, loopID)
	delete(m.pendingTools, loopID)
	delete(m.queuedToolCalls, loopID)
	delete(m.contextManagers, loopID)
	delete(m.cachedTools, loopID)
	delete(m.cachedToolChoice, loopID)
	delete(m.cachedMetadata, loopID)
	delete(m.cachedRequestTimeout, loopID)
	delete(m.taskPrompts, loopID)
	delete(m.truncationRetryAttempts, loopID)

	// Clean up maps keyed by requestID/callID that embed the loopID prefix.
	// Structured IDs use format: {loopID}:req:{short} or {loopID}:tool:{short}.
	prefix := loopID + ":"
	for k := range m.requestToLoop {
		if strings.HasPrefix(k, prefix) {
			delete(m.requestToLoop, k)
			delete(m.requestStartTimes, k)
		}
	}
	for k := range m.toolCallToLoop {
		if strings.HasPrefix(k, prefix) {
			delete(m.toolCallToLoop, k)
			delete(m.callIDToName, k)
			delete(m.callIDToArguments, k)
			delete(m.toolStartTimes, k)
		}
	}
	return nil
}

// GetContextManager retrieves the context manager for a loop
func (m *LoopManager) GetContextManager(loopID string) *ContextManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contextManagers[loopID]
}

// CacheTools stores tool definitions for a loop (discovered once, reused for all requests)
func (m *LoopManager) CacheTools(loopID string, tools []agentic.ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedTools[loopID] = tools
}

// GetCachedTools retrieves the cached tool definitions for a loop
func (m *LoopManager) GetCachedTools(loopID string) []agentic.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedTools[loopID]
}

// CacheToolChoice stores the tool choice strategy for a loop (set once from task, reused for all requests)
func (m *LoopManager) CacheToolChoice(loopID string, tc *agentic.ToolChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedToolChoice[loopID] = tc
}

// GetCachedToolChoice retrieves the cached tool choice for a loop
func (m *LoopManager) GetCachedToolChoice(loopID string) *agentic.ToolChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedToolChoice[loopID]
}

// CacheMetadata stores domain context metadata for a loop (set once from task, reused for all tool calls).
// Makes a defensive copy to isolate from the caller's map.
func (m *LoopManager) CacheMetadata(loopID string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]any, len(metadata))
	for k, v := range metadata {
		cp[k] = v
	}
	m.cachedMetadata[loopID] = cp
}

// GetCachedMetadata retrieves the cached metadata for a loop
func (m *LoopManager) GetCachedMetadata(loopID string) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedMetadata[loopID]
}

// CacheRequestTimeout stores the per-request timeout for a loop (from
// TaskMessage.Timeout). Reused for all continuation iterations so the
// task-level budget persists across LLM calls in the same loop.
func (m *LoopManager) CacheRequestTimeout(loopID, timeout string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedRequestTimeout[loopID] = timeout
}

// GetCachedRequestTimeout retrieves the cached per-request timeout for a loop.
// Returns empty string when no task-level timeout was set.
func (m *LoopManager) GetCachedRequestTimeout(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedRequestTimeout[loopID]
}

// CacheTaskPrompt stores the original task prompt for context recovery.
// If GC/repair leaves the context empty, this prompt is re-injected as a
// synthetic user message so the model always has contents to work with.
func (m *LoopManager) CacheTaskPrompt(loopID, prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskPrompts[loopID] = prompt
}

// GetTaskPrompt retrieves the cached task prompt for a loop
func (m *LoopManager) GetTaskPrompt(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskPrompts[loopID]
}

// GetCurrentIteration returns the current iteration for a loop
func (m *LoopManager) GetCurrentIteration(loopID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return 0
	}
	return entity.Iterations
}

// TransitionLoop transitions a loop to a new state
func (m *LoopManager) TransitionLoop(loopID string, newState agentic.LoopState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.TransitionTo(newState)
}

// IncrementIteration increments the loop iteration counter
func (m *LoopManager) IncrementIteration(loopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.IncrementIteration()
}

// AddPendingTool adds a pending tool call to the loop
func (m *LoopManager) AddPendingTool(loopID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[loopID]; !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	if m.pendingTools[loopID] == nil {
		m.pendingTools[loopID] = make(map[string]bool)
	}

	m.pendingTools[loopID][callID] = true
	return nil
}

// RemovePendingTool removes a pending tool call from the loop
func (m *LoopManager) RemovePendingTool(loopID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pendingTools[loopID] != nil {
		delete(m.pendingTools[loopID], callID)
	}

	return nil
}

// GetPendingTools returns all pending tool calls for a loop
func (m *LoopManager) GetPendingTools(loopID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := m.pendingTools[loopID]
	if pending == nil {
		return []string{}
	}

	result := make([]string, 0, len(pending))
	for callID := range pending {
		result = append(result, callID)
	}

	return result
}

// AllToolsComplete returns true if there are no pending tool calls
func (m *LoopManager) AllToolsComplete(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := m.pendingTools[loopID]
	return len(pending) == 0
}

// QueueToolCalls stores tool calls to be dispatched serially after the current call completes.
func (m *LoopManager) QueueToolCalls(loopID string, calls []agentic.ToolCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queuedToolCalls[loopID] = append(m.queuedToolCalls[loopID], calls...)
}

// DequeueToolCall removes and returns the next queued tool call for dispatch.
func (m *LoopManager) DequeueToolCall(loopID string) (agentic.ToolCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue := m.queuedToolCalls[loopID]
	if len(queue) == 0 {
		return agentic.ToolCall{}, false
	}

	next := queue[0]
	queue[0] = agentic.ToolCall{} // zero for GC (arguments/metadata maps)
	m.queuedToolCalls[loopID] = queue[1:]
	return next, true
}

// HasQueuedTools returns true if there are tool calls waiting to be dispatched.
func (m *LoopManager) HasQueuedTools(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queuedToolCalls[loopID]) > 0
}

// ClearQueuedTools discards all queued tool calls (e.g., when StopLoop fires).
func (m *LoopManager) ClearQueuedTools(loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queuedToolCalls, loopID)
}

// TrackRequest associates a request ID with a loop ID
func (m *LoopManager) TrackRequest(requestID, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestToLoop[requestID] = loopID
}

// GetLoopForRequest retrieves the loop ID for a request ID
func (m *LoopManager) GetLoopForRequest(requestID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loopID, exists := m.requestToLoop[requestID]
	return loopID, exists
}

// TrackToolCall associates a tool call ID with a loop ID
func (m *LoopManager) TrackToolCall(callID, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCallToLoop[callID] = loopID
}

// TrackToolName associates a tool call ID with its function name.
// This is used to populate the name field on tool result messages (required by Gemini).
func (m *LoopManager) TrackToolName(callID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callIDToName[callID] = name
}

// GetToolName retrieves the function name for a tool call ID.
func (m *LoopManager) GetToolName(callID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.callIDToName[callID]
}

// TrackToolArguments associates a tool call ID with its arguments.
// This is used to populate the ToolArguments field on trajectory steps for audit.
func (m *LoopManager) TrackToolArguments(callID string, args map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callIDToArguments[callID] = args
}

// GetToolArguments retrieves a shallow copy of the arguments for a tool call ID.
func (m *LoopManager) GetToolArguments(callID string) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	orig := m.callIDToArguments[callID]
	if orig == nil {
		return nil
	}
	cp := make(map[string]any, len(orig))
	maps.Copy(cp, orig)
	return cp
}

// TrackRequestStart records when a model request was sent.
func (m *LoopManager) TrackRequestStart(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestStartTimes[requestID] = time.Now()
}

// GetRequestStart retrieves the start time for a model request.
func (m *LoopManager) GetRequestStart(requestID string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.requestStartTimes[requestID]
}

// TrackToolStart records when a tool call was dispatched for execution.
func (m *LoopManager) TrackToolStart(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolStartTimes[callID] = time.Now()
}

// GetToolStart retrieves the start time for a tool call.
func (m *LoopManager) GetToolStart(callID string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.toolStartTimes[callID]
}

// GetLoopForToolCall retrieves the loop ID for a tool call ID
func (m *LoopManager) GetLoopForToolCall(callID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loopID, exists := m.toolCallToLoop[callID]
	return loopID, exists
}

// StoreToolResult stores a tool result in the loop entity for later retrieval
func (m *LoopManager) StoreToolResult(loopID string, result agentic.ToolResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	if entity.PendingToolResults == nil {
		entity.PendingToolResults = make(map[string]agentic.ToolResult)
	}
	entity.PendingToolResults[result.CallID] = result
	return nil
}

// GetAndClearToolResults retrieves all accumulated tool results and clears them
func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return nil
	}

	results := make([]agentic.ToolResult, 0, len(entity.PendingToolResults))
	for _, r := range entity.PendingToolResults {
		results = append(results, r)
	}
	entity.PendingToolResults = nil
	return results
}

// SetTimeout sets the timeout for a loop
func (m *LoopManager) SetTimeout(loopID string, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	now := time.Now()
	entity.StartedAt = now
	entity.TimeoutAt = now.Add(timeout)
	return nil
}

// IsTimedOut checks if a loop has exceeded its timeout
func (m *LoopManager) IsTimedOut(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return false
	}

	// If no timeout set, not timed out
	if entity.TimeoutAt.IsZero() {
		return false
	}

	return time.Now().After(entity.TimeoutAt)
}

// SetParentLoop sets the parent loop ID for tracking architect->editor relationships
func (m *LoopManager) SetParentLoop(loopID, parentLoopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.ParentLoopID = parentLoopID
	return nil
}

// SetParentLoopID is an alias for SetParentLoop for consistency with TaskMessage field names
func (m *LoopManager) SetParentLoopID(loopID, parentLoopID string) error {
	return m.SetParentLoop(loopID, parentLoopID)
}

// SetDepth sets the depth tracking for a loop in the multi-agent hierarchy
func (m *LoopManager) SetDepth(loopID string, depth, maxDepth int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.Depth = depth
	entity.MaxDepth = maxDepth
	return nil
}

// GetDepth returns the current depth and max depth for a loop
func (m *LoopManager) GetDepth(loopID string) (depth, maxDepth int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return 0, 0, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.Depth, entity.MaxDepth, nil
}

// SetWorkflowContext sets the workflow slug and step for loops created by workflow commands
func (m *LoopManager) SetWorkflowContext(loopID, workflowSlug, workflowStep string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.WorkflowSlug = workflowSlug
	entity.WorkflowStep = workflowStep
	return nil
}

// SetUserContext sets the user routing info for error notifications
func (m *LoopManager) SetUserContext(loopID, channelType, channelID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.ChannelType = channelType
	entity.ChannelID = channelID
	entity.UserID = userID
	return nil
}

// SetMetadata sets domain context metadata on the loop entity.
func (m *LoopManager) SetMetadata(loopID string, metadata map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	// Defensive copy to isolate from caller's map
	cp := make(map[string]any, len(metadata))
	for k, v := range metadata {
		cp[k] = v
	}
	entity.Metadata = cp
	return nil
}

// GenerateRequestID creates a structured request ID that embeds the loop ID.
// Format: loopID:req:shortUUID
// This allows recovery of loop ID from request ID if in-memory maps are lost.
func (m *LoopManager) GenerateRequestID(loopID string) string {
	shortID := uuid.New().String()[:8]
	return fmt.Sprintf("%s:req:%s", loopID, shortID)
}

// GenerateToolCallID creates a structured tool call ID that embeds the loop ID.
// Format: loopID:tool:shortUUID
// This allows recovery of loop ID from tool call ID if in-memory maps are lost.
func (m *LoopManager) GenerateToolCallID(loopID string) string {
	shortID := uuid.New().String()[:8]
	return fmt.Sprintf("%s:tool:%s", loopID, shortID)
}

// ExtractLoopIDFromRequest extracts the loop ID from a structured request ID.
// Returns empty string if the ID is not in structured format.
func (m *LoopManager) ExtractLoopIDFromRequest(requestID string) string {
	parts := strings.Split(requestID, ":req:")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// ExtractLoopIDFromToolCall extracts the loop ID from a structured tool call ID.
// Returns empty string if the ID is not in structured format.
func (m *LoopManager) ExtractLoopIDFromToolCall(toolCallID string) string {
	parts := strings.Split(toolCallID, ":tool:")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// GetLoopForRequestWithRecovery retrieves the loop ID for a request ID,
// attempting recovery from structured ID if not found in cache.
func (m *LoopManager) GetLoopForRequestWithRecovery(requestID string) (string, bool) {
	// Try cache first
	if loopID, exists := m.GetLoopForRequest(requestID); exists {
		return loopID, true
	}

	// Try to extract from structured ID
	if loopID := m.ExtractLoopIDFromRequest(requestID); loopID != "" {
		// Verify loop exists
		m.mu.RLock()
		_, exists := m.loops[loopID]
		m.mu.RUnlock()
		if exists {
			// Re-establish the mapping
			m.TrackRequest(requestID, loopID)
			return loopID, true
		}
	}

	return "", false
}

// GetLoopForToolCallWithRecovery retrieves the loop ID for a tool call ID,
// attempting recovery from structured ID if not found in cache.
func (m *LoopManager) GetLoopForToolCallWithRecovery(toolCallID string) (string, bool) {
	// Try cache first
	if loopID, exists := m.GetLoopForToolCall(toolCallID); exists {
		return loopID, true
	}

	// Try to extract from structured ID
	if loopID := m.ExtractLoopIDFromToolCall(toolCallID); loopID != "" {
		// Verify loop exists
		m.mu.RLock()
		_, exists := m.loops[loopID]
		m.mu.RUnlock()
		if exists {
			// Re-establish the mapping
			m.TrackToolCall(toolCallID, loopID)
			return loopID, true
		}
	}

	return "", false
}

// UpdateCompletion updates a loop with completion data (outcome, result, error).
// This is called when a loop finishes to populate fields for SSE delivery via KV watch.
func (m *LoopManager) UpdateCompletion(loopID, outcome, result, errMsg string) error {
	if !isValidOutcome(outcome) {
		return errs.WrapInvalid(fmt.Errorf("invalid outcome: %s", outcome), "LoopManager", "UpdateCompletion", "validate outcome")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.Outcome = outcome
	entity.Result = result
	entity.Error = errMsg
	entity.CompletedAt = time.Now()
	return nil
}

// isValidOutcome checks if the outcome is one of the valid constants.
func isValidOutcome(outcome string) bool {
	switch outcome {
	case agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeCancelled, agentic.OutcomeTruncated:
		return true
	default:
		return false
	}
}

// CancelLoop atomically cancels a loop and populates completion data.
// Returns the updated entity for further processing, or an error if the loop
// cannot be cancelled (not found or already terminal).
func (m *LoopManager) CancelLoop(loopID, cancelledBy string) (agentic.LoopEntity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "CancelLoop", "find loop")
	}

	if entity.State.IsTerminal() {
		return agentic.LoopEntity{}, errs.WrapInvalid(
			fmt.Errorf("cannot cancel terminal loop %s in state %s", loopID, entity.State),
			"LoopManager",
			"CancelLoop",
			"check loop state",
		)
	}

	now := time.Now()
	entity.State = agentic.LoopStateCancelled
	entity.CancelledBy = cancelledBy
	entity.CancelledAt = now
	entity.Outcome = agentic.OutcomeCancelled
	entity.CompletedAt = now
	entity.Error = "cancelled by user"

	// Clear queued tool calls so no further tools are dispatched.
	delete(m.queuedToolCalls, loopID)

	return *entity, nil
}
