package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
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

type dispatchSettlementMsg struct {
	data  []byte
	acks  atomic.Int32
	naks  atomic.Int32
	terms atomic.Int32
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchProductionCallbacksDoNotAckFalseDone(t *testing.T) {
	t.Run("unknown task publication quarantines", func(t *testing.T) {
		deps := componentDependenciesForCausalTest()
		deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
		discoverable, err := NewComponent([]byte(`{}`), deps)
		require.NoError(t, err)
		c := discoverable.(*Component)
		c.modelRegistry = newTestRegistry()
		withPersistedLoops(c, nil)
		c.waitForStreamInput = func(context.Context, string) error { return nil }
		callbacks := make(map[string]func(context.Context, jetstream.Msg))
		handles := make(map[string]*causalConsumeHandle)
		c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
			callbacks[owner.Port] = callback
			handles[owner.Port] = handle
			return handle, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		require.NoError(t, c.setupSubscriptions(ctx))

		msg := &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: "message-failed-publish", ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
			Content: "do the work", Timestamp: time.Now().UTC(),
		})}
		callbacks["user.message"](ctx, msg)
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
		require.Eventually(t, func() bool { return handles["user.message"].drains.Load() == 1 }, time.Second, time.Millisecond)
		for port, handle := range handles {
			if port != "user.message" {
				require.Zero(t, handle.drains.Load(), "task failure drained unrelated owner %s", port)
			}
		}
		require.Contains(t, c.Health().LastError, "unknown durable state")
		cancel()
		for _, binding := range c.consumers {
			<-binding.observerDone
		}
	})

	t.Run("unaccepted pending projection retries", func(t *testing.T) {
		deps := componentDependenciesForCausalTest()
		deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
		discoverable, err := NewComponent([]byte(`{}`), deps)
		require.NoError(t, err)
		c := discoverable.(*Component)
		c.waitForStreamInput = func(context.Context, string) error { return nil }
		callbacks := make(map[string]func(context.Context, jetstream.Msg))
		c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			callbacks[owner.Port] = callback
			return &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		require.NoError(t, c.setupSubscriptions(ctx))
		for i := range pendingApprovalBufferCap {
			require.False(t, c.loopTracker.SetPendingApproval(fmt.Sprintf("buffered-%d", i), &PendingApprovalInfo{CallID: "call"}))
		}

		msg := &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.ApprovalPendingEvent{
			LoopID: "00000000-0000-4000-8000-000000000099", CallID: "call-overflow", ToolName: "search", RequestedAt: time.Now().UTC(),
		})}
		callbacks["agent.approval_pending"](ctx, msg)
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Equal(t, int32(1), msg.naks.Load())
		cancel()
		for _, binding := range c.consumers {
			<-binding.observerDone
		}
	})
}

func (m *dispatchSettlementMsg) Data() []byte                            { return m.data }
func (*dispatchSettlementMsg) Subject() string                           { return "dispatch.test" }
func (*dispatchSettlementMsg) Reply() string                             { return "" }
func (*dispatchSettlementMsg) Headers() nats.Header                      { return nil }
func (*dispatchSettlementMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *dispatchSettlementMsg) Ack() error                              { m.acks.Add(1); return nil }
func (*dispatchSettlementMsg) DoubleAck(context.Context) error           { return nil }
func (m *dispatchSettlementMsg) Nak() error                              { m.naks.Add(1); return nil }
func (m *dispatchSettlementMsg) NakWithDelay(time.Duration) error        { m.naks.Add(1); return nil }
func (*dispatchSettlementMsg) InProgress() error                         { return nil }
func (m *dispatchSettlementMsg) Term() error                             { m.terms.Add(1); return nil }
func (m *dispatchSettlementMsg) TermWithReason(string) error             { return m.Term() }

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
	require.Equal(t, result.Err().Error(), health.LastError)

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

func TestDeliveryFatalHealthKeepsFirstCauseAcrossLanes(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	c := &Component{started: true}
	newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal).latch(result)
	first := c.Health()
	newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal).latch(result)
	second := c.Health()
	require.False(t, first.Healthy)
	require.Equal(t, "delivery ownership lost", first.Status)
	require.Equal(t, 1, first.ErrorCount)
	require.Equal(t, first.LastError, second.LastError)
	require.Equal(t, first.ErrorCount, second.ErrorCount)
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchProductionCallbacksTerminateMalformedNonHeartbeatInputs(t *testing.T) {
	deps := componentDependenciesForCausalTest()
	deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
	discoverable, err := NewComponent([]byte(`{}`), deps)
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	responses := make([]agentic.UserResponse, 0, 1)
	c.sendResponseFn = func(response agentic.UserResponse) { responses = append(responses, response) }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx))

	for _, port := range []string{"user.message", "agent.created", "agent.approval_pending"} {
		callback, ok := callbacks[port]
		require.True(t, ok, "production setup did not bind %s", port)
		msg := &dispatchSettlementMsg{data: []byte("{")}
		callback(ctx, msg)
		require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)
		require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)
		require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)
	}

	loopID := "00000000-0000-4000-8000-000000000001"
	valid := map[string][]byte{
		"user.message": mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: "message-1", ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
			Content: "/help", Timestamp: time.Now().UTC(),
		}),
		"agent.created": mustMarshalDispatchSettlementPayload(t, &agentic.LoopCreatedEvent{
			LoopID: loopID, TaskID: "task-1", Role: "research", MaxIterations: 3, CreatedAt: time.Now().UTC(),
		}),
		"agent.approval_pending": mustMarshalDispatchSettlementPayload(t, &agentic.ApprovalPendingEvent{
			LoopID: loopID, CallID: "call-1", ToolName: "search", RequestedAt: time.Now().UTC(),
		}),
	}
	for _, port := range []string{"user.message", "agent.created", "agent.approval_pending"} {
		msg := &dispatchSettlementMsg{data: valid[port]}
		callbacks[port](ctx, msg)
		require.Equal(t, int32(1), msg.acks.Load(), "%s successful declared consequence must ACK", port)
		require.Zero(t, msg.naks.Load()+msg.terms.Load())
	}
	require.Len(t, responses, 1)
	require.Contains(t, responses[0].Content, "/help")
	tracked := c.loopTracker.Get(loopID)
	require.NotNil(t, tracked)
	require.NotNil(t, tracked.PendingApproval)
	require.Equal(t, "call-1", tracked.PendingApproval.CallID)

	cancel()
	for _, binding := range c.consumers {
		if binding.observerDone != nil {
			<-binding.observerDone
		}
	}
}

func mustMarshalDispatchSettlementPayload(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	encoded, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
	return encoded
}

var _ component.Discoverable = (*Component)(nil)
