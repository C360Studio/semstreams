//go:build integration

package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const (
	restartIdentityUserStream  = "USER_RESTART_IDENTITY"
	restartIdentityAgentStream = "AGENT_RESTART_IDENTITY"
	restartIdentityMessageID   = "user-message-restart-identity"
)

func newRestartIdentityDispatch(t *testing.T, client *natsclient.Client) *Component {
	t.Helper()
	cfg := DefaultConfig()
	for i := range cfg.Ports.Outputs {
		switch cfg.Ports.Outputs[i].Name {
		case "agent.task":
			cfg.Ports.Outputs[i].Config = component.JetStreamPort{
				Subjects: []string{"agent.task.*"}, StreamName: restartIdentityAgentStream,
			}
		case "user.response":
			cfg.Ports.Outputs[i].Config = component.JetStreamPort{
				Subjects: []string{"user.response.>"}, StreamName: restartIdentityUserStream,
			}
		}
	}

	c := &Component{
		config:        cfg,
		modelRegistry: newTestRegistry(),
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		loopTracker:   NewLoopTrackerWithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		registry:      NewCommandRegistry(),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		natsClient:    client,
		decoder:       payloadbuiltins.NewTestDecoder(t),
	}
	c.registerBuiltinCommands()
	return c
}

func fetchRestartIdentityDelivery(
	t *testing.T,
	consumer jetstream.Consumer,
) jetstream.Msg {
	t.Helper()
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)
	msg, ok := <-batch.Messages()
	require.True(t, ok, "expected one source delivery")
	require.NotNil(t, msg)
	require.NoError(t, batch.Error())
	return msg
}

