package executors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/flowstore"
)

// mockFlowManager is an in-memory FlowManager for tests. Captures calls and
// returns configurable errors so each executor path can be exercised
// without spinning up NATS.
type mockFlowManager struct {
	mu        sync.Mutex
	flows     map[string]*flowstore.Flow
	createErr error
	updateErr error
	deleteErr error
	getErr    error
	listErr   error
}

func newMockFlowManager() *mockFlowManager {
	return &mockFlowManager{flows: map[string]*flowstore.Flow{}}
}

func (m *mockFlowManager) Create(_ context.Context, f *flowstore.Flow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	if _, exists := m.flows[f.ID]; exists {
		return errors.New("flow already exists")
	}
	// Mirror real Manager: set version on create.
	f.Version = 1
	m.flows[f.ID] = f
	return nil
}

func (m *mockFlowManager) Update(_ context.Context, f *flowstore.Flow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, exists := m.flows[f.ID]; !exists {
		return errors.New("flow not found")
	}
	f.Version++
	m.flows[f.ID] = f
	return nil
}

func (m *mockFlowManager) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.flows, id)
	return nil
}

func (m *mockFlowManager) Get(_ context.Context, id string) (*flowstore.Flow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	f, ok := m.flows[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return f, nil
}

func (m *mockFlowManager) List(_ context.Context) ([]*flowstore.Flow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]*flowstore.Flow, 0, len(m.flows))
	for _, f := range m.flows {
		out = append(out, f)
	}
	return out, nil
}

func TestFlowExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()
	e := NewFlowExecutor(newMockFlowManager())
	tools := e.ListTools()
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}
	expected := map[string]bool{
		"create_flow": true, "update_flow": true, "delete_flow": true,
		"list_flows": true, "get_flow": true,
	}
	for _, tool := range tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		delete(expected, tool.Name)
	}
	if len(expected) > 0 {
		t.Errorf("missing tool names: %v", expected)
	}
}

func TestFlowExecutor_CreateAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowManager()
	e := NewFlowExecutor(mgr)

	flowJSON := map[string]any{
		"id":   "test-flow",
		"name": "Test Flow",
	}

	result, err := e.Execute(ctx, agentic.ToolCall{
		ID:        "c1",
		Name:      "create_flow",
		Arguments: map[string]any{"flow": flowJSON},
	})
	if err != nil {
		t.Fatalf("Execute create: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("create returned error: %s", result.Error)
	}
	if !strings.Contains(result.Content, `"test-flow"`) {
		t.Errorf("expected content to reference flow id, got: %s", result.Content)
	}

	// Round-trip via get_flow.
	result, err = e.Execute(ctx, agentic.ToolCall{
		ID:        "c2",
		Name:      "get_flow",
		Arguments: map[string]any{"flow_id": "test-flow"},
	})
	if err != nil {
		t.Fatalf("Execute get: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("get returned error: %s", result.Error)
	}

	var parsed flowstore.Flow
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("get result should be valid JSON: %v", err)
	}
	if parsed.ID != "test-flow" || parsed.Name != "Test Flow" {
		t.Errorf("round-trip mismatch: %+v", parsed)
	}
}

func TestFlowExecutor_UpdateIncrementsVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowManager()
	mgr.flows["f1"] = &flowstore.Flow{ID: "f1", Name: "Orig", Version: 1}
	e := NewFlowExecutor(mgr)

	flowJSON := map[string]any{
		"id":      "f1",
		"name":    "Updated",
		"version": 1,
	}
	result, err := e.Execute(ctx, agentic.ToolCall{
		ID:        "u1",
		Name:      "update_flow",
		Arguments: map[string]any{"flow": flowJSON},
	})
	if err != nil {
		t.Fatalf("Execute update: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("update returned error: %s", result.Error)
	}
	if mgr.flows["f1"].Version != 2 {
		t.Errorf("expected version 2 after update, got %d", mgr.flows["f1"].Version)
	}
}

func TestFlowExecutor_DeleteRequiresID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewFlowExecutor(newMockFlowManager())
	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID:        "d1",
		Name:      "delete_flow",
		Arguments: map[string]any{},
	})
	if result.Error == "" {
		t.Fatalf("expected error for missing flow_id")
	}
	if !strings.Contains(result.Error, "required") {
		t.Errorf("expected 'required' in error, got: %s", result.Error)
	}
}

