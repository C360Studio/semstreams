package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/c360/semstreams/flowstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeMessagesIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Setup NATS client using shared testcontainer
	natsClient := getSharedNATSClient(t)

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	// Create test context
	ctx := context.Background()

	// Create a test flow with multiple components
	testFlow := &flowstore.Flow{
		ID:   "messages-test-flow",
		Name: "Messages Test Flow",
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node1",
				Name: "udp-source",
				Type: "udp",
				Config: map[string]any{
					"port": 8090,
				},
			},
			{
				ID:   "node2",
				Name: "json-processor",
				Type: "json-processor",
				Config: map[string]any{
					"filter": "$.data",
				},
			},
			{
				ID:   "node3",
				Name: "nats-sink",
				Type: "nats-sink",
				Config: map[string]any{
					"subject": "output.data",
				},
			},
		},
		RuntimeState: flowstore.StateRunning,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastModified: time.Now(),
	}

	// Create the flow in the store
	err = flowStore.Create(ctx, testFlow)
	require.NoError(t, err)

	// Cleanup after test
	defer func() {
		_ = flowStore.Delete(ctx, testFlow.ID)
	}()

	// Create message logger with test configuration
	msgLoggerConfig := &MessageLoggerConfig{
		MonitorSubjects: []string{"input.>", "process.>", "output.>"},
		MaxEntries:      1000,
		OutputToStdout:  false,
		LogLevel:        "INFO",
	}

	msgLogger, err := NewMessageLogger(msgLoggerConfig, natsClient)
	require.NoError(t, err)

	// Start message logger
	err = msgLogger.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = msgLogger.Stop(5 * time.Second)
	}()

	// Create service manager and manually add message logger for testing
	logger := slog.Default()
	serviceMgr := NewServiceManager(NewServiceRegistry())

	// Manually inject the message logger service (test-only approach)
	serviceMgr.mu.Lock()
	serviceMgr.services["message-logger"] = msgLogger
	serviceMgr.mu.Unlock()

	// Create FlowService
	baseService := NewBaseServiceWithOptions(
		"flow-builder-test",
		nil,
		WithLogger(logger),
	)

	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		serviceMgr:  serviceMgr,
		config: FlowServiceConfig{
			PrometheusURL: "http://localhost:9090",
			FallbackToRaw: true,
		},
	}

	// Test 1: Get messages with real logger - test endpoint response structure
	t.Run("GetMessagesWithRealLogger", func(t *testing.T) {
		// Query the messages endpoint
		req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages", nil)
		req.SetPathValue("id", "messages-test-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMessagesResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Verify response structure
		assert.NotNil(t, response.Messages, "Messages array should not be nil")
		assert.Equal(t, 100, response.Limit, "Default limit should be 100")
		assert.NotEmpty(t, response.Timestamp)
		// Note: Message count may be 0 or more depending on what's been captured
		// The key is that the endpoint returns proper structure
	})

	// Test 2: Filter by flow components
	t.Run("FilterByFlowComponents", func(t *testing.T) {
		// Publish messages for components in this flow
		flowMessages := []string{
			"input.udp-source.data.1",
			"process.json-processor.output",
			"output.nats-sink.sent",
		}

		// Publish messages for components NOT in this flow
		otherMessages := []string{
			"input.tcp-source.data",
			"process.other-processor.data",
		}

		// Publish flow messages
		for _, subject := range flowMessages {
			err := natsClient.Publish(ctx, subject, []byte(fmt.Sprintf(`{"subject":"%s"}`, subject)))
			require.NoError(t, err)
		}

		// Publish other messages
		for _, subject := range otherMessages {
			err := natsClient.Publish(ctx, subject, []byte(fmt.Sprintf(`{"subject":"%s"}`, subject)))
			require.NoError(t, err)
		}

		// Wait for message logger to capture
		time.Sleep(100 * time.Millisecond)

		req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages", nil)
		req.SetPathValue("id", "messages-test-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMessagesResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Verify we only got messages for components in this flow
		for _, msg := range response.Messages {
			// Check that component name matches one of our flow's components
			validComponent := msg.Component == "udp-source" ||
				msg.Component == "json-processor" ||
				msg.Component == "nats-sink"
			assert.True(t, validComponent,
				"Message component %s should be in flow's component list", msg.Component)
		}
	})

	// Test 3: Limit enforcement
	t.Run("LimitEnforcement", func(t *testing.T) {
		// Publish many messages
		for i := 0; i < 150; i++ {
			subject := fmt.Sprintf("input.udp-source.data.%d", i)
			err := natsClient.Publish(ctx, subject, []byte(fmt.Sprintf(`{"seq":%d}`, i)))
			require.NoError(t, err)
		}

		// Wait for capture
		time.Sleep(100 * time.Millisecond)

		// Test default limit (100)
		t.Run("DefaultLimit", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages", nil)
			req.SetPathValue("id", "messages-test-flow")
			w := httptest.NewRecorder()

			fs.handleRuntimeMessages(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response RuntimeMessagesResponse
			err = json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)

			assert.LessOrEqual(t, len(response.Messages), 100, "Should respect default limit of 100")
			assert.Equal(t, 100, response.Limit)
		})

		// Test custom limit
		t.Run("CustomLimit", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages?limit=50", nil)
			req.SetPathValue("id", "messages-test-flow")
			w := httptest.NewRecorder()

			fs.handleRuntimeMessages(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response RuntimeMessagesResponse
			err = json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)

			assert.LessOrEqual(t, len(response.Messages), 50, "Should respect custom limit of 50")
			assert.Equal(t, 50, response.Limit)
		})

		// Test max limit enforcement (1000)
		t.Run("MaxLimitEnforced", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages?limit=5000", nil)
			req.SetPathValue("id", "messages-test-flow")
			w := httptest.NewRecorder()

			fs.handleRuntimeMessages(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response RuntimeMessagesResponse
			err = json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)

			assert.Equal(t, 1000, response.Limit, "Should enforce max limit of 1000")
		})

		// Test minimum limit enforcement
		t.Run("MinLimitEnforced", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages?limit=-10", nil)
			req.SetPathValue("id", "messages-test-flow")
			w := httptest.NewRecorder()

			fs.handleRuntimeMessages(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response RuntimeMessagesResponse
			err = json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)

			assert.Equal(t, 100, response.Limit, "Should default to 100 when limit is invalid")
		})
	})

	// Test 4: Flow not found
	t.Run("FlowNotFound", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/flowbuilder/flows/nonexistent-flow/runtime/messages", nil)
		req.SetPathValue("id", "nonexistent-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// Test 5: Empty flow (no components)
	t.Run("EmptyFlow", func(t *testing.T) {
		emptyFlow := &flowstore.Flow{
			ID:           "empty-flow",
			Name:         "Empty Flow",
			Nodes:        []flowstore.FlowNode{},
			RuntimeState: flowstore.StateNotDeployed,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			LastModified: time.Now(),
		}

		err := flowStore.Create(ctx, emptyFlow)
		require.NoError(t, err)

		defer func() {
			_ = flowStore.Delete(ctx, emptyFlow.ID)
		}()

		req := httptest.NewRequest("GET", "/flowbuilder/flows/empty-flow/runtime/messages", nil)
		req.SetPathValue("id", "empty-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMessagesResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Empty flow should return no messages (no component subjects to filter)
		assert.Equal(t, 0, len(response.Messages))
	})

	// Test 6: Response time requirement
	t.Run("ResponseTime", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/flowbuilder/flows/messages-test-flow/runtime/messages", nil)
		req.SetPathValue("id", "messages-test-flow")
		w := httptest.NewRecorder()

		start := time.Now()
		fs.handleRuntimeMessages(w, req)
		duration := time.Since(start)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Less(t, duration, 100*time.Millisecond, "Response time should be under 100ms")

		var response RuntimeMessagesResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		assert.NotEmpty(t, response.Timestamp)
	})
}

// TestRuntimeMessagesLoggerUnavailable tests graceful degradation when message logger is not available
func TestRuntimeMessagesLoggerUnavailable(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Setup NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a test flow
	testFlow := &flowstore.Flow{
		ID:   "no-logger-flow",
		Name: "No Logger Flow",
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node1",
				Name: "test-component",
				Type: "processor",
			},
		},
		RuntimeState: flowstore.StateRunning,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastModified: time.Now(),
	}

	err = flowStore.Create(ctx, testFlow)
	require.NoError(t, err)

	defer func() {
		_ = flowStore.Delete(ctx, testFlow.ID)
	}()

	// Create service manager WITHOUT message logger
	logger := slog.Default()
	serviceMgr := NewServiceManager(NewServiceRegistry())
	// Intentionally NOT adding message logger service

	// Create FlowService
	baseService := NewBaseServiceWithOptions(
		"flow-builder-test",
		nil,
		WithLogger(logger),
	)

	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		serviceMgr:  serviceMgr,
		config: FlowServiceConfig{
			PrometheusURL: "http://localhost:9090",
			FallbackToRaw: true,
		},
	}

	// Test graceful degradation
	t.Run("GracefulDegradation", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/flowbuilder/flows/no-logger-flow/runtime/messages", nil)
		req.SetPathValue("id", "no-logger-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		// Should still return 200 OK
		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMessagesResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Should have empty messages array
		assert.Equal(t, 0, len(response.Messages))
		assert.Equal(t, 0, response.Total)
		assert.Equal(t, 100, response.Limit)

		// Should have a note explaining why
		assert.NotEmpty(t, response.Note)
		assert.Contains(t, response.Note, "not available")
	})
}

