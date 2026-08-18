package config

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

	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
)

// Manager provides centralized durable configuration management.
type Manager struct {
	config   *SafeConfig
	bootMu   sync.RWMutex
	boot     *Config
	kv       jetstream.KeyValue
	kvStore  *natsclient.KVStore
	watchers []jetstream.KeyWatcher
	logger   *slog.Logger

	pendingMu    sync.Mutex
	pendingLocal map[string]pendingLocalWrite

	// Lifecycle management
	shutdownCh chan struct{}  // Signal shutdown to goroutines
	wg         sync.WaitGroup // Track all goroutines
	stopped    atomic.Bool    // Indicates manager is stopped

	// detached is set when Start refuses to adopt config from a bucket
	// owned by a different platform identity (gh#459). In detached mode the
	// manager runs on its local file config and must not touch the shared KV
	// bucket at all — the write methods (PushToKV / PutComponentToKV /
	// DeleteComponentFromKV) reject writes so a later explicit diagram
	// publish cannot bleed the local app's components INTO the foreign bucket
	// (the reverse-direction of the adoption bug).
	detached atomic.Bool
}

type pendingLocalWrite struct {
	revision uint64
	delete   bool
}

func detachedConfigWriteError(operation, target string) error {
	cause := fmt.Errorf("config manager detached from foreign KV bucket: cannot %s %s", operation, target)
	return errs.WrapFatal(cause, "ConfigManager", operation, "refuse write to foreign KV bucket")
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(ctx context.Context, cfg *Config, natsClient *natsclient.Client, logger *slog.Logger) (*Manager, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}
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
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "semstreams_config",
		Description: "SemStreams durable desired configuration",
		History:     5, // Keep last 5 versions
	})
	if err != nil {
		return nil, fmt.Errorf("create/get KV bucket: %w", err)
	}

	// Create KVStore for safe operations
	kvStore := natsClient.NewKVStore(kv)

	return &Manager{
		config:       NewSafeConfig(cfg),
		kv:           kv,
		kvStore:      kvStore,
		pendingLocal: make(map[string]pendingLocalWrite),
		logger:       logger,
	}, nil
}

// GetConfig returns the current configuration
func (cm *Manager) GetConfig() *SafeConfig {
	return cm.config
}

// Start arbitrates and seals boot configuration, then watches durable desired
// configuration so authoring reads remain current. Watch updates never mutate
// the sealed process composition.
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
		// Guard against cross-app config bleed on shared NATS (gh#459).
		// The config bucket has a fixed global name (semstreams_config), so
		// two sem* apps pointed at the same NATS server share it. Sync
		// direction is otherwise decided purely by version, and matching
		// versions is NOT matching identity — the second app to boot would
		// silently adopt the first's components (and can panic creating a
		// foreign component). If the stored config carries a DIFFERENT
		// platform identity (org+id+env) than the local file, refuse to
		// adopt: mark the manager detached, run on local file config, and
		// touch the shared bucket no further (no sync, push, watch, or later
		// runtime write — see the detached field). Shout so the operator
		// notices they're on the wrong NATS. Identity-less configs (no
		// org/id on either side) fall through to the existing behavior —
		// they're indistinguishable, and per-platform bucket namespacing is
		// the complete fix for that case.
		if kvIdentity, found := cm.kvPlatformIdentity(ctx); found {
			localIdentity := cm.config.Get().Platform
			if platformHasIdentity(localIdentity) && platformHasIdentity(kvIdentity) &&
				platformIdentityKey(localIdentity) != platformIdentityKey(kvIdentity) {
				cm.detached.Store(true)
				cm.logger.Error(
					"Refusing to adopt config from a bucket owned by a different platform identity; "+
						"running on local file config, detached from KV. Likely pointed at the wrong "+
						"NATS server, or the shared config bucket needs per-platform namespacing.",
					"local_identity", platformIdentityKey(localIdentity),
					"kv_identity", platformIdentityKey(kvIdentity),
					"bucket", "semstreams_config")
				cm.sealBootConfig()
				return nil
			}
		}

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
				// Versions equal: sync from KV (an author may have changed desired state)
				cm.logger.Debug("File and KV versions match, syncing from KV",
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
		"services.*",     // Matches services.metrics but NOT services.metrics.enabled
		"components.*",   // Matches components.udp but NOT components.udp.port
		"platform",       // Single key
		"nats",           // Single key
		"model_registry", // Single key
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

	// Writes made during startup happened before UpdatesOnly watchers existed,
	// so they cannot produce echoes and must not remain pending forever. From
	// this point onward every local write has an active watcher to classify it.
	cm.pendingMu.Lock()
	clear(cm.pendingLocal)
	cm.pendingMu.Unlock()

	// Successful arbitration is the one boot authority. Seal it before any
	// watcher can apply later desired-state writes.
	cm.sealBootConfig()

	// Process updates from all watchers in background
	for _, watcher := range cm.watchers {
		cm.wg.Add(1)
		go cm.processWatcher(ctx, watcher)
	}

	return nil
}

