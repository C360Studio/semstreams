package agentictools

import (
	"context"
	"fmt"
	"sync"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// ToolExecutor defines the interface for tool executors
type ToolExecutor interface {
	Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
	ListTools() []agentic.ToolDefinition
}

// ExecutorRegistry manages tool executors and provides thread-safe registration and execution
type ExecutorRegistry struct {
	executors map[string]ToolExecutor
	mu        sync.RWMutex
}

// Compile-time assertion that *ExecutorRegistry satisfies the
// component.ToolRegistryReader interface. The component package's
// Dependencies struct holds the registry as that interface; any future
// drift in either side's signature breaks here at compile time rather
// than at first nil-shared deployment.
var _ component.ToolRegistryReader = (*ExecutorRegistry)(nil)

// NewExecutorRegistry creates a new empty executor registry
func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{
		executors: make(map[string]ToolExecutor),
	}
}

// RegisterTool registers a tool executor with the given name
// Returns an error if a tool with the same name is already registered
func (r *ExecutorRegistry) RegisterTool(name string, executor ToolExecutor) error {
	if name == "" {
		return errs.WrapInvalid(fmt.Errorf("tool name cannot be empty"), "ExecutorRegistry", "RegisterTool", "validate name")
	}
	if executor == nil {
		return errs.WrapInvalid(fmt.Errorf("executor cannot be nil"), "ExecutorRegistry", "RegisterTool", "validate executor")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.executors[name]; exists {
		return errs.WrapInvalid(fmt.Errorf("tool %q is already registered", name), "ExecutorRegistry", "RegisterTool", "check duplicate")
	}

	r.executors[name] = executor
	return nil
}

// GetTool retrieves a tool executor by name
// Returns nil if the tool is not registered
func (r *ExecutorRegistry) GetTool(name string) ToolExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.executors[name]
}

// ListTools returns all registered tool definitions
// Returns an empty slice (not nil) when no tools are registered
func (r *ExecutorRegistry) ListTools() []agentic.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Initialize with empty slice to ensure non-nil return
	tools := []agentic.ToolDefinition{}
	for _, executor := range r.executors {
		tools = append(tools, executor.ListTools()...)
	}

	return tools
}

// Execute executes a tool call using the registered executor.
//
// On a miss, returns a populated ToolResult (so callers can publish
// it as an error response without further work) plus a wrapped
// agentic.ErrToolNotFound. Callers detect the miss via
// errors.Is(err, agentic.ErrToolNotFound) instead of parsing error
// strings — replaces the string-match dispatch fallback that lived in
// component.go before the registry refactor.
func (r *ExecutorRegistry) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	r.mu.RLock()
	executor, exists := r.executors[call.Name]
	r.mu.RUnlock()

	if !exists {
		result := agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("tool %q not found", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
			LoopID:    call.LoopID,
			TraceID:   call.TraceID,
		}
		return result, fmt.Errorf("ExecutorRegistry.Execute: %w: %q", agentic.ErrToolNotFound, call.Name)
	}

	// Execute with context (supports timeout/cancellation)
	result, err := executor.Execute(ctx, call)
	// Propagate trace correlation fields
	result.LoopID = call.LoopID
	result.TraceID = call.TraceID
	return result, err
}
