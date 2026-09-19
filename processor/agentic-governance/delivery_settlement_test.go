package agenticgovernance

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type governanceSettlementMsg struct {
	data  []byte
	acks  atomic.Int32
	naks  atomic.Int32
	terms atomic.Int32
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceAllowedPublicationFailureQuarantinesExactOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.running = true
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*governanceSettlementHandle)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &governanceSettlementHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupInputConsumers(ctx))
	data, err := json.Marshal(Message{ID: "message-1", Content: Content{Text: "clean"}})
	require.NoError(t, err)
	msg := &governanceSettlementMsg{data: data}
	callbacks["task_validation"](ctx, msg)
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["task_validation"].drains.Load() == 1 }, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "task_validation" {
			require.Zero(t, handle.drains.Load(), "publish failure drained unrelated owner %s", port)
		}
	}
	require.Contains(t, c.Health().LastError, "unknown durable publication state")
	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

func (m *governanceSettlementMsg) Data() []byte                            { return m.data }
func (*governanceSettlementMsg) Subject() string                           { return "governance.test" }
func (*governanceSettlementMsg) Reply() string                             { return "" }
func (*governanceSettlementMsg) Headers() nats.Header                      { return nil }
func (*governanceSettlementMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *governanceSettlementMsg) Ack() error                              { m.acks.Add(1); return nil }
func (*governanceSettlementMsg) DoubleAck(context.Context) error           { return nil }
func (m *governanceSettlementMsg) Nak() error                              { m.naks.Add(1); return nil }
func (m *governanceSettlementMsg) NakWithDelay(time.Duration) error        { m.naks.Add(1); return nil }
func (*governanceSettlementMsg) InProgress() error                         { return nil }
func (m *governanceSettlementMsg) Term() error                             { m.terms.Add(1); return nil }
func (m *governanceSettlementMsg) TermWithReason(string) error             { return m.Term() }

type governanceSettlementHandle struct {
	closed chan struct{}
	drains atomic.Int32
}

func (*governanceSettlementHandle) Stop()                     {}
func (h *governanceSettlementHandle) Drain()                  { h.drains.Add(1) }
func (h *governanceSettlementHandle) Closed() <-chan struct{} { return h.closed }

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceProductionCallbacksTerminateMalformedInputs(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &governanceSettlementHandle{closed: make(chan struct{})}, nil
	}
	require.NoError(t, c.setupInputConsumers(t.Context()))

	for _, port := range []string{"task_validation", "request_validation", "response_validation"} {
		callback, ok := callbacks[port]
		require.True(t, ok, "production setup did not bind %s", port)
		msg := &governanceSettlementMsg{data: []byte("{")}
		callback(t.Context(), msg)
		require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)
		require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)
		require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)
	}
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceProductionCallbackPanicLatchesFirstFatalAndDrainsExactOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.running = true
	atomic.StoreInt64(&c.errors, 4)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*governanceSettlementHandle)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &governanceSettlementHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupInputConsumers(ctx))
	c.chain = nil

	msg := &governanceSettlementMsg{data: []byte(`{"id":"panic"}`)}
	callbacks["task_validation"](ctx, msg)
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["task_validation"].drains.Load() == 1 }, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "task_validation" {
			require.Zero(t, handle.drains.Load(), "panic drained unrelated owner %s", port)
		}
	}
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Equal(t, 5, health.ErrorCount)
	require.Contains(t, health.LastError, "governance delivery work panicked")

	callbacks["request_validation"](ctx, &governanceSettlementMsg{data: []byte(`{"id":"later"}`)})
	require.Eventually(t, func() bool { return handles["request_validation"].drains.Load() == 1 }, time.Second, time.Millisecond)
	later := c.Health()
	require.Equal(t, health.LastError, later.LastError)
	require.Equal(t, health.ErrorCount, later.ErrorCount)

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}
