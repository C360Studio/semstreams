package rule

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	rp.graphStateGuardRequired.Store(true)
	if err := rp.startGraphStateGuard(watcherCtx); err != nil {
		return err
	}

	// Build effective bucket-to-patterns map
	bucketPatterns := rp.getEffectiveBucketPatterns()

	if len(bucketPatterns) == 0 {
		rp.logger.Info("No rule entity patterns configured; authoritative graph guard remains active")
		return nil
	}

	// Start watchers for all configured buckets and patterns
	for bucketName, patterns := range bucketPatterns {
		for _, pattern := range patterns {
			if err := rp.startWatcherForBucketPattern(watcherCtx, bucketName, pattern); err != nil {
				rp.markGraphStateGuardDegraded(ctx, fmt.Errorf("start %s pattern %q: %w", bucketName, pattern, err))
				return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady, err)
			}
		}
	}

	return nil
}

func (rp *Processor) startGraphStateGuard(ctx context.Context) error {
	bucket, err := rp.getOrCreateBucket(ctx, gtypes.BucketEntityStates)
	if err != nil {
		rp.markGraphStateGuardDegraded(ctx, err)
		return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady, err)
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		rp.markGraphStateGuardDegraded(ctx, err)
		return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady, err)
	}
	rp.mu.Lock()
	rp.entityWatchers = append(rp.entityWatchers, watcher)
	rp.mu.Unlock()
	go rp.handleGraphStateGuard(ctx, watcher)
	return nil
}

func (rp *Processor) handleGraphStateGuard(ctx context.Context, watcher jetstream.KeyWatcher) {
	for {
		select {
		case <-ctx.Done():
			watcher.Stop()
			return
		case <-rp.shutdown:
			watcher.Stop()
			return
		case <-rp.graphStateGuardDone:
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				if rp.graphGuardTransportCloseExpected(ctx) {
					watcher.Stop()
					return
				}
				rp.markGraphStateGuardDegraded(ctx, errors.New("authoritative ENTITY_STATES watcher closed unexpectedly"))
				watcher.Stop()
				return
			}
			if entry == nil {
				rp.graphStateGuardReady.Store(true)
				rp.graphStateGuardReadyOnce.Do(func() { close(rp.graphStateGuardReadyCh) })
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
				rp.advanceGraphStateGuardRevision(entry.Revision())
				continue
			}
			var state gtypes.EntityState
			if err := gtypes.UnmarshalEntityState(entry.Value(), &state); err != nil {
				rp.markGraphStateResetRequired(ctx, entry.Key(), err)
				watcher.Stop()
				return
			}
			rp.advanceGraphStateGuardRevision(entry.Revision())
		}
	}
}

func (rp *Processor) advanceGraphStateGuardRevision(revision uint64) {
	if revision == 0 {
		return
	}
	rp.graphStateProgressMu.Lock()
	defer rp.graphStateProgressMu.Unlock()
	if revision <= rp.graphStateGuardRevision.Load() {
		return
	}
	rp.graphStateGuardRevision.Store(revision)
	close(rp.graphStateProgress)
	rp.graphStateProgress = make(chan struct{})
}

// waitGraphStateGuardRevision prevents an ENTITY_STATES pattern subscription
// from overtaking the authoritative WatchAll validator. Revision R is safe to
// evaluate only after the ordered guard has processed cleanly through R.
func (rp *Processor) waitGraphStateGuardRevision(ctx context.Context, revision uint64) bool {
	if revision == 0 {
		return rp.waitGraphStateGuard(ctx)
	}
	for {
		if rp.graphStateResetRequired.Load() || rp.graphStateGuardDegraded.Load() {
			return false
		}
		rp.graphStateProgressMu.Lock()
		if rp.graphStateGuardRevision.Load() >= revision {
			rp.graphStateProgressMu.Unlock()
			return rp.graphRuleEvaluationReady()
		}
		progress := rp.graphStateProgress
		rp.graphStateProgressMu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-rp.shutdown:
			return false
		case <-rp.graphStateGuardDone:
			return false
		case <-progress:
		}
	}
}

func (rp *Processor) graphGuardTransportCloseExpected(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	select {
	case <-rp.shutdown:
		return true
	default:
		return false
	}
}

