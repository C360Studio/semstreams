//go:build integration

package agenticgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type governancePublishedMessage struct {
	subject string
	message Message
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestIntegrationGovernanceProductionBindingsPublishDeclaredConsequences(t *testing.T) {
	testClient := natsclient.NewTestClient(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outputs := make(chan governancePublishedMessage, 3)
	sub, err := testClient.Client.Subscribe(ctx, "agent.*.validated.*", func(_ context.Context, msg *nats.Msg) {
		var decoded Message
		if decodeErr := json.Unmarshal(msg.Data, &decoded); decodeErr != nil {
			return
		}
		outputs <- governancePublishedMessage{subject: msg.Subject, message: decoded}
	})
	require.NoError(t, err)

	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	type capturedBinding struct {
		cfg      natsclient.StreamConsumerConfig
		callback func(context.Context, jetstream.Msg)
	}
	captured := make(map[string]capturedBinding)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		captured[owner.Port] = capturedBinding{cfg: cfg, callback: callback}
		return &governanceFastOwnerHandle{closed: make(chan struct{})}, nil
	}
	require.NoError(t, c.setupInputConsumers(ctx))

	rows := []struct {
		port      string
		msgType   MessageType
		subject   string
		messageID string
	}{
		{port: "task_validation", msgType: MessageTypeTask, subject: "agent.task.validated.message-0", messageID: "message-0"},
		{port: "request_validation", msgType: MessageTypeRequest, subject: "agent.request.validated.message-1", messageID: "message-1"},
		{port: "response_validation", msgType: MessageTypeResponse, subject: "agent.response.validated.message-2", messageID: "message-2"},
	}
	for _, row := range rows {
		binding, ok := captured[row.port]
		require.True(t, ok, "production setup did not bind %s", row.port)
		require.Equal(t, governanceFastDeliveryAckWait, binding.cfg.AckWait)
		require.Equal(t, governanceFastDeliveryAckWait, binding.cfg.MessageTimeout)
		data, marshalErr := json.Marshal(Message{ID: row.messageID, Content: Content{Text: "clean"}})
		require.NoError(t, marshalErr)
		msg := &governanceFastOwnerMsg{data: data}
		binding.callback(ctx, msg)
		require.Equal(t, int32(1), msg.acks.Load(), "%s validated publication must precede ACK", row.port)
		require.Zero(t, msg.naks.Load())
		require.Zero(t, msg.terms.Load())
		select {
		case published := <-outputs:
			require.Equal(t, row.subject, published.subject)
			require.Equal(t, row.msgType, published.message.Type)
			require.Equal(t, row.messageID, published.message.ID)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not publish its required validated output", row.port)
		}
	}

	require.NoError(t, sub.Drain(t.Context()))
	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

type governanceFastDeliveryObservation struct {
	attempt  uint64
	decision natsclient.DeliveryDecision
	err      error
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestIntegrationGovernanceFastDeliveryOwnersCancelJoinAndRetry(t *testing.T) {
	t.Parallel()

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{
			Name: "GOVERNANCE_FAST_OWNER",
			Subjects: []string{
				"agent.task.fast-owner",
				"agent.request.fast-owner",
				"agent.response.fast-owner",
			},
		},
	))

	for index, lane := range []struct {
		port    string
		subject string
	}{
		{port: "task_validation", subject: "agent.task.fast-owner"},
	} {
		t.Run(lane.port, func(t *testing.T) {
			exerciseGovernanceFastDeliveryBoundary(t, testClient, lane.port, lane.subject, index)
		})
	}
}

func exerciseGovernanceFastDeliveryBoundary(
	t *testing.T,
	testClient *natsclient.TestClient,
	port string,
	subject string,
	index int,
) {
	t.Helper()
	ctx := t.Context()
	consumerName := fmt.Sprintf("governance-fast-owner-%d", index)
	results := make(chan governanceFastDeliveryObservation, 2)
	started := make(chan time.Time, 2)
	joined := make(chan error, 1)
	var invocations atomic.Int32
	var active atomic.Int32
	var overlap atomic.Bool

	cfg := natsclient.StreamConsumerConfig{
		StreamName: "GOVERNANCE_FAST_OWNER", ConsumerName: consumerName, FilterSubject: subject,
		DeliverPolicy: "all", AckPolicy: "explicit", AckWait: governanceFastDeliveryAckWait,
		MaxDeliver: 3, MaxAckPending: 10, MessageTimeout: governanceFastDeliveryAckWait,
	}
	handle, err := testClient.Client.ConsumeStreamWithConfig(
		ctx,
		natsclient.PortConsumerContext{Component: "agentic-governance", Port: port},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			metadata, metadataErr := msg.Metadata()
			attempt := uint64(0)
			if metadataErr == nil && metadata != nil {
				attempt = metadata.NumDelivered
			}
			decision, deliveryErr := consumeGovernanceFastDelivery(
				msgCtx,
				msg,
				func(workCtx context.Context, _ []byte) (natsclient.DeliveryDecision, error) {
					if active.Add(1) != 1 {
						overlap.Store(true)
					}
					defer active.Add(-1)
					call := invocations.Add(1)
					started <- time.Now()
					if call == 1 {
						<-workCtx.Done()
						joined <- workCtx.Err()
						return natsclient.DeliveryDecisionRetry, workCtx.Err()
					}
					return natsclient.DeliveryDecisionAck, nil
				},
			)
			results <- governanceFastDeliveryObservation{
				attempt: attempt, decision: decision, err: errors.Join(metadataErr, deliveryErr),
			}
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		handle.Drain()
		select {
		case <-handle.Closed():
		case <-time.After(5 * time.Second):
			t.Errorf("%s consume handle did not close", port)
		}
	})

	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, cfg.StreamName, consumerName)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, governanceFastDeliveryAckWait, info.Config.AckWait)

	require.NoError(t, testClient.Client.PublishToStream(ctx, subject, []byte(port)))
	firstStarted := <-started
	select {
	case joinErr := <-joined:
		require.ErrorIs(t, joinErr, context.DeadlineExceeded)
	case <-time.After(governanceFastDeliveryAckWait + 5*time.Second):
		t.Fatal("cooperative delivery work was not canceled and joined before AckWait")
	}
	// This is the one deliberate wall-clock assertion: it observes the real
	// 25s owner deadline while allowing scheduler jitter inside the 5s margin.
	elapsed := time.Since(firstStarted)
	require.GreaterOrEqual(t, elapsed, governanceFastDeliveryWorkBudget-time.Second)
	require.Less(t, elapsed, governanceFastDeliveryAckWait)

	first := <-results
	require.Equal(t, uint64(1), first.attempt)
	require.Equal(t, natsclient.DeliveryDecisionRetry, first.decision)
	require.ErrorIs(t, first.err, context.DeadlineExceeded)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Retry did not leave the source available for redelivery")
	}
	second := <-results
	require.Equal(t, uint64(2), second.attempt)
	require.Equal(t, natsclient.DeliveryDecisionAck, second.decision)
	require.NoError(t, second.err)
	require.False(t, overlap.Load(), "one source delivery ran concurrently with its redelivery")
	require.Equal(t, int32(0), active.Load(), "all delivery work must join before callback return")
}
