package executors

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRuleManager implements RuleManager for testing.
type mockRuleManager struct {
	rules map[string]rule.Definition
}

func newMockRuleManager() *mockRuleManager {
	return &mockRuleManager{rules: make(map[string]rule.Definition)}
}

func (m *mockRuleManager) SaveRule(_ context.Context, ruleID string, def rule.Definition) error {
	m.rules[ruleID] = def
	return nil
}

func (m *mockRuleManager) DeleteRule(_ context.Context, ruleID string) error {
	delete(m.rules, ruleID)
	return nil
}

func (m *mockRuleManager) GetRule(_ context.Context, ruleID string) (*rule.Definition, error) {
	def, ok := m.rules[ruleID]
	if !ok {
		return nil, assert.AnError
	}
	return &def, nil
}

func (m *mockRuleManager) ListRules(_ context.Context) (map[string]rule.Definition, error) {
	return m.rules, nil
}

func TestRuleExecutor_ListTools(t *testing.T) {
	e := NewRuleExecutor(newMockRuleManager())
	tools := e.ListTools()
	assert.Len(t, tools, 5)

	// Verify expected tool names are present
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	assert.True(t, names["create_rule"], "should have create_rule")
	assert.True(t, names["update_rule"], "should have update_rule")
	assert.True(t, names["delete_rule"], "should have delete_rule")
	assert.True(t, names["list_rules"], "should have list_rules")
	assert.True(t, names["get_rule"], "should have get_rule")
	// NOTE: approval policy is config-driven (Config.ApprovalRequired),
	// not a property of the tool definition. See approval_filter_test.go.
}

func TestRuleExecutor_CreateRule(t *testing.T) {
	mgr := newMockRuleManager()
	e := NewRuleExecutor(mgr)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-1",
		Name: "create_rule",
		Arguments: map[string]any{
			"rule_id": "test-rule",
			"rule": map[string]any{
				"type":    "expression",
				"name":    "Test Rule",
				"enabled": true,
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "created successfully")
	assert.Empty(t, result.Error)

	// Verify rule was saved
	assert.Contains(t, mgr.rules, "test-rule")
	assert.Equal(t, "Test Rule", mgr.rules["test-rule"].Name)
}

func TestRuleExecutor_DeleteRule(t *testing.T) {
	mgr := newMockRuleManager()
	mgr.rules["existing"] = rule.Definition{ID: "existing", Name: "Existing"}
	e := NewRuleExecutor(mgr)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2",
		Name:      "delete_rule",
		Arguments: map[string]any{"rule_id": "existing"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "deleted successfully")
	assert.NotContains(t, mgr.rules, "existing")
}

func TestRuleExecutor_ListRules(t *testing.T) {
	mgr := newMockRuleManager()
	mgr.rules["rule-a"] = rule.Definition{ID: "rule-a", Name: "Rule A"}
	mgr.rules["rule-b"] = rule.Definition{ID: "rule-b", Name: "Rule B"}
	e := NewRuleExecutor(mgr)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-3",
		Name:      "list_rules",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Active rules (2)")
	assert.Contains(t, result.Content, "Rule A")
	assert.Contains(t, result.Content, "Rule B")
}

func TestRuleExecutor_GetRule(t *testing.T) {
	mgr := newMockRuleManager()
	mgr.rules["my-rule"] = rule.Definition{ID: "my-rule", Name: "My Rule", Type: "expression"}
	e := NewRuleExecutor(mgr)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-4",
		Name:      "get_rule",
		Arguments: map[string]any{"rule_id": "my-rule"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "My Rule")
	assert.Contains(t, result.Content, "expression")
}

func TestRuleExecutor_MissingRuleID(t *testing.T) {
	e := NewRuleExecutor(newMockRuleManager())

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-5",
		Name:      "create_rule",
		Arguments: map[string]any{"rule": map[string]any{}},
	})
	require.NoError(t, err)
	assert.Equal(t, "rule_id is required", result.Error)
}

func TestRuleExecutor_UnknownTool(t *testing.T) {
	e := NewRuleExecutor(newMockRuleManager())

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-6",
		Name: "unknown_rule_tool",
	})
	assert.Error(t, err)
}
