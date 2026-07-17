package rule

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ApplyConfigUpdate applies validated configuration changes
func (rp *Processor) ApplyConfigUpdate(changes map[string]any) error {
	var (
		buckets    map[string][]string
		hasBuckets bool
	)
	if bucketsVal, ok := changes["entity_watch_buckets"]; ok {
		var err error
		buckets, err = parseEntityWatchBuckets(bucketsVal)
		if err != nil {
			return errs.WrapInvalid(err, "RuleProcessor", "ApplyConfigUpdate", "parse entity watch buckets")
		}
		hasBuckets = true
	}

	rp.mu.Lock()

	// Apply rule configuration changes
	if rulesConfig, ok := changes["rules"]; ok {
		rulesMap := rulesConfig.(map[string]any) // Validated in ValidateConfigUpdate
		if err := rp.applyRuleChanges(rulesMap); err != nil {
			rp.mu.Unlock()
			return errs.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "apply rule changes")
		}
	}

	// Apply enable_graph_integration changes
	if integrationVal, ok := changes["enable_graph_integration"]; ok {
		integration := integrationVal.(bool) // Validated in ValidateConfigUpdate
		rp.config.EnableGraphIntegration = integration
		rp.logger.Info("Updated graph integration setting", "enabled", integration)
	}
	rp.mu.Unlock()

	// Watch preparation performs NATS I/O outside the processor config mutex.
	if hasBuckets {
		if err := rp.UpdateWatchBuckets(buckets); err != nil {
			return errs.Wrap(err, "RuleProcessor", "ApplyConfigUpdate", "update watch buckets")
		}
	}

	return nil
}

// applyRuleChanges applies dynamic rule configuration changes
func (rp *Processor) applyRuleChanges(rulesMap map[string]any) error {
	// Ensure maps are initialized (struct-literal callers may omit them).
	if rp.matchCounters == nil {
		rp.matchCounters = make(map[string]*atomic.Int64)
	}
	if rp.ruleDefinitions == nil {
		rp.ruleDefinitions = make(map[string]Definition)
	}
	if rp.cronRules == nil {
		rp.cronRules = make(map[string]*CronRule)
	}

	// Track existing rules in both registries so we can compute removals at
	// the end. Cron rules and expression rules share the same ID namespace
	// — a rule cannot switch types in place; the deletion + re-add path
	// handles type changes naturally.
	currentRuleIDs := make(map[string]bool)
	for ruleID := range rp.rules {
		currentRuleIDs[ruleID] = true
	}
	for ruleID := range rp.cronRules {
		currentRuleIDs[ruleID] = true
	}

	// Process rule updates/additions
	for ruleID, ruleConfig := range rulesMap {
		delete(currentRuleIDs, ruleID) // Remove from deletion list

		ruleMap := ruleConfig.(map[string]any) // Validated in ValidateConfigUpdate

		if t, _ := ruleMap["type"].(string); t == CronRuleType {
			if err := rp.applyCronRuleChange(ruleID, ruleMap); err != nil {
				return err
			}
			continue
		}

		if err := rp.applyExpressionRuleChange(ruleID, ruleMap); err != nil {
			return err
		}
	}

	// Remove rules that are no longer configured
	for ruleID := range currentRuleIDs {
		rp.removeRule(ruleID)
	}

	// Update active rules metric
	if rp.metrics != nil {
		rp.metrics.activeRules.Set(float64(len(rp.rules)))
	}

	return nil
}

// applyCronRuleChange installs or replaces a cron rule definition. If a
// rule with the same ID already exists in either registry it is dropped
// from both before the new cron rule is registered, which makes type
// swaps (expression → cron) symmetric with applyExpressionRuleChange.
//
// Idempotent on identical re-applies: when ruleMap equals the previously-
// stored config for this ID, the function is a no-op. This matters for
// the seed-then-reconcile path used at startup with InlineRules — the
// inline rules get seeded to the KV bucket and the watcher's reconcile
// pass replays them. Without the idempotency check, that replay would
// Deregister + Register every cron rule, resetting in-memory cooldown
// state and causing a transient "ignored cooldown" window right at
// process startup.
//
// Caller holds rp.mu.Lock.
func (rp *Processor) applyCronRuleChange(ruleID string, ruleMap map[string]any) error {
	if existing, ok := rp.ruleConfigs[ruleID]; ok && reflect.DeepEqual(existing, ruleMap) {
		// Same definition already applied. Skip the deregister-register
		// hop — preserves entry.lastFiredNanos, tickCount, and any other
		// in-memory scheduler state across the reconcile pass.
		return nil
	}

	def, err := definitionFromMap(ruleID, ruleMap)
	if err != nil {
		return fmt.Errorf("failed to parse cron rule %s: %w", ruleID, err)
	}
	cronRule, err := NewCronRule(def)
	if err != nil {
		return fmt.Errorf("failed to create cron rule %s: %w", ruleID, err)
	}

	// Validate-then-mutate ordering: register on the scheduler BEFORE we
	// touch the rp.* maps. If Register fails, the rule isn't yet visible
	// in cronRules/ruleDefinitions/ruleConfigs and the operator just sees
	// the error — no inconsistent half-state where the maps say
	// "registered" but the scheduler doesn't.
	if rp.cronScheduler != nil {
		// Deregister is a no-op when the rule isn't registered, so
		// first-time additions and replacements share one path. The
		// replacement is logically atomic from the operator's perspective;
		// in the worst case a tick lands between Deregister and Register
		// and is dropped, which matches the log-only missed-fire policy.
		rp.cronScheduler.Deregister(ruleID)
		if err := rp.cronScheduler.Register(cronRule); err != nil {
			return fmt.Errorf("failed to register cron rule %s: %w", ruleID, err)
		}
	}

	// Drop any previous Rule-typed entry under this ID (type swap), then
	// install the new cron-side bookkeeping.
	delete(rp.rules, ruleID)
	delete(rp.matchCounters, ruleID)
	rp.cronRules[ruleID] = cronRule
	rp.ruleDefinitions[ruleID] = def
	rp.ruleConfigs[ruleID] = ruleMap

	rp.logger.Info("Applied cron rule configuration",
		"rule_id", ruleID,
		"schedule", def.Schedule,
		"action_count", len(def.Actions))
	return nil
}

