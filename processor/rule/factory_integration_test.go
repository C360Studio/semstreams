package rule_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/processor/rule"
	"github.com/c360/semstreams/processor/rule/expression"
)

// TestIntegration_FactoryRegistry tests multiple rule factories registered simultaneously
func TestIntegration_FactoryRegistry(t *testing.T) {
	// Get registered rule types
	ruleTypes := rule.GetRegisteredRuleTypes()
	require.Greater(t, len(ruleTypes), 0, "Should have at least one registered rule type")

	// Verify test_rule factory is registered
	testFactory, exists := rule.GetRuleFactory("test_rule")
	require.True(t, exists, "test_rule factory should be registered")
	require.NotNil(t, testFactory)

	// Verify factory type
	assert.Equal(t, "test_rule", testFactory.Type())

	// Verify factory schema
	schema := testFactory.Schema()
	assert.Equal(t, "test_rule", schema.Type)
	assert.Contains(t, schema.Required, "id")
	assert.Contains(t, schema.Required, "name")
}

// TestIntegration_FactoryValidation tests factory validation logic
func TestIntegration_FactoryValidation(t *testing.T) {
	testFactory, exists := rule.GetRuleFactory("test_rule")
	require.True(t, exists)
	require.NotNil(t, testFactory)

	tests := []struct {
		name      string
		def       rule.RuleDefinition
		shouldErr bool
	}{
		{
			name: "valid_rule_definition",
			def: rule.RuleDefinition{
				ID:   "test-001",
				Type: "test_rule",
				Name: "Valid Test Rule",
				Conditions: []expression.ConditionExpression{
					{
						Field:    "value",
						Operator: "gt",
						Value:    50.0,
						Required: true,
					},
				},
				Logic:   "and",
				Enabled: true,
			},
			shouldErr: false,
		},
		{
			name: "missing_id",
			def: rule.RuleDefinition{
				Type: "test_rule",
				Name: "No ID Rule",
			},
			shouldErr: false, // test_rule factory accepts any definition
		},
		{
			name:      "empty_definition",
			def:       rule.RuleDefinition{},
			shouldErr: false, // test_rule factory is permissive for testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testFactory.Validate(tt.def)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIntegration_FactoryCreate tests factory rule creation
func TestIntegration_FactoryCreate(t *testing.T) {
	testFactory, exists := rule.GetRuleFactory("test_rule")
	require.True(t, exists)
	require.NotNil(t, testFactory)

	// Create test rule definition
	ruleDef := rule.RuleDefinition{
		ID:   "factory-test-001",
		Type: "test_rule",
		Name: "Factory Test Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "battery.level",
				Operator: "lte",
				Value:    20.0,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
	}

	// Create rule via factory
	deps := rule.RuleDependencies{} // Empty dependencies for test
	ruleInstance, err := testFactory.Create("factory-test-001", ruleDef, deps)
	require.NoError(t, err)
	require.NotNil(t, ruleInstance)

	// Verify rule implements Rule interface
	assert.Equal(t, "Factory Test Rule", ruleInstance.Name())
	assert.NotNil(t, ruleInstance.Subscribe())
}

// TestIntegration_FactorySchema tests factory schema and examples
func TestIntegration_FactorySchema(t *testing.T) {
	testFactory, exists := rule.GetRuleFactory("test_rule")
	require.True(t, exists)
	require.NotNil(t, testFactory)

	// Get factory schema
	schema := testFactory.Schema()
	assert.Equal(t, "test_rule", schema.Type)
	assert.NotEmpty(t, schema.Required, "Schema should specify required fields")

	// Verify schema includes examples
	// Note: Examples are optional in the schema
	if len(schema.Examples) > 0 {
		for _, example := range schema.Examples {
			assert.NotEmpty(t, example.Name, "Example should have a name")
			assert.NotEmpty(t, example.Description, "Example should have a description")
		}
	}
}

// TestIntegration_MultipleFactoryTypes tests behavior with multiple rule types
// NOTE: This test will expand as more rule factories are added (battery_monitor, etc.)
func TestIntegration_MultipleFactoryTypes(t *testing.T) {
	ruleTypes := rule.GetRegisteredRuleTypes()

	// Verify at least test_rule exists
	assert.GreaterOrEqual(t, len(ruleTypes), 1, "Should have at least test_rule factory")

	// Verify each factory type is accessible and unique
	seenTypes := make(map[string]bool)
	for _, ruleType := range ruleTypes {
		assert.False(t, seenTypes[ruleType], "Rule type %s appears multiple times", ruleType)
		seenTypes[ruleType] = true

		// Verify factory is accessible
		factory, exists := rule.GetRuleFactory(ruleType)
		require.True(t, exists, "Factory for type %s should exist", ruleType)

		// Verify factory type matches
		assert.Equal(t, ruleType, factory.Type(), "Factory type mismatch for %s", ruleType)
	}

	// Log registered factories for debugging
	t.Logf("Registered rule types: %v", ruleTypes)
}

// TestIntegration_FactoryErrorHandling tests factory error conditions
func TestIntegration_FactoryErrorHandling(t *testing.T) {
	testFactory, exists := rule.GetRuleFactory("test_rule")
	require.True(t, exists)
	require.NotNil(t, testFactory)

	// Test creating rule with mismatched type
	invalidDef := rule.RuleDefinition{
		ID:   "invalid-001",
		Type: "nonexistent_type", // Wrong type
		Name: "Invalid Type Rule",
	}

	// Note: test_rule factory accepts any type (permissive for testing)
	// This test documents the behavior; stricter factories would error here
	_, err := testFactory.Create("invalid-001", invalidDef, rule.RuleDependencies{})

	// test_rule factory is permissive, so this should not error
	// Future factories (battery_monitor, etc.) should validate type strictly
	assert.NoError(t, err, "test_rule factory is permissive for testing purposes")
}
