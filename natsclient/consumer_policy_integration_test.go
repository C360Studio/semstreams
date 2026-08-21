//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegration_MaxAckPendingEffectivePolicy(t *testing.T) {
	ctx := t.Context()
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	js, err := client.JetStream()
	require.NoError(t, err)
	for index, requested := range []int{7, 0, -1} {
		t.Run(fmt.Sprintf("requested_%d", requested), func(t *testing.T) {
			streamName := fmt.Sprintf("POLICY_%d", index)
			subject := fmt.Sprintf("policy.%d", index)
			consumerName := fmt.Sprintf("policy-consumer-%d", index)
			_, ensureErr := client.EnsureStream(ctx, jetstream.StreamConfig{
				Name: streamName, Subjects: []string{subject}, Storage: jetstream.MemoryStorage,
				MaxAge: testStreamMaxAge, MaxBytes: testStreamMaxBytes,
			})
			require.NoError(t, ensureErr)

			handle, consumeErr := client.ConsumeStreamWithConfig(ctx,
				PortConsumerContext{Component: "integration", Port: subject},
				StreamConsumerConfig{
					StreamName: streamName, ConsumerName: consumerName, FilterSubject: subject,
					AckPolicy: "explicit", MaxAckPending: requested,
				}, func(context.Context, jetstream.Msg) {})
			require.NoError(t, consumeErr)
			t.Cleanup(func() { drainNativeConsume(t, handle) })

			consumer, consumerErr := js.Consumer(ctx, streamName, consumerName)
			require.NoError(t, consumerErr)
			info, infoErr := consumer.Info(ctx)
			require.NoError(t, infoErr)
			if requested == 0 {
				require.Positive(t, info.Config.MaxAckPending,
					"zero must leave the effective policy to NATS")
				return
			}
			require.Equal(t, requested, info.Config.MaxAckPending)
		})
	}
}

func TestIntegration_MaxAckPendingUpdatesDurableInPlace(t *testing.T) {
	ctx := t.Context()
	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	const (
		streamName   = "POLICY_UPDATE"
		subject      = "policy.update"
		consumerName = "policy-update-consumer"
	)
	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{subject}, Storage: jetstream.MemoryStorage,
		MaxAge: testStreamMaxAge, MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	acked := make(chan struct{}, 1)
	start := func(maxAckPending int, handler func(context.Context, jetstream.Msg)) jetstream.ConsumeContext {
		t.Helper()
		handle, consumeErr := client.ConsumeStreamWithConfig(ctx,
			PortConsumerContext{Component: "integration", Port: "input"},
			StreamConsumerConfig{
				StreamName: streamName, ConsumerName: consumerName, FilterSubject: subject,
				DeliverPolicy: "all", AckPolicy: "explicit", MaxAckPending: maxAckPending,
			}, handler)
		require.NoError(t, consumeErr)
		return handle
	}
	firstHandle := start(3, func(messageCtx context.Context, msg jetstream.Msg) {
		if ackErr := msg.DoubleAck(messageCtx); ackErr != nil {
			return
		}
		select {
		case acked <- struct{}{}:
		default:
		}
	})
	_, err = client.PublishToStreamWithAck(ctx, subject, []byte("establish durable position"))
	require.NoError(t, err)
	select {
	case <-acked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for acknowledged delivery")
	}

	js, err := client.JetStream()
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, streamName, consumerName)
	require.NoError(t, err)
	before, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, before.Config.MaxAckPending)
	require.Positive(t, before.AckFloor.Stream)

	drainNativeConsume(t, firstHandle)
	waitForInternalClaimRelease(t, client, internalConsumerIdentity{stream: streamName, durable: consumerName})
	secondHandle := start(9, func(_ context.Context, msg jetstream.Msg) { _ = msg.Ack() })
	defer drainNativeConsume(t, secondHandle)

	consumer, err = js.Consumer(ctx, streamName, consumerName)
	require.NoError(t, err)
	after, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, before.Created, after.Created, "durable consumer was replaced instead of updated")
	require.Equal(t, before.AckFloor.Stream, after.AckFloor.Stream,
		"in-place policy update reset the durable position")
	require.Equal(t, 9, after.Config.MaxAckPending)
}
