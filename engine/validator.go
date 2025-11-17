package flowengine

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/component/flowgraph"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/natsclient"
)

// Validator provides flow validation using FlowGraph analysis
type Validator struct {
	componentRegistry *component.Registry
	natsClient        *natsclient.Client
	logger            *slog.Logger
}

// NewValidator creates a new flow validator with component registry and NATS client.
// The validator performs structural, type, and semantic validation of flow definitions
// before deployment to ensure they can be safely executed.
func NewValidator(registry *component.Registry, natsClient *natsclient.Client, logger *slog.Logger) *Validator {
	return &Validator{
		componentRegistry: registry,
		natsClient:        natsClient,
		logger:            logger,
	}
}

// ValidationResult contains the results of flow validation
type ValidationResult struct {
	Status                string                 `json:"validation_status"` // "valid", "warnings", "errors"
	Errors                []ValidationIssue      `json:"errors"`
	Warnings              []ValidationIssue      `json:"warnings"`
	Nodes                 []ValidatedNode        `json:"nodes"`                  // Nodes with port information
	DiscoveredConnections []DiscoveredConnection `json:"discovered_connections"` // Auto-discovered edges
}

// ValidatedNode represents a flow node with its port information
type ValidatedNode struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	InputPorts  []ValidatedPort `json:"input_ports"`
	OutputPorts []ValidatedPort `json:"output_ports"`
}

// ValidatedPort represents a port with validation information
type ValidatedPort struct {
	Name         string `json:"name"`
	Direction    string `json:"direction"`
	Type         string `json:"type"` // Interface contract type (e.g., "message.Storable")
	Required     bool   `json:"required"`
	ConnectionID string `json:"connection_id"` // NATS subject, network address, etc.
	Pattern      string `json:"pattern"`       // stream, request, watch, api
	Description  string `json:"description"`   // Port description
}

// DiscoveredConnection represents an auto-discovered connection between ports
type DiscoveredConnection struct {
	SourceNodeID string `json:"source_node_id"`
	SourcePort   string `json:"source_port"`
	TargetNodeID string `json:"target_node_id"`
	TargetPort   string `json:"target_port"`
	ConnectionID string `json:"connection_id"`
	Pattern      string `json:"pattern"`
}

// ValidationIssue represents a single validation problem
type ValidationIssue struct {
	Type          string   `json:"type"`     // "orphaned_port", "disconnected_node", "unknown_component", etc.
	Severity      string   `json:"severity"` // "error", "warning"
	ComponentName string   `json:"component_name"`
	PortName      string   `json:"port_name,omitempty"`
	Message       string   `json:"message"`
	Suggestions   []string `json:"suggestions,omitempty"`
}

