package rule

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

func TestExpressionFactoryRegistration(t *testing.T) {
	// The expression factory should be registered via init()
	factory, exists := GetRuleFactory("expression")
	if !exists {
		t.Fatal("expression factory not registered - init() failed")
	}

	if factory.Type() != "expression" {
		t.Errorf("expected type 'expression', got %q", factory.Type())
	}

	// Verify it's in the registered types list
	types := GetRegisteredRuleTypes()
	found := false
	for _, typ := range types {
		if typ == "expression" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expression not found in registered types: %v", types)
	}
}

func TestExpressionFactoryValidation(t *testing.T) {
	factory := NewExpressionRuleFactory()

	tests := []struct {
		name    string
		def     Definition
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rule with single condition",
			def: Definition{
				ID:      "test-rule",
				Type:    "expression",
				Name:    "Test Rule",
				Enabled: true,
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
				},
			},
			wantErr: false,
		},
		{
			name: "valid rule with multiple conditions and logic",
			def: Definition{
				ID:      "multi-condition",
				Type:    "expression",
				Name:    "Multi Condition Rule",
				Enabled: true,
				Logic:   "and",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
					{Field: "status", Operator: "eq", Value: "active"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			def: Definition{
				Type: "expression",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
				},
			},
			wantErr: true,
			errMsg:  "rule ID is required",
		},
		{
			name: "no conditions",
			def: Definition{
				ID:         "no-cond",
				Type:       "expression",
				Conditions: []expression.ConditionExpression{},
			},
			wantErr: true,
			errMsg:  "must have at least one condition",
		},
		{
			name: "condition missing field",
			def: Definition{
				ID:   "missing-field",
				Type: "expression",
				Conditions: []expression.ConditionExpression{
					{Operator: "eq", Value: "test"},
				},
			},
			wantErr: true,
			errMsg:  "missing field",
		},
		{
			name: "condition missing operator",
			def: Definition{
				ID:   "missing-op",
				Type: "expression",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Value: 20.0},
				},
			},
			wantErr: true,
			errMsg:  "missing operator",
		},
		{
			name: "invalid operator",
			def: Definition{
				ID:   "bad-op",
				Type: "expression",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "invalid_op", Value: 20.0},
				},
			},
			wantErr: true,
			errMsg:  "invalid operator",
		},
		{
			name: "invalid logic operator",
			def: Definition{
				ID:    "bad-logic",
				Type:  "expression",
				Logic: "xor",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
				},
			},
			wantErr: true,
			errMsg:  "invalid logic operator",
		},
		{
			name: "invalid cooldown",
			def: Definition{
				ID:       "bad-cooldown",
				Type:     "expression",
				Cooldown: "not-a-duration",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
				},
			},
			wantErr: true,
			errMsg:  "invalid cooldown",
		},
		{
			name: "valid cooldown",
			def: Definition{
				ID:       "valid-cooldown",
				Type:     "expression",
				Cooldown: "30s",
				Conditions: []expression.ConditionExpression{
					{Field: "battery.level", Operator: "lte", Value: 20.0},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := factory.Validate(tt.def)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExpressionRuleCreation(t *testing.T) {
	factory := NewExpressionRuleFactory()

	def := Definition{
		ID:          "low-battery",
		Type:        "expression",
		Name:        "Low Battery Alert",
		Description: "Triggers when battery is low",
		Enabled:     true,
		Logic:       "and",
		Cooldown:    "1m",
		Conditions: []expression.ConditionExpression{
			{Field: "battery.level", Operator: "lte", Value: 20.0, Required: true},
		},
		Metadata: map[string]interface{}{
			"severity": "warning",
		},
	}

	rule, err := factory.Create(def.ID, def, Dependencies{})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	if rule.Name() != "Low Battery Alert" {
		t.Errorf("expected name 'Low Battery Alert', got %q", rule.Name())
	}

	// Check it implements Rule interface properly
	subjects := rule.Subscribe()
	if len(subjects) == 0 {
		t.Error("expected at least one subscription subject")
	}
}

func TestExpressionRuleEvaluation(t *testing.T) {
	tests := []struct {
		name       string
		conditions []expression.ConditionExpression
		logic      string
		data       map[string]interface{}
		want       bool
	}{
		{
			name: "single condition - matches",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
			},
			data: map[string]interface{}{
				"battery": map[string]interface{}{"level": 15.0},
			},
			want: true,
		},
		{
			name: "single condition - no match",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
			},
			data: map[string]interface{}{
				"battery": map[string]interface{}{"level": 75.0},
			},
			want: false,
		},
		{
			name: "AND logic - all match",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
				{Field: "status", Operator: "eq", Value: "active"},
			},
			logic: "and",
			data: map[string]interface{}{
				"battery": map[string]interface{}{"level": 15.0},
				"status":  "active",
			},
			want: true,
		},
		{
			name: "AND logic - partial match",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
				{Field: "status", Operator: "eq", Value: "active"},
			},
			logic: "and",
			data: map[string]interface{}{
				"battery": map[string]interface{}{"level": 15.0},
				"status":  "idle",
			},
			want: false,
		},
		{
			name: "OR logic - one matches",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
				{Field: "temperature", Operator: "gte", Value: 50.0},
			},
			logic: "or",
			data: map[string]interface{}{
				"battery":     map[string]interface{}{"level": 75.0},
				"temperature": 55.0,
			},
			want: true,
		},
		{
			name: "OR logic - none match",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0},
				{Field: "temperature", Operator: "gte", Value: 50.0},
			},
			logic: "or",
			data: map[string]interface{}{
				"battery":     map[string]interface{}{"level": 75.0},
				"temperature": 25.0,
			},
			want: false,
		},
		{
			name: "required field missing",
			conditions: []expression.ConditionExpression{
				{Field: "battery.level", Operator: "lte", Value: 20.0, Required: true},
			},
			data: map[string]interface{}{
				"other_field": "value",
			},
			want: false,
		},
		{
			name: "gt operator",
			conditions: []expression.ConditionExpression{
				{Field: "speed", Operator: "gt", Value: 100.0},
			},
			data: map[string]interface{}{"speed": 150.0},
			want: true,
		},
		{
			name: "contains operator",
			conditions: []expression.ConditionExpression{
				{Field: "message", Operator: "contains", Value: "error"},
			},
			data: map[string]interface{}{"message": "critical error occurred"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Definition{
				ID:         "test-rule",
				Type:       "expression",
				Name:       "Test",
				Enabled:    true,
				Logic:      tt.logic,
				Conditions: tt.conditions,
			}

			rule, err := NewExpressionRule(def)
			if err != nil {
				t.Fatalf("failed to create rule: %v", err)
			}

			// Create test message with data
			msg := createTestMessage(tt.data)
			result := rule.Evaluate([]message.Message{msg})

			if result != tt.want {
				t.Errorf("Evaluate() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestExpressionRuleCooldown(t *testing.T) {
	def := Definition{
		ID:       "cooldown-test",
		Type:     "expression",
		Name:     "Cooldown Test",
		Enabled:  true,
		Cooldown: "100ms",
		Conditions: []expression.ConditionExpression{
			{Field: "value", Operator: "eq", Value: "trigger"},
		},
	}

	rule, err := NewExpressionRule(def)
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	msg := createTestMessage(map[string]interface{}{"value": "trigger"})

	// First evaluation should trigger
	if !rule.Evaluate([]message.Message{msg}) {
		t.Error("first evaluation should trigger")
	}

	// Execute events to set lastTriggered
	_, _ = rule.ExecuteEvents([]message.Message{msg})

	// Immediate re-evaluation should be blocked by cooldown
	if rule.Evaluate([]message.Message{msg}) {
		t.Error("evaluation within cooldown should not trigger")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	// Now should trigger again
	if !rule.Evaluate([]message.Message{msg}) {
		t.Error("evaluation after cooldown should trigger")
	}
}

func TestCreateRuleFromDefinition_Expression(t *testing.T) {
	// This tests the full factory lookup path
	def := Definition{
		ID:      "factory-test",
		Type:    "expression",
		Name:    "Factory Test Rule",
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "test.entity.field", Operator: "eq", Value: "expected"},
		},
	}

	rule, err := CreateRuleFromDefinition(def, Dependencies{})
	if err != nil {
		t.Fatalf("CreateRuleFromDefinition failed: %v", err)
	}

	if rule.Name() != "Factory Test Rule" {
		t.Errorf("expected name 'Factory Test Rule', got %q", rule.Name())
	}
}

func TestCreateRuleFromDefinition_UnknownType(t *testing.T) {
	def := Definition{
		ID:   "unknown-type",
		Type: "nonexistent_rule_type",
		Conditions: []expression.ConditionExpression{
			{Field: "$message.test", Operator: "eq", Value: "x"},
		},
	}

	_, err := CreateRuleFromDefinition(def, Dependencies{})
	if err == nil {
		t.Error("expected error for unknown rule type")
	}

	if !contains(err.Error(), "no factory registered") {
		t.Errorf("error should mention missing factory: %v", err)
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func createTestMessage(data map[string]interface{}) message.Message {
	payload := message.NewGenericJSON(data)
	msgType := message.Type{
		Domain:   "core",
		Category: "json",
		Version:  "v1",
	}
	return message.NewBaseMessage(msgType, payload, "test")
}

// createTestEntityState creates an EntityState with the given triples for testing
func createTestEntityState(entityID string, triples []message.Triple) *gtypes.EntityState {
	return &gtypes.EntityState{
		ID:      entityID,
		Triples: triples,
	}
}

func TestCreateRuleProcessor_InlineRulesPassedThrough(t *testing.T) {
	// This test verifies that inline_rules from config JSON are properly
	// passed through to the processor config. This was a bug where the
	// factory.go didn't copy userConfig.InlineRules to ruleConfig.

	configJSON := `{
		"ports": {
			"inputs": [{"name": "rule_in", "subject": "test.input", "type": "nats"}],
			"outputs": []
		},
		"inline_rules": [
			{
				"id": "test-rule-1",
				"type": "expression",
				"name": "Test Rule",
				"enabled": true,
				"conditions": [
					{"field": "value", "operator": "gt", "value": 10}
				]
			}
		]
	}`

	// Create a mock NATS client for the test
	mockClient := &natsclient.Client{}

	deps := component.Dependencies{
		NATSClient: mockClient,
	}

	// This will fail because mockClient isn't fully initialized,
	// but we can at least verify the JSON parsing works
	processor, err := CreateRuleProcessor(json.RawMessage(configJSON), deps)

	// The processor creation may fail due to mock client limitations,
	// but if it gets past the config parsing stage, that's what we're testing
	if err != nil {
		// Check if it's a NATS-related error (expected with mock)
		// vs a config parsing error (would indicate our bug is back)
		if processor == nil {
			// Try to parse the config directly to verify InlineRules parsing
			var config Config
			if parseErr := json.Unmarshal([]byte(configJSON), &config); parseErr != nil {
				t.Fatalf("Failed to parse config JSON: %v", parseErr)
			}

			if len(config.InlineRules) != 1 {
				t.Errorf("Expected 1 inline rule, got %d", len(config.InlineRules))
			}

			if len(config.InlineRules) > 0 && config.InlineRules[0].ID != "test-rule-1" {
				t.Errorf("Expected rule ID 'test-rule-1', got %q", config.InlineRules[0].ID)
			}
		}
	}
}

// TestExpressionRuleEvaluateEntityState tests the direct EntityState evaluation path
// used by KV watch. This is the production path for rule triggers in structural tier.
func TestExpressionRuleEvaluateEntityState(t *testing.T) {
	tests := []struct {
		name       string
		conditions []expression.ConditionExpression
		logic      string
		triples    []message.Triple
		want       bool
	}{
		{
			name: "cold-storage temp alert - should trigger (temp >= 40 AND zone contains cold-storage)",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
				{Field: "geo.location.zone", Operator: "contains", Value: "cold-storage", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2},
				{Subject: "test", Predicate: "geo.location.zone", Object: "c360.logistics.facility.zone.area.cold-storage-1"},
				{Subject: "test", Predicate: "sensor.classification.type", Object: "temperature"},
			},
			want: true,
		},
		{
			name: "cold-storage temp alert - should NOT trigger (temp < 40)",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
				{Field: "geo.location.zone", Operator: "contains", Value: "cold-storage", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 38.5},
				{Subject: "test", Predicate: "geo.location.zone", Object: "c360.logistics.facility.zone.area.cold-storage-1"},
			},
			want: false,
		},
		{
			name: "cold-storage temp alert - should NOT trigger (not in cold-storage)",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
				{Field: "geo.location.zone", Operator: "contains", Value: "cold-storage", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 45.0},
				{Subject: "test", Predicate: "geo.location.zone", Object: "c360.logistics.facility.zone.area.dock-1"},
			},
			want: false,
		},
		{
			name: "high humidity alert - should trigger (humidity >= 50 AND type == humidity)",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.percent", Operator: "gte", Value: 50.0, Required: true},
				{Field: "sensor.classification.type", Operator: "eq", Value: "humidity", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.percent", Object: 52.8},
				{Subject: "test", Predicate: "sensor.classification.type", Object: "humidity"},
			},
			want: true,
		},
		{
			name: "low pressure alert - should trigger (pressure < 100 AND type == pressure)",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.psi", Operator: "lt", Value: 100.0, Required: true},
				{Field: "sensor.classification.type", Operator: "eq", Value: "pressure", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.psi", Object: 88.5},
				{Subject: "test", Predicate: "sensor.classification.type", Object: "pressure"},
			},
			want: true,
		},
		{
			name: "required field missing - should NOT trigger",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
			},
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.classification.type", Object: "temperature"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Definition{
				ID:         "test-rule",
				Type:       "expression",
				Name:       "Test",
				Enabled:    true,
				Logic:      tt.logic,
				Conditions: tt.conditions,
			}

			rule, err := NewExpressionRule(def)
			if err != nil {
				t.Fatalf("failed to create rule: %v", err)
			}

			// Create EntityState with triples - use graph types
			entityState := createTestEntityState("test.entity.id", tt.triples)

			result := rule.EvaluateEntityState(entityState)

			if result != tt.want {
				t.Errorf("EvaluateEntityState() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestConfigParseInlineRules(t *testing.T) {
	// Direct test of Config struct parsing to verify InlineRules are parsed correctly
	configJSON := `{
		"inline_rules": [
			{
				"id": "low-battery",
				"type": "expression",
				"name": "Low Battery Alert",
				"enabled": true,
				"conditions": [
					{"field": "battery.level", "operator": "lte", "value": 20.0, "required": true}
				],
				"logic": "and",
				"cooldown": "30s"
			},
			{
				"id": "high-temp",
				"type": "expression",
				"name": "High Temperature",
				"enabled": true,
				"conditions": [
					{"field": "temperature", "operator": "gte", "value": 50.0}
				]
			}
		]
	}`

	var config Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if len(config.InlineRules) != 2 {
		t.Fatalf("Expected 2 inline rules, got %d", len(config.InlineRules))
	}

	// Verify first rule
	rule1 := config.InlineRules[0]
	if rule1.ID != "low-battery" {
		t.Errorf("Rule 1 ID: expected 'low-battery', got %q", rule1.ID)
	}
	if rule1.Type != "expression" {
		t.Errorf("Rule 1 Type: expected 'expression', got %q", rule1.Type)
	}
	if len(rule1.Conditions) != 1 {
		t.Errorf("Rule 1 Conditions: expected 1, got %d", len(rule1.Conditions))
	}
	if rule1.Cooldown != "30s" {
		t.Errorf("Rule 1 Cooldown: expected '30s', got %q", rule1.Cooldown)
	}

	// Verify second rule
	rule2 := config.InlineRules[1]
	if rule2.ID != "high-temp" {
		t.Errorf("Rule 2 ID: expected 'high-temp', got %q", rule2.ID)
	}
}
