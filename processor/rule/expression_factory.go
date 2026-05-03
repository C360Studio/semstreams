// Package rule - Expression Rule Factory for condition-based rules
package rule

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ExpressionRule implements Rule interface for expression-based condition evaluation
type ExpressionRule struct {
	id            string
	name          string
	description   string
	subscribed    []string
	enabled       bool
	conditions    []expression.ConditionExpression
	logic         string
	cooldown      time.Duration
	metadata      map[string]interface{}
	evaluator     *expression.Evaluator
	lastTriggered time.Time
	shouldTrigger bool
}

// NewExpressionRule creates a new expression-based rule
func NewExpressionRule(def Definition) (*ExpressionRule, error) {
	// Parse cooldown if specified
	var cooldown time.Duration
	if def.Cooldown != "" {
		var err error
		cooldown, err = time.ParseDuration(def.Cooldown)
		if err != nil {
			return nil, fmt.Errorf("invalid cooldown duration %q: %w", def.Cooldown, err)
		}
	}

	// Default logic to "and" if not specified
	logic := def.Logic
	if logic == "" {
		logic = expression.LogicAnd
	}

	// Determine subscription subjects
	subjects := []string{">"}
	if def.Entity.Pattern != "" {
		subjects = []string{def.Entity.Pattern}
	}

	return &ExpressionRule{
		id:          def.ID,
		name:        def.Name,
		description: def.Description,
		subscribed:  subjects,
		enabled:     def.IsActive(),
		conditions:  def.Conditions,
		logic:       logic,
		cooldown:    cooldown,
		metadata:    def.Metadata,
		evaluator:   expression.NewExpressionEvaluator(),
	}, nil
}

// Name returns the rule name
func (r *ExpressionRule) Name() string {
	return r.name
}

// Subscribe returns subjects this rule subscribes to
func (r *ExpressionRule) Subscribe() []string {
	return r.subscribed
}

// Evaluate evaluates the rule against messages
func (r *ExpressionRule) Evaluate(messages []message.Message) bool {
	if !r.enabled || len(messages) == 0 {
		return false
	}

	// Check cooldown
	if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {
		return false
	}

	// For expression rules, evaluate the last message
	msg := messages[len(messages)-1]

	// Get payload data - try multiple approaches
	payload := msg.Payload()
	var data map[string]interface{}

	// GenericJSONPayload with nested data structure (for NATS message-based rules)
	if genericPayload, ok := payload.(*message.GenericJSONPayload); ok {
		data = genericPayload.Data
	}

	if len(data) == 0 {
		return false
	}

	// Evaluate conditions
	if len(r.conditions) > 0 {
		r.shouldTrigger = r.evaluateConditions(data)
		return r.shouldTrigger
	}

	return false
}

// EvaluateEntityState evaluates the rule directly against EntityState triples.
// This bypasses the message transformation layer and evaluates conditions
// directly against triple predicates (e.g., "sensor.measurement.fahrenheit").
func (r *ExpressionRule) EvaluateEntityState(entityState *gtypes.EntityState) bool {
	if !r.enabled || entityState == nil {
		return false
	}

	// Check cooldown
	if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {
		return false
	}

	if len(r.conditions) == 0 {
		return false
	}

	// Build LogicalExpression from rule conditions
	expr := r.buildLogicalExpression()

	// Use the expression.Evaluator for direct triple evaluation
	result, err := r.evaluator.Evaluate(entityState, expr)
	if err != nil {
		slog.Debug("ExpressionRule: evaluation error",
			"rule", r.name,
			"entity_id", entityState.ID,
			"error", err)
		return false
	}

	r.shouldTrigger = result
	return result
}

// buildLogicalExpression converts rule conditions to expression.LogicalExpression
func (r *ExpressionRule) buildLogicalExpression() expression.LogicalExpression {
	return expression.LogicalExpression{
		Conditions: r.conditions,
		Logic:      r.logic,
	}
}

