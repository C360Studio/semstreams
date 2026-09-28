package rule

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/nats-io/nats.go/jetstream"
)

// processorStopBudget bounds a test processor's Stop. The rule processor has
// no stop budget of its own; this is the production root's: the
// --shutdown-timeout default (internal/boot/flags.go, 30s) that bounds the
// whole runtime's StopAll. An unbounded Stop turned the #1283 barrier hang
// into a 20-minute CI job timeout.
const processorStopBudget = 30 * time.Second

// stopProcessorWithinBudget stops proc under processorStopBudget and fails the
// test if Stop reports an error.
func stopProcessorWithinBudget(t *testing.T, proc *Processor) {
	t.Helper()
	stopCtx, cancel := context.WithTimeout(context.Background(), processorStopBudget)
	defer cancel()
	if err := proc.Stop(stopCtx); err != nil {
		t.Errorf("rule processor Stop within %s: %v", processorStopBudget, err)
	}
}

type ruleLifecycleTestConsumer struct {
	drained chan struct{}
	closed  chan struct{}
}

type bootstrapActionRule struct{}

func (r *bootstrapActionRule) Name() string                    { return "blocking" }
func (r *bootstrapActionRule) Subscribe() []string             { return nil }
func (r *bootstrapActionRule) Evaluate([]message.Message) bool { return false }
func (r *bootstrapActionRule) ExecuteEvents([]message.Message) ([]Event, error) {
	return nil, nil
}
func (r *bootstrapActionRule) EvaluateEntityState(context.Context, *graph.EntityState) bool {
	return true
}

type cancelBlockingActionExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (e *cancelBlockingActionExecutor) Execute(ctx context.Context, _ Action, _ *ExecutionContext) error {
	close(e.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.release:
		return nil
	}
}

func (c *ruleLifecycleTestConsumer) Stop()                   {}
func (c *ruleLifecycleTestConsumer) Drain()                  { close(c.drained) }
func (c *ruleLifecycleTestConsumer) Closed() <-chan struct{} { return c.closed }

// TestRuleLifecycleOwnersRetainNoContext pins the framework context-ownership
// contract at every rule-package type that owns continuing work. Runtime work
// must receive the Start context lexically; structs retain only cancellation,
// native handles, and exact completion signals.
func TestRuleLifecycleOwnersRetainNoContext(t *testing.T) {
	t.Parallel()

	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	for _, owner := range []any{Processor{}, ConfigManager{}, CronScheduler{}} {
		typ := reflect.TypeOf(owner)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Type == contextType {
				t.Errorf("%s retains context in field %s", typ.Name(), field.Name)
			}
		}
	}
}

// TestRuleProcessorRetiresGenerationAuthority prevents the old shared
// running-Stop/rejoin state machine from returning to this one-shot owner.
func TestRuleProcessorRetiresGenerationAuthority(t *testing.T) {
	t.Parallel()

	if _, ok := reflect.TypeOf(Processor{}).FieldByName("generation"); ok {
		t.Fatal("Processor still retains shared Generation state")
	}
}

func TestRuleCleanupDrainsJetStreamBeforeCancel(t *testing.T) {
	consumer := &ruleLifecycleTestConsumer{drained: make(chan struct{}), closed: make(chan struct{})}
	runCtx, runCancel := context.WithCancel(context.Background())
	cancelCalled := make(chan struct{})
	var cancelOnce sync.Once
	processor := &Processor{
		streamConsumers: []ruleStreamConsumer{{handle: consumer}},
		runtimeDone:     make(chan struct{}),
		cancel: func() {
			cancelOnce.Do(func() { close(cancelCalled) })
			runCancel()
		},
	}
	processor.commandLane.open()
	go func() {
		processor.commandLane.run(runCtx)
		close(processor.runtimeDone)
	}()

	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- processor.cleanup(context.Background()) }()
	select {
	case <-consumer.drained:
	case <-time.After(time.Second):
		t.Fatal("JetStream Drain was not issued")
	}
	select {
	case <-cancelCalled:
		t.Fatal("runtime canceled before JetStream Closed")
	default:
	}
	close(consumer.closed)
	select {
	case err := <-cleanupDone:
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not join after JetStream Closed")
	}
}