// attachLifecycleManager offers the Lifecycle harness Manager (ADR-047) to a
// freshly created rule instance. Only ExpressionRule implements the setter, so
// the type-assert keeps the Rule interface narrow. EVERY path that constructs a
// rule instance — the file/inline load (rule_loader.go) AND the hot-reload KV
// reconcile (applyExpressionRuleChange) — must route through this, or the rule
// keeps a nil manager and `$entity.lifecycle.*` conditions silently never
// resolve (gh#451). It reads rp.lifecycleManager, which is set once at factory
// time (factory.go, before Start) and never mutated after — so both callers
// (init-time loadRules and the lock-held reconcile) read it race-free; the
// helper takes no lock of its own.
func (rp *Processor) attachLifecycleManager(rule Rule) {
	if rp.lifecycleManager == nil {
		return
	}
	if setter, ok := rule.(interface {
		SetLifecycleManager(LifecycleManager)
	}); ok {
		setter.SetLifecycleManager(rp.lifecycleManager)
	}
}

// applyExpressionRuleChange installs or replaces an expression-style rule.
// Drops any previous CronRule under the same ID first to keep type swaps
// symmetric with applyCronRuleChange.
//
// Caller holds rp.mu.Lock.
func (rp *Processor) applyExpressionRuleChange(ruleID string, ruleMap map[string]any) error {
	newRule, def, err := rp.createRuleFromConfig(ruleID, ruleMap)
	if err != nil {
		return fmt.Errorf("failed to create rule %s: %w", ruleID, err)
	}

	// gh#451: hot-reloaded rules must receive the Lifecycle Manager just like
	// the file-load path (rule_loader.go). Without this, rules created via the
	// KV-config reconcile have a nil manager and $entity.lifecycle.* conditions
	// never fire.
	rp.attachLifecycleManager(newRule)

	if _, hadCron := rp.cronRules[ruleID]; hadCron {
		if rp.cronScheduler != nil {
			rp.cronScheduler.Deregister(ruleID)
		}
		delete(rp.cronRules, ruleID)
		rp.deleteScheduleRecord(ruleID)
	}

	rp.rules[ruleID] = newRule
	rp.ruleDefinitions[ruleID] = def
	rp.ruleConfigs[ruleID] = ruleMap
	rp.matchCounters[ruleID] = &atomic.Int64{}

	rp.logger.Info("Applied rule configuration", "rule_id", ruleID, "rule_type", ruleMap["type"])
	return nil
}

// removeRule drops a rule from every registry it might appear in (rules,
// cronRules, scheduler, definitions, configs, matchCounters). Called from
// the removals loop in applyRuleChanges. Caller holds rp.mu.Lock.
func (rp *Processor) removeRule(ruleID string) {
	if _, isCron := rp.cronRules[ruleID]; isCron {
		if rp.cronScheduler != nil {
			rp.cronScheduler.Deregister(ruleID)
		}
		delete(rp.cronRules, ruleID)
		rp.deleteScheduleRecord(ruleID)
	}
	delete(rp.rules, ruleID)
	delete(rp.ruleDefinitions, ruleID)
	delete(rp.ruleConfigs, ruleID)
	delete(rp.matchCounters, ruleID)
	rp.logger.Info("Removed rule", "rule_id", ruleID)
}

// deleteScheduleRecord clears the persisted last-fired record for a cron
// rule that's being removed or type-swapped to expression. Best-effort:
// failures log a Warn but do not block the rule removal — a stale record
// is purely an observability concern, and a future re-add of the same
// rule ID will overwrite the record on its first fire.
//
// Uses context.Background() because this runs under rp.mu.Lock from
// hot-reload paths (ApplyConfigUpdate) where no caller-supplied context
// is available. The KV Delete is fast and idempotent; the underlying
// NATS client enforces its own request timeout, so an unbounded ctx
// here is bounded in practice.
func (rp *Processor) deleteScheduleRecord(ruleID string) {
	if rp.scheduleTracker == nil {
		return
	}
	if err := rp.scheduleTracker.Delete(context.Background(), ruleID); err != nil {
		rp.logger.Warn("Failed to delete schedule record on rule removal",
			"rule_id", ruleID,
			"error", err)
	}
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
		"entity_watch_buckets":     rp.config.EntityWatchBuckets,
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
