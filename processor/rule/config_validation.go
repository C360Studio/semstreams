package rule

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/c360studio/semstreams/vocabulary"
)

// ValidateConfigUpdate validates proposed configuration changes
func (rp *Processor) ValidateConfigUpdate(changes map[string]any) error {
	// Validate rule configurations if present
	if rulesConfig, ok := changes["rules"]; ok {
		rulesMap, ok := rulesConfig.(map[string]any)
		if !ok {
			return errs.WrapInvalid(
				fmt.Errorf("rules configuration must be an object, got %T", rulesConfig),
				"RuleProcessor", "ValidateConfigUpdate", "validate rules type")
		}

		// Validate each rule definition
		for ruleID, ruleConfig := range rulesMap {
			if err := rp.validateSingleRuleConfig(ruleID, ruleConfig); err != nil {
				return errs.Wrap(err, "RuleProcessor", "ValidateConfigUpdate",
					fmt.Sprintf("validate rule %s", ruleID))
			}
		}
	}

	// Validate entity_watch_patterns if present
	if patternsVal, ok := changes["entity_watch_patterns"]; ok {
		if _, ok := patternsVal.([]string); !ok {
			// Try to convert from []any to []string
			if anySlice, ok := patternsVal.([]any); ok {
				for i, v := range anySlice {
					if _, ok := v.(string); !ok {
						return errs.WrapInvalid(
							fmt.Errorf("entity_watch_patterns[%d] must be string, got %T", i, v),
							"RuleProcessor", "ValidateConfigUpdate", "validate patterns type")
					}
				}
			} else {
				return errs.WrapInvalid(
					fmt.Errorf("entity_watch_patterns must be array of strings, got %T", patternsVal),
					"RuleProcessor", "ValidateConfigUpdate", "validate patterns type")
			}
		}
	}

	// Validate enable_graph_integration if present
	if integrationVal, ok := changes["enable_graph_integration"]; ok {
		if _, ok := integrationVal.(bool); !ok {
			return errs.WrapInvalid(
				fmt.Errorf("enable_graph_integration must be boolean, got %T", integrationVal),
				"RuleProcessor", "ValidateConfigUpdate", "validate integration type")
		}
	}

	return nil
}

// validateSingleRuleConfig validates a single rule configuration
func (rp *Processor) validateSingleRuleConfig(ruleID string, ruleConfig any) error {
	// Convert to map
	ruleMap, ok := ruleConfig.(map[string]any)
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s configuration must be an object, got %T", ruleID, ruleConfig),
			"RuleProcessor", "validateSingleRuleConfig", "check config type")
	}

	// Validate required fields
	ruleType, ok := ruleMap["type"]
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s missing required field 'type'", ruleID),
			"RuleProcessor", "validateSingleRuleConfig", "check type field")
	}

	ruleTypeStr, ok := ruleType.(string)
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s type must be string, got %T", ruleID, ruleType),
			"RuleProcessor", "validateSingleRuleConfig", "check type value")
	}

	// Validate rule type is supported (check if factory exists)
	if !rp.isKnownRuleType(ruleTypeStr) {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s has unknown type: %s (no factory registered)", ruleID, ruleTypeStr),
			"RuleProcessor", "validateSingleRuleConfig", "check rule type")
	}

	// Cron rules carry a different shape — schedule + actions, no
	// conditions, no logic. Build the Definition and round-trip it through
	// NewCronRule so the same validation surface (forbidden fields,
	// schedule parse, cooldown parse, action type non-empty) catches both
	// file-loaded and hot-reloaded rules.
	if ruleTypeStr == CronRuleType {
		def, err := definitionFromMap(ruleID, ruleMap)
		if err != nil {
			return errs.Wrap(err, "RuleProcessor", "validateSingleRuleConfig",
				fmt.Sprintf("parse cron rule %s", ruleID))
		}
		if _, err := NewCronRule(def); err != nil {
			return errs.WrapInvalid(err, "RuleProcessor", "validateSingleRuleConfig",
				fmt.Sprintf("validate cron rule %s", ruleID))
		}
		return nil
	}

	// Validate expression-based rules if conditions are present
	if _, hasConditions := ruleMap["conditions"]; hasConditions {
		if err := rp.validateExpressionRule(ruleID, ruleMap); err != nil {
			return err
		}
	}

	return nil
}

