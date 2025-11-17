package flowgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
)

// TestFlowGraphConstruction tests basic FlowGraph creation and structure
func TestFlowGraphConstruction(t *testing.T) {
	t.Run("create empty FlowGraph", func(t *testing.T) {
		graph := NewFlowGraph()

		assert.NotNil(t, graph)
		assert.Empty(t, graph.GetNodes())
		assert.Empty(t, graph.GetEdges())
	})

	t.Run("add component node", func(t *testing.T) {
		graph := NewFlowGraph()
		mockComponent := createMockComponent("test-component", "processor")

		err := graph.AddComponentNode("test-component", mockComponent)
		require.NoError(t, err)

		nodes := graph.GetNodes()
		assert.Len(t, nodes, 1)
		assert.Contains(t, nodes, "test-component")

		node := nodes["test-component"]
		assert.Equal(t, "test-component", node.ComponentName)
		assert.Equal(t, mockComponent, node.Component)
	})

	t.Run("add duplicate component node returns error", func(t *testing.T) {
		graph := NewFlowGraph()
		mockComponent := createMockComponent("test-component", "processor")

		err := graph.AddComponentNode("test-component", mockComponent)
		require.NoError(t, err)

		// Adding same component again should return error
		err = graph.AddComponentNode("test-component", mockComponent)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

// TestStreamPatternConnections tests stream pattern edge detection and connection
func TestStreamPatternConnections(t *testing.T) {
	t.Run("connect stream pattern components", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create publisher component
		publisher := createMockComponentWithPorts("publisher", "processor",
			nil, // no input ports
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "test.data"},
			}},
		)

		// Create subscriber component
		subscriber := createMockComponentWithPorts("subscriber", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "test.data"},
			}},
			nil, // no output ports
		)

		// Add components to graph
		err := graph.AddComponentNode("publisher", publisher)
		require.NoError(t, err)
		err = graph.AddComponentNode("subscriber", subscriber)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Verify connection was created
		edges := graph.GetEdges()
		assert.Len(t, edges, 1)

		edge := edges[0]
		assert.Equal(t, "publisher", edge.From.ComponentName)
		assert.Equal(t, "output", edge.From.PortName)
		assert.Equal(t, "subscriber", edge.To.ComponentName)
		assert.Equal(t, "input", edge.To.PortName)
		assert.Equal(t, PatternStream, edge.Pattern)
		assert.Equal(t, "test.data", edge.ConnectionID)
	})

	t.Run("no connection when subjects don't match", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create publisher with different subject
		publisher := createMockComponentWithPorts("publisher", "processor",
			nil,
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "different.subject"},
			}},
		)

		// Create subscriber with different subject
		subscriber := createMockComponentWithPorts("subscriber", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "test.data"},
			}},
			nil,
		)

		// Add components and connect
		graph.AddComponentNode("publisher", publisher)
		graph.AddComponentNode("subscriber", subscriber)

		err := graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Should have no connections
		edges := graph.GetEdges()
		assert.Empty(t, edges)
	})

	t.Run("fan-out connection - one publisher, multiple subscribers", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create one publisher
		publisher := createMockComponentWithPorts("publisher", "processor",
			nil,
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "fanout.data"},
			}},
		)

		// Create multiple subscribers
		subscriber1 := createMockComponentWithPorts("subscriber1", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "fanout.data"},
			}},
			nil,
		)

		subscriber2 := createMockComponentWithPorts("subscriber2", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "fanout.data"},
			}},
			nil,
		)

		// Add components and connect
		graph.AddComponentNode("publisher", publisher)
		graph.AddComponentNode("subscriber1", subscriber1)
		graph.AddComponentNode("subscriber2", subscriber2)

		err := graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Should have two connections (fan-out)
		edges := graph.GetEdges()
		assert.Len(t, edges, 2)

		// Both edges should be from publisher to different subscribers
		for _, edge := range edges {
			assert.Equal(t, "publisher", edge.From.ComponentName)
			assert.Equal(t, "output", edge.From.PortName)
			assert.Equal(t, PatternStream, edge.Pattern)
			assert.Equal(t, "fanout.data", edge.ConnectionID)
			assert.True(t, edge.To.ComponentName == "subscriber1" || edge.To.ComponentName == "subscriber2")
		}
	})
}

