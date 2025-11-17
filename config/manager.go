package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
)

// Update represents a configuration change notification
type Update struct {
	Path   string      // Changed path (e.g., "services.metrics")
	Config *SafeConfig // Full latest configuration
}

// Manager provides centralized configuration management with channel-based updates
type Manager struct {
	config      *SafeConfig              // Current configuration
	kv          jetstream.KeyValue       // NATS KV bucket for config
	kvStore     *natsclient.KVStore      // KVStore abstraction for safe operations
	watchers    []jetstream.KeyWatcher   // Watchers for specific patterns
	subscribers map[string][]chan Update // Pattern -> channels
	mu          sync.RWMutex             // Protects subscribers map
	logger      *slog.Logger             // Structured logger

	// Lifecycle management
	shutdownCh chan struct{}  // Signal shutdown to goroutines
	wg         sync.WaitGroup // Track all goroutines
	stopped    atomic.Bool    // Indicates manager is stopped
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(cfg *Config, natsClient *natsclient.Client, logger *slog.Logger) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if natsClient == nil {
		return nil, fmt.Errorf("nats client cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Create or get KV bucket for config
	ctx := context.Background()
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "semstreams_config",
		Description: "SemStreams runtime configuration",
		History:     5, // Keep last 5 versions
	})
	if err != nil {
		return nil, fmt.Errorf("create/get KV bucket: %w", err)
	}

	// Create KVStore for safe operations
	kvStore := natsClient.NewKVStore(kv)

	return &Manager{
		config:      NewSafeConfig(cfg),
		kv:          kv,
		kvStore:     kvStore,
		subscribers: make(map[string][]chan Update),
		logger:      logger,
	}, nil
}

// GetConfig returns the current configuration
func (cm *Manager) GetConfig() *SafeConfig {
	return cm.config
}

// OnChange subscribes to configuration changes matching the pattern
// Returns a channel that receives updates when configuration changes
// Pattern examples:
//   - "services.metrics" - exact match
//   - "services.*" - all services
//   - "components.*" - all components
//   - "components.udp-*" - components starting with udp-
func (cm *Manager) OnChange(pattern string) <-chan Update {
	ch := make(chan Update, 1) // Buffered to prevent blocking

	cm.mu.Lock()
	cm.subscribers[pattern] = append(cm.subscribers[pattern], ch)
	cm.mu.Unlock()

	// Send initial config immediately
	select {
	case ch <- Update{
		Path:   pattern,
		Config: cm.config,
	}:
	default:
		// Channel full, skip initial update
	}

	return ch
}

