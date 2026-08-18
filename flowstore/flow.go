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

	// Desired activation is durable authoring state. Runtime observation is
	// process-local and is populated on reads; it is never persisted as flow
	// authority.
	DesiredState      DesiredState        `json:"desired_state"`
	DesiredComponents DesiredComponentSet `json:"desired_components"`
	DesiredChangedAt  *time.Time          `json:"desired_changed_at,omitempty"`

	EffectiveState        EffectiveState    `json:"effective_state,omitempty"`
	DesiredProvenance     *ConfigProvenance `json:"desired_provenance,omitempty"`
	BootAppliedProvenance *ConfigProvenance `json:"boot_applied_provenance,omitempty"`
	RestartRequired       *bool             `json:"restart_required"`

	// Audit
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

// DesiredComponentSet is the complete server-owned component bundle selected
// by a flow for the next successful process boot.
type DesiredComponentSet map[string]types.ComponentConfig

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

// DesiredState is the flow activation requested for the next successful boot.
type DesiredState string

const (
	// DesiredAbsent requests no components from this flow at the next boot.
	DesiredAbsent DesiredState = "absent"
	// DesiredDisabled requests present but disabled component configuration.
	DesiredDisabled DesiredState = "disabled"
	// DesiredEnabled requests enabled component configuration.
	DesiredEnabled DesiredState = "enabled"
)

// EffectiveState is an independently observed runtime activation state.
type EffectiveState string

const (
	// EffectiveUnknown means no authoritative runtime observer is available.
	EffectiveUnknown EffectiveState = "unknown"
	// EffectiveAbsent means the runtime observer found no active flow components.
	EffectiveAbsent EffectiveState = "absent"
	// EffectiveDisabled means the runtime observer found the flow disabled.
	EffectiveDisabled EffectiveState = "disabled"
	// EffectiveEnabled means the runtime observer found the flow enabled.
	EffectiveEnabled EffectiveState = "enabled"
)

// ConfigProvenance identifies one canonical desired or boot-applied flow
// configuration. BootID is set only for boot-applied provenance.
type ConfigProvenance struct {
	BootID string `json:"boot_id,omitempty"`
	Digest string `json:"digest"`
}

// Validate checks if the flow is valid for deployment
func (f *Flow) Validate() error {
	// Validate flow-level fields
	if f.ID == "" {
		return errs.WrapInvalid(fmt.Errorf("flow ID cannot be empty"), "flowstore", "Validate", "validation failed")
	}
	if f.Name == "" {
		return errs.WrapInvalid(fmt.Errorf("flow name cannot be empty"), "flowstore", "Validate", "validation failed")
	}

	// Validate desired activation state.
	validStates := map[DesiredState]bool{
		DesiredAbsent:   true,
		DesiredDisabled: true,
		DesiredEnabled:  true,
	}
	if !validStates[f.DesiredState] {
		return errs.WrapInvalid(
			fmt.Errorf("invalid desired state: %s", string(f.DesiredState)),
			"flowstore", "Validate", "desired state validation failed")
	}
	if err := validateDesiredActivation(f.DesiredState, f.DesiredComponents); err != nil {
		return errs.WrapInvalid(err, "flowstore", "Validate", "desired activation validation failed")
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
