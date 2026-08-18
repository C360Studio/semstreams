package flowstore

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
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

	// Audit
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

// FlowNode represents a component instance on the canvas
type FlowNode struct {
	ID        string              `json:"id"`        // Unique instance ID
	Component string              `json:"component"` // Component factory name (e.g., "udp", "graph-processor")
	Type      types.ComponentType `json:"type"`      // Component category (input/processor/output/storage/gateway)
	Name      string              `json:"name"`      // Instance name
	Position  Position            `json:"position"`  // Canvas coordinates
	Config    map[string]any      `json:"config"`    // Component configuration
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

// Validate checks whether the saved flow diagram is structurally valid.
func (f *Flow) Validate() error {
	// Validate flow-level fields
	if f.ID == "" {
		return errs.WrapInvalid(fmt.Errorf("flow ID cannot be empty"), "flowstore", "Validate", "validation failed")
	}
	if f.Name == "" {
		return errs.WrapInvalid(fmt.Errorf("flow name cannot be empty"), "flowstore", "Validate", "validation failed")
	}

	// Validate nodes
	nodeIDs := make(map[string]bool)
	for i, node := range f.Nodes {
		if node.ID == "" {
			return errs.WrapInvalid(
				fmt.Errorf("node at index %d has empty ID", i),
				"flowstore", "Validate", "node ID validation failed")
		}
		if node.Component == "" {
			return errs.WrapInvalid(
				fmt.Errorf("node '%s' has empty component", node.ID),
				"flowstore", "Validate", "node component validation failed")
		}
		if node.Type == "" {
			return errs.WrapInvalid(
				fmt.Errorf("node '%s' has empty type", node.ID),
				"flowstore", "Validate", "node type validation failed")
		}
		if node.Name == "" {
			return errs.WrapInvalid(
				fmt.Errorf("node '%s' has empty name", node.ID),
				"flowstore", "Validate", "node name validation failed")
		}

		// Check for duplicate node IDs
		if nodeIDs[node.ID] {
			return errs.WrapInvalid(
				fmt.Errorf("duplicate node ID: %s", node.ID),
				"flowstore", "Validate", "duplicate node ID detected")
		}
		nodeIDs[node.ID] = true
	}

	// Validate connections
	for i, conn := range f.Connections {
		if conn.ID == "" {
			return errs.WrapInvalid(
				fmt.Errorf("connection at index %d has empty ID", i),
				"flowstore", "Validate", "connection ID validation failed")
		}
		if conn.SourcePort == "" {
			return errs.WrapInvalid(
				fmt.Errorf("connection '%s' has empty source port", conn.ID),
				"flowstore", "Validate", "connection source port validation failed")
		}
		if conn.TargetPort == "" {
			return errs.WrapInvalid(
				fmt.Errorf("connection '%s' has empty target port", conn.ID),
				"flowstore", "Validate", "connection target port validation failed")
		}

		// Validate node references
		if !nodeIDs[conn.SourceNodeID] {
			return errs.WrapInvalid(
				fmt.Errorf("connection '%s' references non-existent source node: %s", conn.ID, conn.SourceNodeID),
				"flowstore", "Validate", "connection source node validation failed")
		}
		if !nodeIDs[conn.TargetNodeID] {
			return errs.WrapInvalid(
				fmt.Errorf("connection '%s' references non-existent target node: %s", conn.ID, conn.TargetNodeID),
				"flowstore", "Validate", "connection target node validation failed")
		}
	}

	return nil
}
