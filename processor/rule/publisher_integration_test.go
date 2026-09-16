//go:build integration

package rule

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
// Real NATS proves that actionPublisher waits for a stream acknowledgement:
// core NATS can deliver bytes without one, including after the stream is deleted.
// One isolated container owns every stream and subscription in this test.
func TestIntegration_ActionPublisherUsesDeclaredTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	namespace := "r8_" + uuid.NewString()
	subject := "agent.task." + namespace
	streamName := "PUBLISHER_" + namespace
	stream, err := testClient.CreateStream(ctx, streamName, []string{"agent.task.*"})
	require.NoError(t, err)
	processor := &Processor{
		config: &Config{Ports: &component.PortConfig{Outputs: []component.PortDefinition{
			{Name: "core-first", Config: component.NATSPort{Subject: subject}},
			{Name: "agent_task", Config: component.JetStreamPort{
				StreamName: streamName, Subjects: []string{"agent.task.*"},
			}},
		}}},
		natsClient: testClient.Client,
		metrics:    publisherContractMetrics(),
	}
	require.NoError(t, processor.setupPorts())
	publisher := newActionPublisher(processor)
	data := []byte(`{"message":"keep these exact bytes", "sequence":1}`)
	require.NoError(t, publisher.Publish(ctx, subject, data))
	stored, err := stream.GetLastMsgForSubject(ctx, subject)
	require.NoError(t, err)
	require.Equal(t, subject, stored.Subject)
	require.Equal(t, data, stored.Data)
	require.EqualValues(t, 1, processor.eventsPublished)
	require.Equal(t, 1.0, testutil.ToFloat64(
		processor.metrics.eventsPublishedTotal.WithLabelValues(subject, "action_publish")))

	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	require.NoError(t, js.DeleteStream(ctx, streamName))
	_, err = js.Stream(ctx, streamName)
	require.ErrorIs(t, err, jetstream.ErrStreamNotFound)

	// The same configured output must now report the missing PubAck. A core
	// fallback would return success, so stored bytes alone are not this proof.
	err = publisher.Publish(ctx, subject, data)
	require.ErrorIs(t, err, jetstream.ErrNoStreamResponse)
	require.True(t, errs.IsTransient(err))
	require.EqualValues(t, 1, processor.eventsPublished, "missing PubAck must not count as publication")
	require.Equal(t, 1.0, testutil.ToFloat64(
		processor.metrics.eventsPublishedTotal.WithLabelValues(subject, "action_publish")))

	// With no stream present, a core-only declaration still publishes normally.
	coreSubject := "core." + namespace
	coreProcessor := &Processor{
		config: &Config{Ports: &component.PortConfig{Outputs: []component.PortDefinition{
			{Name: "core-output", Config: component.NATSPort{Subject: coreSubject}},
		}}},
		natsClient: testClient.Client,
		metrics:    publisherContractMetrics(),
	}
	require.NoError(t, coreProcessor.setupPorts())
	connection := testClient.GetNativeConnection()
	subscription, err := connection.SubscribeSync(coreSubject)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, subscription.Unsubscribe()) })
	require.NoError(t, connection.FlushWithContext(ctx))
	require.NoError(t, newActionPublisher(coreProcessor).Publish(ctx, coreSubject, data))
	delivered, err := subscription.NextMsgWithContext(ctx)
	require.NoError(t, err)
	require.Equal(t, coreSubject, delivered.Subject)
	require.Equal(t, data, delivered.Data)
	require.EqualValues(t, 1, coreProcessor.eventsPublished)
	require.Equal(t, 1.0, testutil.ToFloat64(
		coreProcessor.metrics.eventsPublishedTotal.WithLabelValues(coreSubject, "action_publish")))
}
