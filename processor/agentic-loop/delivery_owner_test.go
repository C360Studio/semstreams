package agenticloop

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

type loopDeliveryOwnerMsg struct {
	data        []byte
	dataCalls   atomic.Int32
	heartbeats  atomic.Int32
	settlement  atomic.Int32
	metadata    atomic.Int32
	metadataErr error
}

type capturedVerdict struct {
	decision string
	callID   string
}

type capturingGovernanceDispatcher struct {
	verdicts chan capturedVerdict
}

func (*capturingGovernanceDispatcher) Propose(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
	return DispatcherResult{}, nil
}
func (d *capturingGovernanceDispatcher) HandleVerdict(decision, callID string, _ []byte) {
	d.verdicts <- capturedVerdict{decision: decision, callID: callID}
}
func (*capturingGovernanceDispatcher) Mode() string { return ToolCallGovernanceModeDisabled }

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

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopFastOwnerInvalidTupleQuarantinesAndDrainsExactHandle(t *testing.T) {
	admission := newDeliveryLaneAdmission(nil)
	msg := &loopDeliveryOwnerMsg{data: []byte("work")}
	decision, admitted, err := consumeAdmittedLoopFastDelivery(
		t.Context(), msg,
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, errors.New("ack cannot carry an error")
		},
		admission,
	)
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	require.ErrorContains(t, err, "invalid loop fast delivery decision")
	require.Zero(t, msg.settlement.Load(), "invalid tuples must not settle an unsafe delivery")

	handle := &loopPolicyHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeDeliveryLane(ctx, &binding, admission, "agent.signal")
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted, _ = consumeAdmittedLoopFastDelivery(
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

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
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
	admission := newDeliveryLaneAdmission(nil)
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

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopSetupWiresMetadataFailureToAcquiredOwner(t *testing.T) {
	handles := []*loopPolicyHandle{
		{closed: make(chan struct{})},
		{closed: make(chan struct{})},
	}
	callbacks := make([]func(context.Context, jetstream.Msg), 0, len(handles))
	var workCalls atomic.Int32
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), started: true, startTime: time.Now(),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			index := len(callbacks)
			callbacks = append(callbacks, handler)
			return handles[index], nil
		},
	}
	c.trajectoryAuditHealth.latch("trajectory audit degraded")
	require.Equal(t, 1, c.Health().ErrorCount)

	ctx, cancel := context.WithCancel(t.Context())
	for _, portName := range []string{"agent.task", "agent.response"} {
		port, err := (component.PortDefinition{
			Name:     portName,
			Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{portName + ".>"}},
			Required: true,
		}).Resolve(component.DirectionInput)
		require.NoError(t, err)
		require.NoError(t, c.setupConsumer(
			ctx, ctx, port, portName+".>", func(context.Context, []byte) error {
				workCalls.Add(1)
				return nil
			}, nil,
		))
	}
	require.Len(t, callbacks, 2)
	require.Len(t, c.consumers, 2)

	firstCause := errors.New("first metadata unavailable")
	first := &loopDeliveryOwnerMsg{metadataErr: firstCause}
	callbacks[0](ctx, first)
	firstHealth := c.Health()
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status,
		"owner loss must take precedence over trajectory degradation")
	require.Equal(t, "delivery_metadata_unavailable: "+firstCause.Error(), firstHealth.LastError)
	require.NotContains(t, firstHealth.LastError, "trajectory audit degraded")
	require.Equal(t, 2, firstHealth.ErrorCount, "owner loss adds exactly one error to existing trajectory degradation")
	require.Eventually(t, func() bool { return handles[0].drains.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, handles[1].drains.Load(), "first fatal must not drain another owner")

	secondCause := errors.New("later metadata unavailable")
	second := &loopDeliveryOwnerMsg{metadataErr: secondCause}
	callbacks[1](ctx, second)
	require.Eventually(t, func() bool { return handles[1].drains.Load() == 1 }, time.Second, time.Millisecond)
	secondHealth := c.Health()
	require.Equal(t, "delivery ownership lost", secondHealth.Status)
	require.Equal(t, firstHealth.LastError, secondHealth.LastError, "the first fatal cause must remain sticky")
	require.NotContains(t, secondHealth.LastError, secondCause.Error())
	require.Equal(t, 2, secondHealth.ErrorCount, "later fatal owner loss must not recount")
	require.Equal(t, int32(1), handles[0].drains.Load(), "later fatal must not redrain the first owner")

	callbacks[0](ctx, first)
	require.Equal(t, int32(1), first.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	require.Zero(t, workCalls.Load())
	for index, msg := range []*loopDeliveryOwnerMsg{first, second} {
		require.Zero(t, msg.dataCalls.Load(), "message %d must not expose data to work", index)
		require.Zero(t, msg.heartbeats.Load(), "message %d must not heartbeat", index)
		require.Zero(t, msg.settlement.Load(), "message %d must not settle", index)
		require.Equal(t, int32(1), handles[index].drains.Load(), "only the exact owner handle drains once")
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopFastProductionBindingsUseDeclaredOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	verdicts := &capturingGovernanceDispatcher{verdicts: make(chan capturedVerdict, 2)}
	c.handler.SetGovernanceDispatcher(verdicts)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	type capturedBinding struct {
		cfg      natsclient.StreamConsumerConfig
		callback func(context.Context, jetstream.Msg)
	}
	captured := make(map[string]capturedBinding)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		captured[owner.Port] = capturedBinding{cfg: cfg, callback: callback}
		return &loopPolicyHandle{closed: make(chan struct{})}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	for _, port := range []string{"agent.signal", "agent.approval_response", "agent.toolcall.approved", "agent.toolcall.rejected"} {
		binding, ok := captured[port]
		require.True(t, ok, "production setup did not bind %s", port)
		require.Equal(t, loopFastDeliveryAckWait, binding.cfg.AckWait)
		require.Equal(t, loopFastDeliveryAckWait, binding.cfg.MessageTimeout)
		data := []byte("{")
		switch port {
		case "agent.toolcall.approved":
			data = []byte(`{"decision":"approved","call_id":"call-approved"}`)
		case "agent.toolcall.rejected":
			data = []byte(`{"decision":"rejected","call_id":"call-rejected"}`)
		}
		msg := &loopDeliveryOwnerMsg{data: data}
		binding.callback(ctx, msg)
		require.Equal(t, int32(1), msg.dataCalls.Load(), "%s must execute its production handler", port)
		require.Equal(t, int32(1), msg.settlement.Load(), "%s must settle exactly once", port)
	}
	require.Equal(t, capturedVerdict{decision: "approved", callID: "call-approved"}, <-verdicts.verdicts)
	require.Equal(t, capturedVerdict{decision: "rejected", callID: "call-rejected"}, <-verdicts.verdicts)

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// scenario: Approval handler panics
func TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	loopID := c.handler.loopManager.GenerateLoopID()
	c.handler.loopManager = nil // panic at the deepest controllable production dependency
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	response := &agentic.ApprovalResponse{
		LoopID: loopID, CallID: "call-panic", Decision: agentic.ApprovalDecisionApprove,
		ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(response.Schema(), response, "test")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	msg := &loopDeliveryOwnerMsg{data: data}
	callbacks["agent.approval_response"](ctx, msg)

	require.Zero(t, msg.settlement.Load(), "panic must not ACK, NAK, or terminate the source")
	require.Eventually(t, func() bool {
		return handles["agent.approval_response"].drains.Load() == 1
	}, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "agent.approval_response" {
			require.Zero(t, handle.drains.Load(), "panic drained unrelated owner %s", port)
		}
	}
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Contains(t, health.LastError, "approval response handler panicked")

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}
