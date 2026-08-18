package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/config"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/types"
	"github.com/google/uuid"
)

func init() {
	RegisterOpenAPISpec("flow-service", flowServiceOpenAPISpec())
}

// FlowServiceConfig holds configuration for saved-diagram observations.
type FlowServiceConfig struct {
	PrometheusURL string `json:"prometheus_url,omitempty"`
	FallbackToRaw bool   `json:"fallback_to_raw,omitempty"`
}

// FlowCreateRequest is the complete authoring input for a new saved diagram.
// Identity, version, and audit fields are owned by the server.
type FlowCreateRequest struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	Nodes       []flowstore.FlowNode       `json:"nodes"`
	Connections []flowstore.FlowConnection `json:"connections"`
}

func (FlowCreateRequest) closedJSONSchema() {}

// FlowUpdateRequest is the complete authoring input for an existing saved
// diagram. ExpectedVersion supplies optimistic concurrency; all resulting
// persistence metadata remains server-owned.
type FlowUpdateRequest struct {
	Name            string                     `json:"name"`
	Description     string                     `json:"description,omitempty"`
	Nodes           []flowstore.FlowNode       `json:"nodes"`
	Connections     []flowstore.FlowConnection `json:"connections"`
	ExpectedVersion int64                      `json:"expected_version"`
}

func (FlowUpdateRequest) closedJSONSchema() {}

// FlowValidateRequest is the complete authoring input for validating a draft
// diagram without persisting it.
type FlowValidateRequest struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	Nodes       []flowstore.FlowNode       `json:"nodes"`
	Connections []flowstore.FlowConnection `json:"connections"`
}

func (FlowValidateRequest) closedJSONSchema() {}

type componentConfigPublisher interface {
	GetConfig() *config.SafeConfig
	BootConfig() *config.Config
	PutComponentToKV(context.Context, string, types.ComponentConfig) error
	ComponentRestartRequired() (bool, error)
}

// FlowService provides saved flow-diagram CRUD, validation, compilation, and
// observations keyed by the component names declared in a diagram. A diagram
// is not a runtime lifecycle owner.
type FlowService struct {
	*BaseService

	flowStore  *flowstore.Manager
	flowEngine *flowengine.Engine
	configMgr  componentConfigPublisher
	bootConfig *config.Config

	overrideExpiry *streamOverrideExpiryReporter
	serviceMgr     *Manager
	config         FlowServiceConfig
}

// NewFlowServiceFromConfig creates a saved-flow service.
func NewFlowServiceFromConfig(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	cfg := FlowServiceConfig{
		PrometheusURL: "http://localhost:9090",
		FallbackToRaw: true,
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
	bootConfig := deps.Manager.BootConfig()
	if bootConfig == nil {
		return nil, fmt.Errorf("flow service requires a started config manager")
	}

	baseService := NewBaseServiceWithOptions(
		"flow-builder",
		nil,
		WithLogger(deps.Logger),
		WithMetrics(deps.MetricsRegistry),
		WithNATS(deps.NATSClient),
	)
	return &FlowService{
		BaseService: baseService,
		flowStore:   deps.FlowManager,
		flowEngine:  flowengine.NewEngine(deps.ComponentRegistry, deps.NATSClient, deps.Logger, deps.MetricsRegistry),
		configMgr:   deps.Manager,
		bootConfig:  bootConfig,
		serviceMgr:  deps.ServiceManager,
		config:      cfg,
	}, nil
}

// Start starts saved-diagram service work for the supplied process lifetime.
func (fs *FlowService) Start(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "FlowService", "Start"); err != nil {
		return err
	}
	fs.SetHealthCheck(func() error { return nil })
	if err := fs.BaseService.Start(ctx); err != nil {
		return err
	}
	if err := fs.ensureDefaultFlowFromConfig(ctx); err != nil {
		fs.logger.Warn("Failed to create default flow diagram from boot config", "error", err)
	}
	fs.startOverrideExpiryReporter(ctx)
	fs.logger.Info("Flow service started")
	return nil
}

