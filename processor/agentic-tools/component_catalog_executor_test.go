package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
)

// stubComponentFactory is a no-op factory so Registry.RegisterFactory accepts
// the registration (it validates Factory is non-nil).
func stubComponentFactory(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
	return nil, errors.New("stub: not meant to be called")
}

// setupCatalogTestRegistry registers three fake factories for executor tests.
func setupCatalogTestRegistry(t *testing.T) *component.Registry {
	t.Helper()
	reg := component.NewRegistry()

	type entry struct {
		name, compType, protocol, domain, desc, version string
	}
	for _, e := range []entry{
		{"alpha-input", "input", "udp", "network", "Alpha UDP input", "1.0.0"},
		{"beta-processor", "processor", "internal", "semantic", "Beta semantic processor", "2.0.0"},
		{"gamma-output", "output", "file", "storage", "Gamma file output", "3.0.0"},
	} {
		if err := reg.RegisterFactory(e.name, &component.Registration{
			Name:        e.name,
			Type:        e.compType,
			Protocol:    e.protocol,
			Domain:      e.domain,
			Description: e.desc,
			Version:     e.version,
			Factory:     stubComponentFactory,
			Ports:       func(json.RawMessage, string) (component.PortConfig, error) { return component.PortConfig{}, nil },
		}); err != nil {
			t.Fatalf("register %s: %v", e.name, err)
		}
	}

	return reg
}

func TestComponentCatalogExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()

	reg := setupCatalogTestRegistry(t)
	e := NewComponentCatalogExecutor(reg, slog.Default())

	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != ComponentCatalogToolName {
		t.Errorf("expected tool name %q, got %q", ComponentCatalogToolName, tools[0].Name)
	}
	// Must advertise type_filter in parameters.
	params, _ := tools[0].Parameters["properties"].(map[string]any)
	if _, ok := params["type_filter"]; !ok {
		t.Error("tool parameters should include 'type_filter'")
	}
}

func TestComponentCatalogExecutor_NoFilter_ReturnsAll(t *testing.T) {
	t.Parallel()

	reg := setupCatalogTestRegistry(t)
	e := NewComponentCatalogExecutor(reg, slog.Default())

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ComponentCatalogToolName,
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute returned tool error: %s", result.Error)
	}

	var catalog []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &catalog); err != nil {
		t.Fatalf("content is not valid JSON array: %v", err)
	}
	if len(catalog) != 3 {
		t.Fatalf("expected 3 entries (all registered), got %d", len(catalog))
	}

	// Every entry must carry the required keys.
	requiredKeys := []string{"id", "name", "type", "protocol", "domain", "description", "version", "category", "schema"}
	for _, entry := range catalog {
		for _, k := range requiredKeys {
			if _, ok := entry[k]; !ok {
				t.Errorf("entry missing key %q: %v", k, entry)
			}
		}
	}
}

func TestComponentCatalogExecutor_TypeFilter_NarrowsResult(t *testing.T) {
	t.Parallel()

	reg := setupCatalogTestRegistry(t)
	e := NewComponentCatalogExecutor(reg, slog.Default())

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c2",
		Name:      ComponentCatalogToolName,
		Arguments: map[string]any{"type_filter": "beta-processor"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute tool error: %s", result.Error)
	}

	var catalog []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &catalog); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if len(catalog) != 1 {
		t.Fatalf("type_filter should return 1 entry, got %d", len(catalog))
	}
	if id, _ := catalog[0]["id"].(string); id != "beta-processor" {
		t.Errorf("expected id='beta-processor', got %q", id)
	}
}

func TestComponentCatalogExecutor_TypeFilter_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()

	reg := setupCatalogTestRegistry(t)
	e := NewComponentCatalogExecutor(reg, slog.Default())

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c3",
		Name:      ComponentCatalogToolName,
		Arguments: map[string]any{"type_filter": "does-not-exist"},
	})
	if err != nil {
		// Execute should not propagate errors for not-found — it's a ToolResult error.
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected ToolResult.Error for missing type_filter ID")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("error should mention 'not found', got: %s", result.Error)
	}
}

func TestComponentCatalogExecutor_UnknownToolName_ReturnsError(t *testing.T) {
	t.Parallel()

	reg := setupCatalogTestRegistry(t)
	e := NewComponentCatalogExecutor(reg, slog.Default())

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c4",
		Name: "not_a_real_tool",
	})
	if err == nil {
		t.Fatal("expected error for unknown tool name")
	}
	if !strings.Contains(result.Error, "unknown tool") {
		t.Errorf("expected 'unknown tool', got: %s", result.Error)
	}
}
