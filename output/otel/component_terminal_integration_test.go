//go:build integration

package otel

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegrationConsumerTermsFlatTerminalIntentWithoutAck(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "OTEL_TERMINAL", Subjects: []string{"agent.>"}},
	))
	stream, err := tc.Client.GetStream(ctx, "OTEL_TERMINAL")
	require.NoError(t, err)
	const consumerName = "otel-terminal-flat"
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: consumerName, Durable: consumerName, FilterSubject: "agent.>",
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	require.NoError(t, err)

	advisory, err := tc.Client.GetConnection().SubscribeSync(
		"$JS.EVENT.ADVISORY.CONSUMER.MSG_TERMINATED.OTEL_TERMINAL." + consumerName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = advisory.Unsubscribe() })

	c := &Component{
		config:        Config{ExportTraces: true},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		spanCollector: NewSpanCollector("test", "v1", 1),
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumeEventsFromConsumer(ctx, consumer)
	}()

	flat := []byte(`{"domain":"agentic","category":"loop_completed","version":"v1","payload":{"loop_id":"loop-flat","task_id":"task-flat","outcome":"success","completed_at":"2026-08-12T12:00:00Z"}}`)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete.loop-flat", flat))
	_, err = advisory.NextMsg(10 * time.Second)
	require.NoError(t, err, "flat terminal intent must reach shared decoder and Term")

	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Zero(t, info.NumRedelivered)
	require.Eventually(t, func() bool {
		current, infoErr := consumer.Info(ctx)
		return infoErr == nil && current.NumAckPending == 0
	}, 3*time.Second, 25*time.Millisecond)

	cancel()
	c.wg.Wait()
}