// ensureDefaultFlowFromConfig imports the immutable boot component map as a
// first-run diagram. It does not make that diagram runtime authority.
func (fs *FlowService) ensureDefaultFlowFromConfig(ctx context.Context) error {
	flows, err := fs.flowStore.List(ctx)
	if err != nil && !strings.Contains(err.Error(), "no keys found") {
		return fmt.Errorf("list flows: %w", err)
	}
	if len(flows) > 0 || fs.bootConfig == nil || len(fs.bootConfig.Components) == 0 {
		return nil
	}

	defaultFlow, err := flowstore.FromComponentConfigs("default", fs.bootConfig.Components)
	if err != nil {
		return fmt.Errorf("convert config to flow: %w", err)
	}
	if validation, validationErr := fs.flowEngine.ValidateFlowDefinition(defaultFlow); validationErr == nil && validation != nil {
		for _, discovered := range validation.DiscoveredConnections {
			defaultFlow.Connections = append(defaultFlow.Connections, flowstore.FlowConnection{
				ID:           uuid.New().String(),
				SourceNodeID: discovered.SourceNodeID,
				SourcePort:   discovered.SourcePort,
				TargetNodeID: discovered.TargetNodeID,
				TargetPort:   discovered.TargetPort,
			})
		}
	}
	if err := fs.flowStore.Create(ctx, defaultFlow); err != nil {
		return fmt.Errorf("create default flow: %w", err)
	}
	fs.logger.Info("Created default flow diagram from boot config",
		"flow_id", defaultFlow.ID,
		"components", len(defaultFlow.Nodes),
		"connections", len(defaultFlow.Connections))
	return nil
}

// Stop joins saved-diagram service work.
func (fs *FlowService) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "FlowService", "Stop"); err != nil {
		return err
	}
	return fs.BaseService.Stop(ctx)
}

// RegisterHTTPHandlers registers diagram CRUD, validation, publication, and
// saved-diagram observation routes.
func (fs *FlowService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc("GET "+prefix+"flows", fs.handleListFlows)
	mux.HandleFunc("POST "+prefix+"flows", fs.handleCreateFlow)
	mux.HandleFunc("GET "+prefix+"flows/{id}", fs.handleGetFlowWrapper)
	mux.HandleFunc("PUT "+prefix+"flows/{id}", fs.handleUpdateFlowWrapper)
	mux.HandleFunc("DELETE "+prefix+"flows/{id}", fs.handleDeleteFlowWrapper)
	mux.HandleFunc("POST "+prefix+"flows/{id}/validate", fs.handleValidateFlow)
	mux.HandleFunc("POST "+prefix+"flows/{id}/publish-component-configs", fs.handlePublishComponentConfigs)
	mux.HandleFunc("GET "+prefix+"flows/{id}/observations/metrics", fs.handleRuntimeMetrics)
	mux.HandleFunc("GET "+prefix+"flows/{id}/observations/health", fs.handleRuntimeHealth)
	mux.HandleFunc("GET "+prefix+"flows/{id}/observations/messages", fs.handleRuntimeMessages)
}

// OpenAPISpec returns the saved-flow HTTP contract.
func (fs *FlowService) OpenAPISpec() *OpenAPISpec { return flowServiceOpenAPISpec() }

