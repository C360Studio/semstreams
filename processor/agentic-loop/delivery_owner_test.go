package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func task4HeartbeatPolicy(t *testing.T, port string, handler inputHandler) natsclient.HeartbeatDeliveryPolicy {
	t.Helper()
	policy, err := newLoopHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{
		AckWait: 2 * time.Minute, BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}, MaxDeliver: 2,
	}, 15*time.Second, port, handler)
	require.NoError(t, err)
	return policy
}

type loopDeliveryOwnerMsg struct {
	data        []byte
	dataCalls   atomic.Int32
	heartbeats  atomic.Int32
	settlement  atomic.Int32
	acks        atomic.Int32
	naks        atomic.Int32
	terms       atomic.Int32
	metadata    atomic.Int32
	metadataErr error
}

func (m *loopDeliveryOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return m.data }
func (*loopDeliveryOwnerMsg) Subject() string      { return "agent.task.test" }
func (*loopDeliveryOwnerMsg) Reply() string        { return "" }
func (*loopDeliveryOwnerMsg) Headers() nats.Header { return nil }
func (m *loopDeliveryOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadata.Add(1)
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *loopDeliveryOwnerMsg) Ack() error                    { m.acks.Add(1); m.settlement.Add(1); return nil }
func (*loopDeliveryOwnerMsg) DoubleAck(context.Context) error { return nil }
func (m *loopDeliveryOwnerMsg) Nak() error                    { m.naks.Add(1); m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) NakWithDelay(time.Duration) error {
	m.naks.Add(1)
	m.settlement.Add(1)
	return nil
}
func (m *loopDeliveryOwnerMsg) InProgress() error           { m.heartbeats.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) Term() error                 { m.terms.Add(1); m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) TermWithReason(string) error { return m.Term() }

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestResponseAndToolResultPersistenceFailureCannotAck(t *testing.T) {
	newPolicy := func(t *testing.T, port string, handler inputHandler) natsclient.HeartbeatDeliveryPolicy {
		t.Helper()
		policy, err := newLoopHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{
			AckWait: 2 * time.Minute, BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}, MaxDeliver: 2,
		}, 15*time.Second, port, handler)
		require.NoError(t, err)
		return policy
	}

	t.Run("agent response", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-response", "general", "model", 3)
		require.NoError(t, err)
		requestID := handler.loopManager.GenerateRequestID(loopID)
		handler.loopManager.TrackRequest(requestID, loopID)
		c := releaseTestComponent(t, handler)
		c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}
		c.settlementEvidence = &settlementEvidence{
			request: retainedRequest(t, loopID, requestID), requestFound: true,
		}
		response := &agentic.AgentResponse{
			RequestID: requestID, Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
		}
		data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
		require.NoError(t, err)
		msg := &loopDeliveryOwnerMsg{data: data}
		result, admitted := consumeAdmittedDelivery(t.Context(), msg, newPolicy(t, "agent.response", c.handleResponseMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Equal(t, int32(1), msg.naks.Load())
		require.Contains(t, result.Err().Error(), "persist completion state")
	})

	t.Run("tool result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-tool", "general", "model", 3)
		require.NoError(t, err)
		requestID := loopID + ":req:" + uuid.NewString()
		_, err = handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: requestID, Status: "tool_call",
			Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-tool", Name: "search"}}},
		})
		require.NoError(t, err)
		c := releaseTestComponent(t, handler)
		c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}
		toolResult := &agentic.ToolResult{
			RequestID: requestID, ExecutionID: deriveToolExecutionID(requestID, "call-tool", 1),
			CallID: "call-tool", CallOrdinal: 1, Name: "search", Content: "result",
		}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		msg := &loopDeliveryOwnerMsg{data: data}
		result, admitted := consumeAdmittedDelivery(t.Context(), msg, newPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Equal(t, int32(1), msg.naks.Load())
		require.Contains(t, result.Err().Error(), "persist loop state")
	})
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestTaskPersistenceFailureCannotAck(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}
	task := &agentic.TaskMessage{
		LoopID: uuid.NewString(), TaskID: "task-kv-failure", Role: "general", Model: "model", Prompt: "work",
	}
	data, err := json.Marshal(message.NewBaseMessage(task.Schema(), task, "test"))
	require.NoError(t, err)
	msg := &loopDeliveryOwnerMsg{data: data}

	result, admitted := consumeAdmittedDelivery(
		t.Context(), msg, task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute)), newDeliveryLaneAdmission(nil),
	)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
	require.Zero(t, msg.acks.Load()+msg.terms.Load())
	require.Equal(t, int32(1), msg.naks.Load())
	require.Contains(t, result.Err().Error(), "persist loop state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestToolTimeoutCommitsTerminalFailureBeforeAck(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-tool-timeout", "general", "model", 3)
	require.NoError(t, err)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	entity.TimeoutAt = time.Now().Add(-time.Second)
	require.NoError(t, handler.UpdateLoop(entity))
	executionID := deriveToolExecutionID(loopID+":req:1", "call-timeout", 1)
	handler.loopManager.TrackToolCall(executionID, loopID)
	handler.loopManager.TrackToolName(executionID, "search")
	handler.loopManager.TrackToolOrdinal(executionID, 1)
	bucket := &settlementBucket{values: make(map[string][]byte)}
	c := releaseTestComponent(t, handler)
	c.loopsBucket = bucket
	toolResult := &agentic.ToolResult{
		LoopID: loopID, RequestID: loopID + ":req:1", ExecutionID: executionID,
		CallID: "call-timeout", CallOrdinal: 1, Name: "search", Content: "late",
	}

	decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, toolResult))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID)
	var persisted agentic.LoopEntity
	require.NoError(t, json.Unmarshal(bucket.values[loopID], &persisted))
	require.Equal(t, agentic.LoopStateFailed, persisted.State)
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "settled terminal tool failure retained process state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestMalformedLoopWorkTerminatesInsteadOfAcking(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		handler inputHandler
	}{
		{name: "task", port: "agent.task"},
		{name: "response", port: "agent.response"},
		{name: "tool result", port: "tool.result"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			switch tc.port {
			case "agent.task":
				tc.handler = c.taskInputHandler(time.Minute)
			case "agent.response":
				tc.handler = c.handleResponseMessage
			case "tool.result":
				tc.handler = c.handleToolResultMessage
			}
			msg := &loopDeliveryOwnerMsg{data: []byte(`{"not":"a registered envelope"}`)}

			result, admitted := consumeAdmittedDelivery(
				t.Context(), msg, task4HeartbeatPolicy(t, tc.port, tc.handler), newDeliveryLaneAdmission(nil),
			)

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
			require.Zero(t, msg.acks.Load()+msg.naks.Load())
			require.Equal(t, int32(1), msg.terms.Load())
		})
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestRegisteredInvalidLoopPayloadTerminatesBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *Component) ([]byte, string)
		handle  func(*Component) inputHandler
	}{
		{
			name: "response status",
			prepare: func(t *testing.T, c *Component) ([]byte, string) {
				loopID, err := c.handler.loopManager.CreateLoop("task-invalid-response", "general", "model", 3)
				require.NoError(t, err)
				requestID := loopID + ":req:1"
				c.handler.loopManager.TrackRequest(requestID, loopID)
				valid := settlementEnvelope(t, &agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusComplete})
				return []byte(strings.Replace(string(valid), `"status":"complete"`, `"status":"not-a-status"`, 1)), loopID
			},
			handle: func(c *Component) inputHandler { return c.handleResponseMessage },
		},
		{
			name: "tool result call ID",
			prepare: func(t *testing.T, c *Component) ([]byte, string) {
				loopID, err := c.handler.loopManager.CreateLoop("task-invalid-tool", "general", "model", 3)
				require.NoError(t, err)
				requestID := loopID + ":req:1"
				executionID := deriveToolExecutionID(requestID, "call-1", 1)
				c.handler.loopManager.TrackToolCall(executionID, loopID)
				valid := settlementEnvelope(t, &agentic.ToolResult{
					LoopID: loopID, RequestID: requestID, ExecutionID: executionID,
					CallID: "call-1", CallOrdinal: 1, Name: "search", Content: "result",
				})
				return []byte(strings.Replace(string(valid), `"call_id":"call-1"`, `"call_id":""`, 1)), loopID
			},
			handle: func(c *Component) inputHandler { return c.handleToolResultMessage },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			data, loopID := tc.prepare(t, c)
			before, err := c.handler.GetLoop(loopID)
			require.NoError(t, err)

			decision, err := tc.handle(c)(t.Context(), data)

			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			after, lookupErr := c.handler.GetLoop(loopID)
			require.NoError(t, lookupErr)
			require.Equal(t, before, after, "invalid payload mutated loop state")
		})
	}
}

