package flowstore

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
)

// Flow represents a visual flow definition with metadata and canvas layout
type Flow struct {
	// Identity
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Version for optimistic concurrency control
	Version int64 `json:"version"`

	// Canvas layout
	Nodes       []FlowNode       `json:"nodes"`
	Connections []FlowConnection `json:"connections"`

	// Runtime state
	RuntimeState RuntimeState `json:"runtime_state"`
	DeployedAt   *time.Time   `json:"deployed_at,omitempty"`
	StartedAt    *time.Time   `json:"started_at,omitempty"`
	StoppedAt    *time.Time   `json:"stopped_at,omitempty"`

	// Audit
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

// FlowNode represents a component instance on the canvas
type FlowNode struct {
	ID       string         `json:"id"`       // Unique instance ID
	Type     string         `json:"type"`     // Component type (e.g., "udp", "graph-processor")
	Name     string         `json:"name"`     // Instance name
	Position Position       `json:"position"` // Canvas coordinates
	Config   map[string]any `json:"config"`   // Component configuration
}

// FlowConnection represents a connection between two component ports
type FlowConnection struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	SourcePort   string `json:"source_port"`
	TargetNodeID string `json:"target_node_id"`
	TargetPort   string `json:"target_port"`
}

// Position represents canvas coordinates for a node
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// RuntimeState represents the deployment and execution state of a flow
type RuntimeState string

// RuntimeState constants define the lifecycle states of a flow:
//   - StateNotDeployed: Flow exists but has never been deployed
//   - StateDeployedStopped: Flow deployed to config but not running
//   - StateRunning: Flow is actively processing messages
//   - StateError: Flow encountered an error during deployment/execution
const (
	StateNotDeployed     RuntimeState = "not_deployed"
	StateDeployedStopped RuntimeState = "deployed_stopped"
	StateRunning         RuntimeState = "running"
	StateError           RuntimeState = "error"
)

// Validate checks if the flow is valid for deployment
func (f *Flow) Validate() error {
	// Validate flow-level fields
	if f.ID == "" {
		return errors.WrapInvalid(fmt.Errorf("flow ID cannot be empty"), "flowstore", "Validate", "validation failed")
	}
	if f.Name == "" {
		return errors.WrapInvalid(fmt.Errorf("flow name cannot be empty"), "flowstore", "Validate", "validation failed")
	}

	// Validate runtime state
	validStates := map[RuntimeState]bool{
		StateNotDeployed:     true,
		StateDeployedStopped: true,
		StateRunning:         true,
		StateError:           true,
	}
	if !validStates[f.RuntimeState] {
		return errors.WrapInvalid(
			fmt.Errorf("invalid runtime state: %s", string(f.RuntimeState)),
			"flowstore", "Validate", "runtime state validation failed")
	}

	// Validate nodes
	nodeIDs := make(map[string]bool)
	for i, node := range f.Nodes {
		if node.ID == "" {
			return errors.WrapInvalid(
				fmt.Errorf("node at index %d has empty ID", i),
				"flowstore", "Validate", "node ID validation failed")
		}
		if node.Type == "" {
			return errors.WrapInvalid(
				fmt.Errorf("node '%s' has empty type", node.ID),
				"flowstore", "Validate", "node type validation failed")
		}
		if node.Name == "" {
			return errors.WrapInvalid(
				fmt.Errorf("node '%s' has empty name", node.ID),
				"flowstore", "Validate", "node name validation failed")
		}

		// Check for duplicate node IDs
		if nodeIDs[node.ID] {
			return errors.WrapInvalid(
				fmt.Errorf("duplicate node ID: %s", node.ID),
				"flowstore", "Validate", "duplicate node ID detected")
		}
		nodeIDs[node.ID] = true
	}

	// Validate connections
	for i, conn := range f.Connections {
		if conn.ID == "" {
			return errors.WrapInvalid(
				fmt.Errorf("connection at index %d has empty ID", i),
				"flowstore", "Validate", "connection ID validation failed")
		}
		if conn.SourcePort == "" {
			return errors.WrapInvalid(
				fmt.Errorf("connection '%s' has empty source port", conn.ID),
				"flowstore", "Validate", "connection source port validation failed")
		}
		if conn.TargetPort == "" {
			return errors.WrapInvalid(
				fmt.Errorf("connection '%s' has empty target port", conn.ID),
				"flowstore", "Validate", "connection target port validation failed")
		}

		// Validate node references
		if !nodeIDs[conn.SourceNodeID] {
			return errors.WrapInvalid(
				fmt.Errorf("connection '%s' references non-existent source node: %s", conn.ID, conn.SourceNodeID),
				"flowstore", "Validate", "connection source node validation failed")
		}
		if !nodeIDs[conn.TargetNodeID] {
			return errors.WrapInvalid(
				fmt.Errorf("connection '%s' references non-existent target node: %s", conn.ID, conn.TargetNodeID),
				"flowstore", "Validate", "connection target node validation failed")
		}
	}

	return nil
}