func flowServiceOpenAPISpec() *OpenAPISpec {
	idParam := []ParameterSpec{{Name: "id", In: "path", Required: true, Description: "Saved flow diagram ID", Schema: Schema{Type: "string"}}}
	observation := func(summary, description, schema string) *OperationSpec {
		return &OperationSpec{
			Summary: summary, Description: description, Tags: []string{"Flow observations"}, Parameters: idParam,
			Responses: map[string]ResponseSpec{
				"200": {Description: summary, ContentType: "application/json", SchemaRef: schema},
				"404": {Description: "Saved flow diagram not found"},
			},
		}
	}
	return &OpenAPISpec{
		Paths: map[string]PathSpec{
			"/flows": {
				GET:  &OperationSpec{Summary: "List saved flow diagrams", Description: "Lists saved diagrams; no runtime lifecycle state is implied.", Tags: []string{"Flows"}, Responses: map[string]ResponseSpec{"200": {Description: "Saved flow diagrams", ContentType: "application/json"}}},
				POST: &OperationSpec{Summary: "Create a saved flow diagram", Description: "Saves a diagram without changing runtime configuration. Identity, version, and audit fields are server-owned.", Tags: []string{"Flows"}, RequestBody: &RequestBodySpec{Description: "Flow authoring fields", Required: true, SchemaRef: "#/components/schemas/FlowCreateRequest"}, Responses: map[string]ResponseSpec{"201": {Description: "Diagram created", ContentType: "application/json", SchemaRef: "#/components/schemas/Flow"}, "400": {Description: "Invalid request"}}},
			},
			"/flows/{id}": {
				GET:    &OperationSpec{Summary: "Get a saved flow diagram", Description: "Returns diagram metadata, nodes, connections, and audit fields.", Tags: []string{"Flows"}, Parameters: idParam, Responses: map[string]ResponseSpec{"200": {Description: "Saved diagram", ContentType: "application/json"}, "404": {Description: "Saved diagram not found"}}},
				PUT:    &OperationSpec{Summary: "Update a saved flow diagram", Description: "Updates authoring fields with optimistic concurrency; path identity and resulting persistence metadata are server-owned. Runtime configuration is unchanged.", Tags: []string{"Flows"}, Parameters: idParam, RequestBody: &RequestBodySpec{Description: "Flow authoring fields and expected version", Required: true, SchemaRef: "#/components/schemas/FlowUpdateRequest"}, Responses: map[string]ResponseSpec{"200": {Description: "Diagram updated", ContentType: "application/json", SchemaRef: "#/components/schemas/Flow"}, "400": {Description: "Invalid request"}, "409": {Description: "Version conflict"}}},
				DELETE: &OperationSpec{Summary: "Delete a saved flow diagram", Description: "Deletes only the diagram; runtime configuration is unchanged.", Tags: []string{"Flows"}, Parameters: idParam, Responses: map[string]ResponseSpec{"204": {Description: "Diagram deleted"}}},
			},
			"/flows/{id}/validate":                  {POST: &OperationSpec{Summary: "Validate a flow diagram", Description: "Validates a saved diagram or strict request-body draft without changing configuration. The path supplies draft identity.", Tags: []string{"Flows"}, Parameters: idParam, RequestBody: &RequestBodySpec{Description: "Optional flow authoring fields", SchemaRef: "#/components/schemas/FlowValidateRequest"}, Responses: map[string]ResponseSpec{"200": {Description: "Validation result", ContentType: "application/json"}, "400": {Description: "Invalid request"}}}},
			"/flows/{id}/publish-component-configs": {POST: &OperationSpec{Summary: "Publish component configuration candidates", Description: "Compiles the saved diagram and upserts its component configurations. The current process remains unchanged; a restart is required when the published map differs from the sealed boot map.", Tags: []string{"Flows"}, Parameters: idParam, Responses: map[string]ResponseSpec{"200": {Description: "Published component configuration names", ContentType: "application/json", SchemaRef: "#/components/schemas/PublishComponentConfigsResponse"}, "400": {Description: "Diagram validation failed"}, "500": {Description: "Partial or complete persistence failure"}}}},
			"/flows/{id}/observations/metrics":      {GET: observation("Observe metrics for diagram component names", "Queries metrics for component names declared by the saved diagram; it does not assert ownership or activation.", "#/components/schemas/RuntimeMetricsResponse")},
			"/flows/{id}/observations/health":       {GET: observation("Observe health for diagram component names", "Queries health for component names declared by the saved diagram; it does not assert ownership or activation.", "#/components/schemas/RuntimeHealthResponse")},
			"/flows/{id}/observations/messages":     {GET: observation("Observe messages for diagram component names", "Filters message observations using names declared by the saved diagram; it does not assert ownership or activation.", "#/components/schemas/RuntimeMessagesResponse")},
		},
		Tags: []TagSpec{
			{Name: "Flows", Description: "Saved flow-diagram CRUD, validation, and explicit component-config publishing"},
			{Name: "Flow observations", Description: "Best-effort observation keyed by names declared in a saved diagram"},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(RuntimeHealthResponse{}),
			reflect.TypeOf(RuntimeMetricsResponse{}),
			reflect.TypeOf(RuntimeMessagesResponse{}),
			reflect.TypeOf(PublishComponentConfigsResponse{}),
			reflect.TypeOf(flowstore.Flow{}),
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(FlowCreateRequest{}),
			reflect.TypeOf(FlowUpdateRequest{}),
			reflect.TypeOf(FlowValidateRequest{}),
		},
	}
}

func (fs *FlowService) handleGetFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleGetFlow(w, r, r.PathValue("id"))
}

func (fs *FlowService) handleUpdateFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleUpdateFlow(w, r, r.PathValue("id"))
}

func (fs *FlowService) handleDeleteFlowWrapper(w http.ResponseWriter, r *http.Request) {
	fs.handleDeleteFlow(w, r, r.PathValue("id"))
}

func (fs *FlowService) handleListFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := fs.flowStore.List(r.Context())
	if err != nil {
		if strings.Contains(err.Error(), "no keys found") {
			fs.writeJSON(w, map[string]any{"flows": []any{}})
			return
		}
		fs.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	fs.writeJSON(w, map[string]any{"flows": flows})
}