// validateExpressionRule validates expression-based rule configuration
func (rp *Processor) validateExpressionRule(ruleID string, ruleMap map[string]any) error {
	// Check for conditions
	conditionsVal, ok := ruleMap["conditions"]
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s missing required 'conditions' field", ruleID),
			"RuleProcessor", "validateExpressionRule", "check conditions field")
	}

	conditionsSlice, ok := conditionsVal.([]any)
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s conditions must be array, got %T", ruleID, conditionsVal),
			"RuleProcessor", "validateExpressionRule", "check conditions type")
	}

	if len(conditionsSlice) == 0 {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s must have at least one condition", ruleID),
			"RuleProcessor", "validateExpressionRule", "check conditions count")
	}

	// Validate each condition
	for i, condVal := range conditionsSlice {
		condMap, ok := condVal.(map[string]any)
		if !ok {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d] must be object, got %T", ruleID, i, condVal),
				"RuleProcessor", "validateExpressionRule", "check condition type")
		}

		// Check required condition fields
		for _, field := range []string{"field", "operator", "value"} {
			if _, ok := condMap[field]; !ok {
				return errs.WrapInvalid(
					fmt.Errorf("rule %s condition[%d] missing required field '%s'", ruleID, i, field),
					"RuleProcessor", "validateExpressionRule", "check condition field")
			}
		}

		// Validate operator
		operator, _ := condMap["operator"].(string)
		if !rp.isValidOperator(operator) {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d] has invalid operator: %s", ruleID, i, operator),
				"RuleProcessor", "validateExpressionRule", "check operator")
		}

		// Reject conditions that predicate on rule-opaque fields. Rule-opaque
		// predicates carry free-form agent-private content (ADR-036); rules
		// match structural facts only. Vocabulary marks the predicate
		// rule-opaque via RuleOpaque metadata; unregistered predicates pass
		// through (opacity is opt-in at registration time).
		field, _ := condMap["field"].(string)
		if field != "" && vocabulary.IsRuleOpaque(field) {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d] predicates on rule-opaque field %q; rule-opaque predicates carry agent-private content and cannot be matched in rule conditions (see ADR-036)", ruleID, i, field),
				"RuleProcessor", "validateExpressionRule", "check rule-opaque field")
		}
	}

	// Validate logic field if present
	if logicVal, ok := ruleMap["logic"]; ok {
		logic, ok := logicVal.(string)
		if !ok {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s logic must be string, got %T", ruleID, logicVal),
				"RuleProcessor", "validateExpressionRule", "check logic type")
		}
		if logic != "and" && logic != "or" {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s logic must be 'and' or 'or', got: %s", ruleID, logic),
				"RuleProcessor", "validateExpressionRule", "check logic value")
		}
	}

	return nil
}

// isKnownRuleType checks if a rule type is supported. Cron is a built-in
// type that does not go through the factory registry (CronRule does not
// implement the Rule interface), so it is recognised here directly.
func (rp *Processor) isKnownRuleType(ruleType string) bool {
	if ruleType == CronRuleType {
		return true
	}
	_, exists := GetRuleFactory(ruleType)
	return exists
}

// isValidOperator checks if an operator is valid
func (rp *Processor) isValidOperator(operator string) bool {
	return isValidOperator(operator)
}

// ValidateDefinition validates a rule Definition for fields that are
// processor-level concerns (not delegated to individual rule factories).
// Call this before passing a Definition to CreateRuleFromDefinition.
//
// Both load paths converge here:
//   - file-load → definitionFromMap → ValidateDefinition (this function)
//   - hot-reload via KV → ValidateConfigUpdate → validateExpressionRule
//
// Discipline checks that apply to both paths (e.g. ADR-036's
// rule-opaque field rejection) live here so a startup-time on-disk
// rule cannot bypass what a hot-reload would catch.
func ValidateDefinition(def Definition) error {
	if def.FireEveryNEvents < 0 {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s fire_every_n_events must be >= 0, got %d", def.ID, def.FireEveryNEvents),
			"RuleProcessor", "ValidateDefinition", "validate fire_every_n_events")
	}
	if err := validateConditionFields(def); err != nil {
		return err
	}
	return validateActionLists(def)
}

// validateConditionFields enforces ADR-036 Rule 1 on a parsed
// Definition: no condition.field may name a vocabulary predicate
// flagged RuleOpaque. Mirrors the check in validateExpressionRule's
// hot-reload path so file-loaded rules see the same gate.
func validateConditionFields(def Definition) error {
	for i, c := range def.Conditions {
		if c.Field == "" {
			continue
		}
		if vocabulary.IsRuleOpaque(c.Field) {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d] predicates on rule-opaque field %q; rule-opaque predicates carry agent-private content and cannot be matched in rule conditions (see ADR-036)", def.ID, i, c.Field),
				"RuleProcessor", "ValidateDefinition", "check rule-opaque field")
		}
	}
	return nil
}

