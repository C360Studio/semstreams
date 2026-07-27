package rule

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// definitionToRuleMap converts a Definition to the same map[string]any
// shape that hot-reload's KV-watcher delivers. Populated into
// rp.ruleConfigs alongside rp.ruleDefinitions so the idempotency check
// in applyCronRuleChange recognises the seed-then-reconcile reapply as
// a no-op (preserves in-memory cron scheduler state — entry.lastFiredNanos
// in particular — across the reconcile pass that immediately follows
// seeding inline rules to KV).
//
// # Round-trip stability
//
// The reflect.DeepEqual idempotency check in applyCronRuleChange
// requires that this function produces the SAME map shape as the
// watcher path (KV.Get → json.Unmarshal-into-Definition → json.Marshal
// → KV.Put → KV.Get → json.Unmarshal-into-map). Both paths converge
// because every Definition field has a fixed-point JSON shape:
//
//   - Typed numerics stay typed (Conditions are typed Go slices, not
//     interface{} containers — so the float64-vs-int worry that bites
//     map[string]any users doesn't apply here).
//   - nil-vs-empty-slice is preserved by Go's encoding/json.
//   - Map-iteration order is irrelevant under reflect.DeepEqual.
//   - The interface{} fields (Conditions[i].Value, Metadata) round-trip
//     stably because they were written from JSON sources upstream.
//
// On marshal failure the function logs at Debug and returns nil. The
// caller treats nil as "no cached config", so the idempotency check
// just falls through and the reapply runs — same behaviour as before
// this helper existed.
func (rp *Processor) definitionToRuleMap(def Definition) map[string]any {
	data, err := json.Marshal(def)
	if err != nil {
		rp.logger.Debug("definitionToRuleMap: marshal failed; idempotency cache miss",
			"rule_id", def.ID, "error", err)
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		rp.logger.Debug("definitionToRuleMap: unmarshal failed; idempotency cache miss",
			"rule_id", def.ID, "error", err)
		return nil
	}
	return m
}

// loadRuleDefinitionsFromFiles loads rule definitions from JSON files
func (rp *Processor) loadRuleDefinitionsFromFiles() ([]Definition, error) {
	var allDefinitions []Definition

	for _, filePath := range rp.config.RulesFiles {
		rp.logger.Debug("Loading rules from file", "path", filePath)

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read rules file %s: %w", filePath, err)
		}

		// Parse JSON - support both single rule and array of rules
		var definitions []Definition

		// Try parsing as array first
		if err := json.Unmarshal(data, &definitions); err != nil {
			// Try parsing as single rule
			var singleDef Definition
			if err2 := json.Unmarshal(data, &singleDef); err2 != nil {
				return nil, fmt.Errorf("failed to parse rules file %s: %w (also tried as single rule: %v)",
					filePath, err, err2)
			}
			definitions = []Definition{singleDef}
		}

		rp.logger.Info("Loaded rule definitions from file", "path", filePath, "count", len(definitions))
		allDefinitions = append(allDefinitions, definitions...)
	}

	return allDefinitions, nil
}

func cloneRuleDefinitions(definitions []Definition) ([]Definition, error) {
	data, err := json.Marshal(definitions)
	if err != nil {
		return nil, fmt.Errorf("copy rule definitions: %w", err)
	}
	var copies []Definition
	if err := json.Unmarshal(data, &copies); err != nil {
		return nil, fmt.Errorf("copy rule definitions: %w", err)
	}
	return copies, nil
}

