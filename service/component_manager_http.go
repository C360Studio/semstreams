package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/health"
)

func init() {
	RegisterOpenAPISpec("component-manager", componentManagerOpenAPISpec())
}

// Ensure ComponentManager implements HTTPHandler interface
var _ HTTPHandler = (*ComponentManager)(nil)

// extractComponentName safely extracts and validates a component name from the URL path
func extractComponentName(path string) (string, bool) {
	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")

	// Split path and get last segment
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", false
	}

	name := parts[len(parts)-1]

	// Validate component name
	if name == "" || name == "." || name == ".." {
		return "", false
	}

	// Decode URL encoding
	decoded, err := url.QueryUnescape(name)
	if err != nil {
		return "", false
	}

	// Check for path traversal attempts
	if strings.Contains(decoded, "/") || strings.Contains(decoded, "\\") {
		return "", false
	}

	return decoded, true
}

// RegisterHTTPHandlers registers HTTP endpoints for the ComponentManager service
func (cm *ComponentManager) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Ensure prefix ends with /
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	cm.logger.Info("ComponentManager HTTP handlers registered", "prefix", prefix)

	// Register endpoints
	mux.HandleFunc(prefix+"health", cm.handleComponentsHealth)
	mux.HandleFunc(prefix+"list", cm.handleComponentsList)
	mux.HandleFunc(prefix+"types/", cm.handleComponentTypeByID)
	mux.HandleFunc(prefix+"types", cm.handleComponentTypes)
	mux.HandleFunc(prefix+"status/", cm.handleComponentStatus)
	mux.HandleFunc(prefix+"config/", cm.handleComponentConfig)

	// FlowGraph endpoints
	mux.HandleFunc(prefix+"flowgraph", cm.handleFlowGraph)
	mux.HandleFunc(prefix+"validate", cm.handleFlowValidation)
	mux.HandleFunc(prefix+"paths", cm.handleFlowPaths)
}

// registerStartupHTTPHandlers registers only read-only component diagnostics.
// It deliberately omits configuration, type, flowgraph, and gateway routes.
func (cm *ComponentManager) registerStartupHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"health", cm.handleComponentsHealth)
	mux.HandleFunc(prefix+"list", cm.handleComponentsList)
	mux.HandleFunc(prefix+"status/", cm.handleComponentStatus)
}

// OpenAPISpec returns the OpenAPI specification for ComponentManager endpoints
func (cm *ComponentManager) OpenAPISpec() *OpenAPISpec {
	return componentManagerOpenAPISpec()
}

