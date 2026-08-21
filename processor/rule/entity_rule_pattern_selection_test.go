package rule

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/stretchr/testify/require"
)

type patternSelectionRule struct {
	name             string
	subjects         []string
	triggered        bool
	evaluated        atomic.Int64
	messageEvaluated atomic.Int64
}

func (r *patternSelectionRule) Name() string        { return r.name }
func (r *patternSelectionRule) Subscribe() []string { return r.subjects }
func (r *patternSelectionRule) Evaluate([]message.Message) bool {
	r.messageEvaluated.Add(1)
	return r.triggered
}
func (r *patternSelectionRule) ExecuteEvents([]message.Message) ([]Event, error) {
	return nil, nil
}
func (r *patternSelectionRule) EvaluateEntityState(context.Context, *graph.EntityState) bool {
	r.evaluated.Add(1)
	return r.triggered
}

func TestEntityRulePatternsSelectOnlyMatchingEntities(t *testing.T) {
	t.Parallel()

	robotics := &patternSelectionRule{name: "robotics"}
	environmental := &patternSelectionRule{name: "environmental"}
	processor := newPatternSelectionProcessor(robotics, environmental)

	roboticsID := "acme.prod.robotics.gcs.drone.d007"
	processor.evaluateRulesForEntityState(context.Background(), roboticsID, entitySnapshot{
		State:  &graph.EntityState{ID: roboticsID},
		Action: "UPDATED",
	}, false)
	require.Equal(t, int64(1), robotics.evaluated.Load())
	require.Zero(t, environmental.evaluated.Load())

	environmentalID := "acme.prod.environmental.lab.sensor.s009"
	processor.evaluateRulesForEntityState(context.Background(), environmentalID, entitySnapshot{
		State:  &graph.EntityState{ID: environmentalID},
		Action: "UPDATED",
	}, false)
	require.Equal(t, int64(1), robotics.evaluated.Load())
	require.Equal(t, int64(1), environmental.evaluated.Load())
}

func TestEntityRulePatternsSelectOnRecovery(t *testing.T) {
	t.Parallel()

	robotics := &patternSelectionRule{name: "robotics", triggered: true}
	environmental := &patternSelectionRule{name: "environmental", triggered: true}
	processor := newPatternSelectionProcessor(robotics, environmental)
	tracker := NewStateTracker(newMockKVBucket(), nil)
	executor := &mockActionExecutor{}
	processor.stateTracker = tracker
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	roboticsDef := processor.ruleDefinitions["robotics"]
	roboticsDef.OnRecovery = []Action{{Type: ActionTypePublish, Subject: "test.recovered"}}
	processor.ruleDefinitions["robotics"] = roboticsDef
	environmentalDef := processor.ruleDefinitions["environmental"]
	environmentalDef.OnRecovery = []Action{{Type: ActionTypePublish, Subject: "test.action2"}}
	processor.ruleDefinitions["environmental"] = environmentalDef

	entityID := "acme.prod.robotics.gcs.drone.recovery"
	for _, ruleID := range []string{"robotics", "environmental"} {
		require.NoError(t, tracker.Set(context.Background(), MatchState{
			RuleID: ruleID, EntityKey: entityID, IsMatching: true,
			LastTransition: string(TransitionEntered),
		}))
	}

	processor.evaluateRulesForEntityState(context.Background(), entityID, entitySnapshot{
		State:  &graph.EntityState{ID: entityID},
		Action: "UPDATED",
	}, true)

	require.Equal(t, int64(1), robotics.evaluated.Load())
	require.Zero(t, environmental.evaluated.Load())
	require.Equal(t, 1, executor.executeCallCount)
	require.Zero(t, executor.onEnterCalls)
}

