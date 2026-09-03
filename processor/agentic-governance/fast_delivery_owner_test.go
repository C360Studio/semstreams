package agenticgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type governanceFastOwnerMsg struct {
	data       []byte
	dataCalls  atomic.Int32
	settlement atomic.Int32
}

func (m *governanceFastOwnerMsg) Data() []byte {
	m.dataCalls.Add(1)
	if m.data != nil {
		return m.data
	}
	return []byte("work")
}
func (*governanceFastOwnerMsg) Subject() string      { return "agent.task.test" }
func (*governanceFastOwnerMsg) Reply() string        { return "" }
func (*governanceFastOwnerMsg) Headers() nats.Header { return nil }
func (*governanceFastOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *governanceFastOwnerMsg) Ack() error                       { m.settlement.Add(1); return nil }
func (*governanceFastOwnerMsg) DoubleAck(context.Context) error    { return nil }
func (m *governanceFastOwnerMsg) Nak() error                       { m.settlement.Add(1); return nil }
func (m *governanceFastOwnerMsg) NakWithDelay(time.Duration) error { m.settlement.Add(1); return nil }
func (*governanceFastOwnerMsg) InProgress() error                  { return nil }
func (m *governanceFastOwnerMsg) Term() error                      { m.settlement.Add(1); return nil }
func (m *governanceFastOwnerMsg) TermWithReason(string) error      { m.settlement.Add(1); return nil }

type governanceFastOwnerHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

func (h *governanceFastOwnerHandle) Stop() { h.Drain() }
func (h *governanceFastOwnerHandle) Drain() {
	if h.drains.Add(1) == 1 {
		close(h.closed)
	}
}
func (h *governanceFastOwnerHandle) Closed() <-chan struct{} { return h.closed }

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceFastOwnerPanicQuarantinesAndDrainsExactHandle(t *testing.T) {
	c := &Component{running: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	atomic.StoreInt64(&c.errors, 4)
	admission := newGovernanceDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
	msg := &governanceFastOwnerMsg{}
	decision, admitted, err := consumeAdmittedGovernanceFastDelivery(
		t.Context(), msg,
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) { panic("boom") },
		admission,
	)
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	require.ErrorContains(t, err, "panicked: boom")
	require.Zero(t, msg.settlement.Load(), "quarantine must not settle an unsafe delivery")

	handle := &governanceFastOwnerHandle{closed: make(chan struct{})}
	binding := newGovernanceStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeGovernanceDeliveryLane(ctx, &binding, admission, "task_validation")
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Equal(t, 5, health.ErrorCount)
	require.ErrorContains(t, errors.New(health.LastError), "panicked: boom")

	_, admitted, _ = consumeAdmittedGovernanceFastDelivery(
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

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceProductionBindingsUseDeclaredOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	type capturedBinding struct {
		cfg      natsclient.StreamConsumerConfig
		callback func(context.Context, jetstream.Msg)
	}
	captured := make(map[string]capturedBinding)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		captured[owner.Port] = capturedBinding{cfg: cfg, callback: callback}
		return &governanceFastOwnerHandle{closed: make(chan struct{})}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupInputConsumers(ctx))
	// Setup has captured the production owners; nil suppresses only optional
	// output publication so each real filter-chain branch remains synchronous.
	c.natsClient = nil

	for index, port := range []string{"task_validation", "request_validation", "response_validation"} {
		binding, ok := captured[port]
		require.True(t, ok, "production setup did not bind %s", port)
		require.Equal(t, governanceFastDeliveryAckWait, binding.cfg.AckWait)
		require.Equal(t, governanceFastDeliveryAckWait, binding.cfg.MessageTimeout)
		data, marshalErr := json.Marshal(Message{ID: fmt.Sprintf("message-%d", index), Content: Content{Text: "clean"}})
		require.NoError(t, marshalErr)
		msg := &governanceFastOwnerMsg{}
		msgData := data
		msg.data = msgData
		binding.callback(ctx, msg)
		require.Equal(t, int32(1), msg.dataCalls.Load(), "%s must execute its production handler", port)
		require.Equal(t, int32(1), msg.settlement.Load(), "%s must settle exactly once", port)
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}
