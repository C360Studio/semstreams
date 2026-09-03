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

// spec: agentic-model / Model heartbeat policy is valid before acquisition
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
	admission := newDeliveryLaneAdmission()
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

// spec: agentic-model / Model heartbeat policy is valid before acquisition
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

	handle := &modelPolicyHandle{closed: make(chan struct{})}
	var callback func(context.Context, jetstream.Msg)
	c := &Component{
		name: "agentic-model", config: DefaultConfig(),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			callback = handler
			return handle, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupConsumer(ctx, port))
	require.NotNil(t, callback)
	require.Len(t, c.consumers, 1)

	msg := &modelDeliveryOwnerMsg{metadataErr: errors.New("metadata unavailable")}
	callback(ctx, msg)
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	callback(ctx, msg)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, msg.settlement.Load())

	cancel()
	<-c.consumers[0].observerDone
}

// spec: agentic-model / Model heartbeat policy is valid before acquisition
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
