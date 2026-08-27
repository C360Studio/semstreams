package flowgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
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

		err := addTestComponentNode(graph, "test-component", mockComponent)
		require.NoError(t, err)

		nodes := graph.GetNodes()
		assert.Len(t, nodes, 1)
		assert.Contains(t, nodes, "test-component")

		node := nodes["test-component"]
		assert.Equal(t, "test-component", node.ComponentName)
	})

	t.Run("add duplicate component node returns error", func(t *testing.T) {
		graph := NewFlowGraph()
		mockComponent := createMockComponent("test-component", "processor")

		err := addTestComponentNode(graph, "test-component", mockComponent)
		require.NoError(t, err)

		// Adding same component again should return error
		err = addTestComponentNode(graph, "test-component", mockComponent)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestGetNodesReturnsIndependentInterfaceContracts(t *testing.T) {
	graph := NewFlowGraph()
	instance := createMockComponentWithPorts("reader", "processor", []component.Port{{
		Name:      "events",
		Direction: component.DirectionInput,
		Config: component.NATSPort{
			Subject:   "events.>",
			Interface: &component.InterfaceContract{Type: "example.events", Compatible: []string{"v1"}},
		},
	}}, nil)
	require.NoError(t, addTestComponentNode(graph, "reader", instance))

	first := graph.GetNodes()
	first["reader"].InputPorts[0].Interface.Type = "corrupt"
	first["reader"].InputPorts[0].Interface.Compatible[0] = "corrupt"

	again := graph.GetNodes()["reader"].InputPorts[0].Interface
	require.NotNil(t, again)
	assert.Equal(t, "example.events", again.Type)
	assert.Equal(t, []string{"v1"}, again.Compatible)
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
		err := addTestComponentNode(graph, "publisher", publisher)
		require.NoError(t, err)
		err = addTestComponentNode(graph, "subscriber", subscriber)
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
		assert.Equal(t, component.PatternStream, edge.Pattern)
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
		addTestComponentNode(graph, "publisher", publisher)
		addTestComponentNode(graph, "subscriber", subscriber)

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
		addTestComponentNode(graph, "publisher", publisher)
		addTestComponentNode(graph, "subscriber1", subscriber1)
		addTestComponentNode(graph, "subscriber2", subscriber2)

		err := graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Should have two connections (fan-out)
		edges := graph.GetEdges()
		assert.Len(t, edges, 2)

		// Both edges should be from publisher to different subscribers
		for _, edge := range edges {
			assert.Equal(t, "publisher", edge.From.ComponentName)
			assert.Equal(t, "output", edge.From.PortName)
			assert.Equal(t, component.PatternStream, edge.Pattern)
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
		addTestComponentNode(graph, "input", input)
		addTestComponentNode(graph, "processor", processor)
		addTestComponentNode(graph, "output", output)
		graph.ConnectComponentsByPatterns()

		// Analyze connectivity
		result := graph.AnalyzeConnectivity()
		require.NotNil(t, result)

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
		addTestComponentNode(graph, "connected1", connected1)
		addTestComponentNode(graph, "connected2", connected2)
		addTestComponentNode(graph, "isolated", isolated)
		graph.ConnectComponentsByPatterns()

		// Analyze connectivity
		result := graph.AnalyzeConnectivity()

		// The analysis reports facts, not severity: the isolated node is BOTH
		// disconnected and orphaned, and what that means is composition.Analyze's
		// call (ADR-100 D3).
		assert.Len(t, result.DisconnectedNodes, 1)
		assert.Len(t, result.OrphanedPorts, 1) // isolated component has orphaned input port

		orphanedPort := result.OrphanedPorts[0]
		assert.Equal(t, "isolated", orphanedPort.ComponentName)
		assert.Equal(t, "input", orphanedPort.PortName)
		assert.Equal(t, "isolated.data", orphanedPort.ConnectionID)
	})
}

// TestHTTPClientPortFlowGraph verifies the three flowgraph behaviours required
// by the HTTPClientPort contract:
//  1. canonical facts classify the port as component.PatternHTTPClient.
//  2. canonical facts surface the URL and reject a missing URL.
//  3. findOrphanedPorts does NOT flag an HTTPClientPort input as orphaned —
//     outbound client connections have no internal publisher by design.
func TestHTTPClientPortFlowGraph(t *testing.T) {
	g := &FlowGraph{nodes: make(map[string]*ComponentNode), edges: []FlowEdge{}}

	t.Run("canonical facts classify and identify HTTP client", func(t *testing.T) {
		infos, err := extractTestPortInfo(g, []component.Port{{
			Name: "http", Direction: component.DirectionInput,
			Config: component.HTTPClientPort{Method: "GET", URLPattern: "https://api.weather.gov/alerts/active"},
		}})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, component.PatternHTTPClient, infos[0].Pattern)
		assert.Equal(t, "https://api.weather.gov/alerts/active", infos[0].ConnectionID)
	})

	t.Run("missing URL is rejected", func(t *testing.T) {
		_, err := extractTestPortInfo(g, []component.Port{{
			Name: "http", Direction: component.DirectionInput, Config: component.HTTPClientPort{Method: "GET"},
		}})
		require.Error(t, err)
	})

	t.Run("HTTPClientPort input is NOT reported as orphaned", func(t *testing.T) {
		// A component with only an HTTPClientPort input and a NATS output is a
		// valid CAP-poller-style input component. Its HTTPClientPort has no
		// internal publisher, but that is correct — the external HTTP resource
		// IS the publisher. findOrphanedPorts must NOT flag it.
		comp := createMockComponentWithPorts("cap_poller", "input",
			[]component.Port{{
				Name:      "cap_feed",
				Direction: component.DirectionInput,
				Required:  true,
				Config: component.HTTPClientPort{
					Method:     "GET",
					URLPattern: "https://api.weather.gov/alerts/active",
				},
			}},
			[]component.Port{{
				Name:      "alerts_out",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "alerts.cap.>"},
			}},
		)
		graph := NewFlowGraph()
		require.NoError(t, addTestComponentNode(graph, "cap_poller", comp))

		orphans := graph.findOrphanedPorts()

		for _, o := range orphans {
			if o.PortName == "cap_feed" {
				t.Errorf("HTTPClientPort input %q incorrectly reported as orphaned (issue=%s); "+
					"outbound HTTP inputs have no internal publisher by design",
					o.PortName, o.Issue)
			}
		}
	})
}