// validateActionLists walks every action slice on the Definition
// (OnEnter, OnExit, WhileTrue, OnRecovery) and checks per-action
// invariants. Currently enforces:
//
//   - Action.MaxIterations, when set, must be non-negative. Sentinel
//     0 is valid (operator's explicit unlimited opt-out); negative
//     values are config errors.
func validateActionLists(def Definition) error {
	check := func(label string, actions []Action) error {
		for i, a := range actions {
			if a.MaxIterations != nil && *a.MaxIterations < 0 {
				return errs.WrapInvalid(
					fmt.Errorf("rule %s %s[%d] max_iterations must be >= 0 (0 = unlimited), got %d",
						def.ID, label, i, *a.MaxIterations),
					"RuleProcessor", "ValidateDefinition", "validate action max_iterations")
			}
		}
		return nil
	}
	// Walk every list of Actions on the Definition. Note `def.Actions`
	// is the cron-rule action list (cron_rule.go uses Definition.Actions
	// rather than the OnEnter/OnExit/WhileTrue/OnRecovery transition
	// shape). Validation MUST cover it too, even though the runtime cap
	// gate doesn't yet apply to cron-fired actions — see migration
	// guide for the deferred-runtime caveat.
	for label, actions := range map[string][]Action{
		"on_enter":    def.OnEnter,
		"on_exit":     def.OnExit,
		"while_true":  def.WhileTrue,
		"on_recovery": def.OnRecovery,
		"actions":     def.Actions,
	} {
		if err := check(label, actions); err != nil {
			return err
		}
	}
	return nil
}

// definitionFromMap converts a ruleMap (the hot-reload wire format) into a
// Definition struct for a given ruleID. It mirrors the field extraction that
// loadRules performs when reading from JSON files so that hot-reloaded rules
// carry the same gate and stateful-action metadata as file-loaded rules.
func definitionFromMap(ruleID string, ruleMap map[string]any) (Definition, error) {
	// Defensive type assertion on Type: validateSingleRuleConfig gates
	// on a string Type before reaching here, but the watcher path can
	// also reach definitionFromMap from places that pre-date that
	// validator. Failing with a typed error rather than a panic keeps
	// the reconcile goroutine alive.
	typeStr, ok := ruleMap["type"].(string)
	if !ok {
		return Definition{}, fmt.Errorf("rule %s: type must be a string, got %T", ruleID, ruleMap["type"])
	}
	def := Definition{
		ID:      ruleID,
		Type:    typeStr,
		Name:    getStringWithDefault(ruleMap, "name", ruleID),
		Enabled: getBoolWithDefault(ruleMap, "enabled", true),
		Logic:   getStringWithDefault(ruleMap, "logic", "and"),
	}

	// Parse description if present. Defensive type assertion: a
	// malformed KV record with a non-string description must not
	// panic the watcher's reconcile goroutine.
	def.Description = getStringWithDefault(ruleMap, "description", "")

	conds, err := parseConditionsFromMap(ruleID, ruleMap)
	if err != nil {
		return Definition{}, err
	}
	def.Conditions = conds

	// Parse entity configuration if present. Defensive type assertions
	// throughout — a malformed KV record must not panic the watcher's
	// reconcile goroutine.
	if entityVal, ok := ruleMap["entity"]; ok {
		if entityMap, ok := entityVal.(map[string]any); ok {
			def.Entity.Pattern = getStringWithDefault(entityMap, "pattern", "")
			if buckets, ok := entityMap["watch_buckets"].([]any); ok {
				def.Entity.WatchBuckets = make([]string, 0, len(buckets))
				for i, b := range buckets {
					s, ok := b.(string)
					if !ok {
						return Definition{}, fmt.Errorf("rule %s: entity.watch_buckets[%d] must be string, got %T", ruleID, i, b)
					}
					def.Entity.WatchBuckets = append(def.Entity.WatchBuckets, s)
				}
			}
		}
	}

	// Parse fire_every_n_events if present
	if n, ok := ruleMap["fire_every_n_events"]; ok {
		switch v := n.(type) {
		case float64:
			def.FireEveryNEvents = int(v)
		case int:
			def.FireEveryNEvents = v
		case int64:
			def.FireEveryNEvents = int(v)
		}
	}

	// Parse stateful action lists (OnEnter, OnExit, WhileTrue, OnRecovery).
	// These are stored as []Action in the Definition and used by the stateful
	// evaluator. Without this round-trip, hot-reloaded rules have empty slices
	// and hasStatefulActions evaluates false in message_handler.go.
	parseActions := func(key string) []Action {
		val, ok := ruleMap[key]
		if !ok {
			return nil
		}
		// Re-marshal and unmarshal via JSON to reuse Action's existing JSON tags.
		raw, err := json.Marshal(val)
		if err != nil {
			return nil
		}
		var actions []Action
		if err := json.Unmarshal(raw, &actions); err != nil {
			return nil
		}
		return actions
	}
	def.OnEnter = parseActions("on_enter")
	def.OnExit = parseActions("on_exit")
	def.WhileTrue = parseActions("while_true")
	def.OnRecovery = parseActions("on_recovery")

	// Cron-specific fields. These are populated regardless of Type so the
	// round-trip (Definition → ruleMap → Definition) is lossless and so
	// validation in NewCronRule has the full surface available. NewCronRule
	// rejects them when Type != "cron" via its forbidden-field check.
	def.Actions = parseActions("actions")
	def.Schedule = getStringWithDefault(ruleMap, "schedule", "")
	def.Cooldown = getStringWithDefault(ruleMap, "cooldown", "")

	// Parse metadata if present — provenance carried through to action
	// substitution (e.g., om_entry_id from semteams onboarding).
	if metaVal, ok := ruleMap["metadata"]; ok {
		if metaMap, ok := metaVal.(map[string]any); ok {
			def.Metadata = metaMap
		}
	}

	// Parse RerunOnRecovery flag if present
	def.RerunOnRecovery = getBoolWithDefault(ruleMap, "rerun_on_recovery", false)

	// Validate processor-level fields before returning
	if err := ValidateDefinition(def); err != nil {
		return Definition{}, err
	}

	return def, nil
}

