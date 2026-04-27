//go:build integration

package agentictools_test

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
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up shared NATS container for all tools integration tests
func TestMain(m *testing.M) {
	streams := []natsclient.TestStreamConfig{
		{Name: "AGENT", Subjects: []string{"agent.>", "tool.execute.>", "tool.result.>"}},
	}

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
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

// publishToolCallMessage publishes a ToolCall wrapped in a BaseMessage envelope
func publishToolCallMessage(t *testing.T, natsClient *natsclient.Client, subject string, call *agentic.ToolCall) {
	t.Helper()
	baseMsg := message.NewBaseMessage(call.Schema(), call, "integration-test")
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal BaseMessage")
	err = natsClient.PublishToStream(context.Background(), subject, msgData)
	require.NoError(t, err, "Failed to publish tool call message")
}

// integrationMockExecutor implements a simple test tool executor for integration tests
type integrationMockExecutor struct {
	toolName      string
	resultContent string
	delay         time.Duration
}

func (m *integrationMockExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  "execution cancelled",
			}, ctx.Err()
		}
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: m.resultContent,
	}, nil
}

func (m *integrationMockExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        m.toolName,
			Description: "Mock tool for testing",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "string"},
				},
			},
		},
	}
}

// TestIntegration_ToolExecution tests basic tool execution and result publishing
func TestIntegration_ToolExecution(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "exec-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	// Register mock tool
	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	mockTool := &integrationMockExecutor{
		toolName:      "echo",
		resultContent: "Echo result",
		delay:         0,
	}
	err = toolsComp.RegisterToolExecutor(mockTool)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data, &baseMsg); err == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				receivedResults = append(receivedResults, *result)
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish tool call (wrapped in BaseMessage)
	toolCall := &agentic.ToolCall{
		ID:   "call_123",
		Name: "echo",
		Arguments: map[string]any{
			"input": "test",
		},
	}
	publishToolCallMessage(t, natsClient, "tool.execute.echo", toolCall)

	// Wait for result
	time.Sleep(500 * time.Millisecond)

	// Verify result
	receiveMu.Lock()
	defer receiveMu.Unlock()

	require.Equal(t, 1, len(receivedResults), "Should receive one result")
	result := receivedResults[0]
	assert.Equal(t, "call_123", result.CallID)
	assert.Equal(t, "Echo result", result.Content)
	assert.Empty(t, result.Error)
}

// TestIntegration_ToolAllowedList tests that disallowed tools return errors
func TestIntegration_ToolAllowedList(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "allowed-test",
		AllowedTools:       []string{"allowed_tool"}, // Only this tool is allowed
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	// Register two tools: one allowed, one not
	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	allowedTool := &integrationMockExecutor{
		toolName:      "allowed_tool",
		resultContent: "Allowed result",
	}
	blockedTool := &integrationMockExecutor{
		toolName:      "blocked_tool",
		resultContent: "This should not execute",
	}

	err = toolsComp.RegisterToolExecutor(allowedTool)
	require.NoError(t, err)
	err = toolsComp.RegisterToolExecutor(blockedTool)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data, &baseMsg); err == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				receivedResults = append(receivedResults, *result)
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Try to execute blocked tool (wrapped in BaseMessage)
	blockedCall := &agentic.ToolCall{
		ID:   "call_blocked",
		Name: "blocked_tool",
		Arguments: map[string]any{
			"input": "test",
		},
	}
	publishToolCallMessage(t, natsClient, "tool.execute.blocked", blockedCall)

	time.Sleep(500 * time.Millisecond)

	// Verify blocked tool returned error
	receiveMu.Lock()
	require.Equal(t, 1, len(receivedResults), "Should receive one error result")
	result := receivedResults[0]
	receiveMu.Unlock()

	assert.Equal(t, "call_blocked", result.CallID)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "not allowed")
}

