package agentictools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// stubExecutor returns a fixed result content. ListTools mirrors the name
// passed in so each Test creates an executor with a unique name.
type stubExecutor struct {
	name    string
	content string
}

func (s *stubExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        s.name,
		Description: "stub for wrapping_pattern_test",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (s *stubExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: s.content}, nil
}

// wrappingExecutor delegates to inner.Execute, then transforms the result
// content. Demonstrates the recommended path for downstream consumers
// (semspec, semdragon) that need to mutate a built-in tool's behaviour
// without forking the implementation. This is the pattern the registry
// refactor was specifically designed to support without requiring a
// pre/post hook system in the framework.
type wrappingExecutor struct {
	inner agentictools.ToolExecutor
	tag   string
}

func (w *wrappingExecutor) ListTools() []agentic.ToolDefinition {
	return w.inner.ListTools()
}

func (w *wrappingExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	res, err := w.inner.Execute(ctx, call)
	if err != nil {
		return res, err
	}
	res.Content = w.tag + ":" + res.Content
	return res, nil
}

// Test A: a wrapping executor registered in a local registry transforms the
// inner executor's result. No global state involved — the registry is built
// fresh and disposed with the test.
func TestWrappingPattern_TransformsResult(t *testing.T) {
	t.Parallel()

	reg := agentictools.NewExecutorRegistry()
	wrapped := &wrappingExecutor{
		inner: &stubExecutor{name: "wrap_target", content: "ok"},
		tag:   "WRAPPED",
	}
	if err := reg.RegisterTool("wrap_target", wrapped); err != nil {
		t.Fatalf("register: %v", err)
	}

	res, err := reg.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-1",
		Name: "wrap_target",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Content != "WRAPPED:ok" {
		t.Fatalf("Content = %q, want WRAPPED:ok", res.Content)
	}
}

// Test B: registering two executors under the same name is a hard error.
// Replaces the prior swallow-on-duplicate behaviour from the deleted
// registerGlobal wrapper. Boot-time misconfigurations (two packages
// claiming the same tool name) now surface immediately.
func TestWrappingPattern_DuplicateRegistrationErrors(t *testing.T) {
	t.Parallel()

	reg := agentictools.NewExecutorRegistry()
	first := &stubExecutor{name: "dup_target", content: "first"}
	if err := reg.RegisterTool("dup_target", first); err != nil {
		t.Fatalf("first register: %v", err)
	}

	second := &stubExecutor{name: "dup_target", content: "second"}
	err := reg.RegisterTool("dup_target", second)
	if err == nil {
		t.Fatalf("duplicate register should error, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error %q should mention already registered", err)
	}
}

// Test C: when both a component-local registry and a deps-injected shared
// registry have an executor for the same tool name, the local one wins.
// This is the "wrapping pattern" payoff: a downstream binary registers a
// thin wrapper locally on the component without disturbing the shared
// registry that other components in the process share.
func TestWrappingPattern_LocalBeatsShared(t *testing.T) {
	t.Parallel()

	// Shared registry (would be deps.ToolRegistry in production wiring).
	shared := agentictools.NewExecutorRegistry()
	implA := &stubExecutor{name: "shared_target", content: "shared"}
	if err := shared.RegisterTool("shared_target", implA); err != nil {
		t.Fatalf("shared register: %v", err)
	}

	// Build a component with the shared registry plumbed via deps.
	rawConfig, err := json.Marshal(agentictools.DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	deps := component.Dependencies{
		ToolRegistry: shared,
	}
	comp, err := agentictools.NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}

	// Register a local wrapper on the component for the same tool name.
	registrar, ok := comp.(interface {
		RegisterToolExecutor(executor agentictools.ToolExecutor) error
	})
	if !ok {
		t.Fatalf("component should implement RegisterToolExecutor")
	}
	implB := &stubExecutor{name: "shared_target", content: "local"}
	if err := registrar.RegisterToolExecutor(implB); err != nil {
		t.Fatalf("RegisterToolExecutor: %v", err)
	}

	// Confirm the shared registry was NOT mutated by the component-local
	// registration. The local override is an isolation guarantee.
	res, err := shared.Execute(context.Background(), agentic.ToolCall{
		ID:   "shared-call",
		Name: "shared_target",
	})
	if err != nil {
		t.Fatalf("shared Execute: %v", err)
	}
	if res.Content != "shared" {
		t.Fatalf("shared registry mutated: Content = %q, want shared", res.Content)
	}

	// Sanity check: the typed not-found sentinel still fires when neither
	// registry has the requested tool. Catches future drift in dispatch
	// fallback behaviour that would otherwise be silent.
	_, err = shared.Execute(context.Background(), agentic.ToolCall{
		ID:   "missing",
		Name: "no_such_tool",
	})
	if !errors.Is(err, agentic.ErrToolNotFound) {
		t.Fatalf("missing-tool dispatch should return ErrToolNotFound, got %v", err)
	}
}
