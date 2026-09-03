package agenticgovernance

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

type governanceFastOwnerMsg struct {
	dataCalls  atomic.Int32
	settlement atomic.Int32
}

func (m *governanceFastOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return []byte("work") }
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
	admission := newGovernanceDeliveryLaneAdmission()
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
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeGovernanceDeliveryLane(ctx, &binding, admission, "task_validation")
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

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
