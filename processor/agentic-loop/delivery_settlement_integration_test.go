//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Delivery work joins before settlement
func TestIntegrationApprovalRejectionJoinsCancelledGraphRequestBeforeQuarantine(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	setupCtx, stop := context.WithCancel(t.Context())
	defer stop()
	graphStarted := make(chan struct{})
	graphRelease := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(graphRelease)
		}
	})
	_, err := testClient.Client.SubscribeForRequests(setupCtx, "graph.mutation.triple.append", func(_ context.Context, data []byte) ([]byte, error) {
		var request gtypes.AppendTriplesRequest
		if decodeErr := json.Unmarshal(data, &request); decodeErr != nil {
			return nil, decodeErr
		}
		close(graphStarted)
		<-graphRelease
		return json.Marshal(gtypes.AppendTriplesResponse{})
	})
	require.NoError(t, err)

	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: testClient.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		Platform: component.PlatformMeta{Org: "acme", Platform: "ops"},
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	require.NoError(t, c.setupSubscriptions(setupCtx, setupCtx))

	loopID := setUpAwaitingLoop(t, c.handler, time.Minute, 0)
	entity, err := c.handler.GetLoop(loopID)
	require.NoError(t, err)
	entity.Iterations = entity.MaxIterations
	require.NoError(t, c.handler.UpdateLoop(entity))
	response := &agentic.ApprovalResponse{
		LoopID: loopID, CallID: "call-gated", Decision: agentic.ApprovalDecisionReject,
		Reason: "policy", DecidedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	msg := &loopSettlementMsg{data: data}
	deliveryCtx, cancelDelivery := context.WithCancel(setupCtx)
	callbackReturned := make(chan struct{})
	go func() {
		callbacks["agent.approval_response"](deliveryCtx, msg)
		close(callbackReturned)
	}()

	select {
	case <-graphStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal rejection did not reach its production graph request")
	}
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(), "source settled while graph request was live")
	select {
	case <-callbackReturned:
		t.Fatal("production callback returned while graph request was live")
	default:
	}
	cancelDelivery()
	select {
	case <-callbackReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("production graph request did not honor delivery cancellation and join")
	}
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(), "unknown graph effect must not settle the source")
	require.Eventually(t, func() bool { return handles["agent.approval_response"].drains.Load() == 1 }, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "agent.approval_response" {
			require.Zero(t, handle.drains.Load(), "graph failure drained unrelated owner %s", port)
		}
	}
	close(graphRelease)
	released = true

	stop()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationLoopSignalAndApprovalCallbacksCommitBeforeAck(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan string, 1)
	toolCalls := make(chan string, 1)
	completeSub, err := testClient.Client.Subscribe(ctx, "agent.complete.>", func(_ context.Context, msg *nats.Msg) {
		completed <- msg.Subject
	})
	require.NoError(t, err)
	toolSub, err := testClient.Client.Subscribe(ctx, "tool.execute.>", func(_ context.Context, msg *nats.Msg) {
		toolCalls <- msg.Subject
	})
	require.NoError(t, err)

	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: testClient.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.graphWriter = nil
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &loopPolicyHandle{closed: make(chan struct{})}, nil
	}
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	cancelLoopID, err := c.handler.loopManager.CreateLoop("task-cancel", "general", "m", 3)
	require.NoError(t, err)
	signal := &agentic.UserSignal{
		SignalID: "signal-1", Type: agentic.SignalCancel, LoopID: cancelLoopID,
		UserID: "operator", Timestamp: time.Now().UTC(),
	}
	signalData, err := json.Marshal(message.NewBaseMessage(signal.Schema(), signal, "test"))
	require.NoError(t, err)
	signalMsg := &loopSettlementMsg{data: signalData}
	callbacks["agent.signal"](ctx, signalMsg)
	require.Equal(t, int32(1), signalMsg.acks.Load())
	require.Zero(t, signalMsg.naks.Load()+signalMsg.terms.Load())
	select {
	case subject := <-completed:
		require.Equal(t, "agent.complete."+cancelLoopID, subject)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not publish its terminal consequence")
	}

	approvalLoopID := setUpAwaitingLoop(t, c.handler, time.Minute, 0)
	response := &agentic.ApprovalResponse{
		LoopID: approvalLoopID, CallID: "call-gated", Decision: agentic.ApprovalDecisionApprove,
		ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
	}
	responseData, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	responseMsg := &loopSettlementMsg{data: responseData}
	callbacks["agent.approval_response"](ctx, responseMsg)
	require.Equal(t, int32(1), responseMsg.acks.Load())
	require.Zero(t, responseMsg.naks.Load()+responseMsg.terms.Load())
	entity, err := c.handler.GetLoop(approvalLoopID)
	require.NoError(t, err)
	require.NotEqual(t, agentic.LoopStateAwaitingApproval, entity.State)
	require.Nil(t, entity.PendingApproval)
	select {
	case subject := <-toolCalls:
		require.Equal(t, "tool.execute.delete_rule", subject)
	case <-time.After(2 * time.Second):
		t.Fatal("approved response did not publish its tool consequence")
	}

	require.NoError(t, completeSub.Drain(t.Context()))
	require.NoError(t, toolSub.Drain(t.Context()))
	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}