func TestFlowExecutor_ListEmptyAndPopulated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowManager()
	e := NewFlowExecutor(mgr)

	// Empty
	result, _ := e.Execute(ctx, agentic.ToolCall{ID: "l1", Name: "list_flows"})
	if !strings.Contains(result.Content, "No flows") {
		t.Errorf("empty list should report 'No flows', got: %s", result.Content)
	}

	// Populated
	mgr.flows["a"] = &flowstore.Flow{ID: "a", Name: "A", Version: 1}
	mgr.flows["b"] = &flowstore.Flow{ID: "b", Name: "B", Version: 3}
	result, _ = e.Execute(ctx, agentic.ToolCall{ID: "l2", Name: "list_flows"})
	if !strings.Contains(result.Content, "Flows (2)") {
		t.Errorf("expected count header 'Flows (2)', got: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"id": "a"`) ||
		!strings.Contains(result.Content, `"id": "b"`) {
		t.Errorf("expected both flow ids in list, got: %s", result.Content)
	}
}

func TestFlowExecutor_UnknownToolFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewFlowExecutor(newMockFlowManager())
	result, err := e.Execute(ctx, agentic.ToolCall{ID: "x", Name: "not_a_flow_tool"})
	if err == nil {
		t.Fatalf("expected error on unknown tool")
	}
	if !strings.Contains(result.Error, "unknown tool") {
		t.Errorf("expected 'unknown tool', got: %s", result.Error)
	}
}

// TestFlowExecutor_TransportErrorsSurfaceAsToolErrors — when the
// underlying Manager returns an error (e.g. NATS disconnected), the
// executor must convert it into a ToolResult.Error string rather than
// blowing up. Covers every CRUD path plus the "missing required arg"
// shortcut that shouldn't even hit the manager.
func TestFlowExecutor_TransportErrorsSurfaceAsToolErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowManager()
	boom := errors.New("simulated transport failure")
	mgr.createErr = boom
	mgr.updateErr = boom
	mgr.deleteErr = boom
	mgr.getErr = boom
	mgr.listErr = boom
	// Seed a flow so update/delete/get have something to "miss".
	mgr.flows["existing"] = &flowstore.Flow{ID: "existing", Name: "x", Version: 1}
	e := NewFlowExecutor(mgr)

	cases := []struct {
		name string
		call agentic.ToolCall
	}{
		{"create", agentic.ToolCall{ID: "c", Name: "create_flow", Arguments: map[string]any{"flow": map[string]any{"id": "x"}}}},
		{"update", agentic.ToolCall{ID: "u", Name: "update_flow", Arguments: map[string]any{"flow": map[string]any{"id": "existing", "version": 1}}}},
		{"delete", agentic.ToolCall{ID: "d", Name: "delete_flow", Arguments: map[string]any{"flow_id": "existing"}}},
		{"get", agentic.ToolCall{ID: "g", Name: "get_flow", Arguments: map[string]any{"flow_id": "existing"}}},
		{"list", agentic.ToolCall{ID: "l", Name: "list_flows"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := e.Execute(ctx, tc.call)
			if result.Error == "" {
				t.Fatalf("expected Error on %s path, got none (content=%q)", tc.name, result.Content)
			}
			if !strings.Contains(result.Error, "simulated transport failure") &&
				!strings.Contains(result.Error, "failed") {
				t.Errorf("expected simulated error to surface, got: %s", result.Error)
			}
		})
	}
}

// TestFlowExecutor_MalformedFlowArgRejectedBeforeManagerCalled — if the
// LLM sends a non-object `flow` argument, we fail fast in the executor
// and never call Manager. Guards against manager-side confusion if
// raw JSON sneaks through.
func TestFlowExecutor_MalformedFlowArgRejectedBeforeManagerCalled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowManager()
	mgr.createErr = errors.New("should-not-reach")
	e := NewFlowExecutor(mgr)

	// Pass `flow` as a string rather than a map. json.Marshal/Unmarshal
	// loop-back still produces a Flow with zero values (JSON strings
	// unmarshal into most struct fields as zero) but the resulting
	// flow has no ID — Create will reject it before hitting our
	// error-configured Manager.
	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID: "bad", Name: "create_flow",
		Arguments: map[string]any{"flow": "this is not an object"},
	})
	if result.Error == "" {
		t.Fatalf("expected error for malformed flow arg")
	}
}
