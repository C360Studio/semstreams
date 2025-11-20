// Package rule - NATS KV Configuration Integration for Rules
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/natsclient"
	rtypes "github.com/c360/semstreams/types/rule"
	"github.com/nats-io/nats.go/jetstream"
)

// RuleConfigManager manages rules through NATS KV configuration
type RuleConfigManager struct {
	processor  *Processor
	kvStore    *natsclient.KVStore
	configMgr  *config.Manager
	updateChan <-chan config.Update // Channel received from ConfigManager
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *slog.Logger
	mu         sync.RWMutex
}

// NewRuleConfigManager creates a new rule configuration manager
func NewRuleConfigManager(processor *Processor, configMgr *config.Manager, logger *slog.Logger) *RuleConfigManager {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RuleConfigManager{
		processor: processor,
		configMgr: configMgr,
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger.With("component", "rule-config-manager"),
	}
}

// Start begins watching for rule configuration updates
func (rcm *RuleConfigManager) Start(ctx context.Context) error {
	// Subscribe to rules.* pattern for configuration updates
	rcm.updateChan = rcm.configMgr.OnChange("rules.*")

	// Start processing updates
	go rcm.processConfigUpdates()

	rcm.logger.Info("Rule configuration manager started", "pattern", "rules.*")
	return nil
}

// Stop stops the configuration manager
func (rcm *RuleConfigManager) Stop() error {
	rcm.cancel()

	// The channel from ConfigManager will be closed when ConfigManager stops
	// We don't close it here since we don't own it

	rcm.logger.Info("Rule configuration manager stopped")
	return nil
}

// processConfigUpdates handles configuration change notifications
func (rcm *RuleConfigManager) processConfigUpdates() {
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
func (rcm *RuleConfigManager) handleConfigUpdate(update config.Update) {
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
func (rcm *RuleConfigManager) extractRulesConfig(cfg *config.SafeConfig) map[string]any {
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
func (rcm *RuleConfigManager) SaveRule(ctx context.Context, ruleID string, ruleDef RuleDefinition) error {
	key := fmt.Sprintf("rules.%s", ruleID)

	// Convert to JSON
	data, err := json.Marshal(ruleDef)
	if err != nil {
		return fmt.Errorf("failed to marshal rule definition: %w", err)
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
func (rcm *RuleConfigManager) saveViaConfigManager(ctx context.Context, key string, ruleDef RuleDefinition) error {
	// This would typically be exposed by ConfigManager
	// For now, we'll return an error indicating this needs implementation
	return fmt.Errorf("direct KV save not yet implemented - use ConfigManager.Update()")
}

// DeleteRule removes a rule configuration from NATS KV
func (rcm *RuleConfigManager) DeleteRule(ctx context.Context, ruleID string) error {
	key := fmt.Sprintf("rules.%s", ruleID)

	if rcm.kvStore != nil {
		return rcm.kvStore.Delete(ctx, key)
	}

	return rcm.deleteViaConfigManager(ctx, key)
}

// deleteViaConfigManager deletes through the ConfigManager's KV bucket
func (rcm *RuleConfigManager) deleteViaConfigManager(ctx context.Context, key string) error {
	return fmt.Errorf("direct KV delete not yet implemented - use ConfigManager.Update()")
}

// GetRule retrieves a rule configuration from NATS KV
func (rcm *RuleConfigManager) GetRule(ctx context.Context, ruleID string) (*RuleDefinition, error) {
	key := fmt.Sprintf("rules.%s", ruleID)

	if rcm.kvStore != nil {
		entry, err := rcm.kvStore.Get(ctx, key)
		if err != nil {
			if err == jetstream.ErrKeyNotFound {
				return nil, fmt.Errorf("rule not found: %s", ruleID)
			}
			return nil, fmt.Errorf("failed to get rule: %w", err)
		}

		var ruleDef RuleDefinition
		if err := json.Unmarshal(entry.Value, &ruleDef); err != nil {
			return nil, fmt.Errorf("failed to unmarshal rule definition: %w", err)
		}

		return &ruleDef, nil
	}

	return rcm.getRuleViaConfigManager(ctx, ruleID)
}

// getRuleViaConfigManager retrieves through the ConfigManager
func (rcm *RuleConfigManager) getRuleViaConfigManager(ctx context.Context, ruleID string) (*RuleDefinition, error) {
	// Get current config from processor
	currentConfig := rcm.processor.GetRuntimeConfig()

	if rulesMap, ok := currentConfig["rules"].(map[string]any); ok {
		if ruleConfig, ok := rulesMap[ruleID].(map[string]any); ok {
			// Convert map to RuleDefinition
			def := RuleDefinition{
				ID:      ruleID,
				Type:    getStringWithDefault(ruleConfig, "type", ""),
				Name:    getStringWithDefault(ruleConfig, "name", ruleID),
				Enabled: getBoolWithDefault(ruleConfig, "enabled", true),
			}
			return &def, nil
		}
	}

	return nil, fmt.Errorf("rule not found: %s", ruleID)
}

// ListRules returns all rule configurations
func (rcm *RuleConfigManager) ListRules(ctx context.Context) (map[string]RuleDefinition, error) {
	rules := make(map[string]RuleDefinition)

	// Get current config from processor
	currentConfig := rcm.processor.GetRuntimeConfig()

	if rulesMap, ok := currentConfig["rules"].(map[string]any); ok {
		for ruleID, ruleConfig := range rulesMap {
			if configMap, ok := ruleConfig.(map[string]any); ok {
				rules[ruleID] = RuleDefinition{
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
func (rcm *RuleConfigManager) WatchRules(ctx context.Context, callback func(ruleID string, rule rtypes.Rule, operation string)) error {
	// This would set up a more sophisticated watcher
	// For now, we use the existing subscription mechanism

	// The callback would be invoked from handleConfigUpdate
	// when rules are added/updated/deleted

	return fmt.Errorf("watch rules not yet implemented")
}

// InitializeKVStore initializes the KVStore for direct KV operations
func (rcm *RuleConfigManager) InitializeKVStore(natsClient *natsclient.Client) error {
	rcm.mu.Lock()
	defer rcm.mu.Unlock()

	if natsClient == nil {
		return fmt.Errorf("NATS client is required")
	}

	// Get or create the config KV bucket
	kv, err := natsClient.CreateKeyValueBucket(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "semstreams_config",
		Description: "SemStreams runtime configuration",
		History:     5,
	})
	if err != nil {
		return fmt.Errorf("failed to create/get KV bucket: %w", err)
	}

	// Create KVStore for the config bucket
	rcm.kvStore = natsClient.NewKVStore(kv)

	rcm.logger.Info("Initialized KVStore for rule configuration")
	return nil
}
