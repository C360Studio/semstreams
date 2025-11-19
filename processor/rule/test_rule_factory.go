// Package rule - Test Rule Factory for Integration Tests
package rule

import (
	"fmt"
	"strings"

	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/processor/rule/expression"
	gtypes "github.com/c360/semstreams/types/graph"
	rtypes "github.com/c360/semstreams/types/rule"
)

// TestRule is a functional rule implementation for testing
type TestRule struct {
	id          string
	name        string
	subscribed  []string
	enabled     bool
	conditions  []expression.ConditionExpression
	shouldTrigger bool // Set to true when conditions match
}

// NewTestRule creates a new test rule
func NewTestRule(id, name string, subjects []string, conditions []expression.ConditionExpression) *TestRule {
	return &TestRule{
		id:         id,
		name:       name,
		subscribed: subjects,
		enabled:    true,
		conditions: conditions,
	}
}

// Name returns the rule name
func (r *TestRule) Name() string {
	return r.name
}

// Subscribe returns subjects this rule subscribes to
func (r *TestRule) Subscribe() []string {
	return r.subscribed
}

// Evaluate evaluates the rule against messages
func (r *TestRule) Evaluate(messages []message.Message) bool {
	if !r.enabled || len(messages) == 0 {
		return false
	}

	// For test purposes, evaluate the last message
	msg := messages[len(messages)-1]

	// Get payload data - try to cast to GenericJSONPayload
	payload := msg.Payload()
	genericPayload, ok := payload.(*message.GenericJSONPayload)
	if !ok {
		return false
	}

	data := genericPayload.Data
	if data == nil {
		return false
	}

	// Evaluate conditions if present
	if len(r.conditions) > 0 {
		return r.evaluateConditions(data)
	}

	// Default: trigger if we have data
	r.shouldTrigger = len(data) > 0
	return r.shouldTrigger
}

// evaluateConditions checks if message data matches rule conditions
func (r *TestRule) evaluateConditions(data map[string]interface{}) bool {
	for _, cond := range r.conditions {
		// Extract nested field value (e.g., "battery.level")
		value := extractNestedValue(data, cond.Field)
		if value == nil {
			return false
		}

		// Evaluate condition
		if !evaluateCondition(value, cond.Operator, cond.Value) {
			return false
		}
	}

	r.shouldTrigger = true
	return true
}

// ExecuteEvents generates events when rule triggers
func (r *TestRule) ExecuteEvents(messages []message.Message) ([]gtypes.Event, error) {
	if !r.shouldTrigger || len(messages) == 0 {
		return []gtypes.Event{}, nil
	}

	msg := messages[len(messages)-1]

	// Create a rule trigger event
	event := gtypes.Event{
		Type:     gtypes.EventEntityUpdate, // Using a standard event type
		EntityID: "test.entity." + r.id,
		Properties: map[string]interface{}{
			"rule_id":    r.id,
			"rule_name":  r.name,
			"message_id": msg.ID(),
			"triggered":  true,
		},
		Metadata: gtypes.EventMetadata{
			Source:    r.name,
			Timestamp: msg.Meta().CreatedAt(),
			Reason:    "Rule triggered",
			RuleName:  r.name,
			Version:   "1.0.0",
		},
		Confidence: 1.0,
	}

	r.shouldTrigger = false // Reset trigger state
	return []gtypes.Event{event}, nil
}

// extractNestedValue extracts a value from nested map using dot notation
func extractNestedValue(data map[string]interface{}, field string) interface{} {
	parts := strings.Split(field, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			// Last part - return the value
			return current[part]
		}

		// Navigate deeper
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}

	return nil
}

// evaluateCondition evaluates a single condition
func evaluateCondition(value interface{}, operator string, expected interface{}) bool {
	switch operator {
	case "eq":
		return value == expected
	case "ne":
		return value != expected
	case "lt":
		return compareNumbers(value, expected) < 0
	case "lte":
		return compareNumbers(value, expected) <= 0
	case "gt":
		return compareNumbers(value, expected) > 0
	case "gte":
		return compareNumbers(value, expected) >= 0
	default:
		return false
	}
}

// compareNumbers compares two values as numbers
func compareNumbers(a, b interface{}) int {
	aFloat := toFloat64(a)
	bFloat := toFloat64(b)

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

// toFloat64 converts a value to float64
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	default:
		return 0
	}
}

// TestRuleFactory creates test rules for integration testing
type TestRuleFactory struct {
	ruleType string
}

// NewTestRuleFactory creates a new test rule factory
func NewTestRuleFactory() *TestRuleFactory {
	return &TestRuleFactory{
		ruleType: "test_rule",
	}
}

// Type returns the factory type
func (f *TestRuleFactory) Type() string {
	return f.ruleType
}

// Create creates a test rule from definition
func (f *TestRuleFactory) Create(ruleID string, def RuleDefinition, deps RuleDependencies) (rtypes.Rule, error) {
	// Create test rule with conditions from definition
	// For test rules, subscribe to all subjects by default to simplify testing
	subjects := []string{">"}

	// If the rule definition has entity patterns, subscribe to that pattern
	// This allows the rule to receive KV watcher updates for matching entity keys
	if def.Entity.Pattern != "" {
		subjects = []string{def.Entity.Pattern}
	}

	rule := NewTestRule(ruleID, def.Name, subjects, def.Conditions)
	return rule, nil
}

// Validate validates the rule definition
func (f *TestRuleFactory) Validate(def RuleDefinition) error {
	// Test rules accept any definition
	return nil
}

// Schema returns the test rule schema
func (f *TestRuleFactory) Schema() RuleSchema {
	return RuleSchema{
		Type:     "test_rule",
		Required: []string{"id", "name"},
	}
}

// Examples returns test rule examples
func (f *TestRuleFactory) Examples() []RuleExample {
	return []RuleExample{
		{
			Name:        "Basic Test Rule",
			Description: "Simple test rule for integration tests",
		},
	}
}

// init registers the test rule factory
func init() {
	// Register test_rule factory for integration tests
	factory := NewTestRuleFactory()
	if err := RegisterRuleFactory("test_rule", factory); err != nil {
		// Ignore duplicate registration errors in tests
		fmt.Printf("Warning: Failed to register test_rule factory: %v\n", err)
	}
}
