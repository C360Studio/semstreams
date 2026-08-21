// Package rule - NATS KV Configuration Integration for Rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// hotReloadDebounce is the window used to coalesce rapid KV writes into a
// single reconcile call. Successive watcher events within this window are
// collapsed so a burst of tool-driven writes (e.g., create + update) produces
// exactly one processor apply.
const hotReloadDebounce = 250 * time.Millisecond

// ConfigManager manages rules through NATS KV configuration.
//
// Two instances of ConfigManager coexist in a running binary:
//  1. The Pattern-B CRUD manager constructed in cmd/semstreams/main.go
//     (buildRuleManager) with processor=nil. It handles agent tool writes
//     (create_rule, update_rule, delete_rule) and has no watcher.
//  2. The component-internal manager constructed inside Processor.Start
//     with processor non-nil. It owns the KV watcher and applies hot-reload
//     updates to the running processor.
//
// Both read/write semstreams_config:rules.*. The component watcher picks up
// writes from the CRUD manager via normal KV semantics.
type ConfigManager struct {
	// processor is nil for the Pattern-B CRUD-only path. When non-nil, the
	// ConfigManager drives hot-reload via SeedFromRuntime + reconcileFromKV.
	processor  *Processor
	kvStore    *natsclient.KVStore
	natsClient *natsclient.Client
	// configMgr is retained for callers that pass it; no longer used for the
	// watch subscription (rules.* is not in config.Manager's watch list).
	configMgr *config.Manager
	logger    *slog.Logger
	mu        sync.RWMutex

	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	watcher        jetstream.KeyWatcher
	done           chan struct{}
	watchRules     func(context.Context, *natsclient.KVStore) (jetstream.KeyWatcher, error)

	// reconcileCount is incremented each time reconcileFromKV completes. It is
	// only meaningful in test scenarios; production code should not depend on it.
	// For tests only.
	reconcileCount int64
}

// NewConfigManager creates a new rule configuration manager
func NewConfigManager(processor *Processor, configMgr *config.Manager, logger *slog.Logger) *ConfigManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &ConfigManager{
		processor: processor,
		configMgr: configMgr,
		logger:    logger.With("component", "rule-config-manager"),
	}
}

// Start begins watching for rule configuration updates.
//
// When processor is nil (Pattern-B CRUD path), Start is a no-op so that
// cmd/semstreams/main.go can safely call it without wiring a watcher.
//
// When processor is non-nil (component-internal path), Start:
//  1. Seeds file-loaded rules into KV idempotently via SeedFromRuntime.
//  2. Opens a KV watcher on "rules.*".
//  3. Spawns a goroutine that debounces watcher events and calls
//     reconcileFromKV on each coalesced burst.
func (rcm *ConfigManager) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "Start", "context already cancelled")
	}
	rcm.lifecycleMu.Lock()
	if rcm.lifecycleUsed {
		rcm.lifecycleMu.Unlock()
		return errs.WrapInvalid(errs.ErrAlreadyStarted, "ConfigManager", "Start", "cleanup authority already active")
	}
	rcm.lifecycleUsed = true
	rcm.cleanupPending = true
	rcm.startDone = make(chan struct{})
	startDone := rcm.startDone
	runCtx, cancel := context.WithCancel(ctx)
	rcm.cancel = cancel
	rcm.lifecycleMu.Unlock()

	finish := func(committed bool) {
		rcm.lifecycleMu.Lock()
		if committed {
			rcm.cleanupPending = false
		} else {
			cancel()
			rcm.cleanupPending = false
			rcm.terminal = true
			rcm.cancel = nil
		}
		close(startDone)
		rcm.startDone = nil
		rcm.lifecycleMu.Unlock()
	}
	// Guard: CRUD-only manager (processor nil) — no watcher needed.
	if rcm.processor == nil {
		rcm.logger.Debug("Rule config manager started in CRUD-only mode (no hot-reload watcher)")
		finish(true)
		return nil
	}
	if _, err := rcm.ensureKVStore(ctx); err != nil {
		finish(false)
		return err
	}

	// Seed any file-loaded rules idempotently before opening the watcher.
	if err := rcm.SeedFromRuntime(runCtx); err != nil {
		// Non-fatal: component runs with whatever is already in KV.
		rcm.logger.Warn("SeedFromRuntime completed with errors; some file rules may not be in KV",
			"error", err)
	}

	rcm.mu.RLock()
	kvStore := rcm.kvStore
	rcm.mu.RUnlock()
	watchRules := rcm.watchRules
	if watchRules == nil {
		watchRules = func(watchCtx context.Context, store *natsclient.KVStore) (jetstream.KeyWatcher, error) {
			return store.Watch(watchCtx, "rules.*")
		}
	}
	watcher, err := watchRules(runCtx, kvStore)
	if err != nil {
		// Hot-reload silently disabled; processor still runs with file rules.
		rcm.logger.Warn("Failed to open KV watcher for rules.*, hot-reload disabled", "error", err)
		finish(true)
		return nil
	}
	if err := runCtx.Err(); err != nil {
		// Stop may cancel an in-flight acquisition whose implementation still
		// returns a handle. The acquiring goroutine owns that late handle: stop it
		// before terminalizing, and never publish it as running state that Stop's
		// already-taken acquisition fence could miss.
		stopErr := watcher.Stop()
		finish(false)
		return errors.Join(err, stopErr)
	}
	done := make(chan struct{})
	rcm.lifecycleMu.Lock()
	rcm.cancel = cancel
	rcm.watcher = watcher
	rcm.done = done
	rcm.lifecycleMu.Unlock()

	go rcm.processKVUpdates(runCtx, watcher, done)

	rcm.logger.Info("Rule configuration hot-reload watcher started", "pattern", "rules.*")
	finish(true)
	return nil
}

