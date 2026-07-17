package rule

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

type atomicBootstrapWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped atomic.Bool
}

func newAtomicBootstrapWatcher() *atomicBootstrapWatcher {
	return &atomicBootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 16)}
}

func (w *atomicBootstrapWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *atomicBootstrapWatcher) Stop() error {
	w.stopped.Store(true)
	return nil
}

func TestRuleWatcherBootstrapIsAtomicAcrossPredicatePoisonOrdering(t *testing.T) {
	t.Parallel()
	valid := validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.valid", 1)
	// Deliberately outside the configured rule pattern: the shared WatchAll
	// guard, not a pattern subscriber, must still stop every rule action.
	poison := poisonedRuleEntityEntry("acme.ops.other.gcs.device.poison", 2)

	for _, tc := range []struct {
		name    string
		entries []jetstream.KeyValueEntry
	}{
		{name: "valid then poison", entries: []jetstream.KeyValueEntry{valid, poison}},
		{name: "poison then valid", entries: []jetstream.KeyValueEntry{poison, valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processor := newAtomicBootstrapProcessor(t)
			processor.graphStateGuardRequired.Store(true)
			guard := newAtomicBootstrapWatcher()
			watcher := newAtomicBootstrapWatcher()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go processor.handleGraphStateGuard(ctx, guard)
			go processor.handleEntityUpdates(ctx, watcher)

			watcher.updates <- valid
			watcher.updates <- nil
			for _, entry := range tc.entries {
				guard.updates <- entry
			}

			waitForRuleWatcher(t, func() bool { return processor.graphStateResetRequired.Load() })
			if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
				t.Fatalf("bootstrap evaluations = %d, want zero", got)
			}
			if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
				t.Fatalf("bootstrap actions = %d, want zero", got)
			}
		})
	}
}

func TestRuleWatcherWaitsForSharedGuardBeforeBootstrapEvaluation(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	processor.graphStateGuardRequired.Store(true)
	guard := newAtomicBootstrapWatcher()
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleGraphStateGuard(ctx, guard)
	go processor.handleEntityUpdates(ctx, watcher)

	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.buffered", 1)
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
		t.Fatalf("evaluation before bootstrap sentinel = %d, want zero", got)
	}

	watcher.updates <- nil
	guard.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.buffered", 1)
	guard.updates <- nil
	waitForRuleWatcher(t, func() bool { return atomic.LoadInt64(&processor.messagesEvaluated) == 1 })
}

func TestRuleWatcherCleanBootstrapPreservesOnRecovery(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	processor.graphStateGuardRequired.Store(true)
	guard := newAtomicBootstrapWatcher()
	tracker := NewStateTracker(newMockKVBucket(), nil)
	executor := &atomicRecoveryExecutor{}
	processor.stateTracker = tracker
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	recoveryRule, err := NewTestRule("atomic-bootstrap-test", "recovery-rule", "Recovery Rule", nil, nil)
	if err != nil {
		t.Fatalf("NewTestRule: %v", err)
	}
	processor.rules["recovery-rule"] = recoveryRule
	processor.ruleDefinitions["recovery-rule"] = Definition{
		ID: "recovery-rule", Name: "Recovery Rule", Type: "test_rule",
		Entity:     EntityConfig{Pattern: "*.*.*.*.*.*"},
		OnRecovery: []Action{{Type: ActionTypePublish, Subject: "test.recovered"}},
	}
	// This fixture observes the stateful OnRecovery leg only and has no NATS
	// publisher; leave the separate triggered-event action leg gated off.
	entityID := "acme.ops.rule.gcs.mission.recovery"
	if err := tracker.Set(context.Background(), MatchState{
		RuleID: "recovery-rule", EntityKey: entityID, IsMatching: true,
		LastTransition: string(TransitionEntered),
	}); err != nil {
		t.Fatalf("seed recovery state: %v", err)
	}

	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleGraphStateGuard(ctx, guard)
	go processor.handleEntityUpdates(ctx, watcher)
	watcher.updates <- validRuleEntityEntry(t, entityID, 1)
	time.Sleep(30 * time.Millisecond)
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("OnRecovery calls before sentinel = %d, want zero", got)
	}
	watcher.updates <- nil
	guard.updates <- validRuleEntityEntry(t, entityID, 1)
	guard.updates <- nil
	waitForRuleWatcher(t, func() bool { return executor.calls.Load() == 1 })
}

func TestRuleWatcherLivePoisonLatchesBeforeAnyLaterAction(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	markRuleGuardClean(processor)
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleEntityUpdates(ctx, watcher)

	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.bootstrap", 1)
	watcher.updates <- nil
	waitForRuleWatcher(t, func() bool { return atomic.LoadInt64(&processor.messagesEvaluated) == 1 })

	watcher.updates <- poisonedRuleEntityEntry("acme.ops.rule.gcs.mission.poison", 2)
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.after", 3)
	waitForRuleWatcher(t, func() bool { return processor.graphStateResetRequired.Load() })
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 1 {
		t.Fatalf("evaluations after live poison = %d, want only clean bootstrap evaluation", got)
	}
	if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
		t.Fatalf("actions after live poison = %d, want zero", got)
	}
}