func TestEntityRulePatternsSelectOnDelete(t *testing.T) {
	t.Parallel()

	robotics := &patternSelectionRule{name: "robotics"}
	environmental := &patternSelectionRule{name: "environmental"}
	processor := newPatternSelectionProcessor(robotics, environmental)
	tracker := NewStateTracker(newMockKVBucket(), nil)
	executor := &mockActionExecutor{}
	processor.stateTracker = tracker
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	roboticsDef := processor.ruleDefinitions["robotics"]
	roboticsDef.OnExit = []Action{{Type: ActionTypePublish, Subject: "test.exited"}}
	processor.ruleDefinitions["robotics"] = roboticsDef
	environmentalDef := processor.ruleDefinitions["environmental"]
	environmentalDef.OnExit = []Action{{Type: ActionTypePublish, Subject: "test.action2"}}
	processor.ruleDefinitions["environmental"] = environmentalDef

	entityID := "acme.prod.robotics.gcs.drone.deleted"
	for _, ruleID := range []string{"robotics", "environmental"} {
		require.NoError(t, tracker.Set(context.Background(), MatchState{
			RuleID: ruleID, EntityKey: entityID, IsMatching: true,
			LastTransition: string(TransitionEntered),
		}))
	}

	processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
		entityKey: entityID,
		snapshot: entitySnapshot{
			Action:   "DELETED",
			Revision: 42,
		},
	}, false)

	require.Zero(t, robotics.evaluated.Load(), "delete selection uses the canonical entity key")
	require.Zero(t, environmental.evaluated.Load())
	require.Equal(t, 1, executor.executeCallCount)
	require.Equal(t, 1, executor.onExitCalls)
	require.Zero(t, executor.onEnterCalls)
}

func TestRuleDefinitionsSelectExactlyOneEvaluationLane(t *testing.T) {
	t.Parallel()

	messageRule := &patternSelectionRule{name: "message", subjects: []string{">"}}
	// Deliberately advertise a message subject too: the entity declaration,
	// not a factory implementation detail, is the authoritative lane contract.
	entityRule := &patternSelectionRule{name: "entity", subjects: []string{">"}}
	processor := &Processor{
		logger: slog.Default(),
		rules: map[string]Rule{
			"message": messageRule,
			"entity":  entityRule,
		},
		ruleDefinitions: map[string]Definition{
			"message": {ID: "message"},
			"entity": {
				ID:     "entity",
				Entity: EntityConfig{Pattern: "*.*.*.*.*.*"},
			},
		},
		matchCounters: make(map[string]*atomic.Int64),
	}

	payload := message.NewGenericJSON(map[string]any{"entity_id": "acme.prod.robotics.gcs.drone.d007"})
	msg := message.NewBaseMessage(message.Type{Domain: "test", Category: "event", Version: "v1"}, payload, "test")
	processor.evaluateRulesForMessage(context.Background(), "test.event", msg)
	require.Equal(t, int64(1), messageRule.messageEvaluated.Load())
	require.Zero(t, entityRule.messageEvaluated.Load())

	entityID := "acme.prod.robotics.gcs.drone.d007"
	for _, bootstrap := range []bool{false, true} {
		processor.evaluateRulesForEntityState(context.Background(), entityID, entitySnapshot{
			State:  &graph.EntityState{ID: entityID},
			Action: "UPDATED",
		}, bootstrap)
	}
	require.Zero(t, messageRule.evaluated.Load())
	require.Equal(t, int64(2), entityRule.evaluated.Load())
}

func TestMessageOnlyRuleIsExcludedFromEntityDelete(t *testing.T) {
	t.Parallel()

	messageRule := &patternSelectionRule{name: "message", subjects: []string{">"}}
	entityRule := &patternSelectionRule{name: "robotics"}
	processor := newPatternSelectionProcessor(entityRule, &patternSelectionRule{name: "other"})
	processor.rules["message"] = messageRule
	processor.ruleDefinitions["message"] = Definition{
		ID:     "message",
		OnExit: []Action{{Type: ActionTypePublish, Subject: "test.action2"}},
	}
	entityDefinition := processor.ruleDefinitions["robotics"]
	entityDefinition.OnExit = []Action{{Type: ActionTypePublish, Subject: "test.exited"}}
	processor.ruleDefinitions["robotics"] = entityDefinition
	tracker := NewStateTracker(newMockKVBucket(), nil)
	executor := &mockActionExecutor{}
	processor.stateTracker = tracker
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	entityID := "acme.prod.robotics.gcs.drone.deleted"
	for _, ruleID := range []string{"message", "robotics"} {
		require.NoError(t, tracker.Set(context.Background(), MatchState{
			RuleID: ruleID, EntityKey: entityID, IsMatching: true,
			LastTransition: string(TransitionEntered),
		}))
	}

	processor.evaluateRulesForEntityState(context.Background(), entityID,
		entitySnapshot{Action: "DELETED", Revision: 42}, false)

	messageState, err := tracker.Get(context.Background(), "message", entityID)
	require.NoError(t, err)
	require.True(t, messageState.IsMatching, "message-only rule state must be untouched by an entity delete")
	entityState, err := tracker.Get(context.Background(), "robotics", entityID)
	require.NoError(t, err)
	require.False(t, entityState.IsMatching)
	require.Equal(t, 1, executor.executeCallCount)
	require.Equal(t, 1, executor.onExitCalls)
}

