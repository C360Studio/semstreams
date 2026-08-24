//go:build integration

package agentictools_test

import (
	"context"
	"encoding/json"
	"sort"
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
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
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
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
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

// TestIntegration_AdvertisedToolsEnforced drives the gh#551 acceptance case
// through the PRODUCTION wire (JetStream consumer → production decoder →
// handleToolCall → publishResult): global AllowedTools includes both "decide"
// and "create_change"; a call for "create_change" carrying an advertised set
// of only ["decide"] (stamped as []string, decoded off the wire as []any)
// must be rejected with the per-loop error text — distinct from the global
// "not allowed" rejection — while "decide" under the same metadata executes.
func TestIntegration_AdvertisedToolsEnforced(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "advertised-test",
		AllowedTools:       []string{"decide", "create_change"},
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)
	for _, name := range []string{"decide", "create_change"} {
		require.NoError(t, toolsComp.RegisterToolExecutor(&integrationMockExecutor{
			toolName:      name,
			resultContent: name + " executed",
		}))
	}

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)
	require.NoError(t, lc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, lc.Start(ctx))
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	received := make(map[string]agentic.ToolResult)
	var receiveMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				receiveMu.Lock()
				received[result.CallID] = *result
				receiveMu.Unlock()
			}
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// The loop advertised only "decide". Stamp as []string — the wire
	// round-trip delivers []any to handleToolCall, the production shape.
	advertised := map[string]any{
		agentic.MetadataKeyAdvertisedTools: []string{"decide"},
	}

	publishToolCallMessage(t, natsClient, "tool.execute.create_change", &agentic.ToolCall{
		ID:       "call_unadvertised",
		Name:     "create_change",
		LoopID:   "loop-adv-int",
		Metadata: advertised,
	})
	publishToolCallMessage(t, natsClient, "tool.execute.decide", &agentic.ToolCall{
		ID:       "call_advertised",
		Name:     "decide",
		LoopID:   "loop-adv-int",
		Metadata: advertised,
	})

	require.Eventually(t, func() bool {
		receiveMu.Lock()
		defer receiveMu.Unlock()
		return len(received) == 2
	}, 5*time.Second, 100*time.Millisecond, "expected results for both calls")

	receiveMu.Lock()
	defer receiveMu.Unlock()

	rejected := received["call_unadvertised"]
	assert.NotEmpty(t, rejected.Error, "unadvertised tool must be rejected")
	assert.Contains(t, rejected.Error, "is not permitted for this loop (advertised tool set)")
	assert.NotContains(t, rejected.Error, "is not allowed",
		"per-loop rejection must be distinguishable from the global-allowlist rejection")

	admitted := received["call_advertised"]
	assert.Empty(t, admitted.Error)
	assert.Equal(t, "decide executed", admitted.Content)
}

