//go:build integration

package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegrationConsumeDeliveryWithHeartbeatHealthyRenewalPreventsOverlap(t *testing.T) {
	ctx := t.Context()
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "DELIVERY_RENEWAL", Subjects: []string{"delivery.renewal"}},
	))

	const firstBackOff = 400 * time.Millisecond
	cfg := StreamConsumerConfig{
		StreamName:    "DELIVERY_RENEWAL",
		ConsumerName:  "delivery-renewal",
		FilterSubject: "delivery.renewal",
		AckWait:       5 * time.Second,
		BackOff:       []time.Duration{firstBackOff, 800 * time.Millisecond},
		MaxAckPending: 2,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}

	started := make(chan DeliveryAttempt, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	results := make(chan DeliveryResult, 2)
	var invocations atomic.Int32
	policy, err := ValidateHeartbeatDeliveryPolicy(
		ctx,
		cfg,
		100*time.Millisecond,
		ImmediateDeliveryRetry(),
		func(workCtx context.Context, attempt DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
			invocations.Add(1)
			started <- attempt
			select {
			case <-release:
				return DeliveryDecisionAck, nil
			case <-workCtx.Done():
				return DeliveryDecisionQuarantine, workCtx.Err()
			}
		},
	)
	require.NoError(t, err)

	handle, err := testClient.Client.ConsumeStreamWithConfig(
		ctx,
		PortConsumerContext{Component: "integration", Port: "renewal"},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			results <- ConsumeDeliveryWithHeartbeat(msgCtx, msg, policy)
		},
	)
	require.NoError(t, err)
	defer drainNativeConsume(t, handle)
	defer releaseOnce.Do(func() { close(release) })

	require.NoError(t, testClient.Client.PublishToStream(ctx, "delivery.renewal", []byte("long-work")))
	select {
	case attempt := <-started:
		require.Equal(t, uint64(1), attempt.Number())
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery did not start")
	}

	// Three full first-redelivery intervals give the server repeated chances
	// to overlap the same delivery if InProgress is not renewing its lease.
	renewalWindow := 3 * firstBackOff
	select {
	case attempt := <-started:
		releaseOnce.Do(func() { close(release) })
		t.Fatalf("healthy renewal allowed overlapping attempt %d", attempt.Number())
	case <-time.After(renewalWindow):
	}

	releaseOnce.Do(func() { close(release) })
	select {
	case result := <-results:
		require.Equal(t, DeliveryDecisionAck, result.Decision())
		require.NoError(t, result.Err())
	case <-time.After(5 * time.Second):
		t.Fatal("renewed delivery did not settle")
	}
	require.Equal(t, int32(1), invocations.Load())
}

func TestIntegrationConsumeDeliveryWithHeartbeatStoppedRenewalUsesBackOff(t *testing.T) {
	ctx := t.Context()
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "DELIVERY_BACKOFF", Subjects: []string{"delivery.backoff"}},
	))

	first, err := NewClient(testClient.URL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, first.Connect(ctx))
	defer first.Close(context.Background())

	const (
		firstBackOff  = 800 * time.Millisecond
		semanticDelay = 4 * time.Second
	)
	cfg := StreamConsumerConfig{
		StreamName:    "DELIVERY_BACKOFF",
		ConsumerName:  "delivery-backoff",
		FilterSubject: "delivery.backoff",
		AckWait:       8 * time.Second,
		BackOff:       []time.Duration{firstBackOff, 2 * time.Second},
		MaxAckPending: 1,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}
	retry, err := DelayedDeliveryRetry(semanticDelay)
	require.NoError(t, err)
	started := make(chan time.Time, 1)
	results := make(chan DeliveryResult, 1)
	retryCause := errors.New("retry after renewal stops")
	policy, err := ValidateHeartbeatDeliveryPolicy(
		ctx,
		cfg,
		100*time.Millisecond,
		retry,
		func(workCtx context.Context, _ DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
			started <- time.Now()
			<-workCtx.Done()
			return DeliveryDecisionRetry, retryCause
		},
	)
	require.NoError(t, err)

	_, err = first.ConsumeStreamWithConfig(
		ctx,
		PortConsumerContext{Component: "integration", Port: "backoff"},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			results <- ConsumeDeliveryWithHeartbeat(msgCtx, msg, policy)
		},
	)
	require.NoError(t, err)
	require.NoError(t, testClient.Client.PublishToStream(ctx, "delivery.backoff", []byte("restart")))

	var firstStarted time.Time
	select {
	case firstStarted = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery did not start")
	}

	// Closing the delivery owner's connection stops renewal and makes any
	// explicit semantic retry method fail. JetStream must therefore redeliver
	// on BackOff, independently of both the longer AckWait and semantic delay.
	first.GetConnection().Close()
	select {
	case result := <-results:
		require.Equal(t, DeliveryDecisionRetry, result.Decision())
		require.ErrorIs(t, result.Err(), retryCause)
		require.False(t, result.SettlementMethodSucceeded())
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not return after renewal stopped")
	}

	second, err := NewClient(testClient.URL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, second.Connect(ctx))
	defer second.Close(context.Background())
	js, err := second.JetStream()
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, cfg.StreamName, cfg.ConsumerName)
	require.NoError(t, err)
	redelivery := fetchOneDelivery(t, consumer)
	elapsed := time.Since(firstStarted)
	// The lower bound tolerates scheduler/server jitter around the 800ms
	// class. The upper bound is below both the 4s semantic delay and 8s
	// AckWait, proving neither policy supplied the missing-settlement timer.
	require.GreaterOrEqual(t, elapsed, 500*time.Millisecond)
	require.Less(t, elapsed, semanticDelay)
	metadata, err := redelivery.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(2), metadata.NumDelivered)
	require.NoError(t, redelivery.Ack())
}

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