func TestCoalescedWorkCannotBeLaunderedAcrossWatcherGenerations(t *testing.T) {
	t.Parallel()

	rule := &patternSelectionRule{name: "entity"}
	processor := newPatternSelectionProcessor(rule, &patternSelectionRule{name: "other"})
	key := watcherKey(graph.BucketEntityStates, "*.*.*.*.*.*")
	processor.entityDispatchRecords = map[string]managedEntityWatcher{
		key: {generation: 1},
	}
	entityID := "acme.prod.robotics.gcs.drone.d007"
	stale := encodeEntityWatchPendingKey(entityID, entityWatchProvenance{key: key, generation: 1})
	// Retire generation 1, then re-add the identical pattern as generation 2
	// before its already-queued debounce item is flushed.
	processor.entityDispatchGate.Lock()
	delete(processor.entityDispatchRecords, key)
	processor.entityDispatchRecords[key] = managedEntityWatcher{generation: 2}
	processor.entityDispatchGate.Unlock()
	fetches := atomic.Int64{}
	fetch := func(context.Context, string) (entitySnapshot, error) {
		fetches.Add(1)
		return entitySnapshot{State: &graph.EntityState{ID: entityID}, Action: "UPDATED"}, nil
	}

	processor.evaluateEntitiesInBatchWithFetcher(context.Background(), []string{stale}, fetch)
	require.Zero(t, rule.evaluated.Load())

	fresh := encodeEntityWatchPendingKey(entityID, entityWatchProvenance{key: key, generation: 2})
	processor.evaluateEntitiesInBatchWithFetcher(context.Background(), []string{stale, fresh}, fetch)
	require.Equal(t, int64(1), rule.evaluated.Load())
	require.Equal(t, int64(1), fetches.Load(), "retired generations are rejected before fetch")
}

func TestOverlappingActiveWatchersCoalesceToOneEntityEvaluation(t *testing.T) {
	t.Parallel()

	rule := &patternSelectionRule{name: "entity"}
	processor := newPatternSelectionProcessor(rule, &patternSelectionRule{name: "other"})
	keyA := watcherKey(graph.BucketEntityStates, "acme.*.*.*.*.*")
	keyB := watcherKey(graph.BucketEntityStates, "*.*.robotics.*.*.*")
	processor.entityDispatchRecords = map[string]managedEntityWatcher{
		keyA: {generation: 11},
		keyB: {generation: 12},
	}
	entityID := "acme.prod.robotics.gcs.drone.d007"
	pending := []string{
		encodeEntityWatchPendingKey(entityID, entityWatchProvenance{key: keyA, generation: 11}),
		encodeEntityWatchPendingKey(entityID, entityWatchProvenance{key: keyB, generation: 12}),
	}
	var fetches atomic.Int64
	processor.evaluateEntitiesInBatchWithFetcher(context.Background(), pending, func(context.Context, string) (entitySnapshot, error) {
		fetches.Add(1)
		return entitySnapshot{State: &graph.EntityState{ID: entityID}, Action: "UPDATED"}, nil
	})

	require.Equal(t, int64(1), fetches.Load())
	require.Equal(t, int64(1), rule.evaluated.Load())
}

type orderedEntityActionExecutor struct {
	mu       sync.Mutex
	subjects []string
}

