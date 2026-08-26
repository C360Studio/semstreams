package executors

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
)

// skipAllBut derives the SkipBuiltins list that leaves exactly one group
// registered, so the production wire (RegisterBuiltins) is driven while the
// dependency-hungry groups stay out of the way.
func skipAllBut(keep string) []string {
	var skip []string
	for _, key := range BuiltinGroupKeys {
		if key != keep {
			skip = append(skip, key)
		}
	}
	return skip
}

func compositionTestRegistry(t *testing.T) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	declare := func(ports component.PortConfig) component.PortDeclarer {
		return func(json.RawMessage, string) (component.PortConfig, error) { return ports, nil }
	}
	factory := func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
		return nil, errors.New("tools must not construct")
	}
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "typed-src", Type: "input", Protocol: "fake", Domain: "test", Version: "0.0.1", Factory: factory,
		Ports: declare(component.PortConfig{Outputs: []component.PortDefinition{{
			Name: "out", Required: true, Config: component.NATSPort{Subject: "typed.raw", Interface: &component.InterfaceContract{Type: "a.v1"}},
		}}}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "typed-sink", Type: "output", Protocol: "fake", Domain: "test", Version: "0.0.1", Factory: factory,
		Ports: declare(component.PortConfig{Inputs: []component.PortDefinition{{
			Name: "in", Required: true, Config: component.NATSPort{Subject: "typed.raw", Interface: &component.InterfaceContract{Type: "b.v1"}},
		}}}),
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func compositionToolRegistry(t *testing.T, compReg *component.Registry) *agentictools.ExecutorRegistry {
	t.Helper()
	tools := agentictools.NewExecutorRegistry()
	if err := RegisterBuiltins(context.Background(), tools, ToolDependencies{
		ComponentRegistry: compReg,
		Logger:            slog.Default(),
		SkipBuiltins:      skipAllBut("component_catalog"),
	}); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	return tools
}

func mismatchConfig() *config.Config {
	return &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "tools", ID: "test", Environment: "test"},
		Components: config.ComponentConfigs{
			"src":  {Name: "typed-src", Type: types.ComponentTypeInput, Enabled: true, Config: json.RawMessage(`{}`)},
			"sink": {Name: "typed-sink", Type: types.ComponentTypeOutput, Enabled: true, Config: json.RawMessage(`{}`)},
		},
	}
}

func configDocument(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestValidateCompositionToolReturnsFindings(t *testing.T) {
	compReg := compositionTestRegistry(t)
	tools := compositionToolRegistry(t, compReg)
	cfg := mismatchConfig()

	result, err := tools.Execute(context.Background(), agentic.ToolCall{
		ID: "v1", Name: "validate_composition", Arguments: map[string]any{"config": configDocument(t, cfg)},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("tool returned an error attachment: %s", result.Error)
	}
	var decoded composition.Result
	if err := json.Unmarshal([]byte(result.Content), &decoded); err != nil {
		t.Fatalf("content is not a composition.Result: %v\n%s", err, result.Content)
	}
	expected, err := composition.Validate(compReg, cfg)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, _ := json.Marshal(expected)
	var expectedDecoded composition.Result
	if err := json.Unmarshal(expectedJSON, &expectedDecoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, expectedDecoded) {
		t.Fatalf("tool result differs from composition.Validate:\ntool=%s\nlib=%s", result.Content, expectedJSON)
	}
	mismatches := 0
	for _, finding := range decoded.Errors {
		if finding.Type == composition.TypeInterfaceMismatch {
			mismatches++
		}
	}
	if mismatches != 1 {
		t.Fatalf("tool result carries %d interface_mismatch errors, want 1: %s", mismatches, result.Content)
	}
}

func TestCompositionGraphToolReturnsMermaid(t *testing.T) {
	compReg := compositionTestRegistry(t)
	tools := compositionToolRegistry(t, compReg)
	cfg := mismatchConfig()

	result, err := tools.Execute(context.Background(), agentic.ToolCall{
		ID: "g1", Name: "composition_graph", Arguments: map[string]any{"config": configDocument(t, cfg), "format": "mermaid"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("tool returned an error attachment: %s", result.Error)
	}
	expected, err := composition.Validate(compReg, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != composition.Mermaid(expected.Graph) {
		t.Fatalf("tool Mermaid differs from composition.Mermaid:\n%s\n---\n%s", result.Content, composition.Mermaid(expected.Graph))
	}
	if !strings.HasPrefix(result.Content, "flowchart LR") || strings.Count(result.Content, "-->") != 1 {
		t.Fatalf("unexpected Mermaid shape:\n%s", result.Content)
	}
}

func TestListComponentsCarriesPorts(t *testing.T) {
	compReg := compositionTestRegistry(t)
	tools := compositionToolRegistry(t, compReg)

	result, err := tools.Execute(context.Background(), agentic.ToolCall{
		ID: "l1", Name: agentictools.ComponentCatalogToolName, Arguments: map[string]any{"type_filter": "typed-src"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("tool returned an error attachment: %s", result.Error)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &entries); err != nil {
		t.Fatalf("content is not a catalog array: %v\n%s", err, result.Content)
	}
	if len(entries) != 1 {
		t.Fatalf("type_filter returned %d entries, want 1", len(entries))
	}
	ports, ok := entries[0]["default_ports"].(map[string]any)
	if !ok {
		t.Fatalf("entry lacks default_ports: %v", entries[0])
	}
	outputs, _ := ports["outputs"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("default_ports.outputs = %v, want one port", ports["outputs"])
	}
}