// ValidateFlow performs comprehensive flow validation using FlowGraph
func (v *Validator) ValidateFlow(flow *flowstore.Flow) (*ValidationResult, error) {
	result := &ValidationResult{
		Status:   "valid",
		Errors:   []ValidationIssue{},
		Warnings: []ValidationIssue{},
	}

	v.logger.Debug("Starting flow validation",
		"flow_id", flow.ID,
		"node_count", len(flow.Nodes))

	// Reject empty flows
	if len(flow.Nodes) == 0 {
		v.logger.Debug("Rejecting empty flow")
		result.Status = "errors"
		result.Errors = append(result.Errors, ValidationIssue{
			Type:          "empty_flow",
			Severity:      "error",
			ComponentName: "(none)",
			Message:       "Flow must contain at least one component",
			Suggestions: []string{
				"Add components from the palette",
				"Drag and drop components onto the canvas",
			},
		})
		return result, nil
	}

	// Build FlowGraph from flow nodes
	graph, buildErrors := v.buildFlowGraph(flow)
	v.logger.Debug("FlowGraph build complete",
		"build_errors", len(buildErrors))

	if len(buildErrors) > 0 {
		// Component type errors are critical
		result.Errors = append(result.Errors, buildErrors...)
		result.Status = "errors"
		v.logger.Debug("Build errors found, extracting ports from valid nodes")

		// Extract port info from successfully built nodes (for UI visualization)
		v.extractNodePorts(flow, graph, result)
		v.logger.Debug("Port extraction complete despite build errors",
			"nodes_with_ports", len(result.Nodes))

		// Don't extract connections since we couldn't build complete graph
		return result, nil
	}

	// Auto-connect components by pattern matching
	v.logger.Debug("Auto-connecting components by patterns")
	if err := graph.ConnectComponentsByPatterns(); err != nil {
		// Connection pattern errors (network conflicts, etc.)
		v.logger.Debug("Pattern-based connection failed", "error", err)
		return nil, errors.WrapInvalid(err, "validator", "ValidateFlow", "connect components failed")
	}

	// Log all edges created by pattern matching
	edges := graph.GetEdges()
	v.logger.Debug("FlowGraph edges after pattern matching",
		"edge_count", len(edges))
	for i, edge := range edges {
		v.logger.Debug("FlowGraph edge details",
			"index", i,
			"from_component", edge.From.ComponentName,
			"from_port", edge.From.PortName,
			"to_component", edge.To.ComponentName,
			"to_port", edge.To.PortName,
			"connection_id", edge.ConnectionID,
			"pattern", edge.Pattern)
	}

	// Analyze connectivity
	analysis := graph.AnalyzeConnectivity()
	v.logger.Debug("Connectivity analysis complete",
		"orphaned_ports", len(analysis.OrphanedPorts),
		"disconnected_nodes", len(analysis.DisconnectedNodes))

	// Convert analysis to validation result
	v.convertAnalysisToResult(analysis, result)
	v.logger.Debug("Validation issues extracted",
		"errors", len(result.Errors),
		"warnings", len(result.Warnings))

	// Extract node port information from FlowGraph
	v.extractNodePorts(flow, graph, result)
	v.logger.Debug("Node ports extracted",
		"nodes", len(result.Nodes))

	// Extract discovered connections from FlowGraph edges
	v.extractDiscoveredConnections(graph, result)
	v.logger.Debug("Discovered connections extracted",
		"connections", len(result.DiscoveredConnections))

	// Validate interface contracts between connected ports
	v.validateInterfaceContracts(result)
	v.logger.Debug("Interface contract validation complete")

	// Determine overall status
	if len(result.Errors) > 0 {
		result.Status = "errors"
	} else if len(result.Warnings) > 0 {
		result.Status = "warnings"
	}

	v.logger.Debug("Flow validation complete",
		"status", result.Status,
		"errors", len(result.Errors),
		"warnings", len(result.Warnings),
		"nodes", len(result.Nodes),
		"connections", len(result.DiscoveredConnections))

	return result, nil
}

// buildFlowGraph creates a FlowGraph from a Flow
// Returns the graph and any errors encountered (e.g., unknown component types)
func (v *Validator) buildFlowGraph(flow *flowstore.Flow) (*flowgraph.FlowGraph, []ValidationIssue) {
	graph := flowgraph.NewFlowGraph()
	var buildErrors []ValidationIssue

	for _, node := range flow.Nodes {
		v.logger.Debug("Adding component to FlowGraph",
			"node_id", node.ID,
			"node_type", node.Type,
			"node_name", node.Name,
			"config", node.Config)

		// Get component from registry with node's actual config
		comp, err := v.getComponentFromRegistry(node.Type, node.Config)
		if err != nil {
			v.logger.Debug("Component lookup failed",
				"type", node.Type,
				"error", err)
			// Unknown component type is a critical error
			buildErrors = append(buildErrors, ValidationIssue{
				Type:          "unknown_component",
				Severity:      "error",
				ComponentName: node.Name,
				Message:       fmt.Sprintf("Unknown component type: %s", node.Type),
				Suggestions: []string{
					"Check that component type is registered",
					"Verify component name spelling",
				},
			})
			continue
		}
		v.logger.Debug("Component lookup succeeded", "type", node.Type)

		// Add to graph using node.ID (unique identifier), not node.Name (user-friendly label)
		if err := graph.AddComponentNode(node.ID, comp); err != nil {
			v.logger.Debug("Failed to add component to graph",
				"node_id", node.ID,
				"node_name", node.Name,
				"error", err)
			buildErrors = append(buildErrors, ValidationIssue{
				Type:          "graph_build_error",
				Severity:      "error",
				ComponentName: node.Name,
				Message:       fmt.Sprintf("Failed to add component to graph: %v", err),
			})
		} else {
			v.logger.Debug("Component added to graph successfully",
				"node_id", node.ID,
				"node_name", node.Name)
		}
	}

	return graph, buildErrors
}

