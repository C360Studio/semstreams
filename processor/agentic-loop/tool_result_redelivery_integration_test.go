//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// toolResultCheckpointBucket cancels the first process only after its ordinary
// result checkpoint commits. The ensuing publication uses the canceled Start
// context and cannot settle the source. All KV operations remain real.
type toolResultCheckpointBucket struct {
	jetstream.KeyValue
	loopID string
	cancel context.CancelFunc
}

func (b *toolResultCheckpointBucket) Put(ctx context.Context, key string, data []byte) (uint64, error) {
	revision, err := b.KeyValue.Put(ctx, key, data)
	if err == nil && key == b.loopID {
		var entity agentic.LoopEntity
		if json.Unmarshal(data, &entity) == nil && len(entity.PendingToolResults) > 0 {
			b.cancel()
		}
	}
	return revision, err
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Tool execution has stable framework correlation
func TestIntegrationColdToolResultRedeliveryUnblocksLaterApproval(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	decoder := payloadbuiltins.NewTestDecoder(t)
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	const suffix = "cold-tool-result"
	type observation struct {
		port string
		msg  *terminalMarkerDelivery
	}
	settled := make(chan observation, 16)
	first := newTerminalMarkerProcess(t, tc.Client, suffix)
	observe := func(c *Component) {
		c.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext,
			cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg),
		) (jetstream.ConsumeContext, error) {
			return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
				func(msgCtx context.Context, msg jetstream.Msg) {
					observed := &terminalMarkerDelivery{Msg: msg}
					if owner.Port == "tool.result" {
						observed.beforeAck = func() error {
							decoded, err := decoder.Decode(msg.Data())
							if err != nil {
								return err
							}
							result := decoded.Payload().(*agentic.ToolResult)
							entity, found, err := c.readLoopEntity(msgCtx, result.LoopID)
							if err != nil || !found {
								return fmt.Errorf("source ACK preceded result checkpoint: found=%v err=%v", found, err)
							}
							if entity.PendingToolResults[result.ExecutionID].Content != result.Content ||
								entity.PendingToolResults[result.ExecutionID].Error != result.Error {
								return fmt.Errorf("source ACK preceded matching result checkpoint")
							}
							if agentic.IsApprovalRequired(result.Error) {
								if entity.State != agentic.LoopStateAwaitingApproval || entity.PendingApproval == nil || entity.PendingApproval.ExecutionID != result.ExecutionID {
									return fmt.Errorf("source ACK preceded durable approval wait")
								}
								_, err = stream.GetLastMsgForSubject(msgCtx, "agent.approval_pending."+result.LoopID)
								return err
							}
							request, err := stream.GetLastMsgForSubject(msgCtx, "agent.request."+result.LoopID)
							if err != nil {
								return err
							}
							decoded, err = decoder.Decode(request.Data)
							if err != nil {
								return err
							}
							next := decoded.Payload().(*agentic.AgentRequest)
							if next.RequestID == result.RequestID {
								return fmt.Errorf("source ACK preceded next request PubAck")
							}
							for _, message := range next.Messages {
								if message.Role == "tool" && message.ToolCallID == result.CallID && message.Content == result.Content && message.Name == result.Name {
									return nil
								}
							}
							return fmt.Errorf("source ACK preceded matching result in next request")
						}
					}
					handler(msgCtx, observed)
					settled <- observation{port: owner.Port, msg: observed}
				})
		}
	}
	wait := func(port string, budget time.Duration) *terminalMarkerDelivery {
		t.Helper()
		timer := time.NewTimer(budget)
		defer timer.Stop()
		for {
			select {
			case got := <-settled:
				if got.port == port {
					return got.msg
				}
			case <-timer.C:
				t.Fatalf("started owner %s did not finish within %s", port, budget)
			}
		}
	}
	startCall := func(loopID, name string) agentic.ToolCall {
		t.Helper()
		task := &agentic.TaskMessage{LoopID: loopID, TaskID: uuid.NewString(), Role: "general", Model: "test-model", Prompt: "perform task"}
		require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.test", settlementEnvelope(t, task)))
		require.Equal(t, 1, wait("agent.task", 5*time.Second).acks)
		raw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
		require.NoError(t, err)
		decoded, err := decoder.Decode(raw.Data)
		require.NoError(t, err)
		request := decoded.Payload().(*agentic.AgentRequest)
		response := &agentic.AgentResponse{RequestID: request.RequestID, Status: agentic.StatusToolCall,
			Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "same-provider-id", Name: name, Arguments: map[string]any{"query": name}}}}}
		require.NoError(t, tc.Client.PublishToStream(ctx, "agent.response."+request.RequestID, settlementEnvelope(t, response)))
		require.Equal(t, 1, wait("agent.response", 5*time.Second).acks)
		raw, err = stream.GetLastMsgForSubject(ctx, "tool.execute."+name)
		require.NoError(t, err)
		decoded, err = decoder.Decode(raw.Data)
		require.NoError(t, err)
		return *decoded.Payload().(*agentic.ToolCall)
	}
	observe(first)
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	loopID := uuid.NewString()
	first.initializeKVBucketsInput = func(initCtx context.Context) error {
		if err := first.initializeKVBuckets(initCtx); err != nil {
			return err
		}
		first.loopsBucket = &toolResultCheckpointBucket{KeyValue: first.loopsBucket, loopID: loopID, cancel: cancelFirst}
		return nil
	}
	require.NoError(t, first.Start(firstCtx))
	call := startCall(loopID, "search")
	result := &agentic.ToolResult{LoopID: loopID, RequestID: call.RequestID, ExecutionID: call.ExecutionID,
		CallOrdinal: call.CallOrdinal, CallID: call.ID, Name: call.Name, TraceID: call.TraceID, Content: "retained answer"}
	resultData := settlementEnvelope(t, result)
	require.NoError(t, tc.Client.PublishToStream(ctx, "tool.result."+call.ExecutionID, resultData))
	failed := wait("tool.result", 5*time.Second)
	require.Zero(t, failed.acks+failed.terms)
	require.Equal(t, 1, failed.naks)
	checkpoint, err := first.loopsBucket.Get(ctx, loopID)
	require.NoError(t, err)
	var interrupted agentic.LoopEntity
	require.NoError(t, json.Unmarshal(checkpoint.Value(), &interrupted))
	require.Equal(t, *result, interrupted.PendingToolResults[call.ExecutionID])
	require.Equal(t, 1, interrupted.Iterations)
	priorRequest, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
	require.NoError(t, err)
	prior, err := decoder.Decode(priorRequest.Data)
	require.NoError(t, err)
	require.Equal(t, call.RequestID, prior.Payload().(*agentic.AgentRequest).RequestID, "interrupted next request must not have published")
	stopCtx, cancelStop := context.WithTimeout(ctx, 5*time.Second)
	defer cancelStop()
	require.NoError(t, first.Stop(stopCtx), "Stop must join the canceled real delivery")
	before, err := failed.Metadata()
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, before.Consumer)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.Config.MaxAckPending)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, before.Sequence.Stream)

	replacement := newTerminalMarkerProcess(t, tc.Client, suffix)
	_, err = replacement.handler.GetLoop(loopID)
	require.Error(t, err, "replacement must begin without loop memory")
	observe(replacement)
	require.NoError(t, replacement.Start(ctx))
	// Keep the production 30-second retry policy; this is server redelivery,
	// never a manually reinvoked callback or republished source.
	redelivered := wait("tool.result", 45*time.Second)
	after, err := redelivered.Metadata()
	require.NoError(t, err)
	require.Equal(t, before.Sequence.Stream, after.Sequence.Stream)
	require.Greater(t, after.NumDelivered, before.NumDelivered)
	require.Equal(t, resultData, redelivered.Data())
	require.Equal(t, 1, redelivered.acks, "cold result must settle before the next MaxAckPending=1 delivery")
	require.NoError(t, redelivered.ackCheckErr)
	require.Zero(t, redelivered.naks+redelivered.terms)
	require.Eventually(t, func() bool {
		info, err := consumer.Info(ctx)
		return err == nil && info.NumAckPending == 0 && info.AckFloor.Stream >= after.Sequence.Stream
	}, 5*time.Second, 10*time.Millisecond)
	raw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
	require.NoError(t, err)
	decoded, err := decoder.Decode(raw.Data)
	require.NoError(t, err)
	next := decoded.Payload().(*agentic.AgentRequest)
	require.NotEqual(t, call.RequestID, next.RequestID)
	require.Contains(t, next.Messages, agentic.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: result.Content})
	recovered, err := replacement.loopsBucket.Get(ctx, loopID)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(recovered.Value(), &interrupted))
	require.Equal(t, 1, interrupted.Iterations, "publication replay must spend the iteration once")

	approvalLoopID := uuid.NewString()
	approvalCall := startCall(approvalLoopID, "approval_tool")
	approval := &agentic.ToolResult{LoopID: approvalLoopID, RequestID: approvalCall.RequestID, ExecutionID: approvalCall.ExecutionID,
		CallOrdinal: approvalCall.CallOrdinal, CallID: approvalCall.ID, Name: approvalCall.Name, TraceID: approvalCall.TraceID, Error: "approval_required: owner approval"}
	require.NoError(t, tc.Client.PublishToStream(ctx, "tool.result."+approval.ExecutionID, settlementEnvelope(t, approval)))
	approvalDelivery := wait("tool.result", 5*time.Second)
	require.Equal(t, 1, approvalDelivery.acks)
	require.NoError(t, approvalDelivery.ackCheckErr)
	require.Zero(t, approvalDelivery.naks+approvalDelivery.terms)
	entry, err := replacement.loopsBucket.Get(ctx, approvalLoopID)
	require.NoError(t, err)
	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))
	require.Equal(t, agentic.LoopStateAwaitingApproval, entity.State)
	require.NotNil(t, entity.PendingApproval)
	require.Equal(t, approval.ExecutionID, entity.PendingApproval.ExecutionID)
	require.Equal(t, *approval, entity.PendingToolResults[approval.ExecutionID])
	_, err = stream.GetLastMsgForSubject(ctx, "agent.approval_pending."+approvalLoopID)
	require.NoError(t, err, "later approval must publish durably through the same unblocked owner")
}
