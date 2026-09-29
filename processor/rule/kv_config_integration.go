// Package rule - NATS KV Configuration Integration for Rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// hotReloadDebounce is the window used to coalesce rapid KV writes into a
// single reconcile call. Successive watcher events within this window are
// collapsed so a burst of tool-driven writes (e.g., create + update) produces
// exactly one processor apply.
const hotReloadDebounce = 250 * time.Millisecond

// rulesFamily is the key family "rules.<id>" in the configuration bucket.
const rulesFamily = "rules"

// HotReloadTarget is what a running rule processor exposes to the one rule
// ConfigManager: the rules it loaded from files and inline configuration, and
// the runtime-update seam that hot reload reconciles into.
type HotReloadTarget interface {
	LoadedRuleDefinitions() map[string]Definition
	ValidateConfigUpdate(changes map[string]any) error
	ApplyConfigUpdate(changes map[string]any) error
}

// ConfigManager manages rules through the configuration bucket's `rules.*` key
// family.
//
// There is exactly one, and the composition root owns it (internal/boot). It
// never acquires the bucket: the bucket's name is derived from the
// deployment's declared authority pair, which only config.Manager holds. It
// registers its key family with config.Manager (config.WithKeyFamily), which
// delivers the family's entries to it and scopes its reads and writes to
// `rules.*`. Owner ruling on #1188, 2026-09-27 (Q1 (d)).
//
// It serves rule CRUD to the agent tools (create_rule, update_rule,
// delete_rule, list_rules, get_rule). Once the root calls Start with the
// constructed rule processors, it seeds their loaded rules into the family
// and reconciles the full `rules.*` set into each through ApplyConfigUpdate,
// again after every debounced change.
type ConfigManager struct {
	family *config.KeyFamily
	logger *slog.Logger

	// wake carries "the family changed" from the config manager's watch
	// goroutine to the reconcile loop. Buffer one: a pending wake already
	// covers any later change, because a reconcile lists the whole family.
	wake chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	terminal    bool
	cancel      context.CancelFunc
	done        chan struct{}

	// reconcileCount is incremented each time a reconcile completes. For tests only.
	reconcileCount int64
}

// NewConfigManager creates the rule configuration manager and its key family.
// Register the family with config.WithKeyFamily(rcm.KeyFamily()) before the
// config manager starts.
func NewConfigManager(logger *slog.Logger) (*ConfigManager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	rcm := &ConfigManager{
		logger: logger.With("component", "rule-config-manager"),
		wake:   make(chan struct{}, 1),
	}
	family, err := config.NewKeyFamily(rulesFamily, rcm.onRuleEntry)
	if err != nil {
		return nil, err
	}
	rcm.family = family
	return rcm, nil
}

// KeyFamily returns the `rules` key family to register with the config manager.
func (rcm *ConfigManager) KeyFamily() *config.KeyFamily {
	return rcm.family
}

// onRuleEntry runs on the config manager's watch goroutine and must not
// block, so it only leaves a wake-up for the reconcile loop.
func (rcm *ConfigManager) onRuleEntry(config.KeyFamilyEntry) {
	select {
	case rcm.wake <- struct{}{}:
	default:
	}
}

// Start seeds each target's loaded rules into the family, reconciles the full
// `rules.*` set into every target, and then keeps reconciling after each
// debounced change until Stop.
//
// The root runs it as the "rule-config" service, which service.Manager starts
// after the component manager has started the rule processors, so the first
// reconcile finds processors whose subscriptions and scheduler exist; and after
// seeding, so a full-replace reconcile never meets an empty family and removes
// the file rules. Entries delivered before Start only leave a wake-up pending.
//
// Cancellation and the completion fence are published before the seeding
// work, so a Stop that races Start cancels it and waits until Start has
// released its targets and the reconcile loop has exited.
func (rcm *ConfigManager) Start(ctx context.Context, targets []HotReloadTarget) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "Start", "context cannot be nil")
	}
	rcm.lifecycleMu.Lock()
	if rcm.started || rcm.terminal {
		rcm.lifecycleMu.Unlock()
		return errs.WrapInvalid(errs.ErrAlreadyStarted, "ConfigManager", "Start", "rule configuration manager is one-shot")
	}
	rcm.started = true
	if len(targets) == 0 {
		rcm.lifecycleMu.Unlock()
		rcm.logger.Debug("No rule processor constructed; rule hot reload has nothing to reconcile")
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	rcm.cancel = cancel
	rcm.done = done
	rcm.lifecycleMu.Unlock()

	targets = append([]HotReloadTarget(nil), targets...)
	rcm.seed(runCtx, targets)
	if runCtx.Err() == nil {
		if err := rcm.reconcile(runCtx, targets); err != nil {
			// Declared, not silent: the processors keep the rules they loaded and
			// the next change retries the whole set.
			rcm.logger.Error("Initial rule reconcile failed; processors keep their loaded rules", "error", err)
		}
	}

	// run owns done from here: it closes it when runCtx ends, which is at once
	// if a Stop already cancelled it.
	go rcm.run(runCtx, targets, done)
	rcm.logger.Info("Rule configuration hot reload started", "targets", len(targets))
	return nil
}