// getComponentFromRegistry retrieves a component from the registry by type
// Creates a temporary instance using the factory for port discovery
func (v *Validator) getComponentFromRegistry(
	componentType string,
	nodeConfig map[string]any,
) (component.Discoverable, error) {
	// Get the factory function
	factory, exists := v.componentRegistry.GetFactory(componentType)
	if !exists {
		return nil, fmt.Errorf("component type %s not found in registry", componentType)
	}

	// Marshal node config to JSON for factory
	var configJSON []byte
	var err error
	if len(nodeConfig) == 0 {
		configJSON = []byte("{}")
	} else {
		configJSON, err = json.Marshal(nodeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal node config: %w", err)
		}
	}

	// Create dependencies with real NATS client
	deps := component.Dependencies{
		NATSClient: v.natsClient,
		Logger:     nil, // Logger not needed for port discovery
	}

	// Create component instance with actual config
	comp, err := factory(configJSON, deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create component instance for port discovery: %w", err)
	}

	return comp, nil
}

// convertAnalysisToResult converts FlowGraph analysis to ValidationResult
func (v *Validator) convertAnalysisToResult(analysis *flowgraph.FlowAnalysisResult, result *ValidationResult) {
	// Convert disconnected nodes
	for _, node := range analysis.DisconnectedNodes {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Type:          "disconnected_node",
			Severity:      "warning",
			ComponentName: node.ComponentName,
			Message:       node.Issue,
			Suggestions:   node.Suggestions,
		})
	}

	// Convert orphaned ports
	for _, port := range analysis.OrphanedPorts {
		// Determine severity based on pattern and required flag
		severity := "warning"
		suggestions := []string{}

		switch port.Issue {
		case "no_publishers":
			if port.Required && port.Pattern == flowgraph.PatternStream {
				severity = "error"
				suggestions = []string{
					"Connect an output from another component",
					"Check that source component is configured correctly",
				}
			} else {
				suggestions = []string{
					"This port is optional and can remain unconnected",
				}
			}

		case "no_subscribers":
			// Output ports with no subscribers are warnings, not errors
			// The component functions correctly - it just publishes to NATS with nobody listening
			// This is not ideal, but it's not a deployment blocker
			severity = "warning"
			if port.Required && port.Pattern == flowgraph.PatternStream {
				suggestions = []string{
					"Consider connecting to a processor or output component",
					fmt.Sprintf("Data will be published to %s but not consumed", port.ConnectionID),
				}
			} else {
				suggestions = []string{
					"This port is optional and can remain unconnected",
				}
			}

		case "optional_api_unused":
			severity = "warning"
			suggestions = []string{
				"This API port is optional",
			}

		case "optional_interface_unused":
			severity = "warning"
			suggestions = []string{
				"This interface-specific port is optional",
			}

		case "optional_index_unwatched":
			severity = "warning"
			suggestions = []string{
				"KV index ports are optional observation points",
			}
		}

		issue := ValidationIssue{
			Type:          "orphaned_port",
			Severity:      severity,
			ComponentName: port.ComponentName,
			PortName:      port.PortName,
			Message: fmt.Sprintf("%s port '%s' (%s): %s",
				port.Direction,
				port.PortName,
				port.Pattern,
				port.Issue),
			Suggestions: suggestions,
		}

		if severity == "error" {
			result.Errors = append(result.Errors, issue)
		} else {
			result.Warnings = append(result.Warnings, issue)
		}
	}
}