// componentManagerOpenAPISpec returns the OpenAPI specification for ComponentManager endpoints.
// This is a standalone function so it can be called from init() for registration.
func componentManagerOpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Paths: map[string]PathSpec{
			"/health": {
				GET: &OperationSpec{
					Summary:     "Get component health status",
					Description: "Returns aggregated health status for all managed components",
					Tags:        []string{"Components"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Component health information",
							ContentType: "application/json",
						},
					},
				},
			},
			"/list": {
				GET: &OperationSpec{
					Summary:     "List all components",
					Description: "Returns a list of all managed components with basic information",
					Tags:        []string{"Components"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "List of components",
							ContentType: "application/json",
						},
					},
				},
			},
			"/types": {
				GET: &OperationSpec{
					Summary:     "List available component types",
					Description: "Returns array of component metadata including schemas",
					Tags:        []string{"Components"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Array of component types",
							ContentType: "application/json",
						},
					},
				},
			},
			"/types/{id}": {
				GET: &OperationSpec{
					Summary:     "Get component type by ID",
					Description: "Returns metadata and schema for a specific component type",
					Tags:        []string{"Components"},
					Parameters: []ParameterSpec{
						{
							Name:        "id",
							In:          "path",
							Required:    true,
							Description: "Component type ID",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Component type metadata",
							ContentType: "application/json",
						},
						"404": {
							Description: "Component type not found",
						},
					},
				},
			},
			"/status/{name}": {
				GET: &OperationSpec{
					Summary:     "Get component status",
					Description: "Returns detailed status for a specific component",
					Tags:        []string{"Components"},
					Parameters: []ParameterSpec{
						{
							Name:        "name",
							In:          "path",
							Required:    true,
							Description: "Component name",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Component status",
							ContentType: "application/json",
						},
						"404": {
							Description: "Component not found",
						},
					},
				},
			},
			"/config/{name}": {
				GET: &OperationSpec{
					Summary:     "Get component configuration",
					Description: "Returns the constructor-captured boot configuration for a specific component",
					Tags:        []string{"Components"},
					Parameters: []ParameterSpec{
						{
							Name:        "name",
							In:          "path",
							Required:    true,
							Description: "Component name",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Component configuration",
							ContentType: "application/json",
						},
						"404": {
							Description: "Component not found",
						},
					},
				},
			},
			"/flowgraph": {
				GET: &OperationSpec{
					Summary:     "Get the composition graph projection",
					Description: "Returns the boot composition's graph projection (nodes with resolved ports, derived edges) as retained at boot; Mermaid when format=mermaid",
					Tags:        []string{"Components", "FlowGraph"},
					Parameters: []ParameterSpec{
						{
							Name:        "format",
							In:          "query",
							Required:    false,
							Description: "json (default) or mermaid",
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Composition graph projection",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Graph",
						},
					},
				},
			},
			"/validate": {
				GET: &OperationSpec{
					Summary:     "Get the boot composition findings",
					Description: "Returns the composition validation result computed over the admitted composition at boot (ADR-100), verbatim",
					Tags:        []string{"Components", "FlowGraph"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Composition validation result",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Result",
						},
					},
				},
			},
			"/paths": {
				GET: &OperationSpec{
					Summary:     "Get component data paths",
					Description: "Returns data paths from input components to all reachable components",
					Tags:        []string{"Components", "FlowGraph"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Data paths through component graph",
							ContentType: "application/json",
						},
					},
				},
			},
		},
		Tags: []TagSpec{
			{
				Name:        "Components",
				Description: "Component management and monitoring endpoints",
			},
			{
				Name:        "FlowGraph",
				Description: "Component flow analysis and connectivity validation endpoints",
			},
		},
		// The composition projections have typed responses; the remaining
		// operations use dynamic map[string]any responses.
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(composition.Result{}),
			reflect.TypeOf(composition.Graph{}),
		},
	}
}

// handleComponentsHealth returns aggregated health status for all components
func (cm *ComponentManager) handleComponentsHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get component health statuses
	componentHealthMap := cm.GetComponentHealth()

	// Convert component.HealthStatus to health.Status
	var healthStatuses []health.Status
	for name, compHealth := range componentHealthMap {
		healthStatuses = append(healthStatuses,
			health.FromComponentHealth(name, compHealth))
	}

	// Aggregate all component health
	overallHealth := health.Aggregate("components", healthStatuses)

	// Create response with overall and individual statuses
	response := struct {
		Overall    health.Status   `json:"overall"`
		Components []health.Status `json:"components"`
		Total      int             `json:"total"`
	}{
		Overall:    overallHealth,
		Components: healthStatuses,
		Total:      len(healthStatuses),
	}

	// Set HTTP status based on overall health
	w.Header().Set("Content-Type", "application/json")
	if overallHealth.IsUnhealthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if overallHealth.IsDegraded() {
		w.WriteHeader(http.StatusOK) // 200 but degraded in body
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error("Failed to encode health response", "error", err)
	}
}

