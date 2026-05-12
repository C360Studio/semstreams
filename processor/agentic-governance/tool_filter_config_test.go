package agenticgovernance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolCallFilterConfig_JSONRoundTrip verifies the FilterConfig surface
// for the tool_call_governance filter survives a JSON encode/decode cycle
// with all fields preserved. Required per the operator-configurable
// surface discipline — any new operator-reachable field MUST have a
// round-trip test or it silently breaks on the next struct churn.
func TestToolCallFilterConfig_JSONRoundTrip(t *testing.T) {
	original := FilterConfig{
		Name:    "tool_call_governance",
		Enabled: true,
		ToolCallConfig: &ToolCallFilterConfig{
			BlockedCommandPatterns: []string{
				"cd /workspace ",
				"> /workspace/",
				"tee /workspace/",
			},
			BlockedURLPatterns: []string{
				"internal.example.invalid",
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded FilterConfig
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Enabled, decoded.Enabled)
	require.NotNil(t, decoded.ToolCallConfig, "ToolCallConfig must survive round trip")
	assert.Equal(t, original.ToolCallConfig.BlockedCommandPatterns, decoded.ToolCallConfig.BlockedCommandPatterns)
	assert.Equal(t, original.ToolCallConfig.BlockedURLPatterns, decoded.ToolCallConfig.BlockedURLPatterns)
}

// TestToolCallFilterConfig_OmittedFieldStaysNil verifies the operator
// can omit ToolCallConfig entirely (decoded value is nil), distinguishing
// "no operator extension configured" from "explicitly empty arrays".
func TestToolCallFilterConfig_OmittedFieldStaysNil(t *testing.T) {
	raw := `{"name":"tool_call_governance","enabled":true}`

	var decoded FilterConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))

	assert.Nil(t, decoded.ToolCallConfig, "omitted tool_call_config must decode to nil")
}

// TestToolCallFilterConfig_AcceptsValidate exercises FilterConfig.Validate
// for tool_call_governance. Bare config and pattern-bearing config both
// validate; only the missing-name case fails.
func TestToolCallFilterConfig_AcceptsValidate(t *testing.T) {
	bare := FilterConfig{Name: "tool_call_governance", Enabled: true}
	assert.NoError(t, bare.Validate())

	withPatterns := FilterConfig{
		Name:    "tool_call_governance",
		Enabled: true,
		ToolCallConfig: &ToolCallFilterConfig{
			BlockedCommandPatterns: []string{"foo"},
		},
	}
	assert.NoError(t, withPatterns.Validate())

	missingName := FilterConfig{Name: "", Enabled: true}
	assert.Error(t, missingName.Validate())
}

// TestNewToolCallFilterWithConfig_AppendsToDefaults verifies operator
// patterns extend the safety floor instead of replacing it. The
// semspec acceptance criterion calls this out explicitly: a custom
// pattern blocks AND a default pattern still blocks.
func TestNewToolCallFilterWithConfig_AppendsToDefaults(t *testing.T) {
	cfg := &ToolCallFilterConfig{
		BlockedCommandPatterns: []string{"cd /workspace "},
		BlockedURLPatterns:     []string{"internal.example.invalid"},
	}
	f := NewToolCallFilterWithConfig(nil, cfg)

	customBash := ToolCallToMessage(agentic.ToolCall{
		ID:        "custom-bash",
		Name:      "bash",
		Arguments: map[string]any{"command": "cd /workspace && ls"},
	}, "", "")
	customRes, err := f.Process(context.Background(), customBash)
	require.NoError(t, err)
	assert.False(t, customRes.Allowed, "custom bash pattern must still block")

	defaultBash := ToolCallToMessage(agentic.ToolCall{
		ID:        "default-bash",
		Name:      "bash",
		Arguments: map[string]any{"command": "curl 169.254.169.254"},
	}, "", "")
	defaultRes, err := f.Process(context.Background(), defaultBash)
	require.NoError(t, err)
	assert.False(t, defaultRes.Allowed, "default safety pattern must still block when config provided")

	customURL := ToolCallToMessage(agentic.ToolCall{
		ID:        "custom-url",
		Name:      "http_request",
		Arguments: map[string]any{"url": "https://internal.example.invalid/x"},
	}, "", "")
	customURLRes, err := f.Process(context.Background(), customURL)
	require.NoError(t, err)
	assert.False(t, customURLRes.Allowed, "custom URL pattern must still block")

	defaultURL := ToolCallToMessage(agentic.ToolCall{
		ID:        "default-url",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://localhost/admin"},
	}, "", "")
	defaultURLRes, err := f.Process(context.Background(), defaultURL)
	require.NoError(t, err)
	assert.False(t, defaultURLRes.Allowed, "default URL pattern must still block when config provided")
}

