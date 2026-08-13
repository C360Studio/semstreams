//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationConsumeWithHeartbeatAckFailureLeavesDeliveryForRedelivery(t *testing.T) {
	ctx := t.Context()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(context.Background())

	first, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, first.Connect(ctx))
	defer first.Close(context.Background())
	stream, err := first.EnsureStream(ctx, jetstream.StreamConfig{
		Name: "HEARTBEAT_ACK_FAILURE", Subjects: []string{"heartbeat.ack.failure"},
		MaxAge: testStreamMaxAge, MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "ack-failure", FilterSubject: "heartbeat.ack.failure",
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: 250 * time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, first.PublishToStream(ctx, "heartbeat.ack.failure", []byte("redeliver-me")))
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg)

	first.GetConnection().Close()
	err = ConsumeWithHeartbeat(ctx, msg, time.Second, func(context.Context) error { return nil })
	require.Error(t, err, "failed ACK must remain visible to the caller")

	second, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, second.Connect(ctx))
	defer second.Close(context.Background())
	js, err := second.JetStream()
	require.NoError(t, err)
	rebound, err := js.Consumer(ctx, "HEARTBEAT_ACK_FAILURE", "ack-failure")
	require.NoError(t, err)
	redelivery, err := rebound.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)
	redelivered := <-redelivery.Messages()
	require.NotNil(t, redelivered)
	assert.Equal(t, []byte("redeliver-me"), redelivered.Data())
	require.NoError(t, redelivered.Ack())
}

func TestIntegrationConsumeWithHeartbeatShutdownDelayedNAKRedelivers(t *testing.T) {
	ctx := t.Context()
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "HEARTBEAT_SHUTDOWN", Subjects: []string{"heartbeat.shutdown"}},
	))
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "HEARTBEAT_SHUTDOWN")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "shutdown", FilterSubject: "heartbeat.shutdown", AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, testClient.Client.PublishToStream(ctx, "heartbeat.shutdown", []byte("shutdown")))
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg)
	shutdownCtx, cancel := context.WithCancel(ctx)
	cancel()
	err = ConsumeWithHeartbeat(shutdownCtx, msg, time.Second,
		func(workCtx context.Context) error { <-workCtx.Done(); return workCtx.Err() })
	require.ErrorIs(t, err, context.Canceled)

	redelivery, err := consumer.Fetch(1, jetstream.FetchMaxWait(7*time.Second))
	require.NoError(t, err)
	redelivered := <-redelivery.Messages()
	require.NotNil(t, redelivered, "five-second shutdown NAK must redeliver")
	require.NoError(t, redelivered.Ack())
}

func TestIntegrationConsumeWithHeartbeatFailureLeavesDeliveryUnsettled(t *testing.T) {
	ctx := t.Context()
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "HEARTBEAT_FAILURE", Subjects: []string{"heartbeat.failure"}},
	))
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "HEARTBEAT_FAILURE")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "heartbeat", FilterSubject: "heartbeat.failure", AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	publisher, err := NewClient(testClient.URL)
	require.NoError(t, err)
	require.NoError(t, publisher.Connect(ctx))
	defer publisher.Close(context.Background())
	require.NoError(t, publisher.PublishToStream(ctx, "heartbeat.failure", []byte("heartbeat")))
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg)
	testClient.GetNativeConnection().Close()
	err = ConsumeWithHeartbeat(ctx, msg, 25*time.Millisecond,
		func(workCtx context.Context) error { <-workCtx.Done(); return workCtx.Err() })
	require.ErrorIs(t, err, ErrHeartbeatFailed)

	second, err := NewClient(testClient.URL)
	require.NoError(t, err)
	require.NoError(t, second.Connect(ctx))
	defer second.Close(context.Background())
	secondJS, err := second.JetStream()
	require.NoError(t, err)
	rebound, err := secondJS.Consumer(ctx, "HEARTBEAT_FAILURE", "heartbeat")
	require.NoError(t, err)
	redelivery, err := rebound.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)
	redelivered := <-redelivery.Messages()
	require.NotNil(t, redelivered, "heartbeat failure must leave delivery for AckWait redelivery")
	require.NoError(t, redelivered.Ack())
}
