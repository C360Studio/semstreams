//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up shared NATS container for all loop integration tests
func TestMain(m *testing.M) {
	streams := []natsclient.TestStreamConfig{
		{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
	}

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithKVBuckets("AGENT_LOOPS"),
		natsclient.WithStreams(streams...),
		natsclient.WithTestTimeout(5*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		panic("Failed to create shared test client: " + err.Error())
	}

	sharedTestClient = testClient
	sharedNATSClient = testClient.Client

	exitCode := m.Run()

	sharedTestClient.Terminate()

	if exitCode != 0 {
		panic("tests failed")
	}
}

func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized")
	}
	return sharedNATSClient
}

// publishTaskMessage publishes a TaskMessage wrapped in a BaseMessage envelope
func publishTaskMessage(t *testing.T, natsClient *natsclient.Client, subject string, task *agentic.TaskMessage) {
	t.Helper()
	baseMsg := message.NewBaseMessage(task.Schema(), task, "integration-test")
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal BaseMessage")
	err = natsClient.PublishToStream(context.Background(), subject, msgData)
	require.NoError(t, err, "Failed to publish task message")
}

// publishResponseMessage publishes an AgentResponse wrapped in a BaseMessage envelope
func publishResponseMessage(t *testing.T, natsClient *natsclient.Client, subject string, response *agentic.AgentResponse) {
	t.Helper()
	baseMsg := message.NewBaseMessage(response.Schema(), response, "integration-test")
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal BaseMessage")
	err = natsClient.PublishToStream(context.Background(), subject, msgData)
	require.NoError(t, err, "Failed to publish response message")
}

// publishToolResultMessage publishes a ToolResult wrapped in a BaseMessage envelope
func publishToolResultMessage(t *testing.T, natsClient *natsclient.Client, subject string, result *agentic.ToolResult) {
	t.Helper()
	baseMsg := message.NewBaseMessage(result.Schema(), result, "integration-test")
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal BaseMessage")
	err = natsClient.PublishToStream(context.Background(), subject, msgData)
	require.NoError(t, err, "Failed to publish tool result message")
}

