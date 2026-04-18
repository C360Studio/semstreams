package executors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/flowtemplate"
)

type mockFlowTemplateManager struct {
	mu   sync.Mutex
	data map[string]*flowtemplate.Template
}

func newMockFlowTemplateManager() *mockFlowTemplateManager {
	return &mockFlowTemplateManager{data: map[string]*flowtemplate.Template{}}
}

func (m *mockFlowTemplateManager) Create(_ context.Context, t *flowtemplate.Template) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[t.ID]; exists {
		return errors.New("template exists")
	}
	m.data[t.ID] = t
	return nil
}

func (m *mockFlowTemplateManager) Update(_ context.Context, t *flowtemplate.Template) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[t.ID]; !exists {
		return errors.New("not found")
	}
	m.data[t.ID] = t
	return nil
}

func (m *mockFlowTemplateManager) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func (m *mockFlowTemplateManager) Get(_ context.Context, id string) (*flowtemplate.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.data[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return t, nil
}

func (m *mockFlowTemplateManager) List(_ context.Context) (map[string]*flowtemplate.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*flowtemplate.Template, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func TestFlowTemplateExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()
	e := NewFlowTemplateExecutor(newMockFlowTemplateManager())
	tools := e.ListTools()
	if len(tools) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(tools))
	}
	expected := map[string]bool{
		"create_flow_template": true, "update_flow_template": true,
		"delete_flow_template": true, "list_flow_templates": true,
		"get_flow_template": true, "instantiate_flow_template": true,
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

func TestFlowTemplateExecutor_CreateGetInstantiate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowTemplateManager()
	e := NewFlowTemplateExecutor(mgr)

	// Create
	tpl := map[string]any{
		"id":   "research-pipeline",
		"name": "Research Pipeline",
		"body": `{"id": "{{.FlowID}}", "name": "{{.FlowName}}", "nodes": [], "connections": []}`,
		"parameters": []map[string]any{
			{"name": "FlowID", "default": "research-flow"},
			{"name": "FlowName", "default": "Research"},
		},
	}
	result, err := e.Execute(ctx, agentic.ToolCall{
		ID: "c1", Name: "create_flow_template",
		Arguments: map[string]any{"template": tpl},
	})
	if err != nil || result.Error != "" {
		t.Fatalf("create: err=%v error=%s", err, result.Error)
	}

	// Instantiate with default params.
	result, err = e.Execute(ctx, agentic.ToolCall{
		ID: "i1", Name: "instantiate_flow_template",
		Arguments: map[string]any{"template_id": "research-pipeline"},
	})
	if err != nil || result.Error != "" {
		t.Fatalf("instantiate default: err=%v error=%s", err, result.Error)
	}
	if !strings.Contains(result.Content, `"id": "research-flow"`) {
		t.Errorf("expected default FlowID in rendered output, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "not persisted") {
		t.Errorf("expected 'not persisted' hint so caller knows to create_flow")
	}

	// Instantiate with override.
	result, err = e.Execute(ctx, agentic.ToolCall{
		ID: "i2", Name: "instantiate_flow_template",
		Arguments: map[string]any{
			"template_id": "research-pipeline",
			"parameters":  map[string]any{"FlowID": "custom-flow"},
		},
	})
	if err != nil || result.Error != "" {
		t.Fatalf("instantiate override: err=%v error=%s", err, result.Error)
	}
	if !strings.Contains(result.Content, `"id": "custom-flow"`) {
		t.Errorf("expected overridden FlowID, got: %s", result.Content)
	}
}

func TestFlowTemplateExecutor_GetReturnsFullTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowTemplateManager()
	mgr.data["t1"] = &flowtemplate.Template{
		ID:   "t1",
		Name: "Name",
		Body: `{"id": "x", "name": "x", "nodes": [], "connections": []}`,
	}
	e := NewFlowTemplateExecutor(mgr)

	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID: "g1", Name: "get_flow_template",
		Arguments: map[string]any{"template_id": "t1"},
	})
	if result.Error != "" {
		t.Fatalf("get: %s", result.Error)
	}

	var got flowtemplate.Template
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "t1" || got.Name != "Name" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestFlowTemplateExecutor_DeleteRequiresID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewFlowTemplateExecutor(newMockFlowTemplateManager())
	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID: "d1", Name: "delete_flow_template",
		Arguments: map[string]any{},
	})
	if !strings.Contains(result.Error, "required") {
		t.Errorf("expected 'required' error, got: %s", result.Error)
	}
}

