//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/google/uuid"
)

var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

func TestTrajectoryFactBucketIsImmutableHistoryWithoutTTL(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	raw, err := json.Marshal(map[string]any{
		"consumer_name_suffix": "trajectory-fact-bucket-contract",
	})
	require.NoError(t, err)
	discoverable, err := agenticloop.NewComponent(raw, component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	lifecycle := discoverable.(component.LifecycleComponent)
	require.NoError(t, lifecycle.Start(context.Background()))
	t.Cleanup(func() { _ = lifecycle.Stop(context.Background()) })

	js, err := natsClient.JetStream()
	require.NoError(t, err)
	bucket, err := js.KeyValue(context.Background(), agentic.TrajectoryBucketName)
	require.NoError(t, err)
	status, err := bucket.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), status.History())
	assert.Zero(t, status.TTL())
}

func TestExistingIncompatibleTrajectoryBucketDisablesAuditAndDegradesHealth(t *testing.T) {
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithKVBuckets("AGENT_LOOPS"),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}}),
		natsclient.WithTestTimeout(5*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testClient.Terminate() })
	natsClient := testClient.Client
	ctx := context.Background()
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: agentic.TrajectoryBucketName, History: 2,
	})
	require.NoError(t, err)

	raw, err := json.Marshal(map[string]any{
		"consumer_name_suffix": "trajectory-incompatible-contract",
	})
	require.NoError(t, err)
	discoverable, err := agenticloop.NewComponent(raw, component.Dependencies{
		NATSClient: natsClient, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	lifecycle := discoverable.(component.LifecycleComponent)
	require.NoError(t, lifecycle.Start(ctx))
	t.Cleanup(func() { _ = lifecycle.Stop(context.Background()) })

	health := lifecycle.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "degraded", health.Status)
	require.Contains(t, strings.ToLower(health.LastError), "wipe")
	// Loop instance tokens are framework-minted canonical UUIDs (ADR-105, #1192);
	// these tasks travel the production BaseMessage wire, which validates them.
	loopID := uuid.NewString()
	publishTaskMessage(t, natsClient, "agent.task.test", &agentic.TaskMessage{
		LoopID: loopID, TaskID: "task_incompatible_trajectory_bucket",
		Role: "general", Model: "test-model", Prompt: "continue useful work",
	})
	loops, err := js.KeyValue(ctx, "AGENT_LOOPS")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, getErr := loops.Get(ctx, loopID)
		return getErr == nil
	}, 5*time.Second, 25*time.Millisecond, "useful task state should persist despite incompatible audit bucket")
	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	require.Zero(t, status.Values(), "incompatible bucket must receive no audit writes")
}

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
// fullCycleLoopID is the loop this test's request assertion matches on. A loop
// instance token is a framework-minted canonical UUID (ADR-105, #1192).
var fullCycleLoopID = uuid.NewString()

func TestIntegration_LoopFullCycle(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
				{
					Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}},
				},
				{
					Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

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
		LoopID: fullCycleLoopID,
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
		assert.Equal(t, fullCycleLoopID, req.LoopID)
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
					Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
				{
					Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}},
				},
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.>"}},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}},
				},
				{
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.*"}},
				},
				{
					Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

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
		LoopID: uuid.NewString(),
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
	require.NotEmpty(t, receivedToolCalls, "Should publish tool call")
	call := receivedToolCalls[0]
	toolMu.Unlock()
	assert.Equal(t, "call_001", call.ID)
	assert.Equal(t, "read_file", call.Name)

	// Simulate tool result
	toolResult := &agentic.ToolResult{
		CallID:      call.ID,
		RequestID:   call.RequestID,
		ExecutionID: call.ExecutionID,
		CallOrdinal: call.CallOrdinal,
		Content:     "file contents",
	}
	publishToolResultMessage(t, natsClient, "tool.result."+call.ExecutionID, toolResult)

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
					Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
				{
					Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}},
				},
				{
					Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

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
		LoopID: uuid.NewString(),
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
					Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
				{
					Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	kv, err := js.KeyValue(ctx, "AGENT_LOOPS")
	require.NoError(t, err)

	// Publish task
	loopID := uuid.NewString()
	watcher, err := kv.Watch(ctx, loopID, jetstream.UpdatesOnly())
	require.NoError(t, err)
	defer watcher.Stop()

	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task_persist",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test persistence",
	}
	publishTaskMessage(t, natsClient, "agent.task.persist", task)

	var entry jetstream.KeyValueEntry
	select {
	case entry = <-watcher.Updates():
		require.NotNil(t, entry, "Loop entity should be persisted in KV")
	case <-ctx.Done():
		t.Fatalf("timed out waiting for loop entity persistence: %v", ctx.Err())
	}

	var entity agentic.LoopEntity
	err = json.Unmarshal(entry.Value(), &entity)
	require.NoError(t, err)

	assert.Equal(t, loopID, entity.ID)
	assert.Equal(t, "task_persist", entity.TaskID)
	assert.Equal(t, "general", entity.Role)
	assert.Equal(t, "test-model", entity.Model)
}

// TestIntegration_LoopTrajectoryCapture tests that durable observations are readable on completion.
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
					Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true,
				},
				{
					Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}},
				},
				{
					Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

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
	loopID := uuid.NewString()
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

	// Verify trajectory via the declared NATS query handler. The response is
	// reconstructed from immutable KV facts, not process memory.
	trajReq, err := json.Marshal(map[string]string{"loopId": loopID})
	require.NoError(t, err)

	trajResp, err := natsClient.Request(ctx, "agentic.query.trajectory", trajReq, 5*time.Second)
	require.NoError(t, err, "Trajectory should be available via query handler")

	var trajectory struct {
		LoopID           string `json:"loop_id"`
		Coverage         string `json:"coverage"`
		TerminalObserved bool   `json:"terminal_observed"`
		ObservedTotals   struct {
			Facts                uint64 `json:"facts"`
			TokensIn             uint64 `json:"tokens_in"`
			TokensOut            uint64 `json:"tokens_out"`
			TerminalObservations uint64 `json:"terminal_observations"`
		} `json:"observed_totals"`
		Facts []agentic.TrajectoryFactV1 `json:"facts"`
	}
	err = json.Unmarshal(trajResp, &trajectory)
	require.NoError(t, err)

	assert.Equal(t, loopID, trajectory.LoopID)
	assert.Equal(t, "observed", trajectory.Coverage)
	assert.True(t, trajectory.TerminalObserved)
	assert.Greater(t, trajectory.ObservedTotals.Facts, uint64(0))
	assert.Equal(t, uint64(1), trajectory.ObservedTotals.TerminalObservations)
	assert.Greater(t, len(trajectory.Facts), 0, "trajectory should expose visible facts")
}
