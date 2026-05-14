// Package rule - Test Rule Factory for Integration Tests
package rule

import (
	"fmt"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// TestRule is a functional rule implementation for testing
type TestRule struct {
	id            string
	name          string
	subscribed    []string
	enabled       bool
	conditions    []expression.ConditionExpression
	logic         string
	evaluator     *expression.Evaluator
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
		logic:      expression.LogicAnd,
		evaluator:  expression.NewExpressionEvaluator(),
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

// Evaluate evaluates the rule against messages.
// Routes through the unified expression.Evaluator — same field
// resolution semantics as production rules. See ADR-041.
func (r *TestRule) Evaluate(messages []message.Message) bool {
	if !r.enabled || len(messages) == 0 {
		return false
	}

	// For test purposes, evaluate the last message
	msg := messages[len(messages)-1]

	// Get payload data — only GenericJSONPayload supports field-keyed matching.
	payload := msg.Payload()
	genericPayload, ok := payload.(*message.GenericJSONPayload)
	if !ok {
		return false
	}

	data := genericPayload.Data
	if data == nil {
		return false
	}

	if len(r.conditions) == 0 {
		// No conditions - trigger if we have data
		r.shouldTrigger = len(data) > 0
		return r.shouldTrigger
	}

	expr := expression.LogicalExpression{
		Conditions: r.conditions,
		Logic:      r.logic,
	}
	result, err := r.evaluator.EvaluateWithStateAndMessage(nil, nil, expression.MessageFields(data), expr)
	if err != nil {
		return false
	}
	r.shouldTrigger = result
	return result
}

// EvaluateEntityState evaluates the rule against entity triples via the
// unified evaluator. Implements the EntityStateEvaluator interface for
// KV watch-based evaluation.
func (r *TestRule) EvaluateEntityState(entityState *gtypes.EntityState) bool {
	if !r.enabled || entityState == nil {
		return false
	}

	if len(r.conditions) == 0 {
		// No conditions - trigger on any entity state
		r.shouldTrigger = true
		return true
	}

	expr := expression.LogicalExpression{
		Conditions: r.conditions,
		Logic:      r.logic,
	}
	result, err := r.evaluator.Evaluate(entityState, expr)
	if err != nil {
		return false
	}
	r.shouldTrigger = result
	return result
}

// ExecuteEvents generates events when rule triggers
func (r *TestRule) ExecuteEvents(messages []message.Message) ([]Event, error) {
	if !r.shouldTrigger || len(messages) == 0 {
		return []Event{}, nil
	}

	msg := messages[len(messages)-1]

	// Create a rule trigger event
	event := gtypes.Event{
		Type:     gtypes.EventEntityUpdate, // Using a standard event type
		EntityID: "test.entity." + r.id,
		Properties: map[string]any{
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
	return []Event{&event}, nil
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
func (f *TestRuleFactory) Create(ruleID string, def Definition, _ Dependencies) (Rule, error) {
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
func (f *TestRuleFactory) Validate(_ Definition) error {
	// Test rules accept any definition
	return nil
}

// Schema returns the test rule schema
func (f *TestRuleFactory) Schema() Schema {
	return Schema{
		Type:     "test_rule",
		Required: []string{"id", "name"},
	}
}

// Examples returns test rule examples
func (f *TestRuleFactory) Examples() []Example {
	return []Example{
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
