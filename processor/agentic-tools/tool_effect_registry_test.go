package agentictools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// effectExecutor is a minimal executor whose advertised definitions are supplied
// per-test, including definitions the production executors would never emit
// (unrecognized spellings, mutation after registration).
type effectExecutor struct {
	defs []agentic.ToolDefinition
}

func (e *effectExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "ok"}, nil
}

func (e *effectExecutor) ListTools() []agentic.ToolDefinition { return e.defs }

func def(name string, effect agentic.ToolEffect) agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        name,
		Description: "test tool",
		Parameters:  map[string]any{"type": "object"},
		Effect:      effect,
	}
}

// TestRegisterExecutor_RejectsUnknownEffect is the "reject unknown enum values"
// acceptance test, bound to the seam that actually runs in production
// (ExecutorRegistry.RegisterExecutor), not to the caller-less
// ToolDefinition.Validate.
//
// The no-partial-commit assertion matters as much as the error: registration is
// two-pass precisely so a later invalid definition cannot leave earlier names
// committed, and a rejection that half-registered an executor would be worse
// than no rejection at all.
func TestRegisterExecutor_RejectsUnknownEffect(t *testing.T) {
	t.Parallel()

	registry := agentictools.NewExecutorRegistry()
	exec := &effectExecutor{defs: []agentic.ToolDefinition{
		def("valid_first_tool", agentic.ToolEffectReadOnly),
		def("invalid_second_tool", "destructive"),
	}}

	if err := registry.RegisterExecutor(exec); err == nil {
		t.Fatal("RegisterExecutor accepted a definition declaring an unrecognized effect")
	}

	// Neither name may be committed — not even the one that validated.
	for _, name := range []string{"valid_first_tool", "invalid_second_tool"} {
		if got := registry.GetTool(name); got != nil {
			t.Errorf("tool %q was committed despite a failed registration", name)
		}
	}
	if tools := registry.ListTools(); len(tools) != 0 {
		t.Errorf("registry advertises %d tools after a failed registration, want 0", len(tools))
	}
}

// TestRegisterExecutor_AcceptsUndeclaredEffect pins the other half of the
// asymmetry: declaring nothing is legal (a producer need not classify itself),
// while declaring something invalid is not.
func TestRegisterExecutor_AcceptsUndeclaredEffect(t *testing.T) {
	t.Parallel()

	registry := agentictools.NewExecutorRegistry()
	exec := &effectExecutor{defs: []agentic.ToolDefinition{def("undeclared_tool", "")}}

	if err := registry.RegisterExecutor(exec); err != nil {
		t.Fatalf("RegisterExecutor rejected an undeclared effect: %v", err)
	}
	if got := registry.GetTool("undeclared_tool"); got == nil {
		t.Error("undeclared_tool was not registered")
	}
}

// TestRegistryListTools_NormalizesEffect covers the two holes that
// registration-time rejection structurally cannot close, which is why
// normalization at aggregation is load-bearing rather than belt-and-suspenders:
//
//   - RegisterTool(name, executor) never inspects a ToolDefinition, so its
//     executors never pass the RegisterExecutor check at all.
//   - The registry stores EXECUTORS and re-invokes ListTools() live on every
//     aggregation, so a definition can differ at read time from the one
//     validated at registration.
func TestRegistryListTools_NormalizesEffect(t *testing.T) {
	t.Parallel()

	t.Run("RegisterTool path never saw a definition", func(t *testing.T) {
		t.Parallel()
		registry := agentictools.NewExecutorRegistry()
		exec := &effectExecutor{defs: []agentic.ToolDefinition{def("side_door_tool", "destructive")}}

		// RegisterTool takes a name and an executor; no definition is inspected,
		// so an invalid effect sails past the registration gate.
		if err := registry.RegisterTool("side_door_tool", exec); err != nil {
			t.Fatalf("RegisterTool: %v", err)
		}

		tools := registry.ListTools()
		if len(tools) != 1 {
			t.Fatalf("got %d tools, want 1", len(tools))
		}
		if tools[0].Effect != agentic.ToolEffectUnknown {
			t.Errorf("served Effect = %q, want %q — aggregation must normalize what registration never inspected",
				tools[0].Effect, agentic.ToolEffectUnknown)
		}
	})

	t.Run("definition drifts after registration", func(t *testing.T) {
		t.Parallel()
		registry := agentictools.NewExecutorRegistry()
		exec := &effectExecutor{defs: []agentic.ToolDefinition{def("drifting_tool", agentic.ToolEffectReadOnly)}}
		if err := registry.RegisterExecutor(exec); err != nil {
			t.Fatalf("RegisterExecutor: %v", err)
		}

		// The executor now returns something registration never validated. The
		// registry re-reads ListTools() live, so this reaches consumers.
		exec.defs = []agentic.ToolDefinition{def("drifting_tool", "destructive")}

		tools := registry.ListTools()
		if len(tools) != 1 {
			t.Fatalf("got %d tools, want 1", len(tools))
		}
		if tools[0].Effect != agentic.ToolEffectUnknown {
			t.Errorf("served Effect = %q, want %q — post-registration drift must still normalize",
				tools[0].Effect, agentic.ToolEffectUnknown)
		}
	})

	t.Run("undeclared becomes explicit unknown", func(t *testing.T) {
		t.Parallel()
		registry := agentictools.NewExecutorRegistry()
		if err := registry.RegisterExecutor(&effectExecutor{defs: []agentic.ToolDefinition{def("quiet_tool", "")}}); err != nil {
			t.Fatalf("RegisterExecutor: %v", err)
		}
		tools := registry.ListTools()
		if len(tools) != 1 || tools[0].Effect != agentic.ToolEffectUnknown {
			t.Errorf("served Effect = %q, want %q", tools[0].Effect, agentic.ToolEffectUnknown)
		}
	})

	t.Run("normalization does not mutate the producer's definition", func(t *testing.T) {
		t.Parallel()
		registry := agentictools.NewExecutorRegistry()
		exec := &effectExecutor{defs: []agentic.ToolDefinition{def("owned_tool", "")}}
		if err := registry.RegisterExecutor(exec); err != nil {
			t.Fatalf("RegisterExecutor: %v", err)
		}
		_ = registry.ListTools()
		if exec.defs[0].Effect != "" {
			t.Errorf("producer's own definition was mutated to %q; normalization applies to the served copy only", exec.defs[0].Effect)
		}
	})

	t.Run("declared values pass through unchanged", func(t *testing.T) {
		t.Parallel()
		for _, want := range []agentic.ToolEffect{
			agentic.ToolEffectReadOnly, agentic.ToolEffectMutating,
			agentic.ToolEffectExternal, agentic.ToolEffectUnknown,
		} {
			registry := agentictools.NewExecutorRegistry()
			if err := registry.RegisterExecutor(&effectExecutor{defs: []agentic.ToolDefinition{def("passthrough_tool", want)}}); err != nil {
				t.Fatalf("RegisterExecutor(%q): %v", want, err)
			}
			tools := registry.ListTools()
			if len(tools) != 1 || tools[0].Effect != want {
				t.Errorf("served Effect = %q, want %q", tools[0].Effect, want)
			}
		}
	})
}