func TestRuleWatcherRevisionBarrierBlocksLaterValidBehindEarlierPoison(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	processor.graphStateGuardRequired.Store(true)
	guard := newAtomicBootstrapWatcher()
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleGraphStateGuard(ctx, guard)
	go processor.handleEntityUpdates(ctx, watcher)

	guard.updates <- nil
	waitForRuleWatcher(t, processor.graphRuleEvaluationReady)

	// Schedule the matching revision ahead of the authoritative stream. It
	// must wait for the guard to validate through its revision. The earlier
	// authoritative revision is poison, so no evaluation may occur.
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.after", 3)
	select {
	case <-time.After(30 * time.Millisecond):
		if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
			t.Fatalf("revision 3 escaped its authoritative barrier: evaluations=%d", got)
		}
	}
	guard.updates <- poisonedRuleEntityEntry("acme.ops.other.gcs.device.poison", 2)
	waitForRuleWatcher(t, func() bool { return processor.graphStateResetRequired.Load() })
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
		t.Fatalf("later valid revision evaluated after earlier poison: %d", got)
	}
	if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
		t.Fatalf("later valid revision triggered actions after earlier poison: %d", got)
	}
}

func TestRuleWatcherUnexpectedTransportCloseDegradesWithoutResetPoison(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	markRuleGuardClean(processor)
	watcher := newAtomicBootstrapWatcher()
	done := make(chan struct{})
	go func() {
		processor.handleEntityUpdates(context.Background(), watcher)
		close(done)
	}()

	close(watcher.updates)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher loop did not stop after unexpected close")
	}
	if processor.graphStateResetRequired.Load() {
		t.Fatal("transport close was misclassified as reset-required poison")
	}
	if !processor.graphStateGuardDegraded.Load() {
		t.Fatal("transport close did not mark rule graph lane degraded")
	}
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
		t.Fatalf("partial bootstrap evaluations = %d, want zero", got)
	}
}

func TestRuleWatcherCloseAfterCancellationDoesNotReportPoison(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	markRuleGuardClean(processor)
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		processor.handleEntityUpdates(ctx, watcher)
		close(done)
	}()

	cancel()
	close(watcher.updates)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher loop did not stop after cancellation")
	}
	if processor.graphStateResetRequired.Load() {
		t.Fatal("normal cancellation was misreported as graph poison")
	}
}

func TestRuleSharedGuardTransportCloseDegradesWithoutResetPoison(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	processor.graphStateGuardRequired.Store(true)
	guard := newAtomicBootstrapWatcher()
	done := make(chan struct{})
	go func() {
		processor.handleGraphStateGuard(context.Background(), guard)
		close(done)
	}()
	close(guard.updates)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shared guard did not stop after transport close")
	}
	if processor.graphStateResetRequired.Load() {
		t.Fatal("shared-guard transport close was misclassified as reset-required")
	}
	if !processor.graphStateGuardDegraded.Load() {
		t.Fatal("shared-guard transport close did not mark lane degraded")
	}
}

func newAtomicBootstrapProcessor(t *testing.T) *Processor {
	t.Helper()
	cfg := mustTestConfig(t, "rule-test-pack")
	cfg.PackID = "atomic-bootstrap-test"
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}
	return processor
}

func validRuleEntityEntry(t *testing.T, entityID string, revision uint64) jetstream.KeyValueEntry {
	t.Helper()
	data, err := graph.MarshalEntityState(&graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "entity.identity.type", Object: "mission",
		}},
	})
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	return &mockKVEntry{key: entityID, value: data, revision: revision, created: time.Now()}
}

func poisonedRuleEntityEntry(entityID string, revision uint64) jetstream.KeyValueEntry {
	return &mockKVEntry{
		key: entityID, revision: revision, created: time.Now(),
		value: []byte(`{"id":"` + entityID + `","triples":[{"subject":"` + entityID + `","predicate":"legacy.predicate","object":"mission"}]}`), // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	}
}

type atomicRecoveryExecutor struct {
	calls atomic.Int64
}

func markRuleGuardClean(processor *Processor) {
	processor.graphStateGuardRequired.Store(true)
	processor.graphStateGuardReady.Store(true)
	processor.graphStateGuardRevision.Store(^uint64(0))
	processor.graphStateGuardReadyOnce.Do(func() { close(processor.graphStateGuardReadyCh) })
}

func (e *atomicRecoveryExecutor) Execute(context.Context, Action, *ExecutionContext) error {
	e.calls.Add(1)
	return nil
}

func waitForRuleWatcher(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for watcher state")
}
