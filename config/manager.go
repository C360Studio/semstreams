package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
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
	config *SafeConfig // Current configuration

	// bucketName is this deployment's configuration bucket, derived once at
	// construction by BucketName from the DECLARED pair — before Start can
	// replace platform.id with the minted identifier, which lives inside the
	// bucket and so can never name it (#1188). Resolved through the catalog's
	// configuration family, never spelled locally.
	bucketName string

	// natsClient is retained so the configuration bucket is acquired
	// under Start's context rather than one the constructor invented. The
	// Manager retains the CLIENT, never a context (repository hard rule).
	natsClient *natsclient.Client

	// bucketMu guards kv/kvStore, which are nil until Start acquires them.
	bucketMu sync.RWMutex
	kv       jetstream.KeyValue  // NATS KV bucket for config
	kvStore  *natsclient.KVStore // KVStore abstraction for safe operations

	watchers    []jetstream.KeyWatcher   // Watchers for specific patterns
	families    []*KeyFamily             // Key families registered by WithKeyFamily
	subscribers map[string][]chan Update // Pattern -> channels
	mu          sync.RWMutex             // Protects subscribers map
	logger      *slog.Logger             // Structured logger

	// Lifecycle management
	shutdownCh chan struct{}  // Signal shutdown to goroutines
	wg         sync.WaitGroup // Track all goroutines
	stopped    atomic.Bool    // Indicates manager is stopped

	// engineHighWaterRev is the highest KV revision the Manager has
	// produced via its own write methods (PutComponentToKV,
	// DeleteComponentFromKV, PushToKV). The watcher's handleUpdate
	// skips events whose revision is <= this watermark, because
	// those events come from in-process engine writes that have
	// ALREADY been applied synchronously to in-memory state.
	//
	// Without this guard, the watcher's async processing of queued
	// KV events can override the Manager's recent in-memory desired-state
	// writes — for example, an older PUT processed after a later DELETE can
	// reinsert a component into the next-boot configuration view.
	//
	// NATS KV revisions are bucket-monotonic, so a single per-bucket
	// watermark is sufficient. External writers (UI, other processes)
	// produce events at revisions strictly greater than the watermark
	// at the time they wrote, so their events apply normally.
	engineHighWaterRev atomic.Uint64
}

// platformIdentityKVKey is the key in the configuration bucket holding
// the deployment's durable platform identity (ADR-104).
//
// It is NOT configuration. It is created once with an atomic Create, never
// written by PushToKV, never applied by syncFromKV or updateConfig, never
// watched, and never counted as configuration by first-boot detection.
const platformIdentityKVKey = "platform_identity"

