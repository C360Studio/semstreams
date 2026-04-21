package rule

import (
	"fmt"

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

// isKnownRuleType checks if a rule type is supported
func (rp *Processor) isKnownRuleType(ruleType string) bool {
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

// createRuleFromConfig creates a rule instance from configuration
func (rp *Processor) createRuleFromConfig(ruleID string, ruleMap map[string]any) (Rule, error) {
	// Convert map to Definition
	def := Definition{
		ID:      ruleID,
		Type:    ruleMap["type"].(string),
		Name:    getStringWithDefault(ruleMap, "name", ruleID),
		Enabled: getBoolWithDefault(ruleMap, "enabled", true),
		Logic:   getStringWithDefault(ruleMap, "logic", "and"),
	}

	// Parse description if present
	if desc, ok := ruleMap["description"]; ok {
		def.Description = desc.(string)
	}

	// Parse conditions
	if conditionsVal, ok := ruleMap["conditions"]; ok {
		conditionsSlice := conditionsVal.([]any)
		def.Conditions = make([]expression.ConditionExpression, len(conditionsSlice))
		for i, condVal := range conditionsSlice {
			condMap := condVal.(map[string]any)
			cond := expression.ConditionExpression{
				Field:    condMap["field"].(string),
				Operator: condMap["operator"].(string),
				Value:    condMap["value"],
				Required: getBoolWithDefault(condMap, "required", true),
			}
			if from, ok := condMap["from"]; ok {
				cond.From = from
			}
			def.Conditions[i] = cond
		}
	}

	// Parse entity configuration if present
	if entityVal, ok := ruleMap["entity"]; ok {
		if entityMap, ok := entityVal.(map[string]any); ok {
			def.Entity.Pattern = getStringWithDefault(entityMap, "pattern", "")
			if buckets, ok := entityMap["watch_buckets"].([]any); ok {
				def.Entity.WatchBuckets = make([]string, len(buckets))
				for i, b := range buckets {
					def.Entity.WatchBuckets[i] = b.(string)
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

	// Validate processor-level fields before handing off to factory
	if err := ValidateDefinition(def); err != nil {
		return nil, err
	}

	// Create dependencies
	deps := Dependencies{
		NATSClient: rp.natsClient,
		Logger:     rp.logger,
	}

	// Use factory to create rule
	return CreateRuleFromDefinition(def, deps)
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
