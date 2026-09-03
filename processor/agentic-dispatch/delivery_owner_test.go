package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type dispatchFastOwnerMsg struct {
	data       []byte
	dataCalls  atomic.Int32
	settlement atomic.Int32
	acks       atomic.Int32
	naks       atomic.Int32
	terms      atomic.Int32
}

type dispatchHealthProbeHandle struct {
	drains  atomic.Int32
	closed  chan struct{}
	onDrain func()
}

func (*dispatchHealthProbeHandle) Stop() {}
func (h *dispatchHealthProbeHandle) Drain() {
	if h.drains.Add(1) == 1 && h.onDrain != nil {
		h.onDrain()
	}
}
func (h *dispatchHealthProbeHandle) Closed() <-chan struct{} { return h.closed }

func (m *dispatchFastOwnerMsg) Data() []byte {
	m.dataCalls.Add(1)
	if m.data != nil {
		return m.data
	}
	return []byte("work")
}
func (*dispatchFastOwnerMsg) Subject() string      { return "user.message.test" }
func (*dispatchFastOwnerMsg) Reply() string        { return "" }
func (*dispatchFastOwnerMsg) Headers() nats.Header { return nil }
func (*dispatchFastOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *dispatchFastOwnerMsg) Ack() error {
	m.settlement.Add(1)
	m.acks.Add(1)
	return nil
}
func (*dispatchFastOwnerMsg) DoubleAck(context.Context) error { return nil }
func (m *dispatchFastOwnerMsg) Nak() error {
	m.settlement.Add(1)
	m.naks.Add(1)
	return nil
}
func (m *dispatchFastOwnerMsg) NakWithDelay(time.Duration) error { return m.Nak() }
func (*dispatchFastOwnerMsg) InProgress() error                  { return nil }
func (m *dispatchFastOwnerMsg) Term() error {
	m.settlement.Add(1)
	m.terms.Add(1)
	return nil
}
func (m *dispatchFastOwnerMsg) TermWithReason(string) error { return m.Term() }

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchFastOwnerPanicQuarantinesAndDrainsExactHandle(t *testing.T) {
	fatalCause := errors.New("dispatch fast delivery work panicked: boom")
	c := &Component{started: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	admission := newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
	msg := &dispatchFastOwnerMsg{}
	decision, admitted, err := consumeAdmittedDispatchFastDelivery(
		t.Context(), msg,
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) { panic("boom") },
		admission,
	)
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	require.ErrorContains(t, err, "panicked: boom")
	require.Zero(t, msg.settlement.Load(), "quarantine must not settle an unsafe delivery")

	handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeDeliveryLane(ctx, &binding, admission)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Equal(t, 1, health.ErrorCount)
	require.Equal(t, fatalCause.Error(), health.LastError)

	_, admitted, _ = consumeAdmittedDispatchFastDelivery(
		t.Context(), msg,
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		},
		admission,
	)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.dataCalls.Load(), "closed admission must refuse work before reading payload")
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load())
	cancel()
	<-binding.observerDone
}

