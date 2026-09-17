package agentictools

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

type deliveryOwnerMsg struct {
	data         []byte
	dataCalls    atomic.Int32
	heartbeats   atomic.Int32
	settlement   atomic.Int32
	metadata     atomic.Int32
	metadataN    uint64
	metadataErr  error
	metadataNil  bool
	metadataZero bool
	ackErr       error
}

func (m *deliveryOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return m.data }
func (*deliveryOwnerMsg) Subject() string      { return "tool.delivery" }
func (*deliveryOwnerMsg) Reply() string        { return "" }
func (*deliveryOwnerMsg) Headers() nats.Header { return nil }
func (m *deliveryOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadata.Add(1)
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	if m.metadataNil {
		return nil, nil
	}
	if m.metadataZero {
		return &jetstream.MsgMetadata{}, nil
	}
	number := m.metadataN
	if number == 0 {
		number = 1
	}
	return &jetstream.MsgMetadata{NumDelivered: number}, nil
}
func (m *deliveryOwnerMsg) Ack() error                       { m.settlement.Add(1); return m.ackErr }
func (*deliveryOwnerMsg) DoubleAck(context.Context) error    { return nil }
func (m *deliveryOwnerMsg) Nak() error                       { m.settlement.Add(1); return nil }
func (m *deliveryOwnerMsg) NakWithDelay(time.Duration) error { m.settlement.Add(1); return nil }
func (m *deliveryOwnerMsg) InProgress() error                { m.heartbeats.Add(1); return nil }
func (m *deliveryOwnerMsg) Term() error                      { m.settlement.Add(1); return nil }
func (m *deliveryOwnerMsg) TermWithReason(string) error      { m.settlement.Add(1); return nil }

type deliveryOwnerHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

func (*deliveryOwnerHandle) Stop()                     {}
func (h *deliveryOwnerHandle) Drain()                  { h.drains.Add(1) }
func (h *deliveryOwnerHandle) Closed() <-chan struct{} { return h.closed }

func TestDeliveryLaneBuffersFatalBeforeHandleAndRefusesLaterDelivery(t *testing.T) {
	cause := errors.New("ambiguous effect")
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{}, time.Second,
		natsclient.ImmediateDeliveryRetry(), func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionQuarantine, cause
		})
	require.NoError(t, err)
	component := &Component{running: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	admission := newDeliveryLaneAdmission(component.recordDeliveryOwnerFatal)
	msg := &deliveryOwnerMsg{data: []byte("first")}
	result, admitted := consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.True(t, admitted)
	require.True(t, result.OwnerStopRequired())
	require.Len(t, admission.fatal, 1, "fatal must be buffered before the handle exists")
	health := component.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Contains(t, health.LastError, cause.Error())
	require.Equal(t, 1, health.ErrorCount)
	admission.latch(result)
	require.Len(t, admission.fatal, 1, "the first fatal result is sticky")
	require.Equal(t, 1, component.Health().ErrorCount)

	handle := &deliveryOwnerHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	component.observeDeliveryLane(ctx, &binding, admission)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.dataCalls.Load())
	require.Zero(t, msg.heartbeats.Load())
	require.Zero(t, msg.settlement.Load())
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load(), "fatal and ordinary stop share drain-once")
	cancel()
	<-binding.observerDone
}

func TestDeliveryMetadataFailureBuffersBeforeHandleAndDrainsExactOwner(t *testing.T) {
	metadataCause := errors.New("metadata unavailable")
	var workCalls atomic.Int32
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{}, time.Second,
		natsclient.ImmediateDeliveryRetry(), func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			workCalls.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		})
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission(nil)
	msg := &deliveryOwnerMsg{data: []byte("must-not-run"), metadataErr: metadataCause}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg, policy, admission)

	require.True(t, admitted)
	require.True(t, result.OwnerStopRequired())
	require.ErrorIs(t, result.Cause(), metadataCause)
	require.Len(t, admission.fatal, 1, "fatal must be retained before the exact handle exists")
	require.Equal(t, int32(1), msg.metadata.Load())
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, workCalls.Load())
	require.Zero(t, msg.heartbeats.Load())
	require.Zero(t, msg.settlement.Load())

	handle := &deliveryOwnerHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	component := &Component{running: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	component.observeDeliveryLane(ctx, &binding, admission)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed admission must not inspect a later delivery")
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load(), "only the exact owner is drained once")
	cancel()
	<-binding.observerDone
}

func TestDeliveryLaneAllowsAlreadyAdmittedWorkToComplete(t *testing.T) {
	slowEntered := make(chan struct{})
	releaseSlow := make(chan struct{})
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{}, time.Second,
		natsclient.ImmediateDeliveryRetry(), func(_ context.Context, _ natsclient.DeliveryAttempt, data []byte) (natsclient.DeliveryDecision, error) {
			if string(data) == "slow" {
				close(slowEntered)
				<-releaseSlow
				return natsclient.DeliveryDecisionAck, nil
			}
			<-slowEntered
			return natsclient.DeliveryDecisionQuarantine, errors.New("fatal")
		})
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission(nil)
	type slowOutcome struct {
		result   natsclient.DeliveryResult
		admitted bool
	}
	slowDone := make(chan slowOutcome, 1)
	go func() {
		result, admitted := consumeAdmittedDelivery(t.Context(), &deliveryOwnerMsg{data: []byte("slow")}, policy, admission)
		slowDone <- slowOutcome{result: result, admitted: admitted}
	}()
	<-slowEntered
	fatal, admitted := consumeAdmittedDelivery(t.Context(), &deliveryOwnerMsg{data: []byte("fatal")}, policy, admission)
	require.True(t, admitted)
	require.True(t, fatal.OwnerStopRequired())
	close(releaseSlow)
	slow := <-slowDone
	require.True(t, slow.admitted)
	require.NoError(t, slow.result.Err())
	require.False(t, admission.admit(), "new work closes after the first fatal result")
}

func TestDeliveryMethodErrorDoesNotCloseAdmission(t *testing.T) {
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{}, time.Second,
		natsclient.ImmediateDeliveryRetry(), func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		})
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission(nil)
	msg := &deliveryOwnerMsg{ackErr: errors.New("ack unknown")}
	result, admitted := consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.True(t, admitted)
	require.True(t, result.SettlementMethodFailed())
	require.False(t, result.OwnerStopRequired())
	require.True(t, admission.admit())
}

func TestToolsDeliveryPolicyUsesExactTargetConfigurationBeforeAcquisition(t *testing.T) {
	require.Equal(t, 5*time.Second, defaultToolsHeartbeatInterval)
	require.Equal(t, 5*time.Minute, defaultToolsAckWait)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handle := &deliveryOwnerHandle{closed: make(chan struct{})}
	var acquired atomic.Int32
	var observed natsclient.StreamConsumerConfig
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			acquired.Add(1)
			observed = cfg
			return handle, nil
		},
	}
	setup := consumerSetup{
		port: component.Port{Name: "tool.execute"}, streamName: "TOOLS", subject: "tool.execute.>",
		consumerConfig: component.ConsumerConfig{AckPolicy: "explicit"},
	}
	require.NoError(t, c.setupConsumer(ctx, setup))
	require.Equal(t, int32(1), acquired.Load())
	require.Equal(t, 5*time.Minute, observed.AckWait)
	require.Equal(t, []time.Duration{15 * time.Second, 60 * time.Second}, observed.BackOff)
	cancel()
	for _, binding := range c.consumers {
		if binding.observerDone != nil {
			<-binding.observerDone
		}
	}
}