func TestFlowTemplateExecutor_ListEmptyAndPopulated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockFlowTemplateManager()
	e := NewFlowTemplateExecutor(mgr)

	result, _ := e.Execute(ctx, agentic.ToolCall{ID: "l1", Name: "list_flow_templates"})
	if !strings.Contains(result.Content, "No flow templates") {
		t.Errorf("empty list expected 'No flow templates', got: %s", result.Content)
	}

	mgr.data["a"] = &flowtemplate.Template{ID: "a", Name: "A", Body: "{}"}
	mgr.data["b"] = &flowtemplate.Template{ID: "b", Name: "B", Body: "{}"}
	result, _ = e.Execute(ctx, agentic.ToolCall{ID: "l2", Name: "list_flow_templates"})
	if !strings.Contains(result.Content, "Flow templates (2)") {
		t.Errorf("expected 'Flow templates (2)', got: %s", result.Content)
	}
}

func TestFlowTemplateExecutor_UnknownToolFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewFlowTemplateExecutor(newMockFlowTemplateManager())
	result, err := e.Execute(ctx, agentic.ToolCall{ID: "x", Name: "nope"})
	if err == nil || !strings.Contains(result.Error, "unknown tool") {
		t.Errorf("expected unknown-tool error, got err=%v error=%s", err, result.Error)
	}
}

// errorFlowTemplateManager returns a supplied error from every method.
type errorFlowTemplateManager struct {
	err error
}

func (m *errorFlowTemplateManager) Create(_ context.Context, _ *flowtemplate.Template) error {
	return m.err
}
func (m *errorFlowTemplateManager) Update(_ context.Context, _ *flowtemplate.Template) error {
	return m.err
}
func (m *errorFlowTemplateManager) Delete(_ context.Context, _ string) error { return m.err }
func (m *errorFlowTemplateManager) Get(_ context.Context, _ string) (*flowtemplate.Template, error) {
	return nil, m.err
}
func (m *errorFlowTemplateManager) List(_ context.Context) (map[string]*flowtemplate.Template, error) {
	return nil, m.err
}

// TestFlowTemplateExecutor_TransportErrorsSurfaceAsToolErrors — Manager
// errors on each CRUD + Instantiate path land as ToolResult.Error strings.
func TestFlowTemplateExecutor_TransportErrorsSurfaceAsToolErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("simulated flow-template transport failure")
	e := NewFlowTemplateExecutor(&errorFlowTemplateManager{err: boom})

	validTemplate := map[string]any{
		"id":   "t1",
		"name": "T",
		"body": `{"id": "x", "name": "x", "nodes": [], "connections": []}`,
	}

	cases := []struct {
		name string
		call agentic.ToolCall
	}{
		{"create", agentic.ToolCall{ID: "c", Name: "create_flow_template",
			Arguments: map[string]any{"template": validTemplate}}},
		{"update", agentic.ToolCall{ID: "u", Name: "update_flow_template",
			Arguments: map[string]any{"template": validTemplate}}},
		{"delete", agentic.ToolCall{ID: "d", Name: "delete_flow_template",
			Arguments: map[string]any{"template_id": "t1"}}},
		{"get", agentic.ToolCall{ID: "g", Name: "get_flow_template",
			Arguments: map[string]any{"template_id": "t1"}}},
		{"list", agentic.ToolCall{ID: "l", Name: "list_flow_templates"}},
		{"instantiate", agentic.ToolCall{ID: "i", Name: "instantiate_flow_template",
			Arguments: map[string]any{"template_id": "t1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := e.Execute(ctx, tc.call)
			if result.Error == "" {
				t.Fatalf("expected Error on %s, got content=%q", tc.name, result.Content)
			}
		})
	}
}
