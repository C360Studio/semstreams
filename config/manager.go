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
	"github.com/c360studio/semstreams/types"
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

	// detached is set when Start refuses to adopt config from a bucket
	// owned by a different platform identity (gh#459). In detached mode the
	// manager runs on its local file config and must not touch the shared KV
	// bucket at all — the write methods (PushToKV / PutComponentToKV /
	// DeleteComponentFromKV) no-op so a later operator-triggered flow deploy
	// cannot bleed the local app's components INTO the foreign bucket
	// (the reverse-direction of the adoption bug).
	detached atomic.Bool

	// engineHighWaterRev is the highest KV revision the Manager has
	// produced via its own write methods (PutComponentToKV,
	// DeleteComponentFromKV, PushToKV). The watcher's handleUpdate
	// skips events whose revision is <= this watermark, because
	// those events come from in-process engine writes that have
	// ALREADY been applied synchronously to in-memory state.
	//
	// Without this guard, the watcher's async processing of queued
	// KV events can override the engine's recent in-memory writes —
	// e.g. a stale Stop PUT (Enabled=false) processed after Undeploy's
	// DELETE re-inserts the component into the in-memory map.
	//
	// NATS KV revisions are bucket-monotonic, so a single per-bucket
	// watermark is sufficient. External writers (UI, other processes)
	// produce events at revisions strictly greater than the watermark
	// at the time they wrote, so their events apply normally.
	engineHighWaterRev atomic.Uint64
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

// WatchModelRegistry returns a channel that emits the latest
// *model.Registry whenever the model_registry KV key changes. The
// channel is buffered (cap 1); slow consumers see the most recent
// registry on their next read — intermediate updates coalesce.
//
// Use this for external library consumers that hold their own
// *model.Registry alongside the semstreams runtime and need to refresh
// it on KV change. semstreams components do NOT need this — they're
// restarted by ComponentManager when their factory declares
// component.DepModelRegistry.
//
// See model.Watch for a one-line consumer pattern.
//
// The channel closes when the manager Stop()s.
func (cm *Manager) WatchModelRegistry() <-chan *model.Registry {
	in := cm.OnChange("model_registry")
	out := make(chan *model.Registry, 1)

	cm.wg.Add(1)
	go func() {
		defer cm.wg.Done()
		defer close(out)
		for u := range in {
			if cm.stopped.Load() {
				return
			}
			cfg := u.Config.Get()
			// Coalesce: if a previous registry is still pending in the
			// buffer, drop it in favor of the latest. Keeps slow
			// consumers from staring at stale state.
			select {
			case <-out:
			default:
			}
			select {
			case out <- cfg.ModelRegistry:
			default:
				// Should not happen since we just drained, but be
				// defensive against concurrent reader.
			}
		}
	}()

	return out
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
				// Versions equal: sync from KV (UI may have made changes)
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
				cm.handleUpdate(entry.Key(), entry.Value(), entry.Revision())
			}
		}
	}
}