// Start begins watching for configuration changes
func (cm *Manager) Start(ctx context.Context) error {
	// Initialize shutdown channel
	cm.shutdownCh = make(chan struct{})

	// Determine if this is first boot or subsequent boot
	hasConfig, err := cm.hasKVConfig(ctx)
	if err != nil {
		cm.logger.Warn("Failed to check KV config existence", "error", err)
		// Assume first boot on error
		hasConfig = false
	}

	if !hasConfig {
		// First boot: push file config to KV for UI
		cm.logger.Info("First boot detected, pushing config to KV")
		if err := cm.PushToKV(ctx); err != nil {
			cm.logger.Error("Failed to push initial config to KV", "error", err)
			// Continue anyway - UI won't have initial state but app can run
		}
	} else {
		// Subsequent boot: compare versions to decide sync direction
		fileVersion := cm.config.Get().Version
		kvVersion, err := cm.getKVVersion(ctx)
		if err != nil {
			cm.logger.Warn("Failed to get KV version, syncing from KV", "error", err)
			// Fall back to syncing from KV if we can't get version
			if err := cm.syncFromKV(ctx); err != nil {
				cm.logger.Warn("Failed to sync from KV on startup", "error", err)
			}
		} else {
			// Compare versions
			cmp, err := CompareVersions(fileVersion, kvVersion)
			if err != nil {
				cm.logger.Warn("Failed to compare versions, syncing from KV",
					"file_version", fileVersion,
					"kv_version", kvVersion,
					"error", err)
				// Fall back to syncing from KV on version comparison error
				if err := cm.syncFromKV(ctx); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			} else if cmp > 0 {
				// File version is newer: update KV from file
				cm.logger.Info("File version is newer than KV, updating KV",
					"file_version", fileVersion,
					"kv_version", kvVersion)
				if err := cm.PushToKV(ctx); err != nil {
					cm.logger.Error("Failed to update KV with newer config", "error", err)
				}
			} else if cmp < 0 {
				// KV version is newer: warn and use KV
				cm.logger.Warn("File version is older than KV, using KV config",
					"file_version", fileVersion,
					"kv_version", kvVersion,
					"hint", "bump file version to update KV")
				if err := cm.syncFromKV(ctx); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			} else {
				// Versions equal: sync from KV (UI may have made changes)
				cm.logger.Info("File and KV versions match, syncing from KV",
					"version", fileVersion)
				if err := cm.syncFromKV(ctx); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			}
		}
	}

	// Watch specific patterns (2-part keys only)
	// Use * for single-level wildcard to exclude property-level keys
	patterns := []string{
		"services.*",   // Matches services.metrics but NOT services.metrics.enabled
		"components.*", // Matches components.udp but NOT components.udp.port
		"platform",     // Single key
		"nats",         // Single key
	}

	// Create watchers with cleanup on error
	cm.watchers = make([]jetstream.KeyWatcher, 0, len(patterns))

	// Cleanup function if we error out
	cleanup := func() {
		for _, w := range cm.watchers {
			if w != nil {
				_ = w.Stop() // Ignore stop errors during cleanup
			}
		}
		cm.watchers = nil
	}

	for _, pattern := range patterns {
		// Use UpdatesOnly since we've already synced existing values
		watcher, err := cm.kv.Watch(ctx, pattern, jetstream.UpdatesOnly())
		if err != nil {
			// Ignore errors for patterns that don't exist yet
			// They'll be picked up when keys are created
			cm.logger.Debug("Failed to create watcher", "pattern", pattern, "error", err)
			continue
		}
		cm.watchers = append(cm.watchers, watcher)
	}

	// If we didn't create any watchers, that's an error
	if len(cm.watchers) == 0 {
		cleanup()
		return fmt.Errorf("failed to create any watchers")
	}

	// Process updates from all watchers in background
	for _, watcher := range cm.watchers {
		cm.wg.Add(1)
		go cm.processWatcher(ctx, watcher)
	}

	return nil
}

// Stop stops watching for configuration changes
func (cm *Manager) Stop(timeout time.Duration) error {
	// Mark as stopped to prevent new operations
	if !cm.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}

	// Signal shutdown to all goroutines
	if cm.shutdownCh != nil {
		close(cm.shutdownCh)
	}

	// Stop all watchers
	for _, watcher := range cm.watchers {
		if watcher != nil {
			_ = watcher.Stop() // Ignore errors during shutdown
		}
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		cm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(timeout):
		cm.logger.Warn("Manager shutdown timeout", "timeout", timeout)
	}

	// Now close all subscriber channels (after watchers stopped)
	cm.mu.Lock()
	for _, channels := range cm.subscribers {
		for _, ch := range channels {
			close(ch)
		}
	}
	cm.subscribers = make(map[string][]chan Update)
	cm.mu.Unlock()

	return nil
}

// processWatcher handles incoming KV updates from a specific watcher
func (cm *Manager) processWatcher(ctx context.Context, watcher jetstream.KeyWatcher) {
	defer cm.wg.Done()

	for {
		select {
		case <-ctx.Done():
			// Parent context cancelled
			return

		case <-cm.shutdownCh:
			// Manager is shutting down
			return

		case entry := <-watcher.Updates():
			// With UpdatesOnly, we shouldn't get nil entries
			// but check anyway for safety
			if entry != nil {
				cm.handleUpdate(entry.Key(), entry.Value())
			}
		}
	}
}

// handleUpdate processes a single configuration update
func (cm *Manager) handleUpdate(key string, value []byte) {
	// Check if we're shutting down
	if cm.stopped.Load() {
		return
	}

	// Update internal configuration
	if err := cm.updateConfig(key, value); err != nil {
		cm.logger.Error("Failed to update configuration",
			"key", key,
			"error", err)
		return
	}

	// Create update notification
	update := Update{
		Path:   key,
		Config: cm.config,
	}

	// Notify matching subscribers - check shutdown before each send
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for pattern, channels := range cm.subscribers {
		if cm.matchesPattern(key, pattern) {
			for _, ch := range channels {
				// Check if still running before sending
				if cm.stopped.Load() {
					return
				}

				// Non-blocking send
				select {
				case ch <- update:
					// Sent successfully
				default:
					// Channel full, subscriber not keeping up
					// This is by design - we don't wait for slow consumers
				}
			}
		}
	}
}

