package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// watchEntityStates creates KV watchers for entity state changes
func (rp *Processor) watchEntityStates(ctx context.Context) error {
	// Store the watcher context for dynamic management
	rp.mu.Lock()
	rp.watcherCtx, rp.watcherCancelFunc = context.WithCancel(ctx)
	watcherCtx := rp.watcherCtx
	rp.mu.Unlock()

	// Build effective bucket-to-patterns map
	bucketPatterns := rp.getEffectiveBucketPatterns()

	if len(bucketPatterns) == 0 {
		rp.logger.Info("No entity watch patterns configured, skipping KV watch setup")
		return nil
	}

	// Start watchers for all configured buckets and patterns
	for bucketName, patterns := range bucketPatterns {
		for _, pattern := range patterns {
			if err := rp.startWatcherForBucketPattern(watcherCtx, bucketName, pattern); err != nil {
				rp.logger.Warn("Failed to start watcher",
					"bucket", bucketName,
					"pattern", pattern,
					"error", err)
				// Continue with other patterns - don't fail completely
			}
		}
	}

	return nil
}

// getEffectiveBucketPatterns returns the bucket-to-patterns map to use.
// If EntityWatchBuckets is configured, use it directly.
// Otherwise, fall back to EntityWatchPatterns for ENTITY_STATES bucket (backwards compatibility).
func (rp *Processor) getEffectiveBucketPatterns() map[string][]string {
	if len(rp.config.EntityWatchBuckets) > 0 {
		return rp.config.EntityWatchBuckets
	}

	// Backwards compatibility: use EntityWatchPatterns for ENTITY_STATES.
	// Apply org suffix per ADR-032 tag 4 so the watcher reads the right bucket.
	if len(rp.config.EntityWatchPatterns) > 0 {
		return map[string][]string{
			component.BucketName(component.EntityStatesBucketName, rp.org): rp.config.EntityWatchPatterns,
		}
	}

	return nil
}

// getOrCreateBucket gets or creates a KV bucket by name.
// Uses appropriate defaults based on bucket purpose.
func (rp *Processor) getOrCreateBucket(ctx context.Context, bucketName string) (jetstream.KeyValue, error) {
	// Try to get existing bucket first
	bucket, err := rp.natsClient.GetKeyValueBucket(ctx, bucketName)
	if err == nil {
		return bucket, nil
	}

	// Bucket doesn't exist — only create the entity-states bucket (others should exist).
	// Compare against the org-suffixed name so that ENTITY_STATES_acme is also created
	// when the watcher resolves the suffixed name at startup.
	entityStatesBucket := component.BucketName(component.EntityStatesBucketName, rp.org)
	if bucketName == entityStatesBucket {
		return rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
			Bucket:      bucketName,
			Description: "Entity state storage",
			History:     10,
			TTL:         7 * 24 * time.Hour, // 7 days
			MaxBytes:    -1,                 // Unlimited
		})
	}

	// For other buckets (WORKFLOW_EXECUTIONS, AGENT_LOOPS), they should already exist
	// Return the error to indicate the bucket isn't available
	return nil, errs.WrapTransient(err, "Processor", "getOrCreateBucket", fmt.Sprintf("bucket %s not found", bucketName))
}

// getOrCreateEntityBucket gets or creates the ENTITY_STATES KV bucket.
// DEPRECATED: Use getOrCreateBucket for multi-bucket support.
func (rp *Processor) getOrCreateEntityBucket(ctx context.Context) (jetstream.KeyValue, error) {
	return rp.getOrCreateBucket(ctx, component.BucketName(component.EntityStatesBucketName, rp.org))
}

// startWatcherForPattern starts a KV watcher for a specific pattern on ENTITY_STATES.
// DEPRECATED: Use startWatcherForBucketPattern for multi-bucket support.
func (rp *Processor) startWatcherForPattern(ctx context.Context, pattern string) error {
	return rp.startWatcherForBucketPattern(ctx, component.BucketName(component.EntityStatesBucketName, rp.org), pattern)
}

// startWatcherForBucketPattern starts a KV watcher for a specific bucket and pattern.
// Returns an error if the watcher cannot be started.
func (rp *Processor) startWatcherForBucketPattern(ctx context.Context, bucketName, pattern string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.startWatcherForBucketPatternLocked(ctx, bucketName, pattern)
}