// platformIdentityRecord says which platform authority a configuration bucket
// belongs to. Its shape is a cross-repo contract (ADR-104): adopters without Go
// bindings read this record to learn the pair the deployment actually mints
// under, so it carries exactly these three fields.
type platformIdentityRecord struct {
	// Org is the deployment's platform.org, which minting never changes.
	Org string `json:"org"`
	// Stem is the platform.id the configuration document declared.
	Stem string `json:"stem"`
	// ID is the effective platform.id — the stem plus the minted entropy
	// suffix, or the stem itself when an operator pre-created the record.
	ID string `json:"id"`
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(cfg *Config, natsClient *natsclient.Client, logger *slog.Logger, opts ...ManagerOption) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if natsClient == nil {
		return nil, fmt.Errorf("nats client cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// No I/O here, and no context. The configuration bucket is acquired
	// in Start(ctx), under the caller's lifecycle context: this constructor used
	// to invent a context.Background() root for CreateKeyValueBucket, which the
	// repository hard rule forbids and which left the caller's cancellation
	// unable to bound the acquisition. It became removal work rather than
	// inherited debt the moment this package made that bucket the home of
	// create-once identity state.
	bucketName, err := BucketName(cfg.Platform.Org, cfg.Platform.ID)
	if err != nil {
		return nil, fmt.Errorf("name the configuration bucket: %w", err)
	}
	cm := &Manager{
		bucketName:  bucketName,
		config:      NewSafeConfig(cfg),
		natsClient:  natsClient,
		subscribers: make(map[string][]chan Update),
		logger:      logger,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("config manager option cannot be nil")
		}
		opt(cm)
	}
	for _, family := range cm.families {
		if family == nil {
			return nil, fmt.Errorf("key family cannot be nil")
		}
		if managerOwnedKeyPrefixes[family.prefix] {
			return nil, fmt.Errorf(
				"key family prefix %q is a key the config manager owns; a registered family must use its own prefix",
				family.prefix)
		}
	}
	return cm, nil
}

// managerWatchPatterns are the keys the Manager watches (2-part keys only);
// * is a single-level wildcard, so property-level keys are excluded.
var managerWatchPatterns = []string{
	"services.*",     // Matches services.metrics but NOT services.metrics.enabled
	"components.*",   // Matches components.udp but NOT components.udp.port
	"platform",       // Single key
	"nats",           // Single key
	"model_registry", // Single key
}

// managerOwnedKeyPrefixes are the first key tokens of the Manager's own keys:
// its watch patterns and the identity record. A registered key family may not
// claim one, so a family holder cannot reach them.
var managerOwnedKeyPrefixes = func() map[string]bool {
	owned := map[string]bool{platformIdentityKVKey: true}
	for _, pattern := range managerWatchPatterns {
		owned[strings.SplitN(pattern, ".", 2)[0]] = true
	}
	return owned
}()

// errBucketNotAcquired is returned by any bucket-dependent method called before
// Start. Fail closed and say why: before Start the Manager has no bucket, and a
// nil-dereference panic would say nothing at all.
var errBucketNotAcquired = errors.New(
	"config manager has no configuration bucket yet: it is acquired by Start(ctx), which must run first")

// acquireBucket obtains the configuration bucket under the CALLER's
// context and refuses a policy that could silently delete what this package
// mints into it.
func (cm *Manager) acquireBucket(ctx context.Context) (jetstream.KeyValue, *natsclient.KVStore, error) {
	// Through the catalog's owner seam, not a direct create. The descriptor —
	// not this function — declares History 5 and the strict no-lifecycle
	// retention that refuses an evicting policy rather than repairing one,
	// because the identity such a policy could already have deleted is
	// create-once (ADR-104; ADR-102 decision 7). This is the bucket's only
	// acquisition: its name is derived from the declared pair only this
	// Manager holds, and the rule ConfigManager writes rules.* through the key
	// family this Manager serves it (#1188).
	kv, err := graph.EnsureCatalogBucket(ctx, cm.natsClient, cm.bucketName)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire config bucket %q: %w", cm.bucketName, err)
	}
	return kv, cm.natsClient.NewKVStore(kv), nil
}

// publishBucket makes the acquired handles visible to the EXPORTED writers.
//
// It runs once, at the very end of a successful Start, and that placement is
// the contract: until it runs, PushToKV, PutComponentToKV and
// DeleteComponentFromKV all return errBucketNotAcquired. A Start that refused —
// a foreign identity, a pre-identity bucket, a malformed record, a watcher
// that would not open — therefore leaves no armed
// writer behind. Publishing at acquisition instead let a caller overwrite the
// very bucket Start had just refused as another platform's, which is the
// detached running mode component-runtime-config says does not exist.
func (cm *Manager) publishBucket(kv jetstream.KeyValue, kvStore *natsclient.KVStore) {
	cm.bucketMu.Lock()
	cm.kv = kv
	cm.kvStore = kvStore
	cm.bucketMu.Unlock()
	for _, family := range cm.families {
		family.bind(kvStore, cm.logger)
	}
}

// store returns the acquired KVStore, or errBucketNotAcquired before Start.
func (cm *Manager) store() (*natsclient.KVStore, error) {
	cm.bucketMu.RLock()
	defer cm.bucketMu.RUnlock()
	if cm.kvStore == nil {
		return nil, errBucketNotAcquired
	}
	return cm.kvStore, nil
}

