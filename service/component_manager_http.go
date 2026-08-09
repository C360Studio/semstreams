package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/types"
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
	mux.HandleFunc(prefix+"gaps", cm.handleFlowGaps)
	mux.HandleFunc(prefix+"paths", cm.handleFlowPaths)
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
					Description: "Returns the current configuration for a specific component",
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
					Summary:     "Get component FlowGraph",
					Description: "Returns the complete FlowGraph with nodes and edges for all managed components",
					Tags:        []string{"Components", "FlowGraph"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "FlowGraph with nodes and edges",
							ContentType: "application/json",
						},
					},
				},
			},
			"/validate": {
				GET: &OperationSpec{
					Summary:     "Validate component flow connectivity",
					Description: "Performs FlowGraph connectivity analysis for operational validation (used by E2E tests)",
					Tags:        []string{"Components", "FlowGraph"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Flow connectivity analysis results",
							ContentType: "application/json",
						},
					},
				},
			},
			"/gaps": {
				GET: &OperationSpec{
					Summary:     "Get component flow gaps",
					Description: "Returns disconnected nodes and orphaned ports in the component flow",
					Tags:        []string{"Components", "FlowGraph"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Component flow gaps and disconnected nodes",
							ContentType: "application/json",
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
		// Note: ComponentManager uses dynamic map[string]any responses and flowgraph types
		// Response types from flowgraph package would need separate handling
		ResponseTypes: nil,
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

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	components := make([]map[string]any, 0, len(cm.components))

	for name, mc := range cm.components {
		compInfo := map[string]any{
			"name":  name,
			"state": mc.State.String(),
		}

		// Report the effective config the component is actually running, from the
		// single source of truth refreshed on every write path (gh#522).
		compInfo["component"] = mc.Config.Name    // Component factory name (e.g., "udp", "graph-processor")
		compInfo["type"] = string(mc.Config.Type) // Component category (input/processor/output/storage/gateway)
		compInfo["enabled"] = mc.Config.Enabled

		// Add health status
		healthStatus := mc.Component.Health()
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
// types with their schemas. Both the GET /components/types HTTP handler and
// the list_components agent tool call this — keep the shape in one place.
func BuildComponentTypeCatalog(registry *component.Registry, logger *slog.Logger) []map[string]any {
	// Get all registered factories from the component registry
	factories := registry.ListFactories()

	// Map of component IDs to human-readable display names
	displayNames := map[string]string{
		"udp":               "UDP Input",
		"websocket":         "WebSocket Output",
		"robotics":          "Robotics Processor",
		"graph-processor":   "Graph Processor",
		"rule-processor":    "Rule Processor",
		"context-processor": "Context Processor",
		"objectstore":       "Object Store",
	}

	// Convert to a slice of component type metadata (flat array format per OpenAPI contract)
	componentTypes := make([]map[string]any, 0, len(factories))
	for id, registration := range factories {
		// Use display name if available, otherwise use ID
		displayName := id
		if name, exists := displayNames[id]; exists {
			displayName = name
		}

		// Get component schema from registry
		schema, err := registry.GetComponentSchema(id)
		if err != nil {
			// Log warning but continue - component may not have schema
			logger.Warn("Failed to get schema for component type", "component_type", id, "error", err)
		}

		componentTypes = append(componentTypes, map[string]any{
			"id":          id,                // Component ID (map key)
			"name":        displayName,       // Human-readable display name
			"type":        registration.Type, // input, processor, output, storage
			"protocol":    registration.Protocol,
			"domain":      registration.Domain, // Business domain (robotics, semantic, network, storage)
			"description": registration.Description,
			"version":     registration.Version,
			"category":    registration.Type, // Map type to category for frontend
			"schema":      schema,            // Component configuration schema
		})
	}

	return componentTypes
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
	// Get all registered factories from the component registry
	factories := cm.registry.ListFactories()

	// Find the requested component type
	registration, exists := factories[componentType]
	if !exists {
		http.Error(w, fmt.Sprintf(`{"error":"Component type %s not found"}`, componentType), http.StatusNotFound)
		return
	}

	// Map of component IDs to human-readable display names
	displayNames := map[string]string{
		"udp":               "UDP Input",
		"websocket":         "WebSocket Output",
		"robotics":          "Robotics Processor",
		"graph-processor":   "Graph Processor",
		"rule-processor":    "Rule Processor",
		"context-processor": "Context Processor",
		"objectstore":       "Object Store",
	}

	// Use display name if available, otherwise use ID
	displayName := componentType
	if name, exists := displayNames[componentType]; exists {
		displayName = name
	}

	// Get component schema from registry
	schema, err := cm.registry.GetComponentSchema(componentType)
	if err != nil {
		// Log warning but continue - component may not have schema
		cm.logger.Warn("Failed to get schema for component type", "component_type", componentType, "error", err)
	}

	// Return single component type metadata
	response := map[string]any{
		"id":          componentType,
		"name":        displayName,
		"type":        registration.Type,
		"protocol":    registration.Protocol,
		"domain":      registration.Domain, // Business domain (robotics, semantic, network, storage)
		"description": registration.Description,
		"version":     registration.Version,
		"category":    registration.Type,
		"schema":      schema,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error("Failed to encode component type", "error", err)
	}
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

	cm.mu.RLock()
	mc, exists := cm.components[componentName]
	defer cm.mu.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	status := map[string]any{
		"name":        componentName,
		"state":       mc.State.String(),
		"start_order": mc.StartOrder,
	}

	// Effective config from the single source of truth (gh#522).
	status["type"] = string(mc.Config.Type)
	status["enabled"] = mc.Config.Enabled

	// Add health information
	healthStatus := mc.Component.Health()
	status["healthy"] = healthStatus.Healthy
	if healthStatus.LastError != "" {
		status["last_error"] = healthStatus.LastError
		status["error_count"] = healthStatus.ErrorCount
	}
	if healthStatus.Uptime > 0 {
		status["uptime_seconds"] = healthStatus.Uptime.Seconds()
	}

	// Add last error if present (avoid duplicate if already set from health)
	if mc.LastError != nil && healthStatus.LastError == "" {
		status["lifecycle_error"] = mc.LastError.Error()
	}

	// Add debug information if requested and component supports it
	if includeDebug {
		if debugProvider, ok := mc.Component.(component.DebugStatusProvider); ok {
			status["debug"] = debugProvider.DebugStatus()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		cm.logger.Error("Failed to encode component status", "error", err)
	}
}

// handleComponentConfig handles component configuration GET and PUT requests
func (cm *ComponentManager) handleComponentConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cm.handleGetComponentConfig(w, r)
	case http.MethodPut:
		cm.handlePutComponentConfig(w, r)
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

	// Return the effective config the component is actually running, from the
	// single source of truth refreshed on every write path — create, KV-restart,
	// and live-PUT — so a KV-driven restart does not leave a stale body (gh#522).
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

// handlePutComponentConfig updates component configuration with schema validation
func (cm *ComponentManager) handlePutComponentConfig(w http.ResponseWriter, r *http.Request) {
	// Extract and validate component name from URL path
	componentName, valid := extractComponentName(r.URL.Path)
	if !valid {
		http.Error(w, "Invalid component name", http.StatusBadRequest)
		return
	}
	unlock := cm.lockInstanceOperation(componentName)
	defer unlock()

	// Check if component exists, and capture the factory name from the component's
	// effective config under the same lock (gh#522: the single source of truth, not
	// the boot-time componentConfigs which is stale for a KV-restarted or
	// runtime-added component).
	cm.mu.RLock()
	comp, exists := cm.components[componentName]
	var componentType string
	var effectiveConfig types.ComponentConfig
	if exists {
		componentType = comp.Config.Name // Factory name
		effectiveConfig = cloneComponentConfig(comp.Config)
	}
	cm.mu.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	// Parse request body
	var req struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if componentType == "" {
		cm.logger.Warn("Component type not found, skipping validation", "component_name", componentName)
		http.Error(w, "Component type not found", http.StatusInternalServerError)
		return
	}

	// Validate configuration against schema if config manager is available
	if cm.configManager != nil {
		validationErrors := component.ValidateComponentConfig(
			r.Context(),
			cm.logger,
			cm.registry,
			componentType,
			req.Config,
		)

		if len(validationErrors) > 0 {
			// Return structured validation errors (FR-005)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": validationErrors,
			})
			return
		}
	}

	// Prove declaration neutrality against a separately constructed candidate
	// before invoking either live-mutation contract or changing retained config.
	proposedConfig := effectiveConfig
	proposedConfig.Config = append(json.RawMessage(nil), req.Config...)
	proof, err := cm.registry.ValidateDeclarationUpdate(
		componentadmission.Access{}, componentName, proposedConfig, cm.buildComponentDependencies(),
	)
	if err != nil {
		var declarationChange *component.DeclarationChangeRequiresReplacementError
		if errors.As(err, &declarationChange) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":  declarationChange.Code,
				"error": declarationChange.Error(),
			})
			return
		}
		cm.logger.Error("Failed to validate component declaration update",
			"component_name", componentName, "error", err)
		http.Error(w, fmt.Sprintf("Failed to validate declaration: %v", err), http.StatusBadRequest)
		return
	}
	if err := cm.registry.ConfirmDeclarationUpdate(componentadmission.Access{}, proof); err != nil {
		http.Error(w, fmt.Sprintf("Component changed during declaration validation: %v", err), http.StatusConflict)
		return
	}

	// Configuration passed schema validation. Apply it to the running component
	// via whichever runtime-reconfig contract it implements, learning whether it
	// was applied live (gh#455). Apply BEFORE persisting so a component's own
	// validate/apply rejection leaves no stored-but-unapplied config that a
	// restart would silently load.
	applied, err := cm.applyRuntimeConfig(r.Context(), comp.Component, req.Config)
	if err != nil {
		cm.logger.Error("Failed to apply config update", "component_name", componentName, "error", err)
		// A component's own validation rejection is a client error (structured
		// 400), mirroring the schema-validation path above; a genuine apply
		// failure is a server error (500). Either way nothing was stored (apply
		// runs before the persist below), so a restart won't load it.
		if errors.Is(err, errReconfigValidation) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []string{err.Error()},
			})
			return
		}
		http.Error(w, fmt.Sprintf("Failed to apply config: %v", err), http.StatusInternalServerError)
		return
	}
	if err := cm.registry.ConfirmDeclarationUpdate(componentadmission.Access{}, proof); err != nil {
		http.Error(w, fmt.Sprintf("Component changed during config apply: %v", err), http.StatusConflict)
		return
	}

	// Update the ComponentManager's in-memory view of the config, only after a
	// successful apply or an explicit no-hook accept, so the GET-config read stays
	// consistent with what was applied. NOTE: this is the manager's in-memory state
	// only — this endpoint does NOT durably persist to the config KV store (that
	// write is one-directional KV→cm here, and an in-handler KV write is a known
	// deadlock hazard — gh#388), so the change does not survive a restart. That is
	// exactly why the response below does not promise a restart-time apply.
	//
	// The retained ManagedComponent.Config is the single source of truth for the
	// component's effective config (gh#522): it backs the GET /config read AND the
	// KV-watch idempotency guard (gh#520). Refreshing it here keeps both correct —
	// GET returns the live-applied body, and a later KV re-push of the same config
	// is a no-op rather than a spurious restart.
	cm.mu.Lock()
	if mc, ok := cm.components[componentName]; ok && mc == comp {
		mc.Config.Config = append(json.RawMessage(nil), req.Config...)
	} else {
		cm.mu.Unlock()
		http.Error(w, "Component changed during config commit", http.StatusConflict)
		return
	}
	cm.mu.Unlock()

	// Honest response (gh#455): report whether the change was applied to the
	// running component live, instead of an unconditional success that implies a
	// live apply. A no-hook component is NOT reconfigured live, and this endpoint
	// does not persist for restart, so we do not claim a restart-time apply.
	message := "Configuration applied to the running component"
	if !applied {
		message = "Component does not support live runtime reconfiguration; the update was not applied to the running component"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"message": message,
		"applied": applied,
	})
}