// extractNodePorts extracts port information from FlowGraph nodes and adds to validation result
func (v *Validator) extractNodePorts(flow *flowstore.Flow, graph *flowgraph.FlowGraph, result *ValidationResult) {
	graphNodes := graph.GetNodes()

	// Create maps from node ID to node metadata for lookup
	// Note: FlowGraph now uses node.ID as keys (not node.Name)
	idToName := make(map[string]string)
	idToType := make(map[string]string)
	for _, flowNode := range flow.Nodes {
		idToName[flowNode.ID] = flowNode.Name
		idToType[flowNode.ID] = flowNode.Type
	}

	result.Nodes = make([]ValidatedNode, 0, len(graphNodes))

	for nodeID, graphNode := range graphNodes {
		// Find the flow node name and type
		nodeName := idToName[nodeID]
		nodeType := idToType[nodeID]

		// Convert input ports
		inputPorts := make([]ValidatedPort, 0, len(graphNode.InputPorts))
		for _, port := range graphNode.InputPorts {
			portType := ""
			if port.Interface != nil {
				portType = port.Interface.Type
			}
			inputPorts = append(inputPorts, ValidatedPort{
				Name:         port.Name,
				Direction:    string(port.Direction),
				Type:         portType,
				Required:     port.Required,
				ConnectionID: port.ConnectionID,
				Pattern:      string(port.Pattern),
				Description:  "", // TODO: Extract from original Port if needed
			})
		}

		// Convert output ports
		outputPorts := make([]ValidatedPort, 0, len(graphNode.OutputPorts))
		for _, port := range graphNode.OutputPorts {
			portType := ""
			if port.Interface != nil {
				portType = port.Interface.Type
			}
			outputPorts = append(outputPorts, ValidatedPort{
				Name:         port.Name,
				Direction:    string(port.Direction),
				Type:         portType,
				Required:     port.Required,
				ConnectionID: port.ConnectionID,
				Pattern:      string(port.Pattern),
				Description:  "", // TODO: Extract from original Port if needed
			})
		}

		result.Nodes = append(result.Nodes, ValidatedNode{
			ID:          nodeID,
			Type:        nodeType,
			Name:        nodeName,
			InputPorts:  inputPorts,
			OutputPorts: outputPorts,
		})
	}
}

// extractDiscoveredConnections extracts auto-discovered connections from FlowGraph edges
func (v *Validator) extractDiscoveredConnections(graph *flowgraph.FlowGraph, result *ValidationResult) {
	edges := graph.GetEdges()
	v.logger.Debug("Extracting discovered connections",
		"input_edge_count", len(edges))

	result.DiscoveredConnections = make([]DiscoveredConnection, 0, len(edges))

	for i, edge := range edges {
		conn := DiscoveredConnection{
			SourceNodeID: edge.From.ComponentName, // Using component name as ID for now
			SourcePort:   edge.From.PortName,
			TargetNodeID: edge.To.ComponentName,
			TargetPort:   edge.To.PortName,
			ConnectionID: edge.ConnectionID,
			Pattern:      string(edge.Pattern),
		}
		v.logger.Debug("Converting edge to discovered connection",
			"index", i,
			"source_node", conn.SourceNodeID,
			"source_port", conn.SourcePort,
			"target_node", conn.TargetNodeID,
			"target_port", conn.TargetPort,
			"connection_id", conn.ConnectionID,
			"pattern", conn.Pattern)
		result.DiscoveredConnections = append(result.DiscoveredConnections, conn)
	}

	v.logger.Debug("Extracted discovered connections",
		"output_connection_count", len(result.DiscoveredConnections))
}