// handleComponentsList returns a list of all managed components
func (cm *ComponentManager) handleComponentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses := cm.GetComponentStatus()
	components := make([]map[string]any, 0, len(statuses))

	for name, status := range statuses {
		cfg := cm.componentConfigs[name]
		compInfo := map[string]any{
			"name":  name,
			"state": status.State.String(),
		}

		// Report the effective config the component is actually running, from the
		// single source of truth refreshed on every write path (gh#522).
		compInfo["component"] = cfg.Name    // Component factory name (e.g., "udp", "graph-processor")
		compInfo["type"] = string(cfg.Type) // Component category (input/processor/output/storage/gateway)
		compInfo["enabled"] = cfg.Enabled

		healthStatus := status.Health
		compInfo["healthy"] = healthStatus.Healthy
		if healthStatus.LastError != "" {
			compInfo["last_error"] = healthStatus.LastError
		}

		components = append(components, compInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(components); err != nil {
		cm.logger.Error("Failed to encode components list", "error", err)
	}
}

// BuildComponentTypeCatalog returns the list of registered component factory
// types with their schemas and default ports. Both the GET /components/types
// HTTP handler and the list_components agent tool call this; the one home for
// the shape is composition.Catalog, projected here to the flat map array the
// OpenAPI contract has always served.
func BuildComponentTypeCatalog(registry *component.Registry, logger *slog.Logger) []map[string]any {
	entries := composition.Catalog(registry)
	componentTypes := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		projected, err := catalogEntryMap(entry)
		if err != nil {
			logger.Warn("Failed to project catalog entry", "component_type", entry.ID, "error", err)
			continue
		}
		componentTypes = append(componentTypes, projected)
	}
	return componentTypes
}

// catalogEntryMap round-trips one catalog entry through its JSON wire shape.
func catalogEntryMap(entry composition.CatalogEntry) (map[string]any, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var projected map[string]any
	if err := json.Unmarshal(data, &projected); err != nil {
		return nil, err
	}
	return projected, nil
}

// handleComponentTypes returns available component types from the registry
func (cm *ComponentManager) handleComponentTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	componentTypes := BuildComponentTypeCatalog(cm.registry, cm.logger)

	// Return flat array (matches OpenAPI contract in specs/008-fix-ui-code/contracts/component-types-api.yaml)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(componentTypes); err != nil {
		cm.logger.Error("Failed to encode component types", "error", err)
	}
}

// handleComponentTypeByID returns metadata and schema for a specific component type
func (cm *ComponentManager) handleComponentTypeByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract component type from URL path
	componentType, valid := extractComponentName(r.URL.Path)
	if !valid {
		http.Error(w, "Invalid component type", http.StatusBadRequest)
		return
	}
	for _, entry := range composition.Catalog(cm.registry) {
		if entry.ID != componentType {
			continue
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entry); err != nil {
			cm.logger.Error("Failed to encode component type", "error", err)
		}
		return
	}
	http.Error(w, fmt.Sprintf(`{"error":"Component type %s not found"}`, componentType), http.StatusNotFound)
}

