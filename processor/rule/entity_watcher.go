package rule

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

	// Read the configured ENTITY_STATES patterns.
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
	rp.entityWatcherUpdateMu.Lock()
	defer rp.entityWatcherUpdateMu.Unlock()

	if err := validateEntityWatchPattern(bucketName, pattern); err != nil {
		return err
	}
	key := watcherKey(bucketName, pattern)
	rp.mu.RLock()
	_, exists := rp.entityWatcherMap[key]
	rp.mu.RUnlock()
	if exists {
		rp.logger.Debug("Watcher already exists", "bucket", bucketName, "pattern", pattern)
		return nil
	}

	// Watch creation performs NATS I/O and must not hold either the config
	// mutex or dispatch gate.
	watcher, err := rp.prepareEntityWatcher(ctx, bucketName, pattern)
	if err != nil {
		return err
	}

	rp.entityDispatchGate.Lock()
	rp.mu.Lock()
	if _, exists = rp.entityWatcherMap[key]; exists {
		rp.mu.Unlock()
		rp.entityDispatchGate.Unlock()
		_ = watcher.Stop()
		return nil
	}
	watcherCtx, generation := rp.registerEntityWatcherLocked(ctx, key, watcher)
	rp.mu.Unlock()
	rp.entityDispatchGate.Unlock()

	go rp.handleManagedEntityUpdates(watcherCtx, watcher, key, generation)
	rp.logger.Info("Started KV watcher", "bucket", bucketName, "pattern", pattern)
	return nil
}

// watcherKey creates a unique key for bucket+pattern combination
func watcherKey(bucketName, pattern string) string {
	return bucketName + ":" + pattern
}

type preparedEntityWatcher struct {
	key     string
	pattern string
	watcher jetstream.KeyWatcher
}

type entityWatcherFactory func(context.Context, string, string) (jetstream.KeyWatcher, error)

type managedEntityWatcher struct {
	watcher    jetstream.KeyWatcher
	generation uint64
}

func (rp *Processor) prepareEntityWatcher(ctx context.Context, bucketName, pattern string) (jetstream.KeyWatcher, error) {
	if err := validateEntityWatchPattern(bucketName, pattern); err != nil {
		return nil, err
	}
	bucket, err := rp.getOrCreateBucket(ctx, bucketName)
	if err != nil {
		return nil, errs.WrapTransient(err, "Processor", "prepareEntityWatcher", "get ENTITY_STATES bucket")
	}
	watcher, err := bucket.Watch(ctx, pattern)
	if err != nil {
		return nil, errs.Wrap(err, "RuleProcessor", "prepareEntityWatcher", "create ENTITY_STATES watcher")
	}
	return watcher, nil
}

// registerEntityWatcherLocked publishes a watcher generation while both
// entityDispatchGate and mu are write-locked by the caller.
func (rp *Processor) registerEntityWatcherLocked(
	parent context.Context,
	key string,
	watcher jetstream.KeyWatcher,
) (context.Context, uint64) {
	watcherCtx, cancel := context.WithCancel(parent)
	if rp.entityWatcherCancels == nil {
		rp.entityWatcherCancels = make(map[string]context.CancelFunc)
	}
	rp.entityWatchers = append(rp.entityWatchers, watcher)
	rp.entityWatcherMap[key] = watcher
	rp.entityWatcherCancels[key] = cancel
	if rp.entityDispatchRecords == nil {
		rp.entityDispatchRecords = make(map[string]managedEntityWatcher)
	}
	rp.entityNextGeneration++
	generation := rp.entityNextGeneration
	rp.entityDispatchRecords[key] = managedEntityWatcher{watcher: watcher, generation: generation}
	return watcherCtx, generation
}

// deactivateEntityWatcherLocked retires a watcher while both
// entityDispatchGate and mu are write-locked by the caller.
func (rp *Processor) deactivateEntityWatcherLocked(key string, watcher jetstream.KeyWatcher) {
	if cancel := rp.entityWatcherCancels[key]; cancel != nil {
		cancel()
	}
	delete(rp.entityWatcherCancels, key)
	delete(rp.entityWatcherMap, key)
	delete(rp.entityDispatchRecords, key)
	for index, candidate := range rp.entityWatchers {
		if candidate == watcher {
			rp.entityWatchers = append(rp.entityWatchers[:index], rp.entityWatchers[index+1:]...)
			return
		}
	}
}