// watcherKey creates a unique key for bucket+pattern combination
func watcherKey(bucketName, pattern string) string {
	return bucketName + ":" + pattern
}

// startWatcherForPatternLocked is the internal version that assumes the caller holds the lock.
// DEPRECATED: Use startWatcherForBucketPatternLocked for multi-bucket support.
func (rp *Processor) startWatcherForPatternLocked(ctx context.Context, pattern string) error {
	return rp.startWatcherForBucketPatternLocked(ctx, "ENTITY_STATES", pattern)
}

// startWatcherForBucketPatternLocked is the internal version that assumes the caller holds the lock.
func (rp *Processor) startWatcherForBucketPatternLocked(ctx context.Context, bucketName, pattern string) error {
	key := watcherKey(bucketName, pattern)

	// Check if watcher already exists for this bucket+pattern
	if _, exists := rp.entityWatcherMap[key]; exists {
		rp.logger.Debug("Watcher already exists", "bucket", bucketName, "pattern", pattern)
		return nil
	}

	// Get bucket
	bucket, err := rp.getOrCreateBucket(ctx, bucketName)
	if err != nil {
		return errs.WrapTransient(err, "Processor", "startWatcherForBucketPatternLocked", fmt.Sprintf("get bucket %s", bucketName))
	}

	watcher, err := bucket.Watch(ctx, pattern)
	if err != nil {
		return errs.Wrap(err, "RuleProcessor", "startWatcherForBucketPattern", "create watcher")
	}

	// Store watcher in both slice (for legacy cleanup) and map (for dynamic management)
	rp.entityWatchers = append(rp.entityWatchers, watcher)
	rp.entityWatcherMap[key] = watcher

	// Start goroutine to handle updates
	go rp.handleEntityUpdates(ctx, watcher)

	rp.logger.Info("Started KV watcher", "bucket", bucketName, "pattern", pattern)
	return nil
}

// stopWatcherForPattern stops a KV watcher for a specific pattern on ENTITY_STATES.
// DEPRECATED: Use stopWatcherForBucketPattern for multi-bucket support.
func (rp *Processor) stopWatcherForPattern(pattern string) error {
	return rp.stopWatcherForBucketPattern("ENTITY_STATES", pattern)
}

// stopWatcherForBucketPattern stops a KV watcher for a specific bucket and pattern.
func (rp *Processor) stopWatcherForBucketPattern(bucketName, pattern string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.stopWatcherForBucketPatternLocked(bucketName, pattern)
}

// stopWatcherForPatternLocked is the internal version that assumes the caller holds the lock.
// DEPRECATED: Use stopWatcherForBucketPatternLocked for multi-bucket support.
func (rp *Processor) stopWatcherForPatternLocked(pattern string) error {
	return rp.stopWatcherForBucketPatternLocked("ENTITY_STATES", pattern)
}

// stopWatcherForBucketPatternLocked is the internal version that assumes the caller holds the lock.
func (rp *Processor) stopWatcherForBucketPatternLocked(bucketName, pattern string) error {
	key := watcherKey(bucketName, pattern)
	watcher, exists := rp.entityWatcherMap[key]
	if !exists {
		rp.logger.Debug("No watcher exists", "bucket", bucketName, "pattern", pattern)
		return nil
	}

	// Stop the watcher
	if err := watcher.Stop(); err != nil {
		rp.logger.Warn("Error stopping watcher", "bucket", bucketName, "pattern", pattern, "error", err)
		// Continue with cleanup even if stop fails
	}

	// Remove from map
	delete(rp.entityWatcherMap, key)

	// Remove from slice (find and remove)
	for i, w := range rp.entityWatchers {
		if w == watcher {
			rp.entityWatchers = append(rp.entityWatchers[:i], rp.entityWatchers[i+1:]...)
			break
		}
	}

	rp.logger.Info("Stopped KV watcher", "bucket", bucketName, "pattern", pattern)
	return nil
}

// UpdateWatchPatterns dynamically updates the entity watch patterns for ENTITY_STATES.
// DEPRECATED: Use UpdateWatchBuckets for multi-bucket support.
func (rp *Processor) UpdateWatchPatterns(newPatterns []string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.updateWatchPatternsLocked(newPatterns)
}