func TestRuleRuntimeCoordinatorUsesExactStartContextAndFences(t *testing.T) {
	type contextKey string
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey("key"), "value"))
	defer cancel()
	processor := &Processor{}
	processor.commandLane.open()
	go processor.commandLane.run(ctx)

	if err := processor.submitRuntimeCommand(func(commandCtx context.Context) error {
		if got := commandCtx.Value(contextKey("key")); got != "value" {
			t.Fatalf("runtime context value = %v", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("submit runtime command: %v", err)
	}
	barrier := processor.commandLane.fence()
	if err := <-barrier; err != nil {
		t.Fatalf("runtime barrier: %v", err)
	}
	if err := processor.submitRuntimeCommand(func(context.Context) error { return nil }); err == nil {
		t.Fatal("runtime command admitted after fence")
	}
}

func TestRuleCleanupDeadlineCancelsAdmittedBootstrapEvaluationWithoutGateWait(t *testing.T) {
	const (
		pattern  = "acme.prod.robotics.*.drone.*"
		entityID = "acme.prod.robotics.gcs.drone.blocked"
	)
	rule := &bootstrapActionRule{}
	executor := &cancelBlockingActionExecutor{started: make(chan struct{}), release: make(chan struct{})}
	tracker := NewStateTracker(newMockKVBucket(), nil)
	if err := tracker.Set(context.Background(), MatchState{
		RuleID: "blocking", EntityKey: entityID, IsMatching: true,
		LastTransition: string(TransitionEntered),
	}); err != nil {
		t.Fatalf("seed recovery state: %v", err)
	}
	watcher := newTransactionalTestWatcher()
	key := watcherKey(graph.BucketEntityStates, pattern)
	runCtx, cancelRun := context.WithCancel(context.Background())
	processor := &Processor{
		logger:                slog.Default(),
		cancel:                cancelRun,
		runtimeDone:           make(chan struct{}),
		graphStateGuardDone:   make(chan struct{}),
		entityWatcherMap:      map[string]jetstream.KeyWatcher{key: watcher},
		entityWatchers:        []jetstream.KeyWatcher{watcher},
		entityDispatchRecords: make(map[string]managedEntityWatcher),
		rules:                 map[string]Rule{"blocking": rule},
		ruleDefinitions: map[string]Definition{
			"blocking": {
				ID: "blocking", Entity: EntityConfig{Pattern: pattern},
				OnRecovery: []Action{{Type: ActionTypePublish, Subject: "test.blocked"}},
			},
		},
		stateTracker: tracker,
	}
	processor.commandLane.open()
	processor.statefulEvaluator = NewStatefulEvaluator(tracker, executor, nil)
	watcherCtx, watcherCancel := context.WithCancel(runCtx)
	processor.entityDispatchRecords[key] = managedEntityWatcher{
		watcher: watcher, generation: 1, cancel: watcherCancel, done: make(chan struct{}),
	}
	go func() {
		processor.commandLane.run(runCtx)
		close(processor.runtimeDone)
	}()

	evaluationDone := make(chan struct{})
	go func() {
		processor.dispatchManagedEntityWatchUpdate(runCtx, entityWatchUpdate{
			entityKey: entityID,
			snapshot: entitySnapshot{
				Action: "CREATED", Revision: 1, State: &graph.EntityState{ID: entityID},
			},
		}, true, key, watcher, 1)
		close(evaluationDone)
	}()
	<-executor.started

	stopCtx, cancelStop := context.WithCancel(context.Background())
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- processor.cleanup(stopCtx) }()
	deadline := time.Now().Add(time.Second)
	for {
		processor.commandLane.mu.Lock()
		fenced := processor.commandLane.fenced
		queued := len(processor.commandLane.queue)
		processor.commandLane.mu.Unlock()
		if fenced && queued == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(executor.release)
			<-cleanupDone
			t.Fatal("cleanup did not settle the runtime-command fence")
		}
		runtime.Gosched()
	}
	cancelStop()
	select {
	case err := <-cleanupDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cleanup error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		close(executor.release)
		<-cleanupDone
		t.Fatal("cleanup blocked on the entity dispatch gate after its deadline")
	}
	select {
	case <-evaluationDone:
	case <-time.After(time.Second):
		t.Fatal("Start cancellation did not release admitted bootstrap evaluation")
	}
	watcherCancel()
	_ = watcherCtx
}