// Stop stops the configuration manager and waits for the watcher goroutine
// to exit cleanly.
func (rcm *ConfigManager) Stop() error {
	for {
		rcm.lifecycleMu.Lock()
		if !rcm.lifecycleUsed {
			rcm.lifecycleUsed = true
			rcm.terminal = true
			rcm.lifecycleMu.Unlock()
			return nil
		}
		if rcm.terminal {
			rcm.lifecycleMu.Unlock()
			return nil
		}
		if rcm.startDone != nil {
			startDone := rcm.startDone
			cancel := rcm.cancel
			rcm.lifecycleMu.Unlock()
			// Cleanup authority is published before watcher acquisition. A Stop
			// racing Start cancels that exact attempt, then joins startDone before
			// selecting the acquired-watcher or no-watcher cleanup path.
			if cancel != nil {
				cancel()
			}
			<-startDone
			continue
		}
		if rcm.stopping {
			rcm.lifecycleMu.Unlock()
			return errors.New("rule configuration manager: concurrent Stop")
		}
		rcm.stopping = true
		cancel := rcm.cancel
		watcher := rcm.watcher
		done := rcm.done
		rcm.lifecycleMu.Unlock()

		var stopErr error
		if watcher != nil {
			stopErr = watcher.Stop()
		}
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}

		rcm.lifecycleMu.Lock()
		rcm.stopping = false
		rcm.cleanupPending = false
		rcm.terminal = true
		rcm.cancel = nil
		rcm.watcher = nil
		rcm.done = nil
		rcm.lifecycleMu.Unlock()
		rcm.logger.Info("Rule configuration manager stopped")
		return stopErr
	}
}

// processKVUpdates is the watcher goroutine. It applies a 250 ms debounce:
// successive events within the window are collapsed into a single
// reconcileFromKV call. All reconcile work runs inside this goroutine so
// the WaitGroup correctly gates Stop().
func (rcm *ConfigManager) processKVUpdates(ctx context.Context, watcher jetstream.KeyWatcher, done chan<- struct{}) {
	defer close(done)

	// pendingReconcile is a timer channel; nil means no pending reconcile.
	var timerCh <-chan time.Time
	var timer *time.Timer

	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				// Drain the channel if Stop returned false (timer already fired).
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer = time.NewTimer(hotReloadDebounce)
		timerCh = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return

		case entry, ok := <-watcher.Updates():
			if !ok {
				// Channel closed; watcher stopped externally.
				return
			}
			// nil is the initial-snapshot delimiter — trigger a reconcile to
			// pick up whatever is already in KV, then keep watching for live
			// updates.
			resetTimer()
			_ = entry // Whether nil (snapshot done) or a real entry, debounce fires.

		case <-timerCh:
			timerCh = nil
			timer = nil
			if err := rcm.reconcileFromKV(ctx); err != nil {
				rcm.logger.Error("Rule hot-reload reconcile failed", "error", err)
			}
		}
	}
}

// reconcileFromKV reads the full rules.* set from KV and applies it to the
// processor via ValidateConfigUpdate + ApplyConfigUpdate.
//
// Full-replace semantics are intentional: applyRuleChanges removes any rule
// absent from the update map, so we always pass the complete ruleset.
func (rcm *ConfigManager) reconcileFromKV(ctx context.Context) error {
	defs, err := rcm.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("list rules from KV: %w", err)
	}

	// Convert map[string]Definition → map[string]any for the processor API.
	// Round-trip via JSON to get the same shape that ValidateConfigUpdate expects.
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

	if err := rcm.processor.ValidateConfigUpdate(changes); err != nil {
		rcm.logger.Error("Rule configuration validation failed during hot-reload", "error", err)
		return fmt.Errorf("validate config update: %w", err)
	}

	if err := rcm.processor.ApplyConfigUpdate(changes); err != nil {
		rcm.logger.Error("Failed to apply rule configuration during hot-reload", "error", err)
		return fmt.Errorf("apply config update: %w", err)
	}

	rcm.logger.Info("Hot-reload: applied rule configuration from KV", "rule_count", len(rulesMap))
	atomic.AddInt64(&rcm.reconcileCount, 1)
	return nil
}