// bucket returns the acquired KV handle, or errBucketNotAcquired before Start.
func (cm *Manager) bucket() (jetstream.KeyValue, error) {
	cm.bucketMu.RLock()
	defer cm.bucketMu.RUnlock()
	if cm.kv == nil {
		return nil, errBucketNotAcquired
	}
	return cm.kv, nil
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
// Use this for external library consumers that deliberately maintain a live
// model-registry view. SemStreams components receive the registry selected at
// boot; later writes are durable desired state for the next process start and
// do not restart or rewire the running ComponentManager.
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
	// Reject a nil context BEFORE any state is mutated or NATS is touched.
	// Moving bucket acquisition here made Start this package's exported
	// context-taking boundary, and the hard rule says such a boundary rejects
	// nil when it can return an error. It is not a theoretical rule here: nil
	// reaches JetStream's wrapContextWithoutDeadline, which calls Deadline() on
	// the nil interface and panics — after shutdownCh had already been replaced.
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigManager", "Start", "context cannot be nil")
	}

	// Initialize shutdown channel
	cm.shutdownCh = make(chan struct{})

	// Acquire the configuration bucket under THIS context, and refuse a
	// policy that could evict the identity established below.
	//
	// The handles stay LOCAL through every step of Start that can refuse. They
	// reach the struct — and with it the exported writers — only at the end,
	// through publishBucket.
	kvHandle, kvStore, err := cm.acquireBucket(ctx)
	if err != nil {
		return err
	}

	// Establish this deployment's platform identity BEFORE arbitration,
	// watchers, or writes (ADR-104). The same single read of the bucket's keys
	// answers first-boot detection, so there is no second probe to disagree
	// with it.
	hasConfig, err := cm.establishPlatformIdentity(ctx, kvStore)
	if err != nil {
		return err
	}

	if !hasConfig {
		// First boot: push file config to KV for UI
		cm.logger.Info("First boot detected, pushing config to KV")
		if err := cm.pushToKV(ctx, kvStore); err != nil {
			cm.logger.Error("Failed to push initial config to KV", "error", err)
			// Continue anyway - UI won't have initial state but app can run
		}
	} else {
		// Subsequent boot: compare versions to decide sync direction
		fileVersion := cm.config.Get().Version
		kvVersion, err := cm.getKVVersion(ctx, kvHandle)
		if err != nil {
			cm.logger.Warn("Failed to get KV version, syncing from KV", "error", err)
			// Fall back to syncing from KV if we can't get version
			if err := cm.syncFromKV(ctx, kvHandle); err != nil {
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
				if err := cm.syncFromKV(ctx, kvHandle); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			} else if cmp > 0 {
				// File version is newer: update KV from file
				cm.logger.Info("File version is newer than KV, updating KV",
					"file_version", fileVersion,
					"kv_version", kvVersion)
				if err := cm.pushToKV(ctx, kvStore); err != nil {
					cm.logger.Error("Failed to update KV with newer config", "error", err)
				}
			} else if cmp < 0 {
				// KV version is newer: warn and use KV
				cm.logger.Warn("File version is older than KV, using KV config",
					"file_version", fileVersion,
					"kv_version", kvVersion,
					"hint", "bump file version to update KV")
				if err := cm.syncFromKV(ctx, kvHandle); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			} else {
				// Versions equal: sync from KV (UI may have made changes)
				cm.logger.Debug("File and KV versions match, syncing from KV",
					"version", fileVersion)
				if err := cm.syncFromKV(ctx, kvHandle); err != nil {
					cm.logger.Warn("Failed to sync from KV on startup", "error", err)
				}
			}
		}
	}

	patterns := managerWatchPatterns

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
		watcher, err := kvHandle.Watch(ctx, pattern, jetstream.UpdatesOnly())
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

	// A registered key family is a declared dependency, not an optional
	// pattern: its owner has no other way to reach this bucket, so a family
	// that cannot be watched refuses Start. Opened WITHOUT UpdatesOnly, so the
	// owner receives the entries present now, then every change.
	familyWatchers := make([]jetstream.KeyWatcher, 0, len(cm.families))
	for _, family := range cm.families {
		watcher, err := kvHandle.Watch(ctx, family.pattern())
		if err != nil {
			for _, w := range familyWatchers {
				_ = w.Stop()
			}
			cleanup()
			return fmt.Errorf("watch key family %q in config bucket %q: %w", family.pattern(), cm.bucketName, err)
		}
		familyWatchers = append(familyWatchers, watcher)
	}

	// Every step that can refuse has now passed. Publishing the handles here,
	// and only here, is what makes errBucketNotAcquired truthful for a Start
	// that was attempted and refused — not merely for one never called.
	cm.publishBucket(kvHandle, kvStore)

	// Process updates from all watchers in background
	for _, watcher := range cm.watchers {
		cm.wg.Add(1)
		go cm.processWatcher(ctx, watcher)
	}
	for i, family := range cm.families {
		cm.watchers = append(cm.watchers, familyWatchers[i])
		cm.wg.Add(1)
		go cm.processFamily(ctx, family, familyWatchers[i])
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
// overwrite more recent desired-state writes when the watcher is lagging
// behind a rapid PUT/DELETE sequence.
//
// Subscribers are notified for BOTH Manager-owned and external events. The
// skip suppresses only the in-memory re-apply, never durable desired-state
// observation. A delete at revision N followed by a later Manager PUT can
// raise the high-water above N; observers must still see that delete even
// though no running component is reconciled from it.
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
	// notify subscribers below so durable desired-state observers see it.
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

		// The KV `platform` key is a PUBLISHED MIRROR, never a source. It is
		// pushed for readers (the UI) and deliberately has no case here:
		// applying it would unmarshal a foreign or stale block — platform.ID
		// included — straight over the authority every identity this process
		// mints is composed from, after Start established it (ADR-104).
		// Unknown keys fall through to the default and change nothing, while
		// subscribers are still notified.

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

// DeleteComponentFromKV removes a component from durable next-boot desired
// state. PushToKV only puts keys that exist in memory; it does not delete keys
// that are absent.
//
// The removal is also applied synchronously to the Manager's current
// desired-state view. It does not tear down or otherwise change the running
// ComponentManager; composition changes only on process restart. The NATS API
// does not expose the Delete revision, so this path cannot bump the watermark.
// Its watcher event either reapplies the idempotent delete or skips a redundant
// reapply after a later Manager write, while still notifying observers.
func (cm *Manager) DeleteComponentFromKV(ctx context.Context, name string) error {
	kvStore, err := cm.store()
	if err != nil {
		return err
	}
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
	if err := kvStore.Delete(ctx, key); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			// Already gone from KV; ensure it is also gone from in-memory config.
			if aerr := cm.updateConfig(key, nil); aerr != nil {
				return fmt.Errorf("apply delete of component %s in memory: %w", name, aerr)
			}
			return nil
		}
		return fmt.Errorf("delete component %s from KV: %w", name, err)
	}
	// Apply the removal to the desired-state view synchronously. updateConfig
	// with an empty value deletes the component from the in-memory map; the
	// running component set remains unchanged.
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
// The method performs write KV → apply desired state in memory → bump watermark.
// It records a component candidate for the next process start; it does not add
// or restart a component in the running ComponentManager. The revision returned
// by KV.Put lets handleUpdate skip the redundant reapply while still notifying
// desired-state observers. KV-write is first so a failed Put leaves memory
// untouched.
func (cm *Manager) PutComponentToKV(ctx context.Context, name string, compConfig types.ComponentConfig) error {
	key := fmt.Sprintf("components.%s", sanitizeNATSKey(name))
	kvStore, err := cm.store()
	if err != nil {
		return err
	}
	data, err := json.Marshal(compConfig)
	if err != nil {
		return fmt.Errorf("marshal component %s: %w", name, err)
	}
	rev, err := kvStore.Put(ctx, key, data)
	if err != nil {
		return fmt.Errorf("put component %s to KV: %w", name, err)
	}
	// Apply the next-boot desired state synchronously; the watcher can skip its
	// redundant reapply while the current runtime remains unchanged.
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
	kvStore, err := cm.store()
	if err != nil {
		return err
	}
	return cm.pushToKV(ctx, kvStore)
}

