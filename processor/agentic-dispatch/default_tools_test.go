package agenticdispatch

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// dispatchStubTool is a minimal ToolExecutor satisfying the agentictools
// surface without pulling in real work. Registered globally once per test
// run via unique names to keep -count=N stable.
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

func TestResolveDefaultTools_KnownNameResolves(t *testing.T) {
	name := "dispatch_default_tools_known"
	if err := agentictools.RegisterTool(name, &dispatchStubTool{name: name}); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("register: %v", err)
	}

	got := resolveDefaultTools([]string{name}, slog.Default())
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1", len(got))
	}
	if got[0].Name != name {
		t.Fatalf("got name %q, want %q", got[0].Name, name)
	}
}

func TestResolveDefaultTools_UnknownNameDropped(t *testing.T) {
	name := "dispatch_default_tools_known2"
	if err := agentictools.RegisterTool(name, &dispatchStubTool{name: name}); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("register: %v", err)
	}

	got := resolveDefaultTools([]string{name, "no_such_tool_xyz"}, slog.Default())
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1 (unknown dropped)", len(got))
	}
	if got[0].Name != name {
		t.Fatalf("surviving tool = %q, want %q", got[0].Name, name)
	}
}

func TestResolveDefaultTools_EmptyInputReturnsNil(t *testing.T) {
	if got := resolveDefaultTools(nil, slog.Default()); got != nil {
		t.Fatalf("nil input should return nil, got %v", got)
	}
	if got := resolveDefaultTools([]string{}, slog.Default()); got != nil {
		t.Fatalf("empty input should return nil, got %v", got)
	}
}
