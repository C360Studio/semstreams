package agenticloop

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type loopDeliveryOwnerMsg struct {
	data        []byte
	dataCalls   atomic.Int32
	heartbeats  atomic.Int32
	settlement  atomic.Int32
	metadata    atomic.Int32
	metadataErr error
}

func (m *loopDeliveryOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return m.data }
func (*loopDeliveryOwnerMsg) Subject() string      { return "agent.task.test" }
func (*loopDeliveryOwnerMsg) Reply() string        { return "" }
func (*loopDeliveryOwnerMsg) Headers() nats.Header { return nil }
func (m *loopDeliveryOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadata.Add(1)
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *loopDeliveryOwnerMsg) Ack() error                       { m.settlement.Add(1); return nil }
func (*loopDeliveryOwnerMsg) DoubleAck(context.Context) error    { return nil }
func (m *loopDeliveryOwnerMsg) Nak() error                       { m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) NakWithDelay(time.Duration) error { m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) InProgress() error                { m.heartbeats.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) Term() error                      { m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) TermWithReason(string) error      { m.settlement.Add(1); return nil }

// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestLoopUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner(t *testing.T) {
	retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)
	var workCalls atomic.Int32
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(),
		natsclient.StreamConsumerConfig{BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}},
		15*time.Second,
		retry,
		func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			workCalls.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		},
	)
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission()
	metadataCause := errors.New("metadata unavailable")
	msg := &loopDeliveryOwnerMsg{data: []byte("must-not-run"), metadataErr: metadataCause}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg, policy, admission)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.True(t, result.Quarantined())
	require.True(t, result.OwnerStopRequired())
	require.Contains(t, result.Err().Error(), "delivery_metadata_unavailable")
	require.ErrorIs(t, result.Cause(), metadataCause)
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, workCalls.Load())
	require.Zero(t, msg.heartbeats.Load())
	require.Zero(t, msg.settlement.Load())

	handle := &loopPolicyHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeDeliveryLane(ctx, &binding, admission, "agent.task")
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed admission must not inspect another delivery")
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load(), "fatal and ordinary stop share exact drain-once authority")
	cancel()
	<-binding.observerDone
}

// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestLoopSetupWiresMetadataFailureToAcquiredOwner(t *testing.T) {
	port, err := (component.PortDefinition{
		Name:     "agent.task",
		Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.>"}},
		Required: true,
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)

	handle := &loopPolicyHandle{closed: make(chan struct{})}
	var callback func(context.Context, jetstream.Msg)
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			callback = handler
			return handle, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupConsumer(
		ctx, ctx, port, "agent.task.>", func(context.Context, []byte) error { return nil },
	))
	require.NotNil(t, callback)
	require.Len(t, c.consumers, 1)

	msg := &loopDeliveryOwnerMsg{metadataErr: errors.New("metadata unavailable")}
	callback(ctx, msg)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	callback(ctx, msg)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, msg.settlement.Load())

	cancel()
	<-c.consumers[0].observerDone
}
