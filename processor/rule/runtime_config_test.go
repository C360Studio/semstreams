// Package rule - Tests for Runtime Configuration and Dynamic Rule CRUD
package rule

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeConfigurable_ValidateConfigUpdate tests configuration validation
func TestRuntimeConfigurable_ValidateConfigUpdate(t *testing.T) {
	// Create processor with mock dependencies
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]Rule),
	}

	tests := []struct {
		name      string
		changes   map[string]any
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_test_rule",
			changes: map[string]any{
				"rules": map[string]any{
					"test_001": map[string]any{
						"type": "test_rule",
						"name": "Test Rule",
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
								"field":    "test.test.field",
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
					"test_003": map[string]any{
						"type": "test_rule",
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
					"test_004": map[string]any{
						"type": "test_rule",
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
					"test_005": map[string]any{
						"type":       "test_rule",
						"conditions": []any{},
					},
				},
			},
			wantError: true,
			errorMsg:  "must have at least one condition",
		},
		{
			name: "entity_watch_buckets_require_restart",
			changes: map[string]any{
				"entity_watch_buckets": map[string][]string{
					"ENTITY_STATES": {"*.robotics.*.battery.*.*"},
				},
			},
			wantError: true,
			errorMsg:  "rules",
		},
		{
			name: "graph_integration_requires_restart",
			changes: map[string]any{
				"enable_graph_integration": false,
			},
			wantError: true,
			errorMsg:  "rules",
		},
		{
			name: "graph integration requires pack identity",
			changes: map[string]any{
				"enable_graph_integration": true,
			},
			wantError: true,
			errorMsg:  "rules",
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

func TestValidateConfigUpdateRejectsGraphIntegrationWithStablePackIdentity(t *testing.T) {
	processor := &Processor{config: &Config{PackID: "runtime-opt-in-pack"}}
	if err := processor.ValidateConfigUpdate(map[string]any{"enable_graph_integration": true}); err == nil {
		t.Fatal("ValidateConfigUpdate accepted non-rule runtime update")
	}
}

func TestRuntimeConfigRejectsStaticPackIDUpdate(t *testing.T) {
	processor := &Processor{config: &Config{PackID: "original-pack"}}
	changes := map[string]any{"pack_id": "replacement-pack"}
	if err := processor.ValidateConfigUpdate(changes); err == nil || !strings.Contains(err.Error(), "rules") {
		t.Fatalf("ValidateConfigUpdate pack_id error = %v, want static rejection", err)
	}
	if err := processor.ApplyConfigUpdate(context.Background(), changes); err == nil || !strings.Contains(err.Error(), "rules") {
		t.Fatalf("ApplyConfigUpdate pack_id error = %v, want static rejection", err)
	}
	if processor.config.PackID != "original-pack" {
		t.Fatalf("runtime update changed pack_id to %q", processor.config.PackID)
	}
}

// TestRuntimeConfigurable_ApplyConfigUpdate tests dynamic rule application
func TestRuntimeConfigurable_ApplyConfigUpdate(t *testing.T) {
	// Create processor with test dependencies
	// Use mustTestConfig(t, "rule-test-pack") to ensure valid duration strings and avoid parse warnings
	cfg := mustTestConfig(t, "rule-test-pack")
	processor := &Processor{
		natsClient:  &natsclient.Client{},
		logger:      slog.Default(),
		rules:       make(map[string]Rule),
		ruleConfigs: make(map[string]map[string]any),
		config:      &cfg,
	}

	// Test adding a new rule
	t.Run("add_new_rule", func(t *testing.T) {
		changes := map[string]any{
			"rules": map[string]any{
				"battery_test": map[string]any{
					"type": "test_rule",
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

		err := processor.ApplyConfigUpdate(context.Background(), changes)
		require.NoError(t, err)

		// Verify rule was added
		config := processor.GetRuntimeConfig()
		rules, ok := config["rules"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, rules, "battery_test")
	})

	// Non-rule component fields remain fixed until restart.
	t.Run("reject_graph_integration_toggle", func(t *testing.T) {
		before := processor.config.EnableGraphIntegration
		changes := map[string]any{
			"enable_graph_integration": false,
		}

		err := processor.ApplyConfigUpdate(context.Background(), changes)
		require.ErrorContains(t, err, "rules")

		config := processor.GetRuntimeConfig()
		assert.Equal(t, before, config["enable_graph_integration"].(bool))
	})
}

// TestRuntimeConfigurable_GetRuntimeConfig tests runtime configuration retrieval
func TestRuntimeConfigurable_GetRuntimeConfig(t *testing.T) {
	// Create processor with test configuration
	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]Rule),
		config: &Config{
			BufferWindowSize:       "10m",
			AlertCooldownPeriod:    "2m",
			PackID:                 "runtime-config-test",
			EnableGraphIntegration: true,
			EntityWatchBuckets:     map[string][]string{"ENTITY_STATES": {"*.robotics.*.*.*.*"}},
		},
		isSubscribed: true,
	}

	config := processor.GetRuntimeConfig()

	// Verify all expected fields are present
	assert.NotNil(t, config["buffer_window_size"])
	assert.NotNil(t, config["alert_cooldown_period"])
	assert.Equal(t, "runtime-config-test", config["pack_id"])
	assert.NotNil(t, config["enable_graph_integration"])
	assert.NotNil(t, config["entity_watch_buckets"])
	assert.NotNil(t, config["rules"])
	assert.NotNil(t, config["rule_count"])
	assert.NotNil(t, config["is_running"])

	// Check specific values
	assert.Equal(t, true, config["enable_graph_integration"])
	assert.Equal(t, true, config["is_running"])
}

// TestRuleFactory_CreateAndValidate tests rule factory creation and validation
func TestRuleFactoryRegistry(t *testing.T) {
	// Battery monitor factory should be registered via init()
	factory, exists := GetRuleFactory("test_rule")
	assert.True(t, exists)
	assert.NotNil(t, factory)

	// Test getting all registered types
	types := GetRegisteredRuleTypes()
	assert.Contains(t, types, "test_rule")

	// Test getting all schemas
	schemas := GetRuleSchemas()
	assert.Contains(t, schemas, "test_rule")

	// Test unknown factory type
	_, exists = GetRuleFactory("unknown_type")
	assert.False(t, exists)
}

// TestCreateRuleFromDefinition tests rule creation from definition
func TestCreateRuleFromDefinition(t *testing.T) {
	def := Definition{
		ID:   "test_battery_001",
		Type: "test_rule",
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
		PackID:     "runtime-config-factory-test",
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
	assert.Contains(t, componentSchema.Properties, "rules")

}

// TestDynamicRuleCRUD tests complete CRUD operations for rules
func TestDynamicRuleCRUD(t *testing.T) {
	ctx := context.Background()

	// Create processor with mustTestConfig(t, "rule-test-pack") to ensure valid duration strings
	cfg := mustTestConfig(t, "rule-test-pack")
	processor := &Processor{
		natsClient:  &natsclient.Client{},
		logger:      slog.Default(),
		rules:       make(map[string]Rule),
		ruleConfigs: make(map[string]map[string]any),
		config:      &cfg,
	}

	// CREATE - Add a new rule
	createChanges := map[string]any{
		"rules": map[string]any{
			"battery_crud_test": map[string]any{
				"type": "test_rule",
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

	err = processor.ApplyConfigUpdate(context.Background(), createChanges)
	require.NoError(t, err)

	// READ - Verify rule exists
	config := processor.GetRuntimeConfig()
	rules := config["rules"].(map[string]any)
	assert.Contains(t, rules, "battery_crud_test")

	// UPDATE - Modify the rule
	updateChanges := map[string]any{
		"rules": map[string]any{
			"battery_crud_test": map[string]any{
				"type": "test_rule",
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

	err = processor.ApplyConfigUpdate(context.Background(), updateChanges)
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
		Type: "test_rule",
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