func restartIdentityTask(
	t *testing.T,
	decoder *message.Decoder,
	raw *jetstream.RawStreamMsg,
) *agentic.TaskMessage {
	t.Helper()
	decoded, err := decoder.Decode(raw.Data)
	require.NoError(t, err)
	task, ok := decoded.Payload().(*agentic.TaskMessage)
	require.Truef(t, ok, "stored output payload is %T, not *agentic.TaskMessage", decoded.Payload())
	return task
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
// This is the narrow committed-effect-before-source-ACK window. The 600 ms
// source AckWait deliberately exceeds the 100 ms destination duplicate window
// by 6x, so the second fetch supplies the wait instead of an arbitrary sleep.
// Both dispatch instances run the production decoder and task/user-response
// publication paths against real JetStream; only source settlement is withheld
// to model replacement after both PubAcks and before ACK.
func TestIntegrationUserMessageReplayAfterTaskCommitKeepsOneLogicalTask(t *testing.T) {
	ctx := t.Context()
	const duplicateWindow = 100 * time.Millisecond
	const sourceAckWait = 600 * time.Millisecond

	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
	)
	_, err := tc.Client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:       restartIdentityUserStream,
		Subjects:   []string{"user.message.>", "user.response.>"},
		Storage:    jetstream.MemoryStorage,
		MaxAge:     time.Minute,
		MaxBytes:   1 << 20,
		Discard:    jetstream.DiscardOld,
		Duplicates: duplicateWindow,
	})
	require.NoError(t, err)
	agentStream, err := tc.Client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:       restartIdentityAgentStream,
		Subjects:   []string{"agent.task.>"},
		Storage:    jetstream.MemoryStorage,
		MaxAge:     time.Minute,
		MaxBytes:   1 << 20,
		Discard:    jetstream.DiscardOld,
		Duplicates: duplicateWindow,
	})
	require.NoError(t, err)

	firstClient, err := natsclient.NewClient(tc.URL)
	require.NoError(t, err)
	require.NoError(t, firstClient.Connect(ctx))
	firstUserStream, err := firstClient.GetStream(ctx, restartIdentityUserStream)
	require.NoError(t, err)
	sourceConsumer, err := firstUserStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "agentic-dispatch-restart-identity",
		FilterSubject: "user.message.http",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       sourceAckWait,
		MaxDeliver:    3,
	})
	require.NoError(t, err)

	userMessage := &agentic.UserMessage{
		MessageID:   restartIdentityMessageID,
		ChannelType: "http",
		ChannelID:   "restart-session",
		UserID:      "restart-user",
		Content:     "perform one logical task",
		Timestamp:   time.Now().UTC(),
	}
	sourceData, err := json.Marshal(message.NewBaseMessage(userMessage.Schema(), userMessage, "restart-test"))
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "user.message.http", sourceData))

	firstSource := fetchRestartIdentityDelivery(t, sourceConsumer)
	firstMeta, err := firstSource.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstMeta.NumDelivered)
	firstDispatch := newRestartIdentityDispatch(t, firstClient)
	decision, cause := firstDispatch.handleUserMessage(ctx, firstSource.Data())
	require.Equal(t, natsclient.DeliveryDecisionAck, decision,
		"both required destination publications received PubAck")
	require.NoError(t, cause)

	firstAgentInfo, err := agentStream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstAgentInfo.State.Msgs,
		"the original task must be durable before the source ACK")
	firstRaw, err := agentStream.GetMsg(ctx, firstAgentInfo.State.FirstSeq)
	require.NoError(t, err)
	decoder := payloadbuiltins.NewTestDecoder(t)
	firstTask := restartIdentityTask(t, decoder, firstRaw)
	require.Equal(t, stableDispatchTaskID(*userMessage), firstTask.TaskID)
	require.Equal(t, userMessage.MessageID, firstTask.SourceMessageID)
	require.Equal(t, userMessage.ChannelType, firstTask.ChannelType)
	require.Equal(t, userMessage.ChannelID, firstTask.ChannelID)
	require.Equal(t, userMessage.UserID, firstTask.UserID)
	mintedLoopID, err := uuid.Parse(firstTask.LoopID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), mintedLoopID.Version(), "new work must receive a random UUIDv4 LoopID")
	loops, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	_, err = loops.Get(ctx, firstTask.LoopID)
	require.True(t, natsclient.IsKVNotFoundError(err),
		"the failpoint is before agentic-loop creates AGENT_LOOPS/%s: %v", firstTask.LoopID, err)

	// Replacement closes the first process connection while its source delivery
	// is still unacknowledged. The durable consumer and original task remain on
	// the server.
	require.NoError(t, firstClient.Close(ctx))

	replacementClient, err := natsclient.NewClient(tc.URL)
	require.NoError(t, err)
	require.NoError(t, replacementClient.Connect(ctx))
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = replacementClient.Close(closeCtx)
	})
	replacementUserStream, err := replacementClient.GetStream(ctx, restartIdentityUserStream)
	require.NoError(t, err)
	replacementConsumer, err := replacementUserStream.Consumer(ctx, "agentic-dispatch-restart-identity")
	require.NoError(t, err)
	replacementStarted := time.Now()
	secondSource := fetchRestartIdentityDelivery(t, replacementConsumer)
	require.GreaterOrEqual(t, time.Since(replacementStarted), duplicateWindow,
		"redelivery must occur after the deliberately short destination duplicate window")
	secondMeta, err := secondSource.Metadata()
	require.NoError(t, err)
	require.Equal(t, firstMeta.Sequence.Stream, secondMeta.Sequence.Stream,
		"the replacement must receive the same durable source message")
	require.Equal(t, uint64(2), secondMeta.NumDelivered)

	replacementDispatch := newRestartIdentityDispatch(t, replacementClient)
	// Mutable AutoContinue state has moved on while the source was waiting for
	// redelivery. The committed task is still authoritative for this exact source.
	replacementDispatch.loopTracker.Track(&LoopInfo{
		LoopID:      uuid.NewString(),
		UserID:      userMessage.UserID,
		ChannelType: userMessage.ChannelType,
		ChannelID:   userMessage.ChannelID,
		State:       "pending",
	})
	decision, cause = replacementDispatch.handleUserMessage(ctx, secondSource.Data())
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.NoError(t, cause)
	require.NoError(t, secondSource.Ack())

	secondAgentInfo, err := agentStream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), secondAgentInfo.State.Msgs,
		"current replay publishes a second task after the duplicate window")
	secondRaw, err := agentStream.GetMsg(ctx, secondAgentInfo.State.LastSeq)
	require.NoError(t, err)
	secondTask := restartIdentityTask(t, decoder, secondRaw)
	require.Equal(t, firstTask.Prompt, secondTask.Prompt,
		"both tasks came from the same source content")

	t.Logf("source stream sequence %d delivered twice; first task_id=%s loop_id=%s; second task_id=%s loop_id=%s",
		firstMeta.Sequence.Stream, firstTask.TaskID, firstTask.LoopID, secondTask.TaskID, secondTask.LoopID)
	require.Equal(t, firstTask.LoopID, secondTask.LoopID,
		"one retry-equivalent UserMessage must not become two logical loops")
	require.Equal(t, firstTask.TaskID, secondTask.TaskID,
		"one retry-equivalent UserMessage must not become two logical tasks")
	require.Equal(t, firstTask.SourceMessageID, secondTask.SourceMessageID)
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
func TestIntegrationUserMessageTaskMappingConflictQuarantines(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Component, *agentic.TaskMessage, *agentic.UserMessage)
		mutateData func([]byte, *agentic.TaskMessage) []byte
	}{
		{
			name: "same TaskID names a different source",
			mutate: func(_ *Component, task *agentic.TaskMessage, _ *agentic.UserMessage) {
				task.SourceMessageID = "another-source-message"
			},
		},
		{
			name: "same TaskID names a different LoopID",
			mutate: func(_ *Component, _ *agentic.TaskMessage, msg *agentic.UserMessage) {
				msg.ReplyTo = uuid.NewString()
			},
		},
		{
			name:   "retained TaskMessage is malformed",
			mutate: func(_ *Component, _ *agentic.TaskMessage, _ *agentic.UserMessage) {},
			mutateData: func(data []byte, task *agentic.TaskMessage) []byte {
				return bytes.Replace(data, []byte(task.LoopID), []byte("not-a-loop-id"), 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
			_, err := tc.Client.EnsureStream(ctx, jetstream.StreamConfig{
				Name: restartIdentityAgentStream, Subjects: []string{"agent.task.>"},
				Storage: jetstream.MemoryStorage, MaxAge: time.Minute, MaxBytes: 1 << 20,
				Discard: jetstream.DiscardOld,
			})
			require.NoError(t, err)

			dispatch := newRestartIdentityDispatch(t, tc.Client)
			userMessage := &agentic.UserMessage{
				MessageID: "conflicting-source", ChannelType: "http", ChannelID: "conflict-session",
				UserID: "conflict-user", Content: "perform exactly one task", Timestamp: time.Now().UTC(),
			}
			taskID := stableDispatchTaskID(*userMessage)
			retained := dispatch.buildTaskMessage(ctx, *userMessage, uuid.NewString(), taskID)
			tt.mutate(dispatch, &retained, userMessage)
			retainedData, err := json.Marshal(message.NewBaseMessage(retained.Schema(), &retained, "conflict-test"))
			require.NoError(t, err)
			if tt.mutateData != nil {
				retainedData = tt.mutateData(retainedData, &retained)
			}
			require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task."+taskID, retainedData))

			sourceData, err := json.Marshal(message.NewBaseMessage(userMessage.Schema(), userMessage, "conflict-test"))
			require.NoError(t, err)
			decision, cause := dispatch.handleUserMessage(ctx, sourceData)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.ErrorContains(t, cause, "task mapping conflict")

			stream, err := tc.Client.GetStream(ctx, restartIdentityAgentStream)
			require.NoError(t, err)
			info, err := stream.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, uint64(1), info.State.Msgs, "conflict must not publish or overwrite either mapping")
		})
	}
}
