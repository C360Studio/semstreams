//go:build integration

package agenticloop

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationTaskConsumerSerializesRedeliveryBeforeLaterWork(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT_TASK_SERIAL", Subjects: []string{"agent.task.*"}},
	))
	port, err := (component.PortDefinition{
		Name: "agent.task",
		Config: component.JetStreamPort{
			StreamName: "AGENT_TASK_SERIAL", Subjects: []string{"agent.task.*"},
		},
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)
	_, maxAckPending, err := agenticLoopConsumerPolicy(port)
	require.NoError(t, err)
	require.Equal(t, 1, maxAckPending)

	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "AGENT_TASK_SERIAL")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "agentic-loop-task-serial-proof",
		FilterSubject: "agent.task.*",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       5 * time.Second,
		MaxAckPending: maxAckPending,
	})
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.role", []byte("task-n")))
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.role", []byte("task-n-plus-1")))

	batch, err := consumer.Fetch(2, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	first, ok := <-batch.Messages()
	require.True(t, ok)
	require.Equal(t, []byte("task-n"), first.Data())
	firstMetadata, err := first.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)

	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending,
		"the unacked task did not occupy the component-owned sole pending slot")
	require.Equal(t, uint64(1), info.NumPending,
		"the later task became deliverable while the older task was unacked")

	// DoubleAck is the authoritative server confirmation. Once it returns,
	// task N is no longer eligible for redelivery and the blocked N+1 delivery
	// may occupy the sole pending slot.
	require.NoError(t, first.DoubleAck(ctx))
	second, ok := <-batch.Messages()
	require.True(t, ok)
	require.Equal(t, []byte("task-n-plus-1"), second.Data())
	secondMetadata, err := second.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(1), secondMetadata.NumDelivered,
		"server-confirmed task N ACK was followed by an older-task redelivery")
	require.NoError(t, second.DoubleAck(ctx))

	info, err = consumer.Info(ctx)
	require.NoError(t, err)
	require.Zero(t, info.NumAckPending)
	require.Zero(t, info.NumPending)
}
