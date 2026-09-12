//go:build integration

package agenticmodel

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-model / Model response publication is durably at-least-once
func TestIntegrationModelResponsePublicationMayRepeat(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "MODEL_RESPONSE_AT_LEAST_ONCE", Subjects: []string{"agent.response.>"}},
	))
	c := &Component{config: DefaultConfig(), natsClient: tc.Client}
	response := agentic.AgentResponse{
		RequestID: "request-at-least-once",
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "done"},
	}

	require.NoError(t, c.publishResponse(ctx, response))
	require.NoError(t, c.publishResponse(ctx, response))

	stream, err := tc.Client.GetStream(ctx, "MODEL_RESPONSE_AT_LEAST_ONCE")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "model-response-at-least-once",
		FilterSubject: "agent.response." + response.RequestID,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(2, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)
	count := 0
	for msg := range batch.Messages() {
		count++
		require.NoError(t, msg.Ack())
	}
	require.NoError(t, batch.Error())
	require.Equal(t, 2, count, "ordinary response publication may repeat with the same RequestID")
}
