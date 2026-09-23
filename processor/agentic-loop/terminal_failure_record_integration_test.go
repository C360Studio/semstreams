//go:build integration

package agenticloop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The terminal record (COMPLETE_<loopID>) is created BEFORE the failure event
// is published, and the delivery settles on the whole terminal commit. This needs a broker because the claim is
// about what reached the stream: with a nil client "nothing was published" is
// true by construction and proves nothing.
//
// The route is the tool-result timeout (handlers.go:2420-2433): HandleToolResult
// fails the loop, builds its failure record AND its failure publications, and
// returns them with a fatal error, so settleFailedToolResult persists the
// terminal result and settles on it. Before round 3 that branch stamped graph
// triples and published the event with COMPLETE_<loopID> never written.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestIntegrationTerminalFailureRecordPrecedesItsPublication(t *testing.T) {
	timedOutLoop := func(
		t *testing.T,
	) (*Component, *natsclient.TestClient, *recordingLoopBucket, string, string, string) {
		t.Helper()
		testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV(),
			natsclient.WithStreams(natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}}))
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-terminal-record", "general", "model", 3)
		require.NoError(t, err)
		callID := "call-terminal-record"
		executionID := "execution-terminal-record"
		handler.loopManager.TrackToolCall(executionID, loopID)
		require.NoError(t, handler.loopManager.AddPendingTool(loopID, callID))
		require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
		c := releaseTestComponent(t, handler)
		c.natsClient = testClient.Client
		require.NoError(t, c.initializeKVBuckets(t.Context()))
		// After initializeKVBuckets, which installs the real bucket.
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		seedLoopRecord(t, c, loopID)
		return c, testClient, bucket, loopID, executionID, callID
	}
	// Both identities: the lane routes on the framework execution id and the
	// loop's pending-tool set is keyed by the provider call id, so a result
	// carrying one of them settles as an expected drop — which also ACKs, and
	// would leave the ACK assertion below passing over a lane that never ran.
	toolResultBytes := func(t *testing.T, executionID, callID string) []byte {
		t.Helper()
		toolResult := &agentic.ToolResult{
			ExecutionID: executionID, CallID: callID, Name: "search", Content: "executor ran this"}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		return data
	}
	published := func(t *testing.T, tc *natsclient.TestClient) uint64 {
		t.Helper()
		stream, err := tc.Client.GetStream(t.Context(), "AGENT")
		require.NoError(t, err)
		info, err := stream.Info(t.Context())
		require.NoError(t, err)
		return info.State.Msgs
	}

	t.Run("the record is present and the event is published", func(t *testing.T) {
		c, tc, bucket, loopID, executionID, callID := timedOutLoop(t)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := deliverylane.Consume(t.Context(), msg,
			heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())

		record, ok := bucket.value("COMPLETE_" + loopID)
		require.True(t, ok, "the terminal record must exist before the delivery is acknowledged")
		var failure agentic.LoopFailedEvent
		require.NoError(t, json.Unmarshal(record, &failure))
		require.Equal(t, agentic.OutcomeFailed, failure.Outcome)
		require.Positive(t, published(t, tc), "the failure event reaches the stream on the healthy path")
	})

	t.Run("a terminal record that cannot be written publishes nothing", func(t *testing.T) {
		c, tc, bucket, loopID, executionID, callID := timedOutLoop(t)
		// Only the record write fails. Before #1362 the loop key still landed
		// here, and a branch that wrote it and ACKed was the defect this test
		// was written for; the terminal owner now writes the loop key LAST, so
		// a marker that did not land leaves it unwritten.
		bucket.fail = errKVUnavailable
		bucket.failPrefix = "COMPLETE_"

		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := deliverylane.Consume(t.Context(), msg,
			heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
			"a terminal failure whose record did not land cannot be acknowledged")
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())

		require.NotContains(t, bucket.written(), loopID,
			"the loop record was written terminal ahead of its marker: the record is the terminal owner's last step")
		require.Zero(t, published(t, tc),
			"the failure event was published before its record: a watcher would read agent.failed "+
				"and find no COMPLETE_ record behind it")
	})
}