func (cm *Manager) sealBootConfig() {
	cm.bootMu.Lock()
	defer cm.bootMu.Unlock()
	cm.boot = cm.config.Get()
}

// BootConfig returns a defensive copy of the exact configuration selected by
// successful Start arbitration. Later desired writes never change it.
func (cm *Manager) BootConfig() *Config {
	cm.bootMu.RLock()
	defer cm.bootMu.RUnlock()
	if cm.boot == nil {
		return nil
	}
	return cm.boot.Clone()
}

// ComponentRestartRequired reports whether current desired component
// configuration differs from the sealed boot component map.
func (cm *Manager) ComponentRestartRequired() (bool, error) {
	boot := cm.BootConfig()
	if boot == nil {
		return false, fmt.Errorf("config manager has no sealed boot configuration")
	}
	current := cm.config.Get()
	if current == nil {
		return false, fmt.Errorf("config manager has no current configuration")
	}
	if len(boot.Components) != len(current.Components) {
		return true, nil
	}
	for name, bootComponent := range boot.Components {
		currentComponent, ok := current.Components[name]
		if !ok || !bootComponent.Equal(currentComponent) {
			return true, nil
		}
	}
	return false, nil
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

	// Wait for goroutines to finish with timeout BEFORE stopping watchers.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine if workers are still reading.
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

	// Stop all watchers after goroutines have exited
	for _, watcher := range cm.watchers {
		if watcher != nil {
			_ = watcher.Stop() // Ignore errors during shutdown
		}
	}

	cm.pendingMu.Lock()
	clear(cm.pendingLocal)
	cm.pendingMu.Unlock()

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
				cm.handleUpdate(entry.Key(), entry.Value(), entry.Revision())
			}
		}
	}
}

// handleUpdate applies external configuration and classifies exact local KV
// echoes per key. Bucket-wide watermarks are incorrect because an unrelated
// local write can otherwise hide a lower-revision external write.
func (cm *Manager) handleUpdate(key string, value []byte, revision uint64) {
	// Check if we're shutting down
	if cm.stopped.Load() {
		return
	}

	if cm.classifyLocalEcho(key, value, revision) {
		return
	}
	if err := cm.updateConfig(key, value); err != nil {
		cm.logger.Error("Failed to update configuration", "key", key, "error", err)
	}
}

func (cm *Manager) classifyLocalEcho(key string, value []byte, revision uint64) bool {
	cm.pendingMu.Lock()
	defer cm.pendingMu.Unlock()
	pending, ok := cm.pendingLocal[key]
	if !ok {
		return false
	}
	if pending.delete {
		if len(value) == 0 {
			delete(cm.pendingLocal, key)
		}
		return true
	}
	if revision < pending.revision {
		return true
	}
	delete(cm.pendingLocal, key)
	if revision == pending.revision {
		return true
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

	// Apply the update as a single serialized read-modify-write so a concurrent
	// mutation (the watcher goroutine vs a caller-goroutine PutComponentToKV /
	// DeleteComponentFromKV, or explicit diagram publication) cannot drop this
	// desired-state change (gh#515).
	// Returning errNoConfigChange from the mutation signals an ignored key without
	// swapping — surfaced as a nil error to the caller.
	err := cm.config.Mutate(func(currentConfig *Config) error {
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

		case "model_registry":
			if len(value) == 0 {
				currentConfig.ModelRegistry = nil
			} else {
				var registry model.Registry
				if err := json.Unmarshal(value, &registry); err != nil {
					return fmt.Errorf("parse model_registry config: %w", err)
				}
				currentConfig.ModelRegistry = &registry
			}

		// Graph and ObjectStore config moved to components

		default:
			// Unknown top-level key, ignore — no config change.
			return errNoConfigChange
		}
		return nil
	})
	if errors.Is(err, errNoConfigChange) {
		return nil
	}
	return err
}

