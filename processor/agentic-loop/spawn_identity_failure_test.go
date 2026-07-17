package agenticloop

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type inputAckMsg struct {
	data       []byte
	acked      atomic.Bool
	naked      atomic.Bool
	nakCount   atomic.Int32
	nakDelay   atomic.Int64
	inProgress atomic.Int32
	progress   chan struct{}
}

func (m *inputAckMsg) Data() []byte                              { return m.data }
func (m *inputAckMsg) Subject() string                           { return "agent.task.test" }
func (m *inputAckMsg) Reply() string                             { return "" }
func (m *inputAckMsg) Headers() nats.Header                      { return nil }
func (m *inputAckMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *inputAckMsg) Ack() error {
	m.acked.Store(true)
	return nil
}
func (m *inputAckMsg) DoubleAck(context.Context) error { return nil }
func (m *inputAckMsg) Nak() error {
	m.naked.Store(true)
	m.nakCount.Add(1)
	return nil
}
func (m *inputAckMsg) NakWithDelay(delay time.Duration) error {
	m.naked.Store(true)
	m.nakCount.Add(1)
	m.nakDelay.Store(int64(delay))
	return nil
}
func (m *inputAckMsg) InProgress() error {
	m.inProgress.Add(1)
	if m.progress != nil {
		m.progress <- struct{}{}
	}
	return nil
}
func (m *inputAckMsg) Term() error                 { return nil }
func (m *inputAckMsg) TermWithReason(string) error { return nil }

func TestHandleSpawnIdentityFailure_GraphStatePoisonHasNoBusinessSideEffects(t *testing.T) {
	t.Parallel()

	loopManager := NewLoopManager()
	const loopID = "loop-poison"
	if _, err := loopManager.CreateLoopWithID(loopID, "task-poison", "researcher", "model"); err != nil {
		t.Fatalf("CreateLoopWithID() error = %v", err)
	}
	before, err := loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before error = %v", err)
	}

	trajectoryManager := NewTrajectoryManager()
	if _, err := trajectoryManager.StartTrajectory(loopID); err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}
	c := &Component{
		handler: &MessageHandler{loopManager: loopManager, trajectoryManager: trajectoryManager},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		// All business-output dependencies intentionally remain nil. Reaching
		// handleLoopFailure would mutate the loop before attempting those outputs.
	}
	loopManager.CacheResponseFormat(loopID, &agentic.ResponseFormat{Type: agentic.ResponseFormatJSONObject})
	poison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})

	poisonCtx, cancelPoison := context.WithCancel(context.Background())
	cancelPoison()
	gotErr := c.handleSpawnIdentityFailure(poisonCtx, loopID, before, poison)
	var classified *errs.ClassifiedError
	if !errors.As(gotErr, &classified) ||
		classified.Class != errs.ErrorFatal ||
		classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("handleSpawnIdentityFailure() error = %#v, want fatal/%q", classified, graph.ErrorCodeGraphStateResetRequired)
	}

	if _, err := loopManager.GetLoop(loopID); err == nil {
		t.Fatal("poisoned pre-birth loop remained active; want in-memory creation rollback")
	}
	if _, err := trajectoryManager.GetTrajectory(loopID); err == nil {
		t.Fatal("poisoned pre-birth trajectory remained active; want in-memory creation rollback")
	}
	if cached := loopManager.GetCachedResponseFormat(loopID); cached != nil {
		t.Fatalf("poisoned pre-birth response format remained cached: %#v", cached)
	}
	if health := c.Health(); health.Healthy || health.Status != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("Health() = %#v, want unhealthy %q", health, graph.ErrorCodeGraphStateResetRequired)
	}
	// The reset latch is checked before decoding or loop creation. A nil decoder
	// would panic if this task reached ordinary intake.
	latchedCtx, cancelLatched := context.WithCancel(context.Background())
	cancelLatched()
	if err := c.handleTaskMessage(latchedCtx, []byte("not decoded while reset-required")); !errors.Is(err, gotErr) {
		t.Fatalf("latched task error = %v, want reset-required error %v", err, gotErr)
	}
}

func TestGraphStatePoisonRouting_DistinguishesOperationalErrors(t *testing.T) {
	t.Parallel()

	poison := &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity}
	if !graph.IsStateContractError(poison) {
		t.Fatal("StateContractError must route to the reset-required fail-closed path")
	}
	remotePoison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		errors.New("remote graph reset required"))
	if !graph.IsStateContractError(remotePoison) {
		t.Fatal("wire-reconstructed reset-required code must route to the fail-closed path")
	}

	operational := errors.New("request timeout")
	if graph.IsStateContractError(operational) {
		t.Fatal("ordinary operational errors must continue to handleLoopFailure")
	}
}

