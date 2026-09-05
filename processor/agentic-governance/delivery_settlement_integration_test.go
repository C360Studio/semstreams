//go:build integration

package agenticgovernance

import (
	"context"
	"encoding/json"
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
// spec: agentic-governance / Governance publications are durably at-least-once
func TestIntegrationGovernanceProductionCallbacksPublishBeforeAck(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outputs := make(chan governancePublishedMessage, 3)
	sub, err := testClient.Client.Subscribe(ctx, "agent.*.validated.*", func(_ context.Context, msg *nats.Msg) {
		var decoded Message
		if decodeErr := json.Unmarshal(msg.Data, &decoded); decodeErr == nil {
			outputs <- governancePublishedMessage{subject: msg.Subject, message: decoded}
		}
	})
	require.NoError(t, err)

	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &governanceSettlementHandle{closed: make(chan struct{})}, nil
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
		data, marshalErr := json.Marshal(Message{ID: row.messageID, Content: Content{Text: "clean"}})
		require.NoError(t, marshalErr)
		for attempt := range 2 {
			msg := &governanceSettlementMsg{data: data}
			callbacks[row.port](ctx, msg)
			require.Equal(t, int32(1), msg.acks.Load())
			require.Zero(t, msg.naks.Load()+msg.terms.Load())
			select {
			case published := <-outputs:
				require.Equal(t, row.subject, published.subject)
				require.Equal(t, row.msgType, published.message.Type)
				require.Equal(t, row.messageID, published.message.ID)
			case <-time.After(2 * time.Second):
				t.Fatalf("%s attempt %d did not publish its required validated output", row.port, attempt+1)
			}
		}
	}
	require.NoError(t, sub.Drain(t.Context()))
	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}
