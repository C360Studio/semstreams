package agenticdetonator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// WildcardExecutor implements agentictools.ToolExecutor for the
// detonator sandbox. Exposes the attacker-target tool surface from
// ADR-043 line 184-189 (read_file, execute, bash, fetch, send_email,
// read_env, db_query, list_users, get_api_key) and a few adjacent
// shapes the OSINT threat model implicates (send_message,
// http_request).
//
// Each Execute call:
//   - Records the (tool, args, timestamp) into an in-memory
//     trajectory for downstream signal extraction.
//   - Returns a deterministic synthetic ToolResult keyed on
//     sha256(tool_name + canonical_args_json). Same args → same
//     fake result, so canary behavior is reproducible.
//   - Never touches the filesystem, network, or any external
//     resource. The whole point is to observe what the LLM tries
//     to do, not to do it.
//
// One executor instance per detonation. Cheap to construct (no
// dependencies) and the trajectory is the natural per-detonation
// scope. Thread-safe within a single instance.
type WildcardExecutor struct {
	mu         sync.Mutex
	calls      []RecordedCall
	tools      []agentic.ToolDefinition
	toolByName map[string]wildcardTool
}

// RecordedCall captures one observed tool invocation for downstream
// signal extraction.
type RecordedCall struct {
	ToolName  string
	Arguments map[string]any
	CallID    string
	At        time.Time
}

// wildcardTool is the internal contract between NewWildcardExecutor
// and the tool surface: name + definition + synthetic-response
// builder. Kept package-private so the tool surface can evolve
// without churning public types.
type wildcardTool struct {
	def    agentic.ToolDefinition
	respFn func(args map[string]any, argHash string) string
}

// NewWildcardExecutor returns a fresh executor with the standard
// ADR-043 attacker tool surface. The tool surface is intentionally
// fixed — adding a tool is a code change reviewed against the
// signal-bucket taxonomy, not a config-time decision.
func NewWildcardExecutor() *WildcardExecutor {
	tools := defaultWildcardTools()
	defs := make([]agentic.ToolDefinition, 0, len(tools))
	byName := make(map[string]wildcardTool, len(tools))
	for _, t := range tools {
		defs = append(defs, t.def)
		byName[t.def.Name] = t
	}
	return &WildcardExecutor{
		tools:      defs,
		toolByName: byName,
	}
}

// ListTools returns the attacker tool surface. Stable order so the
// LLM sees the same advertised tool list across detonations.
func (e *WildcardExecutor) ListTools() []agentic.ToolDefinition {
	out := make([]agentic.ToolDefinition, len(e.tools))
	copy(out, e.tools)
	return out
}

// Execute records the call into the trajectory and returns a
// deterministic synthetic result. Never errors on unknown tools
// because the LLM may try a name we didn't advertise; we record it
// and return a generic plausible-looking response so the canary
// keeps exploring.
//
// ctx is accepted to satisfy the agentictools.ToolExecutor
// interface but not consulted — this executor is pure-Go (no I/O,
// no waits) and bounded by the model-call surface. The canary loop
// re-checks ctx.Err() at every turn boundary, so a deadline hit
// mid-tool-loop converts to a clean timeout at the next turn.
// Future executors with real I/O must wire ctx through.
func (e *WildcardExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	hash := hashArgs(call.Name, call.Arguments)

	e.mu.Lock()
	e.calls = append(e.calls, RecordedCall{
		ToolName:  call.Name,
		Arguments: call.Arguments,
		CallID:    call.ID,
		At:        time.Now(),
	})
	tool, known := e.toolByName[call.Name]
	e.mu.Unlock()

	// respFn intentionally runs OUTSIDE the lock — it only reads
	// args (closed-over by value, no mutation) and hashing is
	// stateless. Future refactors that store mutable state on the
	// wildcardTool need to reacquire the lock here.
	var content string
	if known {
		content = tool.respFn(call.Arguments, hash)
	} else {
		// Unknown tool — the LLM invented a name. Record it and
		// return a generic plausible response so the canary
		// continues. The signal extractor in signals.go will
		// classify by best-effort.
		content = fmt.Sprintf("synthetic detonator response (tool=%s args_hash=%s)", call.Name, hash[:8])
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Name:    call.Name,
		Content: content,
		LoopID:  call.LoopID,
		TraceID: call.TraceID,
	}, nil
}

// Trajectory returns a snapshot of the recorded calls in observation
// order. Returned slice is a copy — caller can mutate without
// racing the executor.
func (e *WildcardExecutor) Trajectory() []RecordedCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]RecordedCall, len(e.calls))
	copy(out, e.calls)
	return out
}

// hashArgs canonicalises the (tool, args) pair into a hex sha256
// for deterministic synthetic-response keying. Args order is
// stabilised by JSON-marshalling with sorted keys; nil/empty args
// hash the same as {} so probe-style calls collapse to one
// response.
func hashArgs(toolName string, args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]any, len(args))
	for _, k := range keys {
		ordered[k] = args[k]
	}
	body, _ := json.Marshal(ordered)
	sum := sha256.Sum256(append([]byte(toolName+"|"), body...))
	return hex.EncodeToString(sum[:])
}
