package executors

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// mockFlowEngineManager captures lifecycle calls and returns
// configurable errors so each tool path can be exercised without
// spinning up the real flowengine.Engine (which transitively requires
// the component registry, NATS, and metrics).
type mockFlowEngineManager struct {
	mu          sync.Mutex
	deployed    map[string]bool
	started     map[string]bool
	stopped     map[string]bool
	undeployed  map[string]bool
	deployErr   error
	startErr    error
	stopErr     error
	undeployErr error
}

func newMockFlowEngineManager() *mockFlowEngineManager {
	return &mockFlowEngineManager{
		deployed:   map[string]bool{},
		started:    map[string]bool{},
		stopped:    map[string]bool{},
		undeployed: map[string]bool{},
	}
}

func (m *mockFlowEngineManager) Deploy(_ context.Context, flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deployErr != nil {
		return m.deployErr
	}
	m.deployed[flowID] = true
	return nil
}

func (m *mockFlowEngineManager) Start(_ context.Context, flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.started[flowID] = true
	return nil
}

func (m *mockFlowEngineManager) Stop(_ context.Context, flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped[flowID] = true
	return nil
}

func (m *mockFlowEngineManager) Undeploy(_ context.Context, flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.undeployErr != nil {
		return m.undeployErr
	}
	m.undeployed[flowID] = true
	return nil
}

func TestFlowLifecycleExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()
	e := NewFlowLifecycleExecutor(newMockFlowEngineManager())
	tools := e.ListTools()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	expected := map[string]bool{
		"deploy_flow": true, "start_flow": true,
		"stop_flow": true, "undeploy_flow": true,
	}
	for _, tool := range tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		delete(expected, tool.Name)
		// Every lifecycle tool has the same shape: required flow_id string.
		required, _ := tool.Parameters["required"].([]string)
		if len(required) != 1 || required[0] != "flow_id" {
			t.Errorf("%s: expected required=[flow_id], got %v", tool.Name, required)
		}
	}
	if len(expected) > 0 {
		t.Errorf("missing tool names: %v", expected)
	}
}

func TestFlowLifecycleExecutor_EachTransitionCallsCorrectMethod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowEngineManager()
	e := NewFlowLifecycleExecutor(mgr)

	cases := []struct {
		toolName string
		check    func() bool
		verb     string
	}{
		{"deploy_flow", func() bool { return mgr.deployed["f1"] }, "deployed"},
		{"start_flow", func() bool { return mgr.started["f1"] }, "started"},
		{"stop_flow", func() bool { return mgr.stopped["f1"] }, "stopped"},
		{"undeploy_flow", func() bool { return mgr.undeployed["f1"] }, "undeployed"},
	}
	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			result, err := e.Execute(ctx, agentic.ToolCall{
				ID:        "c-" + tc.toolName,
				Name:      tc.toolName,
				Arguments: map[string]any{"flow_id": "f1"},
			})
			if err != nil {
				t.Fatalf("Execute %s: %v", tc.toolName, err)
			}
			if result.Error != "" {
				t.Fatalf("%s returned error: %s", tc.toolName, result.Error)
			}
			if !tc.check() {
				t.Errorf("%s did not call expected manager method", tc.toolName)
			}
			if !strings.Contains(result.Content, tc.verb) {
				t.Errorf("%s: expected content to contain %q, got: %s", tc.toolName, tc.verb, result.Content)
			}
			if !strings.Contains(result.Content, `"f1"`) {
				t.Errorf("%s: expected content to reference flow id, got: %s", tc.toolName, result.Content)
			}
		})
	}
}

func TestFlowLifecycleExecutor_MissingFlowIDFailsBeforeEngine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowEngineManager()
	mgr.deployErr = errors.New("should-not-reach")
	mgr.startErr = errors.New("should-not-reach")
	mgr.stopErr = errors.New("should-not-reach")
	mgr.undeployErr = errors.New("should-not-reach")
	e := NewFlowLifecycleExecutor(mgr)

	for _, toolName := range []string{"deploy_flow", "start_flow", "stop_flow", "undeploy_flow"} {
		t.Run(toolName, func(t *testing.T) {
			result, _ := e.Execute(ctx, agentic.ToolCall{
				ID:        "miss-" + toolName,
				Name:      toolName,
				Arguments: map[string]any{},
			})
			if result.Error == "" {
				t.Fatalf("expected error for missing flow_id")
			}
			if !strings.Contains(result.Error, "required") {
				t.Errorf("expected 'required' in error, got: %s", result.Error)
			}
			// Engine should not have been invoked — error contains
			// "should-not-reach" if it was, "required" if not.
			if strings.Contains(result.Error, "should-not-reach") {
				t.Errorf("%s called engine despite missing flow_id", toolName)
			}
		})
	}
}

// TestFlowLifecycleExecutor_EngineErrorsSurfaceAsToolErrors — when
// flowengine.Engine returns a transition-precondition violation (e.g.
// "deploy: flow not_deployed state required"), the executor must surface
// it as ToolResult.Error verbatim so the agent gets the engine's
// diagnostic rather than a tool-side rephrasing.
func TestFlowLifecycleExecutor_EngineErrorsSurfaceAsToolErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowEngineManager()
	boom := errors.New("simulated transition precondition violation")
	mgr.deployErr = boom
	mgr.startErr = boom
	mgr.stopErr = boom
	mgr.undeployErr = boom
	e := NewFlowLifecycleExecutor(mgr)

	for _, toolName := range []string{"deploy_flow", "start_flow", "stop_flow", "undeploy_flow"} {
		t.Run(toolName, func(t *testing.T) {
			result, _ := e.Execute(ctx, agentic.ToolCall{
				ID:        "err-" + toolName,
				Name:      toolName,
				Arguments: map[string]any{"flow_id": "f1"},
			})
			if result.Error == "" {
				t.Fatalf("expected Error on %s path, got none (content=%q)", toolName, result.Content)
			}
			if !strings.Contains(result.Error, "simulated transition precondition violation") {
				t.Errorf("expected engine error to surface verbatim, got: %s", result.Error)
			}
		})
	}
}

func TestFlowLifecycleExecutor_UnknownToolFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewFlowLifecycleExecutor(newMockFlowEngineManager())
	result, err := e.Execute(ctx, agentic.ToolCall{ID: "x", Name: "not_a_lifecycle_tool"})
	if err == nil {
		t.Fatalf("expected error on unknown tool")
	}
	if !strings.Contains(result.Error, "unknown tool") {
		t.Errorf("expected 'unknown tool', got: %s", result.Error)
	}
}

// TestFlowLifecycleExecutor_DuckTypingWithFlowengineEngine — compile-time
// assertion that *flowengine.Engine satisfies FlowEngineManager. Kept as
// a build-time check rather than a runtime test to avoid pulling the
// full engine dependency into the executor test scope; the assertion is
// in flow_lifecycle_engine_check_test.go (separate file for clarity).