// pushToKV is PushToKV over an explicit handle, so Start can publish
// configuration while the handles are still private.
func (cm *Manager) pushToKV(ctx context.Context, kvStore *natsclient.KVStore) error {
	cfg := cm.config.Get()

	// Push version first
	cm.logger.Debug("PushToKV: checking version", "version", cfg.Version)
	if cfg.Version != "" {
		data, err := json.Marshal(cfg.Version)
		if err != nil {
			return fmt.Errorf("marshal version: %w", err)
		}
		cm.logger.Debug("Pushing version to KV", "version", cfg.Version)
		rev, err := kvStore.Put(ctx, "version", data)
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
		rev, err := kvStore.Put(ctx, key, data)
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
		rev, err := kvStore.Put(ctx, key, data)
		if err != nil {
			return fmt.Errorf("push component %s: %w", name, err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// Platform
	if data, err := json.Marshal(cfg.Platform); err == nil && len(data) > 2 { // > 2 to skip empty {}
		rev, err := kvStore.Put(ctx, "platform", data)
		if err != nil {
			return fmt.Errorf("push platform: %w", err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// NATS
	if data, err := json.Marshal(cfg.NATS); err == nil && len(data) > 2 {
		rev, err := kvStore.Put(ctx, "nats", data)
		if err != nil {
			return fmt.Errorf("push nats: %w", err)
		}
		cm.bumpEngineHighWater(rev)
	}

	// Model Registry
	if cfg.ModelRegistry != nil {
		if data, err := json.Marshal(cfg.ModelRegistry); err == nil && len(data) > 2 {
			rev, err := kvStore.Put(ctx, "model_registry", data)
			if err != nil {
				return fmt.Errorf("push model_registry: %w", err)
			}
			cm.bumpEngineHighWater(rev)
		}
	}

	// After bulk push, notify durable desired-state observers.
	// Individual KV watcher notifications may be dropped when the subscriber
	// channel (buffer=1) is full during rapid successive puts.
	cm.notifySubscribers("components.*")

	return nil
}

// notifySubscribers sends a synthetic update to all subscribers matching the
// given path. This is used after bulk operations such as PushToKV to preserve
// desired-state observation when individual per-key notifications were dropped.
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
				// Drain any stale notification so the latest observation signal is
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

// establishPlatformIdentity establishes the deployment's effective platform.id
// from the bucket's identity record, before arbitration, watchers, or writes,
// and answers first-boot detection from the SAME single read (ADR-104).
//
// Three branches, one read:
//
//	record present           adopt it, refusing a foreign org or a file whose
//	                         platform.id is not the record's stem
//	record absent, no other  a genuine first boot: mint the suffix and Create
//	record absent, others    the bucket predates identity minting: refuse,
//	                         minting nothing and creating nothing
//
// It returns whether the bucket already holds CONFIGURATION — every key except
// the identity record. Counting the record would make a boot that has just
// created it look like a subsequent boot: it would skip the initial PushToKV,
// and syncFromKV would then reset the in-memory service map from a bucket that
// holds nothing to repopulate it with.
func (cm *Manager) establishPlatformIdentity(ctx context.Context, kvStore *natsclient.KVStore) (bool, error) {
	keys, err := kvStore.Keys(ctx)
	if err != nil {
		// Fail closed: a bucket that cannot be read is a bucket that must not
		// be minted into. Guessing "first boot" here would Create a second
		// authority for a deployment that already has one.
		return false, fmt.Errorf("read config bucket %q to establish platform identity: %w", cm.bucketName, err)
	}

	recordPresent := false
	configKeys := 0
	for _, key := range keys {
		switch key {
		case platformIdentityKVKey:
			recordPresent = true
		default:
			configKeys++
		}
	}

	switch {
	case recordPresent:
		return configKeys > 0, cm.adoptPlatformIdentity(ctx, kvStore)
	case configKeys > 0:
		declared := cm.config.Get().Platform
		return false, fmt.Errorf(
			"config bucket %q holds %d configuration key(s) (%s) but no %q record, so nothing was minted and nothing was written. "+
				"This package is the bucket's only writer until its first Start succeeds — rules.* arrive only through the "+
				"key family it serves — so the keys were written some other way: the bucket predates framework-minted "+
				"platform identity (ADR-104), or something wrote it by hand. "+
				"Provision fresh NATS storage for this deployment — ADR-102 decision 7 forbids rewriting a minted "+
				"authority — or, to adopt the pair this configuration declares, pre-create %q as "+
				"{\"org\":%q,\"stem\":%q,\"id\":%q}",
			cm.bucketName, configKeys, summarizeKeys(keys), platformIdentityKVKey,
			platformIdentityKVKey, declared.Org, declared.ID, declared.ID,
		)
	default:
		return false, cm.mintPlatformIdentity(ctx, kvStore)
	}
}

// summarizeKeys renders at most a handful of bucket keys so the refusal above
// says WHICH keys were found. "3 configuration keys" sends an operator to go
// looking; "rules.foo, platform, version" tells them which writer got there
// first, which is the difference between the two causes the message names.
func summarizeKeys(keys []string) string {
	const shown = 5
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	if len(sorted) <= shown {
		return strings.Join(sorted, ", ")
	}
	return strings.Join(sorted[:shown], ", ") + fmt.Sprintf(", and %d more", len(sorted)-shown)
}

// mintPlatformIdentity mints the entropy suffix on a genuine first boot and
// records it once. Create, not Put: two co-processes booting against one bucket
// must converge on ONE authority, and ADR-102 decision 7 forbids the rewrite
// that would repair a split one. The loser of the race adopts the winner's.
func (cm *Manager) mintPlatformIdentity(ctx context.Context, kvStore *natsclient.KVStore) error {
	declared := cm.config.Get().Platform
	if declared.Org == "" || declared.ID == "" {
		// There is no authority to suffix. Config.Validate requires both, so
		// this is an unvalidated configuration reaching Start; minting a
		// half-empty pair would durably record an authority no identity can be
		// composed under.
		return fmt.Errorf(
			"cannot mint platform identity: platform.org=%q platform.id=%q — both are required (ADR-102) and the configuration reaching Start was never validated",
			declared.Org, declared.ID,
		)
	}
	if err := cm.refuseDeclaredMintedIdentifier(ctx, declared.Org, declared.ID); err != nil {
		return err
	}
	suffix, mintErr := mintIdentitySuffix()
	if mintErr != nil {
		return fmt.Errorf("mint platform identity suffix: %w", mintErr)
	}
	record := platformIdentityRecord{Org: declared.Org, Stem: declared.ID, ID: declared.ID + "-" + suffix}

	// Bound the EFFECTIVE value before it becomes durable, against the
	// family-table budget and not the declarable one — the suffix is already on
	// it. Load reserved these seven bytes at the declaration boundary, so a pair
	// that loaded cannot fail here; this is what makes "no record is ever
	// created that a later boot rejects" a local property of this function
	// rather than an argument about a distant check.
	if err := validateAuthorityPair(record.Org, record.ID); err != nil {
		return fmt.Errorf("minted platform identity %q is not a usable authority: %w", record.ID, err)
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal platform identity record: %w", err)
	}
	if _, err := kvStore.Create(ctx, platformIdentityKVKey, data); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyExists) {
			return cm.adoptPlatformIdentity(ctx, kvStore)
		}
		return fmt.Errorf("create platform identity record: %w", err)
	}

	cm.logger.Info("Minted platform identity",
		"org", record.Org, "stem", record.Stem, "platform", record.ID)
	return cm.applyEffectivePlatformID(record.ID)
}

// refuseDeclaredMintedIdentifier keeps ADR-104 decision 5 on the mint branch.
//
// The bucket is named by the declared pair, so a configuration that declares
// an identifier minted from stem s names the empty bucket
// semstreams_config_<org>_<s-xxxxxx> and would silently mint a second
// authority there — unrepairable under ADR-102 decision 7. Before minting, it
// reads the identity record of every other bucket in the org's family and
// refuses with the d5 guidance when one recorded this org and exactly the
// declared value as its minted identifier. That is a comparison against stored
// values, never a reading of the declared string's shape (owner ruling on
// #1188, Q4 (b)).
//
// Bounded: one bucket listing and one Get per sibling bucket, under the
// caller's context, with no retries. The prefix is over-inclusive — an org may
// itself contain `_` — which is harmless because the recorded org and id are
// compared, not the bucket name. A sibling that vanishes between the listing
// and its read, or holds no record, is skipped; any other read failure fails
// closed, because guessing "no match" here mints a second authority.
func (cm *Manager) refuseDeclaredMintedIdentifier(ctx context.Context, org, declaredID string) error {
	names, err := cm.natsClient.ListKeyValueBuckets(ctx)
	if err != nil {
		return fmt.Errorf("list configuration buckets before minting platform identity: %w", err)
	}
	prefix := graph.BucketSemStreamsConfig + "_" + org + "_"
	for _, name := range names {
		if name == cm.bucketName || !strings.HasPrefix(name, prefix) {
			continue
		}
		record, found, err := cm.readSiblingIdentity(ctx, name)
		if err != nil {
			return err
		}
		if found && record.Org == org && record.ID == declaredID {
			return fmt.Errorf(
				"config bucket %q records platform identity %q, minted from stem %q, and this configuration declares the minted identifier: "+
					"declare the stem %q, not the minted identifier %q — the framework composes the effective value and records it there",
				name, record.ID, record.Stem, record.Stem, record.ID,
			)
		}
	}
	return nil
}

// readSiblingIdentity reads one other bucket's identity record, reporting
// found=false when the bucket or the record is gone.
func (cm *Manager) readSiblingIdentity(ctx context.Context, bucket string) (platformIdentityRecord, bool, error) {
	var record platformIdentityRecord
	// The catalog's reader seam: must-exist, never creates, never reconciles a
	// bucket this Manager does not own.
	kv, err := graph.OpenCatalogReader(ctx, cm.natsClient, bucket)
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) && classified.Code == natsclient.ErrorCodeBucketNotReady {
		return record, false, nil
	}
	if err != nil {
		return record, false, fmt.Errorf("open config bucket %q before minting platform identity: %w", bucket, err)
	}
	entry, err := kv.Get(ctx, platformIdentityKVKey)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return record, false, nil
	}
	if err != nil {
		return record, false, fmt.Errorf("read %q of config bucket %q before minting platform identity: %w",
			platformIdentityKVKey, bucket, err)
	}
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return record, false, fmt.Errorf("parse %q of config bucket %q before minting platform identity: %w",
			platformIdentityKVKey, bucket, err)
	}
	return record, true, nil
}

// mintIdentitySuffix returns the six lowercase hex bytes of the entropy suffix.
func mintIdentitySuffix() (string, error) {
	raw := make([]byte, mintedSuffixBytes/2)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// adoptPlatformIdentity takes the recorded identifier as this process's
// effective platform.id. The comparison is its own — it does not depend on the
// gh#459 guard reading the KV `platform` config key, which #1188 retires.
func (cm *Manager) adoptPlatformIdentity(ctx context.Context, kvStore *natsclient.KVStore) error {
	entry, err := kvStore.Get(ctx, platformIdentityKVKey)
	if err != nil {
		return fmt.Errorf("read platform identity record %q: %w", platformIdentityKVKey, err)
	}
	var record platformIdentityRecord
	if err := json.Unmarshal(entry.Value, &record); err != nil {
		return fmt.Errorf("parse platform identity record %q: %w", platformIdentityKVKey, err)
	}
	if record.Org == "" || record.Stem == "" || record.ID == "" {
		return fmt.Errorf(
			"platform identity record %q is incomplete (org=%q stem=%q id=%q): every field is required; provision fresh NATS storage",
			platformIdentityKVKey, record.Org, record.Stem, record.ID,
		)
	}

	declared := cm.config.Get().Platform
	if record.Org != declared.Org || declared.ID != record.Stem {
		// Configuration declares the STEM — one kind of value, so the load
		// boundary's seven-byte reserve is never applied to something that
		// already carries the suffix. The full identifier is not a declarable
		// value: at the legal boundary a 163-byte stem mints to a 170-byte
		// identifier, which load would refuse before this code ever ran.
		//
		// When the file happens to hold exactly the identifier this bucket
		// recorded, say so. That is a comparison against a STORED value, not
		// grammar detection — ADR-104 forbids deciding "already minted" by
		// inspecting the shape of a string, and this decides nothing from the
		// shape.
		if record.Org == declared.Org && declared.ID == record.ID {
			return fmt.Errorf(
				"config bucket %q records platform identity %q, minted from stem %q, and this configuration declares the minted identifier: "+
					"declare the stem %q, not the minted identifier %q — the framework composes the effective value and records it here",
				cm.bucketName, record.ID, record.Stem, record.Stem, record.ID,
			)
		}
		// The bucket is named by the declared pair, so a record for another
		// pair is reachable only through an alias — `_` is legal inside both
		// parts — or a hand-written record (owner ruling on #1188, Q2 (a):
		// refused here, not prevented at naming).
		return fmt.Errorf(
			"config bucket platform identity mismatch: "+
				"this configuration declares org=%q platform.id=%q, but config bucket %q records org=%q stem=%q id=%q. "+
				"The bucket is named from the declared pair, so this happens only when two pairs alias to one name "+
				"(`_` is legal inside both parts: org \"a_b\" with platform.id \"c\" and org \"a\" with platform.id \"b_c\" "+
				"share a bucket) or when the record was written by hand. Declare a pair that does not alias, or provision "+
				"fresh NATS storage — ADR-102 decision 7 forbids rewriting a minted authority",
			declared.Org, declared.ID, cm.bucketName, record.Org, record.Stem, record.ID,
		)
	}

	// An adopted identifier is bounded and grammar-checked exactly as an
	// effective configured pair is: the record is operator-writable (it is the
	// knobless opt-out), so it is never trusted further than a configuration
	// value. The declarable reserve does not apply — nothing is minted onto an
	// adopted identifier.
	if err := validateAuthorityPair(record.Org, record.ID); err != nil {
		return fmt.Errorf("recorded platform identity %q/%q is not a usable authority: %w", record.Org, record.ID, err)
	}

	cm.logger.Info("Adopted platform identity",
		"org", record.Org, "stem", record.Stem, "platform", record.ID)
	return cm.applyEffectivePlatformID(record.ID)
}

// applyEffectivePlatformID makes the established identifier the authority every
// identity this process mints is composed under.
//
// Mutate re-validates the WHOLE configuration, which is how the effective pair
// gets bounded — and also means an unrelated defect anywhere in the document
// now surfaces at Start rather than wherever it used to. That finding leads the
// error: reading "apply effective platform identity: … streams: ordinary stream
// bounds are not declared" as an identity failure costs a reader the real
// cause, so the identity step names itself last.
func (cm *Manager) applyEffectivePlatformID(id string) error {
	if err := cm.config.Mutate(func(current *Config) error {
		current.Platform.ID = id
		return nil
	}); err != nil {
		return fmt.Errorf("%w (found while applying the established platform identity %q; Start validates the whole effective configuration)", err, id)
	}
	return nil
}

// getKVVersion retrieves the version from KV bucket
func (cm *Manager) getKVVersion(ctx context.Context, kvHandle jetstream.KeyValue) (string, error) {
	// Try to get version from KV
	entry, err := kvHandle.Get(ctx, "version")
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
func (cm *Manager) syncFromKV(ctx context.Context, kvHandle jetstream.KeyValue) error {
	// List all keys
	keys, err := kvHandle.Keys(ctx)
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
		entry, err := kvHandle.Get(ctx, key)
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