// TestIntegration_ToolTimeout tests that long-running tools are cancelled
func TestIntegration_ToolTimeout(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
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

// TestIntegration_MultipleToolCallsProduceAllResults proves that each admitted
// call produces its exact correlated result without inferring execution overlap.
func TestIntegration_MultipleToolCallsProduceAllResults(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
				},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "multiple-results-test",
		Timeout:            "5s",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}

	comp, err := agentictools.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	testCases := []struct {
		callID  string
		name    string
		content string
	}{
		{callID: "call_multiple_1", name: "multiple_tool_1", content: "Result 1"},
		{callID: "call_multiple_2", name: "multiple_tool_2", content: "Result 2"},
		{callID: "call_multiple_3", name: "multiple_tool_3", content: "Result 3"},
	}
	expected := make(map[string]string, len(testCases))
	for _, testCase := range testCases {
		expected[testCase.callID] = testCase.content
		err = toolsComp.RegisterToolExecutor(&integrationMockExecutor{
			toolName:      testCase.name,
			resultContent: testCase.content,
		})
		require.NoError(t, err)
	}

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	err = lc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = lc.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		assert.NoError(t, lc.Stop(stopCtx), "stop agentic-tools after multiple-result proof")
	})

	results := make(chan agentic.ToolResult, len(expected))

	dec := payloadbuiltins.NewTestDecoder(t)
	subscription, err := natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if result, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
				results <- *result
			}
		}
	})
	require.NoError(t, err)
	defer subscription.Unsubscribe()

	for index, testCase := range testCases {
		call := &agentic.ToolCall{
			ID:        testCase.callID,
			Name:      testCase.name,
			Arguments: map[string]any{"input": index + 1},
		}
		publishToolCallMessage(t, natsClient, "tool.execute."+call.Name, call)
	}

	livenessCtx, livenessCancel := context.WithTimeout(ctx, 5*time.Second)
	defer livenessCancel()
	received := make(map[string]struct{}, len(expected))
	for len(received) < len(expected) {
		select {
		case result := <-results:
			wantContent, known := expected[result.CallID]
			require.True(t, known, "unexpected tool result call_id %q", result.CallID)
			_, duplicate := received[result.CallID]
			require.False(t, duplicate, "duplicate tool result for call_id %q", result.CallID)
			require.Empty(t, result.Error, "tool result for call_id %q carried an error", result.CallID)
			require.Equal(t, wantContent, result.Content, "tool result content for call_id %q", result.CallID)
			received[result.CallID] = struct{}{}
		case <-livenessCtx.Done():
			missing := make([]string, 0, len(expected)-len(received))
			for callID := range expected {
				if _, ok := received[callID]; !ok {
					missing = append(missing, callID)
				}
			}
			sort.Strings(missing)
			require.FailNow(t, "missing correlated tool results", "missing call_ids=%v: %v", missing, livenessCtx.Err())
		}
	}
}

// TestIntegration_ToolListRequestReply tests tool.list request/reply for tool discovery
func TestIntegration_ToolListRequestReply(t *testing.T) {
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "AGENT", Subjects: []string{"agent.>", "tool.execute.>", "tool.result.>"},
		}),
	)
	natsClient := testClient.Client

	// Use a same-kind custom request subject to prove the typed port remains
	// runtime-configurable without retaining the old default as an alias.
	toolListSubject := "discovery.tool.list.list-req-test"

	config := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
				{
					Name: "tool.list", Config: component.NATSRequestPort{Subject: toolListSubject}, Required: false,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

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

	_, err = natsClient.Request(ctx, "tool.list", []byte("{}"), 100*time.Millisecond)
	require.Error(t, err, "legacy tool.list subject must not retain a fallback responder")
	_, err = natsClient.Request(ctx, "discovery.tool.list", []byte("{}"), 100*time.Millisecond)
	require.Error(t, err, "new default subject must not remain as an alias for a custom override")
}

func TestIntegration_ToolListDefaultDoesNotServeLegacySubject(t *testing.T) {
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "AGENT", Subjects: []string{"agent.>", "tool.execute.>", "tool.result.>"},
		}),
	)
	natsClient := testClient.Client
	config := agentictools.DefaultConfig()
	config.ConsumerNameSuffix = "default-list-req-test"
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := agentictools.NewComponent(rawConfig, component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	lifecycle, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)
	require.NoError(t, lifecycle.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Start(ctx))
	defer lifecycle.Stop(context.Background())

	retryConfig := natsclient.DefaultRetryConfig()
	retryConfig.InitialBackoff = 100 * time.Millisecond
	retryConfig.MaxRetries = 5
	_, err = natsClient.RequestWithRetry(
		ctx, "discovery.tool.list", []byte("{}"), 2*time.Second, retryConfig,
	)
	require.NoError(t, err, "new default discovery subject must have a responder")

	_, err = natsClient.Request(ctx, "tool.list", []byte("{}"), 100*time.Millisecond)
	require.Error(t, err, "old default tool.list subject must have no responder")
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
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		ToolRegistry:    sharedReg,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
					Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}},
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
		NATSClient:      natsClient,
		ToolRegistry:    sharedReg,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
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
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	// Subscribe to tool results
	receivedResults := make([]agentic.ToolResult, 0)
	var receiveMu sync.Mutex

	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
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