// matchesPattern checks if a key matches a subscription pattern
func (cm *Manager) matchesPattern(key, pattern string) bool {
	// Exact match
	if pattern == key {
		return true
	}

	// Wildcard suffix: "services.*" matches "services.metrics"
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(key, prefix+".")
	}

	// Prefix wildcard: "components.udp-*" matches "components.udp-sensor"
	if strings.Contains(pattern, "*") {
		// Split at the wildcard and check prefix
		parts := strings.SplitN(pattern, "*", 2)
		if len(parts) > 0 {
			return strings.HasPrefix(key, parts[0])
		}
	}

	return false
}

// updateConfig updates the internal configuration based on KV update
func (cm *Manager) updateConfig(key string, value []byte) error {
	// Validate JSON structure if value is not empty (deletion)
	if len(value) > 0 {
		// Check size limits
		if len(value) > maxConfigSize {
			return fmt.Errorf("config value too large: %d bytes > %d", len(value), maxConfigSize)
		}
		// Validate JSON depth to prevent DoS
		if err := validateJSONDepth(value); err != nil {
			return fmt.Errorf("invalid JSON structure in KV update: %w", err)
		}
	}

	// Parse the key to determine what part of config to update
	// Expected format: "services.metrics", "components.udp-sensor", etc.
	parts := strings.Split(key, ".")
	if len(parts) < 1 {
		return fmt.Errorf("invalid key format: %s", key)
	}

	// Get current config
	currentConfig := cm.config.Get()

	// Update the appropriate section
	switch parts[0] {
	case "services":
		if len(parts) != 2 {
			return fmt.Errorf("invalid service key format: %s", key)
		}
		serviceName := parts[1]

		// Handle deletion
		if len(value) == 0 {
			delete(currentConfig.Services, serviceName)
		} else {
			if currentConfig.Services == nil {
				currentConfig.Services = make(types.ServiceConfigs)
			}
			// Parse the value as ServiceConfig (already validated above)
			var svcConfig types.ServiceConfig
			if err := json.Unmarshal(value, &svcConfig); err != nil {
				return fmt.Errorf("failed to parse service config: %w", err)
			}
			currentConfig.Services[serviceName] = svcConfig
		}

	case "components":
		if len(parts) != 2 {
			return fmt.Errorf("invalid component key format: %s", key)
		}
		componentName := parts[1]

		// Handle deletion
		if len(value) == 0 {
			delete(currentConfig.Components, componentName)
		} else {
			// Parse component config (already validated above)
			var compConfig types.ComponentConfig
			if err := json.Unmarshal(value, &compConfig); err != nil {
				return fmt.Errorf("parse component config: %w", err)
			}
			if currentConfig.Components == nil {
				currentConfig.Components = make(ComponentConfigs)
			}
			currentConfig.Components[componentName] = compConfig
		}

	case "platform":
		// Update platform config (already validated above)
		if err := json.Unmarshal(value, &currentConfig.Platform); err != nil {
			return fmt.Errorf("parse platform config: %w", err)
		}

	case "nats":
		// Update NATS config (already validated above)
		if err := json.Unmarshal(value, &currentConfig.NATS); err != nil {
			return fmt.Errorf("parse NATS config: %w", err)
		}

	// Graph and ObjectStore config moved to components

	default:
		// Unknown top-level key, ignore
		return nil
	}

	// Update the config atomically
	return cm.config.Update(currentConfig)
}

// sanitizeNATSKey replaces characters invalid in NATS keys with underscore s
// NATS key restrictions: no spaces, must use printable ASCII
func sanitizeNATSKey(key string) string {
	// Replace spaces and other problematic characters with underscore s
	// This preserves readability while ensuring NATS compatibility
	return strings.ReplaceAll(key, " ", "_")
}

