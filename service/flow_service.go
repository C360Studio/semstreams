package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/c360/semstreams/config"
	flowengine "github.com/c360/semstreams/engine"
	"github.com/c360/semstreams/flowstore"
	"github.com/google/uuid"
)

// FlowServiceConfig holds configuration for the flow service
type FlowServiceConfig struct {
	// PrometheusURL is the base URL for Prometheus HTTP API
	// Default: http://localhost:9090
	PrometheusURL string `json:"prometheus_url,omitempty"`

	// FallbackToRaw enables falling back to raw metrics when Prometheus unavailable
	// Default: true
	FallbackToRaw bool `json:"fallback_to_raw,omitempty"`

	// LogStreamBufferSize is the buffer size for SSE log streaming channel
	// Larger buffers reduce dropped logs during bursts but use more memory.
	// Default: 100
	LogStreamBufferSize int `json:"log_stream_buffer_size,omitempty"`
}

// FlowService provides HTTP APIs for visual flow builder
type FlowService struct {
	*BaseService

	flowStore      *flowstore.Store
	flowEngine     *flowengine.Engine
	configMgr      *config.Manager
	serviceMgr     *Manager // Access to other services (ComponentManager)
	config         FlowServiceConfig

	mu sync.RWMutex
}

// NewFlowServiceFromConfig creates a new flow service
func NewFlowServiceFromConfig(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config with defaults
	cfg := FlowServiceConfig{
		PrometheusURL:       "http://localhost:9090",
		FallbackToRaw:       true,
		LogStreamBufferSize: 100,
	}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse flow service config: %w", err)
		}
	}

	if deps == nil || deps.NATSClient == nil {
		return nil, fmt.Errorf("flow service requires NATS client")
	}
	if deps.Manager == nil {
		return nil, fmt.Errorf("flow service requires config manager")
	}
	if deps.ComponentRegistry == nil {
		return nil, fmt.Errorf("flow service requires component registry")
	}

	// Create flow store
	flowStore, err := flowstore.NewStore(deps.NATSClient)
	if err != nil {
		return nil, fmt.Errorf("create flow store: %w", err)
	}

	// Create flow engine with metrics
	flowEngine := flowengine.NewEngine(deps.Manager, flowStore, deps.ComponentRegistry, deps.NATSClient, deps.Logger, deps.MetricsRegistry)

	// Create base service
	baseService := NewBaseServiceWithOptions(
		"flow-builder",
		nil,
		WithLogger(deps.Logger),
		WithMetrics(deps.MetricsRegistry),
		WithNATS(deps.NATSClient),
	)

	service := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		flowEngine:  flowEngine,
		configMgr:   deps.Manager,
		serviceMgr:  deps.ServiceManager,
		config:      cfg,
	}

	return service, nil
}

// Start starts the flow service
func (fs *FlowService) Start(ctx context.Context) error {
	// Set health check
	fs.SetHealthCheck(func() error {
		return nil // Always healthy for now
	})

	// Start base service
	if err := fs.BaseService.Start(ctx); err != nil {
		return err
	}

	fs.logger.Info("Flow service started")
	return nil
}

// Stop stops the flow service
func (fs *FlowService) Stop(timeout time.Duration) error {
	fs.logger.Info("Flow service stopped")
	return fs.BaseService.Stop(timeout)
}

// RegisterHTTPHandlers registers HTTP endpoints for the flow service
func (fs *FlowService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	fs.logger.Info("Flow service HTTP handlers registered", "prefix", prefix)

	// Flow CRUD endpoints
	// Note: Go 1.22+ ServeMux supports method and path patterns
	fs.logger.Info(
		"Registering flow routes",
		"list_pattern",
		"GET "+prefix+"flows",
		"get_pattern",
		"GET "+prefix+"flows/{id}",
	)
	mux.HandleFunc("GET "+prefix+"flows", fs.handleListFlows)
	mux.HandleFunc("POST "+prefix+"flows", fs.handleCreateFlow)
	mux.HandleFunc("GET "+prefix+"flows/{id}", fs.handleGetFlowWrapper)
	mux.HandleFunc("PUT "+prefix+"flows/{id}", fs.handleUpdateFlowWrapper)
	mux.HandleFunc("DELETE "+prefix+"flows/{id}", fs.handleDeleteFlowWrapper)

	// Validation endpoint for draft flows
	mux.HandleFunc("POST "+prefix+"flows/{id}/validate", fs.handleValidateFlow)

	// Deployment endpoints
	mux.HandleFunc("POST "+prefix+"deployment/{id}/{operation}", fs.handleDeployment)

	// Runtime metrics endpoint
	mux.HandleFunc("GET "+prefix+"flows/{id}/runtime/metrics", fs.handleRuntimeMetrics)

	// Runtime health endpoint
	mux.HandleFunc("GET "+prefix+"flows/{id}/runtime/health", fs.handleRuntimeHealth)

	// Runtime logs SSE endpoint
	mux.HandleFunc("GET "+prefix+"flows/{id}/runtime/logs", fs.handleRuntimeLogs)

	// Runtime messages endpoint (message logger filtering)
	mux.HandleFunc("GET "+prefix+"flows/{id}/runtime/messages", fs.handleRuntimeMessages)

	// Status WebSocket endpoint
	mux.HandleFunc(prefix+"status/stream", fs.handleStatusWebSocket)
}

