// Package flowgraph provides flow graph analysis and validation for component connections.
//
// # Overview
//
// The flowgraph package builds a directed graph representation of component port
// connections, enabling static analysis to detect configuration issues before runtime.
// It supports five interaction patterns (stream, request, watch, network, http-client) and provides:
//
//   - Pattern-based connection matching (NATS wildcard subject matching)
//   - Connected component analysis (DFS-based clustering)
//   - Orphaned port detection with severity classification
//   - JetStream stream requirement validation
//
// This enables early detection of misconfigurations such as missing publishers,
// dangling subscribers, or JetStream/NATS protocol mismatches.
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                           FlowGraph                                     │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│  ComponentNode[]          FlowEdge[]           InteractionPattern       │
//	│  - InputPorts             - From (portref)     - stream (NATS/JS)       │
//	│  - OutputPorts            - To (portref)       - request (req/reply)    │
//	│  - Component ref          - Pattern            - watch (KV)             │
//	│                           - ConnectionID       - network (external)     │
//	│                                                - http-client (outbound) │
//	└─────────────────────────────────────────────────────────────────────────┘
//	                                 ↓
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                       FlowAnalysisResult                                │
//	│  - ConnectedComponents: clusters of connected components               │
//	│  - OrphanedPorts: ports with no connections                             │
//	│  - DisconnectedNodes: components with no edges                          │
//	│  - ValidationStatus: healthy/warnings                                   │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Usage
//
// Build and analyze a flow graph:
//
//	graph := flowgraph.NewFlowGraph()
//
//	// Add components
//	for name, comp := range components {
//	    if err := graph.AddComponentNode(name, comp); err != nil {
//	        return err
//	    }
//	}
//
//	// Build edges by matching connection patterns
//	if err := graph.ConnectComponentsByPatterns(); err != nil {
//	    return err
//	}
//
//	// Analyze connectivity
//	result := graph.AnalyzeConnectivity()
//	if result.ValidationStatus == "warnings" {
//	    for _, orphan := range result.OrphanedPorts {
//	        log.Warn("orphaned port", "component", orphan.ComponentName, "port", orphan.PortName)
//	    }
//	}
//
// Validate JetStream requirements:
//
//	warnings := graph.ValidateStreamRequirements()
//	for _, w := range warnings {
//	    if w.Severity == "critical" {
//	        return fmt.Errorf("JetStream mismatch: %s", w.Issue)
//	    }
//	}
//
// # Interaction Patterns
//
// PatternStream (NATS, JetStream):
//   - Unidirectional: publishers → subscribers
//   - Supports NATS wildcard matching (* and >)
//   - Multiple publishers/subscribers per subject allowed
//
// PatternRequest (NATS request/reply):
//   - Bidirectional: any port can initiate requests
//   - All ports with same subject are connected
//   - Used for synchronous RPC-style communication
//
// PatternWatch (KV bucket):
//   - Unidirectional: writers → watchers
//   - Multiple writers to same bucket generates warning
//   - Watchers receive change notifications
//
// PatternNetwork (external listener):
//   - External boundary ports (HTTP server, UDP listener, etc.)
//   - Exclusive binding: multiple binds to same port is an error
//   - Not connected in graph (external endpoints)
//
// PatternHTTPClient (outbound HTTP-client/polling):
//   - Component initiates connections to an external HTTP resource
//   - Non-exclusive: multiple components may poll the same URL
//   - Not connected in graph; treated like PatternNetwork for orphan detection
//   - Declared via HTTPClientPort; cadence is owned by a sibling TimerPort
//
// # Connection Matching
//
// NATS subject matching follows standard semantics:
//   - Exact match: "graph.ingest.data" matches "graph.ingest.data"
//   - Single wildcard: "graph.*.data" matches "graph.ingest.data"
//   - Multi wildcard: "graph.>" matches "graph.ingest.data"
//   - Bidirectional: either side can be the pattern
//
// # Analysis Results
//
// FlowAnalysisResult contains:
//
// ConnectedComponents: Clusters of interconnected components found via DFS.
// Multiple clusters indicate isolated subgraphs.
//
// DisconnectedNodes: Components with zero edges. These may be misconfigured
// or intentionally standalone.
//
// OrphanedPorts: Ports with no matching connections. Classified by issue type:
//   - no_publishers: Input port has no matching output ports
//   - no_subscribers: Output port has no matching input ports
//   - optional_api_unused: Request ports are optional by design
//   - optional_interface_unused: Interface-specific alternative ports
//   - optional_index_unwatched: KV watch ports may be intentionally unused
//
// ValidationStatus: "healthy" if no issues, "warnings" if any problems detected.
//
// # JetStream Validation
//
// ValidateStreamRequirements detects protocol mismatches:
//   - JetStream subscribers expect a durable stream to exist
//   - Streams are created by JetStream output ports (via EnsureStreams)
//   - If a JetStream subscriber connects only to NATS publishers, no stream is created
//   - This results in the subscriber hanging indefinitely
//
// The validation returns critical severity warnings for such mismatches.
//
// # Thread Safety
//
// FlowGraph is NOT safe for concurrent modification. It is designed for
// single-threaded construction and analysis during service startup.
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/component]: Port types and component interfaces
//   - [github.com/c360studio/semstreams/service]: Uses flowgraph for flow validation
package flowgraph
