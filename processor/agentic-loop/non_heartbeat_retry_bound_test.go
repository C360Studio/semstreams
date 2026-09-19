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
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// This change made Retry the default classification for every unclassified
// error on the four non-heartbeat lanes. Those lanes shipped with no consumer
// config at all, so they had no BackOff, and SettleDelivery's Retry is a bare
// Nak: every transient error was redelivered at line rate until
// component.GetConsumerConfig's default MaxDeliver of 3 burned through. The
// missing piece was the schedule, not the ceiling. This drives the production
// setup path and reads the consumer it actually acquires.
//
// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestNonHeartbeatLanesAcquireABoundedConsumer(t *testing.T) {
	t.Parallel()

	for _, portName := range []string{
		"agent.signal", "agent.approval_response", "agent.toolcall.approved", "agent.toolcall.rejected",
	} {
		t.Run(portName, func(t *testing.T) {
			t.Parallel()
			var acquired atomic.Int32
			var acquiredConfig natsclient.StreamConsumerConfig
			c := &Component{
				config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				waitForStreamInput: func(context.Context, string) error { return nil },
				consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
					acquired.Add(1)
					acquiredConfig = cfg
					return &loopPolicyHandle{closed: make(chan struct{})}, nil
				},
			}

			require.NoError(t, c.setupConsumer(
				t.Context(), t.Context(), shippedLoopPort(t, portName), portName+".>",
				func(context.Context, []byte) error { return nil },
				func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
					return natsclient.DeliveryDecisionAck, nil
				},
			))

			require.Equal(t, int32(1), acquired.Load())
			require.NotEmpty(t, acquiredConfig.BackOff, "a Retry lane with no BackOff redelivers at line rate")
			require.GreaterOrEqual(t, acquiredConfig.MaxDeliver, len(acquiredConfig.BackOff),
				"MaxDeliver must cover the BackOff; 0 is unlimited")
			require.NoError(t, validateLoopRetryPolicy(portName, acquiredConfig),
				"the floor the heartbeat lanes are held to applies here too")
		})
	}
}

// The acquisition config is only half the bound. The other half is the
// settlement call the lane's own installed callback makes, and swapping it
// back to the bare SettleDelivery left every test green — the primitive's test
// below drives natsclient directly and never observes the wiring. This drives
// the production setup path, keeps the callback it installs, and pushes one
// Retry-classified message through it.
//
// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestNonHeartbeatLaneRetrySettlesAsADelayedNak(t *testing.T) {
	t.Parallel()

	var installed func(context.Context, jetstream.Msg)
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			installed = handler
			return &loopPolicyHandle{closed: make(chan struct{})}, nil
		},
	}

	require.NoError(t, c.setupConsumer(
		t.Context(), t.Context(), shippedLoopPort(t, "agent.signal"), "agent.signal.>",
		func(context.Context, []byte) error { return nil },
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionRetry, errors.New("not yet")
		},
	))
	require.NotNil(t, installed, "setup must install a callback on the non-heartbeat lane")

	msg := &loopDeliveryOwnerMsg{data: []byte("{}")}
	installed(t.Context(), msg)

	require.Equal(t, int32(1), msg.naks.Load(), "a Retry classification must negatively acknowledge")
	require.Equal(t, int32(1), msg.nakDelays.Load(),
		"the lane must settle through the delayed retry policy, not the bare Nak SettleDelivery defaults to")
	require.Zero(t, msg.acks.Load()+msg.terms.Load())
}

// And the same floor refuses before allocation when it is not met, exactly as
// it does on the heartbeat lanes.
//
// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestNonHeartbeatLaneRefusesSingleDeliveryBeforeAllocation(t *testing.T) {
	t.Parallel()

	var acquired atomic.Int32
	config := DefaultConfig()
	// An operator pinning a single delivery on a fast lane.
	port, err := (component.PortDefinition{
		Name:   "agent.signal",
		Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.signal.*"}, MaxDeliver: 1},
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)
	c := &Component{
		config: config, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			acquired.Add(1)
			return &loopPolicyHandle{closed: make(chan struct{})}, nil
		},
	}

	err = c.setupConsumer(
		t.Context(), t.Context(), port, "agent.signal.>",
		func(context.Context, []byte) error { return nil },
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_deliver")
	require.Zero(t, acquired.Load(), "an invalid retry floor must fail before allocation")
}

// The delayed NAK itself, through the exported settlement surface the four
// lanes now use.
//
// spec: jetstream-consumer-policy / settlement-only delivery decisions use one shared interpreter
func TestSettleDeliveryWithRetryPerformsTheDelayedNak(t *testing.T) {
	t.Parallel()

	retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)

	delayed := &loopDeliveryOwnerMsg{}
	result := natsclient.SettleDeliveryWithRetry(
		delayed, retry, natsclient.DeliveryDecisionRetry, errors.New("not yet"))
	require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
	require.Equal(t, int32(1), delayed.naks.Load())
	require.Zero(t, delayed.acks.Load()+delayed.terms.Load())
	require.Equal(t, int32(1), delayed.nakDelays.Load(),
		"a bounded lane must use NakWithDelay, not the bare Nak SettleDelivery defaults to")

	// A bare SettleDelivery is unchanged: immediate Nak, no delay.
	immediate := &loopDeliveryOwnerMsg{}
	require.Equal(t, natsclient.DeliveryDecisionRetry,
		natsclient.SettleDelivery(immediate, natsclient.DeliveryDecisionRetry, errors.New("not yet")).Decision())
	require.Equal(t, int32(1), immediate.naks.Load())
	require.Zero(t, immediate.nakDelays.Load())

	// The decision vocabulary is unchanged by the new entry point.
	acked := &loopDeliveryOwnerMsg{}
	require.Equal(t, natsclient.DeliveryDecisionAck,
		natsclient.SettleDeliveryWithRetry(acked, retry, natsclient.DeliveryDecisionAck, nil).Decision())
	require.Equal(t, int32(1), acked.acks.Load())

	// And it fails closed on the same tuples SettleDelivery does, plus the one
	// it adds: an unset policy must not silently settle as immediate.
	require.True(t, natsclient.SettleDeliveryWithRetry(
		nil, retry, natsclient.DeliveryDecisionAck, nil).OwnerStopRequired())

	badPolicyMsg := &loopDeliveryOwnerMsg{}
	require.True(t, natsclient.SettleDeliveryWithRetry(
		badPolicyMsg, natsclient.DeliveryRetryPolicy{}, natsclient.DeliveryDecisionRetry, errors.New("x"),
	).OwnerStopRequired())
	require.Zero(t, badPolicyMsg.acks.Load()+badPolicyMsg.naks.Load()+badPolicyMsg.terms.Load())
}

// shippedLoopPort resolves one of the component's own shipped input port
// definitions, so the test reads what operators get rather than a fixture.
func shippedLoopPort(t *testing.T, portName string) component.Port {
	t.Helper()
	for _, candidate := range DefaultConfig().Ports.Inputs {
		if candidate.Name != portName {
			continue
		}
		port, err := candidate.Resolve(component.DirectionInput)
		require.NoError(t, err)
		return port
	}
	t.Fatalf("shipped input port %q not found", portName)
	return component.Port{}
}