// stopWatcherForBucketPattern stops a KV watcher for a specific bucket and pattern.
func (rp *Processor) stopWatcherForBucketPattern(bucketName, pattern string) error {
	rp.entityWatcherUpdateMu.Lock()
	defer rp.entityWatcherUpdateMu.Unlock()

	key := watcherKey(bucketName, pattern)
	rp.entityDispatchGate.Lock()
	rp.mu.Lock()
	watcher, exists := rp.entityWatcherMap[key]
	if !exists {
		rp.mu.Unlock()
		rp.entityDispatchGate.Unlock()
		rp.logger.Debug("No watcher exists", "bucket", bucketName, "pattern", pattern)
		return nil
	}

	rp.deactivateEntityWatcherLocked(key, watcher)
	rp.mu.Unlock()
	rp.entityDispatchGate.Unlock()
	if err := watcher.Stop(); err != nil {
		return fmt.Errorf("stop ENTITY_STATES watcher %q: %w", pattern, err)
	}

	rp.logger.Info("Stopped KV watcher", "bucket", bucketName, "pattern", pattern)
	return nil
}

// UpdateWatchBuckets atomically replaces the configured ENTITY_STATES patterns.
func (rp *Processor) UpdateWatchBuckets(newBuckets map[string][]string) error {
	rp.entityWatcherUpdateMu.Lock()
	defer rp.entityWatcherUpdateMu.Unlock()
	return rp.updateWatchBucketsWithFactory(newBuckets, rp.prepareEntityWatcher)
}

func (rp *Processor) updateWatchBucketsWithFactory(
	newBuckets map[string][]string,
	prepare entityWatcherFactory,
) error {
	if err := validateEntityWatchBuckets(newBuckets); err != nil {
		return err
	}
	rp.mu.RLock()
	watcherCtx := rp.watcherCtx

	// If no watcher context, processor not started yet - just update config
	if watcherCtx == nil {
		rp.mu.RUnlock()
		rp.mu.Lock()
		rp.config.EntityWatchBuckets = cloneEntityWatchBuckets(newBuckets)
		rp.mu.Unlock()
		rp.logger.Info("Updated entity watch buckets (processor not running)", "buckets", newBuckets)
		return nil
	}

	// Build set of current watcher keys (bucket:pattern)
	currentKeys := make(map[string]bool)
	for key := range rp.entityWatcherMap {
		currentKeys[key] = true
	}
	rp.mu.RUnlock()

	// Build set of new watcher keys
	newKeys := make(map[string]bool)
	for bucket, patterns := range newBuckets {
		for _, pattern := range patterns {
			key := watcherKey(bucket, pattern)
			newKeys[key] = true
		}
	}

	// Prepare every addition without registering it or starting its callback.
	// A failure stops all prepared additions and leaves old watchers/config intact.
	prepared := make([]preparedEntityWatcher, 0)
	for _, bucket := range sortedWatchBucketNames(newBuckets) {
		patterns := newBuckets[bucket]
		for _, pattern := range patterns {
			key := watcherKey(bucket, pattern)
			if !currentKeys[key] {
				watcher, err := prepare(watcherCtx, bucket, pattern)
				if err != nil {
					rp.stopPreparedEntityWatchers(prepared)
					return fmt.Errorf("prepare ENTITY_STATES watcher %q: %w", pattern, err)
				}
				prepared = append(prepared, preparedEntityWatcher{key: key, pattern: pattern, watcher: watcher})
			}
		}
	}

	removedKeys := make([]string, 0)
	for key := range currentKeys {
		if !newKeys[key] {
			removedKeys = append(removedKeys, key)
		}
	}
	sort.Strings(removedKeys)
	// Register and commit the desired set before retiring old transports. Each
	// managed callback has its own cancellation and a map-membership fence, so a
	// physical Stop failure cannot keep a stale watcher authoritative.
	additionContexts := make(map[string]context.Context, len(prepared))
	additionGenerations := make(map[string]uint64, len(prepared))
	rp.entityDispatchGate.Lock()
	rp.mu.Lock()
	for _, addition := range prepared {
		additionContexts[addition.key], additionGenerations[addition.key] =
			rp.registerEntityWatcherLocked(watcherCtx, addition.key, addition.watcher)
	}
	rp.config.EntityWatchBuckets = cloneEntityWatchBuckets(newBuckets)

	retired := make([]preparedEntityWatcher, 0, len(removedKeys))
	for _, key := range removedKeys {
		watcher := rp.entityWatcherMap[key]
		retired = append(retired, preparedEntityWatcher{key: key, watcher: watcher})
		rp.deactivateEntityWatcherLocked(key, watcher)
	}
	rp.mu.Unlock()
	rp.entityDispatchGate.Unlock()

	for _, addition := range prepared {
		go rp.handleManagedEntityUpdates(
			additionContexts[addition.key],
			addition.watcher,
			addition.key,
			additionGenerations[addition.key],
		)
	}

	var retirementErrors []error
	for _, removal := range retired {
		if err := removal.watcher.Stop(); err != nil {
			retirementErrors = append(retirementErrors, fmt.Errorf("retire ENTITY_STATES watcher %q: %w", removal.key, err))
		}
	}

	rp.logger.Info("Updated entity watch buckets dynamically",
		"added", len(prepared),
		"removed", len(retired),
		"total", len(newKeys))

	return errors.Join(retirementErrors...)
}

