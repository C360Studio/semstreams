package agenticgovernance

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

type governanceFastOwnerMsg struct {
	data       []byte
	dataCalls  atomic.Int32
	settlement atomic.Int32
	acks       atomic.Int32
	naks       atomic.Int32
	terms      atomic.Int32
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
func (m *governanceFastOwnerMsg) Ack() error {
	m.settlement.Add(1)
	m.acks.Add(1)
	return nil
}
func (*governanceFastOwnerMsg) DoubleAck(context.Context) error { return nil }
func (m *governanceFastOwnerMsg) Nak() error {
	m.settlement.Add(1)
	m.naks.Add(1)
	return nil
}
func (m *governanceFastOwnerMsg) NakWithDelay(time.Duration) error { return m.Nak() }
func (*governanceFastOwnerMsg) InProgress() error                  { return nil }
func (m *governanceFastOwnerMsg) Term() error {
	m.settlement.Add(1)
	m.terms.Add(1)
	return nil
}
func (m *governanceFastOwnerMsg) TermWithReason(string) error { return m.Term() }

type governanceFastOwnerHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

type governanceHealthProbeHandle struct {
	drains  atomic.Int32
	closed  chan struct{}
	onDrain func()
}

func (*governanceHealthProbeHandle) Stop() {}
func (h *governanceHealthProbeHandle) Drain() {
	if h.drains.Add(1) == 1 && h.onDrain != nil {
		h.onDrain()
	}
}
func (h *governanceHealthProbeHandle) Closed() <-chan struct{} { return h.closed }

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
func TestGovernanceFatalHealthLatchesBeforeEachExactOwnerDrain(t *testing.T) {
	c := &Component{running: true, startTime: time.Now(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	atomic.StoreInt64(&c.errors, 4)
	firstCause := errors.New("first governance owner lost")
	secondCause := errors.New("later governance owner lost")
	healthAtDrain := make(chan component.HealthStatus, 2)
	firstHealthLatched := make(chan struct{})
	secondHealthLatched := make(chan struct{})
	firstAdmission := &governanceDeliveryLaneAdmission{open: true, fatal: make(chan error), onFatal: func(err error) {
		c.recordDeliveryOwnerFatal(err)
		close(firstHealthLatched)
	}}
	secondAdmission := &governanceDeliveryLaneAdmission{open: true, fatal: make(chan error), onFatal: func(err error) {
		c.recordDeliveryOwnerFatal(err)
		close(secondHealthLatched)
	}}
	firstHandle := &governanceHealthProbeHandle{closed: make(chan struct{}), onDrain: func() {
		health := c.Health()
		if health.Healthy || health.Status != "delivery ownership lost" || health.LastError != firstCause.Error() || health.ErrorCount != 5 {
			t.Errorf("health at first drain = %+v, want first fatal cause after four business errors", health)
		}
		healthAtDrain <- health
	}}
	secondHandle := &governanceHealthProbeHandle{closed: make(chan struct{}), onDrain: func() {
		health := c.Health()
		if health.Healthy || health.Status != "delivery ownership lost" || health.LastError != firstCause.Error() || health.ErrorCount != 5 {
			t.Errorf("health at second drain = %+v, want sticky first fatal cause and count", health)
		}
		healthAtDrain <- health
	}}
	firstBinding := newGovernanceStreamConsumerBinding(firstHandle)
	secondBinding := newGovernanceStreamConsumerBinding(secondHandle)
	ctx, cancel := context.WithCancel(t.Context())

	firstLatchDone := make(chan struct{})
	go func() {
		firstAdmission.latchFatal(firstCause)
		close(firstLatchDone)
	}()
	select {
	case <-firstHealthLatched:
	case <-time.After(time.Second):
		c.observeGovernanceDeliveryLane(ctx, &firstBinding, firstAdmission, "task_validation")
		<-firstLatchDone
		cancel()
		<-firstBinding.observerDone
		t.Fatal("first fatal health was not latched before owner notification")
	}
	c.observeGovernanceDeliveryLane(ctx, &firstBinding, firstAdmission, "task_validation")
	firstHealth := <-healthAtDrain
	<-firstLatchDone
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status)
	require.Equal(t, firstCause.Error(), firstHealth.LastError)
	require.Equal(t, 5, firstHealth.ErrorCount)
	require.Equal(t, int32(1), firstHandle.drains.Load())
	require.Zero(t, secondHandle.drains.Load())

	secondLatchDone := make(chan struct{})
	go func() {
		secondAdmission.latchFatal(secondCause)
		close(secondLatchDone)
	}()
	select {
	case <-secondHealthLatched:
	case <-time.After(time.Second):
		c.observeGovernanceDeliveryLane(ctx, &secondBinding, secondAdmission, "request_validation")
		<-secondLatchDone
		cancel()
		<-firstBinding.observerDone
		<-secondBinding.observerDone
		t.Fatal("later fatal health callback did not finish before owner notification")
	}
	c.observeGovernanceDeliveryLane(ctx, &secondBinding, secondAdmission, "request_validation")
	secondHealth := <-healthAtDrain
	<-secondLatchDone
	require.Equal(t, firstHealth.LastError, secondHealth.LastError)
	require.Equal(t, firstHealth.ErrorCount, secondHealth.ErrorCount)
	require.Equal(t, int32(1), firstHandle.drains.Load())
	require.Equal(t, int32(1), secondHandle.drains.Load())

	cancel()
	<-firstBinding.observerDone
	<-secondBinding.observerDone
}