// TestTimerPortFlowGraph verifies the flowgraph behaviours required by the
// TimerPort cadence-boundary contract (gh#312):
//  1. canonical facts classify the timer as component.PatternTimer.
//  2. findOrphanedPorts does NOT flag an unconnected TimerPort input as orphaned —
//     a cadence/scheduler boundary has no internal publisher by design.
//  3. extractPortInfo surfaces the polling cadence as the port's ConnectionID
//     ("timer:<interval>") so operators can inspect it from the flowgraph,
//     instead of the "unknown_type_*" fallthrough.
func TestTimerPortFlowGraph(t *testing.T) {
	g := &FlowGraph{nodes: make(map[string]*ComponentNode), edges: []FlowEdge{}}

	t.Run("canonical facts classify component.PatternTimer", func(t *testing.T) {
		infos, err := extractTestPortInfo(g, []component.Port{{
			Name: "timer", Direction: component.DirectionInput, Config: component.TimerPort{Interval: "30s"},
		}})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, component.PatternTimer, infos[0].Pattern)
	})

	t.Run("TimerPort input is NOT reported as orphaned", func(t *testing.T) {
		// A CAP-poller-style component: an HTTPClientPort whose cadence is a
		// sibling TimerPort input, plus a NATS output. The TimerPort has no
		// internal publisher — it IS the scheduler boundary — so
		// findOrphanedPorts must NOT flag it.
		comp := createMockComponentWithPorts("cap_poller", "input",
			[]component.Port{
				{
					Name:      "cap_feed",
					Direction: component.DirectionInput,
					Required:  true,
					Config: component.HTTPClientPort{
						Method:      "GET",
						URLPattern:  "https://api.weather.gov/alerts/active",
						TriggerPort: "poll_tick",
					},
				},
				{
					Name:      "poll_tick",
					Direction: component.DirectionInput,
					Config:    component.TimerPort{Interval: "30s"},
				},
			},
			[]component.Port{{
				Name:      "raw_alerts",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "alerts.cap.>"},
			}},
		)
		graph := NewFlowGraph()
		require.NoError(t, addTestComponentNode(graph, "cap_poller", comp))

		orphans := graph.findOrphanedPorts()

		for _, o := range orphans {
			if o.PortName == "poll_tick" {
				t.Errorf("TimerPort input %q incorrectly reported as orphaned (issue=%s); "+
					"a cadence/scheduler boundary has no internal publisher by design",
					o.PortName, o.Issue)
			}
		}
	})

	t.Run("TimerPort ConnectionID surfaces the interval", func(t *testing.T) {
		// The stored PortInfo.ConnectionID must expose the polling cadence
		// ("timer:30s") so operators can inspect it from the flowgraph, rather
		// than the "unknown_type_*" fallthrough it hit before gh#312.
		comp := createMockComponentWithPorts("cap_poller", "input",
			[]component.Port{{
				Name:      "poll_tick",
				Direction: component.DirectionInput,
				Config:    component.TimerPort{Interval: "30s"},
			}},
			nil,
		)
		graph := NewFlowGraph()
		require.NoError(t, addTestComponentNode(graph, "cap_poller", comp))

		node := graph.nodes["cap_poller"]
		require.NotNil(t, node)
		var found bool
		for _, p := range node.InputPorts {
			if p.Name == "poll_tick" {
				found = true
				assert.Equal(t, "timer:30s", p.ConnectionID,
					"TimerPort ConnectionID must surface the interval, not the unknown_type fallthrough")
			}
		}
		assert.True(t, found, "poll_tick port info must be present on the node")
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
