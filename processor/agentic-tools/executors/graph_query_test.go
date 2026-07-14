package executors

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
)

// mockKVGetter implements KVGetter for testing
type mockKVGetter struct {
	data map[string][]byte
}

func newMockKVGetter() *mockKVGetter {
	return &mockKVGetter{
		data: make(map[string][]byte),
	}
}

func (m *mockKVGetter) Put(key string, value []byte) {
	m.data[key] = value
}

func (m *mockKVGetter) Get(_ context.Context, key string) (KVEntry, error) {
	value, ok := m.data[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return &mockEntry{key: key, value: value, revision: 1}, nil
}

// mockEntry implements KVEntry for testing
type mockEntry struct {
	key      string
	value    []byte
	revision uint64
}

func (e *mockEntry) Value() []byte    { return e.value }
func (e *mockEntry) Revision() uint64 { return e.revision }

func TestGraphQueryExecutor_ListTools(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	tools := executor.ListTools()

	// Should have 5 tools: query_entity, query_entities, query_relationships, query_neighbors, query_by_type
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}

	// Verify tool names
	expectedNames := map[string]bool{
		"query_entity":        true,
		"query_entities":      true,
		"query_relationships": true,
		"query_neighbors":     true,
		"query_by_type":       true,
	}

	for _, tool := range tools {
		if !expectedNames[tool.Name] {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		delete(expectedNames, tool.Name)

		if tool.Description == "" {
			t.Errorf("expected non-empty description for tool %s", tool.Name)
		}

		if tool.Parameters == nil {
			t.Errorf("expected non-nil parameters for tool %s", tool.Name)
		}
	}

	if len(expectedNames) > 0 {
		for name := range expectedNames {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestGraphQueryExecutor_QueryEntity_Success(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	// Store test entity
	entityData := map[string]any{
		"id":   "c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		"type": "temperature",
		"properties": map[string]any{
			"reading":  48.2,
			"location": "cold-storage-1",
		},
	}
	entityJSON, _ := json.Marshal(entityData)
	kv.Put("c360.logistics.environmental.sensor.temperature.temp-sensor-001", entityJSON)

	// Execute query
	call := agentic.ToolCall{
		ID:   "call_123",
		Name: "query_entity",
		Arguments: map[string]any{
			"entity_id": "c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CallID != "call_123" {
		t.Errorf("expected call_id call_123, got %s", result.CallID)
	}

	if result.Error != "" {
		t.Errorf("unexpected error in result: %s", result.Error)
	}

	if result.Content == "" {
		t.Error("expected non-empty content")
	}

	// Verify content is valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Errorf("content is not valid JSON: %v", err)
	}

	if parsed["type"] != "temperature" {
		t.Errorf("expected type temperature, got %v", parsed["type"])
	}

	// Check metadata
	if result.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}

	if result.Metadata["entity_id"] != "c360.logistics.environmental.sensor.temperature.temp-sensor-001" {
		t.Errorf("unexpected entity_id in metadata: %v", result.Metadata["entity_id"])
	}
}

func TestGraphQueryExecutor_QueryEntity_NotFound(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	call := agentic.ToolCall{
		ID:   "call_456",
		Name: "query_entity",
		Arguments: map[string]any{
			"entity_id": "nonexistent-entity",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CallID != "call_456" {
		t.Errorf("expected call_id call_456, got %s", result.CallID)
	}

	if result.Error == "" {
		t.Error("expected error for not found entity")
	}

	if result.Error != "entity not found: nonexistent-entity" {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

func TestGraphQueryExecutor_QueryEntity_MissingEntityID(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	call := agentic.ToolCall{
		ID:        "call_789",
		Name:      "query_entity",
		Arguments: map[string]any{},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected error for missing entity_id")
	}
}

func TestGraphQueryExecutor_QueryEntity_EmptyEntityID(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	call := agentic.ToolCall{
		ID:   "call_abc",
		Name: "query_entity",
		Arguments: map[string]any{
			"entity_id": "",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected error for empty entity_id")
	}
}

func TestGraphQueryExecutor_UnknownTool(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	call := agentic.ToolCall{
		ID:   "call_xyz",
		Name: "unknown_tool",
		Arguments: map[string]any{
			"foo": "bar",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err == nil {
		t.Error("expected error for unknown tool")
	}

	if result.Error == "" {
		t.Error("expected error in result for unknown tool")
	}
}

func TestGraphQueryExecutor_NonJSONAuthoritativeStateRequiresReset(t *testing.T) {
	kv := newMockKVGetter()
	executor := NewGraphQueryExecutor(kv)

	// Store non-JSON content
	kv.Put("plain-text-entity", []byte("This is plain text, not JSON"))

	call := agentic.ToolCall{
		ID:   "call_plain",
		Name: "query_entity",
		Arguments: map[string]any{
			"entity_id": "plain-text-entity",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err == nil {
		t.Fatal("expected unreadable authoritative state to fail closed")
	}
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("error = %T %v, want graph state contract error", err, err)
	}
	if result.Error == "" {
		t.Error("tool result must explain the reset-required failure")
	}
}