// handleUpdate processes a single configuration update.
//
// For an engine-owned revision (revision <= engineHighWaterRev) the
// in-memory RE-APPLY is skipped — those events were generated by the
// Manager's own write methods (PutComponentToKV, DeleteComponentFromKV,
// PushToKV), which already applied the change to in-memory state
// synchronously, so re-applying would (a) be redundant and (b) can
// overwrite more recent engine writes when the watcher is lagging behind
// a burst of operations (the Stop-then-Undeploy race surfaced by
// TestEngineIntegrationSuite/TestFullLifecycle).
//
// Subscribers are notified for BOTH engine-owned and external events —
// the skip suppresses only the in-memory re-apply, never the
// notification. Suppressing the notification would silently drop a
// runtime add/remove reconcile (gh#388): e.g. a delete at revision N
// followed by a later engine PUT raising the high-water above N leaves
// the delete event engine-owned, and a subscriber (ComponentManager)
// must still be notified to tear the component down.
//
// External writes (UI, other processes) produce revisions strictly
// greater than the engine's watermark at the time they wrote, so they
// apply normally.
func (cm *Manager) handleUpdate(key string, value []byte, revision uint64) {
	// Check if we're shutting down
	if cm.stopped.Load() {
		return
	}

	// Skip the in-memory RE-APPLY for events produced by our own writes
	// (the engine already applied them synchronously; re-applying from the
	// watcher's queue can override more recent engine state) — but STILL
	// notify subscribers below so the reconcile fires (gh#388).
	engineOwned := revision != 0 && revision <= cm.engineHighWaterRev.Load()
	if engineOwned {
		cm.logger.Debug("Skipping in-memory re-apply for engine-owned revision (still notifying subscribers)",
			"key", key,
			"revision", revision,
			"high_water", cm.engineHighWaterRev.Load())
	} else {
		// Update internal configuration (external event).
		if err := cm.updateConfig(key, value); err != nil {
			cm.logger.Error("Failed to update configuration",
				"key", key,
				"error", err)
			return
		}
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

	// Apply the update as a single serialized read-modify-write so a concurrent
	// mutation (the watcher goroutine vs a caller-goroutine PutComponentToKV /
	// DeleteComponentFromKV, or an engine deploy) cannot drop this change (gh#515).
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

// DeleteComponentFromKV deletes a component's configuration from NATS KV.
// This should be called when a component is removed (e.g., during undeploy).
// PushToKV only puts keys that exist in memory - it doesn't delete removed keys.
//
// This method applies the removal to the in-memory config synchronously (the
// engine pattern), so a runtime caller invokes only this method to remove a
// component and the ComponentManager reconciles (tears down) it. The Delete
// generates a watcher event at a fresh revision the underlying API doesn't
// expose, so no watermark bump: the delete event arrives and is handled by
// handleUpdate — either external (applies the already-idempotent delete) or,
// when a later engine write has raised the high-water above it, engine-owned
// (skips the redundant re-apply but STILL notifies, gh#388). Either path
// reconciles the teardown.
func (cm *Manager) DeleteComponentFromKV(ctx context.Context, name string) error {
	if cm.detached.Load() {
		cm.logger.Warn("Config manager detached from foreign KV bucket; skipping component delete",
			"component", name)
		return nil
	}
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
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
	// Apply the removal in-memory synchronously so the (possibly engine-owned)
	// watcher event skips re-apply but the reconcile still tears down the
	// removed component (gh#388). updateConfig with an empty value deletes the
	// component from the in-memory map.
	if err := cm.updateConfig(key, nil); err != nil {
		return fmt.Errorf("apply delete of component %s in memory: %w", name, err)
	}
	cm.logger.Debug("Deleted component from KV", "component", name, "key", key)
	return nil
}

// bumpEngineHighWater raises the engine watermark to `rev` using a
// CAS loop. Used by every Manager write path that captures a KV
// revision (PutComponentToKV, PushToKV). The CAS-max pattern lets
// concurrent writers all converge to the highest observed revision
// without losing updates.
//
// Callers that don't know the revision (e.g. Delete, whose
// underlying API discards it) simply skip the bump — see the
// DeleteComponentFromKV doc-comment for why the watermark still
// produces the correct end-state even without tracking deletes.
func (cm *Manager) bumpEngineHighWater(rev uint64) {
	if rev == 0 {
		return
	}
	for {
		current := cm.engineHighWaterRev.Load()
		if rev <= current {
			return
		}
		if cm.engineHighWaterRev.CompareAndSwap(current, rev) {
			return
		}
	}
}

// PutComponentToKV writes a single component's configuration to NATS KV.
// This is more efficient than PushToKV when only one component has changed,
// and avoids race conditions with KV watchers when multiple operations are in flight.
//
// This method makes the engine pattern (write KV → apply in-memory → bump
// watermark) self-contained: it applies the component to the in-memory config
// synchronously, so a runtime caller invokes only this method to add a component
// and the ComponentManager reconciles (spawns) it. The revision returned by
// KV.Put is captured into engineHighWaterRev so the watcher's handleUpdate skips
// the redundant re-apply of the resulting event — but still notifies subscribers
// (gh#388). KV-write is first so a failed Put leaves in-memory state untouched.
func (cm *Manager) PutComponentToKV(ctx context.Context, name string, compConfig types.ComponentConfig) error {
	if cm.detached.Load() {
		cm.logger.Warn("Config manager detached from foreign KV bucket; skipping component write",
			"component", name)
		return nil
	}
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
	data, err := json.Marshal(compConfig)
	if err != nil {
		return fmt.Errorf("marshal component %s: %w", name, err)
	}
	rev, err := cm.kvStore.Put(ctx, key, data)
	if err != nil {
		return fmt.Errorf("put component %s to KV: %w", name, err)
	}
	// Apply in-memory synchronously so the (engine-owned) watcher event skips
	// re-apply but the reconcile still sees the added component (gh#388).
	if err := cm.updateConfig(key, data); err != nil {
		return fmt.Errorf("apply component %s in memory: %w", name, err)
	}
	cm.bumpEngineHighWater(rev)
	cm.logger.Debug("Put component to KV", "component", name, "key", key, "revision", rev)
	return nil
}

// PushToKV pushes the current configuration to NATS KV
// This is useful for initial setup or config synchronization
func (cm *Manager) PushToKV(ctx context.Context) error {
	if cm.detached.Load() {
		cm.logger.Warn("Config manager detached from foreign KV bucket; skipping config push")
		return nil
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
		rev, err := cm.kvStore.Put(ctx, "version", data)
		if err != nil {
			return fmt.Errorf("push version: %w", err)
		}
		cm.bumpEngineHighWater(rev)
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
		rev, err := cm.kvStore.Put(ctx, key, data)
		if err != nil {
			return fmt.Errorf("push service %s: %w", name, err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// Components
	for name, compConfig := range cfg.Components {
		key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
		data, err := json.Marshal(compConfig)
		if err != nil {
			return fmt.Errorf("marshal component %s: %w", name, err)
		}
		rev, err := cm.kvStore.Put(ctx, key, data)
		if err != nil {
			return fmt.Errorf("push component %s: %w", name, err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// Platform
	if data, err := json.Marshal(cfg.Platform); err == nil && len(data) > 2 { // > 2 to skip empty {}
		rev, err := cm.kvStore.Put(ctx, "platform", data)
		if err != nil {
			return fmt.Errorf("push platform: %w", err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// NATS
	if data, err := json.Marshal(cfg.NATS); err == nil && len(data) > 2 {
		rev, err := cm.kvStore.Put(ctx, "nats", data)
		if err != nil {
			return fmt.Errorf("push nats: %w", err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// Model Registry
	if cfg.ModelRegistry != nil {
		if data, err := json.Marshal(cfg.ModelRegistry); err == nil && len(data) > 2 {
			rev, err := cm.kvStore.Put(ctx, "model_registry", data)
			if err != nil {
				return fmt.Errorf("push model_registry: %w", err)
			}
			cm.bumpEngineHighWater(rev)
		}
	}

	// After bulk push, notify subscribers to reconcile.
	// Individual KV watcher notifications may be dropped when the subscriber
	// channel (buffer=1) is full during rapid successive puts.
	cm.notifySubscribers("components.*")

	return nil
}

// notifySubscribers sends a synthetic update to all subscribers matching the
// given path. This is used after bulk operations (like PushToKV) to trigger
// reconciliation, since individual per-key notifications may have been dropped.
func (cm *Manager) notifySubscribers(path string) {
	if cm.stopped.Load() {
		return
	}

	update := Update{
		Path:   path,
		Config: cm.config,
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for pattern, channels := range cm.subscribers {
		// Check both directions: the synthetic path may be a wildcard that matches
		// specific subscriber patterns, or subscriber patterns may be wildcards
		// that match the synthetic path.
		if cm.matchesPattern(path, pattern) || cm.matchesPattern(pattern, path) {
			for _, ch := range channels {
				if cm.stopped.Load() {
					return
				}
				// Drain any stale notification so the reconciliation signal is
				// guaranteed to be delivered. This is critical after bulk PushToKV
				// where individual per-key notifications may have filled the buffer.
				select {
				case <-ch:
				default:
				}
				ch <- update
			}
		}
	}
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
