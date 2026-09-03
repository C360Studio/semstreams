package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type dispatchFastOwnerMsg struct {
	dataCalls  atomic.Int32
	settlement atomic.Int32
}

func (m *dispatchFastOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return []byte("work") }
func (*dispatchFastOwnerMsg) Subject() string      { return "user.message.test" }
func (*dispatchFastOwnerMsg) Reply() string        { return "" }
func (*dispatchFastOwnerMsg) Headers() nats.Header { return nil }
func (*dispatchFastOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *dispatchFastOwnerMsg) Ack() error                       { m.settlement.Add(1); return nil }
func (*dispatchFastOwnerMsg) DoubleAck(context.Context) error    { return nil }
func (m *dispatchFastOwnerMsg) Nak() error                       { m.settlement.Add(1); return nil }
func (m *dispatchFastOwnerMsg) NakWithDelay(time.Duration) error { m.settlement.Add(1); return nil }
func (*dispatchFastOwnerMsg) InProgress() error                  { return nil }
func (m *dispatchFastOwnerMsg) Term() error                      { m.settlement.Add(1); return nil }
func (m *dispatchFastOwnerMsg) TermWithReason(string) error      { m.settlement.Add(1); return nil }

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchFastOwnerPanicQuarantinesAndDrainsExactHandle(t *testing.T) {
	admission := newDeliveryLaneAdmission(nil)
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
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeDeliveryLane(ctx, &binding, admission)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

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
	admission := newDeliveryLaneAdmission(c.recordAgentCompleteFatal)
	admission.latch(result)
	require.Len(t, admission.fatal, 1)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Contains(t, health.LastError, "agent.complete")

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

func TestTerminalLaneFatalHealthFailsClosedIndependently(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	tests := []struct {
		name   string
		lane   string
		record func(*Component, error)
	}{
		{name: "complete", lane: "agent.complete", record: (*Component).recordAgentCompleteFatal},
		{name: "failed", lane: "agent.failed", record: (*Component).recordAgentFailedFatal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Component{started: true}
			admission := newDeliveryLaneAdmission(func(err error) { tt.record(c, err) })
			admission.latch(result)
			health := c.Health()
			require.False(t, health.Healthy)
			require.Equal(t, "terminal delivery ownership lost", health.Status)
			require.Equal(t, 1, health.ErrorCount)
			require.Contains(t, health.LastError, tt.lane)
		})
	}
}
