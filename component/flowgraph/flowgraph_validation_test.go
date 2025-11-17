package flowgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
)

// TestFlowGraphPortValidationRefinement tests enhanced port validation that handles different patterns
func TestFlowGraphPortValidationRefinement(t *testing.T) {
	t.Run("network boundary input ports should not be orphaned", func(t *testing.T) {
		// Network input ports (like UDP) are external sources and don't need publishers
		graph := NewFlowGraph()

		// Create UDP input component with network port
		udpPorts := []component.Port{
			{
				Name:      "udp_socket",
				Direction: component.DirectionInput,
				Config: component.NetworkPort{
					Protocol: "udp",
					Host:     "0.0.0.0",
					Port:     14550,
				},
			},
		}
		udpOutputPorts := []component.Port{
			{
				Name:      "data_out",
				Direction: component.DirectionOutput,
				Config: component.NATSPort{
					Subject: "input.udp.mavlink",
				},
			},
		}

		udpComponent := createMockComponentWithPorts("udp-input", "input", udpPorts, udpOutputPorts)
		err := graph.AddComponentNode("udp-input", udpComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// Network input port should NOT appear as orphaned
		for _, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "udp-input" && orphan.PortName == "udp_socket" {
				t.Errorf("Network input port should not be marked as orphaned: %+v", orphan)
			}
		}
	})

	t.Run("network boundary output ports should not be orphaned", func(t *testing.T) {
		// Network output ports (like WebSocket) are external sinks and don't need subscribers
		graph := NewFlowGraph()

		// Create WebSocket output component
		wsInputPorts := []component.Port{
			{
				Name:      "data_in",
				Direction: component.DirectionInput,
				Config: component.NATSPort{
					Subject: "control.>",
				},
			},
		}
		wsOutputPorts := []component.Port{
			{
				Name:      "websocket_endpoint",
				Direction: component.DirectionOutput,
				Config: component.NetworkPort{
					Protocol: "websocket",
					Host:     "localhost",
					Port:     8080,
				},
			},
		}

		wsComponent := createMockComponentWithPorts("websocket-output", "output", wsInputPorts, wsOutputPorts)
		err := graph.AddComponentNode("websocket-output", wsComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// Network output port should NOT appear as orphaned
		for _, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "websocket-output" && orphan.PortName == "websocket_endpoint" {
				t.Errorf("Network output port should not be marked as orphaned: %+v", orphan)
			}
		}
	})

	t.Run("request/response ports should be marked as optional not critical", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create component with request/response API
		apiPorts := []component.Port{
			{
				Name:      "api",
				Direction: component.DirectionInput,
				Config: component.NATSRequestPort{
					Subject: "storage.api",
					Timeout: "2s",
				},
			},
		}

		apiComponent := createMockComponentWithPorts("storage", "storage", apiPorts, nil)
		err := graph.AddComponentNode("storage", apiComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// Request port should be marked as optional, not critical
		var foundPort *OrphanedPort
		for i, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "storage" && orphan.PortName == "api" {
				foundPort = &analysis.OrphanedPorts[i]
				break
			}
		}

		if foundPort != nil {
			assert.Equal(t, "optional_api_unused", foundPort.Issue,
				"Request/response port should be marked as optional, not critical")
		}
	})

	t.Run("KV watch output ports can be intentionally unwatched", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create component with KV watch output
		kvOutputPorts := []component.Port{
			{
				Name:      "predicate_index",
				Direction: component.DirectionOutput,
				Config: component.KVWritePort{
					Bucket: "PREDICATE_INDEX",
				},
			},
		}

		kvComponent := createMockComponentWithPorts("graph", "processor", nil, kvOutputPorts)
		err := graph.AddComponentNode("graph", kvComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// KV watch output should be marked as optional, not critical
		var foundPort *OrphanedPort
		for i, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "graph" && orphan.PortName == "predicate_index" {
				foundPort = &analysis.OrphanedPorts[i]
				break
			}
		}

		if foundPort != nil {
			assert.Equal(t, "optional_index_unwatched", foundPort.Issue,
				"KV watch output should be marked as optional, not critical")
		}
	})

	t.Run("stream ports without connections should be marked as critical", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create component with unconnected stream port
		streamPorts := []component.Port{
			{
				Name:      "data_stream",
				Direction: component.DirectionInput,
				Config: component.NATSPort{
					Subject: "data.stream.unconnected",
				},
			},
		}

		streamComponent := createMockComponentWithPorts("processor", "processor", streamPorts, nil)
		err := graph.AddComponentNode("processor", streamComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// Stream port should be marked as critical (no_publishers)
		var foundPort *OrphanedPort
		for i, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "processor" && orphan.PortName == "data_stream" {
				foundPort = &analysis.OrphanedPorts[i]
				break
			}
		}

		require.NotNil(t, foundPort, "Unconnected stream port should be in orphaned list")
		assert.Equal(t, "no_publishers", foundPort.Issue,
			"Unconnected stream port should be marked as critical")
	})

	t.Run("validation should categorize issues by severity", func(t *testing.T) {
		graph := NewFlowGraph()

		// Add various components with different port patterns

		// 1. Network boundary (should be excluded)
		udpPorts := []component.Port{
			{
				Name:      "udp_socket",
				Direction: component.DirectionInput,
				Config: component.NetworkPort{
					Protocol: "udp",
					Host:     "0.0.0.0",
					Port:     14550,
				},
			},
		}
		udpComponent := createMockComponentWithPorts("udp", "input", udpPorts, nil)
		err := graph.AddComponentNode("udp", udpComponent)
		require.NoError(t, err)

		// 2. Optional API (should be marked optional)
		apiPorts := []component.Port{
			{
				Name:      "api",
				Direction: component.DirectionInput,
				Config: component.NATSRequestPort{
					Subject: "api.endpoint",
					Timeout: "1s",
				},
			},
		}
		apiComponent := createMockComponentWithPorts("api", "processor", apiPorts, nil)
		err = graph.AddComponentNode("api", apiComponent)
		require.NoError(t, err)

		// 3. Critical stream (should be marked critical)
		streamPorts := []component.Port{
			{
				Name:      "critical_stream",
				Direction: component.DirectionInput,
				Config: component.NATSPort{
					Subject: "critical.data",
				},
			},
		}
		streamComponent := createMockComponentWithPorts("stream", "processor", streamPorts, nil)
		err = graph.AddComponentNode("stream", streamComponent)
		require.NoError(t, err)

		// Connect and analyze
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)
		analysis := graph.AnalyzeConnectivity()

		// Verify categorization
		criticalCount := 0
		optionalCount := 0
		excludedCount := 0

		for _, orphan := range analysis.OrphanedPorts {
			switch orphan.Issue {
			case "no_publishers", "no_subscribers":
				criticalCount++
			case "optional_api_unused", "optional_index_unwatched":
				optionalCount++
			}

			// Network ports should be completely excluded
			if orphan.ComponentName == "udp" && orphan.PortName == "udp_socket" {
				excludedCount++
				t.Error("Network boundary port should not appear in orphaned list at all")
			}
		}

		assert.Equal(t, 1, criticalCount, "Should have 1 critical orphaned port (stream)")
		assert.Equal(t, 1, optionalCount, "Should have 1 optional orphaned port (API)")
		assert.Equal(t, 0, excludedCount, "Should have 0 network boundary ports in orphaned list")
	})
}