// validateInterfaceContracts checks that connected ports have compatible interface contracts
func (v *Validator) validateInterfaceContracts(result *ValidationResult) {
	// Build port lookup map: nodeID -> portName -> ValidatedPort
	portLookup := make(map[string]map[string]ValidatedPort)
	for _, node := range result.Nodes {
		portLookup[node.ID] = make(map[string]ValidatedPort)
		for _, port := range node.InputPorts {
			portLookup[node.ID][port.Name] = port
		}
		for _, port := range node.OutputPorts {
			portLookup[node.ID][port.Name] = port
		}
	}

	// Validate each connection
	for _, conn := range result.DiscoveredConnections {
		// Get source and target ports
		sourceNode, sourceExists := portLookup[conn.SourceNodeID]
		if !sourceExists {
			v.logger.Debug("Source node not found in port lookup",
				"node_id", conn.SourceNodeID)
			continue // Node not found (shouldn't happen)
		}

		sourcePort, sourcePortExists := sourceNode[conn.SourcePort]
		if !sourcePortExists {
			v.logger.Debug("Source port not found in port lookup",
				"node_id", conn.SourceNodeID,
				"port_name", conn.SourcePort)
			continue // Port not found (shouldn't happen)
		}

		targetNode, targetExists := portLookup[conn.TargetNodeID]
		if !targetExists {
			v.logger.Debug("Target node not found in port lookup",
				"node_id", conn.TargetNodeID)
			continue // Node not found (shouldn't happen)
		}

		targetPort, targetPortExists := targetNode[conn.TargetPort]
		if !targetPortExists {
			v.logger.Debug("Target port not found in port lookup",
				"node_id", conn.TargetNodeID,
				"port_name", conn.TargetPort)
			continue // Port not found (shouldn't happen)
		}

		v.logger.Debug("Validating interface contract",
			"source_node", conn.SourceNodeID,
			"source_port", conn.SourcePort,
			"source_type", sourcePort.Type,
			"target_node", conn.TargetNodeID,
			"target_port", conn.TargetPort,
			"target_type", targetPort.Type)

		// Check interface contract compatibility
		if targetPort.Type != "" && sourcePort.Type != "" {
			if !v.areInterfacesCompatible(sourcePort.Type, targetPort.Type) {
				// Find user-friendly node names
				var sourceNodeName, targetNodeName string
				for _, node := range result.Nodes {
					if node.ID == conn.SourceNodeID {
						sourceNodeName = node.Name
					}
					if node.ID == conn.TargetNodeID {
						targetNodeName = node.Name
					}
				}

				result.Errors = append(result.Errors, ValidationIssue{
					Type:          "interface_mismatch",
					Severity:      "error",
					ComponentName: fmt.Sprintf("%s → %s", sourceNodeName, targetNodeName),
					PortName:      fmt.Sprintf("%s → %s", conn.SourcePort, conn.TargetPort),
					Message: fmt.Sprintf(
						"Interface mismatch: source port '%s' provides '%s' but target port '%s' requires '%s'",
						conn.SourcePort, sourcePort.Type,
						conn.TargetPort, targetPort.Type),
					Suggestions: []string{
						"Check that connected components have compatible interfaces",
						"Verify port interface contracts in component documentation",
						fmt.Sprintf("Source provides: %s", sourcePort.Type),
						fmt.Sprintf("Target requires: %s", targetPort.Type),
					},
				})
				v.logger.Debug("Interface mismatch detected",
					"source_type", sourcePort.Type,
					"target_type", targetPort.Type)
			}
		} else if targetPort.Type != "" && sourcePort.Type == "" {
			// Target requires interface but source doesn't declare one
			var sourceNodeName, targetNodeName string
			for _, node := range result.Nodes {
				if node.ID == conn.SourceNodeID {
					sourceNodeName = node.Name
				}
				if node.ID == conn.TargetNodeID {
					targetNodeName = node.Name
				}
			}

			result.Warnings = append(result.Warnings, ValidationIssue{
				Type:          "missing_interface",
				Severity:      "warning",
				ComponentName: fmt.Sprintf("%s → %s", sourceNodeName, targetNodeName),
				PortName:      fmt.Sprintf("%s → %s", conn.SourcePort, conn.TargetPort),
				Message: fmt.Sprintf(
					"Source port '%s' does not declare an interface, but target port '%s' requires '%s'",
					conn.SourcePort, conn.TargetPort, targetPort.Type),
				Suggestions: []string{
					"Verify that source component produces compatible data",
					"Check component documentation for interface contracts",
					fmt.Sprintf("Target requires: %s", targetPort.Type),
				},
			})
			v.logger.Debug("Missing interface declaration",
				"target_type", targetPort.Type)
		}
	}
}

// areInterfacesCompatible checks if two interface contract types are compatible
func (v *Validator) areInterfacesCompatible(sourceType, targetType string) bool {
	// Exact match is always compatible
	if sourceType == targetType {
		return true
	}

	// TODO: Implement interface hierarchy/compatibility rules
	// For now, we require exact match for behavioral interfaces
	// Future enhancement: Check if source implements target interface
	// e.g., "message.Locatable" should be compatible with a processor that accepts Locatable
	return false
}
