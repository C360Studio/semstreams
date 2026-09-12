//go:build integration

package agenticloop

import (
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationOrdinaryLoopPublicationsMayRepeat(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "LOOP_PUBLICATION_AT_LEAST_ONCE", Subjects: []string{"agent.>"}},
	))
	c := &Component{natsClient: tc.Client}
	messages := []PublishedMessage{
		{Subject: "agent.created.at-least-once-created", Data: []byte(`{"kind":"created"}`)},
		{Subject: "agent.request.at-least-once-request", Data: []byte(`{"kind":"request"}`)},
		{Subject: "agent.approval_pending.at-least-once-approval", Data: []byte(`{"kind":"approval"}`)},
		{Subject: "agent.request.at-least-once-continuation", Data: []byte(`{"kind":"continuation"}`)},
		{Subject: "agent.complete.at-least-once-terminal", Data: []byte(`{"kind":"terminal"}`)},
	}

	require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))
	require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))

	stream, err := c.natsClient.GetStream(ctx, "LOOP_PUBLICATION_AT_LEAST_ONCE")
	require.NoError(t, err)
	for index, published := range messages {
		consumer, createErr := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Name:          fmt.Sprintf("loop-at-least-once-%d", index),
			FilterSubject: published.Subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
		})
		require.NoError(t, createErr)
		batch, fetchErr := consumer.Fetch(2, jetstream.FetchMaxWait(3*time.Second))
		require.NoError(t, fetchErr)
		count := 0
		for msg := range batch.Messages() {
			count++
			require.NoError(t, msg.Ack())
		}
		require.NoError(t, batch.Error())
		require.Equal(t, 2, count, "%s may repeat after publication uncertainty", published.Subject)
	}
}
