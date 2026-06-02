package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// dispatchStubTool is a minimal ToolExecutor satisfying the agentictools
// surface without pulling in real work.
type dispatchStubTool struct{ name string }

func (s *dispatchStubTool) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        s.name,
		Description: "stub for default_tools test",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (s *dispatchStubTool) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "stub"}, nil
}

func newDispatchTestRegistry(t *testing.T, names ...string) *agentictools.ExecutorRegistry {
	t.Helper()
	reg := agentictools.NewExecutorRegistry()
	for _, name := range names {
		if err := reg.RegisterTool(name, &dispatchStubTool{name: name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	return reg
}

func TestResolveDefaultTools_KnownNameResolves(t *testing.T) {
	name := "dispatch_default_tools_known"
	reg := newDispatchTestRegistry(t, name)

	got := resolveDefaultTools(reg, []string{name}, slog.Default())
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1", len(got))
	}
	if got[0].Name != name {
		t.Fatalf("got name %q, want %q", got[0].Name, name)
	}
}

func TestResolveDefaultTools_UnknownNameDropped(t *testing.T) {
	name := "dispatch_default_tools_known2"
	reg := newDispatchTestRegistry(t, name)

	got := resolveDefaultTools(reg, []string{name, "no_such_tool_xyz"}, slog.Default())
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1 (unknown dropped)", len(got))
	}
	if got[0].Name != name {
		t.Fatalf("surviving tool = %q, want %q", got[0].Name, name)
	}
}

func TestResolveDefaultTools_EmptyInputReturnsNil(t *testing.T) {
	reg := newDispatchTestRegistry(t)
	if got := resolveDefaultTools(reg, nil, slog.Default()); got != nil {
		t.Fatalf("nil input should return nil, got %v", got)
	}
	if got := resolveDefaultTools(reg, []string{}, slog.Default()); got != nil {
		t.Fatalf("empty input should return nil, got %v", got)
	}
}

func TestResolveDefaultTools_NilRegistryReturnsNil(t *testing.T) {
	if got := resolveDefaultTools(nil, []string{"some_tool"}, slog.Default()); got != nil {
		t.Fatalf("nil registry should return nil, got %v", got)
	}
}

// newScopeTestComponent builds a minimally-wired Component for
// exercising scopeTaskTools end-to-end (DefaultTools config + tool
// registry deps). Mirrors newTestComponent in http_loops_test.go but
// also wires deps.ToolRegistry, which scopeTaskTools needs to resolve
// names to ToolDefinitions.
func newScopeTestComponent(t *testing.T, defaultTools []string, registered ...string) *Component {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DefaultTools = defaultTools
	reg := newDispatchTestRegistry(t, registered...)
	return &Component{
		config: cfg,
		deps:   component.Dependencies{ToolRegistry: reg},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestScopeTaskTools_NilDefaultTools_LeavesTaskUnchanged(t *testing.T) {
	// nil DefaultTools is the operator-opt-out — the spawned loop
	// falls back to global discovery (handlers.go discoverTools).
	// scopeTaskTools must leave task.Tools as-is (nil here).
	c := newScopeTestComponent(t, nil, "any_tool")
	task := agentic.TaskMessage{}
	c.scopeTaskTools(&task)
	if task.Tools != nil {
		t.Fatalf("nil DefaultTools should leave task.Tools nil, got %v", task.Tools)
	}
}

func TestScopeTaskTools_ConfiguredDefaultTools_ScopesTask(t *testing.T) {
	// DefaultTools with registered names produces a non-nil resolved
	// slice on task.Tools. The spawned loop respects task.Tools when
	// non-nil and skips global discovery.
	c := newScopeTestComponent(t, []string{"alpha", "beta"}, "alpha", "beta", "gamma")
	task := agentic.TaskMessage{}
	c.scopeTaskTools(&task)
	if len(task.Tools) != 2 {
		t.Fatalf("want 2 scoped tools, got %d: %v", len(task.Tools), task.Tools)
	}
	names := map[string]bool{}
	for _, td := range task.Tools {
		names[td.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Fatalf("scoped set missing expected tools: %v", names)
	}
	if names["gamma"] {
		t.Fatalf("scoped set leaked unregistered name 'gamma': %v", names)
	}
}

func TestScopeTaskTools_EmptyDefaultTools_ProducesNonNilEmpty(t *testing.T) {
	// Explicit empty slice (`"default_tools": []` in flow config) is
	// the "no tools for this role" opt-in. scopeTaskTools must
	// produce a non-nil empty task.Tools so the loop respects it
	// rather than falling back to global discovery (which would
	// silently undo the operator's intent).
	c := newScopeTestComponent(t, []string{}, "alpha")
	task := agentic.TaskMessage{}
	c.scopeTaskTools(&task)
	if task.Tools == nil {
		t.Fatalf("empty DefaultTools should produce non-nil empty task.Tools, got nil")
	}
	if len(task.Tools) != 0 {
		t.Fatalf("empty DefaultTools should produce zero-length task.Tools, got %d: %v", len(task.Tools), task.Tools)
	}
}

func TestScopeTaskTools_UnknownName_DroppedFromScope(t *testing.T) {
	// Names not in the agentictools registry log + drop; the
	// surviving scope contains only registered names. Same contract
	// as resolveDefaultTools but verified through scopeTaskTools so
	// both layers stay in sync.
	c := newScopeTestComponent(t, []string{"alpha", "no_such_xyz"}, "alpha")
	task := agentic.TaskMessage{}
	c.scopeTaskTools(&task)
	if len(task.Tools) != 1 {
		t.Fatalf("want 1 scoped tool (unknown dropped), got %d: %v", len(task.Tools), task.Tools)
	}
	if task.Tools[0].Name != "alpha" {
		t.Fatalf("scoped tool name = %q, want %q", task.Tools[0].Name, "alpha")
	}
}