func (rp *Processor) stopPreparedEntityWatchers(prepared []preparedEntityWatcher) {
	for _, addition := range prepared {
		if err := addition.watcher.Stop(); err != nil {
			rp.logger.Warn("Failed to stop rolled-back ENTITY_STATES watcher", "pattern", addition.pattern, "error", err)
		}
	}
}

func cloneEntityWatchBuckets(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for bucket, patterns := range source {
		clone[bucket] = append([]string(nil), patterns...)
	}
	return clone
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
	rp.handleEntityUpdatesForBucketKey(ctx, watcher, bucketName, "", 0)
}

func (rp *Processor) handleManagedEntityUpdates(
	ctx context.Context,
	watcher jetstream.KeyWatcher,
	key string,
	generation uint64,
) {
	rp.handleEntityUpdatesForBucketKey(ctx, watcher, gtypes.BucketEntityStates, key, generation)
}

func (rp *Processor) handleEntityUpdatesForBucketKey(
	ctx context.Context,
	watcher jetstream.KeyWatcher,
	bucketName string,
	managedKey string,
	managedGeneration uint64,
) {
	defer func() {
		if r := recover(); r != nil {
			rp.logger.Error("Panic in handleEntityUpdates", "error", r)
		}
	}()
	if managedKey != "" && !rp.entityWatcherIsActive(managedKey, watcher, managedGeneration) {
		return
	}
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
			if managedKey == "" {
				watcher.Stop()
			}
			return
		case <-rp.shutdown:
			watcher.Stop()
			return
		case <-rp.graphStateGuardDone:
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				if rp.entityWatcherCloseExpected(ctx, watcher, managedKey, managedGeneration) {
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
			if managedKey != "" && !rp.entityWatcherIsActive(managedKey, watcher, managedGeneration) {
				return
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
			if managedKey != "" && !rp.entityWatcherIsActive(managedKey, watcher, managedGeneration) {
				return
			}
			if managedKey != "" {
				rp.dispatchManagedEntityWatchUpdate(
					ctx, update, bootstrap, managedKey, watcher, managedGeneration,
				)
				continue
			}
			rp.dispatchEntityWatchUpdate(ctx, update, bootstrap)
		}
	}
}

func (rp *Processor) entityWatcherIsActive(key string, watcher jetstream.KeyWatcher, generation uint64) bool {
	rp.entityDispatchGate.RLock()
	defer rp.entityDispatchGate.RUnlock()
	return rp.entityWatcherIsActiveLocked(key, watcher, generation)
}

func (rp *Processor) entityWatcherIsActiveLocked(
	key string,
	watcher jetstream.KeyWatcher,
	generation uint64,
) bool {
	record, ok := rp.entityDispatchRecords[key]
	return ok && record.watcher == watcher && record.generation == generation
}