// Stop ends the reconcile loop and waits, bounded by ctx, for it and for a
// Start still seeding or reconciling. The root runs the manager as the
// "rule-config" service, which service.Manager stops before the component
// manager, so no reconcile races a processor's teardown.
//
// A nil ctx is refused before any action. Stop cancels, then returns nil once
// the completion fence closes, or ctx's error if ctx ends first; the loop
// still exits when the work it waits on returns, and a later Stop waits on the
// same fence again. Every concurrent Stop waits on that fence; once it has
// closed, a repeated Stop is a nil no-op (service.Service's Stop contract,
// gh#520). Stop before Start is a no-op.
func (rcm *ConfigManager) Stop(ctx context.Context) error {
	if err := requireContext(ctx, "Stop"); err != nil {
		return err
	}
	rcm.lifecycleMu.Lock()
	rcm.terminal = true
	cancel, done := rcm.cancel, rcm.done
	rcm.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	// Completion already observed wins over an ended ctx: with both ready, the
	// bounded select below could pick either, and a completed repeated Stop
	// must be a nil no-op (Codex round 2 on #1188; as BaseService.Stop does).
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("rule configuration manager stop: reconcile loop still running: %w", ctx.Err())
	}
}

// run debounces wake-ups and reconciles once per coalesced burst.
func (rcm *ConfigManager) run(ctx context.Context, targets []HotReloadTarget, done chan<- struct{}) {
	defer close(done)
	var timer *time.Timer
	var timerCh <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rcm.wake:
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(hotReloadDebounce)
			timerCh = timer.C
		case <-timerCh:
			timer, timerCh = nil, nil
			if err := rcm.reconcile(ctx, targets); err != nil {
				rcm.logger.Error("Rule hot-reload reconcile failed", "error", err)
			}
		}
	}
}

// reconcile reads the full rules.* set and applies it to every target through
// ValidateConfigUpdate + ApplyConfigUpdate.
//
// Full-replace semantics are intentional: applyRuleChanges removes any rule
// absent from the update map, so we always pass the complete ruleset.
func (rcm *ConfigManager) reconcile(ctx context.Context, targets []HotReloadTarget) error {
	defs, err := rcm.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("list rules from KV: %w", err)
	}

	// Round-trip via JSON to get the shape ValidateConfigUpdate expects.
	rulesMap := make(map[string]any, len(defs))
	for id, def := range defs {
		b, err := json.Marshal(def)
		if err != nil {
			rcm.logger.Warn("Failed to marshal rule definition during reconcile; skipping",
				"rule_id", id, "error", err)
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			rcm.logger.Warn("Failed to unmarshal rule definition during reconcile; skipping",
				"rule_id", id, "error", err)
			continue
		}
		rulesMap[id] = m
	}
	changes := map[string]any{"rules": rulesMap}

	var applyErrs []error
	for i, target := range targets {
		if err := target.ValidateConfigUpdate(changes); err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("rule processor %d: validate config update: %w", i, err))
			continue
		}
		if err := target.ApplyConfigUpdate(changes); err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("rule processor %d: apply config update: %w", i, err))
		}
	}
	if err := errors.Join(applyErrs...); err != nil {
		return err
	}

	rcm.logger.Info("Hot-reload: applied rule configuration from KV", "rule_count", len(rulesMap))
	atomic.AddInt64(&rcm.reconcileCount, 1)
	return nil
}