// applyRuntimeConfig applies a schema-validated raw config to a running
// component via whichever runtime-reconfig contract it implements, returning
// whether the change was applied live. It probes two contracts in order:
//
//  1. the component-side UpdateConfig(ctx, json.RawMessage) (existing behavior);
//  2. the service-flavored reconfig method pair
//     ValidateConfigUpdate/ApplyConfigUpdate over a map[string]any, so a
//     component implementing it (e.g. processor/rule) is reachable via the
//     ComponentManager HTTP API (gh#455).
//
// It type-asserts the reconfig method pair rather than a broader service
// contract because a component's ConfigSchema returns component.ConfigSchema.
// The narrow anonymous interface matches exactly the methods this bridge calls.
//
// A component implementing neither contract returns (false, nil); the caller
// reports applied:false. On the method-pair path, ValidateConfigUpdate runs
// before ApplyConfigUpdate — a validation failure returns an error wrapping
// errReconfigValidation (mapped to a 400 by the caller) and applies nothing.
func (cm *ComponentManager) applyRuntimeConfig(ctx context.Context, comp component.Discoverable, raw json.RawMessage) (bool, error) {
	if configurable, ok := comp.(interface {
		UpdateConfig(ctx context.Context, config json.RawMessage) error
	}); ok {
		if err := configurable.UpdateConfig(ctx, raw); err != nil {
			return false, err
		}
		return true, nil
	}

	if rc, ok := comp.(interface {
		ValidateConfigUpdate(changes map[string]any) error
		ApplyConfigUpdate(changes map[string]any) error
	}); ok {
		var changes map[string]any
		if err := json.Unmarshal(raw, &changes); err != nil {
			return false, fmt.Errorf("%w: config is not a JSON object: %w", errReconfigValidation, err)
		}
		if err := rc.ValidateConfigUpdate(changes); err != nil {
			return false, fmt.Errorf("%w: %w", errReconfigValidation, err)
		}
		if err := rc.ApplyConfigUpdate(changes); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// errReconfigValidation marks a client-side rejection of a runtime config update
// (a malformed body or a component's ValidateConfigUpdate failure) so the PUT
// handler maps it to a structured 400. An ApplyConfigUpdate failure after
// successful validation is a server-side error (bare, → 500).
var errReconfigValidation = errors.New("runtime config rejected by component validation")

// =============================================================================
// FlowGraph HTTP Handlers
// =============================================================================

// handleFlowGraph returns the complete FlowGraph with nodes and edges
func (cm *ComponentManager) handleFlowGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	graph, err := cm.GetFlowGraph()
	if err != nil {
		cm.logger.Error("Failed to build FlowGraph", "error", err)
		http.Error(w, "Failed to build flow graph", http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"nodes": graph.GetNodes(),
		"edges": graph.GetEdges(),
		"metadata": map[string]any{
			"timestamp":  time.Now().UTC(),
			"node_count": len(graph.GetNodes()),
			"edge_count": len(graph.GetEdges()),
			"graph_type": "component_flow",
		},
	}

	// Buffer JSON encoding to catch errors before writing response
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		cm.logger.Error("Failed to encode FlowGraph response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(buf.Bytes()); err != nil {
		cm.logger.Error("Failed to write FlowGraph response", "error", err)
	}
}

// handleFlowValidation performs FlowGraph connectivity analysis for operational validation
func (cm *ComponentManager) handleFlowValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	graph, err := cm.GetFlowGraph()
	if err != nil {
		cm.logger.Error("Failed to build FlowGraph for validation", "error", err)
		http.Error(w, "Failed to build flow graph", http.StatusInternalServerError)
		return
	}
	analysis, err := cm.ValidateFlowConnectivity()
	if err != nil {
		cm.logger.Error("Failed to validate FlowGraph", "error", err)
		http.Error(w, "Failed to validate flow graph", http.StatusInternalServerError)
		return
	}

	// Check for stream requirement issues (JetStream subscribers connected to NATS publishers)
	streamWarnings := graph.ValidateStreamRequirements()

	// Determine validation status including stream requirement issues
	validationStatus := analysis.ValidationStatus
	if len(streamWarnings) > 0 {
		validationStatus = "critical" // Stream requirement issues are critical
	}

	// Add additional metadata for E2E testing
	response := map[string]any{
		"timestamp":            time.Now().UTC(),
		"validation_status":    validationStatus,
		"connected_components": analysis.ConnectedComponents,
		"connected_edges":      analysis.ConnectedEdges,
		"disconnected_nodes":   analysis.DisconnectedNodes,
		"orphaned_ports":       analysis.OrphanedPorts,
		"stream_warnings":      streamWarnings,
		"summary": map[string]any{
			"total_components":        len(graph.GetNodes()),
			"total_connections":       len(analysis.ConnectedEdges),
			"component_groups":        len(analysis.ConnectedComponents),
			"orphaned_port_count":     len(analysis.OrphanedPorts),
			"disconnected_node_count": len(analysis.DisconnectedNodes),
			"stream_warning_count":    len(streamWarnings),
			"has_stream_issues":       len(streamWarnings) > 0,
		},
	}

	// Buffer JSON encoding to catch errors before writing response
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		cm.logger.Error("Failed to encode flow validation response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Set appropriate HTTP status based on validation results
	if analysis.ValidationStatus == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		// Return 200 but indicate warnings in the response
		// E2E tests can check the validation_status field
		w.WriteHeader(http.StatusOK)
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		cm.logger.Error("Failed to write flow validation response", "error", err)
	}
}

// handleFlowGaps returns disconnected nodes and orphaned ports
func (cm *ComponentManager) handleFlowGaps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	analysis, err := cm.ValidateFlowConnectivity()
	if err != nil {
		cm.logger.Error("Failed to validate FlowGraph gaps", "error", err)
		http.Error(w, "Failed to validate flow graph", http.StatusInternalServerError)
		return
	}
	objectStoreGaps, err := cm.DetectObjectStoreGaps()
	if err != nil {
		cm.logger.Error("Failed to inspect object-store flow gaps", "error", err)
		http.Error(w, "Failed to inspect flow gaps", http.StatusInternalServerError)
		return
	}

	// Categorize orphaned ports by severity
	criticalPorts := 0
	optionalPorts := 0
	for _, port := range analysis.OrphanedPorts {
		switch port.Issue {
		case "no_publishers", "no_subscribers":
			// Stream connections are critical only if required
			if port.Pattern == component.PatternStream && port.Required {
				criticalPorts++
			} else {
				optionalPorts++
			}
		case "optional_api_unused", "optional_index_unwatched", "optional_interface_unused":
			// These are always optional
			optionalPorts++
		}
	}

	// Only count critical issues as true gaps
	criticalGaps := len(analysis.DisconnectedNodes) + criticalPorts

	response := map[string]any{
		"timestamp":          time.Now().UTC(),
		"disconnected_nodes": analysis.DisconnectedNodes,
		"orphaned_ports":     analysis.OrphanedPorts,
		"objectstore_gaps":   objectStoreGaps,
		"summary": map[string]any{
			"total_gaps":          criticalGaps, // Only critical issues
			"critical_gaps":       criticalGaps,
			"optional_gaps":       optionalPorts,
			"disconnected_count":  len(analysis.DisconnectedNodes),
			"orphaned_port_count": len(analysis.OrphanedPorts),
			"critical_port_count": criticalPorts,
			"optional_port_count": optionalPorts,
			"objectstore_gaps":    len(objectStoreGaps),
			"has_issues":          criticalGaps > 0 || len(objectStoreGaps) > 0, // Only critical issues
		},
	}

	// Buffer JSON encoding to catch errors before writing response
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		cm.logger.Error("Failed to encode flow gaps response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(buf.Bytes()); err != nil {
		cm.logger.Error("Failed to write flow gaps response", "error", err)
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