func (rp *Processor) entityWatcherCloseExpected(
	ctx context.Context,
	watcher jetstream.KeyWatcher,
	managedKey string,
	managedGeneration uint64,
) bool {
	if ctx.Err() != nil {
		return true
	}
	select {
	case <-rp.shutdown:
		return true
	default:
	}

	if managedKey != "" {
		return !rp.entityWatcherIsActive(managedKey, watcher, managedGeneration)
	}

	// Unmanaged test/legacy handlers use the config map as their ownership
	// source. Managed handlers use the exact generation record above.
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

// dispatchManagedEntityWatchUpdate holds the generation read gate through the
// final rule-selection and dispatch seam. A configuration writer retires the
// generation under the corresponding write gate before physical watcher Stop,
// so an already-decoded stale callback cannot evaluate after retirement.
func (rp *Processor) dispatchManagedEntityWatchUpdate(
	ctx context.Context,
	update entityWatchUpdate,
	bootstrap bool,
	key string,
	watcher jetstream.KeyWatcher,
	generation uint64,
) {
	if rp.entityBeforeDispatch != nil {
		rp.entityBeforeDispatch()
	}
	rp.entityDispatchGate.RLock()
	defer rp.entityDispatchGate.RUnlock()
	if !rp.entityWatcherIsActiveLocked(key, watcher, generation) {
		return
	}
	rp.dispatchEntityWatchUpdateWithProvenance(ctx, update, bootstrap, &entityWatchProvenance{
		key:        key,
		generation: generation,
	})
}

type entityWatchProvenance struct {
	key        string
	generation uint64
}

type entityPendingWork struct {
	legacy      bool
	provenances []entityWatchProvenance
	pendingRefs int
}

const entityWatchPendingSeparator = "\x00"

func encodeEntityWatchPendingKey(entityID string, provenance entityWatchProvenance) string {
	return entityID + entityWatchPendingSeparator + provenance.key +
		entityWatchPendingSeparator + strconv.FormatUint(provenance.generation, 10)
}

func decodeEntityWatchPendingKey(pendingKey string) (string, *entityWatchProvenance, bool) {
	entityID, remainder, encoded := strings.Cut(pendingKey, entityWatchPendingSeparator)
	if !encoded {
		return pendingKey, nil, true
	}
	key, generationText, ok := strings.Cut(remainder, entityWatchPendingSeparator)
	if !ok || entityID == "" || key == "" || generationText == "" ||
		strings.Contains(generationText, entityWatchPendingSeparator) {
		return "", nil, false
	}
	generation, err := strconv.ParseUint(generationText, 10, 64)
	if err != nil || generation == 0 {
		return "", nil, false
	}
	return entityID, &entityWatchProvenance{key: key, generation: generation}, true
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
	rp.dispatchEntityWatchUpdateWithProvenance(ctx, update, bootstrap, nil)
}

func (rp *Processor) dispatchEntityWatchUpdateWithProvenance(
	ctx context.Context,
	update entityWatchUpdate,
	bootstrap bool,
	provenance *entityWatchProvenance,
) {
	if !rp.graphRuleEvaluationReady() {
		return
	}
	if update.snapshot.Action == "DELETED" {
		entry := rp.entityEvaluationFence.retain(update.entityKey)
		if rp.entityBeforeEvalLock != nil {
			rp.entityBeforeEvalLock(update.entityKey)
		}
		entry.mu.Lock()
		if !entry.admits(update.snapshot) {
			entry.mu.Unlock()
			rp.entityEvaluationFence.release(update.entityKey, entry)
			return
		}
		removedPending := 0
		if rp.entityCoalescer != nil {
			if rp.entityCoalescer.Remove(update.entityKey) {
				removedPending++
			}
			removedPending += rp.entityCoalescer.RemovePrefix(update.entityKey + entityWatchPendingSeparator)
		}
		rp.evaluateAdmittedEntitySnapshotLocked(ctx, update.entityKey, update.snapshot, bootstrap, entry)
		entry.mu.Unlock()
		rp.entityEvaluationFence.releaseRetained(update.entityKey, removedPending)
		rp.entityEvaluationFence.release(update.entityKey, entry)
		return
	}

	// Bootstrap entries bypass the coalescer so OnRecovery (or opted-in
	// OnEnter recovery) sees Bootstrap=true exactly as before. Live entries are
	// already contract-validated above, then may be coalesced for efficiency.
	if rp.entityCoalescer == nil || bootstrap {
		entry := rp.entityEvaluationFence.retain(update.entityKey)
		if rp.entityBeforeEvalLock != nil {
			rp.entityBeforeEvalLock(update.entityKey)
		}
		entry.mu.Lock()
		rp.evaluateEntitySnapshotLocked(ctx, update.entityKey, update.snapshot, bootstrap, entry)
		entry.mu.Unlock()
		rp.entityEvaluationFence.release(update.entityKey, entry)
		return
	}
	entry := rp.entityEvaluationFence.retain(update.entityKey)
	pendingKey := update.entityKey
	if provenance == nil {
		if rp.entityCoalescer.Add(pendingKey) {
			return
		}
		rp.entityEvaluationFence.release(update.entityKey, entry)
		return
	}
	pendingKey = encodeEntityWatchPendingKey(update.entityKey, *provenance)
	if rp.entityCoalescer.Add(pendingKey) {
		return
	}
	rp.entityEvaluationFence.release(update.entityKey, entry)
}

func (rp *Processor) evaluateEntitySnapshotLocked(
	ctx context.Context,
	entityID string,
	snapshot entitySnapshot,
	bootstrap bool,
	entry *entityEvaluationFenceEntry,
) {
	if !entry.admits(snapshot) {
		return
	}
	rp.evaluateAdmittedEntitySnapshotLocked(ctx, entityID, snapshot, bootstrap, entry)
}

func (rp *Processor) evaluateAdmittedEntitySnapshotLocked(
	ctx context.Context,
	entityID string,
	snapshot entitySnapshot,
	bootstrap bool,
	entry *entityEvaluationFenceEntry,
) {
	rp.evaluateRulesForEntityState(ctx, entityID, snapshot, bootstrap)
	if snapshot.Action == "DELETED" && rp.stateTracker != nil {
		if err := rp.stateTracker.DeleteAllForEntity(ctx, entityID); err != nil {
			rp.logger.Warn("Failed to clean up rule state for deleted entity",
				"entity", entityID, "error", err)
		}
	}
	entry.record(snapshot)
}

// evaluateEntitiesInBatch fetches current state and evaluates rules for a batch of entities.
// Called by CoalescingSet callback after the debounce window expires.
func (rp *Processor) evaluateEntitiesInBatch(ctx context.Context, pendingKeys []string) {
	rp.evaluateEntitiesInBatchInternal(ctx, pendingKeys, rp.fetchCurrentEntityState, true)
}

func (rp *Processor) evaluateEntitiesInBatchWithFetcher(
	ctx context.Context,
	pendingKeys []string,
	fetch func(context.Context, string) (entitySnapshot, error),
) {
	rp.evaluateEntitiesInBatchInternal(ctx, pendingKeys, fetch, false)
}

func (rp *Processor) evaluateEntitiesInBatchInternal(
	ctx context.Context,
	pendingKeys []string,
	fetch func(context.Context, string) (entitySnapshot, error),
	releasePending bool,
) {
	if len(pendingKeys) == 0 {
		return
	}

	workByEntity := make(map[string]*entityPendingWork, len(pendingKeys))
	orderedEntityIDs := make([]string, 0, len(pendingKeys))
	for _, pendingKey := range pendingKeys {
		entityID, provenance, ok := decodeEntityWatchPendingKey(pendingKey)
		if !ok {
			rp.logger.Warn("Ignoring malformed internal entity-watch work item")
			continue
		}
		work := workByEntity[entityID]
		if work == nil {
			work = &entityPendingWork{}
			workByEntity[entityID] = work
			orderedEntityIDs = append(orderedEntityIDs, entityID)
		}
		if provenance == nil {
			work.legacy = true
		} else {
			work.provenances = append(work.provenances, *provenance)
		}
		work.pendingRefs++
	}
	if len(orderedEntityIDs) == 0 {
		return
	}
	if releasePending {
		defer func() {
			for entityID, work := range workByEntity {
				rp.entityEvaluationFence.releaseRetained(entityID, work.pendingRefs)
			}
		}()
	}
	if !rp.graphRuleEvaluationReady() {
		return
	}

	// Track metrics
	if rp.metrics != nil {
		rp.metrics.debounceDelaysTotal.Add(float64(len(orderedEntityIDs)))
	}

	rp.logger.Debug("Evaluating batched entities", "count", len(orderedEntityIDs))

	for _, entityID := range orderedEntityIDs {
		if !rp.graphRuleEvaluationReady() {
			return
		}
		// The dispatch gate prevents watcher retirement from authorizing stale
		// queued work. Lock ordering is dispatch gate then entity fence on every
		// managed path, so dynamic watcher changes cannot deadlock with deletes.
		rp.entityDispatchGate.RLock()
		if !rp.entityPendingWorkAuthorizedLocked(workByEntity[entityID]) {
			rp.entityDispatchGate.RUnlock()
			continue
		}
		entry := rp.entityEvaluationFence.retain(entityID)
		if rp.entityBeforeEvalLock != nil {
			rp.entityBeforeEvalLock(entityID)
		}
		entry.mu.Lock()
		snap, err := fetch(ctx, entityID)
		if err != nil {
			entry.mu.Unlock()
			rp.entityEvaluationFence.release(entityID, entry)
			rp.entityDispatchGate.RUnlock()
			if rp.markGraphStateResetRequired(ctx, entityID, err) {
				return
			}
			rp.logger.Warn("Failed to fetch entity state for rule evaluation",
				"entityID", entityID, "error", err)
			continue
		}

		// Coalesced paths only run after bootstrap completes. The entity fence is
		// held across fetch and evaluation, so a concurrent delete is totally
		// ordered with this snapshot; its revision watermark also drops stale
		// work when the delete acquired the fence first.
		rp.evaluateEntitySnapshotLocked(ctx, entityID, snap, false, entry)
		entry.mu.Unlock()
		rp.entityEvaluationFence.release(entityID, entry)
		rp.entityDispatchGate.RUnlock()
	}
}

func (rp *Processor) releasePendingEntityWork(pendingKeys []string) {
	counts := make(map[string]int)
	for _, pendingKey := range pendingKeys {
		entityID, _, ok := decodeEntityWatchPendingKey(pendingKey)
		if ok {
			counts[entityID]++
		}
	}
	for entityID, count := range counts {
		rp.entityEvaluationFence.releaseRetained(entityID, count)
	}
}

func (rp *Processor) closeEntityEvaluationQueue() error {
	var closeErr error
	if rp.entityCoalescer != nil {
		closeErr = rp.entityCoalescer.Close()
		rp.releasePendingEntityWork(rp.entityCoalescer.Drain())
	}
	active, _ := rp.entityEvaluationFence.counts()
	rp.entityEvaluationFence.clearIdle()
	if active > 0 {
		return errors.Join(closeErr, fmt.Errorf("entity evaluation fence has %d active references after shutdown drain", active))
	}
	return closeErr
}

// entityPendingWorkAuthorizedLocked reports whether at least one exact watcher
// generation that queued the work is still authoritative. The legacy flag is
// reserved for unmanaged test/compatibility handlers that do not have managed
// watcher provenance.
func (rp *Processor) entityPendingWorkAuthorizedLocked(work *entityPendingWork) bool {
	if work == nil {
		return false
	}
	if work.legacy {
		return true
	}
	for _, provenance := range work.provenances {
		record, ok := rp.entityDispatchRecords[provenance.key]
		if ok && record.generation == provenance.generation {
			return true
		}
	}
	return false
}

// markGraphStateResetRequired recognizes the shared ENTITY_STATES poison
// signal and latches rule evaluation off until the process is restarted after
// an operator reset/reingest. It returns true only for graph-state contract
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
	// Revision is the KV revision observed. It is 0 only for a synthesized
	// DELETED snapshot returned by a current-state fetch after the key vanished.
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
