package agenticmodel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type modelDeliveryOwnerMsg struct {
	data        []byte
	dataCalls   atomic.Int32
	heartbeats  atomic.Int32
	settlement  atomic.Int32
	metadata    atomic.Int32
	metadataErr error
}

func (m *modelDeliveryOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return m.data }
func (*modelDeliveryOwnerMsg) Subject() string      { return "agent.request.test" }
func (*modelDeliveryOwnerMsg) Reply() string        { return "" }
func (*modelDeliveryOwnerMsg) Headers() nats.Header { return nil }
func (m *modelDeliveryOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadata.Add(1)
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *modelDeliveryOwnerMsg) Ack() error                       { m.settlement.Add(1); return nil }
func (*modelDeliveryOwnerMsg) DoubleAck(context.Context) error    { return nil }
func (m *modelDeliveryOwnerMsg) Nak() error                       { m.settlement.Add(1); return nil }
func (m *modelDeliveryOwnerMsg) NakWithDelay(time.Duration) error { m.settlement.Add(1); return nil }
func (m *modelDeliveryOwnerMsg) InProgress() error                { m.heartbeats.Add(1); return nil }
func (m *modelDeliveryOwnerMsg) Term() error                      { m.settlement.Add(1); return nil }
func (m *modelDeliveryOwnerMsg) TermWithReason(string) error      { m.settlement.Add(1); return nil }

// spec: agentic-model / Model request settlement is bound to a durable response
func TestModelUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner(t *testing.T) {
	var workCalls atomic.Int32
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(),
		natsclient.StreamConsumerConfig{AckWait: 2 * time.Minute},
		60*time.Second,
		natsclient.ImmediateDeliveryRetry(),
		func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			workCalls.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		},
	)
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission(nil)
	metadataCause := errors.New("metadata unavailable")
	msg := &modelDeliveryOwnerMsg{data: []byte("must-not-run"), metadataErr: metadataCause}

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

	handle := &modelPolicyHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeDeliveryLane(ctx, &binding, admission)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed admission must not inspect another delivery")
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load(), "fatal and ordinary stop share exact drain-once authority")
	cancel()
	<-binding.observerDone
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestModelSetupWiresMetadataFailureToAcquiredOwner(t *testing.T) {
	port, err := (component.PortDefinition{
		Name: "agent.request",
		Config: component.JetStreamPort{
			StreamName: "AGENT", Subjects: []string{"agent.request.>"},
			AckWait: "120s", HeartbeatInterval: "60s",
		},
		Required: true,
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)

	handles := []*modelPolicyHandle{
		{closed: make(chan struct{})},
		{closed: make(chan struct{})},
	}
	callbacks := make([]func(context.Context, jetstream.Msg), 0, len(handles))
	c := &Component{
		name: "agentic-model", config: DefaultConfig(), running: true, startTime: time.Now(),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			index := len(callbacks)
			callbacks = append(callbacks, handler)
			return handles[index], nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupConsumer(ctx, port))
	require.NoError(t, c.setupConsumer(ctx, port))
	require.Len(t, callbacks, 2)
	require.Len(t, c.consumers, 2)

	firstCause := errors.New("first metadata unavailable")
	first := &modelDeliveryOwnerMsg{metadataErr: firstCause}
	callbacks[0](ctx, first)
	firstHealth := c.Health()
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status)
	require.Equal(t, "delivery_metadata_unavailable: "+firstCause.Error(), firstHealth.LastError)
	require.Equal(t, 1, firstHealth.ErrorCount)
	require.Eventually(t, func() bool { return handles[0].drains.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, handles[1].drains.Load(), "first fatal must not drain another owner")

	secondCause := errors.New("later metadata unavailable")
	second := &modelDeliveryOwnerMsg{metadataErr: secondCause}
	callbacks[1](ctx, second)
	require.Eventually(t, func() bool { return handles[1].drains.Load() == 1 }, time.Second, time.Millisecond)
	secondHealth := c.Health()
	require.False(t, secondHealth.Healthy)
	require.Equal(t, "delivery ownership lost", secondHealth.Status)
	require.Equal(t, firstHealth.LastError, secondHealth.LastError, "the first fatal cause must remain sticky")
	require.NotContains(t, secondHealth.LastError, secondCause.Error())
	require.Equal(t, 1, secondHealth.ErrorCount, "later fatal owner loss must not recount")
	require.Equal(t, int32(1), handles[0].drains.Load(), "later fatal must not redrain the first owner")

	callbacks[0](ctx, first)
	require.Equal(t, int32(1), first.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	for index, msg := range []*modelDeliveryOwnerMsg{first, second} {
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

// spec: agentic-model / Model request settlement is bound to a durable response
func TestDeliveryAttemptExposesOnlyImmutableAttemptObservation(t *testing.T) {
	typeOfAttempt := reflect.TypeOf(natsclient.DeliveryAttempt{})
	require.Equal(t, 1, typeOfAttempt.NumField())
	field := typeOfAttempt.Field(0)
	require.NotEmpty(t, field.PkgPath, "attempt storage must remain unexported")
	require.Equal(t, reflect.Uint64, field.Type.Kind(), "attempt storage must be scalar and immutable by callers")

	methods := make([]string, 0, typeOfAttempt.NumMethod())
	for i := 0; i < typeOfAttempt.NumMethod(); i++ {
		methods = append(methods, typeOfAttempt.Method(i).Name)
	}
	sort.Strings(methods)
	require.Equal(t, []string{"IsRedelivery", "MetadataAvailable", "Number"}, methods)
	for _, forbidden := range []string{"Message", "Ack", "Nak", "Term", "Sequence", "Consumer", "Headers"} {
		_, found := typeOfAttempt.MethodByName(forbidden)
		require.False(t, found, "DeliveryAttempt must not expose %s", forbidden)
	}
	require.Equal(t, typeOfAttempt.NumMethod(), reflect.PointerTo(typeOfAttempt).NumMethod(),
		"pointer-only methods would expose mutable attempt state")
}
