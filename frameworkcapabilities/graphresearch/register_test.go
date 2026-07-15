package graphresearch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/types"
)

func TestValidateConfigAbsent(t *testing.T) {
	if err := ValidateConfig(&config.Config{}); err != nil {
		t.Fatalf("absent graph research should be valid: %v", err)
	}
}

func TestValidateConfigPartialFailsWithMissingDependency(t *testing.T) {
	cfg := &config.Config{Components: config.ComponentConfigs{
		"classify": enabledComponent("research-graph-classify", nil),
	}}
	err := ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "research-graph-route") {
		t.Fatalf("partial graph research error = %v, want missing route", err)
	}
}

func TestSelectedByResearchGraphToolAllowlist(t *testing.T) {
	for _, componentConfig := range []types.ComponentConfig{
		enabledComponent("agentic-tools", map[string]any{"allowed_tools": []string{"research_graph"}}),
		enabledComponent("agentic-dispatch", map[string]any{"default_tools": []string{"research_graph"}}),
	} {
		cfg := &config.Config{Components: config.ComponentConfigs{"selection": componentConfig}}
		if !Selected(cfg) {
			t.Fatalf("Selected returned false for %s research_graph allowlist", componentConfig.Name)
		}
	}
}

func TestSelectedByCanonicalRuleOutsideNamedDirectory(t *testing.T) {
	cfg := &config.Config{Components: config.ComponentConfigs{
		"rules": enabledComponent("rule-processor", map[string]any{
			"rules_files": []string{"/etc/semstreams/00-kickoff-classify.json"},
		}),
	}}
	if !Selected(cfg) {
		t.Fatal("Selected returned false for canonical graph-research rule basename")
	}
}

func TestValidateConfigRejectsMissingAgenticModel(t *testing.T) {
	cfg := completeConfig(t)
	delete(cfg.Components, "agentic-model")
	assertValidationErrorContains(t, cfg, "component agentic-model")
}

func TestValidateConfigRejectsMissingResearchModelCapability(t *testing.T) {
	for _, capability := range []string{"research_routing", "research_assessment", "research_synthesis"} {
		t.Run(capability, func(t *testing.T) {
			cfg := completeConfig(t)
			delete(cfg.ModelRegistry.Capabilities, capability)
			assertValidationErrorContains(t, cfg, "model capability "+capability)
		})
	}
}

func TestValidateConfigRejectsModelWithoutToolCalling(t *testing.T) {
	cfg := completeConfig(t)
	cfg.ModelRegistry.Endpoints["mock"].SupportsTools = false
	assertValidationErrorContains(t, cfg, "tool-capable default model endpoint")
}

func TestValidateConfigRejectsMismatchedLoopsBuckets(t *testing.T) {
	cfg := completeConfig(t)
	cfg.Components["research-graph-assess"] = enabledComponent("research-graph-assess", map[string]any{
		"loops_bucket": "OTHER_LOOPS",
	})
	assertValidationErrorContains(t, cfg, "common loops_bucket")
}

func TestLoopsBucketReturnsValidatedCompositionBucket(t *testing.T) {
	cfg := completeConfig(t)
	cfg.Components["agentic-tools"] = enabledComponent("agentic-tools", map[string]any{
		"allowed_tools": []string{"research_graph", "read_loop_result"},
		"loops_bucket":  "RESEARCH_LOOPS",
	})
	if got := LoopsBucket(cfg); got != "RESEARCH_LOOPS" {
		t.Fatalf("LoopsBucket = %q, want RESEARCH_LOOPS", got)
	}
}

func TestValidateConfigRejectsIncompleteToolAllowlist(t *testing.T) {
	for _, missing := range []string{"research_graph", "read_loop_result"} {
		t.Run(missing, func(t *testing.T) {
			cfg := completeConfig(t)
			allowed := []string{"research_graph", "read_loop_result", "decide"}
			allowed = slicesDelete(allowed, missing)
			cfg.Components["agentic-tools"] = enabledComponent("agentic-tools", map[string]any{
				"allowed_tools": allowed,
				"loops_bucket":  "AGENT_LOOPS",
			})
			assertValidationErrorContains(t, cfg, "agentic-tools allowed_tools entry "+missing)
		})
	}
}

