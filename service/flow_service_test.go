package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestFlowService creates a FlowService instance for testing with HTTP server
func createTestFlowService(t *testing.T) (*http.ServeMux, *flowstore.Store, *natsclient.Client) {
	t.Helper()

	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=1 to run)")
	}

	// Create NATS client using shared test helper
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())
	natsClient := testClient.Client

	// Create test logger
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create component registry and register SemStreams core components
	registry := component.NewRegistry()
	err := componentregistry.Register(registry)
	require.NoError(t, err)

	// Create config manager with minimal config
	baseConfig := &config.Config{}
	configMgr, err := config.NewConfigManager(baseConfig, natsClient, logger)
	require.NoError(t, err)
	require.NoError(t, configMgr.Start(context.Background()))

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	// Create dependencies
	deps := &service.Dependencies{
		NATSClient:        natsClient,
		Manager:           configMgr,
		ComponentRegistry: registry,
		Logger:            logger,
	}

	// Create flow service
	svc, err := service.NewFlowServiceFromConfig(nil, deps)
	require.NoError(t, err)

	// Cast to concrete type to access HTTP handler registration
	flowService, ok := svc.(*service.FlowService)
	require.True(t, ok, "Expected *FlowService")

	// Register HTTP handlers
	mux := http.NewServeMux()
	flowService.RegisterHTTPHandlers("/flowbuilder/", mux)

	return mux, flowStore, natsClient
}

// TestHandleValidateFlow_WithBody tests validation with flow definition in request body
func TestHandleValidateFlow_WithBody(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	// Create test flow in request body
	flowID := "test-flow-with-body"
	requestFlow := flowstore.Flow{
		ID:           flowID,
		Name:         "Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node-1",
				Type: "udp",
				Name: "UDP Input",
				Position: flowstore.Position{
					X: 100,
					Y: 100,
				},
				Config: map[string]any{
					"port": 14550,
				},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	// Marshal flow to JSON
	bodyBytes, err := json.Marshal(requestFlow)
	require.NoError(t, err)

	// Create HTTP request with body
	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/flowbuilder/flows/%s/validate", flowID),
		bytes.NewReader(bodyBytes),
	)
	req.SetPathValue("id", flowID)
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler through mux
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Response body: %s", w.Body.String())

	// Parse validation result
	var result map[string]any
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	// Verify validation result contains port information
	assert.Contains(t, result, "validation_status")
	assert.Contains(t, result, "nodes")

	// Verify nodes array is not null (port info extracted)
	nodes, ok := result["nodes"].([]any)
	require.True(t, ok, "Expected nodes to be an array")
	assert.NotEmpty(t, nodes, "Expected port information in validation result")

	t.Logf("Validation result: %+v", result)
}

// TestHandleValidateFlow_WithoutBody tests validation without body (backwards compatible)
func TestHandleValidateFlow_WithoutBody(t *testing.T) {
	mux, flowStore, _ := createTestFlowService(t)

	// Create and save a flow to NATS KV
	flowID := "test-flow-without-body"
	flow := &flowstore.Flow{
		ID:           flowID,
		Name:         "Test Flow in KV",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node-1",
				Type: "udp",
				Name: "UDP Input",
				Position: flowstore.Position{
					X: 100,
					Y: 100,
				},
				Config: map[string]any{
					"port": 14550,
				},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	// Save flow to NATS KV
	err := flowStore.Create(context.Background(), flow)
	require.NoError(t, err)

	// Cleanup
	defer func() {
		_ = flowStore.Delete(context.Background(), flowID)
	}()

	// Create HTTP request WITHOUT body
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/flowbuilder/flows/%s/validate", flowID), nil)
	req.SetPathValue("id", flowID)

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler through mux
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Response body: %s", w.Body.String())

	// Parse validation result
	var result map[string]any
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	// Verify validation result contains port information
	assert.Contains(t, result, "validation_status")
	assert.Contains(t, result, "nodes")

	// Verify nodes array is not null (port info extracted)
	nodes, ok := result["nodes"].([]any)
	require.True(t, ok, "Expected nodes to be an array")
	assert.NotEmpty(t, nodes, "Expected port information in validation result")

	t.Logf("Validation result: %+v", result)
}

// TestHandleValidateFlow_InvalidJSON tests validation with invalid JSON body
func TestHandleValidateFlow_InvalidJSON(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	flowID := "test-flow-invalid-json"

	// Create HTTP request with invalid JSON
	invalidJSON := `{"nodes": [{"id": "missing-closing-brace"`
	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/flowbuilder/flows/%s/validate", flowID),
		bytes.NewReader([]byte(invalidJSON)),
	)
	req.SetPathValue("id", flowID)
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler through mux
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusBadRequest, w.Code, "Response body: %s", w.Body.String())

	// Parse error response
	var errorResp map[string]string
	err := json.NewDecoder(w.Body).Decode(&errorResp)
	require.NoError(t, err)

	assert.Contains(t, errorResp, "error")
	assert.Contains(t, errorResp["error"], "Invalid JSON")

	t.Logf("Error response: %+v", errorResp)
}

// TestHandleValidateFlow_IDMismatch tests validation when body ID doesn't match URL ID
func TestHandleValidateFlow_IDMismatch(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	urlFlowID := "test-flow-url-id"
	bodyFlowID := "test-flow-body-id"

	// Create test flow with different ID
	requestFlow := flowstore.Flow{
		ID:           bodyFlowID, // Different from URL
		Name:         "Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes:        []flowstore.FlowNode{},
		Connections:  []flowstore.FlowConnection{},
	}

	// Marshal flow to JSON
	bodyBytes, err := json.Marshal(requestFlow)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/flowbuilder/flows/%s/validate", urlFlowID),
		bytes.NewReader(bodyBytes),
	)
	req.SetPathValue("id", urlFlowID)
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler through mux
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusBadRequest, w.Code, "Response body: %s", w.Body.String())

	// Parse error response
	var errorResp map[string]string
	err = json.NewDecoder(w.Body).Decode(&errorResp)
	require.NoError(t, err)

	assert.Contains(t, errorResp, "error")
	assert.Contains(t, errorResp["error"], "Flow ID mismatch")
	assert.Contains(t, errorResp["error"], urlFlowID)
	assert.Contains(t, errorResp["error"], bodyFlowID)

	t.Logf("Error response: %+v", errorResp)
}

// TestHandleValidateFlow_WithBodyNoID tests that body without ID uses URL ID
func TestHandleValidateFlow_WithBodyNoID(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	flowID := "test-flow-no-id-in-body"

	// Create test flow WITHOUT ID in body
	requestFlow := map[string]any{
		"name":          "Test Flow",
		"runtime_state": "not_deployed",
		"nodes": []flowstore.FlowNode{
			{
				ID:   "node-1",
				Type: "udp",
				Name: "UDP Input",
				Position: flowstore.Position{
					X: 100,
					Y: 100,
				},
				Config: map[string]any{
					"port": 14550,
				},
			},
		},
		"connections": []flowstore.FlowConnection{},
	}

	// Marshal flow to JSON
	bodyBytes, err := json.Marshal(requestFlow)
	require.NoError(t, err)

	// Create HTTP request with body (no ID)
	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/flowbuilder/flows/%s/validate", flowID),
		bytes.NewReader(bodyBytes),
	)
	req.SetPathValue("id", flowID)
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler through mux
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Response body: %s", w.Body.String())

	// Parse validation result
	var result map[string]any
	err = json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	// Verify validation result contains port information
	assert.Contains(t, result, "validation_status")
	assert.Contains(t, result, "nodes")

	t.Logf("Validation result: %+v", result)
}
