//go:build integration

package agenticloop

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

type loopFastDeliveryObservation struct {
	attempt  uint64
	decision natsclient.DeliveryDecision
	err      error
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationLoopControlFastDeliveryOwnersCancelJoinAndRetry(t *testing.T) {
	t.Parallel()
	exerciseLoopFastDeliveryGroup(t, "LOOP_CONTROL_FAST_OWNER", []loopFastDeliveryLane{
		{port: "agent.approval_response", subject: "agent.approval_response.fast-owner"},
	})
}

type loopFastDeliveryLane struct {
	port    string
	subject string
}

func exerciseLoopFastDeliveryGroup(t *testing.T, streamName string, lanes []loopFastDeliveryLane) {
	t.Helper()
	subjects := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		subjects = append(subjects, lane.subject)
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: streamName, Subjects: subjects},
	))
	for index, lane := range lanes {
		t.Run(lane.port, func(t *testing.T) {
			exerciseLoopFastDeliveryBoundary(t, testClient, streamName, lane.port, lane.subject, index)
		})
	}
}

func exerciseLoopFastDeliveryBoundary(
	t *testing.T,
	testClient *natsclient.TestClient,
	streamName string,
	port string,
	subject string,
	index int,
) {
	t.Helper()
	ctx := t.Context()
	consumerName := fmt.Sprintf("loop-fast-owner-%s-%d", streamName, index)
	results := make(chan loopFastDeliveryObservation, 2)
	started := make(chan time.Time, 2)
	joined := make(chan error, 1)
	var invocations atomic.Int32
	var active atomic.Int32
	var overlap atomic.Bool

	cfg := natsclient.StreamConsumerConfig{
		StreamName: streamName, ConsumerName: consumerName, FilterSubject: subject,
		DeliverPolicy: "all", AckPolicy: "explicit", AckWait: loopFastDeliveryAckWait,
		MaxDeliver: 3, MaxAckPending: 10, MessageTimeout: loopFastDeliveryAckWait,
	}
	handle, err := testClient.Client.ConsumeStreamWithConfig(
		ctx,
		natsclient.PortConsumerContext{Component: "agentic-loop", Port: port},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			metadata, metadataErr := msg.Metadata()
			attempt := uint64(0)
			if metadataErr == nil && metadata != nil {
				attempt = metadata.NumDelivered
			}
			decision, deliveryErr := consumeLoopFastDelivery(
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
			results <- loopFastDeliveryObservation{
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
	require.Equal(t, loopFastDeliveryAckWait, info.Config.AckWait)

	require.NoError(t, testClient.Client.PublishToStream(ctx, subject, []byte(port)))
	firstStarted := <-started
	select {
	case joinErr := <-joined:
		require.ErrorIs(t, joinErr, context.DeadlineExceeded)
	case <-time.After(loopFastDeliveryAckWait + 5*time.Second):
		t.Fatal("cooperative delivery work was not canceled and joined before AckWait")
	}
	// This is the one deliberate wall-clock assertion: it observes the real
	// 25s owner deadline while allowing scheduler jitter inside the 5s margin.
	elapsed := time.Since(firstStarted)
	require.GreaterOrEqual(t, elapsed, loopFastDeliveryWorkBudget-time.Second)
	require.Less(t, elapsed, loopFastDeliveryAckWait)

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
