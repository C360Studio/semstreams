package rule

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
)

// The existing logger provides a startup boundary without a production test hook.
type ruleStartupGateLogger struct {
	slog.Handler
	entered chan struct{}
	release chan struct{}
}

// The owner-local accepted-Start seam avoids NATS setup; Stop is the real terminal
// owner. Its expired bound permits a delayed readiness worker to remain unjoined,
// but must not revoke that worker's exact completion channel.
func TestRuleReadinessCompletionSurvivesStopDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := mustTestConfig(t, "readiness-stop-deadline")
		processor, err := NewProcessor(nil, &cfg)
		if err != nil {
			t.Fatal(err)
		}
		startCtx, cancelStart := context.WithCancel(context.Background())
		defer cancelStart()
		runCtx, startDone, err := processor.beginStartAuthority(startCtx)
		if err != nil {
			t.Fatal(err)
		}
		runtimeDone := processor.runtimeDone
		go func() {
			processor.commandLane.run(runCtx)
			close(runtimeDone)
		}()
		statusDone := make(chan struct{})
		processor.statusLoopDone = statusDone
		releaseWorker := make(chan struct{})
		workerReturned := make(chan any, 1)
		go func(done chan struct{}) {
			defer func() { workerReturned <- recover() }()
			<-releaseWorker
			processor.statusMetricsLoop(runCtx, done)
		}(statusDone)
		if err := processor.finishStartAttempt(startCtx, startDone, true, nil); err != nil {
			t.Fatal(err)
		}

		stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
		defer cancelStop()
		stopErr := processor.Stop(stopCtx)
		if !errors.Is(stopErr, context.DeadlineExceeded) || stopCtx.Err() != context.DeadlineExceeded {
			t.Errorf("Stop must preserve its exact deadline: stop=%v context=%v", stopErr, stopCtx.Err())
		}
		if processor.statusLoopDone != nil {
			t.Error("Stop did not clear its terminal handle")
		}
		select {
		case <-statusDone:
			t.Error("readiness completion announced before delayed worker entry")
		default:
		}
		close(releaseWorker)
		if panicValue := <-workerReturned; panicValue != nil {
			t.Fatalf("delayed readiness worker panicked after Stop: %v", panicValue)
		}
		select {
		case <-statusDone:
		default:
			t.Fatal("delayed readiness worker did not close its original completion channel")
		}
	})
}

func (h *ruleStartupGateLogger) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "Failed to initialize state tracker, stateful rules will be disabled" {
		close(h.entered)
		<-h.release
	}
	return h.Handler.Handle(ctx, record)
}

// component-lifecycle: accepted Start cancellation must not publish completion
// while Start still owns registration; failed-Start rollback joins that same owner.
func TestRuleRuntimeCompletionWaitsForStartupRegistration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := natsclient.NewClient("nats://localhost:4222")
		if err != nil {
			t.Fatal(err)
		}
		// Keep NATS disconnected: the real Start must roll back after the gate.
		cfg := mustTestConfig(t, "startup-registration")
		processor, err := NewProcessor(client, &cfg)
		if err != nil {
			t.Fatal(err)
		}
		gate := &ruleStartupGateLogger{
			Handler: slog.NewTextHandler(io.Discard, nil),
			entered: make(chan struct{}), release: make(chan struct{}),
		}
		processor.logger = slog.New(gate)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan error, 1)
		go func() { started <- processor.Start(ctx) }()
		<-gate.entered
		processor.lifecycleMu.Lock()
		runtimeDone := processor.runtimeDone
		processor.lifecycleMu.Unlock()
		coordinatorDone := processor.commandLane.done()
		cancel()
		<-coordinatorDone
		synctest.Wait()
		select {
		case <-runtimeDone:
			t.Error("runtime completed while Start still owns worker registration")
		default:
		}

		close(gate.release)
		if err := <-started; err == nil {
			t.Fatal("Start unexpectedly succeeded without a connected NATS client")
		}
		select {
		case <-runtimeDone:
		default:
			t.Fatal("failed-Start rollback returned before its exact runtime joined")
		}
		if !processor.terminal || processor.cleanupPending {
			t.Fatal("failed-Start rollback did not release its cleanup authority")
		}
		if err := processor.Stop(context.Background()); err != nil {
			t.Fatalf("completed Stop: %v", err)
		}
	})
}