func (rp *Processor) markGraphStateGuardDegraded(ctx context.Context, err error) {
	if rp.graphStateGuardDegraded.CompareAndSwap(false, true) {
		rp.graphStateGuardReady.Store(false)
		rp.logger.Warn("Rule evaluation disabled: graph-state guard degraded",
			"code", gtypes.ErrorCodeIndexNotReady, "error", err)
		if rp.lifecycleReporter != nil {
			if reportErr := rp.lifecycleReporter.ReportStage(ctx, "degraded"); reportErr != nil {
				rp.logger.Warn("Failed to report degraded lifecycle stage", "error", reportErr)
			}
		}
		rp.graphStateGuardReadyOnce.Do(func() { close(rp.graphStateGuardReadyCh) })
		rp.graphStateGuardDoneOnce.Do(func() { close(rp.graphStateGuardDone) })
	}
}

func (rp *Processor) graphRuleEvaluationReady() bool {
	if rp.graphStateResetRequired.Load() {
		return false
	}
	if !rp.graphStateGuardRequired.Load() {
		return true
	}
	return rp.graphStateGuardReady.Load() && !rp.graphStateGuardDegraded.Load()
}

func (rp *Processor) waitGraphStateGuard(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-rp.shutdown:
		return false
	case <-rp.graphStateGuardDone:
		return false
	case <-rp.graphStateGuardReadyCh:
		return rp.graphRuleEvaluationReady()
	}
}

// getEffectiveBucketPatterns returns the configured ENTITY_STATES patterns.
func (rp *Processor) getEffectiveBucketPatterns() map[string][]string {
	return rp.config.EntityWatchBuckets
}

// getOrCreateBucket gets or creates a KV bucket by name.
// Uses appropriate defaults based on bucket purpose.
func (rp *Processor) getOrCreateBucket(ctx context.Context, bucketName string) (jetstream.KeyValue, error) {
	if bucketName != gtypes.BucketEntityStates {
		return nil, unsupportedEntityWatchBucket(bucketName)
	}
	// Try to get existing bucket first
	bucket, err := rp.natsClient.GetKeyValueBucket(ctx, bucketName)
	if err == nil {
		return bucket, nil
	}

	return rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      bucketName,
		Description: "Entity state storage",
		History:     10,
		TTL:         7 * 24 * time.Hour, // 7 days
		MaxBytes:    -1,                 // Unlimited
	})
}

