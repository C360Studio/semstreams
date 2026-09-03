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
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Delivery work joins before settlement
// scenario: Terminal approval rejection reaches a bounded graph write
func TestIntegrationApprovalRejectionWaitsForGraphWriteJoinBeforeAck(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	graphStarted := make(chan struct{})
	graphRelease := make(chan struct{})
	graphJoined := make(chan struct{})
	_, err := testClient.Client.SubscribeForRequests(ctx, "graph.mutation.triple.append", func(_ context.Context, data []byte) ([]byte, error) {
		var request gtypes.AppendTriplesRequest
		if decodeErr := json.Unmarshal(data, &request); decodeErr != nil {
			return nil, decodeErr
		}
		close(graphStarted)
		<-graphRelease
		results := make([]gtypes.AppendSubjectResult, 0, len(request.Triples))
		seen := make(map[string]struct{})
		for _, triple := range request.Triples {
			if _, exists := seen[triple.Subject]; exists {
				continue
			}
			seen[triple.Subject] = struct{}{}
			results = append(results, gtypes.AppendSubjectResult{
				EntityID: triple.Subject, Outcome: gtypes.MutationApplied, KVRevision: 1,
			})
		}
		encoded, encodeErr := json.Marshal(gtypes.AppendTriplesResponse{Results: results})
		close(graphJoined)
		return encoded, encodeErr
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
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &loopPolicyHandle{closed: make(chan struct{})}, nil
	}
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

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
	msg := &loopDeliveryOwnerMsg{data: data}
	callbackReturned := make(chan struct{})
	go func() {
		callbacks["agent.approval_response"](ctx, msg)
		close(callbackReturned)
	}()

	select {
	case <-graphStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal rejection did not reach its production graph-write dependency")
	}
	require.Zero(t, msg.settlement.Load(), "source settled while graph-write work was live")
	select {
	case <-callbackReturned:
		t.Fatal("production callback returned while graph-write work was live")
	default:
	}
	close(graphRelease)
	select {
	case <-graphJoined:
	case <-time.After(2 * time.Second):
		t.Fatal("graph-write dependency did not join after release")
	}
	select {
	case <-callbackReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("production callback did not return after graph-write join")
	}
	require.Equal(t, int32(1), msg.acks.Load(), "terminal rejection must ACK only after graph-write join")
	require.Zero(t, msg.naks.Load())
	require.Zero(t, msg.terms.Load())
	_, err = c.handler.GetLoop(loopID)
	require.ErrorContains(t, err, "not found", "joined terminal persistence must release the settled loop")

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationLoopSignalAndApprovalProductionBindingsCommitConsequences(t *testing.T) {
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
	c.graphWriter = nil // cancellation graph projection is optional; stream consequence is required
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if owner.Port == "agent.signal" || owner.Port == "agent.approval_response" {
			require.Equal(t, loopFastDeliveryAckWait, cfg.AckWait, owner.Port)
			require.Equal(t, loopFastDeliveryAckWait, cfg.MessageTimeout, owner.Port)
		}
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
	signalMsg := &loopDeliveryOwnerMsg{data: signalData}
	callbacks["agent.signal"](ctx, signalMsg)
	require.Equal(t, int32(1), signalMsg.acks.Load(), "durable cancel consequence must precede ACK")
	require.Zero(t, signalMsg.naks.Load())
	require.Zero(t, signalMsg.terms.Load())
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
	responseMsg := &loopDeliveryOwnerMsg{data: responseData}
	callbacks["agent.approval_response"](ctx, responseMsg)
	require.Equal(t, int32(1), responseMsg.acks.Load(), "approval consequence must precede ACK")
	require.Zero(t, responseMsg.naks.Load())
	require.Zero(t, responseMsg.terms.Load())
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

type loopFastDeliveryObservation struct {
	attempt  uint64
	decision natsclient.DeliveryDecision
	err      error
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationLoopControlFastDeliveryOwnersCancelJoinAndRetry(t *testing.T) {
	t.Parallel()
	exerciseLoopFastDeliveryGroup(t, "LOOP_CONTROL_FAST_OWNER", []loopFastDeliveryLane{
		{port: "agent.approval_response", subject: "agent.approval_response.fast-owner"},
	})
}

type loopFastDeliveryLane struct {
	port    string
	subject string
}

func exerciseLoopFastDeliveryGroup(t *testing.T, streamName string, lanes []loopFastDeliveryLane) {
	t.Helper()
	subjects := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		subjects = append(subjects, lane.subject)
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: streamName, Subjects: subjects},
	))
	for index, lane := range lanes {
		t.Run(lane.port, func(t *testing.T) {
			exerciseLoopFastDeliveryBoundary(t, testClient, streamName, lane.port, lane.subject, index)
		})
	}
}

func exerciseLoopFastDeliveryBoundary(
	t *testing.T,
	testClient *natsclient.TestClient,
	streamName string,
	port string,
	subject string,
	index int,
) {
	t.Helper()
	ctx := t.Context()
	consumerName := fmt.Sprintf("loop-fast-owner-%s-%d", streamName, index)
	results := make(chan loopFastDeliveryObservation, 2)
	started := make(chan time.Time, 2)
	joined := make(chan error, 1)
	var invocations atomic.Int32
	var active atomic.Int32
	var overlap atomic.Bool

	cfg := natsclient.StreamConsumerConfig{
		StreamName: streamName, ConsumerName: consumerName, FilterSubject: subject,
		DeliverPolicy: "all", AckPolicy: "explicit", AckWait: loopFastDeliveryAckWait,
		MaxDeliver: 3, MaxAckPending: 10, MessageTimeout: loopFastDeliveryAckWait,
	}
	handle, err := testClient.Client.ConsumeStreamWithConfig(
		ctx,
		natsclient.PortConsumerContext{Component: "agentic-loop", Port: port},
		cfg,
		func(msgCtx context.Context, msg jetstream.Msg) {
			metadata, metadataErr := msg.Metadata()
			attempt := uint64(0)
			if metadataErr == nil && metadata != nil {
				attempt = metadata.NumDelivered
			}
			decision, deliveryErr := consumeLoopFastDelivery(
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
			results <- loopFastDeliveryObservation{
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
	require.Equal(t, loopFastDeliveryAckWait, info.Config.AckWait)

	require.NoError(t, testClient.Client.PublishToStream(ctx, subject, []byte(port)))
	firstStarted := <-started
	select {
	case joinErr := <-joined:
		require.ErrorIs(t, joinErr, context.DeadlineExceeded)
	case <-time.After(loopFastDeliveryAckWait + 5*time.Second):
		t.Fatal("cooperative delivery work was not canceled and joined before AckWait")
	}
	// This is the one deliberate wall-clock assertion: it observes the real
	// 25s owner deadline while allowing scheduler jitter inside the 5s margin.
	elapsed := time.Since(firstStarted)
	require.GreaterOrEqual(t, elapsed, loopFastDeliveryWorkBudget-time.Second)
	require.Less(t, elapsed, loopFastDeliveryAckWait)

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
