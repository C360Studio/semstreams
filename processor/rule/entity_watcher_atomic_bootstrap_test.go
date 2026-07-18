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

// TestWatchEntityStatesZeroPatternsHoldsNoEntityWatchers is the
// poison-response-scoping fan-out contract: a rule processor configured with
// zero entity-watch patterns holds ZERO ENTITY_STATES watchers — no dedicated
// contract-guard watcher, no firehose fan-out for a feature it doesn't use.
// newAtomicBootstrapProcessor wires a nil NATS client, so ANY attempt to reach
// the ENTITY_STATES bucket (guard or pattern watcher alike) would fail loudly
// instead of passing silently.
func TestWatchEntityStatesZeroPatternsHoldsNoEntityWatchers(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.watchEntityStates(ctx); err != nil {
		t.Fatalf("watchEntityStates with zero patterns: %v", err)
	}
	processor.mu.RLock()
	trackedWatchers := len(processor.entityWatchers)
	mappedWatchers := len(processor.entityWatcherMap)
	processor.mu.RUnlock()
	if trackedWatchers != 0 || mappedWatchers != 0 {
		t.Fatalf("zero entity-watch patterns must hold zero ENTITY_STATES watchers, got %d tracked / %d mapped",
			trackedWatchers, mappedWatchers)
	}
	if !processor.graphRuleEvaluationReady() {
		t.Fatal("rule evaluation must be ready without any ENTITY_STATES watcher bootstrap")
	}
}

// TestRuleWatcherConsumedBootstrapPoisonLatchesEvaluationOff proves the sticky
// rule-evaluation kill switch fires on CONSUMED poison via the pattern-watch
// input path itself (there is no dedicated contract-guard watcher). A poisoned
// value delivered before any valid entry stops everything; a valid entry
// consumed before the poison arrives evaluates at most once, then the latch
// drops every later delivery.
func TestRuleWatcherConsumedBootstrapPoisonLatchesEvaluationOff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		entries         func(t *testing.T) []jetstream.KeyValueEntry
		wantEvaluations int64
	}{
		{
			name: "poison then valid",
			entries: func(t *testing.T) []jetstream.KeyValueEntry {
				return []jetstream.KeyValueEntry{
					poisonedRuleEntityEntry("acme.ops.rule.gcs.device.poison", 1),
					validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.valid", 2),
				}
			},
			wantEvaluations: 0,
		},
		{
			name: "valid then poison then valid",
			entries: func(t *testing.T) []jetstream.KeyValueEntry {
				return []jetstream.KeyValueEntry{
					validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.valid", 1),
					poisonedRuleEntityEntry("acme.ops.rule.gcs.device.poison", 2),
					validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.after", 3),
				}
			},
			wantEvaluations: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processor := newAtomicBootstrapProcessor(t)
			watcher := newAtomicBootstrapWatcher()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go processor.handleEntityUpdates(ctx, watcher)

			for _, entry := range tc.entries(t) {
				watcher.updates <- entry
			}

			waitForRuleWatcher(t, func() bool { return processor.graphStateResetRequired.Load() })
			if processor.graphRuleEvaluationReady() {
				t.Fatal("consumed poison must leave rule evaluation latched off")
			}
			// Give any (incorrect) late dispatch a chance to land before
			// asserting the counters.
			time.Sleep(30 * time.Millisecond)
			if got := atomic.LoadInt64(&processor.messagesEvaluated); got != tc.wantEvaluations {
				t.Fatalf("evaluations = %d, want %d", got, tc.wantEvaluations)
			}
			if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
				t.Fatalf("actions after consumed poison = %d, want zero", got)
			}
		})
	}
}