// createRuleFromConfig creates a rule instance from configuration.
// It builds a Definition via definitionFromMap and delegates to the factory.
func (rp *Processor) createRuleFromConfig(ruleID string, ruleMap map[string]any) (Rule, Definition, error) {
	def, err := definitionFromMap(ruleID, ruleMap)
	if err != nil {
		return nil, Definition{}, err
	}

	// Create dependencies
	deps := Dependencies{
		NATSClient: rp.natsClient,
		Logger:     rp.logger,
	}

	// Use factory to create rule
	r, err := CreateRuleFromDefinition(def, deps)
	if err != nil {
		return nil, Definition{}, err
	}
	return r, def, nil
}

// parseConditionsFromMap extracts the conditions array from a ruleMap
// (the hot-reload wire format). The map-key check is not enough — a
// rule with no conditions (e.g. cron rules) JSON-marshals as
// `"conditions": null` because Definition.Conditions has no `omitempty`
// tag. Unmarshaling into map[string]any then yields a nil-but-present
// entry; type-asserting nil to []any panics. Guarded explicitly so
// cron rules round-trip cleanly through the hot-reload KV bucket.
func parseConditionsFromMap(ruleID string, ruleMap map[string]any) ([]expression.ConditionExpression, error) {
	conditionsVal, ok := ruleMap["conditions"]
	if !ok || conditionsVal == nil {
		return nil, nil
	}
	conditionsSlice, ok := conditionsVal.([]any)
	if !ok {
		return nil, fmt.Errorf("rule %s: conditions must be an array, got %T", ruleID, conditionsVal)
	}
	conds := make([]expression.ConditionExpression, len(conditionsSlice))
	for i, condVal := range conditionsSlice {
		condMap, ok := condVal.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rule %s: condition[%d] must be an object", ruleID, i)
		}
		cond := expression.ConditionExpression{
			Field:    getStringWithDefault(condMap, "field", ""),
			Operator: getStringWithDefault(condMap, "operator", ""),
			Value:    condMap["value"],
			Required: getBoolWithDefault(condMap, "required", true),
		}
		if from, ok := condMap["from"]; ok {
			cond.From = from
		}
		conds[i] = cond
	}
	return conds, nil
}

// getStringWithDefault safely gets a string value with default
func getStringWithDefault(m map[string]any, key, defaultVal string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getBoolWithDefault safely gets a bool value with default
func getBoolWithDefault(m map[string]any, key string, defaultVal bool) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// convertToStringSlice safely converts various slice types to []string
func (rp *Processor) convertToStringSlice(val any) []string {
	if stringSlice, ok := val.([]string); ok {
		return stringSlice
	}

	if anySlice, ok := val.([]any); ok {
		result := make([]string, len(anySlice))
		for i, v := range anySlice {
			if s, ok := v.(string); ok {
				result[i] = s
			}
		}
		return result
	}

	return []string{}
}
