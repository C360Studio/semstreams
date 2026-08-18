// Package flowstore provides flow persistence and management.
package flowstore

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/c360studio/semstreams/types"
	"github.com/google/uuid"
)

// FromComponentConfigs creates a Flow from component configurations.
// This bridges static config files to the FlowStore, making headless
// configs visible in the UI.
//
// The conversion:
//   - Each ComponentConfig becomes a FlowNode
//   - Node.ID = config key (e.g., "udp-input")
//   - Node.Component = cfg.Name (factory name, e.g., "udp")
//   - Node.Type = cfg.Type (category, e.g., "input", "processor")
//   - Node.Config = component config as map[string]any
//   - Positions are auto-calculated using grid layout
//
// Connections are left empty. A caller may use the flow validator's detached
// port discovery to suggest connections, or users can author them directly.
func FromComponentConfigs(name string, configs map[string]types.ComponentConfig) (*Flow, error) {
	flow := &Flow{
		ID:          uuid.New().String(),
		Name:        name,
		Description: "Auto-generated from static configuration",
		Version:     1,
		CreatedAt:   time.Now(),
		Nodes:       make([]FlowNode, 0, len(configs)),
		Connections: []FlowConnection{}, // Empty - connections are diagram authoring data
	}
	flow.UpdatedAt = flow.CreatedAt
	flow.LastModified = flow.CreatedAt

	// Sort keys for deterministic node ordering
	keys := make([]string, 0, len(configs))
	for key := range configs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Create nodes from configs
	for i, key := range keys {
		cfg := configs[key]

		// Skip disabled components
		if !cfg.Enabled {
			continue
		}
		// Convert json.RawMessage to map[string]any
		var configMap map[string]any
		if len(cfg.Config) > 0 {
			if err := json.Unmarshal(cfg.Config, &configMap); err != nil {
				// If config isn't valid JSON object, wrap it
				configMap = map[string]any{"raw": string(cfg.Config)}
			}
		}
		if configMap == nil {
			configMap = make(map[string]any)
		}

		node := FlowNode{
			ID:        key,
			Component: cfg.Name, // Factory name (e.g., "udp", "graph-processor")
			Type:      cfg.Type, // Category (input/processor/output/storage/gateway)
			Name:      key,      // Use config key as display name
			Position:  calculateGridPosition(i, len(keys)),
			Config:    configMap,
		}
		flow.Nodes = append(flow.Nodes, node)
	}
	return flow, nil
}

// calculateGridPosition calculates canvas position using a grid layout.
// Nodes are arranged in rows with grouping by component type.
func calculateGridPosition(index, _ int) Position {
	const (
		nodeWidth   = 200.0 // Approximate node width
		nodeHeight  = 100.0 // Approximate node height
		paddingX    = 100.0 // Horizontal padding between nodes
		paddingY    = 80.0  // Vertical padding between nodes
		startX      = 50.0  // Starting X position
		startY      = 50.0  // Starting Y position
		nodesPerRow = 4     // Nodes per row for grid layout
	)

	row := index / nodesPerRow
	col := index % nodesPerRow

	return Position{
		X: startX + float64(col)*(nodeWidth+paddingX),
		Y: startY + float64(row)*(nodeHeight+paddingY),
	}
}

// FromComponentConfigsWithConnections creates a Flow with connection inference.
// This variant accepts pre-computed connections from FlowGraph analysis.
// Use this when you have access to instantiated components for port matching.
func FromComponentConfigsWithConnections(
	name string,
	configs map[string]types.ComponentConfig,
	connections []FlowConnection,
) (*Flow, error) {
	flow, err := FromComponentConfigs(name, configs)
	if err != nil {
		return nil, err
	}
	flow.Connections = connections
	return flow, nil
}
