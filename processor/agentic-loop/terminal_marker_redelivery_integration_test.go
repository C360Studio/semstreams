//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// terminalMarkerFailingBucket delegates every real KV operation except the first
// terminal bare-record Put. COMPLETE_ and ordinary nonterminal writes still commit.
type terminalMarkerFailingBucket struct {
	jetstream.KeyValue
	loopID string
	failed atomic.Bool
}

func (b *terminalMarkerFailingBucket) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	if key == b.loopID {
		var entity agentic.LoopEntity
		if err := json.Unmarshal(value, &entity); err != nil {
			return 0, err
		}
		if entity.State.IsTerminal() && b.failed.CompareAndSwap(false, true) {
			return 0, errors.New("injected final terminal marker Put failure")
		}
	}
	return b.KeyValue.Put(ctx, key, value)
}

// terminalMarkerDelivery observes settlement without replacing any server method.
// Callback completion transfers ownership of these counters to the test goroutine.
type terminalMarkerDelivery struct {
	jetstream.Msg
	acks, naks, terms int
	beforeAck         func() error
	ackCheckErr       error
}

func (m *terminalMarkerDelivery) Ack() error {
	m.acks++
	if m.beforeAck != nil {
		m.ackCheckErr = m.beforeAck()
	}
	return m.Msg.Ack()
}

func (m *terminalMarkerDelivery) NakWithDelay(delay time.Duration) error {
	m.naks++
	return m.Msg.NakWithDelay(delay)
}

func (m *terminalMarkerDelivery) Term() error {
	m.terms++
	return m.Msg.Term()
}

func newTerminalMarkerProcess(t *testing.T, client *natsclient.Client, suffix string) *Component {
	t.Helper()
	discoverable, err := NewComponent([]byte(`{"consumer_name_suffix":"`+suffix+`"}`), component.Dependencies{
		NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	// Graph evidence is explicitly nonblocking under the cited contract.
	// All settlement-required KV and terminal stream effects remain real.
	c.graphWriter = nil
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.Stop(stopCtx))
	})
	return c
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationTerminalMarkerFailureRedeliversAfterComponentReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	decoder := payloadbuiltins.NewTestDecoder(t)
	const suffix = "terminal-marker-replacement"
	const responseConsumerName = "agentic-loop-agent-response-all-" + suffix
	loopID := uuid.NewString()
	first := newTerminalMarkerProcess(t, tc.Client, suffix)
	var failingBucket *terminalMarkerFailingBucket
	first.initializeKVBucketsInput = func(initCtx context.Context) error {
		if err := first.initializeKVBuckets(initCtx); err != nil {
			return err
		}
		failingBucket = &terminalMarkerFailingBucket{KeyValue: first.loopsBucket, loopID: loopID}
		first.loopsBucket = failingBucket
		return nil
	}
	taskSettled := make(chan struct{}, 1)
	firstSettled := make(chan *terminalMarkerDelivery, 1)
	first.consumeStream = func(
		setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				if owner.Port == "agent.response" {
					observed := &terminalMarkerDelivery{Msg: msg}
					handler(msgCtx, observed)
					firstSettled <- observed
					return
				}
				handler(msgCtx, msg)
				if owner.Port == "agent.task" {
					taskSettled <- struct{}{}
				}
			})
	}
	require.NoError(t, first.Start(ctx))
	task := &agentic.TaskMessage{
		LoopID: loopID, TaskID: "terminal-marker-task", Role: "general", Model: "test-model", Prompt: "finish this task",
	}
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.test", settlementEnvelope(t, task)))
	select {
	case <-taskSettled:
	case <-time.After(5 * time.Second):
		t.Fatal("real task intake did not settle")
	}
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	retainedRequest, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
	require.NoError(t, err)
	decoded, err := decoder.Decode(retainedRequest.Data)
	require.NoError(t, err)
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	require.True(t, ok)
	require.Equal(t, loopID, request.LoopID)
	require.NotEmpty(t, request.RequestID)
	before, err := failingBucket.Get(ctx, loopID)
	require.NoError(t, err)
	var admitted agentic.LoopEntity
	require.NoError(t, json.Unmarshal(before.Value(), &admitted))
	require.False(t, admitted.State.IsTerminal())
	require.Equal(t, task.TaskID, admitted.TaskID)

	response := &agentic.AgentResponse{
		RequestID: request.RequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "durably completed"},
	}
	responseData := settlementEnvelope(t, response)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.response."+request.RequestID, responseData))
	var failed *terminalMarkerDelivery
	select {
	case failed = <-firstSettled:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal response did not return from its real heartbeat owner")
	}
	require.True(t, failingBucket.failed.Load(), "terminal final Put was never attempted")
	require.Equal(t, 1, failed.naks, "required final-marker failure must Retry")
	require.Zero(t, failed.acks+failed.terms, "required final-marker failure settled the source")
	firstMetadata, err := failed.Metadata()
	require.NoError(t, err)
	require.Equal(t, responseConsumerName, firstMetadata.Consumer)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)

	completionEntry, err := failingBucket.Get(ctx, "COMPLETE_"+loopID)
	require.NoError(t, err, "COMPLETE_ must have committed before the failed final Put")
	var completion agentic.LoopCompletedEvent
	require.NoError(t, json.Unmarshal(completionEntry.Value(), &completion))
	require.Equal(t, response.Message.Content, completion.Result)
	terminalPublication, err := stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
	require.NoError(t, err, "terminal publication must have received PubAck before the failed final Put")
	decoded, err = decoder.Decode(terminalPublication.Data)
	require.NoError(t, err)
	terminal, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
	require.True(t, ok)
	require.Equal(t, task.TaskID, terminal.TaskID)
	require.Equal(t, completion.Result, terminal.Result)
	afterFailure, err := failingBucket.Get(ctx, loopID)
	require.NoError(t, err)
	require.Equal(t, before.Revision(), afterFailure.Revision(), "failed terminal Put advanced the bare marker")
	require.Equal(t, before.Value(), afterFailure.Value(), "failed terminal Put replaced the nonterminal record")

	// Stop joins the actual subscriptions and sweeper before a fresh constructor
	// can recover. The fixed production retry delay is retained, not shortened.
	require.NoError(t, first.Stop(ctx))
	consumer, err := stream.Consumer(ctx, responseConsumerName)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, firstMetadata.Sequence.Stream)

	replacement := newTerminalMarkerProcess(t, tc.Client, suffix)
	settleTerminalMarkerReplacement(t, replacement, stream, retainedRequest, failed, task)
}