// ReconcileCount returns the total number of times reconcileFromKV has completed
// successfully. Intended for integration tests that verify debounce coalescing.
// For tests only.
func (rcm *ConfigManager) ReconcileCount() int {
	return int(atomic.LoadInt64(&rcm.reconcileCount))
}

// SeedFromRuntime writes file-loaded rules from the running processor into KV
// idempotently. Uses Create (not Put) so operator edits already in KV are
// never overwritten — a key-already-exists error is treated as a no-op.
//
// Reads from rp.ruleDefinitions (populated by loadRules/Initialize) since
// rp.ruleConfigs (from GetRuntimeConfig) is only populated by ApplyConfigUpdate,
// not by the initial file/inline load path.
//
// Partial failures are logged but do not abort seeding. The method returns nil
// even when individual writes fail so that hot-reload startup is not blocked.
func (rcm *ConfigManager) SeedFromRuntime(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "SeedFromRuntime", "context cannot be nil")
	}
	rcm.mu.RLock()
	kvStore := rcm.kvStore
	rcm.mu.RUnlock()
	if rcm.processor == nil || kvStore == nil {
		return nil
	}

	// Read definitions under the processor's read lock.
	rcm.processor.mu.RLock()
	defs := make(map[string]Definition, len(rcm.processor.ruleDefinitions))
	for id, def := range rcm.processor.ruleDefinitions {
		defs[id] = def
	}
	rcm.processor.mu.RUnlock()

	if len(defs) == 0 {
		return nil
	}

	for ruleID, def := range defs {
		data, err := json.Marshal(def)
		if err != nil {
			rcm.logger.Warn("SeedFromRuntime: failed to marshal rule; skipping",
				"rule_id", ruleID, "error", err)
			continue
		}
		key := fmt.Sprintf("rules.%s", ruleID)
		if _, err := kvStore.Create(ctx, key, data); err != nil {
			if errors.Is(err, natsclient.ErrKVKeyExists) {
				// Operator edit in KV — preserve it.
				rcm.logger.Debug("SeedFromRuntime: rule already in KV, preserving operator edit",
					"rule_id", ruleID)
				continue
			}
			rcm.logger.Warn("SeedFromRuntime: failed to seed rule into KV; skipping",
				"rule_id", ruleID, "error", err)
		} else {
			rcm.logger.Debug("SeedFromRuntime: seeded rule into KV", "rule_id", ruleID)
		}
	}

	return nil
}

// SaveRule saves a rule configuration to NATS KV.
func (rcm *ConfigManager) SaveRule(ctx context.Context, ruleID string, ruleDef Definition) error {
	kvStore, err := rcm.ensureKVStore(ctx)
	if err != nil {
		return err
	}
	if err := ValidateDefinition(ruleDef); err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "SaveRule", "validate rule authoring contract")
	}
	key := fmt.Sprintf("rules.%s", ruleID)
	data, err := json.Marshal(ruleDef)
	if err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "SaveRule", "marshal rule definition")
	}
	_, err = kvStore.Put(ctx, key, data)
	return err
}

// DeleteRule removes a rule configuration from NATS KV.
func (rcm *ConfigManager) DeleteRule(ctx context.Context, ruleID string) error {
	kvStore, err := rcm.ensureKVStore(ctx)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("rules.%s", ruleID)
	return kvStore.Delete(ctx, key)
}

// GetRule retrieves a rule configuration from NATS KV.
func (rcm *ConfigManager) GetRule(ctx context.Context, ruleID string) (*Definition, error) {
	kvStore, ensureErr := rcm.ensureKVStore(ctx)
	if ensureErr != nil {
		return nil, ensureErr
	}
	key := fmt.Sprintf("rules.%s", ruleID)
	entry, err := kvStore.Get(ctx, key)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, errs.WrapInvalid(errs.ErrKeyNotFound, "ConfigManager", "GetRule", fmt.Sprintf("rule not found: %s", ruleID))
		}
		return nil, errs.WrapTransient(err, "ConfigManager", "GetRule", "get rule from KV")
	}

	var ruleDef Definition
	if err := json.Unmarshal(entry.Value, &ruleDef); err != nil {
		return nil, errs.WrapInvalid(err, "ConfigManager", "GetRule", "unmarshal rule definition")
	}
	return &ruleDef, nil
}

