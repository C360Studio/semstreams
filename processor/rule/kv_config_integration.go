// Package rule - NATS KV Configuration Integration for Rules
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// ConfigManager manages rules through NATS KV configuration
type ConfigManager struct {
	processor  *Processor
	kvStore    *natsclient.KVStore
	configMgr  *config.Manager
	updateChan <-chan config.Update // Channel received from ConfigManager
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *slog.Logger
	mu         sync.RWMutex
}

// NewConfigManager creates a new rule configuration manager
func NewConfigManager(processor *Processor, configMgr *config.Manager, logger *slog.Logger) *ConfigManager {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &ConfigManager{
		processor: processor,
		configMgr: configMgr,
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger.With("component", "rule-config-manager"),
	}
}

// Start begins watching for rule configuration updates
func (rcm *ConfigManager) Start(_ context.Context) error {
	// Subscribe to rules.* pattern for configuration updates
	rcm.updateChan = rcm.configMgr.OnChange("rules.*")

	// Start processing updates
	go rcm.processConfigUpdates()

	rcm.logger.Info("Rule configuration manager started", "pattern", "rules.*")
	return nil
}

// Stop stops the configuration manager
func (rcm *ConfigManager) Stop() error {
	rcm.cancel()

	// The channel from ConfigManager will be closed when ConfigManager stops
	// We don't close it here since we don't own it

	rcm.logger.Info("Rule configuration manager stopped")
	return nil
}

// processConfigUpdates handles configuration change notifications
func (rcm *ConfigManager) processConfigUpdates() {
	for {
		select {
		case <-rcm.ctx.Done():
			return
		case update := <-rcm.updateChan:
			rcm.handleConfigUpdate(update)
		}
	}
}

// handleConfigUpdate processes a single configuration update
func (rcm *ConfigManager) handleConfigUpdate(update config.Update) {
	rcm.logger.Debug("Received configuration update", "path", update.Path)

	// Parse the path to determine the operation
	// Expected patterns:
	// - rules.battery_monitor_001 → single rule update
	// - rules.* → batch update (handled by iterating)

	parts := strings.Split(update.Path, ".")
	if len(parts) < 2 || parts[0] != "rules" {
		rcm.logger.Warn("Invalid rule configuration path", "path", update.Path)
		return
	}

	// Get all rule configurations from the updated config
	rulesConfig := rcm.extractRulesConfig(update.Config)
	if rulesConfig == nil {
		rcm.logger.Debug("No rules configuration in update")
		return
	}

	// Convert to change map for processor
	changes := map[string]any{
		"rules": rulesConfig,
	}

	// Validate changes directly with processor
	if err := rcm.processor.ValidateConfigUpdate(changes); err != nil {
		rcm.logger.Error("Rule configuration validation failed",
			"path", update.Path,
			"error", err)
		return
	}

	// Apply changes directly to processor
	if err := rcm.processor.ApplyConfigUpdate(changes); err != nil {
		rcm.logger.Error("Failed to apply rule configuration",
			"path", update.Path,
			"error", err)
		return
	}

	rcm.logger.Info("Applied rule configuration update",
		"path", update.Path,
		"rule_count", len(rulesConfig))
}

// extractRulesConfig extracts rule configurations from the full config
func (rcm *ConfigManager) extractRulesConfig(cfg *config.SafeConfig) map[string]any {
	if cfg == nil {
		return nil
	}

	// Get the config - returns *config.Config
	_ = cfg.Get()

	// Check if rules exist in Components map
	// Rules would be stored as components in the config
	// This might need adjustment based on how rules are stored
	// For now, return empty map - will be populated via KV updates
	return make(map[string]any)
}

// SaveRule saves a rule configuration to NATS KV
func (rcm *ConfigManager) SaveRule(ctx context.Context, ruleID string, ruleDef Definition) error {
	key := fmt.Sprintf("rules.%s", ruleID)

	// Convert to JSON
	data, err := json.Marshal(ruleDef)
	if err != nil {
		return errs.WrapInvalid(err, "ConfigManager", "SaveRule", "marshal rule definition")
	}

	// Use KVStore for safe CAS operations if available
	if rcm.kvStore != nil {
		_, err = rcm.kvStore.Put(ctx, key, data)
		return err
	}

	// Fallback to ConfigManager's internal KV
	return rcm.saveViaConfigManager(ctx, key, ruleDef)
}

