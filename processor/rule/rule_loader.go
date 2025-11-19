package rule

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// loadRuleDefinitionsFromFiles loads rule definitions from JSON files
func (rp *Processor) loadRuleDefinitionsFromFiles() ([]RuleDefinition, error) {
	var allDefinitions []RuleDefinition

	for _, filePath := range rp.config.RulesFiles {
		rp.logger.Debug("Loading rules from file", "path", filePath)

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read rules file %s: %w", filePath, err)
		}

		// Parse JSON - support both single rule and array of rules
		var definitions []RuleDefinition

		// Try parsing as array first
		if err := json.Unmarshal(data, &definitions); err != nil {
			// Try parsing as single rule
			var singleDef RuleDefinition
			if err2 := json.Unmarshal(data, &singleDef); err2 != nil {
				return nil, fmt.Errorf("failed to parse rules file %s: %w (also tried as single rule: %v)",
					filePath, err, err2)
			}
			definitions = []RuleDefinition{singleDef}
		}

		rp.logger.Info("Loaded rule definitions from file", "path", filePath, "count", len(definitions))
		allDefinitions = append(allDefinitions, definitions...)
	}

	return allDefinitions, nil
}

// loadRules loads and configures rules based on configuration
// IMPORTANT: This function must be called while holding rp.mu lock
// as it modifies rp.rules directly
func (rp *Processor) loadRules() error {
	// Parse buffer window size
	bufferWindowSize, err := time.ParseDuration(rp.config.BufferWindowSize)
	if err != nil {
		bufferWindowSize = 10 * time.Minute
		rp.logger.Warn("Invalid buffer window size, using default", "default", bufferWindowSize, "error", err)
	}

	// Parse alert cooldown period
	alertCooldown, err := time.ParseDuration(rp.config.AlertCooldownPeriod)
	if err != nil {
		alertCooldown = 2 * time.Minute
		rp.logger.Warn("Invalid alert cooldown period, using default", "default", alertCooldown, "error", err)
	}

	// Load rule definitions from files
	fileDefinitions, err := rp.loadRuleDefinitionsFromFiles()
	if err != nil {
		return fmt.Errorf("failed to load rule definitions from files: %w", err)
	}

	// Combine file definitions with inline definitions
	allDefinitions := append(fileDefinitions, rp.config.InlineRules...)

	// Create rules from definitions using factory pattern
	ruleDeps := RuleDependencies{
		NATSClient: rp.natsClient,
		Logger:     rp.logger,
	}

	for _, def := range allDefinitions {
		// Skip disabled rules
		if !def.Enabled {
			rp.logger.Debug("Skipping disabled rule", "rule_id", def.ID)
			continue
		}

		// Create rule from definition
		rule, err := CreateRuleFromDefinition(def, ruleDeps)
		if err != nil {
			rp.logger.Error("Failed to create rule from definition",
				"rule_id", def.ID,
				"rule_type", def.Type,
				"error", err)
			continue // Skip invalid rules but continue loading others
		}

		rp.rules[def.ID] = rule
		rp.logger.Info("Loaded rule from definition",
			"rule_id", def.ID,
			"rule_type", def.Type,
			"rule_name", def.Name)
	}

	// Legacy support: enabled_rules (deprecated)
	if len(rp.config.EnabledRules) > 0 {
		rp.logger.Warn("EnabledRules is deprecated - use rules_files or inline_rules instead",
			"enabled_rules", rp.config.EnabledRules)
	}

	// Warn if no rules are configured
	if len(rp.rules) == 0 {
		rp.logger.Warn("No rules configured - processor will not trigger any actions. " +
			"Configure rules via rules_files or inline_rules in config.")
	}

	// Update active rules metric
	if rp.metrics != nil {
		rp.metrics.activeRules.Set(float64(len(rp.rules)))
	}

	return nil
}
