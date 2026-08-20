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
	expr.Conditions = substituteConditionValues(nil, expr.Conditions, ec)
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
func (r *ExpressionRule) EvaluateEntityState(ctx context.Context, entityState *gtypes.EntityState) bool {
	if !r.enabled || entityState == nil {
		return false
	}

	// Check cooldown
	if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {
		return false
	}

	result, err := evaluateConditionsAgainstEntity(
		ctx, r.evaluator, r.conditions, r.logic, entityState, r.lifecycleManager)
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

// evaluateConditionsAgainstEntity is the pre-processing between a rule's declared
// conditions and the evaluator. It is shared by the stateful path
// (ExpressionRule.EvaluateEntityState) and the stateless one (Matches) so the two
// cannot drift — which is the whole point of gh#731. A consumer that rebuilds this
// sequence loses a step and gets a confident wrong answer; so would a second copy
// of it living in this package.
//
// Three steps, each of which a bare-evaluator caller silently loses:
//
//  1. An empty condition list does NOT match. The evaluator disagrees —
//     EvaluateWithStateAndMessage treats an empty list as passing — and the rule
//     engine's answer is the production one, so it is the answer here.
//  2. $-prefixed condition VALUES are substituted against the entity. Without
//     this, `value: "$entity.triple.foo.length"` reaches the operator as literal
//     template text and coerce-errors (#149, found during reference-pack
//     integration testing).
//  3. $entity.lifecycle.* fields are pre-resolved into a stateFields map and
//     dispatched through EvaluateWithStateAndMessage so the evaluator's prefix
//     check picks them up (ADR-047). No-op on a nil lookup or an entity that is
//     not lifecycle-managed.
//
// Cooldown is deliberately NOT here: it is a property of a rule instance's
// history, not of its conditions, and only the stateful caller has one.
func evaluateConditionsAgainstEntity(
	ctx context.Context,
	evaluator *expression.Evaluator,
	conditions []expression.ConditionExpression,
	logic string,
	entityState *gtypes.EntityState,
	lifecycleLookup LifecycleManager,
) (bool, error) {
	if len(conditions) == 0 {
		return false, nil
	}
	// Tolerant disposition: a lookup failure leaves stateFields empty and the
	// evaluator answers false for an optional lifecycle field. Correct for
	// firing; Matches takes the strict disposition over the same steps.
	stateFields := expression.StateFields{}
	_ = populateLifecycleStateFields(ctx, lifecycleLookup, entityState.ID, stateFields)
	expr := substituteConditionsForEntity(ctx, conditions, logic, entityState, lifecycleLookup)
	return dispatchEvaluation(evaluator, entityState, stateFields, expr)
}

// substituteConditionsForEntity resolves `$`-templated condition VALUES against the
// entity and returns the expression ready for the evaluator.
//
// Split out so the stateless path can INSPECT the substituted conditions before
// evaluating them — an unresolved template that survives substitution is a
// "could not resolve", and non-erroring operators like eq/contains would otherwise
// compare the literal token as an ordinary string and return a confident verdict.
// Both paths run this exact function, so the substitution itself cannot diverge.
func substituteConditionsForEntity(
	ctx context.Context,
	conditions []expression.ConditionExpression,
	logic string,
	entityState *gtypes.EntityState,
	lifecycleLookup LifecycleManager,
) expression.LogicalExpression {
	ec := &ExecutionContext{
		EntityID:  entityState.ID,
		Entity:    entityState,
		Lifecycle: lifecycleLookup,
	}
	return expression.LogicalExpression{
		Conditions: SubstituteConditionValues(ctx, conditions, ec),
		Logic:      logic,
	}
}

// dispatchEvaluation picks the evaluator entry point based on whether any state
// fields resolved. Shared by both paths so the dispatch rule cannot diverge.
func dispatchEvaluation(
	evaluator *expression.Evaluator,
	entityState *gtypes.EntityState,
	stateFields expression.StateFields,
	expr expression.LogicalExpression,
) (bool, error) {
	if len(stateFields) > 0 {
		return evaluator.EvaluateWithStateAndMessage(entityState, stateFields, nil, expr)
	}
	return evaluator.Evaluate(entityState, expr)
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
