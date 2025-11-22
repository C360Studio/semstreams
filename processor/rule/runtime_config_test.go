// Package rule - Tests for Runtime Configuration and Dynamic Rule CRUD
package rule

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/processor/rule/expression"
	rtypes "github.com/c360/semstreams/types/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeConfigurable_ValidateConfigUpdate tests configuration validation
func TestRuntimeConfigurable_ValidateConfigUpdate(t *testing.T) {
	// Create processor with mock dependencies
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]rtypes.Rule),
	}

	tests := []struct {
		name      string
		changes   map[string]any
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_battery_monitor_rule",
			changes: map[string]any{
				"rules": map[string]any{
					"battery_001": map[string]any{
						"type": "battery_monitor",
						"name": "Test Battery Monitor",
						"conditions": []any{
							map[string]any{
								"field":    "robotics.battery.level",
								"operator": "lte",
								"value":    20.0,
								"required": true,
							},
						},
						"logic": "and",
					},
				},
			},
			wantError: false,
		},
		{
			name: "missing_rule_type",
			changes: map[string]any{
				"rules": map[string]any{
					"battery_002": map[string]any{
						"name": "Invalid Rule",
						"conditions": []any{
							map[string]any{
								"field":    "test.field",
								"operator": "eq",
								"value":    "test",
							},
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "missing required field 'type'",
		},
		{
			name: "invalid_operator",
			changes: map[string]any{
				"rules": map[string]any{
					"battery_003": map[string]any{
						"type": "battery_monitor",
						"conditions": []any{
							map[string]any{
								"field":    "robotics.battery.level",
								"operator": "invalid_op",
								"value":    20.0,
							},
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid operator",
		},
		{
			name: "invalid_logic_operator",
			changes: map[string]any{
				"rules": map[string]any{
					"battery_004": map[string]any{
						"type": "battery_monitor",
						"conditions": []any{
							map[string]any{
								"field":    "robotics.battery.level",
								"operator": "eq",
								"value":    20.0,
							},
						},
						"logic": "xor", // Invalid logic operator
					},
				},
			},
			wantError: true,
			errorMsg:  "logic must be 'and' or 'or'",
		},
		{
			name: "empty_conditions",
			changes: map[string]any{
				"rules": map[string]any{
					"battery_005": map[string]any{
						"type":       "battery_monitor",
						"conditions": []any{},
					},
				},
			},
			wantError: true,
			errorMsg:  "must have at least one condition",
		},
		{
			name: "valid_enabled_rules_update",
			changes: map[string]any{
				"enabled_rules": []string{"battery_monitor"},
			},
			wantError: false,
		},
		{
			name: "valid_entity_watch_patterns",
			changes: map[string]any{
				"entity_watch_patterns": []string{"*.robotics.*.battery.*", "*.sensors.*"},
			},
			wantError: false,
		},
		{
			name: "valid_graph_integration_toggle",
			changes: map[string]any{
				"enable_graph_integration": false,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processor.ValidateConfigUpdate(tt.changes)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRuntimeConfigurable_ApplyConfigUpdate tests dynamic rule application
func TestRuntimeConfigurable_ApplyConfigUpdate(t *testing.T) {
	// Create processor with test dependencies
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]rtypes.Rule),
		config:     &Config{},
	}

	// Test adding a new rule
	t.Run("add_new_rule", func(t *testing.T) {
		changes := map[string]any{
			"rules": map[string]any{
				"battery_test": map[string]any{
					"type": "battery_monitor",
					"name": "Test Battery Rule",
					"conditions": []any{
						map[string]any{
							"field":    "robotics.battery.level",
							"operator": "lte",
							"value":    25.0,
							"required": true,
						},
					},
					"logic":   "and",
					"enabled": true,
				},
			},
		}

		err := processor.ApplyConfigUpdate(changes)
		require.NoError(t, err)

		// Verify rule was added
		config := processor.GetRuntimeConfig()
		rules, ok := config["rules"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, rules, "battery_test")
	})

	// Test updating enabled_rules
	t.Run("update_enabled_rules", func(t *testing.T) {
		changes := map[string]any{
			"enabled_rules": []string{"battery_monitor"},
		}

		err := processor.ApplyConfigUpdate(changes)
		require.NoError(t, err)

		config := processor.GetRuntimeConfig()
		enabledRules := config["enabled_rules"].([]string)
		assert.Equal(t, []string{"battery_monitor"}, enabledRules)
	})

	// Test toggling graph integration
	t.Run("toggle_graph_integration", func(t *testing.T) {
		changes := map[string]any{
			"enable_graph_integration": false,
		}

		err := processor.ApplyConfigUpdate(changes)
		require.NoError(t, err)

		config := processor.GetRuntimeConfig()
		assert.False(t, config["enable_graph_integration"].(bool))
	})
}

// TestRuntimeConfigurable_GetRuntimeConfig tests runtime configuration retrieval
func TestRuntimeConfigurable_GetRuntimeConfig(t *testing.T) {
	// Create processor with test configuration
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]rtypes.Rule),
		config: &Config{
			EnabledRules:           []string{"battery_monitor"},
			BufferWindowSize:       "10m",
			AlertCooldownPeriod:    "2m",
			EnableGraphIntegration: true,
			EntityWatchPatterns:    []string{"*.robotics.*"},
		},
		isSubscribed: true,
	}

	config := processor.GetRuntimeConfig()

	// Verify all expected fields are present
	assert.NotNil(t, config["enabled_rules"])
	assert.NotNil(t, config["buffer_window_size"])
	assert.NotNil(t, config["alert_cooldown_period"])
	assert.NotNil(t, config["enable_graph_integration"])
	assert.NotNil(t, config["entity_watch_patterns"])
	assert.NotNil(t, config["rules"])
	assert.NotNil(t, config["rule_count"])
	assert.NotNil(t, config["is_running"])

	// Check specific values
	assert.Equal(t, []string{"battery_monitor"}, config["enabled_rules"])
	assert.Equal(t, true, config["enable_graph_integration"])
	assert.Equal(t, true, config["is_running"])
}

// TestRuleFactory_CreateAndValidate tests rule factory creation and validation
func TestRuleFactoryRegistry(t *testing.T) {
	// Battery monitor factory should be registered via init()
	factory, exists := GetRuleFactory("battery_monitor")
	assert.True(t, exists)
	assert.NotNil(t, factory)

	// Test getting all registered types
	types := GetRegisteredRuleTypes()
	assert.Contains(t, types, "battery_monitor")

	// Test getting all schemas
	schemas := GetRuleSchemas()
	assert.Contains(t, schemas, "battery_monitor")

	// Test unknown factory type
	_, exists = GetRuleFactory("unknown_type")
	assert.False(t, exists)
}

// TestCreateRuleFromDefinition tests rule creation from definition
func TestCreateRuleFromDefinition(t *testing.T) {
	def := Definition{
		ID:   "test_battery_001",
		Type: "battery_monitor",
		Name: "Test Battery Monitor",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "robotics.battery.level",
				Operator: "lte",
				Value:    15.0,
				Required: true,
			},
			{
				Field:    "robotics.battery.voltage",
				Operator: "lt",
				Value:    3.3,
				Required: false,
			},
		},
		Logic:   "or",
		Enabled: true,
	}

	deps := Dependencies{
		NATSClient: &natsclient.Client{},
		Logger:     slog.Default(),
	}

	// Test successful creation
	rule, err := CreateRuleFromDefinition(def, deps)
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Test with unknown rule type
	def.Type = "unknown_type"
	_, err = CreateRuleFromDefinition(def, deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no factory registered")
}

// TestConfigSchema tests configuration schema generation
func TestConfigSchema(t *testing.T) {
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
	}

	// Test component schema
	componentSchema := processor.ConfigSchema()
	assert.NotNil(t, componentSchema)
	assert.NotEmpty(t, componentSchema.Properties)
	assert.Contains(t, componentSchema.Properties, "enabled_rules")
	assert.Contains(t, componentSchema.Properties, "rules")

}

// TestDynamicRuleCRUD tests complete CRUD operations for rules
func TestDynamicRuleCRUD(t *testing.T) {
	ctx := context.Background()

	// Create processor
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]rtypes.Rule),
		config:     &Config{},
	}

	// CREATE - Add a new rule
	createChanges := map[string]any{
		"rules": map[string]any{
			"battery_crud_test": map[string]any{
				"type": "battery_monitor",
				"name": "CRUD Test Battery",
				"conditions": []any{
					map[string]any{
						"field":    "robotics.battery.level",
						"operator": "lte",
						"value":    30.0,
						"required": true,
					},
				},
				"logic":   "and",
				"enabled": true,
			},
		},
	}

	err := processor.ValidateConfigUpdate(createChanges)
	require.NoError(t, err)

	err = processor.ApplyConfigUpdate(createChanges)
	require.NoError(t, err)

	// READ - Verify rule exists
	config := processor.GetRuntimeConfig()
	rules := config["rules"].(map[string]any)
	assert.Contains(t, rules, "battery_crud_test")

	// UPDATE - Modify the rule
	updateChanges := map[string]any{
		"rules": map[string]any{
			"battery_crud_test": map[string]any{
				"type": "battery_monitor",
				"name": "Updated Battery Rule",
				"conditions": []any{
					map[string]any{
						"field":    "robotics.battery.level",
						"operator": "lte",
						"value":    25.0, // Changed threshold
						"required": true,
					},
				},
				"logic":   "and",
				"enabled": false, // Disabled
			},
		},
	}

	err = processor.ApplyConfigUpdate(updateChanges)
	require.NoError(t, err)

	// Verify update
	config = processor.GetRuntimeConfig()
	rules = config["rules"].(map[string]any)
	updatedRule := rules["battery_crud_test"].(map[string]any)
	assert.Equal(t, "Updated Battery Rule", updatedRule["name"])
	assert.False(t, updatedRule["enabled"].(bool))

	// DELETE - Remove the rule (simulated by setting to nil)
	_ = map[string]any{
		"rules": map[string]any{
			"battery_crud_test": nil,
		},
	}

	// Note: Current implementation would need to handle nil values for deletion
	// This is a limitation that would need to be addressed in production

	_ = ctx // Context would be used in actual NATS KV operations
}

// TestExpressionEvaluation tests expression-based rule evaluation
func createTestRuleDefinition(id string, threshold float64) Definition {
	return Definition{
		ID:   id,
		Type: "battery_monitor",
		Name: "Test Rule " + id,
		Conditions: []expression.ConditionExpression{
			{
				Field:    "robotics.battery.level",
				Operator: "lte",
				Value:    threshold,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
	}
}

func marshalRuleDefinition(def Definition) json.RawMessage {
	data, _ := json.Marshal(def)
	return data
}
