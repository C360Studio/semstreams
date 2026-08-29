//go:build integration

package natsclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegrationDeliveryAttemptTracksDurableRedelivery(t *testing.T) {
	ctx := t.Context()
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "DELIVERY_ATTEMPT", Subjects: []string{"delivery.attempt"}},
	))
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "DELIVERY_ATTEMPT")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "delivery-attempt",
		FilterSubject: "delivery.attempt",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, testClient.Client.PublishToStream(ctx, "delivery.attempt", []byte("redeliver")))

	attempts := make(chan DeliveryAttempt, 2)
	retryCause := errors.New("retry once")
	policy, err := ValidateHeartbeatDeliveryPolicy(ctx,
		StreamConsumerConfig{AckWait: 2 * time.Second}, 500*time.Millisecond, ImmediateDeliveryRetry(),
		func(_ context.Context, attempt DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
			attempts <- attempt
			if attempt.Number() == 1 {
				return DeliveryDecisionRetry, retryCause
			}
			return DeliveryDecisionAck, nil
		})
	require.NoError(t, err)

	first := fetchOneDelivery(t, consumer)
	firstResult := ConsumeDeliveryWithHeartbeat(ctx, first, policy)
	require.Equal(t, DeliveryDecisionRetry, firstResult.Decision())
	require.ErrorIs(t, firstResult.Err(), retryCause)
	require.True(t, firstResult.SettlementMethodSucceeded())

	second := fetchOneDelivery(t, consumer)
	secondResult := ConsumeDeliveryWithHeartbeat(ctx, second, policy)
	require.Equal(t, DeliveryDecisionAck, secondResult.Decision())
	require.NoError(t, secondResult.Err())
	firstAttempt := <-attempts
	require.Equal(t, uint64(1), firstAttempt.Number())
	require.True(t, firstAttempt.MetadataAvailable())
	require.False(t, firstAttempt.IsRedelivery())
	secondAttempt := <-attempts
	require.Equal(t, uint64(2), secondAttempt.Number())
	require.True(t, secondAttempt.MetadataAvailable())
	require.True(t, secondAttempt.IsRedelivery())
}

func fetchOneDelivery(t *testing.T, consumer jetstream.Consumer) jetstream.Msg {
	t.Helper()
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg)
	require.NoError(t, batch.Error())
	return msg
}
