package executors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/persona"
)

// mockPersonaManager is an in-memory PersonaManager for tests.
type mockPersonaManager struct {
	mu   sync.Mutex
	data map[string]*persona.Persona
}

func newMockPersonaManager() *mockPersonaManager {
	return &mockPersonaManager{data: map[string]*persona.Persona{}}
}

func (m *mockPersonaManager) Create(_ context.Context, p *persona.Persona) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[p.ID]; exists {
		return errors.New("persona already exists")
	}
	m.data[p.ID] = p
	return nil
}

func (m *mockPersonaManager) Update(_ context.Context, p *persona.Persona) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[p.ID]; !exists {
		return errors.New("persona not found")
	}
	m.data[p.ID] = p
	return nil
}

func (m *mockPersonaManager) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func (m *mockPersonaManager) Get(_ context.Context, id string) (*persona.Persona, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.data[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

func (m *mockPersonaManager) List(_ context.Context) (map[string]*persona.Persona, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*persona.Persona, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func TestPersonaExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()
	e := NewPersonaExecutor(newMockPersonaManager())
	tools := e.ListTools()
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}
	expected := map[string]bool{
		"create_persona": true, "update_persona": true, "delete_persona": true,
		"list_personas": true, "get_persona": true,
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

func TestPersonaExecutor_CreateAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockPersonaManager()
	e := NewPersonaExecutor(mgr)

	p := map[string]any{
		"id":       "role-researcher",
		"content":  "You are a research agent.",
		"category": 100,
		"roles":    []string{"researcher"},
	}

	result, err := e.Execute(ctx, agentic.ToolCall{
		ID: "c1", Name: "create_persona",
		Arguments: map[string]any{"persona": p},
	})
	if err != nil {
		t.Fatalf("Execute create: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("create error: %s", result.Error)
	}

	result, err = e.Execute(ctx, agentic.ToolCall{
		ID: "g1", Name: "get_persona",
		Arguments: map[string]any{"persona_id": "role-researcher"},
	})
	if err != nil || result.Error != "" {
		t.Fatalf("get failed: err=%v error=%s", err, result.Error)
	}
	var got persona.Persona
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "role-researcher" || got.Content != "You are a research agent." || got.Category != 100 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestPersonaExecutor_CreateRejectsInvalid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewPersonaExecutor(newMockPersonaManager())

	// Missing `persona` argument entirely.
	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID: "c1", Name: "create_persona",
		Arguments: map[string]any{},
	})
	if result.Error == "" {
		t.Fatalf("expected error for missing persona argument")
	}
	if !strings.Contains(result.Error, "required") {
		t.Errorf("expected 'required', got: %s", result.Error)
	}
}

func TestPersonaExecutor_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockPersonaManager()
	mgr.data["p1"] = &persona.Persona{ID: "p1", Content: "c"}
	e := NewPersonaExecutor(mgr)

	result, _ := e.Execute(ctx, agentic.ToolCall{
		ID: "d1", Name: "delete_persona",
		Arguments: map[string]any{"persona_id": "p1"},
	})
	if result.Error != "" {
		t.Fatalf("delete failed: %s", result.Error)
	}
	if _, exists := mgr.data["p1"]; exists {
		t.Errorf("p1 should be deleted from mock store")
	}

	// Missing id.
	result, _ = e.Execute(ctx, agentic.ToolCall{
		ID: "d2", Name: "delete_persona",
		Arguments: map[string]any{},
	})
	if result.Error == "" {
		t.Fatalf("expected error for missing persona_id")
	}
}

func TestPersonaExecutor_ListEmptyAndPopulated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newMockPersonaManager()
	e := NewPersonaExecutor(mgr)

	result, _ := e.Execute(ctx, agentic.ToolCall{ID: "l1", Name: "list_personas"})
	if !strings.Contains(result.Content, "No personas") {
		t.Errorf("empty list should report 'No personas', got: %s", result.Content)
	}

	mgr.data["a"] = &persona.Persona{ID: "a", Content: "x", Category: 100, Roles: []string{"researcher"}}
	mgr.data["b"] = &persona.Persona{ID: "b", Content: "y", Category: 200}
	result, _ = e.Execute(ctx, agentic.ToolCall{ID: "l2", Name: "list_personas"})
	if !strings.Contains(result.Content, "Personas (2)") {
		t.Errorf("expected 'Personas (2)', got: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"id": "a"`) ||
		!strings.Contains(result.Content, `"id": "b"`) {
		t.Errorf("expected both persona ids, got: %s", result.Content)
	}
}

func TestPersonaExecutor_UnknownToolFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewPersonaExecutor(newMockPersonaManager())
	result, err := e.Execute(ctx, agentic.ToolCall{ID: "x", Name: "nope"})
	if err == nil || !strings.Contains(result.Error, "unknown tool") {
		t.Errorf("expected unknown-tool error, got err=%v error=%s", err, result.Error)
	}
}

// errorPersonaManager returns the supplied error from every method. Used
// to exercise transport-error paths on the executor without KV.
type errorPersonaManager struct {
	err error
}

func (m *errorPersonaManager) Create(_ context.Context, _ *persona.Persona) error { return m.err }
func (m *errorPersonaManager) Update(_ context.Context, _ *persona.Persona) error { return m.err }
func (m *errorPersonaManager) Delete(_ context.Context, _ string) error           { return m.err }
func (m *errorPersonaManager) Get(_ context.Context, _ string) (*persona.Persona, error) {
	return nil, m.err
}
func (m *errorPersonaManager) List(_ context.Context) (map[string]*persona.Persona, error) {
	return nil, m.err
}

// TestPersonaExecutor_TransportErrorsSurfaceAsToolErrors — Manager errors
// land in ToolResult.Error rather than panicking or returning a Go error
// from Execute. LLMs see a clean message and can retry.
func TestPersonaExecutor_TransportErrorsSurfaceAsToolErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("simulated persona transport failure")
	e := NewPersonaExecutor(&errorPersonaManager{err: boom})

	cases := []struct {
		name string
		call agentic.ToolCall
	}{
		{"create", agentic.ToolCall{ID: "c", Name: "create_persona",
			Arguments: map[string]any{"persona": map[string]any{"id": "x", "content": "y"}}}},
		{"update", agentic.ToolCall{ID: "u", Name: "update_persona",
			Arguments: map[string]any{"persona": map[string]any{"id": "x", "content": "y"}}}},
		{"delete", agentic.ToolCall{ID: "d", Name: "delete_persona",
			Arguments: map[string]any{"persona_id": "x"}}},
		{"get", agentic.ToolCall{ID: "g", Name: "get_persona",
			Arguments: map[string]any{"persona_id": "x"}}},
		{"list", agentic.ToolCall{ID: "l", Name: "list_personas"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := e.Execute(ctx, tc.call)
			if result.Error == "" {
				t.Fatalf("expected Error on %s path, got content=%q", tc.name, result.Content)
			}
			if !strings.Contains(result.Error, "simulated persona transport failure") &&
				!strings.Contains(result.Error, "failed") {
				t.Errorf("expected simulated error to surface on %s, got: %s", tc.name, result.Error)
			}
		})
	}
}