// TestOrphanedPortSeverity tests that we can determine severity of orphaned ports
func TestOrphanedPortSeverity(t *testing.T) {
	testCases := []struct {
		name     string
		port     OrphanedPort
		expected string
	}{
		{
			name: "stream input without publisher is critical",
			port: OrphanedPort{
				Pattern: PatternStream,
				Issue:   "no_publishers",
			},
			expected: "critical",
		},
		{
			name: "stream output without subscriber is critical",
			port: OrphanedPort{
				Pattern: PatternStream,
				Issue:   "no_subscribers",
			},
			expected: "critical",
		},
		{
			name: "request API without clients is optional",
			port: OrphanedPort{
				Pattern: PatternRequest,
				Issue:   "optional_api_unused",
			},
			expected: "warning",
		},
		{
			name: "KV index without watchers is optional",
			port: OrphanedPort{
				Pattern: PatternWatch,
				Issue:   "optional_index_unwatched",
			},
			expected: "warning",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			severity := getOrphanedPortSeverity(tc.port)
			assert.Equal(t, tc.expected, severity)
		})
	}
}

// Helper function to categorize orphaned port severity
func getOrphanedPortSeverity(port OrphanedPort) string {
	switch port.Issue {
	case "no_publishers", "no_subscribers":
		// Stream connections are critical for data flow
		if port.Pattern == PatternStream {
			return "critical"
		}
		return "warning"
	case "optional_api_unused", "optional_index_unwatched", "optional_interface_unused":
		// Optional ports are warnings
		return "warning"
	default:
		return "info"
	}
}