func (e *orderedEntityActionExecutor) Execute(_ context.Context, action Action, _ *ExecutionContext) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.subjects = append(e.subjects, action.Subject)
	return nil
}

func (e *orderedEntityActionExecutor) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.subjects...)
}

func TestConcurrentDeleteDoesNotWaitForBlockedBatchFetch(t *testing.T) {
	t.Parallel()

	processor, executor := newOrderedEntityProcessor()
	entityID := "acme.prod.robotics.gcs.drone.ordered"
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	batchDone := make(chan struct{})
	go func() {
		defer close(batchDone)
		processor.evaluateEntitiesInBatchWithFetcher(context.Background(), []string{entityID},
			func(context.Context, string) (entitySnapshot, error) {
				close(fetchStarted)
				<-releaseFetch
				return entitySnapshot{
					State:    &graph.EntityState{ID: entityID},
					Action:   "UPDATED",
					Revision: 10,
				}, nil
			})
	}()
	<-fetchStarted

	deleteDone := make(chan struct{})
	go func() {
		defer close(deleteDone)
		processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
			entityKey: entityID,
			snapshot:  entitySnapshot{Action: "DELETED", Revision: 11},
		}, false)
	}()

	select {
	case <-deleteDone:
	case <-time.After(time.Second):
		t.Fatal("delete waited on the blocked batch fetch")
	}
	close(releaseFetch)
	select {
	case <-batchDone:
	case <-time.After(time.Second):
		t.Fatal("batch did not complete")
	}

	// The delete records revision 11 while the older revision-10 fetch is
	// blocked. No matching state existed yet, so the delete has no OnExit action;
	// when the fetch returns, revision admission drops its stale update action.
	require.Empty(t, executor.snapshot())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 1)
}

func TestConcurrentOverlappingWatcherDeletesFireOnExitExactlyOnce(t *testing.T) {
	t.Parallel()

	processor, executor := newOrderedEntityProcessor()
	entityID := "acme.prod.robotics.gcs.drone.deleted-once"
	require.NoError(t, processor.stateTracker.Set(context.Background(), MatchState{
		RuleID:         "entity",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		SourceRevision: 40,
	}))

	var arrivals atomic.Int64
	bothRetained := make(chan struct{})
	processor.entityBeforeEvalLock = func(gotEntityID string) {
		require.Equal(t, entityID, gotEntityID)
		if arrivals.Add(1) == 2 {
			close(bothRetained)
		}
		<-bothRetained
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
				entityKey: entityID,
				snapshot:  entitySnapshot{Action: "DELETED", Revision: 42},
			}, false)
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent deletes did not complete")
	}

	require.Equal(t, []string{"test.delete"}, executor.snapshot())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 1)
}

func TestEntityEvaluationFenceReleasesQueuedReferences(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor, _ := newOrderedEntityProcessor()
	processor.entityCoalescer = cache.NewCoalescingSet(ctx, time.Hour, nil)
	entityID := "acme.prod.robotics.gcs.drone.pending"
	processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
		entityKey: entityID,
		snapshot: entitySnapshot{
			State: &graph.EntityState{ID: entityID}, Action: "UPDATED", Revision: 1,
		},
	}, false)
	require.Equal(t, 1, processor.entityCoalescer.PendingCount())
	requireFenceCounts(t, &processor.entityEvaluationFence, 1, 0)

	processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
		entityKey: entityID,
		snapshot:  entitySnapshot{Action: "DELETED", Revision: 2},
	}, false)
	require.Zero(t, processor.entityCoalescer.PendingCount())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 1)
	require.NoError(t, processor.closeEntityEvaluationQueue())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 0)
}

func TestShutdownDrainReleasesAllQueuedEntityReferences(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor, _ := newOrderedEntityProcessor()
	processor.entityCoalescer = cache.NewCoalescingSet(ctx, time.Hour, nil)
	for _, entityID := range []string{
		"acme.prod.robotics.gcs.drone.pending-a",
		"acme.prod.robotics.gcs.drone.pending-b",
	} {
		processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
			entityKey: entityID,
			snapshot: entitySnapshot{
				State: &graph.EntityState{ID: entityID}, Action: "UPDATED", Revision: 1,
			},
		}, false)
	}
	require.Equal(t, 2, processor.entityCoalescer.PendingCount())
	requireFenceCounts(t, &processor.entityEvaluationFence, 2, 0)
	require.NoError(t, processor.closeEntityEvaluationQueue())
	require.Zero(t, processor.entityCoalescer.PendingCount())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 0)
}

