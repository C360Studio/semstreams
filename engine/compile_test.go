package flowengine

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

func compileTestEngine(t *testing.T) *Engine {
	t.Helper()
	registry := component.NewRegistry()
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "compile-test", Type: "processor", Protocol: "nats", Domain: "test",
		Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return &validationTestComponent{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return NewEngine(registry, &natsclient.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func TestCompileValidatesBeforeProducingEnabledCandidates(t *testing.T) {
	engine := compileTestEngine(t)
	flow := &flowstore.Flow{
		ID: "flow", Name: "Flow",
		Nodes: []flowstore.FlowNode{{
			ID: "node", Name: "instance-b", Component: "compile-test",
			Type: types.ComponentTypeProcessor, Config: map[string]any{"value": "two"},
		}},
	}
	configs, result, err := engine.Compile(flow)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status == "errors" {
		t.Fatalf("unexpected validation result: %#v", result)
	}
	got := configs["instance-b"]
	if !got.Enabled || got.Name != "compile-test" || got.Type != types.ComponentTypeProcessor {
		t.Fatalf("unexpected compiled config: %#v", got)
	}
}

func TestCompileRejectsDuplicateInstanceNames(t *testing.T) {
	engine := compileTestEngine(t)
	flow := &flowstore.Flow{ID: "flow", Name: "Flow", Nodes: []flowstore.FlowNode{
		{ID: "one", Name: "duplicate", Component: "compile-test", Type: types.ComponentTypeProcessor},
		{ID: "two", Name: "duplicate", Component: "compile-test", Type: types.ComponentTypeProcessor},
	}}
	configs, _, err := engine.Compile(flow)
	if err == nil || configs != nil {
		t.Fatalf("duplicate compile = %#v, %v; want error", configs, err)
	}
}