// TestFlowGraphInterfaceAlternatives tests the detection of interface-specific alternative ports
func TestFlowGraphInterfaceAlternatives(t *testing.T) {
	t.Run("interface-specific alternatives should be marked as optional", func(t *testing.T) {
		graph := NewFlowGraph()

		// Create ObjectStore-like component with two write ports
		storageInputPorts := []component.Port{
			{
				Name:      "write",
				Direction: component.DirectionInput,
				Required:  false,
				Config: component.NATSPort{
					Subject: "storage.objectstore.write",
				},
			},
			{
				Name:      "write-graphable",
				Direction: component.DirectionInput,
				Required:  false,
				Config: component.NATSPort{
					Subject: "storage.objectstore.graphable",
					Interface: &component.InterfaceContract{
						Type:    "message.Graphable",
						Version: "v1",
					},
				},
			},
		}

		storageComponent := createMockComponentWithPorts("objectstore", "storage", storageInputPorts, nil)
		err := graph.AddComponentNode("objectstore", storageComponent)
		require.NoError(t, err)

		// Connect components by patterns
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)

		// Analyze connectivity
		analysis := graph.AnalyzeConnectivity()

		// Find the write-graphable port in orphaned list
		var graphablePort *OrphanedPort
		var normalWritePort *OrphanedPort

		for i, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "objectstore" {
				if orphan.PortName == "write-graphable" {
					graphablePort = &analysis.OrphanedPorts[i]
				} else if orphan.PortName == "write" {
					normalWritePort = &analysis.OrphanedPorts[i]
				}
			}
		}

		// The regular write port should be marked as no_publishers
		require.NotNil(t, normalWritePort, "Regular write port should be in orphaned list")
		assert.Equal(t, "no_publishers", normalWritePort.Issue,
			"Regular write port should be marked as no_publishers")

		// The interface-specific alternative should be marked as optional
		require.NotNil(t, graphablePort, "Interface alternative port should be in orphaned list")
		assert.Equal(t, "optional_interface_unused", graphablePort.Issue,
			"Interface alternative should be marked as optional_interface_unused")

		// Verify severity categorization
		assert.Equal(t, "warning", getOrphanedPortSeverity(*graphablePort),
			"Interface alternatives should be warnings, not critical")
	})

	t.Run("interface alternatives with naming pattern should be detected", func(t *testing.T) {
		graph := NewFlowGraph()

		// Test various naming patterns that suggest interface alternatives
		testPorts := []component.Port{
			{
				Name:      "input-typed",
				Direction: component.DirectionInput,
				Required:  false,
				Config: component.NATSPort{
					Subject: "processor.input.typed",
					Interface: &component.InterfaceContract{
						Type: "CustomType",
					},
				},
			},
			{
				Name:      "data-validated",
				Direction: component.DirectionInput,
				Required:  false,
				Config: component.NATSPort{
					Subject: "processor.data.validated",
					Interface: &component.InterfaceContract{
						Type: "ValidatedData",
					},
				},
			},
		}

		testComponent := createMockComponentWithPorts("processor", "processor", testPorts, nil)
		err := graph.AddComponentNode("processor", testComponent)
		require.NoError(t, err)

		// Connect and analyze
		err = graph.ConnectComponentsByPatterns()
		require.NoError(t, err)
		analysis := graph.AnalyzeConnectivity()

		// All interface-specific ports with naming patterns should be marked as optional
		for _, orphan := range analysis.OrphanedPorts {
			if orphan.ComponentName == "processor" &&
				(orphan.PortName == "input-typed" || orphan.PortName == "data-validated") {
				assert.Equal(t, "optional_interface_unused", orphan.Issue,
					"Interface ports with naming patterns should be marked as optional")
			}
		}
	})
}
