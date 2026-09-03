//go:build integration

package agenticdispatch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type dispatchFastDeliveryObservation struct {
	attempt  uint64
	decision natsclient.DeliveryDecision
	err      error
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestIntegrationDispatchFastDeliveryOwnersCancelJoinAndRetry(t *testing.T) {
	t.Parallel()

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{
			Name: "DISPATCH_FAST_OWNER",
			Subjects: []string{
				"user.message.fast-owner",
				"agent.created.fast-owner",
				"agent.approval_pending.fast-owner",
			},
		},
	))

	for index, lane := range []struct {
		port    string
		subject string
	}{
		{port: "user.message", subject: "user.message.fast-owner"},
		{port: "agent.created", subject: "agent.created.fast-owner"},
		{port: "agent.approval_pending", subject: "agent.approval_pending.fast-owner"},
	} {
		t.Run(lane.port, func(t *testing.T) {
			exerciseDispatchFastDeliveryBoundary(t, testClient, lane.port, lane.subject, index)
		})
	}
}

func exerciseDispatchFastDeliveryBoundary(
	t *testing.T,
	testClient *natsclient.TestClient,
	port string,
	subject string,
	index int,
) {
	t.Helper()
	ctx := t.Context()
	consumerName := fmt.Sprintf("dispatch-fast-owner-%d", index)
	results := make(chan dispatchFastDeliveryObservation, 2)
	started := make(chan time.Time, 2)
	joined := make(chan error, 1)
	var invocations atomic.Int32
	var active atomic.Int32
	var overlap atomic.Bool

	cfg := natsclient.StreamConsumerConfig{
		StreamName: "DISPATCH_FAST_OWNER", ConsumerName: consumerName, FilterSubject: subject,
		DeliverPolicy: "all", AckPolicy: "explicit", AckWait: dispatchFastDeliveryAckWait,
		MaxDeliver: 3, MaxAckPending: 10, MessageTimeout: dispatchFastDeliveryAckWait,
	}
	handle, err := testClient.Client.ConsumeStreamWithConfig(
		ctx,
		natsclient.PortConsumerContext{Component: "agentic-dispatch", Port: port},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			metadata, metadataErr := msg.Metadata()
			attempt := uint64(0)
			if metadataErr == nil && metadata != nil {
				attempt = metadata.NumDelivered
			}
			decision, deliveryErr := consumeDispatchFastDelivery(
				msgCtx,
				msg,
				func(workCtx context.Context, _ []byte) (natsclient.DeliveryDecision, error) {
					if active.Add(1) != 1 {
						overlap.Store(true)
					}
					defer active.Add(-1)
					call := invocations.Add(1)
					started <- time.Now()
					if call == 1 {
						<-workCtx.Done()
						joined <- workCtx.Err()
						return natsclient.DeliveryDecisionRetry, workCtx.Err()
					}
					return natsclient.DeliveryDecisionAck, nil
				},
			)
			results <- dispatchFastDeliveryObservation{
				attempt: attempt, decision: decision, err: errors.Join(metadataErr, deliveryErr),
			}
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		handle.Drain()
		select {
		case <-handle.Closed():
		case <-time.After(5 * time.Second):
			t.Errorf("%s consume handle did not close", port)
		}
	})

	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, cfg.StreamName, consumerName)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, dispatchFastDeliveryAckWait, info.Config.AckWait)

	require.NoError(t, testClient.Client.PublishToStream(ctx, subject, []byte(port)))
	firstStarted := <-started

	select {
	case joinErr := <-joined:
		require.ErrorIs(t, joinErr, context.DeadlineExceeded)
	case <-time.After(dispatchFastDeliveryAckWait + 5*time.Second):
		t.Fatal("cooperative delivery work was not canceled and joined before AckWait")
	}
	// This is the one deliberate wall-clock assertion: it observes the real
	// 25s owner deadline while allowing scheduler jitter inside the 5s margin.
	elapsed := time.Since(firstStarted)
	require.GreaterOrEqual(t, elapsed, dispatchFastDeliveryWorkBudget-time.Second)
	require.Less(t, elapsed, dispatchFastDeliveryAckWait)

	first := <-results
	require.Equal(t, uint64(1), first.attempt)
	require.Equal(t, natsclient.DeliveryDecisionRetry, first.decision)
	require.ErrorIs(t, first.err, context.DeadlineExceeded)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Retry did not leave the source available for redelivery")
	}
	second := <-results
	require.Equal(t, uint64(2), second.attempt)
	require.Equal(t, natsclient.DeliveryDecisionAck, second.decision)
	require.NoError(t, second.err)
	require.False(t, overlap.Load(), "one source delivery ran concurrently with its redelivery")
	require.Equal(t, int32(0), active.Load(), "all delivery work must join before callback return")
}