func TestSequentialWatcherRevisionsAreOrderedAndDeduplicated(t *testing.T) {
	t.Parallel()

	rule := &patternSelectionRule{name: "robotics"}
	processor := newPatternSelectionProcessor(rule, &patternSelectionRule{name: "other"})
	entityID := "acme.prod.robotics.gcs.drone.revisions"
	dispatchRevision := func(revision uint64) {
		processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
			entityKey: entityID,
			snapshot: entitySnapshot{
				State: &graph.EntityState{ID: entityID}, Action: "UPDATED", Revision: revision,
			},
		}, false)
	}

	dispatchRevision(10)
	require.Equal(t, int64(1), rule.evaluated.Load())
	dispatchRevision(10)
	dispatchRevision(9)
	require.Equal(t, int64(1), rule.evaluated.Load(), "same and lower revisions must be suppressed")
	dispatchRevision(11)
	require.Equal(t, int64(2), rule.evaluated.Load(), "higher revision must be admitted")
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 1)
}

func TestSequentialOverlappingWatcherDeletesFireOnExitExactlyOnce(t *testing.T) {
	t.Parallel()

	processor, executor := newOrderedEntityProcessor()
	entityID := "acme.prod.robotics.gcs.drone.sequential-delete"
	require.NoError(t, processor.stateTracker.Set(context.Background(), MatchState{
		RuleID:         "entity",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		SourceRevision: 40,
	}))
	for range 2 {
		processor.dispatchEntityWatchUpdate(context.Background(), entityWatchUpdate{
			entityKey: entityID,
			snapshot:  entitySnapshot{Action: "DELETED", Revision: 42},
		}, false)
	}
	require.Equal(t, []string{"test.delete"}, executor.snapshot())
	requireFenceCounts(t, &processor.entityEvaluationFence, 0, 1)
}

func requireFenceCounts(
	t *testing.T,
	fence *entityEvaluationFence,
	wantActive int,
	wantIdle int,
) {
	t.Helper()
	active, idle := fence.counts()
	require.Equal(t, wantActive, active)
	require.Equal(t, wantIdle, idle)
}

func newOrderedEntityProcessor() (*Processor, *orderedEntityActionExecutor) {
	rule := &patternSelectionRule{name: "entity", triggered: true}
	processor := &Processor{
		logger: slog.Default(),
		rules:  map[string]Rule{"entity": rule},
		ruleDefinitions: map[string]Definition{
			"entity": {
				ID:     "entity",
				Entity: EntityConfig{Pattern: "*.*.*.*.*.*"},
				OnEnter: []Action{{
					Type: ActionTypePublish, Subject: "test.update",
				}},
				OnExit: []Action{{
					Type: ActionTypePublish, Subject: "test.delete",
				}},
			},
		},
		matchCounters: make(map[string]*atomic.Int64),
	}
	tracker := NewStateTracker(newMockKVBucket(), nil)
	executor := &orderedEntityActionExecutor{}
	processor.stateTracker = tracker
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	return processor, executor
}

func newPatternSelectionProcessor(
	robotics *patternSelectionRule,
	environmental *patternSelectionRule,
) *Processor {
	return &Processor{
		logger: slog.Default(),
		rules: map[string]Rule{
			"robotics":      robotics,
			"environmental": environmental,
		},
		ruleDefinitions: map[string]Definition{
			"robotics": {
				ID: "robotics",
				Entity: EntityConfig{
					Pattern: "acme.*.robotics.*.drone.*",
				},
			},
			"environmental": {
				ID: "environmental",
				Entity: EntityConfig{
					Pattern: "acme.*.environmental.*.sensor.*",
				},
			},
		},
		matchCounters: make(map[string]*atomic.Int64),
	}
}
