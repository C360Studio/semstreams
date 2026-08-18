package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/config"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/metric"
	"github.com/google/uuid"
)

func init() {
	RegisterOpenAPISpec("flow-service", flowServiceOpenAPISpec())
}

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

	flowStore     *flowstore.Manager
	flowEngine    *flowengine.Engine
	configMgr     *config.Manager
	bootConfig    *config.Config
	bootSelection *flowstore.BootSelection

	// overrideExpiry re-evaluates migration-override expiry on an interval, because
	// boot-time evaluation cannot see a bridge that lapses while the process runs.
	overrideExpiry *streamOverrideExpiryReporter
	serviceMgr     *Manager // Access to other services (for health API, message-logger)
	natsClient     natsSubscriber
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
	if deps.FlowManager == nil {
		return nil, fmt.Errorf("flow service requires shared flow manager")
	}
	if deps.BootSelection == nil {
		return nil, fmt.Errorf("flow service requires boot selection")
	}
	flowStore := deps.FlowManager
	bootConfig := deps.BootSelection.Config()

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
		BaseService:   baseService,
		flowStore:     flowStore,
		flowEngine:    flowEngine,
		configMgr:     deps.Manager,
		bootConfig:    bootConfig,
		bootSelection: deps.BootSelection,
		serviceMgr:    deps.ServiceManager,
		natsClient:    deps.NATSClient,
		config:        cfg,
	}

	return service, nil
}

// Start starts the flow service
func (fs *FlowService) Start(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "FlowService", "Start"); err != nil {
		return err
	}
	// Set health check
	fs.SetHealthCheck(func() error {
		return nil // Always healthy for now
	})

	// Start base service
	if err := fs.BaseService.Start(ctx); err != nil {
		return err
	}

	// Bridge static config to FlowStore: create default flow if needed
	// This makes headless static configs visible to the UI.
	// Precedence: KV wins if it exists (preserves UI customizations)
	if err := fs.ensureDefaultFlowFromConfig(ctx); err != nil {
		fs.logger.Warn("Failed to create default flow from config", "error", err)
		// Non-fatal: continue without default flow
	}

	// Start flow status publisher (watches KV and publishes to NATS for WebSocket consumption)
	fs.startFlowStatusPublisher(ctx)

	// A migration override's expiry is otherwise evaluated only at boot, so an
	// instance that started before the deadline would run past it in silence. This
	// REPORTS; the refusal stays at the next boot. See stream_override_expiry.go.
	fs.startOverrideExpiryReporter(ctx)

	fs.logger.Info("Flow service started")
	return nil
}

// ensureDefaultFlowFromConfig creates a default flow from static config if:
// 1. No flows exist in the FlowStore (first boot)
// 2. Static config has enabled components
//
// This bridges the gap between headless static configs and the UI.
// On subsequent boots, existing flows in KV are preserved (KV wins).
func (fs *FlowService) ensureDefaultFlowFromConfig(ctx context.Context) error {
	// Check if any flows already exist
	flows, err := fs.flowStore.List(ctx)
	if err != nil {
		// If error is "no keys found", that's fine - no flows exist
		if !strings.Contains(err.Error(), "no keys found") {
			return fmt.Errorf("list flows: %w", err)
		}
		flows = nil
	}

	if len(flows) > 0 {
		// KV wins: flows already exist, preserve UI customizations
		fs.logger.Debug("Using existing flows from KV", "count", len(flows))
		return nil
	}

	// No flows in KV - check if static config has components
	if fs.configMgr == nil {
		return nil // No config manager available
	}

	if fs.bootConfig == nil {
		return nil
	}
	currentCfg := fs.bootConfig
	if currentCfg == nil || len(currentCfg.Components) == 0 {
		fs.logger.Debug("No components in static config, skipping default flow creation")
		return nil
	}

	// Count enabled components
	enabledCount := 0
	for _, comp := range currentCfg.Components {
		if comp.Enabled {
			enabledCount++
		}
	}

	if enabledCount == 0 {
		fs.logger.Debug("No enabled components in static config")
		return nil
	}

	// First boot: create flow from static config
	defaultFlow, err := flowstore.FromComponentConfigs("default", currentCfg.Components)
	if err != nil {
		return fmt.Errorf("convert config to flow: %w", err)
	}

	// Derive connections using FlowEngine validation
	// The validator builds a FlowGraph and auto-discovers connections via port subject matching
	validationResult, validationErr := fs.flowEngine.ValidateFlowDefinition(defaultFlow)
	if validationErr != nil {
		fs.logger.Debug("Flow validation failed during connection derivation",
			"error", validationErr)
		// Non-fatal: proceed without connections
	} else if validationResult != nil {
		// Convert discovered connections to FlowConnections
		for _, dc := range validationResult.DiscoveredConnections {
			conn := flowstore.FlowConnection{
				ID:           uuid.New().String(),
				SourceNodeID: dc.SourceNodeID,
				SourcePort:   dc.SourcePort,
				TargetNodeID: dc.TargetNodeID,
				TargetPort:   dc.TargetPort,
			}
			defaultFlow.Connections = append(defaultFlow.Connections, conn)
		}
		fs.logger.Debug("Derived connections from static config",
			"connections", len(defaultFlow.Connections))
	}

	if err := fs.flowStore.Create(ctx, defaultFlow); err != nil {
		return fmt.Errorf("create default flow: %w", err)
	}

	fs.logger.Info("Created default flow from static config",
		"flow_id", defaultFlow.ID,
		"components", enabledCount,
		"connections", len(defaultFlow.Connections),
		"desired_state", defaultFlow.DesiredState)

	return nil
}