// OpenAPISpec returns the OpenAPI specification for flow service endpoints
func (fs *FlowService) OpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Paths: map[string]PathSpec{
			"/flows": {
				GET: &OperationSpec{
					Summary:     "List all flows",
					Description: "Returns a list of all visual flows",
					Tags:        []string{"Flows"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "List of flows",
							ContentType: "application/json",
						},
					},
				},
				POST: &OperationSpec{
					Summary:     "Create a new flow",
					Description: "Creates a new visual flow",
					Tags:        []string{"Flows"},
					Responses: map[string]ResponseSpec{
						"201": {
							Description: "Flow created",
							ContentType: "application/json",
						},
						"400": {
							Description: "Invalid request",
						},
					},
				},
			},
			"/flows/{id}": {
				GET: &OperationSpec{
					Summary:     "Get flow by ID",
					Description: "Returns a single flow by ID",
					Tags:        []string{"Flows"},
					Parameters: []ParameterSpec{
						{
							Name:        "id",
							In:          "path",
							Required:    true,
							Description: "Flow ID",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow details",
							ContentType: "application/json",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
				PUT: &OperationSpec{
					Summary:     "Update flow",
					Description: "Updates an existing flow",
					Tags:        []string{"Flows"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow updated",
							ContentType: "application/json",
						},
						"404": {
							Description: "Flow not found",
						},
						"409": {
							Description: "Version conflict",
						},
					},
				},
				DELETE: &OperationSpec{
					Summary:     "Delete flow",
					Description: "Deletes a flow",
					Tags:        []string{"Flows"},
					Responses: map[string]ResponseSpec{
						"204": {
							Description: "Flow deleted",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
			},
			"/deployment/{id}/deploy": {
				POST: &OperationSpec{
					Summary:     "Deploy flow",
					Description: "Deploys a flow to the runtime",
					Tags:        []string{"Deployment"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow deployed",
						},
						"400": {
							Description: "Validation errors",
						},
					},
				},
			},
			"/deployment/{id}/start": {
				POST: &OperationSpec{
					Summary:     "Start flow",
					Description: "Starts a deployed flow",
					Tags:        []string{"Deployment"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow started",
						},
					},
				},
			},
			"/deployment/{id}/stop": {
				POST: &OperationSpec{
					Summary:     "Stop flow",
					Description: "Stops a running flow",
					Tags:        []string{"Deployment"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow stopped",
						},
					},
				},
			},
			"/flows/{id}/runtime/metrics": {
				GET: &OperationSpec{
					Summary:     "Get runtime metrics",
					Description: "Returns runtime metrics for flow components (throughput, errors, queue depth) with graceful degradation",
					Tags:        []string{"Runtime"},
					Parameters: []ParameterSpec{
						{
							Name:        "id",
							In:          "path",
							Required:    true,
							Description: "Flow ID",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Runtime metrics",
							ContentType: "application/json",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
			},
			"/flows/{id}/runtime/health": {
				GET: &OperationSpec{
					Summary:     "Get runtime health",
					Description: "Returns health status and timing for flow components (status, uptime, last activity)",
					Tags:        []string{"Runtime"},
					Parameters: []ParameterSpec{
						{
							Name:        "id",
							In:          "path",
							Required:    true,
							Description: "Flow ID",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Runtime health status",
							ContentType: "application/json",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
			},
			"/flows/{id}/runtime/messages": {
				GET: &OperationSpec{
					Summary:     "Get runtime messages",
					Description: "Returns filtered message logger entries for flow components (NATS message flow visibility)",
					Tags:        []string{"Runtime"},
					Parameters: []ParameterSpec{
						{
							Name:        "id",
							In:          "path",
							Required:    true,
							Description: "Flow ID",
							Schema:      Schema{Type: "string"},
						},
						{
							Name:        "limit",
							In:          "query",
							Required:    false,
							Description: "Maximum number of messages to return (default: 100, max: 1000)",
							Schema:      Schema{Type: "integer"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Runtime message entries",
							ContentType: "application/json",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
			},
			"/status/stream": {
				GET: &OperationSpec{
					Summary:     "WebSocket status stream",
					Description: "Real-time flow status updates via WebSocket",
					Tags:        []string{"Status"},
					Responses: map[string]ResponseSpec{
						"101": {
							Description: "Switching to WebSocket",
						},
					},
				},
			},
		},
		Tags: []TagSpec{
			{
				Name:        "Flows",
				Description: "Visual flow CRUD operations",
			},
			{
				Name:        "Deployment",
				Description: "Flow deployment and runtime control",
			},
			{
				Name:        "Runtime",
				Description: "Runtime metrics and observability",
			},
			{
				Name:        "Status",
				Description: "Real-time status updates",
			},
		},
	}
}

// =============================================================================
// HTTP Handlers
// =============================================================================

// Wrapper handlers for path parameter extraction
func (fs *FlowService) handleGetFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleGetFlow(w, r, r.PathValue("id"))
}

func (fs *FlowService) handleUpdateFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleUpdateFlow(w, r, r.PathValue("id"))
}

func (fs *FlowService) handleDeleteFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleDeleteFlow(w, r, r.PathValue("id"))
}

// handleListFlows returns all flows
func (fs *FlowService) handleListFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := fs.flowStore.List(r.Context())
	if err != nil {
		// If no keys found, return empty array
		if strings.Contains(err.Error(), "no keys found") {
			fs.writeJSON(w, map[string]any{"flows": []any{}})
			return
		}
		fs.logger.Error("Failed to list flows", "error", err)
		fs.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	fs.writeJSON(w, map[string]any{"flows": flows})
}

// handleCreateFlow creates a new flow
func (fs *FlowService) handleCreateFlow(w http.ResponseWriter, r *http.Request) {
	var flow flowstore.Flow
	if err := json.NewDecoder(r.Body).Decode(&flow); err != nil {
		fs.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Generate ID if not provided
	if flow.ID == "" {
		flow.ID = generateFlowID()
	}

	if err := fs.flowStore.Create(r.Context(), &flow); err != nil {
		fs.logger.Error("Failed to create flow", "error", err)
		fs.writeJSONError(w, "Failed to create flow", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(flow); err != nil {
		fs.logger.Error("Failed to encode flow response", "error", err)
	}
}

// handleGetFlow returns a single flow by ID
func (fs *FlowService) handleGetFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	flow, err := fs.flowStore.Get(r.Context(), flowID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(flow); err != nil {
		fs.logger.Error("Failed to encode flow response", "error", err)
	}
}

// handleUpdateFlow updates an existing flow
func (fs *FlowService) handleUpdateFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	var flow flowstore.Flow
	if err := json.NewDecoder(r.Body).Decode(&flow); err != nil {
		fs.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if flow.ID != flowID {
		fs.writeJSONError(w, "ID mismatch", http.StatusBadRequest)
		return
	}

	// Check if flow is running - cannot edit running flows
	existingFlow, err := fs.flowStore.Get(r.Context(), flowID)
	if err != nil {
		fs.logger.Error("Failed to get existing flow", "error", err)
		fs.writeJSONError(w, "Failed to get flow", http.StatusInternalServerError)
		return
	}

	if existingFlow.RuntimeState == flowstore.StateRunning {
		fs.writeJSONError(w, "Cannot modify running flow. Stop the flow first.", http.StatusConflict)
		return
	}

	if err := fs.flowStore.Update(r.Context(), &flow); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			fs.writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		fs.logger.Error("Failed to update flow", "error", err)
		fs.writeJSONError(w, "Failed to update flow", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(flow); err != nil {
		fs.logger.Error("Failed to encode flow response", "error", err)
	}
}

// writeJSON writes a JSON response and logs encoding errors
func (fs *FlowService) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fs.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// writeJSONError writes an error response in JSON format
func (fs *FlowService) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		fs.logger.Error("Failed to encode error response", "error", err, "message", message)
	}
}

// handleDeleteFlow deletes a flow
func (fs *FlowService) handleDeleteFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	if err := fs.flowStore.Delete(r.Context(), flowID); err != nil {
		fs.logger.Error("Failed to delete flow", "error", err)
		fs.writeJSONError(w, "Failed to delete flow", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeployment handles deployment operations (deploy, start, stop, undeploy)
func (fs *FlowService) handleDeployment(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("id")
	operation := r.PathValue("operation")

	var err error
	switch operation {
	case "deploy":
		err = fs.flowEngine.Deploy(r.Context(), flowID)
	case "start":
		err = fs.flowEngine.Start(r.Context(), flowID)
	case "stop":
		err = fs.flowEngine.Stop(r.Context(), flowID)
	case "undeploy":
		err = fs.flowEngine.Undeploy(r.Context(), flowID)
	default:
		fs.writeJSONError(w, "Unknown operation", http.StatusBadRequest)
		return
	}

	if err != nil {
		fs.logger.Error("Deployment operation failed", "operation", operation, "flow_id", flowID, "error", err)

		// Check if it's a validation error with structured details (use errors.As for wrapped errors)
		var validationErr *flowengine.ValidationError
		if errors.As(err, &validationErr) {
			// Return structured validation response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "Flow validation failed",
				"validation_result": validationErr.Result,
			})
			return
		}

		// Handle other error types
		if strings.Contains(err.Error(), "invalid") {
			fs.writeJSONError(w, err.Error(), http.StatusBadRequest)
		} else {
			fs.writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Fetch the updated flow to return with new runtime_state
	updatedFlow, err := fs.flowStore.Get(r.Context(), flowID)
	if err != nil {
		fs.logger.Error("Failed to fetch updated flow after deployment", "flow_id", flowID, "error", err)
		fs.writeJSONError(w, "Deployment succeeded but failed to fetch updated flow", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(updatedFlow); err != nil {
		fs.logger.Error("Failed to encode flow response", "error", err)
	}
}

// handleValidateFlow validates a draft flow without deploying it
// Returns validation results including port information and discovered connections
//
// Accepts optional flow definition in request body:
//   - If body provided → validates the provided flow (preview mode)
//   - If body empty → loads from KV and validates (backwards compatible)
func (fs *FlowService) handleValidateFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("id")

	var flowToValidate *flowstore.Flow

	// Try to parse flow from request body (preview mode)
	if r.ContentLength > 0 {
		fs.logger.Debug("Validating flow from request body", "flow_id", flowID)

		var flowFromRequest flowstore.Flow
		if err := json.NewDecoder(r.Body).Decode(&flowFromRequest); err != nil {
			fs.logger.Error("Failed to decode flow body", "flow_id", flowID, "error", err)
			fs.writeJSONError(w, fmt.Sprintf("Invalid JSON in request body: %v", err), http.StatusBadRequest)
			return
		}

		// Ensure ID from URL matches body ID (if body has ID set)
		if flowFromRequest.ID != "" && flowFromRequest.ID != flowID {
			fs.logger.Error("Flow ID mismatch", "url_id", flowID, "body_id", flowFromRequest.ID)
			fs.writeJSONError(
				w,
				fmt.Sprintf("Flow ID mismatch: URL has '%s' but body has '%s'", flowID, flowFromRequest.ID),
				http.StatusBadRequest,
			)
			return
		}

		// Set ID from URL (in case body didn't include it)
		flowFromRequest.ID = flowID
		flowToValidate = &flowFromRequest

		fs.logger.Debug("Using flow from request body",
			"flow_id", flowID,
			"node_count", len(flowFromRequest.Nodes),
			"connection_count", len(flowFromRequest.Connections))

	} else {
		// No body provided, load from KV (backwards compatible behavior)
		fs.logger.Debug("Validating flow from NATS KV", "flow_id", flowID)

		flow, err := fs.flowStore.Get(r.Context(), flowID)
		if err != nil {
			fs.logger.Error("Failed to load flow for validation", "flow_id", flowID, "error", err)
			fs.writeJSONError(w, "Flow not found", http.StatusNotFound)
			return
		}
		flowToValidate = flow

		fs.logger.Debug("Using flow from NATS KV",
			"flow_id", flowID,
			"node_count", len(flow.Nodes),
			"connection_count", len(flow.Connections))
	}

	// Run validation using FlowEngine's validator
	validationResult, err := fs.flowEngine.ValidateFlowDefinition(flowToValidate)
	if err != nil {
		fs.logger.Error("Validation failed", "flow_id", flowID, "error", err)
		fs.writeJSONError(w, fmt.Sprintf("Validation failed: %v", err), http.StatusInternalServerError)
		return
	}

	fs.logger.Debug("Validation complete",
		"flow_id", flowID,
		"status", validationResult.Status,
		"error_count", len(validationResult.Errors),
		"warning_count", len(validationResult.Warnings))

	// Return validation result with port information
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(validationResult); err != nil {
		fs.logger.Error("Failed to encode validation result", "error", err)
	}
}

// handleStatusWebSocket handles WebSocket connections for real-time status updates
func (fs *FlowService) handleStatusWebSocket(w http.ResponseWriter, _ *http.Request) {
	// TODO: Implement WebSocket upgrade and status streaming
	// This will subscribe to component health events and broadcast to connected clients
	// Implementation follows TDD in tasks

	fs.logger.Info("WebSocket status stream requested (not yet implemented)")
	fs.writeJSONError(w, "WebSocket not yet implemented", http.StatusNotImplemented)
}

// generateFlowID generates a unique flow ID
func generateFlowID() string {
	return uuid.New().String()
}