// errNoConfigChange is returned by an updateConfig mutation for an ignored
// (unknown) key so the SafeConfig swap is skipped; updateConfig maps it to a nil
// error for the caller.
var errNoConfigChange = errors.New("no config change")

// sanitizeNATSKey replaces characters invalid in NATS keys with underscore s
// NATS key restrictions: no spaces, must use printable ASCII
func sanitizeNATSKey(key string) string {
	// Replace spaces and other problematic characters with underscore s
	// This preserves readability while ensuring NATS compatibility
	return strings.ReplaceAll(key, " ", "_")
}

// DeleteComponentFromKV deletes desired next-boot component configuration.
// The current process composition remains sealed until restart. The exact
// delete echo is classified per key so unrelated revisions cannot hide it.
func (cm *Manager) DeleteComponentFromKV(ctx context.Context, name string) error {
	if cm.detached.Load() {
		return detachedConfigWriteError("delete", fmt.Sprintf("component %q", name))
	}
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
	cm.pendingMu.Lock()
	defer cm.pendingMu.Unlock()
	if err := cm.kvStore.Delete(ctx, key); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			// Already gone from KV; ensure it is also gone from in-memory config.
			if aerr := cm.updateConfig(key, nil); aerr != nil {
				return fmt.Errorf("apply delete of component %s in memory: %w", name, aerr)
			}
			return nil
		}
		return fmt.Errorf("delete component %s from KV: %w", name, err)
	}
	// Apply the removal to the author's desired-state view synchronously so the
	// watcher event skips only this write's echo. The sealed boot map and running
	// ComponentManager remain unchanged until a fresh process starts.
	if err := cm.updateConfig(key, nil); err != nil {
		return fmt.Errorf("apply delete of component %s in memory: %w", name, err)
	}
	cm.pendingLocal[key] = pendingLocalWrite{delete: true}
	cm.logger.Debug("Deleted component from KV", "component", name, "key", key)
	return nil
}

// PutComponentToKV writes a single component's configuration to NATS KV.
// This is more efficient than PushToKV when only one component has changed,
// and avoids race conditions with KV watchers when multiple operations are in flight.
//
// It persists desired next-boot state before updating the author's in-memory
// view. The exact resulting revision is recorded per key so the watcher skips
// only this write's echo; unrelated external writes still apply. KV-write is
// first so a failed Put leaves in-memory state untouched.
func (cm *Manager) PutComponentToKV(ctx context.Context, name string, compConfig types.ComponentConfig) error {
	if cm.detached.Load() {
		return detachedConfigWriteError("write", fmt.Sprintf("component %q", name))
	}
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
	data, err := json.Marshal(compConfig)
	if err != nil {
		return fmt.Errorf("marshal component %s: %w", name, err)
	}
	rev, err := cm.putLocal(ctx, key, data, true)
	if err != nil {
		return fmt.Errorf("put component %s to KV: %w", name, err)
	}
	cm.logger.Debug("Put component to KV", "component", name, "key", key, "revision", rev)
	return nil
}

func (cm *Manager) putLocal(ctx context.Context, key string, data []byte, apply bool) (uint64, error) {
	cm.pendingMu.Lock()
	defer cm.pendingMu.Unlock()
	revision, err := cm.kvStore.Put(ctx, key, data)
	if err != nil {
		return 0, err
	}
	if apply {
		if err := cm.updateConfig(key, data); err != nil {
			return 0, err
		}
	}
	cm.pendingLocal[key] = pendingLocalWrite{revision: revision}
	return revision, nil
}