// spec: agentic-loop / Delivery work joins before settlement
func TestCancelledResponseRetriesWithoutTerminalEffects(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-cancelled-response", "general", "model", 3)
	require.NoError(t, err)
	requestID := loopID + ":req:" + uuid.NewString()
	handler.loopManager.TrackRequest(requestID, loopID)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c := releaseTestComponent(t, handler)
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	decision, err := c.handleResponseMessage(ctx, settlementEnvelope(t, &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
	}))

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.NotContains(t, bucket.values, "COMPLETE_"+loopID)
	var durable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(bucket.values[loopID], &durable))
	require.False(t, durable.State.IsTerminal())
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "cancelled response retained process-local loop state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestImpossibleSpawnFailureTransitionQuarantinesThroughHeartbeatOwner(t *testing.T) {
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.deps.Platform = component.PlatformMeta{Org: "acme", Platform: "ops"}
	c.graphWriter = &graphWriter{logger: slog.Default()}
	c.testLineageWriteHook = func(_ context.Context, loopID string, _ map[string]any) error {
		require.NoError(t, c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		return errs.WrapInvalid(errors.New("lineage unavailable"), "agentic-loop", "test", "invalid lineage")
	}
	task := &agentic.TaskMessage{
		LoopID: uuid.NewString(), TaskID: "task-impossible-spawn", Role: "general", Model: "model", Prompt: "work",
		Metadata: map[string]any{
			agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": uuid.NewString()},
		},
	}
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, task)}

	result, admitted := consumeAdmittedDelivery(
		t.Context(), msg, task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute)), newDeliveryLaneAdmission(nil),
	)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(), "cause: %v", result.Err())
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestMissingProcessCorrelationRetriesInsteadOfAcking(t *testing.T) {
	toolLoopID := uuid.NewString()
	tests := []struct {
		name    string
		port    string
		payload message.Payload
	}{
		{
			name: "response", port: "agent.response",
			payload: &agentic.AgentResponse{
				RequestID: uuid.NewString() + ":req:" + uuid.NewString(), Status: agentic.StatusComplete,
				Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
			},
		},
		{
			name: "tool result", port: "tool.result",
			payload: &agentic.ToolResult{
				LoopID: toolLoopID, RequestID: toolLoopID + ":req:" + uuid.NewString(),
				CallID: "call-1", ExecutionID: "execution-1", CallOrdinal: 1, Name: "search", Content: "done",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			data, err := json.Marshal(message.NewBaseMessage(tc.payload.Schema(), tc.payload, "test"))
			require.NoError(t, err)
			msg := &loopDeliveryOwnerMsg{data: data}
			var handler inputHandler
			if tc.port == "agent.response" {
				handler = c.handleResponseMessage
			} else {
				handler = c.handleToolResultMessage
			}

			result, admitted := consumeAdmittedDelivery(
				t.Context(), msg, task4HeartbeatPolicy(t, tc.port, handler), newDeliveryLaneAdmission(nil),
			)

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
			require.Zero(t, msg.acks.Load()+msg.terms.Load())
			require.Equal(t, int32(1), msg.naks.Load())
		})
	}
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTaskAssemblyFailureRollsBackProcessRegistration(t *testing.T) {
	config := DefaultConfig()
	config.Ports.Outputs = nil
	handler := NewMessageHandler(config)
	loopID := uuid.NewString()

	_, err := handler.HandleTask(t.Context(), TaskMessage{
		LoopID: loopID, TaskID: "task-assembly-failure", Role: "general", Model: "model", Prompt: "work",
	})

	require.Error(t, err)
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "failed task assembly left a registered loop that redelivery would ACK as a duplicate")
}

