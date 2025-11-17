package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeMetricsIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Setup NATS client for testing
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())
	natsClient := testClient.Client
	defer func() {
		_ = testClient.Terminate()
	}()

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	// Create a test flow with components
	testFlow := &flowstore.Flow{
		ID:   "metrics-test-flow",
		Name: "Metrics Test Flow",
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node1",
				Name: "udp-input",
				Type: "input",
				Config: map[string]any{
					"port": 8080,
				},
			},
			{
				ID:   "node2",
				Name: "json-processor",
				Type: "processor",
				Config: map[string]any{
					"filter": "$.data",
				},
			},
			{
				ID:   "node3",
				Name: "file-output",
				Type: "output",
				Config: map[string]any{
					"path": "/tmp/output.jsonl",
				},
			},
		},
		RuntimeState: flowstore.StateRunning,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastModified: time.Now(),
	}

	// Create the flow in the store
	err = flowStore.Create(context.Background(), testFlow)
	require.NoError(t, err)

	// Clean up after test
	defer func() {
		_ = flowStore.Delete(context.Background(), testFlow.ID)
	}()

	// Create FlowService with logger
	logger := slog.Default()
	baseService := NewBaseServiceWithOptions(
		"flow-builder-test",
		nil,
		WithLogger(logger),
	)

	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			PrometheusURL: "http://localhost:9090",
			FallbackToRaw: true,
		},
	}

	// Test 1: Successful request (may fail to Prometheus but should return health-only)
	t.Run("GetMetrics", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/flowbuilder/flows/metrics-test-flow/runtime/metrics", nil)
		req.SetPathValue("id", "metrics-test-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMetrics(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMetricsResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Should have 3 components
		assert.Len(t, response.Components, 3)

		// Verify component names and types
		componentNames := make(map[string]bool)
		for _, comp := range response.Components {
			componentNames[comp.Name] = true
			assert.NotEmpty(t, comp.Type)
		}

		assert.True(t, componentNames["udp-input"])
		assert.True(t, componentNames["json-processor"])
		assert.True(t, componentNames["file-output"])

		// Prometheus availability depends on whether it's actually running
		// We don't assert on this value since it's environment-dependent
		t.Logf("Prometheus available: %v", response.PrometheusAvailable)
	})

	// Test 2: Non-existent flow
	t.Run("FlowNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/flowbuilder/flows/nonexistent/runtime/metrics", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		fs.handleRuntimeMetrics(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// Test 3: Empty flow (no components)
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

		err := flowStore.Create(context.Background(), emptyFlow)
		require.NoError(t, err)

		defer func() {
			_ = flowStore.Delete(context.Background(), emptyFlow.ID)
		}()

		req := httptest.NewRequest(http.MethodGet, "/flowbuilder/flows/empty-flow/runtime/metrics", nil)
		req.SetPathValue("id", "empty-flow")
		w := httptest.NewRecorder()

		fs.handleRuntimeMetrics(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response RuntimeMetricsResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		assert.Len(t, response.Components, 0)
	})
}

func TestRuntimeMetrics_WithMockPrometheus(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Create a mock Prometheus server that returns valid responses
	mockPrometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock Prometheus API responses
		if r.URL.Path == "/api/v1/query" {
			query := r.URL.Query().Get("query")

			// Return mock data based on query
			var response any
			if query != "" {
				// Return a valid Prometheus response
				response = map[string]any{
					"status": "success",
					"data": map[string]any{
						"resultType": "vector",
						"result": []map[string]any{
							{
								"metric": map[string]string{
									"component": "test-component",
								},
								"value": []any{
									float64(time.Now().Unix()),
									"123.45", // metric value
								},
							},
						},
					},
				}
			} else {
				response = map[string]any{
					"status": "success",
					"data": map[string]any{
						"resultType": "vector",
						"result":     []any{},
					},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		// Return 404 for other paths
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockPrometheus.Close()

	// Setup NATS client
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())
	natsClient := testClient.Client
	defer func() {
		_ = testClient.Terminate()
	}()

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	// Create test flow
	testFlow := &flowstore.Flow{
		ID:   "prometheus-test-flow",
		Name: "Prometheus Test Flow",
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node1",
				Name: "test-component",
				Type: "input",
			},
		},
		RuntimeState: flowstore.StateRunning,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastModified: time.Now(),
	}

	err = flowStore.Create(context.Background(), testFlow)
	require.NoError(t, err)

	defer func() {
		_ = flowStore.Delete(context.Background(), testFlow.ID)
	}()

	// Create FlowService with mock Prometheus URL and logger
	logger := slog.Default()
	baseService := NewBaseServiceWithOptions(
		"flow-builder-test",
		nil,
		WithLogger(logger),
	)

	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			PrometheusURL: mockPrometheus.URL,
			FallbackToRaw: true,
		},
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/flowbuilder/flows/prometheus-test-flow/runtime/metrics", nil)
	req.SetPathValue("id", "prometheus-test-flow")
	w := httptest.NewRecorder()

	fs.handleRuntimeMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response RuntimeMetricsResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	// Should have metrics from Prometheus
	assert.True(t, response.PrometheusAvailable)
	assert.Len(t, response.Components, 1)

	// Verify component has data
	comp := response.Components[0]
	assert.Equal(t, "test-component", comp.Name)
	assert.Equal(t, "input", comp.Type)

	// Throughput might be populated depending on mock implementation
	t.Logf("Component throughput: %v", comp.Throughput)
	t.Logf("Component error_rate: %v", comp.ErrorRate)
	t.Logf("Component queue_depth: %v", comp.QueueDepth)
}