func TestConfigManagerStartIsOneShot(t *testing.T) {
	rcm, err := NewConfigManager(slog.Default())
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	if err := rcm.Start(context.Background(), nil); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := rcm.Start(context.Background(), nil); err == nil {
		t.Fatal("duplicate Start succeeded")
	}
	if err := rcm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// fakeRuleTarget records every ApplyConfigUpdate it receives.
type fakeRuleTarget struct {
	loaded  map[string]Definition
	applied chan map[string]any
}

func (f *fakeRuleTarget) LoadedRuleDefinitions() map[string]Definition { return f.loaded }
func (f *fakeRuleTarget) ValidateConfigUpdate(map[string]any) error    { return nil }
func (f *fakeRuleTarget) ApplyConfigUpdate(changes map[string]any) error {
	f.applied <- changes
	return nil
}

// TestConfigManagerStopJoinsTheReconcileLoop proves Stop returns only after
// the loop it started has exited, and that Start after Stop is refused. The
// family is unbound (no config manager started it), so every reconcile fails
// at ListRules and no target is applied — the loop still runs and must join.
func TestConfigManagerStopJoinsTheReconcileLoop(t *testing.T) {
	rcm, err := NewConfigManager(slog.Default())
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	target := &fakeRuleTarget{applied: make(chan map[string]any, 1)}
	if err := rcm.Start(context.Background(), []HotReloadTarget{target}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rcm.lifecycleMu.Lock()
	done := rcm.done
	rcm.lifecycleMu.Unlock()
	if done == nil {
		t.Fatal("Start with a target must run the reconcile loop")
	}
	if err := rcm.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Stop returned before the reconcile loop exited")
	}
	if err := rcm.Start(context.Background(), []HotReloadTarget{target}); err == nil {
		t.Fatal("Start after Stop succeeded")
	}
	select {
	case changes := <-target.applied:
		t.Fatalf("an unbound family must not apply anything, got %v", changes)
	default:
	}
}

func TestRuleCleanupSettlesAdmittedWatcherUpdateBeforeSnapshot(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	watcher := newTransactionalTestWatcher()
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	cfg := mustTestConfig(t, "runtime-watcher-cleanup")
	cfg.EntityWatchBuckets = map[string][]string{}
	processor := &Processor{
		logger:                slog.Default(),
		config:                &cfg,
		runtimeDone:           make(chan struct{}),
		cancel:                cancel,
		entityWatcherMap:      make(map[string]jetstream.KeyWatcher),
		entityDispatchRecords: make(map[string]managedEntityWatcher),
	}
	processor.commandLane.open()
	processor.running = true
	processor.entityWatcherPrepare = func(context.Context, string, string) (jetstream.KeyWatcher, error) {
		close(prepareStarted)
		<-releasePrepare
		return watcher, nil
	}
	go func() {
		processor.commandLane.run(runCtx)
		close(processor.runtimeDone)
	}()

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- processor.UpdateWatchBuckets(map[string][]string{
			graph.BucketEntityStates: {"acme.prod.robotics.*.drone.*"},
		})
	}()
	<-prepareStarted

	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- processor.cleanup(context.Background()) }()
	for {
		processor.commandLane.mu.Lock()
		fenced := processor.commandLane.fenced
		processor.commandLane.mu.Unlock()
		if fenced {
			break
		}
		runtime.Gosched()
	}
	close(releasePrepare)
	if err := <-updateDone; err != nil {
		t.Fatalf("runtime update: %v", err)
	}
	if err := <-cleanupDone; err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if !watcher.stopped.Load() {
		t.Fatal("watcher committed by admitted update escaped cleanup snapshot")
	}
}

func TestRuleCleanupDeadlineCancelsAndJoinsCoordinatorBeforeSnapshot(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(context.Background())
	watcher := newTransactionalTestWatcher()
	prepareStarted := make(chan struct{})
	cfg := mustTestConfig(t, "runtime-watcher-deadline")
	cfg.EntityWatchBuckets = map[string][]string{}
	processor := &Processor{
		logger:                slog.Default(),
		config:                &cfg,
		runtimeDone:           make(chan struct{}),
		cancel:                cancelRun,
		entityWatcherMap:      make(map[string]jetstream.KeyWatcher),
		entityDispatchRecords: make(map[string]managedEntityWatcher),
	}
	processor.commandLane.open()
	processor.running = true
	processor.entityWatcherPrepare = func(commandCtx context.Context, _, _ string) (jetstream.KeyWatcher, error) {
		close(prepareStarted)
		<-commandCtx.Done()
		return watcher, nil
	}
	go func() {
		processor.commandLane.run(runCtx)
		close(processor.runtimeDone)
	}()
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- processor.UpdateWatchBuckets(map[string][]string{
			graph.BucketEntityStates: {"acme.prod.environmental.*.sensor.*"},
		})
	}()
	<-prepareStarted

	stopCtx, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	if err := processor.cleanup(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup error = %v, want context canceled", err)
	}
	if err := <-updateDone; err != nil {
		t.Fatalf("runtime update error = %v, want nil commit after canceled acquisition: %v", err, err)
	}
	select {
	case <-processor.commandLane.done():
	default:
		t.Fatal("cleanup returned before the canceled coordinator joined")
	}
	if !watcher.stopped.Load() {
		t.Fatal("watcher committed during deadline cancellation escaped cleanup snapshot")
	}
}

func TestRuleFailedStartRollsBackPublishedAuthority(t *testing.T) {
	cfg := mustTestConfig(t, "failed-start-rollback")
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}
	// Force the composition check to fail after Start has published and
	// launched its coordinator, but before any NATS-dependent setup.
	processor.initialRulesReady = true
	processor.effectiveContracts = []projection.Contract{{Name: "missing-reconciler"}}

	err = processor.Start(context.WithValue(context.Background(), struct{}{}, "rollback-value"))
	if err == nil {
		t.Fatal("Start succeeded without the required predicate reconciler")
	}
	processor.lifecycleMu.Lock()
	terminal := processor.terminal
	cleanupPending := processor.cleanupPending
	processor.lifecycleMu.Unlock()
	if !terminal || cleanupPending {
		t.Fatalf("failed Start authority: terminal=%v cleanupPending=%v", terminal, cleanupPending)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), processorStopBudget)
	defer cancelStop()
	if err := processor.Stop(stopCtx); err != nil {
		t.Fatalf("Stop after successful failed-Start rollback: %v", err)
	}
	if err := processor.Start(context.Background()); err == nil {
		t.Fatal("one-shot processor accepted Start after failed-Start rollback")
	}
}
