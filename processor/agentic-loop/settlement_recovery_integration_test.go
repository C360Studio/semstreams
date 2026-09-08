//go:build integration

package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationTaskAndResponseSettleAcrossProcessReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	bucket, err := tc.CreateKVBucket(ctx, "AGENT_LOOPS_TASK4_REPLACEMENT")
	require.NoError(t, err)
	registry := payloadbuiltins.NewTestRegistry(t)
	newProcess := func() *Component {
		discoverable, componentErr := NewComponent([]byte(`{}`), component.Dependencies{
			NATSClient: tc.Client, PayloadRegistry: registry,
		})
		require.NoError(t, componentErr)
		c := discoverable.(*Component)
		c.loopsBucket = bucket
		c.graphWriter = nil
		return c
	}

	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	entity := agentic.NewLoopEntity(loopID, "task-replacement", "general", "model", 3)
	entityData, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, loopID, entityData)
	require.NoError(t, err)

	request := &agentic.AgentRequest{
		RequestID: requestID, LoopID: loopID, Role: entity.Role, Model: entity.Model,
		Messages: []agentic.ChatMessage{{Role: "user", Content: "work"}},
	}
	requestData, err := json.Marshal(message.NewBaseMessage(request.Schema(), request, "test"))
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.request."+loopID, requestData))

	task := &agentic.TaskMessage{
		LoopID: loopID, TaskID: entity.TaskID, Role: entity.Role, Model: entity.Model, Prompt: "work",
	}
	taskData, err := json.Marshal(message.NewBaseMessage(task.Schema(), task, "test"))
	require.NoError(t, err)
	taskProcess := newProcess()
	taskDecision, err := taskProcess.handleTaskMessage(ctx, taskData)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, taskDecision)
	gotLoopID, found := taskProcess.handler.loopManager.GetLoopForRequest(requestID)
	require.True(t, found)
	require.Equal(t, loopID, gotLoopID)

	response := &agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
	}
	responseData, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	responseDecision, err := newProcess().handleResponseMessage(ctx, responseData)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, responseDecision)

	completionEntry, err := bucket.Get(ctx, "COMPLETE_"+loopID)
	require.NoError(t, err, "response ACKed before its durable terminal state committed")
	var completion agentic.LoopCompletedEvent
	require.NoError(t, json.Unmarshal(completionEntry.Value(), &completion))
	require.Equal(t, "done", completion.Result)
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	_, err = stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
	require.NoError(t, err, "response ACKed before its terminal publication received PubAck")
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestIntegrationWarmResponseUsesCurrentRetainedRequestAsAuthority(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	))
	bucket, err := tc.CreateKVBucket(ctx, "AGENT_LOOPS_TASK4_WARM_RESPONSE")
	require.NoError(t, err)
	registry := payloadbuiltins.NewTestRegistry(t)
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: tc.Client, PayloadRegistry: registry,
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.loopsBucket = bucket
	c.graphWriter = nil

	loopID := uuid.NewString()
	oldRequestID := loopID + ":req:" + uuid.NewString()
	currentRequestID := loopID + ":req:" + uuid.NewString()
	_, err = c.handler.loopManager.CreateLoopWithID(loopID, "task-warm-response", "general", "model", 3)
	require.NoError(t, err)
	c.handler.loopManager.TrackRequest(oldRequestID, loopID)
	c.handler.loopManager.TrackRequest(currentRequestID, loopID)
	_, err = c.handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	entity, err := c.handler.GetLoop(loopID)
	require.NoError(t, err)
	entityData, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, loopID, entityData)
	require.NoError(t, err)

	publishRequest := func(requestID string) {
		t.Helper()
		request := &agentic.AgentRequest{
			RequestID: requestID, LoopID: loopID, Role: entity.Role, Model: entity.Model,
			Messages: []agentic.ChatMessage{{Role: "user", Content: "work"}},
		}
		data, marshalErr := json.Marshal(message.NewBaseMessage(request.Schema(), request, "test"))
		require.NoError(t, marshalErr)
		require.NoError(t, tc.Client.PublishToStream(ctx, "agent.request."+loopID, data))
	}
	publishRequest(oldRequestID)
	publishRequest(currentRequestID)

	stale := &agentic.AgentResponse{
		RequestID: oldRequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "stale"},
	}
	decision, err := c.handleResponseMessage(ctx, settlementEnvelope(t, stale))
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	warm, err := c.handler.GetLoop(loopID)
	require.NoError(t, err)
	require.False(t, warm.State.IsTerminal(), "historical response mutated warm loop before quarantine")
	_, err = bucket.Get(ctx, "COMPLETE_"+loopID)
	require.ErrorIs(t, err, jetstream.ErrKeyNotFound)

	current := &agentic.AgentResponse{
		RequestID: currentRequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "current"},
	}
	decision, err = c.handleResponseMessage(ctx, settlementEnvelope(t, current))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	_, err = bucket.Get(ctx, "COMPLETE_"+loopID)
	require.NoError(t, err, "current retained response did not reach durable terminal settlement")
}
