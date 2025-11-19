package rule

import (
	"fmt"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/processor/rule/expression"
)

// ApplyConfigUpdate applies validated configuration changes
func (rp *Processor) ApplyConfigUpdate(changes map[string]any) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Apply rule configuration changes
	if rulesConfig, ok := changes["rules"]; ok {
		rulesMap := rulesConfig.(map[string]any) // Validated in ValidateConfigUpdate
		if err := rp.applyRuleChanges(rulesMap); err != nil {
			return errors.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "apply rule changes")
		}
	}

	// Apply enabled_rules changes
	if enabledRulesVal, ok := changes["enabled_rules"]; ok {
		enabledRules := rp.convertToStringSlice(enabledRulesVal)
		rp.config.EnabledRules = enabledRules

		// Reload rules with new enabled list
		if err := rp.loadRules(); err != nil {
			return errors.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "reload enabled rules")
		}

		rp.logger.Info("Updated enabled rules", "rules", enabledRules)
	}

	// Apply entity_watch_patterns changes
	if patternsVal, ok := changes["entity_watch_patterns"]; ok {
		patterns := rp.convertToStringSlice(patternsVal)
		rp.config.EntityWatchPatterns = patterns

		// Note: Changing watch patterns requires restart for now
		// TODO: Implement dynamic watcher management
		rp.logger.Info("Updated entity watch patterns (restart required)", "patterns", patterns)
	}

	// Apply enable_graph_integration changes
	if integrationVal, ok := changes["enable_graph_integration"]; ok {
		integration := integrationVal.(bool) // Validated in ValidateConfigUpdate
		rp.config.EnableGraphIntegration = integration
		rp.logger.Info("Updated graph integration setting", "enabled", integration)
	}

	return nil
}

// applyRuleChanges applies dynamic rule configuration changes
func (rp *Processor) applyRuleChanges(rulesMap map[string]any) error {
	// Track rules to remove (existing rules not in new config)
	currentRuleIDs := make(map[string]bool)
	for ruleID := range rp.rules {
		currentRuleIDs[ruleID] = true
	}

	// Process rule updates/additions
	for ruleID, ruleConfig := range rulesMap {
		delete(currentRuleIDs, ruleID) // Remove from deletion list

		ruleMap := ruleConfig.(map[string]any) // Validated in ValidateConfigUpdate

		// Create or update rule
		newRule, err := rp.createRuleFromConfig(ruleID, ruleMap)
		if err != nil {
			return fmt.Errorf("failed to create rule %s: %w", ruleID, err)
		}

		// Install new rule (replacing any existing rule)
		rp.rules[ruleID] = newRule

		// Store rule configuration for GetRuntimeConfig
		rp.ruleConfigs[ruleID] = ruleMap

		rp.logger.Info("Applied rule configuration", "rule_id", ruleID, "rule_type", ruleMap["type"])
	}

	// Remove rules that are no longer configured
	for ruleID := range currentRuleIDs {
		delete(rp.rules, ruleID)
		delete(rp.ruleConfigs, ruleID)
		rp.logger.Info("Removed rule", "rule_id", ruleID)
	}

	// Update active rules metric
	if rp.metrics != nil {
		rp.metrics.activeRules.Set(float64(len(rp.rules)))
	}

	return nil
}

// GetRuntimeConfig returns current runtime configuration
func (rp *Processor) GetRuntimeConfig() map[string]any {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	// Return stored rule configurations
	rulesConfig := make(map[string]any)
	for ruleID, ruleConfig := range rp.ruleConfigs {
		rulesConfig[ruleID] = ruleConfig
	}

	return map[string]any{
		"enabled_rules":            rp.config.EnabledRules,
		"buffer_window_size":       rp.config.BufferWindowSize,
		"alert_cooldown_period":    rp.config.AlertCooldownPeriod,
		"enable_graph_integration": rp.config.EnableGraphIntegration,
		"entity_watch_patterns":    rp.config.EntityWatchPatterns,
		"rules":                    rulesConfig,
		"rule_count":               len(rp.rules),
		"is_running":               rp.isSubscribed,
	}
}

// extractConditions converts expression conditions to configuration format
func (rp *Processor) extractConditions(expr expression.LogicalExpression) []map[string]any {
	conditions := make([]map[string]any, len(expr.Conditions))
	for i, cond := range expr.Conditions {
		conditions[i] = map[string]any{
			"field":    cond.Field,
			"operator": cond.Operator,
			"value":    cond.Value,
			"required": cond.Required,
		}
	}
	return conditions
}