// updateWatchPatternsLocked is the internal version that assumes the caller holds the lock.
// Called by ApplyConfigUpdate which already holds the lock.
// DEPRECATED: Use updateWatchBucketsLocked for multi-bucket support.
func (rp *Processor) updateWatchPatternsLocked(newPatterns []string) error {
	// Convert to bucket format and delegate
	buckets := map[string][]string{
		"ENTITY_STATES": newPatterns,
	}
	if err := rp.updateWatchBucketsLocked(buckets); err != nil {
		return err
	}

	// Update legacy config field for backwards compatibility
	rp.config.EntityWatchPatterns = newPatterns
	return nil
}

// UpdateWatchBuckets dynamically updates the entity watch buckets and patterns.
func (rp *Processor) UpdateWatchBuckets(newBuckets map[string][]string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.updateWatchBucketsLocked(newBuckets)
}

// updateWatchBucketsLocked is the internal version that assumes the caller holds the lock.
func (rp *Processor) updateWatchBucketsLocked(newBuckets map[string][]string) error {
	watcherCtx := rp.watcherCtx

	// If no watcher context, processor not started yet - just update config
	if watcherCtx == nil {
		rp.config.EntityWatchBuckets = newBuckets
		rp.logger.Info("Updated entity watch buckets (processor not running)", "buckets", newBuckets)
		return nil
	}

	// Build set of current watcher keys (bucket:pattern)
	currentKeys := make(map[string]bool)
	for key := range rp.entityWatcherMap {
		currentKeys[key] = true
	}

	// Build set of new watcher keys
	newKeys := make(map[string]bool)
	for bucket, patterns := range newBuckets {
		for _, pattern := range patterns {
			key := watcherKey(bucket, pattern)
			newKeys[key] = true
		}
	}

	// Stop watchers for removed keys
	for key := range currentKeys {
		if !newKeys[key] {
			// Parse bucket:pattern from key
			watcher, exists := rp.entityWatcherMap[key]
			if !exists {
				continue
			}

			if err := watcher.Stop(); err != nil {
				rp.logger.Warn("Error stopping watcher", "key", key, "error", err)
			}
			delete(rp.entityWatcherMap, key)

			// Remove from slice
			for i, w := range rp.entityWatchers {
				if w == watcher {
					rp.entityWatchers = append(rp.entityWatchers[:i], rp.entityWatchers[i+1:]...)
					break
				}
			}
			rp.logger.Debug("Stopped KV watcher", "key", key)
		}
	}

	// Start watchers for new keys
	for bucket, patterns := range newBuckets {
		for _, pattern := range patterns {
			key := watcherKey(bucket, pattern)
			if !currentKeys[key] {
				if err := rp.startWatcherForBucketPatternLocked(watcherCtx, bucket, pattern); err != nil {
					rp.logger.Warn("Failed to start watcher", "bucket", bucket, "pattern", pattern, "error", err)
				}
			}
		}
	}

	// Update config
	rp.config.EntityWatchBuckets = newBuckets

	rp.logger.Info("Updated entity watch buckets dynamically",
		"added", len(newKeys)-len(currentKeys),
		"removed", len(currentKeys)-len(newKeys),
		"total", len(newKeys))

	return nil
}