// handleComponentStatus returns detailed status for a specific component
func (cm *ComponentManager) handleComponentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract and validate component name from URL path
	componentName, valid := extractComponentName(r.URL.Path)
	if !valid {
		http.Error(w, "Invalid component name", http.StatusBadRequest)
		return
	}

	// Check for debug parameter
	debugParam := r.URL.Query().Get("debug")
	includeDebug := debugParam == "true"

	componentStatus, exists := cm.GetComponentStatus()[componentName]

	if !exists {
		http.NotFound(w, r)
		return
	}

	status := map[string]any{
		"name":  componentName,
		"state": componentStatus.State.String(),
	}

	bootConfig := cm.componentConfigs[componentName]
	status["type"] = string(bootConfig.Type)
	status["enabled"] = bootConfig.Enabled

	healthStatus := componentStatus.Health
	status["healthy"] = healthStatus.Healthy
	if healthStatus.LastError != "" {
		status["last_error"] = healthStatus.LastError
		status["error_count"] = healthStatus.ErrorCount
	}
	if healthStatus.Uptime > 0 {
		status["uptime_seconds"] = healthStatus.Uptime.Seconds()
	}

	// Add last error if present (avoid duplicate if already set from health)
	if componentStatus.LastError != nil && healthStatus.LastError == "" {
		status["lifecycle_error"] = componentStatus.LastError.Error()
	}

	// Add debug information if requested and component supports it
	if includeDebug {
		_ = cm.withComponents(func(components map[string]*component.ManagedComponent) error {
			managed := components[componentName]
			if managed != nil {
				if debugProvider, ok := managed.Component.(component.DebugStatusProvider); ok {
					status["debug"] = debugProvider.DebugStatus()
				}
			}
			return nil
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		cm.logger.Error("Failed to encode component status", "error", err)
	}
}

// handleComponentConfig exposes boot-effective configuration as a value-only
// observation. Mutations belong to desired configuration and take effect only
// on a later process boot.
func (cm *ComponentManager) handleComponentConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cm.handleGetComponentConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetComponentConfig returns the current configuration for a specific component
func (cm *ComponentManager) handleGetComponentConfig(w http.ResponseWriter, r *http.Request) {
	// Extract and validate component name from URL path
	componentName, valid := extractComponentName(r.URL.Path)
	if !valid {
		http.Error(w, "Invalid component name", http.StatusBadRequest)
		return
	}

	// Get component configuration from stored configs
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check if component exists
	mc, exists := cm.components[componentName]
	if !exists {
		http.NotFound(w, r)
		return
	}

	// Return the immutable configuration selected for this boot.
	compConfig := mc.Config
	config := any(map[string]any{
		"type":    compConfig.Type,
		"name":    compConfig.Name,
		"enabled": compConfig.Enabled,
		"config":  json.RawMessage(compConfig.Config),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(config); err != nil {
		cm.logger.Error("Failed to encode component config", "error", err)
	}
}

// =============================================================================
// FlowGraph HTTP Handlers
// =============================================================================

// handleFlowGraph serves the boot composition's graph projection, as JSON by
// default and as Mermaid when format=mermaid. It is a projection of the result
// retained at Initialize; nothing is recomputed here.
func (cm *ComponentManager) handleFlowGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := cm.bootCompositionResult()
	if result == nil {
		http.Error(w, "composition not initialized", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Query().Get("format") == "mermaid" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := w.Write([]byte(composition.Mermaid(result.Graph))); err != nil {
			cm.logger.Error("Failed to write Mermaid projection", "error", err)
		}
		return
	}
	cm.writeJSON(w, result.Graph, "composition graph")
}

// handleFlowValidation serves the composition result retained at boot
// verbatim. The status, severities, and ordering are the library's; this
// handler computes nothing of its own.
func (cm *ComponentManager) handleFlowValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := cm.bootCompositionResult()
	if result == nil {
		http.Error(w, "composition not initialized", http.StatusServiceUnavailable)
		return
	}
	cm.writeJSON(w, result, "composition result")
}

// writeJSON buffers the encoding so an encode failure is a 500, not a
// half-written body.
func (cm *ComponentManager) writeJSON(w http.ResponseWriter, value any, what string) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(value); err != nil {
		cm.logger.Error("Failed to encode "+what, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(buf.Bytes()); err != nil {
		cm.logger.Error("Failed to write "+what, "error", err)
	}
}

// handleFlowPaths returns data paths from input components to all reachable components
func (cm *ComponentManager) handleFlowPaths(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	paths, err := cm.GetFlowPaths()
	if err != nil {
		cm.logger.Error("Failed to build FlowGraph paths", "error", err)
		http.Error(w, "Failed to build flow paths", http.StatusInternalServerError)
		return
	}

	// Calculate path statistics
	totalPaths := len(paths)
	maxPathLength := 0
	totalComponents := 0

	for _, path := range paths {
		if len(path) > maxPathLength {
			maxPathLength = len(path)
		}
		totalComponents += len(path)
	}

	var avgPathLength float64
	if totalPaths > 0 {
		avgPathLength = float64(totalComponents) / float64(totalPaths)
	}

	response := map[string]any{
		"timestamp": time.Now().UTC(),
		"paths":     paths,
		"statistics": map[string]any{
			"input_component_count": totalPaths,
			"max_path_length":       maxPathLength,
			"avg_path_length":       avgPathLength,
			"total_reachable":       totalComponents,
		},
	}

	// Buffer JSON encoding to catch errors before writing response
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		cm.logger.Error("Failed to encode flow paths response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(buf.Bytes()); err != nil {
		cm.logger.Error("Failed to write flow paths response", "error", err)
	}
}
