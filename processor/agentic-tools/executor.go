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
// Returns an error if a tool with the same name is already registered.
//
// For executors that expose multiple tools via ListTools(), prefer
// RegisterExecutor — calling RegisterTool in a loop with the same executor
// under different names causes ListTools() to emit duplicate tool defs (one
// per registered name × the executor's full ListTools() output). Anthropic's
// API rejects duplicates with a 400. The dedup pass in (*ExecutorRegistry).
// ListTools() now defends against this, but RegisterExecutor is the
// intended call shape for multi-tool executors.
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

// RegisterExecutor registers an executor under every tool name returned by
// its ListTools(). Atomic: validates no name collisions before committing
// any entry, and rolls back partial state if a later name conflicts.
//
// Use this for executors that expose multiple tools (e.g. GraphQueryExecutor
// with five query_* tools, RuleExecutor with five rule CRUD tools). It is
// the only registration shape that keeps ListTools() advertising and
// Execute() dispatch consistent for multi-tool executors:
//
//   - RegisterTool with one name → dispatch misses on the executor's other
//     tools (registry has only one key).
//   - RegisterTool in a loop → ListTools() emits N×N entries (one set per
//     registered key, each set being the executor's full output).
//
// RegisterExecutor maps each ListTools() entry's Name to the executor, so
// dispatch finds every name and ListTools() walks unique executors and
// dedups tool definitions by name (the latter handled in ListTools()).
//
// Returns an error if the executor is nil, ListTools() is empty, any
// returned ToolDefinition has an empty Name, or any name collides with an
// already-registered tool. On collision no entries are committed.
func (r *ExecutorRegistry) RegisterExecutor(executor ToolExecutor) error {
	if executor == nil {
		return errs.WrapInvalid(fmt.Errorf("executor cannot be nil"), "ExecutorRegistry", "RegisterExecutor", "validate executor")
	}

	defs := executor.ListTools()
	if len(defs) == 0 {
		return errs.WrapInvalid(fmt.Errorf("executor exposes no tools"), "ExecutorRegistry", "RegisterExecutor", "validate defs")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Two-pass: validate every name before committing any. Prevents partial
	// registration on the second-or-later name colliding with an existing
	// entry — caller sees the failure and can decide to abort startup.
	for _, def := range defs {
		if def.Name == "" {
			return errs.WrapInvalid(fmt.Errorf("tool definition has empty Name"), "ExecutorRegistry", "RegisterExecutor", "validate def name")
		}
		// An unrecognized effect spelling is a boot-time failure, not a
		// silent demotion to unknown: enum validation rejects unknown
		// values explicitly rather than dropping them from a derived
		// set. Empty stays legal — a producer need not classify itself.
		if def.Effect != "" && !def.Effect.Known() {
			return errs.WrapInvalid(fmt.Errorf("tool %q declares unknown effect %q", def.Name, def.Effect), "ExecutorRegistry", "RegisterExecutor", "validate def effect")
		}
		if _, exists := r.executors[def.Name]; exists {
			return errs.WrapInvalid(fmt.Errorf("tool %q is already registered", def.Name), "ExecutorRegistry", "RegisterExecutor", "check duplicate")
		}
	}
	for _, def := range defs {
		r.executors[def.Name] = executor
	}
	return nil
}

// GetTool retrieves a tool executor by name
// Returns nil if the tool is not registered
func (r *ExecutorRegistry) GetTool(name string) ToolExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.executors[name]
}

// ListTools returns all registered tool definitions, deduped by Name.
// Returns an empty slice (not nil) when no tools are registered.
//
// Two registry shapes produce the duplicates this dedup eliminates:
//
//   - RegisterTool called in a loop with the same multi-tool executor under
//     N different names. The map then has N entries pointing at one
//     executor; the per-entry call to executor.ListTools() returns the
//     executor's full set on each iteration, producing N copies.
//   - Two registry entries pointing at separate executor instances that
//     happen to expose the same tool name (rare but legal — e.g. two
//     web_search providers behind a fallback). The first wins; the second
//     is dropped silently. Use RegisterExecutor at registration time to
//     surface name collisions explicitly.
//
// Map iteration order in Go is randomized, so when a collision occurs the
// "first" winner is non-deterministic across process restarts. This is a
// known limitation; callers concerned about stability should use
// RegisterExecutor and treat duplicate-name registrations as configuration
// errors at boot.
func (r *ExecutorRegistry) ListTools() []agentic.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	tools := []agentic.ToolDefinition{}
	for _, executor := range r.executors {
		for _, def := range executor.ListTools() {
			if seen[def.Name] {
				continue
			}
			seen[def.Name] = true
			// Normalize the effect on the SERVED COPY (def is a value
			// copy; the producing executor's own definition is
			// untouched). This — not the RegisterExecutor check — is
			// what makes "every framework-served definition carries a
			// recognized effect" total, for two reasons:
			//
			//  1. This registry stores EXECUTORS, not definitions, and
			//     re-invokes executor.ListTools() live on every
			//     aggregation. What boot validated is not necessarily
			//     what is served now.
			//  2. RegisterTool(name, executor) never inspects a
			//     ToolDefinition at all, so executors registered that
			//     way never pass the RegisterExecutor check.
			//
			// Every framework consumer of tool definitions reads through
			// this one choke point. Enumerated, because the enumeration
			// IS the evidence for the guarantee:
			//   - agentic-dispatch/component.go resolveDefaultTools
			//   - agentic-loop/handlers.go discoverTools
			//   - rule/actions.go resolveToolNames (publish_agent's
			//     default_tools resolver)
			//   - agentic-tools/component.go Component.ListTools (both
			//     the local and shared branches)
			// Deliberate non-consumer: agentic-detonator/canary.go calls
			// its own executor's ListTools directly and feeds a canary
			// model, never a registry or a discovery catalog.
			def.Effect = def.Effect.Canonical()
			tools = append(tools, def)
		}
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
