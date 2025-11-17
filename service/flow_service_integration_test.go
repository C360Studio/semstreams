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
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/service"
)

type FlowServiceHTTPSuite struct {
	suite.Suite
	testClient        *natsclient.TestClient
	natsClient        *natsclient.Client
	configMgr         *config.Manager
	componentRegistry *component.Registry
	flowService       service.Service
	flowServiceImpl   *service.FlowService
	httpServer        *httptest.Server
	baseURL           string
	ctx               context.Context
	cancel            context.CancelFunc
}

func (s *FlowServiceHTTPSuite) SetupSuite() {
	s.testClient = natsclient.NewTestClient(s.T(),
		natsclient.WithJetStream(),
		natsclient.WithKV())
	s.natsClient = s.testClient.Client
}

func (s *FlowServiceHTTPSuite) SetupTest() {
	var err error

	// Create component registry with core SemStreams components
	s.componentRegistry = component.NewRegistry()
	err = componentregistry.Register(s.componentRegistry)
	s.Require().NoError(err)

	// Create base config
	baseConfig := &config.Config{
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
		Components: make(config.ComponentConfigs),
	}

	// Create config manager
	s.configMgr, err = config.NewConfigManager(baseConfig, s.natsClient, nil)
	s.Require().NoError(err)

	// Create context for test
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 30*time.Second)

	// Start config manager
	err = s.configMgr.Start(s.ctx)
	s.Require().NoError(err)

	// Create flow service
	deps := &service.Dependencies{
		NATSClient:        s.natsClient,
		Manager:           s.configMgr,
		ComponentRegistry: s.componentRegistry,
		MetricsRegistry:   metric.NewMetricsRegistry(),
		Logger:            slog.Default(),
	}

	s.flowService, err = service.NewFlowServiceFromConfig(nil, deps)
	s.Require().NoError(err)

	// Cast to concrete type for HTTP handler registration
	var ok bool
	s.flowServiceImpl, ok = s.flowService.(*service.FlowService)
	s.Require().True(ok, "FlowService should be of type *service.FlowService")

	// Start flow service
	err = s.flowService.Start(s.ctx)
	s.Require().NoError(err)

	// Create HTTP test server
	mux := http.NewServeMux()
	s.flowServiceImpl.RegisterHTTPHandlers("/flowbuilder", mux)
	s.httpServer = httptest.NewServer(mux)
	s.baseURL = s.httpServer.URL + "/flowbuilder"
}

func (s *FlowServiceHTTPSuite) TearDownTest() {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
	if s.flowService != nil {
		s.flowService.Stop(5 * time.Second)
	}
	if s.configMgr != nil {
		s.configMgr.Stop(5 * time.Second)
	}
	s.cancel()
}

// Helper functions

func (s *FlowServiceHTTPSuite) doRequest(method, path string, body any) (*http.Response, []byte) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		s.Require().NoError(err)
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, s.baseURL+path, reqBody)
	s.Require().NoError(err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)

	respBody, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	resp.Body.Close()

	return resp, respBody
}

// Test Cases

// TestHTTP_CreateFlow tests POST /flows
func (s *FlowServiceHTTPSuite) TestHTTP_CreateFlow() {
	flow := map[string]any{
		"id":   "test-flow-1",
		"name": "Test Flow 1",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-1",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5000},
			},
		},
		"connections": []any{},
	}

	resp, body := s.doRequest("POST", "/flows", flow)

	s.Equal(http.StatusCreated, resp.StatusCode, "Should return 201 Created")
	s.Equal("application/json", resp.Header.Get("Content-Type"))

	var createdFlow flowstore.Flow
	err := json.Unmarshal(body, &createdFlow)
	s.Require().NoError(err)

	s.Equal("test-flow-1", createdFlow.ID)
	s.Equal("Test Flow 1", createdFlow.Name)
	s.Len(createdFlow.Nodes, 1)
}

// TestHTTP_CreateFlowWithoutID tests POST /flows auto-generates ID
func (s *FlowServiceHTTPSuite) TestHTTP_CreateFlowWithoutID() {
	flow := map[string]any{
		"name":        "Auto ID Flow",
		"nodes":       []any{},
		"connections": []any{},
	}

	resp, body := s.doRequest("POST", "/flows", flow)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var createdFlow flowstore.Flow
	err := json.Unmarshal(body, &createdFlow)
	s.Require().NoError(err)

	s.NotEmpty(createdFlow.ID, "Should auto-generate ID")
	s.Equal("Auto ID Flow", createdFlow.Name)
}

