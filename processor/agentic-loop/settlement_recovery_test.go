package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type settlementEntry struct {
	key   string
	value []byte
}

func (e settlementEntry) Bucket() string                  { return "AGENT_LOOPS" }
func (e settlementEntry) Key() string                     { return e.key }
func (e settlementEntry) Value() []byte                   { return append([]byte(nil), e.value...) }
func (e settlementEntry) Revision() uint64                { return 1 }
func (e settlementEntry) Created() time.Time              { return time.Time{} }
func (e settlementEntry) Delta() uint64                   { return 0 }
func (e settlementEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

type settlementBucket struct {
	jetstream.KeyValue
	values      map[string][]byte
	getErr      error
	putErr      error
	failPutKey  string
	failPutLeft int
}

func (b *settlementBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	value, ok := b.values[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return settlementEntry{key: key, value: value}, nil
}

func (b *settlementBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	if b.putErr != nil {
		return 0, b.putErr
	}
	if key == b.failPutKey && b.failPutLeft > 0 {
		b.failPutLeft--
		return 0, errors.New("injected final marker failure")
	}
	b.values[key] = append([]byte(nil), value...)
	return 1, nil
}

type settlementEvidence struct {
	request       retainedLoopMessage
	requestFound  bool
	requestErr    error
	response      retainedLoopMessage
	responseFound bool
	responseErr   error
}

func (e *settlementEvidence) ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error) {
	return e.request, e.requestFound, e.requestErr
}

func (e *settlementEvidence) ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error) {
	return e.response, e.responseFound, e.responseErr
}

func settlementEnvelope(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
	return data
}

func settlementLoopRecord(t *testing.T, entity agentic.LoopEntity) []byte {
	t.Helper()
	data, err := json.Marshal(entity)
	require.NoError(t, err)
	return data
}