func TestHandleSpawnIdentityFailure_OperationalErrorUsesBusinessFailurePath(t *testing.T) {
	t.Parallel()

	loopManager := NewLoopManager()
	const loopID = "loop-operational"
	if _, err := loopManager.CreateLoopWithID(loopID, "task-operational", "researcher", "model"); err != nil {
		t.Fatalf("CreateLoopWithID() error = %v", err)
	}
	entity, err := loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before error = %v", err)
	}
	// Force failure-event serialization to stop before NATS output. The loop
	// transition and completion mutation occur first, which is the business
	// failure routing this test locks without needing an external broker.
	entity.Metadata = map[string]any{"unserializable": func() {}}
	if err := loopManager.UpdateLoop(entity); err != nil {
		t.Fatalf("UpdateLoop() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		handler: &MessageHandler{
			loopManager:       loopManager,
			trajectoryManager: NewTrajectoryManager(),
			logger:            logger,
		},
		logger: logger,
	}
	if err := c.handleSpawnIdentityFailure(context.Background(), loopID, entity, errors.New("temporary graph request failure")); err != nil {
		t.Fatalf("operational birth failure returned consumer error: %v", err)
	}

	after, err := loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() after error = %v", err)
	}
	if after.State != agentic.LoopStateFailed || after.Outcome != agentic.OutcomeFailed {
		t.Fatalf("operational error did not use business failure path: state=%q outcome=%q", after.State, after.Outcome)
	}
}

func TestConsumeLongRunningInput_LatchedGraphPoisonHoldsUntilCancellation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	resetErr := graph.ClassifyStateContractError(&graph.StateContractError{
		Reason: graph.GraphStateReasonNoncanonicalEntityID,
	})
	c := &Component{logger: logger}
	c.graphStateReset.Store(&graphStateResetLatch{err: resetErr})
	msg := &inputAckMsg{
		data:     []byte("must remain pending"),
		progress: make(chan struct{}, 16),
	}
	ctx, cancel := context.WithCancel(context.Background())
	handler := c.taskInputHandler(ctx, time.Millisecond)
	done := make(chan error, 1)
	go func() {
		done <- consumeLongRunningInput(ctx, msg, time.Millisecond, handler)
	}()

	// More heartbeats than both the 1ms ordinary work timeout and configured
	// MaxDeliver=2 prove this is one lifecycle-held delivery, not repeated
	// NAK/redelivery consuming the finite budget.
	for i := 0; i < 3; i++ {
		select {
		case <-msg.progress:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for InProgress heartbeat")
		}
	}
	if msg.acked.Load() {
		t.Fatal("reset-required task was ACKed during operator repair")
	}
	if msg.naked.Load() || msg.nakCount.Load() != 0 {
		t.Fatal("reset-required task was NAKed during operator repair and consumed delivery budget")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consumer cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not release held task on cancellation")
	}
	if msg.acked.Load() {
		t.Fatal("reset-required task was ACKed on cancellation")
	}
	if !msg.naked.Load() || msg.nakCount.Load() != 1 || time.Duration(msg.nakDelay.Load()) != 5*time.Second {
		t.Fatalf("cancellation NAK count=%d delay=%s, want exactly one 5s delayed NAK", msg.nakCount.Load(), time.Duration(msg.nakDelay.Load()))
	}
}

func TestConsumerLifecycleContext_DetachesStartupDeadlineButPreservesValues(t *testing.T) {
	t.Parallel()

	type traceKey struct{}
	startCtx := context.WithValue(context.Background(), traceKey{}, "trace-value")
	startCtx, expireStartup := context.WithCancel(startCtx)
	lifecycleCtx, stop := newConsumerLifecycleContext(startCtx)
	defer stop()

	expireStartup()
	if got := lifecycleCtx.Value(traceKey{}); got != "trace-value" {
		t.Fatalf("lifecycle trace value = %v, want preserved value", got)
	}
	select {
	case <-lifecycleCtx.Done():
		t.Fatal("startup cancellation leaked into consumer lifecycle")
	default:
	}
	stop()
	select {
	case <-lifecycleCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("component lifecycle cancellation did not stop consumer context")
	}
}

func TestCleanupConsumersAfterStartFailure_CancelsAndResetsState(t *testing.T) {
	t.Parallel()

	var cancelled atomic.Bool
	c := &Component{
		natsClient: &natsclient.Client{},
		consumerCancel: func() {
			cancelled.Store(true)
		},
		consumerInfos: []consumerInfo{{streamName: "AGENT", consumerName: "partial-start"}},
	}

	c.cleanupConsumersAfterStartFailure()

	if !cancelled.Load() {
		t.Fatal("partial-start consumer lifecycle was not cancelled")
	}
	if c.consumerCancel != nil || c.consumerInfos != nil {
		t.Fatalf("partial-start consumer state not reset: cancel=%v infos=%v", c.consumerCancel != nil, c.consumerInfos)
	}
}