// TestFlowGraphAnalysis tests connectivity analysis algorithms
func TestFlowGraphAnalysis(t *testing.T) {
	t.Run("analyze connected components", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create a simple connected flow: input -> processor -> output
		input := createMockComponentWithPorts("input", "input",
			nil,
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "raw.data"},
			}},
		)

		processor := createMockComponentWithPorts("processor", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "raw.data"},
			}},
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "processed.data"},
			}},
		)

		output := createMockComponentWithPorts("output", "output",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "processed.data"},
			}},
			nil,
		)

		// Add components and connect
		graph.AddComponentNode("input", input)
		graph.AddComponentNode("processor", processor)
		graph.AddComponentNode("output", output)
		graph.ConnectComponentsByPatterns()

		// Analyze connectivity
		result := graph.AnalyzeConnectivity()
		require.NotNil(t, result)

		assert.Equal(t, "healthy", result.ValidationStatus)
		assert.Len(t, result.ConnectedEdges, 2) // input->processor, processor->output
		assert.Empty(t, result.DisconnectedNodes)
		assert.Empty(t, result.OrphanedPorts)

		// Should have one connected component with all three nodes
		assert.Len(t, result.ConnectedComponents, 1)
		assert.Len(t, result.ConnectedComponents[0], 3)
		assert.Contains(t, result.ConnectedComponents[0], "input")
		assert.Contains(t, result.ConnectedComponents[0], "processor")
		assert.Contains(t, result.ConnectedComponents[0], "output")
	})

	t.Run("detect disconnected nodes", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create connected pair
		connected1 := createMockComponentWithPorts("connected1", "processor",
			nil,
			[]component.Port{{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "connected.data"},
			}},
		)

		connected2 := createMockComponentWithPorts("connected2", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "connected.data"},
			}},
			nil,
		)

		// Create isolated component
		isolated := createMockComponentWithPorts("isolated", "processor",
			[]component.Port{{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "isolated.data"},
			}},
			nil,
		)

		// Add components and connect
		graph.AddComponentNode("connected1", connected1)
		graph.AddComponentNode("connected2", connected2)
		graph.AddComponentNode("isolated", isolated)
		graph.ConnectComponentsByPatterns()

		// Analyze connectivity
		result := graph.AnalyzeConnectivity()

		assert.Equal(t, "warnings", result.ValidationStatus)
		assert.Len(t, result.OrphanedPorts, 1) // isolated component has orphaned input port

		orphanedPort := result.OrphanedPorts[0]
		assert.Equal(t, "isolated", orphanedPort.ComponentName)
		assert.Equal(t, "input", orphanedPort.PortName)
		assert.Equal(t, "isolated.data", orphanedPort.ConnectionID)
	})
}

// Test helper functions
func createMockComponent(name, componentType string) component.Discoverable {
	return createMockComponentWithPorts(name, componentType, nil, nil)
}

func createMockComponentWithPorts(
	name, componentType string,
	inputPorts, outputPorts []component.Port,
) component.Discoverable {
	return &mockFlowGraphComponent{
		metadata: component.Metadata{
			Name: name,
			Type: componentType,
		},
		inputPorts:  inputPorts,
		outputPorts: outputPorts,
	}
}

// mockFlowGraphComponent implements component.Discoverable for FlowGraph testing
type mockFlowGraphComponent struct {
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
}

func (m *mockFlowGraphComponent) Meta() component.Metadata {
	return m.metadata
}

func (m *mockFlowGraphComponent) InputPorts() []component.Port {
	return m.inputPorts
}

func (m *mockFlowGraphComponent) OutputPorts() []component.Port {
	return m.outputPorts
}

func (m *mockFlowGraphComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}

func (m *mockFlowGraphComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}

func (m *mockFlowGraphComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}