func settleTerminalMarkerReplacement(
	t *testing.T, replacement *Component, stream jetstream.Stream, retainedRequest *jetstream.RawStreamMsg,
	failed *terminalMarkerDelivery, task *agentic.TaskMessage,
) {
	t.Helper()
	ctx := t.Context()
	loopID := task.LoopID
	firstMetadata, err := failed.Metadata()
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, firstMetadata.Consumer)
	require.NoError(t, err)
	_, err = replacement.handler.GetLoop(loopID)
	require.Error(t, err, "replacement must begin without a process-local loop")
	replacementSettled := make(chan *terminalMarkerDelivery, 1)
	replacement.consumeStream = func(
		setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		return replacement.natsClient.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				if owner.Port != "agent.response" {
					handler(msgCtx, msg)
					return
				}
				observed := &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
					entity, found, readErr := replacement.readLoopEntity(msgCtx, loopID)
					if readErr != nil {
						return readErr
					}
					if !found || entity.State != agentic.LoopStateComplete || entity.TaskID != task.TaskID {
						return fmt.Errorf("source ACK preceded the exact task's terminal marker: %+v", entity)
					}
					return nil
				}}
				handler(msgCtx, observed)
				replacementSettled <- observed
			})
	}
	require.NoError(t, replacement.Start(ctx))
	var redelivered *terminalMarkerDelivery
	select {
	case redelivered = <-replacementSettled:
	case <-time.After(40 * time.Second): // Production delayed Retry is 30 seconds.
		t.Fatal("replacement did not settle the actual delayed source redelivery")
	}
	require.NoError(t, redelivered.ackCheckErr)
	require.Equal(t, 1, redelivered.acks)
	require.Zero(t, redelivered.naks+redelivered.terms)
	metadata, err := redelivered.Metadata()
	require.NoError(t, err)
	require.Equal(t, firstMetadata.Stream, metadata.Stream)
	require.Equal(t, firstMetadata.Consumer, metadata.Consumer)
	require.Equal(t, firstMetadata.Sequence.Stream, metadata.Sequence.Stream)
	require.Greater(t, metadata.NumDelivered, firstMetadata.NumDelivered)
	require.Equal(t, failed.Data(), redelivered.Data())

	finalMarker, err := replacement.loopsBucket.Get(ctx, loopID)
	require.NoError(t, err)
	finalCompletion, err := replacement.loopsBucket.Get(ctx, "COMPLETE_"+loopID)
	require.NoError(t, err)
	require.Greater(t, finalMarker.Revision(), finalCompletion.Revision(), "terminal bare Put was not the final KV marker")
	currentRequest, err := stream.GetLastMsgForSubject(ctx, retainedRequest.Subject)
	require.NoError(t, err)
	require.Equal(t, retainedRequest.Sequence, currentRequest.Sequence, "recovery minted another provider request")
	require.Equal(t, retainedRequest.Data, currentRequest.Data)
	require.Eventually(t, func() bool {
		settled, infoErr := consumer.Info(ctx)
		return infoErr == nil && settled.NumAckPending == 0 && settled.AckFloor.Stream >= metadata.Sequence.Stream
	}, 5*time.Second, 10*time.Millisecond, "server did not observe source ACK after the final marker")
}