// spec: entity-id-contract / A loop instance token is minted at its framework birth seam
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestDirectHandleTaskRefusesMissingLoopIDBeforeState(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())

	_, err := handler.HandleTask(t.Context(), TaskMessage{
		TaskID: "task-missing-loop", Role: "general", Model: "model", Prompt: "work",
	})

	require.Error(t, err)
	require.True(t, errs.IsInvalid(err))
	require.ErrorContains(t, err, "loop_id")
	_, active := handler.loopManager.HasActiveLoopForTask("task-missing-loop")
	require.False(t, active)
	require.Empty(t, handler.loopManager.loops)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestDirectHandleTaskQuarantinesTaskIDLoopIDConflict(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	first := TaskMessage{
		LoopID: uuid.NewString(), TaskID: "task-loop-conflict", Role: "general", Model: "model", Prompt: "work",
	}
	_, err := handler.HandleTask(t.Context(), first)
	require.NoError(t, err)

	conflict := first
	conflict.LoopID = uuid.NewString()
	_, err = handler.HandleTask(t.Context(), conflict)

	require.Error(t, err)
	require.True(t, errs.IsFatal(err), "task correlation conflict must quarantine")
	require.ErrorContains(t, err, first.LoopID)
	require.ErrorContains(t, err, conflict.LoopID)
	require.Len(t, handler.loopManager.loops, 1)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestTaskDeliveryQuarantinesTaskIDLoopIDConflict(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	first := TaskMessage{
		LoopID: uuid.NewString(), TaskID: "task-delivery-loop-conflict", Role: "general", Model: "model", Prompt: "work",
	}
	_, err := handler.HandleTask(t.Context(), first)
	require.NoError(t, err)

	conflict := first
	conflict.LoopID = uuid.NewString()
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, &conflict)}
	c := releaseTestComponent(t, handler)
	result, admitted := consumeAdmittedDelivery(
		t.Context(), msg, task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute)), newDeliveryLaneAdmission(nil),
	)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(), "cause: %v", result.Err())
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Len(t, handler.loopManager.loops, 1)
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner(t *testing.T) {
	retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)
	var workCalls atomic.Int32
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(),
		natsclient.StreamConsumerConfig{BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}},
		15*time.Second,
		retry,
		func(context.Context, natsclient.DeliveryAttempt, []byte) (natsclient.DeliveryDecision, error) {
			workCalls.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		},
	)
	require.NoError(t, err)
	admission := newDeliveryLaneAdmission(nil)
	metadataCause := errors.New("metadata unavailable")
	msg := &loopDeliveryOwnerMsg{data: []byte("must-not-run"), metadataErr: metadataCause}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg, policy, admission)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.True(t, result.Quarantined())
	require.True(t, result.OwnerStopRequired())
	require.Contains(t, result.Err().Error(), "delivery_metadata_unavailable")
	require.ErrorIs(t, result.Cause(), metadataCause)
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, workCalls.Load())
	require.Zero(t, msg.heartbeats.Load())
	require.Zero(t, msg.settlement.Load())

	handle := &loopPolicyHandle{closed: make(chan struct{})}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.observeDeliveryLane(ctx, &binding, admission, "agent.task")
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = consumeAdmittedDelivery(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed admission must not inspect another delivery")
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load(), "fatal and ordinary stop share exact drain-once authority")
	cancel()
	<-binding.observerDone
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopSetupWiresMetadataFailureToAcquiredOwner(t *testing.T) {
	handles := []*loopPolicyHandle{
		{closed: make(chan struct{})},
		{closed: make(chan struct{})},
	}
	callbacks := make([]func(context.Context, jetstream.Msg), 0, len(handles))
	var workCalls atomic.Int32
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), started: true, startTime: time.Now(),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			index := len(callbacks)
			callbacks = append(callbacks, handler)
			return handles[index], nil
		},
	}
	c.trajectoryAuditHealth.latch("trajectory audit degraded")
	require.Equal(t, 1, c.Health().ErrorCount)

	ctx, cancel := context.WithCancel(t.Context())
	for _, portName := range []string{"agent.task", "agent.response"} {
		port, err := (component.PortDefinition{
			Name:     portName,
			Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{portName + ".>"}},
			Required: true,
		}).Resolve(component.DirectionInput)
		require.NoError(t, err)
		require.NoError(t, c.setupConsumer(
			ctx, ctx, port, portName+".>", func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
				workCalls.Add(1)
				return natsclient.DeliveryDecisionAck, nil
			},
			nil,
		))
	}
	require.Len(t, callbacks, 2)
	require.Len(t, c.consumers, 2)

	firstCause := errors.New("first metadata unavailable")
	first := &loopDeliveryOwnerMsg{metadataErr: firstCause}
	callbacks[0](ctx, first)
	firstHealth := c.Health()
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status,
		"owner loss must take precedence over trajectory degradation")
	require.Equal(t, "delivery_metadata_unavailable: "+firstCause.Error(), firstHealth.LastError)
	require.NotContains(t, firstHealth.LastError, "trajectory audit degraded")
	require.Equal(t, 2, firstHealth.ErrorCount, "owner loss adds exactly one error to existing trajectory degradation")
	require.Eventually(t, func() bool { return handles[0].drains.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, handles[1].drains.Load(), "first fatal must not drain another owner")

	secondCause := errors.New("later metadata unavailable")
	second := &loopDeliveryOwnerMsg{metadataErr: secondCause}
	callbacks[1](ctx, second)
	require.Eventually(t, func() bool { return handles[1].drains.Load() == 1 }, time.Second, time.Millisecond)
	secondHealth := c.Health()
	require.Equal(t, "delivery ownership lost", secondHealth.Status)
	require.Equal(t, firstHealth.LastError, secondHealth.LastError, "the first fatal cause must remain sticky")
	require.NotContains(t, secondHealth.LastError, secondCause.Error())
	require.Equal(t, 2, secondHealth.ErrorCount, "later fatal owner loss must not recount")
	require.Equal(t, int32(1), handles[0].drains.Load(), "later fatal must not redrain the first owner")

	callbacks[0](ctx, first)
	require.Equal(t, int32(1), first.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	require.Zero(t, workCalls.Load())
	for index, msg := range []*loopDeliveryOwnerMsg{first, second} {
		require.Zero(t, msg.dataCalls.Load(), "message %d must not expose data to work", index)
		require.Zero(t, msg.heartbeats.Load(), "message %d must not heartbeat", index)
		require.Zero(t, msg.settlement.Load(), "message %d must not settle", index)
		require.Equal(t, int32(1), handles[index].drains.Load(), "only the exact owner handle drains once")
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopProductionCallbacksTerminateMalformedNonHeartbeatInputs(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	verdicts := &settlementVerdictDispatcher{received: make(chan string, 2)}
	c.handler.SetGovernanceDispatcher(verdicts)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &loopPolicyHandle{closed: make(chan struct{})}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	for _, port := range []string{"agent.signal", "agent.approval_response", "agent.toolcall.approved", "agent.toolcall.rejected"} {
		callback, ok := callbacks[port]
		require.True(t, ok, "production setup did not bind %s", port)
		msg := &loopSettlementMsg{data: []byte("{")}
		callback(ctx, msg)
		require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)
		require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)
		require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)
	}
	for _, row := range []struct {
		port     string
		decision string
		callID   string
	}{
		{port: "agent.toolcall.approved", decision: "approved", callID: "call-approved"},
		{port: "agent.toolcall.rejected", decision: "rejected", callID: "call-rejected"},
	} {
		data := []byte(`{"decision":"` + row.decision + `","execution_id":"` + row.callID + `"}`)
		msg := &loopSettlementMsg{data: data}
		callbacks[row.port](ctx, msg)
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, msg.naks.Load()+msg.terms.Load())
		require.Equal(t, row.decision+":"+row.callID, <-verdicts.received)
	}

	cancel()
	for _, binding := range c.consumers {
		if binding.observerDone != nil {
			<-binding.observerDone
		}
	}
}