// saveViaConfigManager saves through the ConfigManager's KV bucket
func (rcm *ConfigManager) saveViaConfigManager(_ context.Context, _ string, _ Definition) error {
	// This would typically be exposed by ConfigManager
	// For now, we'll return an error indicating this needs implementation
	return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "saveViaConfigManager", "direct KV save not implemented")
}

// DeleteRule removes a rule configuration from NATS KV
func (rcm *ConfigManager) DeleteRule(ctx context.Context, ruleID string) error {
	key := fmt.Sprintf("rules.%s", ruleID)

	if rcm.kvStore != nil {
		return rcm.kvStore.Delete(ctx, key)
	}

	return rcm.deleteViaConfigManager(ctx, key)
}

// deleteViaConfigManager deletes through the ConfigManager's KV bucket
func (rcm *ConfigManager) deleteViaConfigManager(_ context.Context, _ string) error {
	return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "deleteViaConfigManager", "direct KV delete not implemented")
}

// GetRule retrieves a rule configuration from NATS KV
func (rcm *ConfigManager) GetRule(ctx context.Context, ruleID string) (*Definition, error) {
	key := fmt.Sprintf("rules.%s", ruleID)

	if rcm.kvStore != nil {
		entry, err := rcm.kvStore.Get(ctx, key)
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

	return rcm.getRuleViaConfigManager(ctx, ruleID)
}

// getRuleViaConfigManager retrieves through the ConfigManager
func (rcm *ConfigManager) getRuleViaConfigManager(_ context.Context, ruleID string) (*Definition, error) {
	// Get current config from processor
	currentConfig := rcm.processor.GetRuntimeConfig()

	if rulesMap, ok := currentConfig["rules"].(map[string]any); ok {
		if ruleConfig, ok := rulesMap[ruleID].(map[string]any); ok {
			// Convert map to Definition
			def := Definition{
				ID:      ruleID,
				Type:    getStringWithDefault(ruleConfig, "type", ""),
				Name:    getStringWithDefault(ruleConfig, "name", ruleID),
				Enabled: getBoolWithDefault(ruleConfig, "enabled", true),
			}
			return &def, nil
		}
	}

	return nil, errs.WrapInvalid(errs.ErrKeyNotFound, "ConfigManager", "getRuleViaConfigManager", fmt.Sprintf("rule not found: %s", ruleID))
}

// ListRules returns all rule configurations.
//
// Prefers direct KV reads when a kvStore is wired (consistent with
// SaveRule/GetRule/DeleteRule). Falls back to the processor's runtime
// config for deployments that haven't called InitializeKVStore — kept so
// the existing runtime-config-only path still works. Callers that need
// full Definition data should ensure kvStore is initialised; the
// fallback returns a stub Definition carrying only ID/Type/Name/Enabled.
func (rcm *ConfigManager) ListRules(ctx context.Context) (map[string]Definition, error) {
	rules := make(map[string]Definition)

	if rcm.kvStore != nil {
		keys, err := rcm.kvStore.Keys(ctx)
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
			entry, err := rcm.kvStore.Get(ctx, key)
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
func (rcm *ConfigManager) InitializeKVStore(natsClient *natsclient.Client) error {
	rcm.mu.Lock()
	defer rcm.mu.Unlock()

	if natsClient == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "ConfigManager", "InitializeKVStore", "NATS client is required")
	}

	// Get or create the config KV bucket
	kv, err := natsClient.CreateKeyValueBucket(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "semstreams_config",
		Description: "SemStreams runtime configuration",
		History:     5,
	})
	if err != nil {
		return errs.WrapTransient(err, "ConfigManager", "InitializeKVStore", "create/get KV bucket")
	}

	// Create KVStore for the config bucket
	rcm.kvStore = natsClient.NewKVStore(kv)

	rcm.logger.Info("Initialized KVStore for rule configuration")
	return nil
}
