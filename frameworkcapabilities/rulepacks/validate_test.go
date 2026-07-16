package rulepacks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/config"
	ruleprocessor "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
)

func TestValidateConfigRulePackIdentity(t *testing.T) {
	t.Parallel()
	t.Run("missing config", func(t *testing.T) {
		cfg := composition(ruleComponent(true, nil))
		assertConfigError(t, cfg, "explicit pack_id is required")
	})
	t.Run("empty pack", func(t *testing.T) {
		cfg := composition(ruleComponent(true, json.RawMessage(`{"enable_graph_integration":false}`)))
		assertConfigError(t, cfg, "pack_id is required")
	})
	t.Run("invalid pack", func(t *testing.T) {
		cfg := composition(ruleComponent(true, json.RawMessage(`{"pack_id":"bad:pack"}`)))
		assertConfigError(t, cfg, "invalid pack_id")
	})
	t.Run("dotted pack is not one KV token", func(t *testing.T) {
		cfg := composition(ruleComponent(true, json.RawMessage(`{"pack_id":"bad.pack"}`)))
		assertConfigError(t, cfg, "one literal KV token")
	})
	t.Run("duplicate enabled pack", func(t *testing.T) {
		cfg := &config.Config{Components: config.ComponentConfigs{
			"rules-a": ruleComponent(true, validRuleConfig(t, "shared-pack")),
			"rules-b": ruleComponent(true, validRuleConfig(t, "shared-pack")),
		}}
		assertConfigError(t, cfg, `duplicate enabled rule pack_id "shared-pack"`)
	})
	t.Run("disabled duplicate ignored", func(t *testing.T) {
		cfg := &config.Config{Components: config.ComponentConfigs{
			"rules-a": ruleComponent(true, validRuleConfig(t, "shared-pack")),
			"rules-b": ruleComponent(false, validRuleConfig(t, "shared-pack")),
		}}
		if err := ValidateConfig(cfg); err != nil {
			t.Fatalf("ValidateConfig: %v", err)
		}
	})
	t.Run("disabled pack is still required", func(t *testing.T) {
		cfg := composition(ruleComponent(false, json.RawMessage(`{"enable_graph_integration":false}`)))
		assertConfigError(t, cfg, "pack_id is required")
	})
	t.Run("distinct packs", func(t *testing.T) {
		cfg := &config.Config{Components: config.ComponentConfigs{
			"rules-a": ruleComponent(true, validRuleConfig(t, "pack-a")),
			"rules-b": ruleComponent(true, validRuleConfig(t, "pack-b")),
		}}
		if err := ValidateConfig(cfg); err != nil {
			t.Fatalf("ValidateConfig: %v", err)
		}
	})
}

func TestValidateRuntimeUpdate(t *testing.T) {
	t.Parallel()

	enabled := ruleComponent(true, validRuleConfig(t, "stable-pack"))
	disabled := ruleComponent(false, validRuleConfig(t, "stable-pack"))

	t.Run("unchanged enabled component", func(t *testing.T) {
		if err := ValidateRuntimeUpdate("rules", &enabled, enabled); err != nil {
			t.Fatalf("ValidateRuntimeUpdate: %v", err)
		}
	})
	t.Run("new enabled component requires process restart", func(t *testing.T) {
		assertRuntimeUpdateError(t, nil, enabled, "cannot be enabled through component hot reload")
	})
	t.Run("re-enabled component requires process restart", func(t *testing.T) {
		assertRuntimeUpdateError(t, &disabled, enabled, "cannot be enabled through component hot reload")
	})
	t.Run("pack identity is static", func(t *testing.T) {
		changed := ruleComponent(true, validRuleConfig(t, "changed-pack"))
		assertRuntimeUpdateError(t, &enabled, changed, "pack_id is static")
	})
	t.Run("component structural config is static", func(t *testing.T) {
		var cfg ruleprocessor.Config
		if err := json.Unmarshal(enabled.Config, &cfg); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		cfg.EnableGraphIntegration = !cfg.EnableGraphIntegration
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		changed := ruleComponent(true, raw)
		assertRuntimeUpdateError(t, &enabled, changed, "component config is static")
	})
	t.Run("disable preserves static identity", func(t *testing.T) {
		if err := ValidateRuntimeUpdate("rules", &enabled, disabled); err != nil {
			t.Fatalf("ValidateRuntimeUpdate: %v", err)
		}
	})
	t.Run("disabled component still requires valid identity", func(t *testing.T) {
		invalid := ruleComponent(false, json.RawMessage(`{}`))
		assertRuntimeUpdateError(t, &enabled, invalid, "pack_id is required")
	})
	t.Run("non-rule component is outside contract", func(t *testing.T) {
		other := types.ComponentConfig{Name: "other", Enabled: true}
		if err := ValidateRuntimeUpdate("other", nil, other); err != nil {
			t.Fatalf("ValidateRuntimeUpdate: %v", err)
		}
	})
}

func TestValidateConfigAllowsReplicaIdentityAcrossSeparateCompositions(t *testing.T) {
	t.Parallel()
	for index := 0; index < 2; index++ {
		if err := ValidateConfig(composition(ruleComponent(true, validRuleConfig(t, "replica-pack")))); err != nil {
			t.Fatalf("composition %d: %v", index, err)
		}
	}
}

func composition(component types.ComponentConfig) *config.Config {
	return &config.Config{Components: config.ComponentConfigs{"rules": component}}
}

func ruleComponent(enabled bool, raw json.RawMessage) types.ComponentConfig {
	return types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: enabled, Config: raw,
	}
}

func validRuleConfig(t testing.TB, packID string) json.RawMessage {
	t.Helper()
	ruleConfig, err := ruleprocessor.NewConfig(packID)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	raw, err := json.Marshal(ruleConfig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}

func assertConfigError(t testing.TB, cfg *config.Config, want string) {
	t.Helper()
	err := ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateConfig error = %v, want substring %q", err, want)
	}
}

func assertRuntimeUpdateError(
	t testing.TB,
	previous *types.ComponentConfig,
	proposed types.ComponentConfig,
	want string,
) {
	t.Helper()
	err := ValidateRuntimeUpdate("rules", previous, proposed)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRuntimeUpdate error = %v, want substring %q", err, want)
	}
}