type settlementVerdictDispatcher struct{ received chan string }

func (*settlementVerdictDispatcher) Propose(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
	return DispatcherResult{}, nil
}
func (d *settlementVerdictDispatcher) HandleVerdict(decision, callID string, _ []byte) (natsclient.DeliveryDecision, error) {
	d.received <- decision + ":" + callID
	return natsclient.DeliveryDecisionAck, nil
}
func (*settlementVerdictDispatcher) Mode() string { return "enforce" }

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// scenario: Approval handler panics
func TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	loopID := c.handler.loopManager.GenerateLoopID()
	c.handler.loopManager = nil
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	response := &agentic.ApprovalResponse{
		LoopID: loopID, CallID: "call-panic", Decision: agentic.ApprovalDecisionApprove,
		ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	msg := &loopSettlementMsg{data: data}
	callbacks["agent.approval_response"](ctx, msg)

	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["agent.approval_response"].drains.Load() == 1 }, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "agent.approval_response" {
			require.Zero(t, handle.drains.Load(), "approval panic drained unrelated owner %s", port)
		}
	}
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Contains(t, health.LastError, "approval response handler panicked")

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestLoopCancellationUnknownPublicationQuarantinesWithoutReleasingTransientState(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	loopID, err := c.handler.loopManager.CreateLoop("task-cancel", "general", "model", 3)
	require.NoError(t, err)
	_, err = c.handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))
	signal := &agentic.UserSignal{
		SignalID: "signal-cancel", Type: agentic.SignalCancel, LoopID: loopID,
		UserID: "operator", Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(signal.Schema(), signal, "test"))
	require.NoError(t, err)
	msg := &loopSettlementMsg{data: data}
	callbacks["agent.signal"](ctx, msg)

	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["agent.signal"].drains.Load() == 1 }, time.Second, time.Millisecond)
	_, err = c.handler.trajectoryManager.getTrajectory(loopID)
	require.NoError(t, err, "unknown terminal publication released the loop trajectory")
	require.Contains(t, c.Health().LastError, "unknown durability")
	for port, handle := range handles {
		if port != "agent.signal" {
			require.Zero(t, handle.drains.Load(), "cancellation failure drained unrelated owner %s", port)
		}
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.observerDone
	}
}

type loopSettlementMsg struct {
	data  []byte
	acks  atomic.Int32
	naks  atomic.Int32
	terms atomic.Int32
}

func (m *loopSettlementMsg) Data() []byte                            { return m.data }
func (*loopSettlementMsg) Subject() string                           { return "loop.test" }
func (*loopSettlementMsg) Reply() string                             { return "" }
func (*loopSettlementMsg) Headers() nats.Header                      { return nil }
func (*loopSettlementMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *loopSettlementMsg) Ack() error                              { m.acks.Add(1); return nil }
func (*loopSettlementMsg) DoubleAck(context.Context) error           { return nil }
func (m *loopSettlementMsg) Nak() error                              { m.naks.Add(1); return nil }
func (m *loopSettlementMsg) NakWithDelay(time.Duration) error        { m.naks.Add(1); return nil }
func (*loopSettlementMsg) InProgress() error                         { return nil }
func (m *loopSettlementMsg) Term() error                             { m.terms.Add(1); return nil }
func (m *loopSettlementMsg) TermWithReason(string) error             { return m.Term() }
