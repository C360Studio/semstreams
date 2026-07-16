// Package rule - Expression Rule Factory for condition-based rules
package rule

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ExpressionRule implements Rule interface for expression-based condition evaluation
type ExpressionRule struct {
	id            string
	packID        string
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

	// lifecycleManager is the optional pkg/lifecycle.Manager wired by
	// the rule Processor. When non-nil, EvaluateEntityState
	// pre-resolves the trigger entity's lifecycle state so condition
	// fields like `$entity.lifecycle.phase` evaluate at initial-match
	// time. ADR-047.
	lifecycleManager LifecycleManager
}

// SetLifecycleManager installs the Lifecycle harness Manager used by
// initial-state condition.Field resolution for `$entity.lifecycle.*`
// paths. nil arg disables resolution (tokens then surface as missing-
// field errors at evaluation — loud-fail).
func (r *ExpressionRule) SetLifecycleManager(m LifecycleManager) {
	r.lifecycleManager = m
}

// NewExpressionRule creates a new expression-based rule under an explicit
// stable pack identity.
func NewExpressionRule(packID string, def Definition) (*ExpressionRule, error) {
	if err := validatePackID(packID); err != nil {
		return nil, err
	}
	if err := validateDefinitionEntityPattern(def); err != nil {
		return nil, err
	}
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
		logic = "and"
	}

	// Entity.Pattern belongs exclusively to ENTITY_STATES selection. Entity-
	// scoped rules do not participate in the message path; message rules without
	// an entity declaration retain the catch-all subject filter.
	subjects := []string{">"}
	if def.Entity.Pattern != "" {
		subjects = nil
	}

	return &ExpressionRule{
		id:          def.ID,
		packID:      packID,
		name:        def.Name,
		description: def.Description,
		subscribed:  subjects,
		enabled:     def.Enabled,
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

// Evaluate evaluates the rule against messages.
//
// Routes through the unified expression.Evaluator with `messageFields`
// populated from the payload's GenericJSONPayload data. Bare field names
// resolve via the same precedence rule documented on
// `Evaluator.EvaluateWithStateAndMessage`. ADR-041 unified this with the
// action-When path so rule-level conditions and action guards share the
// same field-resolution semantics — no more two-evaluator split.
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

	// Get payload data — only GenericJSONPayload is supported for
	// expression matching because conditions are field-keyed maps.
	payload := msg.Payload()
	var data map[string]any
	if genericPayload, ok := payload.(*message.GenericJSONPayload); ok {
		data = genericPayload.Data
	}

	if len(data) == 0 {
		return false
	}

	if len(r.conditions) == 0 {
		return false
	}

	// #149 / #150 reviewer-rec-1: substitute $-prefixed string Values
	// against the message payload before evaluation. Without this, a
	// condition `value: "$message.expected_count"` reaches the
	// operator as the literal template and coerce-errors. Wired here
	// for symmetry with EvaluateEntityState; the message-path didn't
	// have a production caller using substituted values today, but
	// the asymmetry was exactly the "same shape behaves differently
	// depending on context" class the structural fixes claim to
	// close. MessageData carries the payload; Entity is nil on this
	// path so $entity.triple.* substitutions resolve to leftovers
	// (loud Warn via the unresolved-template path).
	ec := &ExecutionContext{MessageData: data}
	expr := r.buildLogicalExpression()
	expr.Conditions = SubstituteConditionValues(expr.Conditions, ec)
	result, err := r.evaluator.EvaluateWithStateAndMessage(nil, nil, expression.MessageFields(data), expr)
	if err != nil {
		slog.Debug("ExpressionRule: message-path evaluation error",
			"rule", r.name,
			"error", err)
		return false
	}
	r.shouldTrigger = result
	return result
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

	// Build LogicalExpression from rule conditions. Substitute
	// $-prefixed string Values against the entity before evaluation
	// — without this, a condition `value: "$entity.triple.foo.length"`
	// reaches the operator as the literal template and coerce-errors.
	// See SubstituteConditionValues for the contract; #149 surfaced
	// the gap during reference-pack integration testing.
	ec := &ExecutionContext{
		EntityID:  entityState.ID,
		Entity:    entityState,
		Lifecycle: r.lifecycleManager,
	}
	expr := r.buildLogicalExpression()
	expr.Conditions = SubstituteConditionValues(expr.Conditions, ec)

	// ADR-047: pre-resolve $entity.lifecycle.* condition fields into
	// a stateFields map and dispatch via EvaluateWithStateAndMessage
	// so the evaluator's broadened prefix check picks them up.
	// No-op when no Manager is wired or the entity isn't
	// lifecycle-managed.
	stateFields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), r.lifecycleManager, entityState.ID, stateFields)
	var result bool
	var err error
	if len(stateFields) > 0 {
		result, err = r.evaluator.EvaluateWithStateAndMessage(entityState, stateFields, nil, expr)
	} else {
		result, err = r.evaluator.Evaluate(entityState, expr)
	}
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

// ExecuteEvents generates events when rule triggers
func (r *ExpressionRule) ExecuteEvents(messages []message.Message) ([]Event, error) {
	if !r.shouldTrigger || len(messages) == 0 {
		return []Event{}, nil
	}

	msg := messages[len(messages)-1]

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

	entityID, err := ruleTriggerEntityID(r.packID, r.id)
	if err != nil {
		return nil, err
	}
	event, err := gtypes.NewEntityUpdateEvent(entityID, properties, gtypes.EventMetadata{
		Source:    r.name,
		Timestamp: msg.Meta().CreatedAt(),
		Reason:    fmt.Sprintf("Rule %s triggered", r.name),
		RuleName:  r.name,
	})
	if err != nil {
		return nil, err
	}

	r.lastTriggered = time.Now()
	r.shouldTrigger = false
	return []Event{event}, nil
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
func (f *ExpressionRuleFactory) Create(_ string, def Definition, deps Dependencies) (Rule, error) {
	rule, err := NewExpressionRule(deps.PackID, def)
	if err != nil {
		return nil, err
	}
	return rule, nil
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
	}

	// Validate logic operator
	if def.Logic != "" && def.Logic != "and" && def.Logic != "or" {
		return fmt.Errorf("rule %s invalid logic operator: %s (must be 'and' or 'or')", def.ID, def.Logic)
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
