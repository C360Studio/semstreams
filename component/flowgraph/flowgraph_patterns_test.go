package flowgraph

import (
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlowGraphPatterns tests all connection pattern implementations
func TestFlowGraphPatterns(t *testing.T) {
	t.Run("nil checks in AddComponentNode", func(t *testing.T) {
		graph := NewFlowGraph()

		// Test nil component
		err := graph.AddComponentNode("test", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "component cannot be nil")

		// Test empty name
		mockComp := createPatternTestComponent("mock", []component.Port{}, []component.Port{})
		err = graph.AddComponentNode("", mockComp)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "component name cannot be empty")
	})

	t.Run("Request pattern connects bidirectionally", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create components with request ports on same subject
		clientPorts := []component.Port{
			{
				Name:      "api_request",
				Direction: component.DirectionOutput,
				Config:    component.NATSRequestPort{Subject: "api.v1"},
			},
		}
		serverPorts := []component.Port{
			{
				Name:      "api_handler",
				Direction: component.DirectionInput,
				Config:    component.NATSRequestPort{Subject: "api.v1"},
			},
		}

		client := createPatternTestComponent("api-client", []component.Port{}, clientPorts)
		server := createPatternTestComponent("api-server", serverPorts, []component.Port{})

		require.NoError(t, graph.AddComponentNode("api-client", client))
		require.NoError(t, graph.AddComponentNode("api-server", server))

		// Connect by patterns
		err := graph.ConnectComponentsByPatterns()
		assert.NoError(t, err)

		// Check that bidirectional edge was created
		edges := graph.GetEdges()
		assert.Len(t, edges, 1, "Should have one edge for bidirectional request")
		if len(edges) > 0 {
			edge := edges[0]
			assert.Equal(t, PatternRequest, edge.Pattern)
			assert.Equal(t, "api.v1", edge.ConnectionID)
			// Should connect the two components
			assert.True(t,
				(edge.From.ComponentName == "api-client" && edge.To.ComponentName == "api-server") ||
					(edge.From.ComponentName == "api-server" && edge.To.ComponentName == "api-client"),
				"Edge should connect client and server bidirectionally")
		}
	})

	t.Run("Watch pattern connects and warns on multiple writers", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create two writers to same KV bucket
		writer1Ports := []component.Port{
			{
				Name:      "state_writer",
				Direction: component.DirectionOutput,
				Config:    component.KVWatchPort{Bucket: "entity_states"},
			},
		}
		writer2Ports := []component.Port{
			{
				Name:      "state_writer2",
				Direction: component.DirectionOutput,
				Config:    component.KVWatchPort{Bucket: "entity_states"},
			},
		}
		watcherPorts := []component.Port{
			{
				Name:      "state_watcher",
				Direction: component.DirectionInput,
				Config:    component.KVWatchPort{Bucket: "entity_states"},
			},
		}

		writer1 := createPatternTestComponent("writer1", []component.Port{}, writer1Ports)
		writer2 := createPatternTestComponent("writer2", []component.Port{}, writer2Ports)
		watcher := createPatternTestComponent("watcher", watcherPorts, []component.Port{})

		require.NoError(t, graph.AddComponentNode("writer1", writer1))
		require.NoError(t, graph.AddComponentNode("writer2", writer2))
		require.NoError(t, graph.AddComponentNode("watcher", watcher))

		// Connect by patterns - should get warning about multiple writers
		err := graph.ConnectComponentsByPatterns()
		assert.Error(t, err, "Should warn about multiple writers")
		if err != nil {
			assert.Contains(t, err.Error(), "Multiple writers to KV bucket")
		}

		// But edges should still be created
		edges := graph.GetEdges()
		assert.Len(t, edges, 2, "Should have edges from both writers to watcher")
		for _, edge := range edges {
			assert.Equal(t, PatternWatch, edge.Pattern)
			assert.Equal(t, "entity_states", edge.ConnectionID)
		}
	})

	t.Run("Network pattern detects port conflicts", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create two components trying to bind to same network port
		server1Ports := []component.Port{
			{
				Name:      "http_server",
				Direction: component.DirectionInput,
				Config:    component.NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 8080},
			},
		}
		server2Ports := []component.Port{
			{
				Name:      "http_server2",
				Direction: component.DirectionInput,
				Config:    component.NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 8080},
			},
		}

		server1 := createPatternTestComponent("server1", server1Ports, []component.Port{})
		server2 := createPatternTestComponent("server2", server2Ports, []component.Port{})

		require.NoError(t, graph.AddComponentNode("server1", server1))
		require.NoError(t, graph.AddComponentNode("server2", server2))

		// Connect by patterns - should detect conflict
		err := graph.ConnectComponentsByPatterns()
		assert.Error(t, err, "Should detect network port conflict")
		if err != nil {
			assert.Contains(t, err.Error(), "Network port conflict")
			assert.Contains(t, err.Error(), "tcp:0.0.0.0:8080")
		}

		// No edges should be created for network ports
		edges := graph.GetEdges()
		for _, edge := range edges {
			assert.NotEqual(t, PatternNetwork, edge.Pattern, "Network ports don't create edges")
		}
	})

	t.Run("Stream pattern still works", func(t *testing.T) {
		graph := NewFlowGraph()

		// Traditional NATS pub/sub
		pubPorts := []component.Port{
			{
				Name:      "events",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "events.data"},
			},
		}
		subPorts := []component.Port{
			{
				Name:      "events",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "events.data"},
			},
		}

		pub := createPatternTestComponent("publisher", []component.Port{}, pubPorts)
		sub := createPatternTestComponent("subscriber", subPorts, []component.Port{})

		require.NoError(t, graph.AddComponentNode("publisher", pub))
		require.NoError(t, graph.AddComponentNode("subscriber", sub))

		// Connect by patterns
		err := graph.ConnectComponentsByPatterns()
		assert.NoError(t, err)

		// Check edge
		edges := graph.GetEdges()
		assert.Len(t, edges, 1)
		if len(edges) > 0 {
			edge := edges[0]
			assert.Equal(t, PatternStream, edge.Pattern)
			assert.Equal(t, "events.data", edge.ConnectionID)
			assert.Equal(t, "publisher", edge.From.ComponentName)
			assert.Equal(t, "subscriber", edge.To.ComponentName)
		}
	})

	t.Run("KVWritePort to KVWatchPort connections work correctly", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create graph processor (writer) using KVWritePort
		writerPorts := []component.Port{
			{
				Name:      "entity_states",
				Direction: component.DirectionOutput,
				Config: component.KVWritePort{
					Bucket: "ENTITY_STATES",
					Interface: &component.InterfaceContract{
						Type:    "graph.EntityState",
						Version: "v1",
					},
				},
			},
		}

		// Create rule processor (watcher) using KVWatchPort
		watcherPorts := []component.Port{
			{
				Name:      "entity_states",
				Direction: component.DirectionInput,
				Config: component.KVWatchPort{
					Bucket: "ENTITY_STATES",
					Keys:   []string{},
				},
			},
		}

		writer := createPatternTestComponent("graph-processor", []component.Port{}, writerPorts)
		watcher := createPatternTestComponent("rule-processor", watcherPorts, []component.Port{})

		require.NoError(t, graph.AddComponentNode("graph-processor", writer))
		require.NoError(t, graph.AddComponentNode("rule-processor", watcher))

		// Connect by patterns - should work without warnings
		err := graph.ConnectComponentsByPatterns()
		assert.NoError(t, err, "KVWritePort -> KVWatchPort should connect cleanly")

		// Check that edge was created correctly
		edges := graph.GetEdges()
		assert.Len(t, edges, 1, "Should have one edge from writer to watcher")
		if len(edges) > 0 {
			edge := edges[0]
			assert.Equal(t, PatternWatch, edge.Pattern)
			assert.Equal(t, "ENTITY_STATES", edge.ConnectionID)
			assert.Equal(t, "graph-processor", edge.From.ComponentName)
			assert.Equal(t, "rule-processor", edge.To.ComponentName)
		}
	})

	t.Run("extractConnectionID handles nil and missing data", func(t *testing.T) {
		graph := NewFlowGraph()

		// Test nil config
		result := graph.extractConnectionID(nil)
		assert.Equal(t, "nil_port_config", result)

		// Test empty NATS subject
		result = graph.extractConnectionID(component.NATSPort{Subject: ""})
		assert.Equal(t, "nats_missing_subject", result)

		// Test empty KV bucket for watch
		result = graph.extractConnectionID(component.KVWatchPort{Bucket: ""})
		assert.Equal(t, "kv_missing_bucket", result)

		// Test empty KV bucket for write
		result = graph.extractConnectionID(component.KVWritePort{Bucket: ""})
		assert.Equal(t, "kv_missing_bucket", result)

		// Test incomplete network port
		result = graph.extractConnectionID(component.NetworkPort{Protocol: "tcp", Host: "", Port: 0})
		assert.Contains(t, result, "network_incomplete")
	})

	t.Run("NATS wildcard pattern matching", func(t *testing.T) {
		// Test exact match
		assert.True(t, matchNATSPattern("foo.bar", "foo.bar"), "Exact match should work")

		// Test single token wildcard (*)
		assert.True(t, matchNATSPattern("input.udp.mavlink", "input.*.mavlink"), "* should match single token")
		assert.True(t, matchNATSPattern("foo.bar.baz", "foo.*.baz"), "* should match middle token")
		assert.True(t, matchNATSPattern("foo.bar", "*.bar"), "* should match first token")
		assert.True(t, matchNATSPattern("foo.bar", "foo.*"), "* should match last token")

		// Test multi-token wildcard (>)
		assert.True(t, matchNATSPattern("foo.bar.baz.qux", "foo.>"), "> should match multiple tokens")
		assert.True(t, matchNATSPattern("foo", "foo.>"), "> should match zero tokens")

		// Test non-matches
		assert.False(t, matchNATSPattern("foo.bar.baz", "foo.*.qux"), "* shouldn't match wrong token")
		assert.False(t, matchNATSPattern("foo.bar.baz", "foo.*"), "* requires exact token count")
		assert.False(t, matchNATSPattern("foo", "foo.bar"), "No match with more pattern tokens")

		// Test bidirectional matching (pattern in either position)
		assert.True(
			t,
			matchNATSPattern("input.*.mavlink", "input.udp.mavlink"),
			"Pattern should match concrete subject",
		)
	})

	t.Run("Stream pattern with wildcard connections", func(t *testing.T) {
		graph := NewFlowGraph()

		// UDP input publishes to concrete subject
		pubPorts := []component.Port{
			{
				Name:      "output",
				Direction: component.DirectionOutput,
				Config:    component.NATSPort{Subject: "input.udp.mavlink"},
			},
		}

		// Robotics processor subscribes with wildcard
		subPorts := []component.Port{
			{
				Name:      "input",
				Direction: component.DirectionInput,
				Config:    component.NATSPort{Subject: "input.*.mavlink"},
			},
		}

		pub := createPatternTestComponent("udp-input", []component.Port{}, pubPorts)
		sub := createPatternTestComponent("robotics-processor", subPorts, []component.Port{})

		require.NoError(t, graph.AddComponentNode("udp-input", pub))
		require.NoError(t, graph.AddComponentNode("robotics-processor", sub))

		// Connect by patterns
		err := graph.ConnectComponentsByPatterns()
		assert.NoError(t, err)

		// Check that wildcard match created edge
		edges := graph.GetEdges()
		assert.Len(t, edges, 1, "Wildcard pattern should match concrete subject")
		if len(edges) > 0 {
			edge := edges[0]
			assert.Equal(t, PatternStream, edge.Pattern)
			assert.Equal(t, "input.udp.mavlink", edge.ConnectionID, "Should use concrete subject, not pattern")
			assert.Equal(t, "udp-input", edge.From.ComponentName)
			assert.Equal(t, "robotics-processor", edge.To.ComponentName)
		}
	})
}

// Helper function to create mock components for pattern tests
func createPatternTestComponent(name string, inputs []component.Port, outputs []component.Port) component.Discoverable {
	return &mockFlowGraphComponent{
		metadata: component.Metadata{
			Name: name,
		},
		inputPorts:  inputs,
		outputPorts: outputs,
	}
}
