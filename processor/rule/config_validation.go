package rule

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/rule/expression"
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

		// Reject negate:true on the transition operator — three plausible
		// meanings of "negated transition" (NOT-transitioned, NOT-current-matches,
		// NOT-prev-was-in-from) make the semantics ambiguous. Authors must
		// reformulate using explicit conditions.
		if negateVal, ok := condMap["negate"]; ok {
			if negate, ok := negateVal.(bool); ok && negate {
				if operator == expression.OpTransition {
					return errs.WrapInvalid(
						fmt.Errorf("rule %s condition[%d]: negate:true is forbidden on the transition operator (ambiguous semantics — reformulate with explicit conditions)", ruleID, i),
						"RuleProcessor", "validateExpressionRule", "check negate on transition")
				}
			}
		}

		// Validate count_in_window value config
		if operator == expression.OpCountInWindow {
			if err := rp.validateCountInWindowValue(ruleID, i, condMap["value"]); err != nil {
				return err
			}
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

		switch logic {
		case expression.LogicAnd, expression.LogicOr:
			// valid; no additional constraints

		case expression.LogicNot:
			// logic:"not" requires exactly one condition — De Morgan ambiguity
			// prevents defining clear semantics for multiple conditions.
			if len(conditionsSlice) != 1 {
				return errs.WrapInvalid(
					fmt.Errorf("rule %s logic=\"not\" requires exactly 1 condition, got %d (use per-condition negate:true for compound negation)", ruleID, len(conditionsSlice)),
					"RuleProcessor", "validateExpressionRule", "check logic not condition count")
			}
			// Reject combining logic:"not" with per-condition negate:true — double
			// negation (NOT (NOT X) = X) is an anti-pattern that confuses readers.
			for i, cond := range conditionsSlice {
				condMap, _ := cond.(map[string]any)
				if negateVal, ok := condMap["negate"]; ok {
					if negate, ok := negateVal.(bool); ok && negate {
						return errs.WrapInvalid(
							fmt.Errorf("rule %s logic=\"not\" cannot combine with condition[%d] negate:true (double-negation anti-pattern — remove one negation)", ruleID, i),
							"RuleProcessor", "validateExpressionRule", "check logic not double negate")
					}
				}
			}

		default:
			return errs.WrapInvalid(
				fmt.Errorf("rule %s logic must be 'and', 'or', or 'not', got: %s", ruleID, logic),
				"RuleProcessor", "validateExpressionRule", "check logic value")
		}
	}

	return nil
}

// validateCountInWindowValue validates the value config for a count_in_window condition:
//
//   - "window" must be a parseable positive duration string
//   - exactly one of gt/gte/lt/lte/eq/ne must be present and numeric
//   - max_dimensions (optional) must be ≤ DefaultMaxDistinctDimensionsPerRule
//   - max_per_window (optional) must be ≤ DefaultMaxCountsPerWindow
func (rp *Processor) validateCountInWindowValue(ruleID string, condIdx int, value interface{}) error {
	m, ok := value.(map[string]interface{})
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s condition[%d]: count_in_window value must be an object, got %T", ruleID, condIdx, value),
			"RuleProcessor", "validateCountInWindowValue", "check value type")
	}

	// Validate window duration
	windowRaw, ok := m["window"]
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s condition[%d]: count_in_window value missing required 'window' field", ruleID, condIdx),
			"RuleProcessor", "validateCountInWindowValue", "check window field")
	}
	windowStr, ok := windowRaw.(string)
	if !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s condition[%d]: count_in_window window must be a string, got %T", ruleID, condIdx, windowRaw),
			"RuleProcessor", "validateCountInWindowValue", "check window type")
	}
	window, err := time.ParseDuration(windowStr)
	if err != nil || window <= 0 {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s condition[%d]: count_in_window window %q is not a valid positive duration", ruleID, condIdx, windowStr),
			"RuleProcessor", "validateCountInWindowValue", "parse window duration")
	}

	// Validate that exactly one comparison operator is present
	comparators := []string{"gt", "gte", "lt", "lte", "eq", "ne"}
	found := ""
	for _, cmp := range comparators {
		if raw, exists := m[cmp]; exists {
			if found != "" {
				return errs.WrapInvalid(
					fmt.Errorf("rule %s condition[%d]: count_in_window has conflicting comparators %q and %q — use exactly one", ruleID, condIdx, found, cmp),
					"RuleProcessor", "validateCountInWindowValue", "check comparators")
			}
			if _, isNum := toFloat64Validation(raw); !isNum {
				return errs.WrapInvalid(
					fmt.Errorf("rule %s condition[%d]: count_in_window %s threshold must be numeric, got %T", ruleID, condIdx, cmp, raw),
					"RuleProcessor", "validateCountInWindowValue", "check threshold type")
			}
			found = cmp
		}
	}
	if found == "" {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s condition[%d]: count_in_window value missing comparison operator (one of: gt, gte, lt, lte, eq, ne)", ruleID, condIdx),
			"RuleProcessor", "validateCountInWindowValue", "check comparator present")
	}

	// Validate optional cardinality caps — may only LOWER the framework default
	if raw, ok := m["max_dimensions"]; ok {
		v, isNum := toFloat64Validation(raw)
		if !isNum {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d]: count_in_window max_dimensions must be numeric, got %T", ruleID, condIdx, raw),
				"RuleProcessor", "validateCountInWindowValue", "check max_dimensions type")
		}
		if int(v) > DefaultMaxDistinctDimensionsPerRule {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d]: count_in_window max_dimensions %d exceeds framework cap %d — rule-level overrides may only lower the cap", ruleID, condIdx, int(v), DefaultMaxDistinctDimensionsPerRule),
				"RuleProcessor", "validateCountInWindowValue", "check max_dimensions cap")
		}
	}
	if raw, ok := m["max_per_window"]; ok {
		v, isNum := toFloat64Validation(raw)
		if !isNum {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d]: count_in_window max_per_window must be numeric, got %T", ruleID, condIdx, raw),
				"RuleProcessor", "validateCountInWindowValue", "check max_per_window type")
		}
		if int(v) > DefaultMaxCountsPerWindow {
			return errs.WrapInvalid(
				fmt.Errorf("rule %s condition[%d]: count_in_window max_per_window %d exceeds framework cap %d — rule-level overrides may only lower the cap", ruleID, condIdx, int(v), DefaultMaxCountsPerWindow),
				"RuleProcessor", "validateCountInWindowValue", "check max_per_window cap")
		}
	}

	return nil
}