// TestRuleWatcherValidEntriesDoNotLatchKillSwitch is the negative leg: valid
// consumed values evaluate normally and latch nothing.
func TestRuleWatcherValidEntriesDoNotLatchKillSwitch(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleEntityUpdates(ctx, watcher)

	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.boot", 1)
	watcher.updates <- nil // bootstrap sentinel
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.live", 2)
	waitForRuleWatcher(t, func() bool { return atomic.LoadInt64(&processor.messagesEvaluated) == 2 })

	if processor.graphStateResetRequired.Load() {
		t.Fatal("valid consumed values must not latch the reset-required kill switch")
	}
	if processor.graphStateGuardDegraded.Load() {
		t.Fatal("valid consumed values must not degrade the entity-watch lane")
	}
	if !processor.graphRuleEvaluationReady() {
		t.Fatal("rule evaluation must remain ready on valid input")
	}
}

func TestRuleWatcherCleanBootstrapPreservesOnRecovery(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
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
	go processor.handleEntityUpdates(ctx, watcher)
	watcher.updates <- validRuleEntityEntry(t, entityID, 1)
	watcher.updates <- nil
	waitForRuleWatcher(t, func() bool { return executor.calls.Load() == 1 })
}

// TestRuleWatcherNeverMatchedEntityDoesNotRecoverOnBootstrap is the
// negative leg of the gh#530 / rule-evaluation-completeness acceptance
// contract: "never-matched entities do not recover". Deliberately does
// NOT seed prior MatchState (unlike
// TestRuleWatcherCleanBootstrapPreservesOnRecovery above) — the entity
// currently matches (the test rule has no conditions, so any entity
// state matches) but has no persisted history, so bootstrap replay must
// treat it as a fresh Entered, not a recovery: OnRecovery must not fire
// because the bootstrap-recovery fork requires hadPrevState=true.
func TestRuleWatcherNeverMatchedEntityDoesNotRecoverOnBootstrap(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
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
	// No tracker.Set seed here — this entity has no prior state.
	entityID := "acme.ops.rule.gcs.mission.neverbefore"

	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go processor.handleEntityUpdates(ctx, watcher)
	watcher.updates <- validRuleEntityEntry(t, entityID, 1)
	watcher.updates <- nil
	waitForRuleWatcher(t, func() bool { return atomic.LoadInt64(&processor.messagesEvaluated) == 1 })

	// Give any (incorrect) async recovery firing a chance to land before
	// asserting silence.
	time.Sleep(30 * time.Millisecond)
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("OnRecovery fired for an entity with no prior match state: %d calls, want 0", got)
	}

	// The bootstrap replay must still have persisted a fresh MatchState
	// so a LATER restart (this entity now has history) could recover.
	// messagesEvaluated increments when evaluation BEGINS
	// (evaluateRulesForEntityState) — the persist lands later in the same
	// pass, so wait on the awaited state itself, not the counter (gh#557).
	waitForRuleWatcher(t, func() bool {
		st, stErr := tracker.Get(context.Background(), "recovery-rule", entityID)
		return stErr == nil && st.IsMatching
	})
	persisted, err := tracker.Get(context.Background(), "recovery-rule", entityID)
	if err != nil {
		t.Fatalf("expected MatchState to be persisted after bootstrap Entered, got error: %v", err)
	}
	if !persisted.IsMatching {
		t.Error("expected persisted MatchState.IsMatching=true after bootstrap first-match")
	}
}