// TestIntegration_LoopFullCycle tests a complete loop: task → model request → complete
func TestIntegration_LoopFullCycle(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tasks",
					Type:       "jetstream",
					Subject:    "agent.task.*",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:       "responses",
					Type:       "jetstream",
					Subject:    "agent.response.>",
					StreamName: "AGENT",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "requests",
					Type:       "jetstream",
					Subject:    "agent.request.*",
					StreamName: "AGENT",
				},
				{
					Name:       "complete",
					Type:       "jetstream",
					Subject:    "agent.complete.*",
					StreamName: "AGENT",
				},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "fullcycle-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to model requests (extract from BaseMessage envelope)
	receivedRequests := make([]agentic.AgentRequest, 0)
	var requestMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, ok := baseMsg.Payload().(*agentic.AgentRequest); ok {
				requestMu.Lock()
				receivedRequests = append(receivedRequests, *req)
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	// Subscribe to completion events
	receivedComplete := make([]map[string]any, 0)
	var completeMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "agent.complete.>", func(_ context.Context, msg *nats.Msg) {
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err == nil {
			completeMu.Lock()
			receivedComplete = append(receivedComplete, event)
			completeMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish a task
	task := &agentic.TaskMessage{
		LoopID: "loop_001",
		TaskID: "task_001",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Complete this task",
	}
	publishTaskMessage(t, natsClient, "agent.task.test", task)

	time.Sleep(500 * time.Millisecond)

	// Verify model request was published
	requestMu.Lock()
	assert.Greater(t, len(receivedRequests), 0, "Should publish model request")
	if len(receivedRequests) > 0 {
		req := receivedRequests[0]
		assert.Equal(t, "loop_001", req.LoopID)
		assert.Equal(t, "general", req.Role)
	}
	requestMu.Unlock()

	// Simulate model response (complete)
	response := &agentic.AgentResponse{
		RequestID: receivedRequests[0].RequestID,
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Task completed",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}
	publishResponseMessage(t, natsClient, "agent.response."+response.RequestID, response)

	time.Sleep(500 * time.Millisecond)

	// Verify completion event was published
	completeMu.Lock()
	defer completeMu.Unlock()

	assert.Greater(t, len(receivedComplete), 0, "Should publish completion event")
}

// TestIntegration_LoopWithToolCalls tests loop with tool call handling
func TestIntegration_LoopWithToolCalls(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tasks",
					Type:       "jetstream",
					Subject:    "agent.task.*",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:       "responses",
					Type:       "jetstream",
					Subject:    "agent.response.>",
					StreamName: "AGENT",
				},
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.>",
					StreamName: "AGENT",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "requests",
					Type:       "jetstream",
					Subject:    "agent.request.*",
					StreamName: "AGENT",
				},
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.*",
					StreamName: "AGENT",
				},
				{
					Name:       "complete",
					Type:       "jetstream",
					Subject:    "agent.complete.*",
					StreamName: "AGENT",
				},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "toolcalls-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Track model requests (extract from BaseMessage envelope)
	var currentRequestID string
	var requestMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, ok := baseMsg.Payload().(*agentic.AgentRequest); ok {
				requestMu.Lock()
				currentRequestID = req.RequestID
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	// Track tool calls (extract from BaseMessage envelope)
	receivedToolCalls := make([]agentic.ToolCall, 0)
	var toolMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.execute.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if call, ok := baseMsg.Payload().(*agentic.ToolCall); ok {
				toolMu.Lock()
				receivedToolCalls = append(receivedToolCalls, *call)
				toolMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish task
	task := &agentic.TaskMessage{
		LoopID: "loop_tool_001",
		TaskID: "task_tool_001",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Use tools to complete",
	}
	publishTaskMessage(t, natsClient, "agent.task.tool", task)

	time.Sleep(500 * time.Millisecond)

	// Get the request ID
	requestMu.Lock()
	reqID := currentRequestID
	requestMu.Unlock()

	// Simulate model response with tool calls
	response := &agentic.AgentResponse{
		RequestID: reqID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "",
			ToolCalls: []agentic.ToolCall{
				{
					ID:   "call_001",
					Name: "read_file",
					Arguments: map[string]any{
						"path": "test.go",
					},
				},
			},
		},
	}
	publishResponseMessage(t, natsClient, "agent.response."+reqID, response)

	time.Sleep(500 * time.Millisecond)

	// Verify tool call was published
	toolMu.Lock()
	assert.Greater(t, len(receivedToolCalls), 0, "Should publish tool call")
	if len(receivedToolCalls) > 0 {
		call := receivedToolCalls[0]
		assert.Equal(t, "call_001", call.ID)
		assert.Equal(t, "read_file", call.Name)
	}
	callID := receivedToolCalls[0].ID
	toolMu.Unlock()

	// Simulate tool result
	toolResult := &agentic.ToolResult{
		CallID:  callID,
		Content: "file contents",
	}
	publishToolResultMessage(t, natsClient, "tool.result."+callID, toolResult)

	time.Sleep(500 * time.Millisecond)

	// Loop should publish another model request with tool result
	requestMu.Lock()
	newReqID := currentRequestID
	requestMu.Unlock()

	assert.NotEqual(t, reqID, newReqID, "Should publish new request after tool result")
}

// TestIntegration_LoopMaxIterations tests that loop fails after max iterations
func TestIntegration_LoopMaxIterations(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tasks",
					Type:       "jetstream",
					Subject:    "agent.task.*",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:       "responses",
					Type:       "jetstream",
					Subject:    "agent.response.>",
					StreamName: "AGENT",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "requests",
					Type:       "jetstream",
					Subject:    "agent.request.*",
					StreamName: "AGENT",
				},
				{
					Name:       "complete",
					Type:       "jetstream",
					Subject:    "agent.complete.*",
					StreamName: "AGENT",
				},
			},
		},
		MaxIterations:      3, // Low limit for testing
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "maxiter-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Track requests to count iterations (extract from BaseMessage envelope)
	requestCount := 0
	var requestMu sync.Mutex
	var lastRequestID string

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, ok := baseMsg.Payload().(*agentic.AgentRequest); ok {
				requestMu.Lock()
				requestCount++
				lastRequestID = req.RequestID
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	// Track completion events
	receivedComplete := make([]map[string]any, 0)
	var completeMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "agent.complete.>", func(_ context.Context, msg *nats.Msg) {
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err == nil {
			completeMu.Lock()
			receivedComplete = append(receivedComplete, event)
			completeMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish task
	task := &agentic.TaskMessage{
		LoopID: "loop_max_iter",
		TaskID: "task_max_iter",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Never-ending task",
	}
	publishTaskMessage(t, natsClient, "agent.task.maxiter", task)

	// Simulate continuous tool calls to trigger max iterations
	for i := 0; i < 5; i++ {
		time.Sleep(500 * time.Millisecond)

		requestMu.Lock()
		reqID := lastRequestID
		requestMu.Unlock()

		if reqID == "" {
			continue
		}

		// Always respond with tool call to keep iterating
		response := &agentic.AgentResponse{
			RequestID: reqID,
			Status:    "tool_call",
			Message: agentic.ChatMessage{
				Role:    "assistant",
				Content: "",
				ToolCalls: []agentic.ToolCall{
					{
						ID:   "call_" + string(rune(i)),
						Name: "dummy_tool",
					},
				},
			},
		}
		publishResponseMessage(t, natsClient, "agent.response."+reqID, response)
	}

	time.Sleep(1 * time.Second)

	// Verify loop stopped at max iterations
	requestMu.Lock()
	count := requestCount
	requestMu.Unlock()

	assert.LessOrEqual(t, count, 3, "Should not exceed max iterations")
}

// TestIntegration_LoopStatePersistence tests that LoopEntity is saved to KV
func TestIntegration_LoopStatePersistence(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tasks",
					Type:       "jetstream",
					Subject:    "agent.task.*",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:       "responses",
					Type:       "jetstream",
					Subject:    "agent.response.>",
					StreamName: "AGENT",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "requests",
					Type:       "jetstream",
					Subject:    "agent.request.*",
					StreamName: "AGENT",
				},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "persist-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Publish task
	loopID := "loop_persist_" + time.Now().Format("150405")
	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task_persist",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test persistence",
	}
	publishTaskMessage(t, natsClient, "agent.task.persist", task)

	time.Sleep(500 * time.Millisecond)

	// Verify loop entity exists in KV
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	kv, err := js.KeyValue(ctx, "AGENT_LOOPS")
	require.NoError(t, err)

	entry, err := kv.Get(ctx, loopID)
	require.NoError(t, err, "Loop entity should be persisted in KV")

	var entity agentic.LoopEntity
	err = json.Unmarshal(entry.Value(), &entity)
	require.NoError(t, err)

	assert.Equal(t, loopID, entity.ID)
	assert.Equal(t, "task_persist", entity.TaskID)
	assert.Equal(t, "general", entity.Role)
	assert.Equal(t, "test-model", entity.Model)
}

