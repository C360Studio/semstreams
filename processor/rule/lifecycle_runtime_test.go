package rule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/c360studio/semstreams/natsclient"
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
			processor.runRuntimeCoordinator(runCtx)
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
		coordinatorDone := processor.coordinatorDone
		processor.lifecycleMu.Unlock()
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