// TestRuleWatcherLiveMatchPersistsStateForLaterBootstrapRecovery is the
// full-cycle gh#530 / rule-evaluation-completeness proof: a LIVE
// (non-bootstrap) match — not a manually-seeded MatchState — persists
// state through the real dispatch path, and a genuine restart (a second
// Processor + watcher session sharing the same StateTracker/bucket, the
// established idiom for "restart" in this test file) then fires
// OnRecovery exactly once. This is the piece
// TestRuleWatcherCleanBootstrapPreservesOnRecovery doesn't cover: that
// test seeds MatchState directly via tracker.Set rather than proving the
// live path itself persists it.
func TestRuleWatcherLiveMatchPersistsStateForLaterBootstrapRecovery(t *testing.T) {
	t.Parallel()
	tracker := NewStateTracker(newMockKVBucket(), nil)
	entityID := "acme.ops.rule.gcs.mission.livepersist"
	ruleDef := Definition{
		ID: "recovery-rule", Name: "Recovery Rule", Type: "test_rule",
		Entity:     EntityConfig{Pattern: "*.*.*.*.*.*"},
		OnRecovery: []Action{{Type: ActionTypePublish, Subject: "test.recovered"}},
	}

	// "Process instance 1": live traffic only. Sending the nil sentinel
	// FIRST flips bootstrap=false before any real entry arrives, so the
	// entity update below dispatches on the genuine live path.
	liveExecutor := &atomicRecoveryExecutor{}
	liveRule, err := NewTestRule("atomic-bootstrap-test", "recovery-rule", "Recovery Rule", nil, nil)
	if err != nil {
		t.Fatalf("NewTestRule: %v", err)
	}
	processor1 := newAtomicBootstrapProcessor(t)
	processor1.stateTracker = tracker
	processor1.statefulEvaluator = NewStatefulEvaluator(tracker, liveExecutor, nil)
	processor1.rules["recovery-rule"] = liveRule
	processor1.ruleDefinitions["recovery-rule"] = ruleDef

	watcher1 := newAtomicBootstrapWatcher()
	ctx1, cancel1 := context.WithCancel(context.Background())
	go processor1.handleEntityUpdates(ctx1, watcher1)
	watcher1.updates <- nil // empty bootstrap — flips to live immediately
	watcher1.updates <- validRuleEntityEntry(t, entityID, 1)
	waitForRuleWatcher(t, func() bool { return atomic.LoadInt64(&processor1.messagesEvaluated) == 1 })
	// messagesEvaluated increments when evaluation BEGINS
	// (evaluateRulesForEntityState) — the StatefulEvaluator persists
	// MatchState later in the same pass. Waiting on the counter alone races
	// the persist (the gh#557 CI flake); wait on the awaited state itself
	// before tearing down the first watcher session.
	waitForRuleWatcher(t, func() bool {
		st, stErr := tracker.Get(context.Background(), "recovery-rule", entityID)
		return stErr == nil && st.IsMatching
	})
	cancel1()

	if got := liveExecutor.calls.Load(); got != 0 {
		t.Fatalf("OnRecovery fired on a plain live Entered transition: %d calls, want 0", got)
	}
	persisted, err := tracker.Get(context.Background(), "recovery-rule", entityID)
	if err != nil {
		t.Fatalf("expected live match to persist MatchState, got error: %v", err)
	}
	if !persisted.IsMatching {
		t.Fatal("expected persisted MatchState.IsMatching=true after live match")
	}

	// "Process instance 2": simulated restart — new Processor, new
	// StatefulEvaluator, new watcher session, SAME StateTracker/bucket.
	// Bootstrap replay of the same still-matching entity must now
	// classify as recovery and fire OnRecovery exactly once.
	restartExecutor := &atomicRecoveryExecutor{}
	restartRule, err := NewTestRule("atomic-bootstrap-test", "recovery-rule", "Recovery Rule", nil, nil)
	if err != nil {
		t.Fatalf("NewTestRule: %v", err)
	}
	processor2 := newAtomicBootstrapProcessor(t)
	processor2.stateTracker = tracker
	processor2.statefulEvaluator = NewStatefulEvaluator(tracker, restartExecutor, nil)
	processor2.rules["recovery-rule"] = restartRule
	processor2.ruleDefinitions["recovery-rule"] = ruleDef

	watcher2 := newAtomicBootstrapWatcher()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go processor2.handleEntityUpdates(ctx2, watcher2)
	// Revision must advance past the live write's SourceRevision (1) or
	// the stale-replay guard in StatefulEvaluator.Evaluate skips it.
	watcher2.updates <- validRuleEntityEntry(t, entityID, 2)
	watcher2.updates <- nil
	waitForRuleWatcher(t, func() bool { return restartExecutor.calls.Load() == 1 })
}

func TestRuleWatcherLivePoisonLatchesBeforeAnyLaterAction(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
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

func TestRuleWatcherUnexpectedTransportCloseDegradesWithoutResetPoison(t *testing.T) {
	t.Parallel()
	processor := newAtomicBootstrapProcessor(t)
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
