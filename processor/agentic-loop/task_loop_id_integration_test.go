//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	ruleprocessor "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type taskLoopIDStreamPublisher struct {
	client  *natsclient.Client
	subject string
	data    []byte
}

func (p *taskLoopIDStreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	p.subject = subject
	p.data = append([]byte(nil), data...)
	return p.client.PublishToStream(ctx, subject, data)
}

// taskLoopIDDelivery interrupts only the first owner's source ACK. Metadata,
// heartbeats, and every other server method remain the actual JetStream delivery.
// Callback completion transfers the ACK count to the test goroutine.
type taskLoopIDDelivery struct {
	jetstream.Msg
	interruptAck bool
	acks         int
}

func (m *taskLoopIDDelivery) Ack() error {
	m.acks++
	if m.interruptAck {
		return errors.New("test replacement interrupted source ACK after task birth")
	}
	return m.Msg.Ack()
}

// spec: entity-id-contract / A loop instance token is minted at its framework birth seam
// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	registry := payloadbuiltins.NewTestRegistry(t)
	const suffix = "rule-task-identity-replacement"
	const consumerName = "agentic-loop-agent-task-any-" + suffix
	stopProcess := func(c *Component) {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.Stop(stopCtx))
	}
	newProcess := func() *Component {
		discoverable, componentErr := NewComponent([]byte(`{"consumer_name_suffix":"`+suffix+`"}`), component.Dependencies{
			NATSClient: tc.Client, PayloadRegistry: registry,
		})
		require.NoError(t, componentErr)
		c := discoverable.(*Component)
		// Graph evidence is nonblocking; task state and required publications remain real.
		c.graphWriter = nil
		t.Cleanup(func() { stopProcess(c) })
		return c
	}

	first := newProcess()
	firstReturned := make(chan *taskLoopIDDelivery, 1)
	var firstTaskHandle jetstream.ConsumeContext
	first.consumeStream = func(
		setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		handle, consumeErr := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				if owner.Port != "agent.task" {
					handler(msgCtx, msg)
					return
				}
				observed := &taskLoopIDDelivery{Msg: msg, interruptAck: true}
				handler(msgCtx, observed)
				firstReturned <- observed
			})
		if owner.Port == "agent.task" {
			firstTaskHandle = handle
		}
		return handle, consumeErr
	}
	require.NoError(t, first.Start(ctx))

	publisher := &taskLoopIDStreamPublisher{client: tc.Client}
	executor := ruleprocessor.NewActionExecutorFull(nil, nil, publisher, types.PlatformMeta{
		Org: "c360", Platform: "test",
	})
	entityID := semantictest.EntityID(t, "c360", "test", "sensor", "weather", "station", "one")
	require.NoError(t, executor.Execute(ctx, ruleprocessor.Action{
		Type:    ruleprocessor.ActionTypePublishAgent,
		Subject: "agent.task.replacement",
		Role:    "general",
		Model:   "test-model",
		Prompt:  "inspect replacement behavior",
	}, &ruleprocessor.ExecutionContext{EntityID: entityID}))

	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	stored, err := stream.GetLastMsgForSubject(ctx, publisher.subject)
	require.NoError(t, err)
	require.Equal(t, publisher.data, stored.Data, "the loop must receive the exact registered bytes emitted by the rule")

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(stored.Data)
	require.NoError(t, err)
	task, ok := decoded.Payload().(*agentic.TaskMessage)
	require.Truef(t, ok, "expected registered *agentic.TaskMessage, got %T", decoded.Payload())
	require.NoError(t, task.Validate())
	parsedLoopID, err := uuid.Parse(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), parsedLoopID.Version())

	var firstDelivery *taskLoopIDDelivery
	select {
	case firstDelivery = <-firstReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("started loop did not reach source ACK for the rule-produced task")
	}
	require.Equal(t, 1, firstDelivery.acks)
	firstMetadata, err := firstDelivery.Metadata()
	require.NoError(t, err)
	require.Equal(t, "AGENT", firstMetadata.Stream)
	require.Equal(t, consumerName, firstMetadata.Consumer)
	require.Equal(t, stored.Sequence, firstMetadata.Sequence.Stream)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)
	require.Equal(t, stored.Data, firstDelivery.Data())
	beforeReplacement, err := first.loopsBucket.Get(ctx, task.LoopID)
	require.NoError(t, err, "loop birth must commit before the interrupted source ACK")
	var firstDurable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(beforeReplacement.Value(), &firstDurable))
	require.Equal(t, task.LoopID, firstDurable.ID)
	require.Equal(t, task.TaskID, firstDurable.TaskID)

	// Stop joins the real subscriptions before the replacement constructor starts.
	// The server retains its production BackOff and genuinely unacknowledged source.
	stopProcess(first)
	select {
	case <-firstTaskHandle.Closed():
	default:
		t.Fatal("first loop Stop returned before its task consumer closed")
	}
	consumer, err := stream.Consumer(ctx, consumerName)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, firstMetadata.Sequence.Stream)
	firstEntity, err := first.handler.loopManager.GetLoop(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, task.LoopID, firstEntity.ID)
	require.Equal(t, task.TaskID, firstEntity.TaskID)
	require.Len(t, first.handler.loopManager.loops, 1)

	second := newProcess()
	require.Empty(t, second.handler.loopManager.loops, "replacement must have no process-local loop identity")
	secondReturned := make(chan *taskLoopIDDelivery, 1)
	second.consumeStream = func(
		setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				if owner.Port != "agent.task" {
					handler(msgCtx, msg)
					return
				}
				observed := &taskLoopIDDelivery{Msg: msg}
				handler(msgCtx, observed)
				secondReturned <- observed
			})
	}
	require.NoError(t, second.Start(ctx))
	var redelivered *taskLoopIDDelivery
	select {
	case redelivered = <-secondReturned:
	case <-time.After(40 * time.Second): // Production task BackOff begins at 30 seconds.
		t.Fatal("started replacement did not receive the actual unacknowledged task redelivery")
	}
	metadata, err := redelivered.Metadata()
	require.NoError(t, err)
	require.Equal(t, firstMetadata.Stream, metadata.Stream)
	require.Equal(t, firstMetadata.Consumer, metadata.Consumer)
	require.Equal(t, firstMetadata.Sequence.Stream, metadata.Sequence.Stream)
	require.Greater(t, metadata.NumDelivered, firstMetadata.NumDelivered)
	require.Equal(t, firstDelivery.Data(), redelivered.Data())
	require.Equal(t, 1, redelivered.acks)
	require.Eventually(t, func() bool {
		settled, infoErr := consumer.Info(ctx)
		return infoErr == nil && settled.NumAckPending == 0 && settled.AckFloor.Stream >= metadata.Sequence.Stream
	}, 5*time.Second, 10*time.Millisecond, "server did not observe replacement source ACK")
	stopProcess(second)
	secondEntity, err := second.handler.loopManager.GetLoop(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, firstEntity.ID, secondEntity.ID)
	require.Equal(t, firstEntity.TaskID, secondEntity.TaskID)
	require.Len(t, second.handler.loopManager.loops, 1)

	keys, err := second.loopsBucket.Keys(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{task.LoopID}, keys, "replacement must not create a second durable loop identity")
	durable, err := second.loopsBucket.Get(ctx, task.LoopID)
	require.NoError(t, err)
	var durableEntity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(durable.Value(), &durableEntity))
	require.Equal(t, task.LoopID, durableEntity.ID)
	require.Equal(t, task.TaskID, durableEntity.TaskID)
}