// TestIntegration_ToolTimeout tests that long-running tools are cancelled
func TestIntegration_ToolTimeout(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "timeout-test",
		Timeout:            "500ms", // Short timeout
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	// Register slow tool
	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	slowTool := &integrationMockExecutor{
		toolName:      "slow_tool",
		resultContent: "Should timeout",
		delay:         2 * time.Second, // Longer than timeout
	}

	err = toolsComp.RegisterToolExecutor(slowTool)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data, &baseMsg); err == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				receivedResults = append(receivedResults, *result)
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Execute slow tool (wrapped in BaseMessage)
	slowCall := &agentic.ToolCall{
		ID:   "call_slow",
		Name: "slow_tool",
		Arguments: map[string]any{
			"input": "test",
		},
	}
	publishToolCallMessage(t, natsClient, "tool.execute.slow", slowCall)

	// Wait for timeout to occur
	time.Sleep(1 * time.Second)

	// Verify tool execution was cancelled
	receiveMu.Lock()
	defer receiveMu.Unlock()

	require.Equal(t, 1, len(receivedResults), "Should receive timeout result")
	result := receivedResults[0]
	assert.Equal(t, "call_slow", result.CallID)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "cancelled")
}

// TestIntegration_ToolConcurrentExecution tests that multiple tools can execute in parallel
func TestIntegration_ToolConcurrentExecution(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "concurrent-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	// Register multiple tools
	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	tool1 := &integrationMockExecutor{
		toolName:      "tool1",
		resultContent: "Result 1",
		delay:         200 * time.Millisecond,
	}
	tool2 := &integrationMockExecutor{
		toolName:      "tool2",
		resultContent: "Result 2",
		delay:         200 * time.Millisecond,
	}
	tool3 := &integrationMockExecutor{
		toolName:      "tool3",
		resultContent: "Result 3",
		delay:         200 * time.Millisecond,
	}

	err = toolsComp.RegisterToolExecutor(tool1)
	require.NoError(t, err)
	err = toolsComp.RegisterToolExecutor(tool2)
	require.NoError(t, err)
	err = toolsComp.RegisterToolExecutor(tool3)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data, &baseMsg); err == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				receivedResults = append(receivedResults, *result)
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Execute all tools concurrently
	startTime := time.Now()

	calls := []*agentic.ToolCall{
		{ID: "call_1", Name: "tool1", Arguments: map[string]any{"input": "1"}},
		{ID: "call_2", Name: "tool2", Arguments: map[string]any{"input": "2"}},
		{ID: "call_3", Name: "tool3", Arguments: map[string]any{"input": "3"}},
	}

	for _, call := range calls {
		publishToolCallMessage(t, natsClient, "tool.execute."+call.Name, call)
	}

	// Wait for all results - should complete faster than sequential time
	// 3 tools x 200ms each = 600ms sequential, but parallel should be ~200ms + overhead
	require.Eventually(t, func() bool {
		receiveMu.Lock()
		defer receiveMu.Unlock()
		return len(receivedResults) >= 3
	}, 2*time.Second, 50*time.Millisecond, "Should receive all results")

	elapsed := time.Since(startTime)

	// Verify all results received
	receiveMu.Lock()
	resultCount := len(receivedResults)
	receiveMu.Unlock()

	assert.Equal(t, 3, resultCount, "Should receive three results")

	// Verify parallel execution (should be ~200ms + overhead, not 600ms sequential)
	// Allow generous overhead for test infrastructure
	assert.Less(t, elapsed, 800*time.Millisecond, "Tools should execute in parallel (not 600ms+ sequential)")
}