func retainedRequest(t *testing.T, loopID, requestID string) retainedLoopMessage {
	t.Helper()
	request := &agentic.AgentRequest{
		RequestID: requestID, LoopID: loopID, Role: "general", Model: "model",
		Messages: []agentic.ChatMessage{{Role: "user", Content: "work"}},
	}
	return retainedLoopMessage{subject: "agent.request." + loopID, data: settlementEnvelope(t, request)}
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestColdTaskRedeliveryReusesRetainedRequest(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	task := &agentic.TaskMessage{
		LoopID: loopID, TaskID: entity.TaskID, Role: entity.Role, Model: entity.Model, Prompt: "work",
	}

	decision, err := c.handleTaskMessage(t.Context(), settlementEnvelope(t, task))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	gotLoopID, found := c.handler.loopManager.GetLoopForRequest(requestID)
	require.True(t, found, "retained request was not restored as the active provider correlation")
	require.Equal(t, loopID, gotLoopID,
		"cold task replay minted a different logical provider request instead of reusing retained evidence")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestColdTaskRedeliveryConflictingMappingQuarantines(t *testing.T) {
	loopID := uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "durable-task", "general", "model", 3)
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{}
	task := &agentic.TaskMessage{
		LoopID: loopID, TaskID: "conflicting-task", Role: entity.Role, Model: entity.Model, Prompt: "work",
	}

	decision, err := c.handleTaskMessage(t.Context(), settlementEnvelope(t, task))

	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	_, lookupErr := c.handler.GetLoop(loopID)
	require.Error(t, lookupErr, "conflicting durable mapping was installed in process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop(t *testing.T) {
	loopID := uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	entity.StartedAt = time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{}
	task := &agentic.TaskMessage{
		LoopID: loopID, TaskID: entity.TaskID, Role: entity.Role, Model: entity.Model, Prompt: "work",
	}

	decision, err := c.handleTaskMessage(t.Context(), settlementEnvelope(t, task))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	got, lookupErr := c.handler.GetLoop(loopID)
	require.NoError(t, lookupErr)
	require.Equal(t, entity.StartedAt, got.StartedAt,
		"cold task replay replaced the committed loop record instead of resuming its partial birth")
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestColdModelResponseRestoresExactLoopAndCommitsTerminalState(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
	}

	decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID)
	var persisted agentic.LoopEntity
	require.NoError(t, json.Unmarshal(bucket.values[loopID], &persisted))
	require.Equal(t, agentic.LoopStateComplete, persisted.State)
	require.Equal(t, "done", persisted.Result)
	_, lookupErr := c.handler.GetLoop(loopID)
	require.Error(t, lookupErr, "terminal settlement did not release restored process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestColdModelResponseCorrelationConflictQuarantines(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	tests := []struct {
		name    string
		entity  agentic.LoopEntity
		request retainedLoopMessage
	}{
		{
			name: "research loop record has no task owner",
			entity: func() agentic.LoopEntity {
				research := entity
				research.TaskID = ""
				return research
			}(),
			request: retainedRequest(t, loopID, requestID),
		},
		{
			name:    "retained request identity conflicts",
			entity:  entity,
			request: retainedRequest(t, loopID, loopID+":req:"+uuid.NewString()),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, tc.entity)}}
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			c.loopsBucket = bucket
			c.settlementEvidence = &settlementEvidence{request: tc.request, requestFound: true}
			response := &agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusComplete}

			decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))

			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.NotContains(t, bucket.values, "COMPLETE_"+loopID)
		})
	}
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestColdToolResultRestoresOriginatingBatch(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	callID := "provider-call"
	executionID := deriveToolExecutionID(requestID, callID, 1)
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: callID, Name: "search"}}},
	}
	result := &agentic.ToolResult{
		LoopID: loopID, RequestID: requestID, CallID: callID, ExecutionID: executionID,
		CallOrdinal: 1, Name: "search", Content: "answer",
	}
	newComponent := func(t *testing.T, state agentic.LoopState) *Component {
		t.Helper()
		entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
		entity.State = state
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
		c.settlementEvidence = &settlementEvidence{
			request: retainedRequest(t, loopID, requestID), requestFound: true,
			response:      retainedLoopMessage{subject: "agent.response." + requestID, data: settlementEnvelope(t, response)},
			responseFound: true,
		}
		return c
	}

	t.Run("live turn restores and continues", func(t *testing.T) {
		c := newComponent(t, agentic.LoopStateExecuting)
		decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, result))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		entity, err := c.handler.GetLoop(loopID)
		require.NoError(t, err)
		require.Equal(t, 1, entity.Iterations)
		require.Equal(t, *result, entity.PendingToolResults[executionID], "result must remain durable through next request PubAck")
		var durable agentic.LoopEntity
		require.NoError(t, json.Unmarshal(c.loopsBucket.(*settlementBucket).values[loopID], &durable))
		require.Equal(t, *result, durable.PendingToolResults[executionID], "the KV checkpoint must retain result evidence")
		messages := c.handler.loopManager.GetContextManager(loopID).GetContext()
		require.Len(t, messages, 3)
		require.Equal(t, "assistant", messages[1].Role)
		require.Equal(t, callID, messages[2].ToolCallID)
		require.Equal(t, result.Content, messages[2].Content)
	})

	t.Run("terminal marker is not execution-specific proof", func(t *testing.T) {
		decision, err := newComponent(t, agentic.LoopStateComplete).handleToolResultMessage(t.Context(), settlementEnvelope(t, result))
		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
		require.Contains(t, err.Error(), "execution-specific")
	})

	t.Run("lookup failure retries", func(t *testing.T) {
		c := newComponent(t, agentic.LoopStateExecuting)
		c.settlementEvidence = &settlementEvidence{responseErr: errors.New("stream unavailable")}
		decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, result))
		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	})
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestColdModelResponseFinalMarkerProvesExactRequestApplied(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	entity.State = agentic.LoopStateComplete
	entity.Outcome = agentic.OutcomeSuccess
	entity.Result = "already done"
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "duplicate"},
	}

	decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	_, lookupErr := c.handler.GetLoop(loopID)
	require.Error(t, lookupErr, "applied duplicate should not restore terminal process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestResponsePersistenceRetryDiscardsSpeculativeTurnBeforeRedelivery(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	handler := NewMessageHandler(DefaultConfig())
	created, err := handler.loopManager.CreateLoopWithID(loopID, "task-response-retry", "general", "model", 3)
	require.NoError(t, err)
	require.Equal(t, loopID, created)
	handler.loopManager.TrackRequest(requestID, loopID)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	bucket := &settlementBucket{
		values: map[string][]byte{loopID: settlementLoopRecord(t, entity)},
		putErr: errors.New("loop state unavailable"),
	}
	c := releaseTestComponent(t, handler)
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-1", Name: "search"}}},
	}
	data := settlementEnvelope(t, response)

	decision, err := c.handleResponseMessage(t.Context(), data)
	require.ErrorIs(t, err, bucket.putErr)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "failed response attempt retained speculative process state")

	bucket.putErr = nil
	decision, err = c.handleResponseMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	contextMessages := handler.loopManager.GetContextManager(loopID).GetContext()
	require.Len(t, contextMessages, 2, "redelivery duplicated the assistant turn")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestToolPersistenceRetryDiscardsWarmRoutingBeforeColdRedelivery(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	callID := "call-1"
	executionID := deriveToolExecutionID(requestID, callID, 1)
	handler := NewMessageHandler(DefaultConfig())
	created, err := handler.loopManager.CreateLoopWithID(loopID, "task-tool-retry", "general", "model", 3)
	require.NoError(t, err)
	require.Equal(t, loopID, created)
	require.NoError(t, handler.loopManager.AddPendingTool(loopID, callID))
	handler.loopManager.TrackToolCall(executionID, loopID)
	handler.loopManager.TrackToolName(executionID, "search")
	handler.loopManager.TrackToolOrdinal(executionID, 1)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	bucket := &settlementBucket{
		values: map[string][]byte{loopID: settlementLoopRecord(t, entity)},
		putErr: errors.New("loop state unavailable"),
	}
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: callID, Name: "search"}}},
	}
	c := releaseTestComponent(t, handler)
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
		response:      retainedLoopMessage{subject: "agent.response." + requestID, data: settlementEnvelope(t, response)},
		responseFound: true,
	}
	result := &agentic.ToolResult{
		RequestID: requestID, CallID: callID, ExecutionID: executionID,
		CallOrdinal: 1, Name: "search", Content: "answer",
	}
	data := settlementEnvelope(t, result)

	decision, err := c.handleToolResultMessage(t.Context(), data)
	require.ErrorIs(t, err, bucket.putErr)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "failed tool attempt retained consumed execution routing")

	bucket.putErr = nil
	decision, err = c.handleToolResultMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestProcessLocalTerminalResponseWaitsForDurableFinalMarker(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	handler := NewMessageHandler(DefaultConfig())
	_, err := handler.loopManager.CreateLoopWithID(loopID, "task-1", "general", "model")
	require.NoError(t, err)
	handler.loopManager.TrackRequest(requestID, loopID)
	require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))

	durable := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	c := releaseTestComponent(t, handler)
	c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, durable)}}
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "concurrent duplicate"},
	}

	decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))

	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestWarmTerminalResponseRequiresCurrentRetainedRequest(t *testing.T) {
	loopID := uuid.NewString()
	oldRequestID := loopID + ":req:" + uuid.NewString()
	currentRequestID := loopID + ":req:" + uuid.NewString()
	durable := agentic.NewLoopEntity(loopID, "task-1", "general", "model", 3)
	durable.State = agentic.LoopStateComplete
	durable.Outcome = agentic.OutcomeSuccess

	newComponent := func(t *testing.T) *Component {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		_, err := handler.loopManager.CreateLoopWithID(loopID, durable.TaskID, durable.Role, durable.Model)
		require.NoError(t, err)
		handler.loopManager.TrackRequest(oldRequestID, loopID)
		handler.loopManager.TrackRequest(currentRequestID, loopID)
		require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		c := releaseTestComponent(t, handler)
		c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, durable)}}
		c.settlementEvidence = &settlementEvidence{
			request: retainedRequest(t, loopID, currentRequestID), requestFound: true,
		}
		return c
	}

	t.Run("historical mapped request quarantines", func(t *testing.T) {
		response := &agentic.AgentResponse{RequestID: oldRequestID, Status: agentic.StatusComplete}
		decision, err := newComponent(t).handleResponseMessage(t.Context(), settlementEnvelope(t, response))
		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	})

	t.Run("current mapped request is proven applied", func(t *testing.T) {
		response := &agentic.AgentResponse{RequestID: currentRequestID, Status: agentic.StatusComplete}
		decision, err := newComponent(t).handleResponseMessage(t.Context(), settlementEnvelope(t, response))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	})
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestWarmNonterminalResponseRequiresCurrentRetainedRequestBeforeMutation(t *testing.T) {
	loopID := uuid.NewString()
	oldRequestID := loopID + ":req:" + uuid.NewString()
	currentRequestID := loopID + ":req:" + uuid.NewString()
	handler := NewMessageHandler(DefaultConfig())
	_, err := handler.loopManager.CreateLoopWithID(loopID, "task-1", "general", "model", 3)
	require.NoError(t, err)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	handler.loopManager.TrackRequest(oldRequestID, loopID)
	handler.loopManager.TrackRequest(currentRequestID, loopID)
	_, err = handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)

	c := releaseTestComponent(t, handler)
	c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, currentRequestID), requestFound: true,
	}
	contextBefore := handler.loopManager.GetContextManager(loopID).GetContext()
	trajectoryBefore, err := handler.trajectoryManager.getTrajectory(loopID)
	require.NoError(t, err)
	entityBefore, err := handler.GetLoop(loopID)
	require.NoError(t, err)

	stale := &agentic.AgentResponse{
		RequestID: oldRequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "stale"},
	}
	decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, stale))
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)

	entityAfter, getErr := handler.GetLoop(loopID)
	require.NoError(t, getErr)
	require.Equal(t, entityBefore, entityAfter, "stale response mutated warm loop state")
	require.Equal(t, contextBefore, handler.loopManager.GetContextManager(loopID).GetContext(),
		"stale response mutated warm loop context")
	trajectoryAfter, trajectoryErr := handler.trajectoryManager.getTrajectory(loopID)
	require.NoError(t, trajectoryErr)
	require.Equal(t, trajectoryBefore, trajectoryAfter, "stale response mutated warm loop trajectory")
	require.NotContains(t, c.loopsBucket.(*settlementBucket).values, "COMPLETE_"+loopID)

	current := &agentic.AgentResponse{
		RequestID: currentRequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "current"},
	}
	decision, err = c.handleResponseMessage(t.Context(), settlementEnvelope(t, current))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Contains(t, c.loopsBucket.(*settlementBucket).values, "COMPLETE_"+loopID,
		"current retained response did not proceed through durable settlement")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestResponseTerminalSignalsFollowFinalMarkerAndDoNotRepeatForAppliedRedelivery(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	handler := NewMessageHandler(DefaultConfig())
	_, err := handler.loopManager.CreateLoopWithID(loopID, "task-1", "general", "model", 3)
	require.NoError(t, err)
	handler.loopManager.TrackRequest(requestID, loopID)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)

	bucket := &settlementBucket{
		values:     map[string][]byte{loopID: settlementLoopRecord(t, entity)},
		failPutKey: loopID, failPutLeft: 1,
	}
	c := releaseTestComponent(t, handler)
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{
		request: retainedRequest(t, loopID, requestID), requestFound: true,
	}
	c.metrics = getMetrics(nil)
	// getMetrics is process-wide: assert exact delivery deltas without resetting
	// counters already advanced by other tests or previous -count iterations.
	completedBefore := testutil.ToFloat64(c.metrics.loopsCompleted)
	activeBefore := testutil.ToFloat64(c.metrics.activeLoops)
	c.metrics.recordLoopCreated()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c.logger = logger
	handler.logger = logger
	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
	}
	data := settlementEnvelope(t, response)

	decision, err := c.handleResponseMessage(t.Context(), data)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Equal(t, completedBefore, testutil.ToFloat64(c.metrics.loopsCompleted),
		"failed final marker emitted a positive completion signal")
	require.Equal(t, activeBefore+1, testutil.ToFloat64(c.metrics.activeLoops),
		"failed final marker decremented active loops")
	require.NotContains(t, logs.String(), "Loop completed")

	decision, err = c.handleResponseMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, completedBefore+1, testutil.ToFloat64(c.metrics.loopsCompleted))
	require.Equal(t, activeBefore, testutil.ToFloat64(c.metrics.activeLoops))
	require.Equal(t, 1, strings.Count(logs.String(), "Loop completed"))

	decision, err = c.handleResponseMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, completedBefore+1, testutil.ToFloat64(c.metrics.loopsCompleted),
		"redelivery of an applied terminal marker repeated completion signal")
	require.Equal(t, activeBefore, testutil.ToFloat64(c.metrics.activeLoops))
	require.Equal(t, 1, strings.Count(logs.String(), "Loop completed"))
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestFailureTerminalSignalsFollowFinalMarker(t *testing.T) {
	loopID := uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-failure", "general", "model", 3)
	bucket := &settlementBucket{
		values:     map[string][]byte{loopID: settlementLoopRecord(t, entity)},
		failPutKey: loopID, failPutLeft: 1,
	}
	metrics := getMetrics(nil)
	failedBefore := testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("provider_failure"))
	activeBefore := testutil.ToFloat64(metrics.activeLoops)
	metrics.recordLoopCreated()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	newProcess := func(t *testing.T) *Component {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		_, err := handler.loopManager.CreateLoopWithID(loopID, entity.TaskID, entity.Role, entity.Model, entity.MaxIterations)
		require.NoError(t, err)
		handler.logger = logger
		c := releaseTestComponent(t, handler)
		c.loopsBucket = bucket
		c.metrics = metrics
		c.logger = logger
		return c
	}

	first := newProcess(t)
	firstEntity, err := first.handler.GetLoop(loopID)
	require.NoError(t, err)
	err = first.handleLoopFailure(t.Context(), loopID, firstEntity, "provider_failure", errors.New("provider unavailable"))
	require.Error(t, err)
	require.Equal(t, failedBefore, testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("provider_failure")))
	require.Equal(t, activeBefore+1, testutil.ToFloat64(metrics.activeLoops))
	require.NotContains(t, logs.String(), "Loop processing failed")

	second := newProcess(t)
	secondEntity, err := second.handler.GetLoop(loopID)
	require.NoError(t, err)
	require.NoError(t, second.handleLoopFailure(
		t.Context(), loopID, secondEntity, "provider_failure", errors.New("provider unavailable"),
	))
	require.Equal(t, failedBefore+1, testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("provider_failure")))
	require.Equal(t, activeBefore, testutil.ToFloat64(metrics.activeLoops))
	require.Equal(t, 1, strings.Count(logs.String(), "Loop processing failed"))
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalToolSignalsFollowFinalMarker(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	callID := "call-timeout"
	executionID := deriveToolExecutionID(requestID, callID, 1)
	entity := agentic.NewLoopEntity(loopID, "task-tool-timeout", "general", "model", 3)
	entity.TimeoutAt = time.Now().Add(-time.Second)
	bucket := &settlementBucket{
		values:     map[string][]byte{loopID: settlementLoopRecord(t, entity)},
		failPutKey: loopID, failPutLeft: 1,
	}
	metrics := getMetrics(nil)
	failedBefore := testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("timeout"))
	activeBefore := testutil.ToFloat64(metrics.activeLoops)
	metrics.recordLoopCreated()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	newProcess := func(t *testing.T) *Component {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		_, err := handler.loopManager.CreateLoopWithID(loopID, entity.TaskID, entity.Role, entity.Model, entity.MaxIterations)
		require.NoError(t, err)
		require.NoError(t, handler.UpdateLoop(entity))
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		handler.loopManager.TrackToolCall(executionID, loopID)
		handler.loopManager.TrackToolName(executionID, "search")
		handler.loopManager.TrackToolOrdinal(executionID, 1)
		handler.logger = logger
		c := releaseTestComponent(t, handler)
		c.loopsBucket = bucket
		c.metrics = metrics
		c.logger = logger
		return c
	}
	result := &agentic.ToolResult{
		LoopID: loopID, RequestID: requestID, ExecutionID: executionID,
		CallID: callID, CallOrdinal: 1, Name: "search", Content: "late",
	}
	data := settlementEnvelope(t, result)

	decision, err := newProcess(t).handleToolResultMessage(t.Context(), data)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Equal(t, failedBefore, testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("timeout")))
	require.Equal(t, activeBefore+1, testutil.ToFloat64(metrics.activeLoops))
	require.NotContains(t, logs.String(), "Loop failed")

	decision, err = newProcess(t).handleToolResultMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, failedBefore+1, testutil.ToFloat64(metrics.loopsFailed.WithLabelValues("timeout")))
	require.Equal(t, activeBefore, testutil.ToFloat64(metrics.activeLoops))
	require.Equal(t, 1, strings.Count(logs.String(), "Loop failed"))
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestWarmToolResultRequiresExactExecutionCorrelationBeforeMutation(t *testing.T) {
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	callA := agentic.ToolCall{ID: "call-a", Name: "search"}
	callB := agentic.ToolCall{ID: "call-b", Name: "fetch"}
	calls := []agentic.ToolCall{callA, callB}
	require.NoError(t, stampToolExecutionCorrelation(requestID, calls))

	newComponent := func(t *testing.T) (*Component, *MessageHandler) {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		_, err := handler.loopManager.CreateLoopWithID(loopID, "task-1", "general", "model", 3)
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		for _, call := range calls {
			require.NoError(t, handler.loopManager.AddPendingTool(loopID, call.ID))
			handler.loopManager.TrackToolCall(call.ExecutionID, loopID)
			handler.loopManager.TrackToolName(call.ExecutionID, call.Name)
			handler.loopManager.TrackToolOrdinal(call.ExecutionID, call.CallOrdinal)
		}
		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		c := releaseTestComponent(t, handler)
		c.loopsBucket = &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
		return c, handler
	}

	t.Run("execution and call conflict quarantines without mutation", func(t *testing.T) {
		c, handler := newComponent(t)
		conflict := &agentic.ToolResult{
			LoopID: loopID, RequestID: requestID,
			ExecutionID: calls[0].ExecutionID,
			CallID:      calls[1].ID, CallOrdinal: calls[1].CallOrdinal, Name: calls[1].Name,
			Content: "conflicting",
		}

		decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, conflict))
		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		entity, getErr := handler.GetLoop(loopID)
		require.NoError(t, getErr)
		require.ElementsMatch(t, []string{calls[0].ID, calls[1].ID}, handler.loopManager.GetPendingTools(loopID))
		require.Empty(t, entity.PendingToolResults, "conflicting execution persisted a result")
	})

	t.Run("matching execution proceeds", func(t *testing.T) {
		c, handler := newComponent(t)
		matching := &agentic.ToolResult{
			LoopID: loopID, RequestID: requestID,
			ExecutionID: calls[0].ExecutionID,
			CallID:      calls[0].ID, CallOrdinal: calls[0].CallOrdinal, Name: calls[0].Name,
			Content: "matched",
		}

		decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, matching))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Equal(t, []string{calls[1].ID}, handler.loopManager.GetPendingTools(loopID))
		entity, getErr := handler.GetLoop(loopID)
		require.NoError(t, getErr)
		require.Equal(t, "matched", entity.PendingToolResults[calls[0].ExecutionID].Content)
	})
}
