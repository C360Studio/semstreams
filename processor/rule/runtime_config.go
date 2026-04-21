package rule

import (
	"fmt"
	"sync/atomic"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ApplyConfigUpdate applies validated configuration changes
func (rp *Processor) ApplyConfigUpdate(changes map[string]any) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Apply rule configuration changes
	if rulesConfig, ok := changes["rules"]; ok {
		rulesMap := rulesConfig.(map[string]any) // Validated in ValidateConfigUpdate
		if err := rp.applyRuleChanges(rulesMap); err != nil {
			return errs.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "apply rule changes")
		}
	}

	// Apply entity_watch_patterns changes dynamically (backwards compatibility)
	if patternsVal, ok := changes["entity_watch_patterns"]; ok {
		patterns := rp.convertToStringSlice(patternsVal)

		// Dynamically update watchers - no restart required
		// Use the locked version since we already hold the lock
		if err := rp.updateWatchPatternsLocked(patterns); err != nil {
			return errs.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "update watch patterns")
		}
	}

	// Apply entity_watch_buckets changes dynamically (multi-bucket support)
	if bucketsVal, ok := changes["entity_watch_buckets"]; ok {
		buckets := rp.convertToBucketPatterns(bucketsVal)

		// Dynamically update bucket watchers - no restart required
		if err := rp.updateWatchBucketsLocked(buckets); err != nil {
			return errs.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "update watch buckets")
		}
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
	// Ensure matchCounters is initialized (struct-literal callers may omit it).
	if rp.matchCounters == nil {
		rp.matchCounters = make(map[string]*atomic.Int64)
	}

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

		// Install new rule (replacing any existing rule).
		rp.rules[ruleID] = newRule

		// Store rule configuration for GetRuntimeConfig
		rp.ruleConfigs[ruleID] = ruleMap

		// Reset (or create) the match counter for this rule. A policy change
		// implies a fresh rhythm: the caller's intent on update is "apply this
		// new definition from here on," not "preserve the previous counter state."
		rp.matchCounters[ruleID] = &atomic.Int64{}

		rp.logger.Info("Applied rule configuration", "rule_id", ruleID, "rule_type", ruleMap["type"])
	}

	// Remove rules that are no longer configured
	for ruleID := range currentRuleIDs {
		delete(rp.rules, ruleID)
		delete(rp.ruleConfigs, ruleID)
		delete(rp.matchCounters, ruleID)
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
		"buffer_window_size":       rp.config.BufferWindowSize,
		"alert_cooldown_period":    rp.config.AlertCooldownPeriod,
		"enable_graph_integration": rp.config.EnableGraphIntegration,
		"entity_watch_patterns":    rp.config.EntityWatchPatterns,
		"entity_watch_buckets":     rp.config.EntityWatchBuckets,
		"rules":                    rulesConfig,
		"rule_count":               len(rp.rules),
		"is_running":               rp.isSubscribed,
	}
}

// convertToBucketPatterns converts a generic value to map[string][]string
func (rp *Processor) convertToBucketPatterns(val any) map[string][]string {
	result := make(map[string][]string)

	switch v := val.(type) {
	case map[string]any:
		for bucket, patterns := range v {
			result[bucket] = rp.convertToStringSlice(patterns)
		}
	case map[string][]string:
		return v
	}

	return result
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
