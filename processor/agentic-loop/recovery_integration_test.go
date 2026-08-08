//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// publishSignalMessage publishes a UserSignal wrapped in a BaseMessage envelope.
func publishSignalMessage(t *testing.T, natsClient *natsclient.Client, subject string, signal *agentic.UserSignal) {
	t.Helper()
	baseMsg := message.NewBaseMessage(signal.Schema(), signal, "integration-test")
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal BaseMessage")
	err = natsClient.PublishToStream(context.Background(), subject, msgData)
	require.NoError(t, err, "Failed to publish signal message")
}

// TestIntegration_CancelMidExecution_NoOrphanToolCalls is the C4
// end-to-end regression guard for mode (e) of orphan tool-call
// recovery. Drives a loop into a state with one in-flight tool call,
// sends a cancel signal, and asserts the KV-persisted loop entity
// carries a synth-rejection in PendingToolResults — proving the
// drain-before-transition wiring at component.handleCancelSignal
// runs end-to-end and KV-restored loops won't carry orphan
// tool_calls.
func TestIntegration_CancelMidExecution_NoOrphanToolCalls(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true},
				{Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}}},
				{Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.>"}}},
				{Name: "agent.signal", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.signal.>"}}},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}}},
				{Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.*"}}},
				{Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}}},
			},
		},
		MaxIterations:        10,
		Timeout:              "60s",
		StreamName:           "AGENT",
		ConsumerNameSuffix:   "cancel-mid-test",
		DeleteConsumerOnStop: true,
		LoopsBucket:          "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	require.NoError(t, lc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	require.NoError(t, lc.Start(ctx))
	defer func() { _ = lc.Stop(5 * time.Second) }()

	time.Sleep(200 * time.Millisecond)

	// Track the first agent.request so we can respond with a tool_call.
	var currentRequestID string
	var requestMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, ok := baseMsg.Payload().(*agentic.AgentRequest); ok {
				requestMu.Lock()
				if currentRequestID == "" {
					currentRequestID = req.RequestID
				}
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	// Subscribe to tool.execute to confirm a tool was dispatched (so
	// the cancel happens with a tool actually in flight).
	dispatchedCh := make(chan struct{}, 1)
	_, err = natsClient.Subscribe(ctx, "tool.execute.>", func(_ context.Context, _ *nats.Msg) {
		select {
		case dispatchedCh <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Submit a task.
	task := &agentic.TaskMessage{
		LoopID: "loop_cancel_orphan",
		TaskID: "task_cancel_orphan",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Trigger a tool call that we'll cancel mid-flight",
	}
	publishTaskMessage(t, natsClient, "agent.task.cancel", task)

	// Wait for the loop to publish its first request.
	require.Eventually(t, func() bool {
		requestMu.Lock()
		defer requestMu.Unlock()
		return currentRequestID != ""
	}, 5*time.Second, 50*time.Millisecond, "loop should publish initial agent.request")

	requestMu.Lock()
	reqID := currentRequestID
	requestMu.Unlock()

	// Respond with a tool_call so the loop dispatches a tool and waits
	// on the result. This is the "in flight" state we want to cancel.
	response := &agentic.AgentResponse{
		RequestID: reqID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-orphan", Name: "test_tool", Arguments: map[string]any{"x": 1}},
			},
		},
	}
	publishResponseMessage(t, natsClient, "agent.response."+reqID, response)

	// Wait for the loop to dispatch the tool — confirms we're in the
	// "tool in flight, no result yet" state when we cancel.
	select {
	case <-dispatchedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not dispatch tool.execute within 5s")
	}

	// Send the cancel signal — drainPendingToolFailures should fire
	// inside handleCancelSignal BEFORE the loop transitions to
	// LoopStateCancelled.
	signal := &agentic.UserSignal{
		SignalID:    "sig-cancel-1",
		Type:        agentic.SignalCancel,
		LoopID:      "loop_cancel_orphan",
		UserID:      "test-user",
		ChannelType: "test",
		ChannelID:   "session-1",
		Timestamp:   time.Now().UTC(),
	}
	publishSignalMessage(t, natsClient, "agent.signal.cancel", signal)

	// Give the cancel handler time to run + persist.
	time.Sleep(1 * time.Second)

	// Read the loop entity from KV and assert tool-pair integrity.
	kv, err := natsClient.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.NoError(t, err)

	entry, err := kv.Get(ctx, "loop_cancel_orphan")
	require.NoError(t, err, "loop entity should be persisted to KV")

	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))

	// State should be cancelled (terminal).
	require.Equal(t, agentic.LoopStateCancelled, entity.State,
		"loop should be in cancelled state")

	// Critical assertion: the orphan tool_call must have a matching
	// synth-result in PendingToolResults — this is what
	// drainPendingToolFailures is for.
	synthResult, ok := entity.PendingToolResults["call-orphan"]
	require.True(t, ok,
		"PendingToolResults missing synth-rejection for call-orphan; "+
			"orphan recovery did not fire (mode e regression)")
	require.Contains(t, synthResult.Error, "loop cancelled",
		"synth-result should carry cancel diagnostic; got %q", synthResult.Error)
}