// TestIntegration_ToolListRequestReply tests tool.list request/reply for tool discovery
func TestIntegration_ToolListRequestReply(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Use unique subject to avoid interference from other tests sharing the NATS connection.
	// tool.list uses core NATS request/reply (not JetStream), so multiple subscribers
	// on the same subject would cause unpredictable responses.
	toolListSubject := "tool.list.list-req-test"

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
				{
					Name:     "tool.list",
					Type:     "nats",
					Subject:  toolListSubject,
					Required: false,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "list-req-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	// Register internal tool
	mockTool := &integrationMockExecutor{
		toolName:      "internal_tool",
		resultContent: "Internal result",
	}
	err = toolsComp.RegisterToolExecutor(mockTool)
	require.NoError(t, err)

	// Start component
	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Send tool.list request with retry to handle NATS timing issues
	retryConfig := natsclient.DefaultRetryConfig()
	retryConfig.InitialBackoff = 100 * time.Millisecond
	retryConfig.MaxRetries = 5
	responseData, err := natsClient.RequestWithRetry(ctx, toolListSubject, []byte("{}"), 2*time.Second, retryConfig)
	require.NoError(t, err)

	// Parse response
	var response agentictools.ToolListResponse
	err = json.Unmarshal(responseData, &response)
	require.NoError(t, err)

	// Debug: log what we received
	t.Logf("Raw response: %s", string(responseData))
	t.Logf("Parsed %d tools:", len(response.Tools))
	for _, tool := range response.Tools {
		t.Logf("  - Name=%q Provider=%q Available=%v", tool.Name, tool.Provider, tool.Available)
	}

	// Verify response contains internal tool
	var foundInternalTool bool
	for _, tool := range response.Tools {
		if tool.Name == "internal_tool" {
			foundInternalTool = true
			assert.Equal(t, "internal", tool.Provider)
			assert.True(t, tool.Available)
			break
		}
	}
	assert.True(t, foundInternalTool, "Response should include internal tool")
}

// TestIntegration_SharedRegistryTools tests that tools registered in the
// deps-injected shared registry appear in ListTools alongside per-component
// local registrations.
func TestIntegration_SharedRegistryTools(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	sharedReg := agentictools.NewExecutorRegistry()
	sharedTool := &integrationMockExecutor{
		toolName:      "shared_test_tool",
		resultContent: "Shared result",
	}
	require.NoError(t, sharedReg.RegisterTool("shared_test_tool", sharedTool))

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "shared-reg-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:   natsClient,
		ToolRegistry: sharedReg,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	// Also register a local tool
	localTool := &integrationMockExecutor{
		toolName:      "local_test_tool",
		resultContent: "Local result",
	}
	err = toolsComp.RegisterToolExecutor(localTool)
	require.NoError(t, err)

	// Verify both shared and local tools appear in ListTools
	tools := toolsComp.ListTools()

	var foundShared, foundLocal bool
	for _, tool := range tools {
		if tool.Name == "shared_test_tool" {
			foundShared = true
			assert.Equal(t, "internal", tool.Provider)
			assert.True(t, tool.Available)
		}
		if tool.Name == "local_test_tool" {
			foundLocal = true
			assert.Equal(t, "internal", tool.Provider)
			assert.True(t, tool.Available)
		}
	}

	assert.True(t, foundShared, "Should find shared-registered tool")
	assert.True(t, foundLocal, "Should find locally registered tool")
}

// TestIntegration_SharedRegistryExecution tests that tools resolved via the
// deps-injected shared registry can be executed by the component.
func TestIntegration_SharedRegistryExecution(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	sharedReg := agentictools.NewExecutorRegistry()
	sharedExecTool := &integrationMockExecutor{
		toolName:      "shared_exec_tool",
		resultContent: "Executed from shared registry",
	}
	require.NoError(t, sharedReg.RegisterTool("shared_exec_tool", sharedExecTool))

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:       "tool_calls",
					Type:       "jetstream",
					Subject:    "tool.execute.>",
					StreamName: "AGENT",
					Required:   true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:       "tool_results",
					Type:       "jetstream",
					Subject:    "tool.result.*",
					StreamName: "AGENT",
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "shared-exec-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:   natsClient,
		ToolRegistry: sharedReg,
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data, &baseMsg); err == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				receivedResults = append(receivedResults, *result)
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Execute the shared-registered tool (wrapped in BaseMessage)
	toolCall := &agentic.ToolCall{
		ID:   "call_shared_exec",
		Name: "shared_exec_tool",
		Arguments: map[string]any{
			"input": "test",
		},
	}
	publishToolCallMessage(t, natsClient, "tool.execute.shared_exec_tool", toolCall)

	// Wait for result
	time.Sleep(500 * time.Millisecond)

	// Verify result from shared-registered tool
	receiveMu.Lock()
	defer receiveMu.Unlock()

	require.Equal(t, 1, len(receivedResults), "Should receive one result")
	result := receivedResults[0]
	assert.Equal(t, "call_shared_exec", result.CallID)
	assert.Equal(t, "Executed from shared registry", result.Content)
	assert.Empty(t, result.Error)
}