// PushToKV pushes the current configuration to NATS KV
// This is useful for initial setup or config synchronization
func (cm *Manager) PushToKV(ctx context.Context) error {
	cfg := cm.config.Get()

	// Push version first
	cm.logger.Debug("PushToKV: checking version", "version", cfg.Version)
	if cfg.Version != "" {
		data, err := json.Marshal(cfg.Version)
		if err != nil {
			return fmt.Errorf("marshal version: %w", err)
		}
		cm.logger.Info("Pushing version to KV", "version", cfg.Version)
		if _, err := cm.kvStore.Put(ctx, "version", data); err != nil {
			return fmt.Errorf("push version: %w", err)
		}
	} else {
		cm.logger.Warn("Config version is empty, not pushing to KV")
	}

	// Push each section to KV
	// Services
	for name, svcConfig := range cfg.Services {
		key := fmt.Sprintf("services.%s", sanitizeNATSKey(name))
		// Marshal the entire ServiceConfig structure
		data, err := json.Marshal(svcConfig)
		if err != nil {
			return fmt.Errorf("marshal service %s: %w", name, err)
		}
		if _, err := cm.kvStore.Put(ctx, key, data); err != nil {
			return fmt.Errorf("push service %s: %w", name, err)
		}
	}

	// Components
	for name, compConfig := range cfg.Components {
		key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
		data, err := json.Marshal(compConfig)
		if err != nil {
			return fmt.Errorf("marshal component %s: %w", name, err)
		}
		if _, err := cm.kvStore.Put(ctx, key, data); err != nil {
			return fmt.Errorf("push component %s: %w", name, err)
		}
	}

	// Platform
	if data, err := json.Marshal(cfg.Platform); err == nil && len(data) > 2 { // > 2 to skip empty {}
		if _, err := cm.kvStore.Put(ctx, "platform", data); err != nil {
			return fmt.Errorf("push platform: %w", err)
		}
	}

	// NATS
	if data, err := json.Marshal(cfg.NATS); err == nil && len(data) > 2 {
		if _, err := cm.kvStore.Put(ctx, "nats", data); err != nil {
			return fmt.Errorf("push nats: %w", err)
		}
	}

	// Graph and ObjectStore config moved to components

	return nil
}

// hasKVConfig checks if the KV bucket has any configuration
func (cm *Manager) hasKVConfig(ctx context.Context) (bool, error) {
	// Check for any keys in the bucket by listing with limit 1
	keys, err := cm.kv.Keys(ctx)
	if err != nil {
		return false, fmt.Errorf("list KV keys: %w", err)
	}

	// If we have any keys, we have config
	return len(keys) > 0, nil
}

// getKVVersion retrieves the version from KV bucket
func (cm *Manager) getKVVersion(ctx context.Context) (string, error) {
	// Try to get version from KV
	entry, err := cm.kv.Get(ctx, "version")
	if err != nil {
		// Version key doesn't exist (old config format)
		return "0.0.0", nil
	}

	// Parse version string from value
	var version string
	if err := json.Unmarshal(entry.Value(), &version); err != nil {
		cm.logger.Warn("Failed to parse version from KV, treating as 0.0.0", "error", err)
		return "0.0.0", nil
	}

	return version, nil
}

// syncFromKV loads all configuration from KV and applies it
func (cm *Manager) syncFromKV(ctx context.Context) error {
	// List all keys
	keys, err := cm.kv.Keys(ctx)
	if err != nil {
		return fmt.Errorf("list KV keys: %w", err)
	}

	// Process each key
	for _, key := range keys {
		// Skip property-level keys (3+ parts)
		parts := strings.Split(key, ".")
		if len(parts) > 2 {
			cm.logger.Debug("Skipping property-level key during sync", "key", key)
			continue
		}

		// Get the value
		entry, err := cm.kv.Get(ctx, key)
		if err != nil {
			cm.logger.Warn("Failed to get KV entry during sync",
				"key", key,
				"error", err)
			continue
		}

		// Apply the update
		if err := cm.updateConfig(key, entry.Value()); err != nil {
			cm.logger.Warn("Failed to apply KV config during sync",
				"key", key,
				"error", err)
			// Continue with other keys
		}
	}

	cm.logger.Info("Synced configuration from KV", "keys", len(keys))
	return nil
}
