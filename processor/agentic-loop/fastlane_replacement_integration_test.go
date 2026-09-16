//go:build integration

package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// fastLaneObservedConsumer counts owner-requested drains while delegating every
// lifecycle operation to the actual native consumer.
type fastLaneObservedConsumer struct {
	jetstream.ConsumeContext
	drains atomic.Int32
}

func (c *fastLaneObservedConsumer) Drain() {
	c.drains.Add(1)
	c.ConsumeContext.Drain()
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / Per-loop in-process state is released at terminal, through the one release point
func TestIntegrationCancelFinalMarkerRetryAfterComponentReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	const suffix = "cancel-final-marker-replacement"
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
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	taskSettled := make(chan struct{}, 1)
	firstSettled := make(chan *approvalReplacementDelivery, 1)
	handles := make(map[string]*fastLaneObservedConsumer)
	var firstFatal error
	first.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			if owner.Port == "agent.signal" {
				if firstCtx.Err() != nil {
					return
				}
				observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg}}
				handler(msgCtx, observed)
				first.mu.RLock()
				firstFatal = first.deliveryFatalErr
				first.mu.RUnlock()
				// Freeze the completed attempt for replacement, after its real NAK.
				// This cancellation is test-directed, not a quarantine reaction.
				cancelFirst()
				firstSettled <- observed
				return
			}
			handler(msgCtx, msg)
			if owner.Port == "agent.task" {
				taskSettled <- struct{}{}
			}
		})
		if err != nil {
			return nil, err
		}
		observed := &fastLaneObservedConsumer{ConsumeContext: handle}
		handles[owner.Port] = observed
		return observed, nil
	}
	require.NoError(t, first.Start(firstCtx))
	task := &agentic.TaskMessage{LoopID: loopID, TaskID: "cancel-marker-task", Role: "general", Model: "test-model", Prompt: "await cancellation"}
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.test", settlementEnvelope(t, task)))
	select {
	case <-taskSettled:
	case <-time.After(5 * time.Second):
		t.Fatal("real task intake did not settle")
	}
	before, err := failingBucket.Get(ctx, loopID)
	require.NoError(t, err)
	var admitted agentic.LoopEntity
	require.NoError(t, json.Unmarshal(before.Value(), &admitted))
	require.False(t, admitted.State.IsTerminal())
	require.NotEmpty(t, perLoopMapCount(first.handler.loopManager, loopID))
	signal := &agentic.UserSignal{SignalID: "cancel-final-marker", Type: agentic.SignalCancel, LoopID: loopID, UserID: "operator"}
	signalData := settlementEnvelope(t, signal)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.signal."+loopID, signalData))
	var failed *approvalReplacementDelivery
	select {
	case failed = <-firstSettled:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not return from its production callback")
	}
	require.True(t, failingBucket.failed.Load())
	require.Equal(t, 1, failed.naks, "transient final-marker failure must Retry, not Quarantine")
	require.Zero(t, failed.acks+failed.terms)
	require.Nil(t, firstFatal, "transient final-marker failure stopped its owner")
	for port, handle := range handles {
		require.Zero(t, handle.drains.Load(), "failure drained %s before controlled Stop", port)
	}
	require.Empty(t, perLoopMapCount(first.handler.loopManager, loopID))
	firstMetadata, err := failed.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)
	selectedEntry, err := failingBucket.Get(ctx, "COMPLETE_"+loopID)
	require.NoError(t, err)
	var selected agentic.LoopCancelledEvent
	require.NoError(t, json.Unmarshal(selectedEntry.Value(), &selected))
	require.Equal(t, signal.UserID, selected.CancelledBy)
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	publication, err := stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
	require.NoError(t, err, "terminal PubAck must precede the failed final marker")
	decoded, err := first.decoder.Decode(publication.Data)
	require.NoError(t, err)
	require.Equal(t, &selected, decoded.Payload())
	afterFailure, err := failingBucket.Get(ctx, loopID)
	require.NoError(t, err)
	require.Equal(t, before.Revision(), afterFailure.Revision())
	require.Equal(t, before.Value(), afterFailure.Value())
	require.NoError(t, first.Stop(ctx))
	consumer, err := stream.Consumer(ctx, firstMetadata.Consumer)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, firstMetadata.Sequence.Stream)

	t.Run("replacement", func(t *testing.T) {
		replacement := newTerminalMarkerProcess(t, tc.Client, suffix)
		require.Empty(t, perLoopMapCount(replacement.handler.loopManager, loopID))
		settled := make(chan *approvalReplacementDelivery, 1)
		replacement.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
				if owner.Port != "agent.signal" {
					handler(msgCtx, msg)
					return
				}
				observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
					marker, revision, readErr := replacement.readLoopEntityRevision(msgCtx, loopID)
					if readErr != nil {
						return readErr
					}
					if marker.State != agentic.LoopStateCancelled || marker.TaskID != task.TaskID || marker.CancelledBy != selected.CancelledBy || !marker.CancelledAt.Equal(selected.CancelledAt) || revision <= selectedEntry.Revision() {
						return fmt.Errorf("ACK preceded the selected cancellation's final marker: %+v", marker)
					}
					retained, readErr := replacement.loopsBucket.Get(msgCtx, "COMPLETE_"+loopID)
					if readErr != nil {
						return readErr
					}
					if retained.Revision() != selectedEntry.Revision() || !bytes.Equal(retained.Value(), selectedEntry.Value()) || len(perLoopMapCount(replacement.handler.loopManager, loopID)) != 0 {
						return fmt.Errorf("ACK did not preserve selected completion and release process state")
					}
					return nil
				}}}
				handler(msgCtx, observed)
				settled <- observed
			})
		}
		require.NoError(t, replacement.Start(ctx))
		var redelivered *approvalReplacementDelivery
		select {
		case redelivered = <-settled:
		case <-time.After(40 * time.Second): // An in-flight delivery may wait for the unchanged production BackOff.
			t.Fatal("replacement did not settle the original cancellation source")
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
		require.Equal(t, signalData, redelivered.Data())
		publication, err = stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
		require.NoError(t, err)
		decoded, err = replacement.decoder.Decode(publication.Data)
		require.NoError(t, err)
		require.Equal(t, &selected, decoded.Payload(), "replacement changed the selected cancellation")
		require.Empty(t, perLoopMapCount(replacement.handler.loopManager, loopID))
		require.Eventually(t, func() bool {
			info, infoErr := consumer.Info(ctx)
			return infoErr == nil && info.NumAckPending == 0 && info.AckFloor.Stream >= metadata.Sequence.Stream
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationVerdictMissingWaiterRetriesAfterComponentReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	const suffix = "verdict-waiter-replacement"
	const port = "agent.toolcall.rejected"
	const executionID = "execution-replacement"
	first := newTerminalMarkerProcess(t, tc.Client, suffix)
	first.handler.SetGovernanceDispatcher(&enforceDispatcher{logger: first.logger, waiters: make(map[string]verdictWaiter)})
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	firstSettled := make(chan *approvalReplacementDelivery, 1)
	handles := make(map[string]*fastLaneObservedConsumer)
	var firstFatal error
	first.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			if owner.Port != port {
				handler(msgCtx, msg)
				return
			}
			if firstCtx.Err() != nil {
				return
			}
			observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg}}
			handler(msgCtx, observed)
			first.mu.RLock()
			firstFatal = first.deliveryFatalErr
			first.mu.RUnlock()
			cancelFirst() // Controlled replacement after the actual immediate NAK.
			firstSettled <- observed
		})
		if err != nil {
			return nil, err
		}
		observed := &fastLaneObservedConsumer{ConsumeContext: handle}
		handles[owner.Port] = observed
		return observed, nil
	}
	require.NoError(t, first.Start(firstCtx))
	data := settlementEnvelope(t, &message.GenericJSONPayload{Data: map[string]any{
		"decision": "rejected", "execution_id": executionID,
		"loop_id": "loop-1", "request_id": "request-1", "proposal_fingerprint": "fingerprint",
	}})
	require.NoError(t, tc.Client.PublishToStream(ctx, port+"."+executionID, data))
	var failed *approvalReplacementDelivery
	select {
	case failed = <-firstSettled:
	case <-time.After(5 * time.Second):
		t.Fatal("missing-waiter verdict did not reach the production callback")
	}
	require.Equal(t, 1, failed.naks)
	require.Zero(t, failed.acks+failed.terms)
	require.Nil(t, firstFatal)
	for ownerPort, handle := range handles {
		require.Zero(t, handle.drains.Load(), "missing waiter drained %s before controlled Stop", ownerPort)
	}
	firstMetadata, err := failed.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)
	require.NoError(t, first.Stop(ctx))
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, firstMetadata.Consumer)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, firstMetadata.Sequence.Stream)

	replacement := newTerminalMarkerProcess(t, tc.Client, suffix)
	dispatcher := &enforceDispatcher{logger: replacement.logger, waiters: make(map[string]verdictWaiter)}
	waiter := dispatcher.registerWaiter(governanceTestProposal(executionID)).arrivals
	unrelated := dispatcher.registerWaiter(governanceTestProposal("other-execution")).arrivals
	replacement.handler.SetGovernanceDispatcher(dispatcher)
	settled := make(chan *approvalReplacementDelivery, 1)
	replacement.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			if owner.Port != port {
				handler(msgCtx, msg)
				return
			}
			observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
				select {
				case verdict := <-waiter:
					if verdict.decision != "rejected" || len(unrelated) != 0 {
						return fmt.Errorf("ACK followed the wrong verdict or execution route: %+v", verdict)
					}
					return nil
				default:
					return fmt.Errorf("source ACK preceded delivery to the recreated waiter")
				}
			}}}
			handler(msgCtx, observed)
			settled <- observed
		})
	}
	require.NoError(t, replacement.Start(ctx))
	var redelivered *approvalReplacementDelivery
	select {
	case redelivered = <-settled:
	case <-time.After(40 * time.Second):
		t.Fatal("replacement did not receive the original missing-waiter source")
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
	require.Equal(t, data, redelivered.Data())
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(ctx)
		return infoErr == nil && info.NumAckPending == 0 && info.AckFloor.Stream >= metadata.Sequence.Stream
	}, 5*time.Second, 10*time.Millisecond)
}