// startRuleRuntimeForTest drives the NATS-free accepted-Start seam: the real
// Start authority, the real runtime lane on its run context, and a committed
// Start, so Stop runs its production cleanup.
func startRuleRuntimeForTest(t *testing.T, packID string) (*Processor, context.CancelFunc) {
	t.Helper()
	cfg := mustTestConfig(t, packID)
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	startCtx, cancelStart := context.WithCancel(context.Background())
	runCtx, startDone, err := processor.beginStartAuthority(startCtx)
	if err != nil {
		cancelStart()
		t.Fatal(err)
	}
	runtimeDone := processor.runtimeDone
	go func() {
		processor.commandLane.run(runCtx)
		close(runtimeDone)
	}()
	if err := processor.finishStartAttempt(startCtx, startDone, true, nil); err != nil {
		cancelStart()
		t.Fatal(err)
	}
	return processor, cancelStart
}

// The settle helper's deadline arm (#1283 Q-E): an admitted runtime command
// that waits on its runtime context holds the fence barrier past the Stop
// bound. Stop must cancel the runtime, receive the barrier, and join the lane
// — all at the virtual deadline — and the submitter learns the cancellation.
func TestRuleStopDeadlineArmCancelsAndJoinsCoordinator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		processor, cancelStart := startRuleRuntimeForTest(t, "stop-deadline-arm")
		defer cancelStart()
		submitted := make(chan error, 1)
		go func() {
			submitted <- processor.submitRuntimeCommand(func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			})
		}()
		synctest.Wait() // the command is running on the lane

		stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
		defer cancelStop()
		if err := processor.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want its deadline", err)
		}
		select {
		case <-processor.commandLane.done():
		default:
			t.Fatal("Stop returned before the canceled lane ended")
		}
		if err := <-submitted; !errors.Is(err, context.Canceled) {
			t.Fatalf("submitter = %v, want context.Canceled", err)
		}
	})
}

// rp.mu is the one guard on the message cache handle (#1283 Q-E). The handler's
// read is woken by a virtual timer, which gives Stop's clear no happens-before
// edge to it, so under -race an unguarded read is reported deterministically.
func TestRuleMessageCacheOneGuard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		processor, cancelStart := startRuleRuntimeForTest(t, "message-cache-guard")
		defer cancelStart()
		processor.mu.Lock()
		processor.messageCache = cache.NewNoop[message.Message]()
		processor.mu.Unlock()

		evaluated := make(chan struct{})
		go func() {
			defer close(evaluated)
			time.Sleep(time.Hour)
			processor.evaluateRulesForMessage(context.Background(), "test.subject", nil)
		}()
		if err := processor.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		<-evaluated
		processor.mu.RLock()
		cleared := processor.messageCache == nil
		processor.mu.RUnlock()
		if !cleared {
			t.Fatal("Stop did not clear the message cache handle")
		}
	})
}

// A managed watcher whose runtime was cleared must not get a goroutine that no
// Stop joins (#1283 Q-E): the spawn is refused, its record released, the
// watcher stopped, and the refusal logged with its key.
func TestRuleManagedWatcherSpawnRefusedAfterRuntimeEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		watcher := newTransactionalTestWatcher()
		key := watcherKey(graph.BucketEntityStates, "acme.prod.robotics.*.drone.*")
		done := make(chan struct{})
		processor := &Processor{
			logger:                slog.New(slog.NewTextHandler(&logs, nil)),
			entityDispatchRecords: map[string]managedEntityWatcher{key: {watcher: watcher, generation: 1, done: done}},
		}
		processor.clearLifecycleHandles()

		processor.startManagedEntityWatcher(context.Background(), watcher, key, 1)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("managed watcher spawned after its runtime was cleared")
		}
		if !watcher.stopped.Load() {
			t.Fatal("refused watcher was not stopped")
		}
		if got := logs.String(); !strings.Contains(got, "Refused managed entity watcher") || !strings.Contains(got, key) {
			t.Fatalf("refusal not logged with its key: %q", got)
		}
	})
}