func TestTerminalDeliveryFatalBuffersBeforeHandleAndDrainsExactHandleOnce(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	require.True(t, result.OwnerStopRequired())
	observed := make(chan error, 1)
	c := &Component{
		started:                true,
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		terminalDeliveryDoneFn: func(err error) { observed <- err },
	}
	admission := newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
	admission.latch(result)
	require.Len(t, admission.fatal, 1)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Error(t, errors.New(health.LastError))

	handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeDeliveryLane(ctx, &binding, admission)
	select {
	case err := <-observed:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("fatal result was not observed")
	}
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load())
	require.False(t, admission.admit())
	cancel()
	<-binding.observerDone
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDeliveryOwnerFatalHealthKeepsFirstCauseAcrossLanes(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	c := &Component{started: true}
	first := errors.New("first owner fatal")
	c.recordDeliveryOwnerFatal(first)
	newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal).latch(result)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Equal(t, 1, health.ErrorCount)
	require.Equal(t, first.Error(), health.LastError)
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchFatalHealthLatchesBeforeEachExactOwnerDrain(t *testing.T) {
	c := &Component{started: true, startTime: time.Now(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	firstCause := errors.New("first dispatch owner lost")
	secondCause := errors.New("later dispatch owner lost")
	healthAtDrain := make(chan component.HealthStatus, 2)
	firstAdmission := newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
	secondAdmission := newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
	firstHandle := &dispatchHealthProbeHandle{closed: make(chan struct{}), onDrain: func() { healthAtDrain <- c.Health() }}
	secondHandle := &dispatchHealthProbeHandle{closed: make(chan struct{}), onDrain: func() { healthAtDrain <- c.Health() }}
	firstBinding := newStreamConsumerBinding(firstHandle)
	secondBinding := newStreamConsumerBinding(secondHandle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeDeliveryLane(ctx, &firstBinding, firstAdmission)
	c.observeDeliveryLane(ctx, &secondBinding, secondAdmission)

	firstAdmission.latchFatal(firstCause)
	firstHealth := <-healthAtDrain
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status)
	require.Equal(t, firstCause.Error(), firstHealth.LastError)
	require.Equal(t, 1, firstHealth.ErrorCount)
	require.Equal(t, int32(1), firstHandle.drains.Load())
	require.Zero(t, secondHandle.drains.Load())

	secondAdmission.latchFatal(secondCause)
	secondHealth := <-healthAtDrain
	require.Equal(t, firstHealth.LastError, secondHealth.LastError)
	require.Equal(t, firstHealth.ErrorCount, secondHealth.ErrorCount)
	require.Equal(t, int32(1), firstHandle.drains.Load())
	require.Equal(t, int32(1), secondHandle.drains.Load())

	cancel()
	<-firstBinding.observerDone
	<-secondBinding.observerDone
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchFastProductionBindingsUseDeclaredOwner(t *testing.T) {
	deps := componentDependenciesForCausalTest()
	deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
	discoverable, err := NewComponent([]byte(`{}`), deps)
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	responses := make([]agentic.UserResponse, 0, 1)
	c.sendResponseFn = func(response agentic.UserResponse) { responses = append(responses, response) }
	type capturedBinding struct {
		cfg      natsclient.StreamConsumerConfig
		callback func(context.Context, jetstream.Msg)
	}
	captured := make(map[string]capturedBinding)
	handles := make([]*causalConsumeHandle, 0, 5)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
		handles = append(handles, handle)
		captured[owner.Port] = capturedBinding{cfg: cfg, callback: callback}
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx))

	for _, port := range []string{"user.message", "agent.created", "agent.approval_pending"} {
		binding, ok := captured[port]
		require.True(t, ok, "production setup did not bind %s", port)
		require.Equal(t, dispatchFastDeliveryAckWait, binding.cfg.AckWait)
		require.Equal(t, dispatchFastDeliveryAckWait, binding.cfg.MessageTimeout)
		data := []byte("{")
		switch port {
		case "user.message":
			userMessage := &agentic.UserMessage{
				MessageID: "message-1", ChannelType: "cli", ChannelID: "channel-1",
				UserID: "user-1", Content: "/help", Timestamp: time.Now().UTC(),
			}
			data = mustMarshalDispatchPayload(t, userMessage)
		case "agent.created":
			created := &agentic.LoopCreatedEvent{
				LoopID: "00000000-0000-4000-8000-000000000001", TaskID: "task-1",
				Role: "research", MaxIterations: 3, CreatedAt: time.Now().UTC(),
			}
			data = mustMarshalDispatchPayload(t, created)
		case "agent.approval_pending":
			pending := &agentic.ApprovalPendingEvent{
				LoopID: "00000000-0000-4000-8000-000000000001", CallID: "call-1",
				ToolName: "search", RequestedAt: time.Now().UTC(),
			}
			data = mustMarshalDispatchPayload(t, pending)
		}
		msg := &dispatchFastOwnerMsg{data: data}
		binding.callback(ctx, msg)
		require.Equal(t, int32(1), msg.dataCalls.Load(), "%s must execute its production handler", port)
		require.Equal(t, int32(1), msg.acks.Load(), "%s successful consequence must ACK", port)
		require.Zero(t, msg.naks.Load(), "%s must not retry successful work", port)
		require.Zero(t, msg.terms.Load(), "%s must not terminate valid work", port)
	}
	tracked := c.loopTracker.Get("00000000-0000-4000-8000-000000000001")
	require.Len(t, responses, 1, "user.message command must produce its required user response")
	require.Equal(t, agentic.ResponseTypeText, responses[0].Type)
	require.Contains(t, responses[0].Content, "/help")
	require.NotNil(t, tracked, "agent.created callback must update the production tracker")
	require.NotNil(t, tracked.PendingApproval, "approval callback must update the production tracker")
	require.Equal(t, "call-1", tracked.PendingApproval.CallID)

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

func mustMarshalDispatchPayload(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	encoded, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
	return encoded
}