// ReconcileCount returns how many reconciles have completed successfully.
// For tests only.
func (rcm *ConfigManager) ReconcileCount() int {
	return int(atomic.LoadInt64(&rcm.reconcileCount))
}

// seed writes each target's loaded rules into the family idempotently. Create,
// not Put: an operator edit already in KV is never overwritten. A failed write
// is logged and skipped so hot reload is not blocked by one rule.
func (rcm *ConfigManager) seed(ctx context.Context, targets []HotReloadTarget) {
	for _, target := range targets {
		for ruleID, def := range target.LoadedRuleDefinitions() {
			data, err := json.Marshal(def)
			if err != nil {
				rcm.logger.Warn("Seed: failed to marshal rule; skipping", "rule_id", ruleID, "error", err)
				continue
			}
			if err := rcm.family.Create(ctx, ruleID, data); err != nil {
				if errors.Is(err, natsclient.ErrKVKeyExists) {
					rcm.logger.Debug("Seed: rule already in KV, preserving operator edit", "rule_id", ruleID)
					continue
				}
				rcm.logger.Warn("Seed: failed to seed rule into KV; skipping", "rule_id", ruleID, "error", err)
				continue
			}
			rcm.logger.Debug("Seed: seeded rule into KV", "rule_id", ruleID)
		}
	}
}

func requireContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", operation, "context cannot be nil")
	}
	return nil
}

// SaveRule saves a rule configuration to NATS KV.
func (rcm *ConfigManager) SaveRule(ctx context.Context, ruleID string, ruleDef Definition) error {
	if err := requireContext(ctx, "SaveRule"); err != nil {
		return err
	}
	if err := ValidateDefinition(ruleDef); err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "SaveRule", "validate rule authoring contract")
	}
	data, err := json.Marshal(ruleDef)
	if err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "SaveRule", "marshal rule definition")
	}
	return rcm.family.Put(ctx, ruleID, data)
}

// DeleteRule removes a rule configuration from NATS KV.
func (rcm *ConfigManager) DeleteRule(ctx context.Context, ruleID string) error {
	if err := requireContext(ctx, "DeleteRule"); err != nil {
		return err
	}
	return rcm.family.Delete(ctx, ruleID)
}

// GetRule retrieves a rule configuration from NATS KV.
func (rcm *ConfigManager) GetRule(ctx context.Context, ruleID string) (*Definition, error) {
	if err := requireContext(ctx, "GetRule"); err != nil {
		return nil, err
	}
	value, err := rcm.family.Get(ctx, ruleID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errs.WrapInvalid(errs.ErrKeyNotFound, "ConfigManager", "GetRule", fmt.Sprintf("rule not found: %s", ruleID))
		}
		return nil, errs.WrapTransient(err, "ConfigManager", "GetRule", "get rule from KV")
	}

	var ruleDef Definition
	if err := json.Unmarshal(value, &ruleDef); err != nil {
		return nil, errs.WrapInvalid(err, "ConfigManager", "GetRule", "unmarshal rule definition")
	}
	return &ruleDef, nil
}

// ListRules returns every rule definition in the `rules.*` family. An entry
// that cannot be read or decoded is logged and skipped.
func (rcm *ConfigManager) ListRules(ctx context.Context) (map[string]Definition, error) {
	if err := requireContext(ctx, "ListRules"); err != nil {
		return nil, err
	}
	names, err := rcm.family.Names(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "ConfigManager", "ListRules", "list keys from KV")
	}
	rules := make(map[string]Definition, len(names))
	for _, ruleID := range names {
		value, err := rcm.family.Get(ctx, ruleID)
		if err != nil {
			rcm.logger.Warn("Failed to load rule during ListRules; skipping",
				"rule_id", ruleID, "error", err)
			continue
		}
		var def Definition
		if err := json.Unmarshal(value, &def); err != nil {
			rcm.logger.Warn("Failed to unmarshal rule during ListRules; skipping",
				"rule_id", ruleID, "error", err)
			continue
		}
		rules[ruleID] = def
	}
	return rules, nil
}
