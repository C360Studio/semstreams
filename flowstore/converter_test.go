package flowstore

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/types"
)

func TestFromComponentConfigs(t *testing.T) {
	tests := []struct {
		name           string
		flowName       string
		configs        map[string]types.ComponentConfig
		wantNodeCount  int
		wantNodeIDs    []string
		wantComponents map[string]string              // nodeID -> component (factory name)
		wantTypes      map[string]types.ComponentType // nodeID -> type (category)
		wantState      DesiredState
	}{
		{
			name:          "empty configs creates empty flow",
			flowName:      "empty",
			configs:       map[string]types.ComponentConfig{},
			wantNodeCount: 0,
			wantNodeIDs:   []string{},
			wantState:     DesiredEnabled,
		},
		{
			name:     "single component",
			flowName: "single",
			configs: map[string]types.ComponentConfig{
				"udp-input": {
					Type:    types.ComponentTypeInput,
					Name:    "udp",
					Enabled: true,
					Config:  json.RawMessage(`{"port": 14550}`),
				},
			},
			wantNodeCount:  1,
			wantNodeIDs:    []string{"udp-input"},
			wantComponents: map[string]string{"udp-input": "udp"},
			wantTypes:      map[string]types.ComponentType{"udp-input": types.ComponentTypeInput},
			wantState:      DesiredEnabled,
		},
		{
			name:     "multiple components",
			flowName: "multi",
			configs: map[string]types.ComponentConfig{
				"udp-input": {
					Type:    types.ComponentTypeInput,
					Name:    "udp",
					Enabled: true,
					Config:  json.RawMessage(`{"port": 14550}`),
				},
				"graph-processor": {
					Type:    types.ComponentTypeProcessor,
					Name:    "graph-processor",
					Enabled: true,
					Config:  json.RawMessage(`{}`),
				},
				"file-output": {
					Type:    types.ComponentTypeOutput,
					Name:    "file",
					Enabled: true,
					Config:  json.RawMessage(`{"path": "/tmp/out.log"}`),
				},
			},
			wantNodeCount: 3,
			wantNodeIDs:   []string{"file-output", "graph-processor", "udp-input"}, // sorted
			wantComponents: map[string]string{
				"udp-input":       "udp",
				"graph-processor": "graph-processor",
				"file-output":     "file",
			},
			wantTypes: map[string]types.ComponentType{
				"udp-input":       types.ComponentTypeInput,
				"graph-processor": types.ComponentTypeProcessor,
				"file-output":     types.ComponentTypeOutput,
			},
			wantState: DesiredEnabled,
		},
		{
			name:     "disabled components are excluded",
			flowName: "with-disabled",
			configs: map[string]types.ComponentConfig{
				"enabled-input": {
					Type:    types.ComponentTypeInput,
					Name:    "udp",
					Enabled: true,
				},
				"disabled-output": {
					Type:    types.ComponentTypeOutput,
					Name:    "file",
					Enabled: false,
				},
			},
			wantNodeCount:  1,
			wantNodeIDs:    []string{"enabled-input"},
			wantComponents: map[string]string{"enabled-input": "udp"},
			wantTypes:      map[string]types.ComponentType{"enabled-input": types.ComponentTypeInput},
			wantState:      DesiredEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow, err := FromComponentConfigs(tt.flowName, tt.configs)
			if err != nil {
				t.Fatalf("FromComponentConfigs() error = %v", err)
			}

			// Verify flow metadata
			if flow.Name != tt.flowName {
				t.Errorf("flow.Name = %v, want %v", flow.Name, tt.flowName)
			}
			if flow.ID == "" {
				t.Error("flow.ID should not be empty")
			}
			if flow.DesiredState != tt.wantState {
				t.Errorf("flow.DesiredState = %v, want %v", flow.DesiredState, tt.wantState)
			}

			// Verify node count
			if len(flow.Nodes) != tt.wantNodeCount {
				t.Errorf("len(flow.Nodes) = %v, want %v", len(flow.Nodes), tt.wantNodeCount)
			}

			// Verify node IDs are sorted
			for i, wantID := range tt.wantNodeIDs {
				if i >= len(flow.Nodes) {
					t.Errorf("missing node at index %d", i)
					continue
				}
				if flow.Nodes[i].ID != wantID {
					t.Errorf("flow.Nodes[%d].ID = %v, want %v", i, flow.Nodes[i].ID, wantID)
				}
			}

			// Verify components (factory names)
			for _, node := range flow.Nodes {
				if want, ok := tt.wantComponents[node.ID]; ok {
					if node.Component != want {
						t.Errorf("node %s Component = %v, want %v", node.ID, node.Component, want)
					}
				}
			}

			// Verify types (categories)
			for _, node := range flow.Nodes {
				if want, ok := tt.wantTypes[node.ID]; ok {
					if node.Type != want {
						t.Errorf("node %s Type = %v, want %v", node.ID, node.Type, want)
					}
				}
			}

			// Verify connections are empty (derived at runtime)
			if len(flow.Connections) != 0 {
				t.Errorf("flow.Connections should be empty, got %d", len(flow.Connections))
			}
		})
	}
}