// PushToKV pushes the current configuration to NATS KV
// This is useful for initial setup or config synchronization
func (cm *Manager) PushToKV(ctx context.Context) error {
	if cm.detached.Load() {
		return detachedConfigWriteError("push", "configuration")
	}
	cfg := cm.config.Get()

	// Push version first
	cm.logger.Debug("PushToKV: checking version", "version", cfg.Version)
	if cfg.Version != "" {
		data, err := json.Marshal(cfg.Version)
		if err != nil {
			return fmt.Errorf("marshal version: %w", err)
		}
		cm.logger.Debug("Pushing version to KV", "version", cfg.Version)
		_, err = cm.putLocal(ctx, "version", data, false)
		if err != nil {
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
		_, err = cm.putLocal(ctx, key, data, false)
		if err != nil {
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
		_, err = cm.putLocal(ctx, key, data, false)
		if err != nil {
			return fmt.Errorf("push component %s: %w", name, err)
		}
	}

	// Platform
	if data, err := json.Marshal(cfg.Platform); err == nil && len(data) > 2 { // > 2 to skip empty {}
		_, err := cm.putLocal(ctx, "platform", data, false)
		if err != nil {
			return fmt.Errorf("push platform: %w", err)
		}
	}

	// NATS
	if data, err := json.Marshal(cfg.NATS); err == nil && len(data) > 2 {
		_, err := cm.putLocal(ctx, "nats", data, false)
		if err != nil {
			return fmt.Errorf("push nats: %w", err)
		}
	}

	// Model Registry
	if cfg.ModelRegistry != nil {
		if data, err := json.Marshal(cfg.ModelRegistry); err == nil && len(data) > 2 {
			_, err := cm.putLocal(ctx, "model_registry", data, false)
			if err != nil {
				return fmt.Errorf("push model_registry: %w", err)
			}
		}
	}

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

// kvPlatformIdentity reads the stored platform identity from the KV `platform`
// key. Returns found=false when the key is absent or unparseable (an old config
// format, or a bucket written before platform identity was populated), in which
// case the caller must not treat the bucket as identity-mismatched.
func (cm *Manager) kvPlatformIdentity(ctx context.Context) (PlatformConfig, bool) {
	entry, err := cm.kv.Get(ctx, "platform")
	if err != nil {
		return PlatformConfig{}, false
	}
	var p PlatformConfig
	if err := json.Unmarshal(entry.Value(), &p); err != nil {
		cm.logger.Warn("Failed to parse platform identity from KV", "error", err)
		return PlatformConfig{}, false
	}
	return p, true
}

// platformHasIdentity reports whether a platform config carries a discriminating
// identity (org or id). An identity-less config cannot be told apart from
// another, so the cross-app guard does not fire on it.
func platformHasIdentity(p PlatformConfig) bool {
	return p.Org != "" || p.ID != ""
}

// platformIdentityKey is the identity tuple used to compare two platform
// configs for the cross-app config-bleed guard (gh#459). Environment is
// included so two instances of the same org+id but different environments
// (prod vs dev) sharing one NATS are also treated as distinct. A NUL
// separator (illegal in every segment) is used so the join is unambiguous —
// {org:"a",id:"b.c"} and {org:"a.b",id:"c"} must not collide.
func platformIdentityKey(p PlatformConfig) string {
	return p.Org + "\x00" + p.ID + "\x00" + p.Environment
}

// syncFromKV loads all configuration from KV and applies it
func (cm *Manager) syncFromKV(ctx context.Context) error {
	// List all keys
	keys, err := cm.kv.Keys(ctx)
	if err != nil {
		return fmt.Errorf("list KV keys: %w", err)
	}

	// Existing version arbitration selected KV. Services are whole-entry
	// desired next-boot state, so current services.* keys replace the file map
	// instead of overlaying it. Other top-level sections retain their existing
	// synchronization behavior below.
	if err := cm.config.Mutate(func(current *Config) error {
		current.Services = make(types.ServiceConfigs)
		return nil
	}); err != nil {
		return fmt.Errorf("reset services before KV sync: %w", err)
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