// prepareInitialRules parses, validates, and freezes the boot-time rules and
// projection target index. Caller holds rp.mu when the processor is live.
func (rp *Processor) prepareInitialRules() error {
	if rp.initialRulesReady {
		return nil
	}
	targets, err := buildProjectionTargetIndex(rp.config.ProjectionContracts)
	if err != nil {
		return err
	}
	fileDefinitions, err := rp.loadRuleDefinitionsFromFiles()
	if err != nil {
		return fmt.Errorf("failed to load rule definitions from files: %w", err)
	}
	inlineDefinitions, err := cloneRuleDefinitions(rp.config.InlineRules)
	if err != nil {
		return err
	}
	allDefinitions := append(append([]Definition(nil), fileDefinitions...), inlineDefinitions...)
	for _, definition := range allDefinitions {
		if err := ValidateDefinition(definition); err != nil {
			return fmt.Errorf("rule authoring validation failed for %s: %w", definition.ID, err)
		}
		if err := validateRuleReplaceOwnedActions(targets, definition); err != nil {
			return fmt.Errorf(
				"replace_owned envelope validation failed for rule %s: %w",
				definition.ID,
				err,
			)
		}
	}
	snapshot, err := cloneRuleDefinitions(allDefinitions)
	if err != nil {
		return err
	}
	rp.config.ProjectionContracts = cloneProjectionContracts(rp.config.ProjectionContracts)
	rp.initialRules = snapshot
	rp.projectionTargets = targets
	rp.initialRulesReady = true
	return nil
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

	if err := rp.prepareInitialRules(); err != nil {
		return err
	}
	allDefinitions, err := cloneRuleDefinitions(rp.initialRules)
	if err != nil {
		return err
	}

	// Create rules from definitions using factory pattern
	ruleDeps := Dependencies{
		NATSClient: rp.natsClient,
		Logger:     rp.logger,
		PackID:     rp.config.PackID,
	}

	for _, def := range allDefinitions {
		// Skip disabled rules
		if !def.Enabled {
			rp.logger.Debug("Skipping disabled rule", "rule_id", def.ID)
			continue
		}

		// ADR-056 Decision 3: enforce the replace_owned envelope at the
		// PROCESSOR level (the envelope is rp.config.ProjectionContracts, which
		// the stateless rule factory does not see). A replace_owned action
		// naming a predicate outside the pack's owned-replace contracts — or a
		// non-literal predicate — HARD-FAILS the load. Unlike a malformed
		// individual rule (which is logged + skipped so siblings still load), a
		// broken owned-write claim must NOT silently ship: return the error so
		// boot aborts. Runs before the cron/expression split so it covers
		// def.Actions (cron) and the transition lists (expression) uniformly.
		// Cron rules take a separate construction path: they don't go through
		// the factory registry because CronRule does not implement the Rule
		// interface (Subscribe/Evaluate/ExecuteEvents are message-driven and
		// cron has no message). The scheduler picks them up later in
		// initializeCronScheduler. Definition is stored on rp.ruleDefinitions
		// so hot-reload, GetRuntimeConfig, and FireEveryNEvents accounting
		// stay symmetric across rule kinds.
		if def.Type == CronRuleType {
			cronRule, err := NewCronRule(def)
			if err != nil {
				rp.logger.Error("Failed to create cron rule from definition",
					"rule_id", def.ID,
					"error", err)
				continue
			}
			rp.cronRules[def.ID] = cronRule
			rp.ruleDefinitions[def.ID] = def
			rp.ruleConfigs[def.ID] = rp.definitionToRuleMap(def)
			rp.logger.Info("Loaded cron rule from definition",
				"rule_id", def.ID,
				"rule_name", def.Name,
				"schedule", def.Schedule,
				"action_count", len(def.Actions))
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

		// ADR-047: propagate the Lifecycle harness Manager onto rule
		// instances that opt into condition.Field resolution for
		// `$entity.lifecycle.*` paths. Shared with the hot-reload reconcile
		// path via attachLifecycleManager so no rule-construction path can
		// silently drop it (gh#451).
		rp.attachLifecycleManager(rule)

		rp.rules[def.ID] = rule
		rp.ruleDefinitions[def.ID] = def // Store definition for stateful evaluation
		rp.ruleConfigs[def.ID] = rp.definitionToRuleMap(def)

		// Initialize match counter for FireEveryNEvents gating. The counter
		// starts at zero regardless of FireEveryNEvents value; the first Nth
		// match (not the zeroth) will fire. Allocation is cheap; all rules get
		// a counter even when FireEveryNEvents is 0/1 (shouldFireAction fast-paths
		// those cases without touching the counter).
		rp.matchCounters[def.ID] = &atomic.Int64{}

		rp.logger.Info("Loaded rule from definition",
			"rule_id", def.ID,
			"rule_type", def.Type,
			"rule_name", def.Name,
			"on_enter_actions", len(def.OnEnter),
			"on_exit_actions", len(def.OnExit))
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
