//go:build integration

package agenticloop

import (
	"context"
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

// spec: agentic-loop / A logical model request has one deterministic identity
//
// Ruling Q5 (#1330): every agent.request publish stamps its deterministic
// RequestID as Nats-Msg-Id, so a stream that declares a Duplicates window
// rejects the second publish of the same logical request server-side. The
// window is a bonus, never the guarantee — agentic-model's retained-response
// reuse is what holds outside it — but a stamped ID is what makes the bonus
// reachable at all, and the neighbouring agent.created publish, which carries
// no MsgID, still repeats in the same call.
func TestIntegrationStampedRequestIDDeduplicatesInsideTheWindow(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	_, err := tc.Client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:       "LOOP_REQUEST_MSGID",
		Subjects:   []string{"agent.>"},
		Duplicates: 30 * time.Second,
		MaxAge:     time.Hour,
		MaxBytes:   64 << 20,
		Discard:    jetstream.DiscardOld,
	})
	require.NoError(t, err)

	c := &Component{natsClient: tc.Client}
	const loopID = "msgid-loop"
	requestID := loopID + ":req:1:0"
	messages := []PublishedMessage{
		{Subject: "agent.request." + requestID, Data: []byte(`{"kind":"request"}`), MsgID: requestID},
		{Subject: "agent.created." + loopID, Data: []byte(`{"kind":"created"}`)},
	}

	require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))
	require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))

	stream, err := c.natsClient.GetStream(ctx, "LOOP_REQUEST_MSGID")
	require.NoError(t, err)
	require.Equal(t, 1, storedMessageCount(ctx, t, stream, "request-msgid", "agent.request."+requestID),
		"the second publish of the same RequestID must be rejected by the server inside the window")
	require.Equal(t, 2, storedMessageCount(ctx, t, stream, "created-msgid", "agent.created."+loopID),
		"a publish with no MsgID is unchanged: at-least-once still repeats")
}

func storedMessageCount(ctx context.Context, t *testing.T, stream jetstream.Stream, name, subject string) int {
	t.Helper()
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          name,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(3, jetstream.FetchMaxWait(2*time.Second))
	require.NoError(t, err)
	count := 0
	for msg := range batch.Messages() {
		count++
		require.NoError(t, msg.Ack())
	}
	require.NoError(t, batch.Error())
	return count
}