// handleEntityUpdates processes updates from a NATS KV watcher.
//
// The watcher delivers current values on startup (bootstrap phase) followed by
// a nil sentinel, then live updates. We track the bootstrap boundary so rules
// can re-fire OnEnter actions on restart when their persisted match state
// indicates they were matching before the crash (see Gap 2 / on_recovery).
func (rp *Processor) handleEntityUpdates(ctx context.Context, watcher jetstream.KeyWatcher) {
	defer func() {
		if r := recover(); r != nil {
			rp.logger.Error("Panic in handleEntityUpdates", "error", r)
		}
	}()
	// NOTE: watcher.Stop() is called explicitly before each return, not via defer.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine when using defer or calling from another goroutine.

	bootstrap := true

	for {
		select {
		case <-ctx.Done():
			watcher.Stop()
			return
		case <-rp.shutdown:
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				// Channel closed, watcher stopped externally
				watcher.Stop()
				return
			}
			if entry == nil {
				// Nil entry indicates initial state replay is complete.
				// All subsequent entries are live updates.
				bootstrap = false
				continue
			}

			entityKey := entry.Key()
			revision := entry.Revision()

			// Determine action based on operation and revision
			action := "UPDATED"
			if entry.Operation() == jetstream.KeyValueDelete {
				action = "DELETED"
			} else if revision == 1 {
				action = "CREATED"
			}

			// Handle deletion by removing from coalescer and evaluating immediately
			if action == "DELETED" {
				if rp.entityCoalescer != nil {
					rp.entityCoalescer.Remove(entityKey)
				}
				rp.evaluateRulesForEntityState(ctx, entityKey,
					entitySnapshot{Action: action, Revision: revision}, bootstrap)
				continue
			}

			// During bootstrap, bypass the coalescer so OnEnter recovery fires
			// deterministically for each replayed entity. The coalescer can delay
			// evaluation past the bootstrap boundary, which would lose the flag.
			// Because the gate is evaluated before any Add, no pre-sentinel entry
			// ever enters the coalescer from this watcher — so there is no
			// queued-entity race when bootstrap flips to false.
			if rp.entityCoalescer == nil || bootstrap {
				var state gtypes.EntityState
				if err := json.Unmarshal(entry.Value(), &state); err != nil {
					rp.logger.Warn("Failed to unmarshal entity state for immediate evaluation",
						"entity", entityKey, "error", err)
					continue
				}
				rp.evaluateRulesForEntityState(ctx, entityKey,
					entitySnapshot{State: &state, Action: action, Revision: revision}, bootstrap)
			} else {
				// Batched: the coalescer will fetch current state at evaluation time
				rp.entityCoalescer.Add(entityKey)
			}
		}
	}
}

// evaluateEntitiesInBatch fetches current state and evaluates rules for a batch of entities.
// Called by CoalescingSet callback after the debounce window expires.
func (rp *Processor) evaluateEntitiesInBatch(ctx context.Context, entityIDs []string) {
	if len(entityIDs) == 0 {
		return
	}

	// Track metrics
	if rp.metrics != nil {
		rp.metrics.debounceDelaysTotal.Add(float64(len(entityIDs)))
	}

	rp.logger.Debug("Evaluating batched entities", "count", len(entityIDs))

	for _, entityID := range entityIDs {
		snap, err := rp.fetchCurrentEntityState(ctx, entityID)
		if err != nil {
			rp.logger.Warn("Failed to fetch entity state for rule evaluation",
				"entityID", entityID, "error", err)
			continue
		}

		// Evaluate rules against current state. Coalesced paths only run after
		// bootstrap completes, so bootstrap=false here.
		rp.evaluateRulesForEntityState(ctx, entityID, snap, false)
	}
}

// entitySnapshot bundles the outputs of a single KV fetch so callers don't
// thread four values through each evaluation path.
type entitySnapshot struct {
	// State is the parsed entity state. Nil for a DELETED entity.
	State *gtypes.EntityState
	// Action is the CRUD label: CREATED (revision 1), UPDATED, or DELETED.
	Action string
	// Revision is the KV revision observed. 0 when the entity has been deleted.
	Revision uint64
}

// fetchCurrentEntityState retrieves the current state of an entity from the
// ENTITY_STATES KV bucket. A missing entity returns a DELETED snapshot with
// nil State rather than an error.
func (rp *Processor) fetchCurrentEntityState(ctx context.Context, entityID string) (entitySnapshot, error) {
	entityBucket, err := rp.natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES")
	if err != nil {
		return entitySnapshot{}, errs.WrapTransient(err, "Processor", "fetchCurrentEntityState", "get ENTITY_STATES bucket")
	}

	entry, err := entityBucket.Get(ctx, entityID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return entitySnapshot{Action: "DELETED"}, nil
		}
		return entitySnapshot{}, errs.WrapTransient(err, "Processor", "fetchCurrentEntityState", "get entity state")
	}

	var state gtypes.EntityState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return entitySnapshot{}, errs.WrapInvalid(err, "Processor", "fetchCurrentEntityState", "unmarshal entity state")
	}

	action := "UPDATED"
	if entry.Revision() == 1 {
		action = "CREATED"
	}

	return entitySnapshot{State: &state, Action: action, Revision: entry.Revision()}, nil
}
