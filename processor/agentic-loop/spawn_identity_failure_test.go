package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/message"
)

type inputAckMsg struct {
	data       []byte
	acked      atomic.Bool
	naked      atomic.Bool
	nakCount   atomic.Int32
	nakDelay   atomic.Int64
	inProgress atomic.Int32
	terminated atomic.Bool
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
func (m *inputAckMsg) Term() error {
	m.terminated.Store(true)
	return nil
}
func (m *inputAckMsg) TermWithReason(string) error {
	m.terminated.Store(true)
	return nil
}

// Per-entity poison semantics (poison-response-scoping D9): the typed
// graph_state_reset_required classification means THIS loop's entity is
// poisoned. The loop fails through the normal terminal business-failure
// path with the typed error preserved; no component-wide state changes.
func TestHandleSpawnIdentityFailure_GraphStatePoisonFailsLoopPerEntity(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	const loopID = "loop-poison"
	if _, err := handler.loopManager.CreateLoopWithID(loopID, "task-poison", "researcher", "model"); err != nil {
		t.Fatalf("CreateLoopWithID() error = %v", err)
	}
	before, err := handler.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before error = %v", err)
	}
	if _, err := handler.trajectoryManager.startTrajectory(loopID); err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler.logger = logger
	c := &Component{
		handler:   handler,
		logger:    logger,
		started:   true,
		startTime: time.Now(),
	}
	poison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})

	if gotErr := c.handleSpawnIdentityFailure(context.Background(), loopID, before, poison); gotErr != nil {
		t.Fatalf("handleSpawnIdentityFailure() error = %v, want nil (per-loop failure is fully handled)", gotErr)
	}

	after, err := handler.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() after error = %v", err)
	}
	if after.State != agentic.LoopStateFailed || after.Outcome != agentic.OutcomeFailed {
		t.Fatalf("poisoned loop state=%q outcome=%q, want terminal business failure", after.State, after.Outcome)
	}
	if !strings.Contains(after.Error, graph.ErrorCodeGraphStateResetRequired) {
		t.Fatalf("failed loop Error = %q, want the typed %q code preserved", after.Error, graph.ErrorCodeGraphStateResetRequired)
	}

	// One poisoned entity must not degrade the component: Health stays
	// healthy and no component-wide latch blocks subsequent task intake.
	if health := c.Health(); !health.Healthy || health.Status != "running" {
		t.Fatalf("Health() = %#v, want healthy running (per-entity poison must not degrade the component)", health)
	}
}

func TestGraphStatePoisonRouting_DistinguishesOperationalErrors(t *testing.T) {
	t.Parallel()

	poison := &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity}
	if !graph.IsStateContractError(poison) {
		t.Fatal("StateContractError must route to the per-loop poison failure path")
	}
	remotePoison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		errors.New("remote entity poison classification"))
	if !graph.IsStateContractError(remotePoison) {
		t.Fatal("wire-reconstructed graph_state_reset_required code must route to the per-loop poison failure path")
	}

	operational := errors.New("request timeout")
	if graph.IsStateContractError(operational) {
		t.Fatal("ordinary operational errors must keep the spawn_identity_birth_failed reason")
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
			trajectoryManager: newTrajectoryManager(),
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

// TestGraphStatePoisonFailsLoopWhileIntakeContinues drives the production
// task-intake path end to end (envelope decode → HandleTask → graph birth →
// lineage write) for two tasks. The first task's graph write returns the
// typed poison classification: that loop fails terminally with the typed
// error and its delivery is ACKed. The second task — a different loop /
// different entity — must process normally, proving no component-wide latch
// wedges task intake and Health stays healthy. This deliberately inverts the
// retired hold-until-restart behavior (poison-response-scoping D9).
func TestGraphStatePoisonFailsLoopWhileIntakeContinues(t *testing.T) {
	configJSON, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(configJSON, component.Dependencies{
		Platform:        component.PlatformMeta{Org: "acme", Platform: "ops"},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := discoverable.(*Component)
	// NATS-less Start marks the component running so Health reflects the
	// steady state the assertions below depend on.
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop(time.Second)

	poison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})
	var lineageWrites atomic.Int32
	c.testLineageWriteHook = func(_ context.Context, loopID string, _ map[string]any) error {
		lineageWrites.Add(1)
		if loopID == "loop-poisoned" {
			return poison
		}
		return nil
	}

	consumeTask := func(loopID, taskID string) *inputAckMsg {
		t.Helper()
		task := validLineageTask(taskID)
		task.LoopID = loopID
		task.Metadata = map[string]any{
			agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream-loop"},
		}
		envelope := message.NewBaseMessage(task.Schema(), &task, "test")
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		msg := &inputAckMsg{data: data}
		if err := consumeLongRunningInput(context.Background(), msg, time.Hour,
			c.taskInputHandler(time.Minute)); err != nil {
			t.Fatalf("consume(%s) error = %v, want nil", taskID, err)
		}
		return msg
	}

	// Task 1: touches the poisoned entity. The loop fails terminally with
	// the typed error; the delivery is ACKed per the loop-failure
	// convention (never held in flight, never Term'd as producer fault).
	first := consumeTask("loop-poisoned", "task-poisoned-entity")
	if !first.acked.Load() || first.naked.Load() || first.terminated.Load() {
		t.Fatalf("poisoned-loop delivery ack state: ack=%v nak=%v term=%v, want ACK only",
			first.acked.Load(), first.naked.Load(), first.terminated.Load())
	}
	failed, err := c.handler.GetLoop("loop-poisoned")
	if err != nil {
		t.Fatalf("GetLoop(loop-poisoned) error = %v", err)
	}
	if failed.State != agentic.LoopStateFailed || failed.Outcome != agentic.OutcomeFailed {
		t.Fatalf("poisoned loop state=%q outcome=%q, want terminal failure", failed.State, failed.Outcome)
	}
	if !strings.Contains(failed.Error, graph.ErrorCodeGraphStateResetRequired) {
		t.Fatalf("poisoned loop Error = %q, want the typed %q code preserved", failed.Error, graph.ErrorCodeGraphStateResetRequired)
	}

	// Task 2: a different loop over a different entity processes normally —
	// task intake was never wedged by the first loop's poison.
	second := consumeTask("loop-healthy", "task-healthy-entity")
	if !second.acked.Load() || second.naked.Load() || second.terminated.Load() {
		t.Fatalf("healthy-loop delivery ack state: ack=%v nak=%v term=%v, want ACK only",
			second.acked.Load(), second.naked.Load(), second.terminated.Load())
	}
	healthy, err := c.handler.GetLoop("loop-healthy")
	if err != nil {
		t.Fatalf("GetLoop(loop-healthy) error = %v", err)
	}
	if healthy.State == agentic.LoopStateFailed {
		t.Fatalf("healthy loop state = %q, want non-failed active state", healthy.State)
	}
	if got := lineageWrites.Load(); got != 2 {
		t.Fatalf("lineage writes = %d, want 2 (second task reached its graph write)", got)
	}

	// Health never degrades for this class: per-entity poison is a loop
	// outcome, not a component condition.
	if health := c.Health(); !health.Healthy || health.Status != "running" {
		t.Fatalf("Health() = %#v, want healthy running after a poisoned loop", health)
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