// ListRules returns all rule configurations from the KV store.
//
// Prefers direct KV reads when a kvStore is wired (consistent with
// SaveRule/GetRule/DeleteRule). Falls back to the processor's runtime
// config for deployments that haven't called InitializeKVStore — kept so
// the existing runtime-config-only path still works. Callers that need
// full Definition data should ensure kvStore is initialised; the
// fallback returns a stub Definition carrying only ID/Type/Name/Enabled.
func (rcm *ConfigManager) ListRules(ctx context.Context) (map[string]Definition, error) {
	rules := make(map[string]Definition)

	rcm.mu.RLock()
	hasKVAuthority := rcm.natsClient != nil || rcm.kvStore != nil
	rcm.mu.RUnlock()
	if hasKVAuthority {
		kvStore, err := rcm.ensureKVStore(ctx)
		if err != nil {
			return nil, err
		}
		keys, err := kvStore.Keys(ctx)
		if err != nil {
			return nil, errs.WrapTransient(err, "ConfigManager", "ListRules", "list keys from KV")
		}
		for _, key := range keys {
			// Filter to the rules.* namespace — the bucket may hold other
			// config keys.
			if !strings.HasPrefix(key, "rules.") {
				continue
			}
			ruleID := strings.TrimPrefix(key, "rules.")
			entry, err := kvStore.Get(ctx, key)
			if err != nil {
				rcm.logger.Warn("Failed to load rule during ListRules; skipping",
					"rule_id", ruleID, "error", err)
				continue
			}
			var def Definition
			if err := json.Unmarshal(entry.Value, &def); err != nil {
				rcm.logger.Warn("Failed to unmarshal rule during ListRules; skipping",
					"rule_id", ruleID, "error", err)
				continue
			}
			rules[ruleID] = def
		}
		return rules, nil
	}

	// Fallback: runtime-config-only path (no KV). Returns stub Definitions.
	if rcm.processor == nil {
		return rules, nil
	}
	currentConfig := rcm.processor.GetRuntimeConfig()
	if rulesMap, ok := currentConfig["rules"].(map[string]any); ok {
		for ruleID, ruleConfig := range rulesMap {
			if configMap, ok := ruleConfig.(map[string]any); ok {
				rules[ruleID] = Definition{
					ID:      ruleID,
					Type:    getStringWithDefault(configMap, "type", ""),
					Name:    getStringWithDefault(configMap, "name", ruleID),
					Enabled: getBoolWithDefault(configMap, "enabled", true),
				}
			}
		}
	}
	return rules, nil
}

// WatchRules watches for rule changes and returns active rules
func (rcm *ConfigManager) WatchRules(_ context.Context, _ func(ruleID string, rule Rule, operation string)) error {
	// This would set up a more sophisticated watcher
	// For now, we use the existing subscription mechanism

	// The callback would be invoked from handleConfigUpdate
	// when rules are added/updated/deleted

	return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "WatchRules", "watch rules not implemented")
}

// InitializeKVStore initializes the KVStore for direct KV operations
func (rcm *ConfigManager) InitializeKVStore(ctx context.Context, natsClient *natsclient.Client) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "InitializeKVStore", "context cannot be nil")
	}
	rcm.mu.Lock()
	if natsClient == nil {
		rcm.mu.Unlock()
		return errs.WrapInvalid(errs.ErrMissingConfig, "ConfigManager", "InitializeKVStore", "NATS client is required")
	}
	rcm.natsClient = natsClient
	rcm.mu.Unlock()

	_, err := rcm.ensureKVStore(ctx)
	return err
}

func (rcm *ConfigManager) ensureKVStore(ctx context.Context) (*natsclient.KVStore, error) {
	if ctx == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "ensureKVStore", "context cannot be nil")
	}
	rcm.mu.RLock()
	if rcm.kvStore != nil {
		kvStore := rcm.kvStore
		rcm.mu.RUnlock()
		return kvStore, nil
	}
	natsClient := rcm.natsClient
	rcm.mu.RUnlock()
	if natsClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "ensureKVStore", "kvStore not initialised")
	}

	// Remote acquisition is retryable. Publish only the successful handle;
	// concurrent creators converge on the one authoritative bucket.
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "semstreams_config",
		Description: "SemStreams runtime configuration",
		History:     5,
	})
	if err != nil {
		return nil, errs.WrapTransient(err, "ConfigManager", "ensureKVStore", "create/get KV bucket")
	}
	created := natsClient.NewKVStore(kv)
	rcm.mu.Lock()
	if rcm.kvStore == nil {
		rcm.kvStore = created
	}
	kvStore := rcm.kvStore
	rcm.mu.Unlock()

	rcm.logger.Info("Initialized KVStore for rule configuration")
	return kvStore, nil
}