// TestEffectSurvivesRegistrationThroughDiscovery is the end-to-end preservation
// acceptance test: executor → RegisterExecutor → registry.ListTools →
// Component.ListTools → the tool.list response body. Each hop is a place the
// value could be dropped, and the DTO hop already silently drops Strict and
// Paginated today.
func TestEffectSurvivesRegistrationThroughDiscovery(t *testing.T) {
	t.Parallel()

	rawConfig, err := json.Marshal(agentictools.Config{
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true}},
			Outputs: []component.PortDefinition{{Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}}, Required: true}},
		},
		Timeout: "60s",
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	created, err := agentictools.NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp, ok := created.(*agentictools.Component)
	if !ok {
		t.Fatalf("NewComponent returned %T, want *agentictools.Component", created)
	}

	err = comp.RegisterToolExecutor(&effectExecutor{defs: []agentic.ToolDefinition{
		def("catalog_read_tool", agentic.ToolEffectReadOnly),
		def("catalog_write_tool", agentic.ToolEffectMutating),
		def("catalog_external_tool", agentic.ToolEffectExternal),
		def("catalog_quiet_tool", ""),
	}})
	if err != nil {
		t.Fatalf("RegisterToolExecutor: %v", err)
	}

	want := map[string]string{
		"catalog_read_tool":     string(agentic.ToolEffectReadOnly),
		"catalog_write_tool":    string(agentic.ToolEffectMutating),
		"catalog_external_tool": string(agentic.ToolEffectExternal),
		"catalog_quiet_tool":    string(agentic.ToolEffectUnknown), // resolved, not blank
	}

	discovered := comp.ListTools()
	seen := map[string]string{}
	for _, tool := range discovered {
		seen[tool.Name] = tool.Effect
	}
	for name, wantEffect := range want {
		got, ok := seen[name]
		if !ok {
			t.Errorf("tool %q missing from discovery output", name)
			continue
		}
		if got != wantEffect {
			t.Errorf("discovery effect for %q = %q, want %q", name, got, wantEffect)
		}
	}

	// The discovery response is what sisters actually parse — assert on the
	// serialized body, so an omitempty regression on the DTO field is caught
	// here rather than downstream.
	body, err := json.Marshal(agentictools.ToolListResponse{Tools: discovered})
	if err != nil {
		t.Fatalf("marshal discovery response: %v", err)
	}
	var round struct {
		Tools []struct {
			Name   string `json:"name"`
			Effect string `json:"effect"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("unmarshal discovery response: %v", err)
	}
	for _, tool := range round.Tools {
		wantEffect, tracked := want[tool.Name]
		if !tracked {
			continue
		}
		if tool.Effect == "" {
			t.Errorf("tool %q served an empty effect on the wire — the fail-safe value must be explicit", tool.Name)
		}
		if tool.Effect != wantEffect {
			t.Errorf("wire effect for %q = %q, want %q", tool.Name, tool.Effect, wantEffect)
		}
	}
}