// startWatcherForBucketPattern starts a KV watcher for a specific bucket and pattern.
// Returns an error if the watcher cannot be started.
func (rp *Processor) startWatcherForBucketPattern(ctx context.Context, bucketName, pattern string) error {
	if err := validateEntityWatchPattern(bucketName, pattern); err != nil {
		return err
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.startWatcherForBucketPatternLocked(ctx, bucketName, pattern)
}

// watcherKey creates a unique key for bucket+pattern combination
func watcherKey(bucketName, pattern string) string {
	return bucketName + ":" + pattern
}

// startWatcherForBucketPatternLocked is the internal version that assumes the caller holds the lock.
func (rp *Processor) startWatcherForBucketPatternLocked(ctx context.Context, bucketName, pattern string) error {
	if err := validateEntityWatchPattern(bucketName, pattern); err != nil {
		return err
	}
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
	go rp.handleEntityUpdatesForBucket(ctx, watcher, bucketName)

	rp.logger.Info("Started KV watcher", "bucket", bucketName, "pattern", pattern)
	return nil
}

// stopWatcherForBucketPattern stops a KV watcher for a specific bucket and pattern.
func (rp *Processor) stopWatcherForBucketPattern(bucketName, pattern string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.stopWatcherForBucketPatternLocked(bucketName, pattern)
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

// UpdateWatchBuckets dynamically updates the canonical ENTITY_STATES patterns.
func (rp *Processor) UpdateWatchBuckets(newBuckets map[string][]string) error {
	if err := validateEntityWatchBuckets(newBuckets); err != nil {
		return err
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.updateWatchBucketsLocked(newBuckets)
}

// updateWatchBucketsLocked is the internal version that assumes the caller holds the lock.
func (rp *Processor) updateWatchBucketsLocked(newBuckets map[string][]string) error {
	if err := validateEntityWatchBuckets(newBuckets); err != nil {
		return err
	}
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
// The processor-wide graph guard validates the complete authoritative snapshot
// first. This pattern watcher then streams its bounded NATS bootstrap directly,
// preserving Bootstrap=true for OnRecovery until its nil sentinel arrives. For
// ENTITY_STATES, every entry also waits on the guard's revision watermark.
func (rp *Processor) handleEntityUpdates(ctx context.Context, watcher jetstream.KeyWatcher) {
	rp.handleEntityUpdatesForBucket(ctx, watcher, gtypes.BucketEntityStates)
}

func (rp *Processor) handleEntityUpdatesForBucket(ctx context.Context, watcher jetstream.KeyWatcher, bucketName string) {
	defer func() {
		if r := recover(); r != nil {
			rp.logger.Error("Panic in handleEntityUpdates", "error", r)
		}
	}()
	// NOTE: watcher.Stop() is called explicitly before each return, not via defer.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine when using defer or calling from another goroutine.
	if !rp.waitGraphStateGuard(ctx) {
		watcher.Stop()
		return
	}

	bootstrap := true

	for {
		select {
		case <-ctx.Done():
			watcher.Stop()
			return
		case <-rp.shutdown:
			watcher.Stop()
			return
		case <-rp.graphStateGuardDone:
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				if rp.entityWatcherCloseExpected(ctx, watcher) {
					watcher.Stop()
					return
				}
				rp.markGraphStateGuardDegraded(ctx, errors.New("rule pattern watcher closed unexpectedly"))
				watcher.Stop()
				return
			}
			if entry == nil {
				bootstrap = false
				continue
			}
			if rp.graphStateResetRequired.Load() {
				continue
			}
			if bucketName == gtypes.BucketEntityStates && !rp.waitGraphStateGuardRevision(ctx, entry.Revision()) {
				watcher.Stop()
				return
			}

			cursor := classifyEntityWatchEntry(entry)
			update, err := decodeEntityWatchUpdate(entry, cursor)
			if err != nil {
				if rp.markGraphStateResetRequired(ctx, entry.Key(), err) {
					continue
				}
				rp.logger.Warn("Failed to unmarshal live entity state for rule evaluation",
					"entity", entry.Key(), "error", err)
				continue
			}
			rp.dispatchEntityWatchUpdate(ctx, update, bootstrap)
		}
	}
}

func (rp *Processor) entityWatcherCloseExpected(ctx context.Context, watcher jetstream.KeyWatcher) bool {
	if ctx.Err() != nil {
		return true
	}
	select {
	case <-rp.shutdown:
		return true
	default:
	}

	// Dynamic configuration intentionally removes a watcher before its Stop
	// closes Updates. A still-registered watcher closing while the owning
	// context is live is the unexpected/lost-connection case.
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	if rp.watcherCtx == nil {
		return false
	}
	for _, active := range rp.entityWatcherMap {
		if active == watcher {
			return false
		}
	}
	return true
}

type entityWatchUpdate struct {
	entityKey string
	snapshot  entitySnapshot
}

type entityWatchCursor struct {
	entityKey string
	action    string
	revision  uint64
}

func classifyEntityWatchEntry(entry jetstream.KeyValueEntry) entityWatchCursor {
	action := "UPDATED"
	if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
		action = "DELETED"
	} else if entry.Revision() == 1 {
		action = "CREATED"
	}
	return entityWatchCursor{
		entityKey: entry.Key(),
		action:    action,
		revision:  entry.Revision(),
	}
}

func decodeEntityWatchUpdate(entry jetstream.KeyValueEntry, cursor entityWatchCursor) (entityWatchUpdate, error) {
	update := entityWatchUpdate{
		entityKey: cursor.entityKey,
		snapshot: entitySnapshot{
			Action:   cursor.action,
			Revision: cursor.revision,
		},
	}
	if cursor.action == "DELETED" {
		return update, nil
	}
	var state gtypes.EntityState
	if err := gtypes.UnmarshalEntityState(entry.Value(), &state); err != nil {
		return entityWatchUpdate{}, err
	}
	update.snapshot.State = &state
	return update, nil
}

func (rp *Processor) dispatchEntityWatchUpdate(ctx context.Context, update entityWatchUpdate, bootstrap bool) {
	if !rp.graphRuleEvaluationReady() {
		return
	}
	if update.snapshot.Action == "DELETED" {
		if rp.entityCoalescer != nil {
			rp.entityCoalescer.Remove(update.entityKey)
		}
		rp.evaluateRulesForEntityState(ctx, update.entityKey, update.snapshot, bootstrap)
		if rp.stateTracker != nil {
			if err := rp.stateTracker.DeleteAllForEntity(ctx, update.entityKey); err != nil {
				rp.logger.Warn("Failed to clean up rule state for deleted entity",
					"entity", update.entityKey, "error", err)
			}
		}
		return
	}

	// Bootstrap entries bypass the coalescer so OnRecovery (or opted-in
	// OnEnter recovery) sees Bootstrap=true exactly as before. Live entries are
	// already contract-validated above, then may be coalesced for efficiency.
	if rp.entityCoalescer == nil || bootstrap {
		rp.evaluateRulesForEntityState(ctx, update.entityKey, update.snapshot, bootstrap)
		return
	}
	rp.entityCoalescer.Add(update.entityKey)
}

// evaluateEntitiesInBatch fetches current state and evaluates rules for a batch of entities.
// Called by CoalescingSet callback after the debounce window expires.
func (rp *Processor) evaluateEntitiesInBatch(ctx context.Context, entityIDs []string) {
	if len(entityIDs) == 0 || !rp.graphRuleEvaluationReady() {
		return
	}

	// Track metrics
	if rp.metrics != nil {
		rp.metrics.debounceDelaysTotal.Add(float64(len(entityIDs)))
	}

	rp.logger.Debug("Evaluating batched entities", "count", len(entityIDs))

	for _, entityID := range entityIDs {
		if !rp.graphRuleEvaluationReady() {
			return
		}
		snap, err := rp.fetchCurrentEntityState(ctx, entityID)
		if err != nil {
			if rp.markGraphStateResetRequired(ctx, entityID, err) {
				return
			}
			rp.logger.Warn("Failed to fetch entity state for rule evaluation",
				"entityID", entityID, "error", err)
			continue
		}

		// Evaluate rules against current state. Coalesced paths only run after
		// bootstrap completes, so bootstrap=false here.
		rp.evaluateRulesForEntityState(ctx, entityID, snap, false)
	}
}

// markGraphStateResetRequired recognizes the shared ENTITY_STATES poison
// signal and latches rule evaluation off until the process is restarted after
// an operator wipe/restart/reseed. It returns true only for graph-state contract
// failures so callers can keep their normal transient-error behavior.
func (rp *Processor) markGraphStateResetRequired(ctx context.Context, entityID string, err error) bool {
	var contractErr *gtypes.StateContractError
	if !errors.As(err, &contractErr) {
		return false
	}

	if rp.graphStateResetRequired.CompareAndSwap(false, true) {
		rp.graphStateGuardReady.Store(false)
		rp.logger.Error("Rule evaluation disabled: graph state reset required",
			"code", gtypes.ErrorCodeGraphStateResetRequired,
			"reason", contractErr.Reason,
			"entity", entityID,
			"error", err)
		if rp.lifecycleReporter != nil {
			if reportErr := rp.lifecycleReporter.ReportStage(ctx, "reset_required"); reportErr != nil {
				rp.logger.Warn("Failed to report reset-required lifecycle stage", "error", reportErr)
			}
		}
		rp.graphStateGuardReadyOnce.Do(func() { close(rp.graphStateGuardReadyCh) })
		rp.graphStateGuardDoneOnce.Do(func() { close(rp.graphStateGuardDone) })
	}
	return true
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
	if err := gtypes.UnmarshalEntityState(entry.Value(), &state); err != nil {
		return entitySnapshot{}, errs.WrapInvalid(err, "Processor", "fetchCurrentEntityState", "unmarshal entity state")
	}

	action := "UPDATED"
	if entry.Revision() == 1 {
		action = "CREATED"
	}

	return entitySnapshot{State: &state, Action: action, Revision: entry.Revision()}, nil
}