func TestFromComponentConfigs_NodePositions(t *testing.T) {
	configs := make(map[string]types.ComponentConfig)
	for i := 0; i < 8; i++ {
		name := string(rune('a' + i))
		configs[name] = types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "test",
			Enabled: true,
		}
	}

	flow, err := FromComponentConfigs("test", configs)
	if err != nil {
		t.Fatalf("FromComponentConfigs() error = %v", err)
	}

	// Verify all nodes have valid positions
	for _, node := range flow.Nodes {
		if node.Position.X < 0 || node.Position.Y < 0 {
			t.Errorf("node %s has negative position: (%v, %v)", node.ID, node.Position.X, node.Position.Y)
		}
	}

	// Verify nodes are spread out (not all at same position)
	positions := make(map[Position]bool)
	for _, node := range flow.Nodes {
		if positions[node.Position] {
			t.Errorf("duplicate position found: (%v, %v)", node.Position.X, node.Position.Y)
		}
		positions[node.Position] = true
	}
}

func TestFromComponentConfigs_ConfigPreservation(t *testing.T) {
	originalConfig := json.RawMessage(`{"port": 14550, "bind": "0.0.0.0", "buffer_size": 4096}`)
	configs := map[string]types.ComponentConfig{
		"udp-input": {
			Type:    types.ComponentTypeInput,
			Name:    "udp",
			Enabled: true,
			Config:  originalConfig,
		},
	}

	flow, err := FromComponentConfigs("test", configs)
	if err != nil {
		t.Fatalf("FromComponentConfigs() error = %v", err)
	}

	if len(flow.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(flow.Nodes))
	}

	node := flow.Nodes[0]

	// Verify config values are preserved
	port, ok := node.Config["port"].(float64)
	if !ok || port != 14550 {
		t.Errorf("config port = %v, want 14550", node.Config["port"])
	}
	bind, ok := node.Config["bind"].(string)
	if !ok || bind != "0.0.0.0" {
		t.Errorf("config bind = %v, want 0.0.0.0", node.Config["bind"])
	}
	bufferSize, ok := node.Config["buffer_size"].(float64)
	if !ok || bufferSize != 4096 {
		t.Errorf("config buffer_size = %v, want 4096", node.Config["buffer_size"])
	}
}

func TestFromComponentConfigs_EmptyConfig(t *testing.T) {
	configs := map[string]types.ComponentConfig{
		"processor": {
			Type:    types.ComponentTypeProcessor,
			Name:    "test",
			Enabled: true,
			Config:  nil, // nil config
		},
	}

	flow, err := FromComponentConfigs("test", configs)
	if err != nil {
		t.Fatalf("FromComponentConfigs() error = %v", err)
	}

	if len(flow.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(flow.Nodes))
	}

	// Config should be empty map, not nil
	if flow.Nodes[0].Config == nil {
		t.Error("node.Config should not be nil")
	}
}

func TestFromComponentConfigsWithConnections(t *testing.T) {
	configs := map[string]types.ComponentConfig{
		"input": {
			Type:    types.ComponentTypeInput,
			Name:    "udp",
			Enabled: true,
		},
		"output": {
			Type:    types.ComponentTypeOutput,
			Name:    "file",
			Enabled: true,
		},
	}

	connections := []FlowConnection{
		{
			ID:           "conn-1",
			SourceNodeID: "input",
			SourcePort:   "data",
			TargetNodeID: "output",
			TargetPort:   "input",
		},
	}

	flow, err := FromComponentConfigsWithConnections("test", configs, connections)
	if err != nil {
		t.Fatalf("FromComponentConfigsWithConnections() error = %v", err)
	}

	if len(flow.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(flow.Connections))
	}
	if flow.Connections[0].ID != "conn-1" {
		t.Errorf("connection ID = %v, want conn-1", flow.Connections[0].ID)
	}
}

func TestFromComponentConfigs_FlowValidation(t *testing.T) {
	configs := map[string]types.ComponentConfig{
		"input": {
			Type:    types.ComponentTypeInput,
			Name:    "udp",
			Enabled: true,
		},
	}

	flow, err := FromComponentConfigs("valid-flow", configs)
	if err != nil {
		t.Fatalf("FromComponentConfigs() error = %v", err)
	}

	// The generated flow should pass validation
	if err := flow.Validate(); err != nil {
		t.Errorf("flow.Validate() error = %v", err)
	}
}

func TestCalculateGridPosition(t *testing.T) {
	tests := []struct {
		index int
		total int
		wantX float64
		wantY float64
	}{
		{index: 0, total: 1, wantX: 50, wantY: 50},   // First node
		{index: 1, total: 4, wantX: 350, wantY: 50},  // Second column
		{index: 4, total: 8, wantX: 50, wantY: 230},  // Second row, first column
		{index: 5, total: 8, wantX: 350, wantY: 230}, // Second row, second column
	}

	for _, tt := range tests {
		pos := calculateGridPosition(tt.index, tt.total)
		if pos.X != tt.wantX {
			t.Errorf("calculateGridPosition(%d, %d).X = %v, want %v", tt.index, tt.total, pos.X, tt.wantX)
		}
		if pos.Y != tt.wantY {
			t.Errorf("calculateGridPosition(%d, %d).Y = %v, want %v", tt.index, tt.total, pos.Y, tt.wantY)
		}
	}
}