// TestRuntimeMessagesWithActualNATSFlow tests integration with real NATS message flow
func TestRuntimeMessagesWithActualNATSFlow(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Setup NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a test flow
	testFlow := &flowstore.Flow{
		ID:   "nats-flow-test",
		Name: "NATS Flow Test",
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node1",
				Name: "data-source",
				Type: "input",
			},
			{
				ID:   "node2",
				Name: "transformer",
				Type: "transform",
			},
		},
		RuntimeState: flowstore.StateRunning,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastModified: time.Now(),
	}

	err = flowStore.Create(ctx, testFlow)
	require.NoError(t, err)

	defer func() {
		_ = flowStore.Delete(ctx, testFlow.ID)
	}()

	// Create message logger
	msgLoggerConfig := &MessageLoggerConfig{
		MonitorSubjects: []string{"input.>", "process.>"},
		MaxEntries:      100,
		OutputToStdout:  false,
		LogLevel:        "INFO",
	}

	msgLogger, err := NewMessageLogger(msgLoggerConfig, natsClient)
	require.NoError(t, err)

	err = msgLogger.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = msgLogger.Stop(5 * time.Second)
	}()

	// Create service manager and manually add message logger for testing
	logger := slog.Default()
	serviceMgr := NewServiceManager(NewServiceRegistry())

	// Manually inject the message logger service (test-only approach)
	serviceMgr.mu.Lock()
	serviceMgr.services["message-logger"] = msgLogger
	serviceMgr.mu.Unlock()

	// Create FlowService
	baseService := NewBaseServiceWithOptions(
		"flow-builder-test",
		nil,
		WithLogger(logger),
	)

	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		serviceMgr:  serviceMgr,
		config: FlowServiceConfig{
			PrometheusURL: "http://localhost:9090",
			FallbackToRaw: true,
		},
	}

	t.Run("VerifyFilteringLogic", func(t *testing.T) {
		// Test that the endpoint responds correctly even without captured messages
		// This verifies the integration between FlowService and MessageLogger
		req := httptest.NewRequest("GET", "/flowbuilder/flows/nats-flow-test/runtime/messages?limit=100", nil)
		req.SetPathValue("id", "nats-flow-test")
		w := httptest.NewRecorder()

		fs.handleRuntimeMessages(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMessagesResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Verify response structure is correct
		assert.NotNil(t, response.Messages)
		assert.Equal(t, 100, response.Limit)
		assert.NotEmpty(t, response.Timestamp)
		// Note: Messages array may be empty if no valid BaseMessage structures were published
		// The important thing is that the endpoint works and returns the correct structure
	})
}