// toFloat64Validation converts numeric JSON types to float64. Same logic as
// expression.toFloat64 but scoped to the validation package to avoid a
// cross-package call (expression pkg is a sub-package with no upward deps).
func toFloat64Validation(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// validateActionFields performs action-type-specific field validation. Currently
// enforces that sweep_windows actions carry no content fields — those fields
// have no meaning and their presence almost always indicates a copy-paste error
// from another action type.
//
// Called from validateCronDefinition and can be extended to other action types
// that have "empty fields required" constraints.
func validateActionFields(ruleID string, actionIdx int, a Action) error {
	if a.Type != ActionTypeSweepWindows {
		return nil
	}
	// sweep_windows carries no fields. Validate the most likely copy-paste
	// accidents — the full Action struct field list.
	type fieldCheck struct {
		name  string
		empty bool
	}
	checks := []fieldCheck{
		{"subject", a.Subject == ""},
		{"predicate", a.Predicate == ""},
		{"object", a.Object == ""},
		{"ttl", a.TTL == ""},
		{"role", a.Role == ""},
		{"model", a.Model == ""},
		{"prompt", a.Prompt == ""},
		{"workflow_id", a.WorkflowID == ""},
		{"bucket", a.Bucket == ""},
		{"key", a.Key == ""},
		{"reason", a.Reason == ""},
		{"boid_signal_type", a.BoidSignalType == ""},
	}
	var extra []string
	for _, c := range checks {
		if !c.empty {
			extra = append(extra, c.name)
		}
	}
	if len(a.Properties) > 0 {
		extra = append(extra, "properties")
	}
	if len(a.Tools) > 0 {
		extra = append(extra, "tools")
	}
	if len(a.ContextData) > 0 {
		extra = append(extra, "context_data")
	}
	if len(a.Payload) > 0 {
		extra = append(extra, "payload")
	}
	if len(a.When) > 0 {
		extra = append(extra, "when")
	}
	if a.BoidStrength != 0 {
		extra = append(extra, "boid_strength")
	}
	if a.Merge {
		extra = append(extra, "merge")
	}
	if len(extra) > 0 {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s action[%d] (sweep_windows): extraneous fields not allowed: %s "+
				"(sweep_windows takes no fields — remove them or use a different action type)",
				ruleID, actionIdx, strings.Join(extra, ", ")),
			"RuleProcessor", "validateActionFields", "check sweep_windows fields")
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
func ValidateDefinition(def Definition) error {
	if def.FireEveryNEvents < 0 {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s fire_every_n_events must be >= 0, got %d", def.ID, def.FireEveryNEvents),
			"RuleProcessor", "ValidateDefinition", "validate fire_every_n_events")
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
		Enabled: getEnabledMode(ruleMap, "enabled", ModeEnabled),
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
			Negate:   getBoolWithDefault(condMap, "negate", false),
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

// getEnabledMode reads the "enabled" field from a map[string]any, accepting
// both the legacy bool shape (true → "enabled", false → "disabled") and the
// new string shape ("enabled"/"disabled"/"shadow"). Falls back to defaultVal
// when the key is absent or its type is unrecognised.
//
// This replaces getBoolWithDefault for the enabled field specifically so that
// hot-reload's KV-to-Definition path handles all three states without losing
// the shadow mode during a reconcile.
func getEnabledMode(m map[string]any, key string, defaultVal Mode) Mode {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		if val {
			return ModeEnabled
		}
		return ModeDisabled
	case string:
		switch Mode(val) {
		case ModeEnabled, ModeDisabled, ModeShadow:
			return Mode(val)
		}
		switch val {
		case "true":
			return ModeEnabled
		case "false":
			return ModeDisabled
		}
		return defaultVal
	default:
		return defaultVal
	}
}
