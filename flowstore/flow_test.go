package flowstore

import (
	"testing"
	"time"

	"github.com/c360/semstreams/errors"
)

// TestFlowValidation tests the Flow.Validate() method
func TestFlowValidation(t *testing.T) {
	tests := []struct {
		name      string
		flow      Flow
		wantError bool
		errorType string // "invalid", "fatal", etc.
	}{
		{
			name: "valid flow with all required fields",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				Description:  "A test flow",
				Version:      1,
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
						Config:   map[string]any{"port": 5000},
					},
				},
				Connections: []FlowConnection{},
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			},
			wantError: false,
		},
		{
			name: "empty ID should fail",
			flow: Flow{
				ID:           "",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes:        []FlowNode{},
				Connections:  []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "empty name should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "",
				RuntimeState: StateNotDeployed,
				Nodes:        []FlowNode{},
				Connections:  []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "invalid runtime state should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: RuntimeState("invalid_state"),
				Nodes:        []FlowNode{},
				Connections:  []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "node with empty ID should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "",
						Type:     "udp",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
					},
				},
				Connections: []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "node with empty type should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
					},
				},
				Connections: []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "node with empty name should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "",
						Position: Position{X: 100, Y: 100},
					},
				},
				Connections: []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "duplicate node IDs should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "UDP Input 1",
						Position: Position{X: 100, Y: 100},
					},
					{
						ID:       "node-1", // Duplicate ID
						Type:     "graph-processor",
						Name:     "Graph Processor",
						Position: Position{X: 300, Y: 100},
					},
				},
				Connections: []FlowConnection{},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "connection with invalid source node should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
					},
				},
				Connections: []FlowConnection{
					{
						ID:           "conn-1",
						SourceNodeID: "non-existent-node",
						SourcePort:   "output",
						TargetNodeID: "node-1",
						TargetPort:   "input",
					},
				},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "connection with invalid target node should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
					},
				},
				Connections: []FlowConnection{
					{
						ID:           "conn-1",
						SourceNodeID: "node-1",
						SourcePort:   "output",
						TargetNodeID: "non-existent-node",
						TargetPort:   "input",
					},
				},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "connection with empty ID should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "Node 1",
						Position: Position{X: 100, Y: 100},
					},
					{
						ID:       "node-2",
						Type:     "graph-processor",
						Name:     "Node 2",
						Position: Position{X: 300, Y: 100},
					},
				},
				Connections: []FlowConnection{
					{
						ID:           "",
						SourceNodeID: "node-1",
						SourcePort:   "output",
						TargetNodeID: "node-2",
						TargetPort:   "input",
					},
				},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "connection with empty ports should fail",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "Node 1",
						Position: Position{X: 100, Y: 100},
					},
					{
						ID:       "node-2",
						Type:     "graph-processor",
						Name:     "Node 2",
						Position: Position{X: 300, Y: 100},
					},
				},
				Connections: []FlowConnection{
					{
						ID:           "conn-1",
						SourceNodeID: "node-1",
						SourcePort:   "",
						TargetNodeID: "node-2",
						TargetPort:   "input",
					},
				},
			},
			wantError: true,
			errorType: "invalid",
		},
		{
			name: "valid flow with connections",
			flow: Flow{
				ID:           "flow-123",
				Name:         "Test Flow",
				RuntimeState: StateNotDeployed,
				Nodes: []FlowNode{
					{
						ID:       "node-1",
						Type:     "udp",
						Name:     "UDP Input",
						Position: Position{X: 100, Y: 100},
						Config:   map[string]any{"port": 5000},
					},
					{
						ID:       "node-2",
						Type:     "graph-processor",
						Name:     "Graph Processor",
						Position: Position{X: 300, Y: 100},
						Config:   map[string]any{},
					},
				},
				Connections: []FlowConnection{
					{
						ID:           "conn-1",
						SourceNodeID: "node-1",
						SourcePort:   "output",
						TargetNodeID: "node-2",
						TargetPort:   "input",
					},
				},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flow.Validate()

			if tt.wantError {
				if err == nil {
					t.Errorf("Flow.Validate() expected error, got nil")
					return
				}

				// Check error type classification
				if tt.errorType == "invalid" {
					if !errors.IsInvalid(err) {
						t.Errorf("Flow.Validate() error should be Invalid, got: %v", err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Flow.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestRuntimeStateConstants tests that all runtime state constants are defined
func TestRuntimeStateConstants(t *testing.T) {
	states := []RuntimeState{
		StateNotDeployed,
		StateDeployedStopped,
		StateRunning,
		StateError,
	}

	expectedValues := map[RuntimeState]string{
		StateNotDeployed:     "not_deployed",
		StateDeployedStopped: "deployed_stopped",
		StateRunning:         "running",
		StateError:           "error",
	}

	for _, state := range states {
		expected, ok := expectedValues[state]
		if !ok {
			t.Errorf("Unexpected runtime state: %v", state)
			continue
		}

		if string(state) != expected {
			t.Errorf("RuntimeState %v should equal %q, got %q", state, expected, string(state))
		}
	}
}

// TestFlowNodeValidation tests FlowNode field requirements
func TestFlowNodeValidation(t *testing.T) {
	// This test ensures that the Flow.Validate() method properly validates nodes
	node := FlowNode{
		ID:       "node-1",
		Type:     "udp",
		Name:     "Test Node",
		Position: Position{X: 100, Y: 200},
		Config:   map[string]any{"port": 5000},
	}

	flow := Flow{
		ID:           "flow-1",
		Name:         "Test",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{node},
		Connections:  []FlowConnection{},
	}

	// Valid flow should pass
	if err := flow.Validate(); err != nil {
		t.Errorf("Valid flow with valid node should not error: %v", err)
	}

	// Test each required field
	tests := []struct {
		name     string
		modifyFn func(*FlowNode)
	}{
		{"empty ID", func(n *FlowNode) { n.ID = "" }},
		{"empty Type", func(n *FlowNode) { n.Type = "" }},
		{"empty Name", func(n *FlowNode) { n.Name = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testNode := node
			tt.modifyFn(&testNode)

			testFlow := Flow{
				ID:           "flow-1",
				Name:         "Test",
				RuntimeState: StateNotDeployed,
				Nodes:        []FlowNode{testNode},
				Connections:  []FlowConnection{},
			}

			err := testFlow.Validate()
			if err == nil {
				t.Errorf("Flow.Validate() should fail for node with %s", tt.name)
			}
			if err != nil && !errors.IsInvalid(err) {
				t.Errorf("Flow.Validate() should return Invalid error, got: %v", err)
			}
		})
	}
}

// TestFlowConnectionValidation tests FlowConnection field requirements
func TestFlowConnectionValidation(t *testing.T) {
	baseFlow := Flow{
		ID:           "flow-1",
		Name:         "Test",
		RuntimeState: StateNotDeployed,
		Nodes: []FlowNode{
			{
				ID:       "node-1",
				Type:     "udp",
				Name:     "Node 1",
				Position: Position{X: 100, Y: 100},
			},
			{
				ID:       "node-2",
				Type:     "graph-processor",
				Name:     "Node 2",
				Position: Position{X: 300, Y: 100},
			},
		},
		Connections: []FlowConnection{},
	}

	validConnection := FlowConnection{
		ID:           "conn-1",
		SourceNodeID: "node-1",
		SourcePort:   "output",
		TargetNodeID: "node-2",
		TargetPort:   "input",
	}

	tests := []struct {
		name       string
		connection FlowConnection
		wantError  bool
	}{
		{
			name:       "valid connection",
			connection: validConnection,
			wantError:  false,
		},
		{
			name: "empty ID",
			connection: FlowConnection{
				ID:           "",
				SourceNodeID: "node-1",
				SourcePort:   "output",
				TargetNodeID: "node-2",
				TargetPort:   "input",
			},
			wantError: true,
		},
		{
			name: "empty source port",
			connection: FlowConnection{
				ID:           "conn-1",
				SourceNodeID: "node-1",
				SourcePort:   "",
				TargetNodeID: "node-2",
				TargetPort:   "input",
			},
			wantError: true,
		},
		{
			name: "empty target port",
			connection: FlowConnection{
				ID:           "conn-1",
				SourceNodeID: "node-1",
				SourcePort:   "output",
				TargetNodeID: "node-2",
				TargetPort:   "",
			},
			wantError: true,
		},
		{
			name: "invalid source node",
			connection: FlowConnection{
				ID:           "conn-1",
				SourceNodeID: "non-existent",
				SourcePort:   "output",
				TargetNodeID: "node-2",
				TargetPort:   "input",
			},
			wantError: true,
		},
		{
			name: "invalid target node",
			connection: FlowConnection{
				ID:           "conn-1",
				SourceNodeID: "node-1",
				SourcePort:   "output",
				TargetNodeID: "non-existent",
				TargetPort:   "input",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFlow := baseFlow
			testFlow.Connections = []FlowConnection{tt.connection}

			err := testFlow.Validate()
			if tt.wantError {
				if err == nil {
					t.Errorf("Flow.Validate() expected error for %s", tt.name)
				}
				if err != nil && !errors.IsInvalid(err) {
					t.Errorf("Flow.Validate() should return Invalid error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Flow.Validate() unexpected error for %s: %v", tt.name, err)
				}
			}
		})
	}
}