// Stop stops the flow service
func (fs *FlowService) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "FlowService", "Stop"); err != nil {
		return err
	}
	fs.logger.Info("Flow service stopped")
	return fs.BaseService.Stop(ctx)
}

// RegisterHTTPHandlers registers HTTP endpoints for the flow service
func (fs *FlowService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	fs.logger.Debug("Flow service HTTP handlers registered", "prefix", prefix)

	// Flow CRUD endpoints
	// Note: Go 1.22+ ServeMux supports method and path patterns
	fs.logger.Debug(
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
	return flowServiceOpenAPISpec()
}

// flowServiceOpenAPISpec returns the OpenAPI specification for flow service endpoints.
// This is a standalone function so it can be called from init() for registration.
func flowServiceOpenAPISpec() *OpenAPISpec {
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
					RequestBody: &RequestBodySpec{
						Description: "Flow definition to create",
						Required:    true,
						SchemaRef:   "#/components/schemas/Flow",
					},
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
					RequestBody: &RequestBodySpec{
						Description: "Updated flow definition",
						Required:    true,
						SchemaRef:   "#/components/schemas/Flow",
					},
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
							SchemaRef:   "#/components/schemas/RuntimeMetricsResponse",
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
							SchemaRef:   "#/components/schemas/RuntimeHealthResponse",
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
							SchemaRef:   "#/components/schemas/RuntimeMessagesResponse",
						},
						"404": {
							Description: "Flow not found",
						},
					},
				},
			},
			"/status/stream": {
				GET: &OperationSpec{
					Summary: "WebSocket status stream",
					Description: `Real-time flow status updates via WebSocket.

## Connection
Connect with: ws://host/flowbuilder/status/stream?flowId={flowId}

## Message Types (Server → Client)
All messages are wrapped in StatusStreamEnvelope:
- **flow_status**: Flow state changes (deployed, running, stopped, failed)
- **component_health**: Component health updates (every 5s)
- **component_metrics**: Real-time metrics from MetricsForwarder
- **log_entry**: Log messages from LogForwarder

## Filtering (Client → Server)
Send SubscribeCommand JSON to filter messages:
- message_types: Array of message types to receive
- log_level: Minimum log level (DEBUG < INFO < WARN < ERROR)
- sources: Array of component names to filter by

## Example Subscribe Command
{"command":"subscribe","message_types":["flow_status","log_entry"],"log_level":"WARN"}
`,
					Tags: []string{"Status"},
					Parameters: []ParameterSpec{
						{
							Name:        "flowId",
							In:          "query",
							Required:    true,
							Description: "Flow ID to subscribe to for status updates",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"101": {
							Description: "Switching to WebSocket protocol",
						},
						"400": {
							Description: "Missing or invalid flowId parameter",
						},
						"404": {
							Description: "Flow not found",
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
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(RuntimeHealthResponse{}),
			reflect.TypeOf(RuntimeMetricsResponse{}),
			reflect.TypeOf(RuntimeMessagesResponse{}),
			reflect.TypeOf(flowstore.Flow{}),
			// WebSocket message types
			reflect.TypeOf(StatusStreamEnvelope{}),
			reflect.TypeOf(SubscribeCommand{}),
			reflect.TypeOf(FlowStatusPayload{}),
			reflect.TypeOf(LogEntryPayload{}),
			reflect.TypeOf(MetricsPayload{}),
			reflect.TypeOf(MetricEntry{}),
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
	fs.decorateFlows(flows)

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
	fs.bootSelection.Decorate(flow)

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

// handleDeployment records desired activation operations. The running process
// remains sealed; a successful response never claims live mutation.
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

	// Fetch the decorated desired/effective observation after the write.
	updatedFlow, err := fs.flowStore.Get(r.Context(), flowID)
	if err != nil {
		fs.logger.Error("Failed to fetch updated flow after deployment", "flow_id", flowID, "error", err)
		fs.writeJSONError(w, "Deployment succeeded but failed to fetch updated flow", http.StatusInternalServerError)
		return
	}
	fs.bootSelection.Decorate(updatedFlow)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"flow":              updatedFlow,
		"desired_state":     updatedFlow.DesiredState,
		"runtime_unchanged": true,
		"restart_required":  updatedFlow.RestartRequired,
	}); err != nil {
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
func (fs *FlowService) handleStatusWebSocket(w http.ResponseWriter, r *http.Request) {
	fs.handleStatusWebSocketImpl(w, r)
}

// generateFlowID generates a unique flow ID
func generateFlowID() string {
	return uuid.New().String()
}

// startFlowStatusPublisher watches the flows KV bucket for state changes
// and publishes them to flows.{flowId}.status for WebSocket consumption.
// This enables event-driven flow status updates instead of per-client polling.
//
// Seeds the FLOWS JetStream stream with the current state of every flow at
// startup BEFORE the watcher loop begins. Without this, the watcher's
// bootstrap delivery is racy on fresh containers and the flow_status envelope
// type may not appear before the e2e dataflow test's 30s deadline — see
// project_websocket_flake_diagnosis.
func (fs *FlowService) startFlowStatusPublisher(ctx context.Context) {
	if fs.natsClient == nil || fs.flowStore == nil {
		fs.logger.Warn("Flow status publisher disabled: missing NATS client or flow store")
		return
	}

	// Seed FLOWS stream with current state of every flow so last_per_subject
	// consumers see something immediately on connect. Best-effort: errors are
	// logged but do not block publisher startup.
	if flows, err := fs.flowStore.List(ctx); err == nil {
		fs.decorateFlows(flows)
		for _, flow := range flows {
			if flow != nil {
				fs.publishFlowStatus(ctx, flow)
			}
		}
	} else {
		fs.logger.Debug("Flow status publisher: seed-list failed; relying on watcher bootstrap", "error", err)
	}

	// Watch all flows in the KV bucket
	watcher, err := fs.flowStore.Watch(ctx, "*")
	if err != nil {
		fs.logger.Error("Failed to start flow status watcher", "error", err)
		return
	}

	go func() {
		defer watcher.Stop()
		fs.logger.Info("Flow status publisher started")

		for {
			select {
			case <-ctx.Done():
				fs.logger.Debug("Flow status publisher stopping")
				return
			case entry := <-watcher.Updates():
				if entry == nil {
					continue
				}
				// Parse only the identity from the raw desired record, then ask the
				// manager for the independently decorated observation.
				var flow flowstore.Flow
				if err := json.Unmarshal(entry.Value(), &flow); err != nil {
					fs.logger.Debug("Failed to unmarshal flow for status publish", "error", err)
					continue
				}
				observed, err := fs.flowStore.Get(ctx, flow.ID)
				if err != nil {
					fs.logger.Debug("Failed to observe flow for status publish", "flow_id", flow.ID, "error", err)
					continue
				}
				fs.bootSelection.Decorate(observed)
				fs.publishFlowStatus(ctx, observed)
			}
		}
	}()
}

func (fs *FlowService) decorateFlows(flows []*flowstore.Flow) {
	for _, flow := range flows {
		fs.bootSelection.Decorate(flow)
	}
}

// publishFlowStatus publishes a flow state change to NATS JetStream.
func (fs *FlowService) publishFlowStatus(ctx context.Context, flow *flowstore.Flow) {
	if flow == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"timestamp":               time.Now().UnixMilli(),
		"flow_id":                 flow.ID,
		"desired_state":           flow.DesiredState,
		"effective_state":         flow.EffectiveState,
		"restart_required":        flow.RestartRequired,
		"desired_provenance":      flow.DesiredProvenance,
		"boot_applied_provenance": flow.BootAppliedProvenance,
	})
	if err != nil {
		return
	}
	_ = fs.natsClient.PublishToStream(ctx, "flows."+flow.ID+".status", data)
}

// startOverrideExpiryReporter runs the migration-override expiry reporter for this
// instance's lifetime. It is best-effort: a deployment with no configuration
// manager has no overrides to evaluate, and a reporter that could not start must
// never keep the service from running — it reports a hygiene condition, and taking
// the service down over the inability to report one would invert the whole reason
// enforcement was left at boot.
func (fs *FlowService) startOverrideExpiryReporter(ctx context.Context) {
	if fs.configMgr == nil {
		return
	}
	fs.ensureOverrideExpiryReporter()
	go fs.overrideExpiry.run(ctx)
}

// ensureOverrideExpiryReporter builds the reporter once. It is called from both
// RegisterMetrics and Start because the service manager may invoke them in either
// order, and a gauge registered from a nil reporter would silently never update.
func (fs *FlowService) ensureOverrideExpiryReporter() {
	if fs.configMgr == nil || fs.overrideExpiry != nil {
		return
	}
	{
		fs.overrideExpiry = newStreamOverrideExpiryReporter(
			func() *config.Config {
				safe := fs.configMgr.GetConfig()
				if safe == nil {
					return nil
				}
				return safe.Get()
			},
			fs.logger.With("source", "flow-service.stream-overrides"),
		)
	}
}

// RegisterMetrics exposes the migration-override expiry gauge.
//
// The gauge is the alertable half of reporting a lapsed bridge — the WARN log is
// what someone greps after the fact, this is what pages them. Registration failing
// is not fatal: it costs the alert, not the service.
func (fs *FlowService) RegisterMetrics(registrar metric.MetricsRegistrar) error {
	fs.ensureOverrideExpiryReporter()
	if fs.overrideExpiry == nil {
		return nil
	}
	return fs.overrideExpiry.register(registrar)
}