// evaluateConditions checks if message data matches rule conditions
func (r *ExpressionRule) evaluateConditions(data map[string]interface{}) bool {
	results := make([]bool, len(r.conditions))

	for i, cond := range r.conditions {
		// Extract nested field value (e.g., "battery.level")
		value := r.extractNestedValue(data, cond.Field)

		// Handle missing fields based on Required flag. Negate does NOT apply
		// to missing-field exits — the operator never ran, so there is no result
		// to invert. Field-absent always returns false.
		if value == nil {
			if cond.Required {
				return false // Required field missing - fail immediately
			}
			results[i] = false
			continue
		}

		// Evaluate condition, then apply negate if requested
		result := r.evaluateCondition(value, cond.Operator, cond.Value)
		if cond.Negate {
			result = !result
		}
		results[i] = result
	}

	// Apply logic operator
	switch r.logic {
	case expression.LogicOr:
		for _, result := range results {
			if result {
				return true
			}
		}
		return false
	case expression.LogicNot:
		// Config validation ensures exactly one condition; defensive guard here.
		if len(results) != 1 {
			return false
		}
		return !results[0]
	case expression.LogicAnd:
		fallthrough
	default:
		for _, result := range results {
			if !result {
				return false
			}
		}
		return true
	}
}

// extractNestedValue extracts a value from nested map using dot notation.
//
// Security invariant: fields starting with "$" are reserved pseudo-field
// namespaces ($caller.*, $state.*, $prev.*, $schedule.*, etc.) that must
// NOT be resolvable from body data. Returning nil (not-found) for any
// $-prefixed field forces the body-path evaluateConditions to return false,
// ensuring the trusted re-evaluation path in stateful_evaluator.go is the
// sole source of truth for identity-gated conditions.
func (r *ExpressionRule) extractNestedValue(data map[string]interface{}, field string) interface{} {
	// Refuse to look up reserved pseudo-field namespaces against body data.
	// An attacker publishing body {"$caller": {"role": "admin"}} must not be
	// able to satisfy a $caller.role condition; the stateful evaluator's
	// reEvaluateWithCallerFields (trusted re-eval) handles those fields.
	if strings.HasPrefix(field, "$") {
		return nil
	}

	parts := strings.Split(field, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}

		next, ok := current[part].(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}

	return nil
}

// evaluateCondition evaluates a single condition
func (r *ExpressionRule) evaluateCondition(value interface{}, operator string, expected interface{}) bool {
	switch operator {
	case "eq":
		return compareValues(value, expected) == 0
	case "ne":
		return compareValues(value, expected) != 0
	case "lt":
		return compareNumeric(value, expected) < 0
	case "lte":
		return compareNumeric(value, expected) <= 0
	case "gt":
		return compareNumeric(value, expected) > 0
	case "gte":
		return compareNumeric(value, expected) >= 0
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", value), fmt.Sprintf("%v", expected))
	case "starts_with":
		return strings.HasPrefix(fmt.Sprintf("%v", value), fmt.Sprintf("%v", expected))
	case "ends_with":
		return strings.HasSuffix(fmt.Sprintf("%v", value), fmt.Sprintf("%v", expected))
	default:
		return false
	}
}

// compareValues compares two values for equality
func compareValues(a, b interface{}) int {
	// Try numeric comparison first
	_, aIsNum := toNumeric(a)
	_, bIsNum := toNumeric(b)

	if aIsNum && bIsNum {
		return compareNumeric(a, b)
	}

	// Fallback to string comparison
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	if aStr < bStr {
		return -1
	} else if aStr > bStr {
		return 1
	}
	return 0
}

// compareNumeric compares two values as numbers
func compareNumeric(a, b interface{}) int {
	aFloat, _ := toNumeric(a)
	bFloat, _ := toNumeric(b)

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

// toNumeric converts a value to float64
func toNumeric(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	default:
		return 0, false
	}
}

// ExecuteEvents generates events when rule triggers
func (r *ExpressionRule) ExecuteEvents(messages []message.Message) ([]Event, error) {
	if !r.shouldTrigger || len(messages) == 0 {
		return []Event{}, nil
	}

	msg := messages[len(messages)-1]

	// Update last triggered time
	r.lastTriggered = time.Now()

	// Build event properties
	properties := map[string]interface{}{
		"rule_id":    r.id,
		"rule_name":  r.name,
		"message_id": msg.ID(),
		"triggered":  true,
	}

	// Include metadata if present
	for k, v := range r.metadata {
		properties[k] = v
	}

	event := gtypes.Event{
		Type:       gtypes.EventEntityUpdate,
		EntityID:   fmt.Sprintf("rule.%s.triggered", r.id),
		Properties: properties,
		Metadata: gtypes.EventMetadata{
			Source:    r.name,
			Timestamp: msg.Meta().CreatedAt(),
			Reason:    fmt.Sprintf("Rule %s triggered", r.name),
			RuleName:  r.name,
			Version:   "1.0.0",
		},
		Confidence: 1.0,
	}

	r.shouldTrigger = false
	return []Event{&event}, nil
}