func TestValidateConfigRejectsMissingOrCounterfeitRuleFile(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		cfg := completeConfig(t)
		setRuleFile(cfg, 0, filepath.Join(t.TempDir(), requiredRuleFiles[0]))
		assertValidationErrorContains(t, cfg, "read canonical rule")
	})

	t.Run("counterfeit content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, requiredRuleFiles[0])
		if err := os.WriteFile(path, []byte(`{"id":"not_the_canonical_rule","enabled":true,"on_enter":[{"type":"publish","subject":"component.nl_classify.x"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := completeConfig(t)
		setRuleFile(cfg, 0, path)
		assertValidationErrorContains(t, cfg, "canonical rule identity")
	})

	t.Run("non executable content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, requiredRuleFiles[0])
		if err := os.WriteFile(path, []byte(`{"id":"research_kickoff_classify","enabled":true,"conditions":[{"field":"agent.loop.role","operator":"eq","value":"research_pipeline"}],"on_enter":[{"type":"publish","subject":"component.nl_classify.x"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := completeConfig(t)
		setRuleFile(cfg, 0, path)
		assertValidationErrorContains(t, cfg, "missing canonical trigger condition")
	})

	t.Run("wrong role scope", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, requiredRuleFiles[0])
		data := `{"id":"research_kickoff_classify","enabled":true,"conditions":[{"field":"agent.loop.role","operator":"eq","value":"general"},{"field":"research.request.received","operator":"eq","value":"true"}],"on_enter":[{"type":"publish","subject":"component.nl_classify.x"}]}`
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := completeConfig(t)
		setRuleFile(cfg, 0, path)
		assertValidationErrorContains(t, cfg, "research_pipeline role condition")
	})
}

func TestCompleteCompositionRegistersFactoriesAndPayloads(t *testing.T) {
	cfg := completeConfig(t)
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}

	components := component.NewRegistry()
	if err := RegisterComponents(components); err != nil {
		t.Fatalf("RegisterComponents: %v", err)
	}
	for _, name := range stageFactories {
		if _, ok := components.GetFactory(name); !ok {
			t.Errorf("missing factory %q", name)
		}
	}

	payloads := payloadregistry.New()
	if err := RegisterPayloads(payloads); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}
	if _, ok := payloads.GetRegistration("research.intent.v1"); !ok {
		t.Fatal("research.intent.v1 was not registered")
	}
}

func completeConfig(t *testing.T) *config.Config {
	t.Helper()
	components := make(config.ComponentConfigs)
	for _, name := range append(append([]string{}, stageFactories...), requiredRuntimeComponents...) {
		components[name] = enabledComponent(name, map[string]any{"loops_bucket": "AGENT_LOOPS"})
	}
	components["agentic-tools"] = enabledComponent("agentic-tools", map[string]any{
		"allowed_tools": []string{"research_graph", "read_loop_result", "decide"},
		"loops_bucket":  "AGENT_LOOPS",
	})
	rules := make([]string, 0, len(requiredRuleFiles))
	for _, name := range requiredRuleFiles {
		rules = append(rules, filepath.Join("..", "..", "configs", "rules", "research-graph", name))
	}
	components["rule-processor"] = enabledComponent("rule-processor", map[string]any{"rules_files": rules})
	return &config.Config{
		Components: components,
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"mock": {Provider: "openai", URL: "http://mock.invalid/v1", Model: "mock", SupportsTools: true},
			},
			Defaults: model.DefaultsConfig{Model: "mock"},
			Capabilities: map[string]*model.CapabilityConfig{
				"research_routing":    {Preferred: []string{"mock"}},
				"research_assessment": {Preferred: []string{"mock"}},
				"research_synthesis":  {Preferred: []string{"mock"}},
			},
		},
	}
}

func enabledComponent(name string, raw map[string]any) types.ComponentConfig {
	data, _ := json.Marshal(raw)
	return types.ComponentConfig{Name: name, Type: types.ComponentTypeProcessor, Enabled: true, Config: data}
}

func assertValidationErrorContains(t *testing.T, cfg *config.Config, want string) {
	t.Helper()
	err := ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateConfig error = %v, want substring %q", err, want)
	}
}

func slicesDelete(values []string, remove string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			result = append(result, value)
		}
	}
	return result
}

func setRuleFile(cfg *config.Config, index int, path string) {
	componentConfig := cfg.Components["rule-processor"]
	var raw map[string]any
	_ = json.Unmarshal(componentConfig.Config, &raw)
	files := raw["rules_files"].([]any)
	files[index] = path
	componentConfig.Config, _ = json.Marshal(raw)
	cfg.Components["rule-processor"] = componentConfig
}