// TestNewToolCallFilterWithConfig_NilConfig is equivalent to NewToolCallFilter:
// safety defaults only, no operator extension.
func TestNewToolCallFilterWithConfig_NilConfig(t *testing.T) {
	f := NewToolCallFilterWithConfig(nil, nil)
	assert.Equal(t, defaultBlockedCommandPatterns(), f.BlockedCommandPatterns)
	assert.Equal(t, defaultBlockedURLPatterns(), f.BlockedURLPatterns)
}

// TestBuildFromConfig_ToolCallGovernance verifies the createFilter path
// produces a working ToolCallFilter when "tool_call_governance" appears
// in FilterChainConfig.Filters. End-to-end: operator config → live filter.
func TestBuildFromConfig_ToolCallGovernance(t *testing.T) {
	cfg := FilterChainConfig{
		Policy: PolicyFailFast,
		Filters: []FilterConfig{
			{
				Name:    "tool_call_governance",
				Enabled: true,
				ToolCallConfig: &ToolCallFilterConfig{
					BlockedCommandPatterns: []string{"cd /workspace "},
				},
			},
		},
	}
	require.NoError(t, cfg.Validate())

	chain, err := BuildFromConfig(cfg, nil)
	require.NoError(t, err)
	require.Len(t, chain.Filters, 1)

	tcf, ok := chain.Filters[0].(*ToolCallFilter)
	require.True(t, ok, "expected *ToolCallFilter, got %T", chain.Filters[0])

	// Custom pattern blocks.
	customMsg := ToolCallToMessage(agentic.ToolCall{
		ID:        "c1",
		Name:      "bash",
		Arguments: map[string]any{"command": "cd /workspace && echo nope"},
	}, "", "")
	res, err := tcf.Process(context.Background(), customMsg)
	require.NoError(t, err)
	assert.False(t, res.Allowed)

	// Default pattern still blocks.
	defaultMsg := ToolCallToMessage(agentic.ToolCall{
		ID:        "c2",
		Name:      "bash",
		Arguments: map[string]any{"command": "mkfs.ext4 /dev/sda"},
	}, "", "")
	res2, err := tcf.Process(context.Background(), defaultMsg)
	require.NoError(t, err)
	assert.False(t, res2.Allowed)
}

// TestBuildFromConfig_ToolCallGovernance_EmptyConfig confirms a
// tool_call_governance filter declared without ToolCallConfig still
// runs (safety defaults only).
func TestBuildFromConfig_ToolCallGovernance_EmptyConfig(t *testing.T) {
	cfg := FilterChainConfig{
		Filters: []FilterConfig{
			{Name: "tool_call_governance", Enabled: true},
		},
	}
	chain, err := BuildFromConfig(cfg, nil)
	require.NoError(t, err)
	require.Len(t, chain.Filters, 1)

	tcf, ok := chain.Filters[0].(*ToolCallFilter)
	require.True(t, ok)
	assert.Equal(t, defaultBlockedCommandPatterns(), tcf.BlockedCommandPatterns)
}
