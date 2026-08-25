//go:build integration

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
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestFlowService creates a FlowService instance for testing with HTTP server
func createTestFlowService(t *testing.T) (*http.ServeMux, *flowstore.Manager, *natsclient.Client) {
	t.Helper()
	mux, flowStore, natsClient, _ := createTestFlowServiceWithConfigManager(t)
	return mux, flowStore, natsClient
}

func createTestFlowServiceWithConfigManager(
	t *testing.T,
) (*http.ServeMux, *flowstore.Manager, *natsclient.Client, *config.Manager) {
	t.Helper()

	// Build tag ensures this only runs with -tags=integration
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
	baseConfig := &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "c360", ID: "flow-service-test", Type: "test"},
	}
	configMgr, err := config.NewConfigManager(baseConfig, natsClient, logger)
	require.NoError(t, err)
	require.NoError(t, configMgr.Start(context.Background()))

	// Create flow store
	flowStore, err := flowstore.NewManager(natsClient)
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

	return mux, flowStore, natsClient, configMgr
}

func TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager(t *testing.T) {
	mux, _, _, configManager := createTestFlowServiceWithConfigManager(t)
	flow := flowstore.Flow{
		ID: "authoring-contract", Name: "Authoring contract",
		CreatedBy: "preserved-client-field",
		Nodes: []flowstore.FlowNode{{
			ID: "node-1", Component: "udp", Type: types.ComponentTypeInput,
			Name: "published-input", Config: map[string]any{"port": 14550},
		}},
		Connections: []flowstore.FlowConnection{},
	}

	doJSON := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			require.NoError(t, err)
			reader = bytes.NewReader(data)
		}
		request := httptest.NewRequest(method, path, reader)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}

	created := doJSON(http.MethodPost, "/flowbuilder/flows", flow)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &flow))
	require.Equal(t, "authoring-contract", flow.ID)
	require.Equal(t, int64(1), flow.Version)
	require.Equal(t, "preserved-client-field", flow.CreatedBy)
	require.Empty(t, configManager.GetConfig().Get().Components, "CRUD create must not publish component config")

	createdAt := flow.CreatedAt
	require.False(t, createdAt.IsZero(), "create must stamp created_at")

	// A legacy full-Flow body, with a forged created_at: it decodes, the server
	// ignores the client's audit timestamps, and provenance survives the save.
	flow.Description = "updated authoring metadata"
	flow.CreatedAt = time.Date(1999, time.January, 2, 3, 4, 5, 0, time.UTC)
	flow.UpdatedAt = flow.CreatedAt
	flow.LastModified = flow.CreatedAt
	stale := flow
	updated := doJSON(http.MethodPut, "/flowbuilder/flows/authoring-contract", flow)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &flow))
	require.Equal(t, int64(2), flow.Version)
	require.True(t, flow.CreatedAt.Equal(createdAt), "forged created_at was stored: %v", flow.CreatedAt)
	require.True(t, flow.UpdatedAt.Equal(flow.LastModified), "update timestamps must be one server instant")
	require.Empty(t, configManager.GetConfig().Get().Components, "CRUD update must not publish component config")

	// The now-stale body loses the optimistic-concurrency precondition; 409 is
	// decided by the error's classification, not by its message text.
	conflicted := doJSON(http.MethodPut, "/flowbuilder/flows/authoring-contract", stale)
	require.Equal(t, http.StatusConflict, conflicted.Code, conflicted.Body.String())

	for attempt := 1; attempt <= 2; attempt++ {
		published := doJSON(http.MethodPost, "/flowbuilder/flows/authoring-contract/publish-component-configs", nil)
		require.Equal(t, http.StatusOK, published.Code, "attempt %d: %s", attempt, published.Body.String())
		var response map[string]any
		require.NoError(t, json.Unmarshal(published.Body.Bytes(), &response))
		require.Equal(t, []any{"published-input"}, response["persisted_components"])
		require.Equal(t, true, response["runtime_unchanged"])
		require.Equal(t, true, response["restart_required"])
		persisted := configManager.GetConfig().Get().Components["published-input"]
		require.Equal(t, "udp", persisted.Name)
		require.True(t, persisted.Enabled)
	}
}

// TestHandleValidateFlow_WithBody tests validation with flow definition in request body
func TestHandleValidateFlow_WithBody(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	// Create test flow in request body
	flowID := "test-flow-with-body"
	requestFlow := flowstore.Flow{
		ID:   flowID,
		Name: "Test Flow",
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "UDP Input",
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
		ID:   flowID,
		Name: "Test Flow in KV",
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "UDP Input",
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

func TestHandleValidateFlow_IDMismatch(t *testing.T) {
	mux, _, _ := createTestFlowService(t)

	urlFlowID := "test-flow-url-id"
	bodyFlowID := "test-flow-body-id"

	// Create test flow with different ID
	requestFlow := flowstore.Flow{
		ID:          bodyFlowID,
		Name:        "Test Flow",
		Nodes:       []flowstore.FlowNode{},
		Connections: []flowstore.FlowConnection{},
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
		"name": "Test Flow",
		"nodes": []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "UDP Input",
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