func (fs *FlowService) handleCreateFlow(w http.ResponseWriter, r *http.Request) {
	var request FlowCreateRequest
	if err := decodeStrictJSON(r.Body, &request); err != nil {
		fs.writeJSONError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	flow := flowstore.Flow{
		ID:          generateFlowID(),
		Name:        request.Name,
		Description: request.Description,
		Nodes:       request.Nodes,
		Connections: request.Connections,
	}
	if err := fs.flowStore.Create(r.Context(), &flow); err != nil {
		fs.writeJSONError(w, "Failed to create flow", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(flow); err != nil {
		fs.logger.Error("Failed to encode flow response", "error", err)
	}
}

func (fs *FlowService) handleGetFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	flow, err := fs.flowStore.Get(r.Context(), flowID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fs.writeJSON(w, flow)
}

func (fs *FlowService) handleUpdateFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	var request FlowUpdateRequest
	if err := decodeStrictJSON(r.Body, &request); err != nil {
		fs.writeJSONError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	flow := flowstore.Flow{
		ID:          flowID,
		Name:        request.Name,
		Description: request.Description,
		Version:     request.ExpectedVersion,
		Nodes:       request.Nodes,
		Connections: request.Connections,
	}
	if err := fs.flowStore.Update(r.Context(), &flow); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			fs.writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		fs.writeJSONError(w, "Failed to update flow", http.StatusInternalServerError)
		return
	}
	fs.writeJSON(w, flow)
}

func (fs *FlowService) handleDeleteFlow(w http.ResponseWriter, r *http.Request, flowID string) {
	if err := fs.flowStore.Delete(r.Context(), flowID); err != nil {
		fs.writeJSONError(w, "Failed to delete flow", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublishComponentConfigsResponse reports exact progress. Published entries are
// upserts only; diagram omissions never imply component deletion.
type PublishComponentConfigsResponse struct {
	PersistedComponents []string `json:"persisted_components"`
	FailedComponent     string   `json:"failed_component,omitempty"`
	RuntimeUnchanged    bool     `json:"runtime_unchanged"`
	RestartRequired     bool     `json:"restart_required"`
	Error               string   `json:"error,omitempty"`
}

func (fs *FlowService) handlePublishComponentConfigs(w http.ResponseWriter, r *http.Request) {
	flow, err := fs.flowStore.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fs.writeJSONError(w, "Flow not found", http.StatusNotFound)
		return
	}
	configs, validation, err := fs.flowEngine.Compile(flow)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "validation_result": validation})
		return
	}

	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	response := PublishComponentConfigsResponse{
		PersistedComponents: make([]string, 0, len(names)),
		RuntimeUnchanged:    true,
	}
	for _, name := range names {
		if err := fs.configMgr.PutComponentToKV(r.Context(), name, configs[name]); err != nil {
			response.FailedComponent = name
			response.Error = err.Error()
			response.RestartRequired, _ = fs.configMgr.ComponentRestartRequired()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		response.PersistedComponents = append(response.PersistedComponents, name)
	}
	response.RestartRequired, err = fs.configMgr.ComponentRestartRequired()
	if err != nil {
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	fs.writeJSON(w, response)
}

func (fs *FlowService) handleValidateFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("id")
	var flow *flowstore.Flow
	if r.Body != nil && r.Body != http.NoBody {
		var request FlowValidateRequest
		if err := decodeStrictJSON(r.Body, &request); err != nil {
			fs.writeJSONError(w, fmt.Sprintf("Invalid JSON in request body: %v", err), http.StatusBadRequest)
			return
		}
		draft := flowstore.Flow{
			ID:          flowID,
			Name:        request.Name,
			Description: request.Description,
			Nodes:       request.Nodes,
			Connections: request.Connections,
		}
		flow = &draft
	} else {
		var err error
		flow, err = fs.flowStore.Get(r.Context(), flowID)
		if err != nil {
			fs.writeJSONError(w, "Flow not found", http.StatusNotFound)
			return
		}
	}
	result, err := fs.flowEngine.ValidateFlowDefinition(flow)
	if err != nil {
		fs.writeJSONError(w, fmt.Sprintf("Validation failed: %v", err), http.StatusBadRequest)
		return
	}
	fs.writeJSON(w, result)
}

func (fs *FlowService) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fs.logger.Error("Failed to encode JSON response", "error", err)
	}
}

func (fs *FlowService) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		fs.logger.Error("Failed to encode error response", "error", err)
	}
}

func generateFlowID() string { return uuid.New().String() }

func (fs *FlowService) startOverrideExpiryReporter(ctx context.Context) {
	if fs.configMgr == nil {
		return
	}
	fs.ensureOverrideExpiryReporter()
	go fs.overrideExpiry.run(ctx)
}

func (fs *FlowService) ensureOverrideExpiryReporter() {
	if fs.configMgr == nil || fs.overrideExpiry != nil {
		return
	}
	fs.overrideExpiry = newStreamOverrideExpiryReporter(func() *config.Config {
		safe := fs.configMgr.GetConfig()
		if safe == nil {
			return nil
		}
		return safe.Get()
	}, fs.logger.With("source", "flow-service.stream-overrides"))
}

// RegisterMetrics registers stream migration-override expiry reporting.
func (fs *FlowService) RegisterMetrics(registrar metric.MetricsRegistrar) error {
	fs.ensureOverrideExpiryReporter()
	if fs.overrideExpiry == nil {
		return nil
	}
	return fs.overrideExpiry.register(registrar)
}