// TestIntegration_LoopTrajectoryCapture tests that trajectory is saved on completion.
// Uses its own NATS client to avoid query handler conflicts with other test components.
func TestIntegration_LoopTrajectoryCapture(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup(), natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}}),
		natsclient.WithKV(), natsclient.WithKVBuckets("AGENT_LOOPS"))
	natsClient := tc.Client

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tasks",
					Type:       "jetstream",
					Subject:    "agent.task.*",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:       "responses",
					Type:       "jetstream",
					Subject:    "agent.response.>",
					StreamName: "AGENT",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "requests",
					Type:       "jetstream",
					Subject:    "agent.request.*",
					StreamName: "AGENT",
				},
				{
					Name:       "complete",
					Type:       "jetstream",
					Subject:    "agent.complete.*",
					StreamName: "AGENT",
				},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "trajectory-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Track request ID (extract from BaseMessage envelope)
	var requestID string
	var requestMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, ok := baseMsg.Payload().(*agentic.AgentRequest); ok {
				requestMu.Lock()
				requestID = req.RequestID
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish task
	loopID := "loop_traj_" + time.Now().Format("150405")
	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task_trajectory",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test trajectory",
	}
	publishTaskMessage(t, natsClient, "agent.task.traj", task)

	time.Sleep(500 * time.Millisecond)

	// Get request ID
	requestMu.Lock()
	reqID := requestID
	requestMu.Unlock()

	// Simulate complete response
	response := &agentic.AgentResponse{
		RequestID: reqID,
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Task completed",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     200,
			CompletionTokens: 100,
		},
	}
	publishResponseMessage(t, natsClient, "agent.response."+reqID, response)

	time.Sleep(1 * time.Second)

	// Verify trajectory via NATS query handler (served from TTLCache)
	trajReq, err := json.Marshal(map[string]string{"loopId": loopID})
	require.NoError(t, err)

	trajResp, err := natsClient.Request(ctx, "agentic.query.trajectory", trajReq, 5*time.Second)
	require.NoError(t, err, "Trajectory should be available via query handler")

	var trajectory agentic.Trajectory
	err = json.Unmarshal(trajResp, &trajectory)
	require.NoError(t, err)

	assert.Equal(t, loopID, trajectory.LoopID)
	assert.NotNil(t, trajectory.EndTime, "Trajectory should be completed")
	assert.Equal(t, "complete", trajectory.Outcome)
	assert.Greater(t, len(trajectory.Steps), 0, "Trajectory should have steps")
}