// TestHTTP_ListFlows tests GET /flows
func (s *FlowServiceHTTPSuite) TestHTTP_ListFlows() {
	// Create a couple of flows first
	flow1 := map[string]any{
		"id":          "list-flow-1",
		"name":        "List Test 1",
		"nodes":       []any{},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow1)

	flow2 := map[string]any{
		"id":          "list-flow-2",
		"name":        "List Test 2",
		"nodes":       []any{},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow2)

	// List flows
	resp, body := s.doRequest("GET", "/flows", nil)

	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("application/json", resp.Header.Get("Content-Type"))

	var result map[string]any
	err := json.Unmarshal(body, &result)
	s.Require().NoError(err)

	flows, ok := result["flows"].([]any)
	s.True(ok, "Response should have 'flows' array")
	s.GreaterOrEqual(len(flows), 2, "Should have at least 2 flows")
}

// TestHTTP_GetFlow tests GET /flows/{id}
func (s *FlowServiceHTTPSuite) TestHTTP_GetFlow() {
	// Create a flow
	flow := map[string]any{
		"id":          "get-flow-1",
		"name":        "Get Test",
		"description": "Test getting a flow",
		"nodes":       []any{},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Get the flow
	resp, body := s.doRequest("GET", "/flows/get-flow-1", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var retrievedFlow flowstore.Flow
	err := json.Unmarshal(body, &retrievedFlow)
	s.Require().NoError(err)

	s.Equal("get-flow-1", retrievedFlow.ID)
	s.Equal("Get Test", retrievedFlow.Name)
	s.Equal("Test getting a flow", retrievedFlow.Description)
}

// TestHTTP_GetFlowNotFound tests GET /flows/{id} with non-existent ID
func (s *FlowServiceHTTPSuite) TestHTTP_GetFlowNotFound() {
	resp, _ := s.doRequest("GET", "/flows/non-existent-flow", nil)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// TestHTTP_UpdateFlow tests PUT /flows/{id}
func (s *FlowServiceHTTPSuite) TestHTTP_UpdateFlow() {
	// Create a flow
	flow := map[string]any{
		"id":          "update-flow-1",
		"name":        "Original Name",
		"nodes":       []any{},
		"connections": []any{},
	}
	createResp, createBody := s.doRequest("POST", "/flows", flow)
	s.Equal(http.StatusCreated, createResp.StatusCode)

	var createdFlow flowstore.Flow
	json.Unmarshal(createBody, &createdFlow)

	// Update the flow
	createdFlow.Name = "Updated Name"
	createdFlow.Description = "Updated description"

	resp, body := s.doRequest("PUT", "/flows/update-flow-1", createdFlow)

	s.Equal(http.StatusOK, resp.StatusCode)

	var updatedFlow flowstore.Flow
	err := json.Unmarshal(body, &updatedFlow)
	s.Require().NoError(err)

	s.Equal("Updated Name", updatedFlow.Name)
	s.Equal("Updated description", updatedFlow.Description)
}

// TestHTTP_UpdateFlowIDMismatch tests PUT /flows/{id} with mismatched ID
func (s *FlowServiceHTTPSuite) TestHTTP_UpdateFlowIDMismatch() {
	flow := map[string]any{
		"id":          "wrong-id",
		"name":        "Test",
		"nodes":       []any{},
		"connections": []any{},
	}

	resp, body := s.doRequest("PUT", "/flows/correct-id", flow)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var errorResp map[string]string
	json.Unmarshal(body, &errorResp)
	s.Contains(errorResp["error"], "mismatch")
}

// TestHTTP_DeleteFlow tests DELETE /flows/{id}
func (s *FlowServiceHTTPSuite) TestHTTP_DeleteFlow() {
	// Create a flow
	flow := map[string]any{
		"id":          "delete-flow-1",
		"name":        "To Be Deleted",
		"nodes":       []any{},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Delete the flow
	resp, _ := s.doRequest("DELETE", "/flows/delete-flow-1", nil)

	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify it's gone
	getResp, _ := s.doRequest("GET", "/flows/delete-flow-1", nil)
	s.Equal(http.StatusNotFound, getResp.StatusCode)
}

// TestHTTP_ValidateFlow tests POST /flows/{id}/validate
func (s *FlowServiceHTTPSuite) TestHTTP_ValidateFlow() {
	// Create a valid flow (UDP component has all required ports)
	flow := map[string]any{
		"id":   "validate-flow-1",
		"name": "Validation Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-validate",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5000},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Validate the flow
	resp, body := s.doRequest("POST", "/flows/validate-flow-1/validate", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var validationResult map[string]any
	err := json.Unmarshal(body, &validationResult)
	s.Require().NoError(err)

	s.Contains(validationResult, "validation_status")
	// Standalone UDP has unconnected output port, generating warnings
	s.Equal("warnings", validationResult["validation_status"], "Flow should have warnings")
}

// TestHTTP_ValidateFlowInvalid tests validation with invalid flow
func (s *FlowServiceHTTPSuite) TestHTTP_ValidateFlowInvalid() {
	// Create an invalid flow (Filter without connections has unmet ports)
	flow := map[string]any{
		"id":   "invalid-flow-1",
		"name": "Invalid Flow",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "filter",
				"name":     "filter-invalid",
				"position": map[string]int{"x": 100, "y": 100},
				"config": map[string]any{
					"ports": map[string]any{
						"inputs": []map[string]any{
							{"name": "input", "type": "nats", "subject": "test.in", "required": true},
						},
						"outputs": []map[string]any{
							{"name": "output", "type": "nats", "subject": "test.out", "required": true},
						},
					},
					"rules": []map[string]any{
						{"field": "value", "operator": "gt", "value": 100},
					},
				},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Validate the flow
	resp, body := s.doRequest("POST", "/flows/invalid-flow-1/validate", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var validationResult map[string]any
	err := json.Unmarshal(body, &validationResult)
	s.Require().NoError(err)

	s.Contains(validationResult, "validation_status")
	s.Equal("errors", validationResult["validation_status"], "Flow should have errors")
	s.Contains(validationResult, "errors")
}

// TestHTTP_DeployFlow tests POST /deployment/{id}/deploy
func (s *FlowServiceHTTPSuite) TestHTTP_DeployFlow() {
	// Create a valid flow
	flow := map[string]any{
		"id":   "deploy-flow-1",
		"name": "Deploy Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-deploy",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5001},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Deploy the flow
	resp, body := s.doRequest("POST", "/deployment/deploy-flow-1/deploy", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var deployedFlow flowstore.Flow
	err := json.Unmarshal(body, &deployedFlow)
	s.Require().NoError(err)

	s.Equal(flowstore.StateDeployedStopped, deployedFlow.RuntimeState)
}

// TestHTTP_StartFlow tests POST /deployment/{id}/start
func (s *FlowServiceHTTPSuite) TestHTTP_StartFlow() {
	// Create and deploy a flow
	flow := map[string]any{
		"id":   "start-flow-1",
		"name": "Start Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-start",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5002},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)
	s.doRequest("POST", "/deployment/start-flow-1/deploy", nil)

	// Start the flow
	resp, body := s.doRequest("POST", "/deployment/start-flow-1/start", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var startedFlow flowstore.Flow
	err := json.Unmarshal(body, &startedFlow)
	s.Require().NoError(err)

	s.Equal(flowstore.StateRunning, startedFlow.RuntimeState)
}

// TestHTTP_StopFlow tests POST /deployment/{id}/stop
func (s *FlowServiceHTTPSuite) TestHTTP_StopFlow() {
	// Create, deploy, and start a flow
	flow := map[string]any{
		"id":   "stop-flow-1",
		"name": "Stop Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-stop",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5003},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)
	s.doRequest("POST", "/deployment/stop-flow-1/deploy", nil)
	s.doRequest("POST", "/deployment/stop-flow-1/start", nil)

	// Stop the flow
	resp, body := s.doRequest("POST", "/deployment/stop-flow-1/stop", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var stoppedFlow flowstore.Flow
	err := json.Unmarshal(body, &stoppedFlow)
	s.Require().NoError(err)

	s.Equal(flowstore.StateDeployedStopped, stoppedFlow.RuntimeState)
}

// TestHTTP_UndeployFlow tests POST /deployment/{id}/undeploy
func (s *FlowServiceHTTPSuite) TestHTTP_UndeployFlow() {
	// Create and deploy a flow
	flow := map[string]any{
		"id":   "undeploy-flow-1",
		"name": "Undeploy Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-undeploy",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5004},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)
	s.doRequest("POST", "/deployment/undeploy-flow-1/deploy", nil)

	// Undeploy the flow
	resp, body := s.doRequest("POST", "/deployment/undeploy-flow-1/undeploy", nil)

	s.Equal(http.StatusOK, resp.StatusCode)

	var undeployedFlow flowstore.Flow
	err := json.Unmarshal(body, &undeployedFlow)
	s.Require().NoError(err)

	s.Equal(flowstore.StateNotDeployed, undeployedFlow.RuntimeState)
}

// TestHTTP_DeployInvalidFlow tests deploying a flow that fails validation
func (s *FlowServiceHTTPSuite) TestHTTP_DeployInvalidFlow() {
	// Create an invalid flow
	flow := map[string]any{
		"id":   "deploy-invalid-1",
		"name": "Invalid Deploy",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "filter",
				"name":     "filter-deploy-invalid",
				"position": map[string]int{"x": 100, "y": 100},
				"config": map[string]any{
					"ports": map[string]any{
						"inputs": []map[string]any{
							{"name": "input", "type": "nats", "subject": "test.in", "required": true},
						},
						"outputs": []map[string]any{
							{"name": "output", "type": "nats", "subject": "test.out", "required": true},
						},
					},
					"rules": []map[string]any{
						{"field": "value", "operator": "gt", "value": 100},
					},
				},
			},
		},
		"connections": []any{},
	}
	s.doRequest("POST", "/flows", flow)

	// Try to deploy - should fail validation
	resp, body := s.doRequest("POST", "/deployment/deploy-invalid-1/deploy", nil)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var errorResp map[string]any
	json.Unmarshal(body, &errorResp)
	s.Contains(errorResp, "error")
	s.Contains(errorResp, "validation_result")
}

// TestHTTP_FullLifecycle tests complete CRUD + deployment lifecycle via HTTP
func (s *FlowServiceHTTPSuite) TestHTTP_FullLifecycle() {
	flowID := "lifecycle-http-1"

	// 1. Create flow
	flow := map[string]any{
		"id":   flowID,
		"name": "HTTP Lifecycle Test",
		"nodes": []map[string]any{
			{
				"id":       "node-1",
				"type":     "udp",
				"name":     "udp-lifecycle",
				"position": map[string]int{"x": 100, "y": 100},
				"config":   map[string]any{"port": 5005},
			},
		},
		"connections": []any{},
	}
	createResp, _ := s.doRequest("POST", "/flows", flow)
	s.Equal(http.StatusCreated, createResp.StatusCode)

	// 2. Get flow
	getResp, _ := s.doRequest("GET", fmt.Sprintf("/flows/%s", flowID), nil)
	s.Equal(http.StatusOK, getResp.StatusCode)

	// 3. Validate flow
	validateResp, validateBody := s.doRequest("POST", fmt.Sprintf("/flows/%s/validate", flowID), nil)
	s.Equal(http.StatusOK, validateResp.StatusCode)

	var validationResult map[string]any
	json.Unmarshal(validateBody, &validationResult)
	// Standalone UDP has unconnected output port, generating warnings
	s.Equal("warnings", validationResult["validation_status"])

	// 4. Deploy
	deployResp, _ := s.doRequest("POST", fmt.Sprintf("/deployment/%s/deploy", flowID), nil)
	s.Equal(http.StatusOK, deployResp.StatusCode)

	// 5. Start
	startResp, _ := s.doRequest("POST", fmt.Sprintf("/deployment/%s/start", flowID), nil)
	s.Equal(http.StatusOK, startResp.StatusCode)

	// 6. Stop
	stopResp, _ := s.doRequest("POST", fmt.Sprintf("/deployment/%s/stop", flowID), nil)
	s.Equal(http.StatusOK, stopResp.StatusCode)

	// 7. Undeploy
	undeployResp, _ := s.doRequest("POST", fmt.Sprintf("/deployment/%s/undeploy", flowID), nil)
	s.Equal(http.StatusOK, undeployResp.StatusCode)

	// 8. Delete
	deleteResp, _ := s.doRequest("DELETE", fmt.Sprintf("/flows/%s", flowID), nil)
	s.Equal(http.StatusNoContent, deleteResp.StatusCode)

	// 9. Verify deleted
	finalGetResp, _ := s.doRequest("GET", fmt.Sprintf("/flows/%s", flowID), nil)
	s.Equal(http.StatusNotFound, finalGetResp.StatusCode)
}

func TestFlowServiceHTTPSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Skip if INTEGRATION_TESTS not set
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration tests (set INTEGRATION_TESTS=1 to run)")
	}

	suite.Run(t, new(FlowServiceHTTPSuite))
}