// ExpressionRuleFactory creates expression-based rules
type ExpressionRuleFactory struct {
	ruleType string
}

// NewExpressionRuleFactory creates a new expression rule factory
func NewExpressionRuleFactory() *ExpressionRuleFactory {
	return &ExpressionRuleFactory{
		ruleType: "expression",
	}
}

// Type returns the factory type
func (f *ExpressionRuleFactory) Type() string {
	return f.ruleType
}

// Create creates an expression rule from definition
func (f *ExpressionRuleFactory) Create(_ string, def Definition, _ Dependencies) (Rule, error) {
	return NewExpressionRule(def)
}

// Validate validates the rule definition
func (f *ExpressionRuleFactory) Validate(def Definition) error {
	if def.ID == "" {
		return fmt.Errorf("rule ID is required")
	}
	if len(def.Conditions) == 0 {
		return fmt.Errorf("rule %s must have at least one condition", def.ID)
	}

	// Validate each condition
	for i, cond := range def.Conditions {
		if cond.Field == "" {
			return fmt.Errorf("rule %s condition[%d] missing field", def.ID, i)
		}
		if cond.Operator == "" {
			return fmt.Errorf("rule %s condition[%d] missing operator", def.ID, i)
		}
		if !isValidOperator(cond.Operator) {
			return fmt.Errorf("rule %s condition[%d] invalid operator: %s", def.ID, i, cond.Operator)
		}
		// Reject negate:true on the transition operator — three plausible
		// meanings of negation make rule semantics ambiguous. Rule authors
		// who want to invert transition behavior must restate the rule.
		if cond.Negate && cond.Operator == expression.OpTransition {
			return fmt.Errorf("rule %s condition[%d] negate:true is forbidden on operator %q (ambiguous semantics)",
				def.ID, i, expression.OpTransition)
		}
	}

	// Validate logic operator
	if def.Logic != "" && def.Logic != expression.LogicAnd && def.Logic != expression.LogicOr && def.Logic != expression.LogicNot {
		return fmt.Errorf("rule %s invalid logic operator: %s (must be 'and', 'or', or 'not')", def.ID, def.Logic)
	}

	// logic:"not" rule-level constraints: requires exactly 1 condition; cannot
	// combine with per-condition negate:true (double-negation anti-pattern).
	if def.Logic == expression.LogicNot {
		if len(def.Conditions) != 1 {
			return fmt.Errorf("rule %s logic=%q requires exactly 1 condition, got %d (use per-condition negate:true for compound negation)",
				def.ID, expression.LogicNot, len(def.Conditions))
		}
		if def.Conditions[0].Negate {
			return fmt.Errorf("rule %s logic=%q cannot combine with condition[0] negate:true (double-negation anti-pattern)",
				def.ID, expression.LogicNot)
		}
	}

	// Validate cooldown if specified
	if def.Cooldown != "" {
		if _, err := time.ParseDuration(def.Cooldown); err != nil {
			return fmt.Errorf("rule %s invalid cooldown: %w", def.ID, err)
		}
	}

	return nil
}

// Schema returns the expression rule schema
func (f *ExpressionRuleFactory) Schema() Schema {
	return Schema{
		Type:        "expression",
		DisplayName: "Expression Rule",
		Description: "Condition-based rule using field comparisons",
		Category:    "condition",
		Required:    []string{"id", "conditions"},
	}
}

// init registers the expression rule factory
func init() {
	factory := NewExpressionRuleFactory()
	if err := RegisterRuleFactory("expression", factory); err != nil {
		// Log but don't panic - allows tests to re-register
		fmt.Printf("Warning: Failed to register expression factory: %v\n", err)
	}
}
