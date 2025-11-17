// Package semstreams provides a two-layer framework for semantic stream processing,
// combining protocol-level data handling with generic semantic capabilities.
//
// # Philosophy: Two-Layer Generic Framework
//
// SemStreams is a generic framework with two independent layers:
//
// Layer 1 - Protocol Layer (network/data agnostic):
//   - Network Protocols: UDP, TCP, HTTP, WebSocket
//   - Data Formats: JSON, CSV, raw bytes
//   - Messaging: NATS (pub/sub, JetStream, KV)
//   - Infrastructure: Metrics, health checks, logging
//
// Layer 2 - Semantic Layer (domain agnostic):
//   - Entity tracking: Generic entity lifecycle and relationships
//   - Graph structures: Entity graphs and patterns
//   - Vocabularies: Extensible semantic vocabularies (SOSA/SSN compatible)
//   - Enrichment: Generic semantic enrichment pipeline
//
// SemStreams MUST NOT contain:
//   - Domain-specific protocols (MAVLink, ROS, industrial protocols)
//   - Domain-specific business logic (robotics control, IoT rules)
//   - Domain assumptions (robot types, sensor models, industrial processes)
//
// Domain-specific code belongs in separate modules (semstreams-robotics, semstreams-iot)
//
// # Architecture
//
// SemStreams provides a two-layer foundation for semantic stream processing:
//
//	┌─────────────────────────────────────┐
//	│          Flow Engine                │  Component lifecycle
//	│  (start, stop, connect, monitor)    │  State management
//	└─────────────────────────────────────┘
//	           ↓ orchestrates
//	┌─────────────────────────────────────┐
//	│         Components                  │  Inputs, Processors,
//	│   (input, processor, output)        │  Outputs, Services
//	└─────────────────────────────────────┘
//	           ↓ communicate via
//	┌─────────────────────────────────────┐
//	│         NATS Messaging              │  Subjects, streams,
//	│     (pub/sub, request/reply)        │  KV stores
//	└─────────────────────────────────────┘
//
// # Advanced Patterns
//
// Fan-Out Pattern (Multiple Outputs):
//
// Multiple components can subscribe to the same NATS subject, creating
// parallel output paths. Each output processes independently - failure
// in one doesn't affect others. This pattern is demonstrated in the
// core-flow.json configuration.
//
//	                ┌─────────────┐
//	                │  Processor  │
//	                │  (JSONMap)  │
//	                └──────┬──────┘
//	                       │
//	            mapped.messages (NATS subject)
//	                       │
//	     ┌─────────────────┼─────────────────┐
//	     ↓                 ↓                 ↓
//	┌────────┐       ┌──────────┐      ┌──────────┐
//	│  File  │       │HTTPPost  │      │ObjectStore│
//	│ Output │       │ Output   │      │ Storage  │
//	└────────┘       └──────────┘      └──────────┘
//	 /tmp/*.jsonl    webhook endpoint   NATS bucket
//
// Benefits:
//   - Parallel processing: Outputs run concurrently
//   - Fault isolation: One output failure doesn't affect others
//   - Dynamic subscription: Add/remove outputs without code changes
//   - Load distribution: NATS handles message delivery
//
// Early Tap Pattern (Raw Data Access):
//
// Components can tap into data streams at any point in the pipeline.
// Example: WebSocket output subscribing to raw UDP data before processing.
//
//	    ┌─────────┐
//	    │   UDP   │
//	    │  Input  │
//	    └────┬────┘
//	         │
//	    raw.udp.messages
//	         │
//	    ┌────┼────────────────────┐
//	    ↓    ↓                    ↓
//	┌────────┐              ┌──────────┐
//	│WebSocket│             │Processors│
//	│ Output  │             │(filter,  │
//	│ (raw)   │             │ map...)  │
//	└────────┘              └──────────┘
//	:8082/ws                      │
//	                         mapped.messages
//	                              ↓
//	                        (other outputs)
//
// Use cases:
//   - Real-time monitoring: Show raw data in dashboards
//   - Debugging: Inspect data before transformation
//   - Parallel processing: Different paths for raw vs processed
//   - Low-latency: Skip processing overhead
//
// Federation Pattern (Instance-to-Instance):
//
// StreamKit instances can communicate over network boundaries using
// WebSocket connections. This enables edge-to-cloud, multi-region,
// and hierarchical processing topologies.
//
//	┌────────────────────────────┐     ┌────────────────────────────┐
//	│      Instance A (Edge)     │     │     Instance B (Cloud)     │
//	│                            │     │                            │
//	│  ┌──────┐                  │     │                            │
//	│  │ UDP  │                  │     │                            │
//	│  │Input │                  │     │                            │
//	│  └───┬──┘                  │     │                            │
//	│      ↓                     │     │                            │
//	│  ┌──────────┐              │     │                            │
//	│  │Processors│              │     │  ┌──────────────┐          │
//	│  │(filter,  │              │     │  │  WebSocket   │          │
//	│  │ map...)  │              │     │  │    Input     │          │
//	│  └────┬─────┘              │     │  │  :8081/ingest│          │
//	│       ↓                    │     │  └───────┬──────┘          │
//	│  ┌──────────────┐          │     │          ↓                 │
//	│  │  WebSocket   │          │  ws://        ┌──────────┐       │
//	│  │    Output    ├──────────┼─────────────→ │federated │       │
//	│  │:8080/federation         │     │         │  .data   │       │
//	│  └──────────────┘          │     │         └────┬─────┘       │
//	│                            │     │              ↓              │
//	└────────────────────────────┘     │      ┌───────────────┐     │
//	                                   │      │  Processors   │     │
//	                                   │      │   Storage     │     │
//	                                   │      │   Analytics   │     │
//	                                   │      └───────────────┘     │
//	                                   └────────────────────────────┘
//
// Federation use cases:
//   - Edge-to-Cloud: IoT devices send processed data to central hub
//   - Multi-Region: Cross-datacenter data replication
//   - Hierarchical: Pre-process at edge, aggregate at cloud
//   - Failover: Route data to backup instance on failure
//
// Bidirectional Communication (Request/Reply):
//
// WebSocket connections are bidirectional, enabling the input instance
// to send requests back to the output instance. This unlocks advanced
// patterns beyond simple data streaming.
//
//	┌────────────────┐                    ┌────────────────┐
//	│  Instance A    │                    │  Instance B    │
//	│  (Data Source) │                    │  (Subscriber)  │
//	│                │                    │                │
//	│  WebSocket Out │◄───── REQUEST ─────│ WebSocket In   │
//	│  :8080         │                    │ :8081          │
//	│                │                    │                │
//	│                │─────► REPLY ───────►                │
//	│                │                    │                │
//	│                │                    │                │
//	│                │───► DATA STREAM ───►                │
//	└────────────────┘                    └────────────────┘
//
// Request/Reply patterns enabled:
//   - Backpressure: "Slow down, queue full" → adjust send rate
//   - Selective subscription: "Only send type=sensor" → filter upstream
//   - Historical query: "Send last 100 messages" → replay from buffer
//   - Status queries: "What's your throughput?" → observability
//   - Dynamic routing: "I handle region=us-west" → route by capability
//   - Acknowledgment: "Received batch ID=123" → reliable delivery
//
// This maps naturally to NATS request/reply pattern, exposed over WebSocket:
//   - Client sends request on "ws.request.{topic}"
//   - Server replies on "ws.reply.{topic}.{client_id}"
//   - Standard NATS timeout/retry semantics apply
//
// Example: Backpressure Control
//
//	Instance B → Instance A:
//	  Request: {"type": "backpressure", "rate_limit": 100, "unit": "msg/sec"}
//	  Reply:   {"status": "ok", "current_rate": 250, "adjusted_to": 100}
//
// Example: Selective Subscription
//
//	Instance B → Instance A:
//	  Request: {"type": "subscribe", "filter": "$.severity >= 'warning'"}
//	  Reply:   {"status": "ok", "subscription_id": "sub-123"}
//
// This bidirectional capability transforms federation from simple data
// forwarding into a distributed control plane.
//
// # Framework Packages
//
// Protocol Layer - Component System:
//   - component: Component lifecycle, registry, port definitions
//   - componentregistry: Registration of protocol and semantic components
//
// Protocol Layer - Flow Management:
//   - engine: Component orchestration and lifecycle
//   - flowstore: Flow persistence (NATS KV)
//   - config: Configuration loading and validation
//
// Protocol Layer - Infrastructure:
//   - natsclient: NATS connection management
//   - service: HTTP services (discovery, flow-builder, metrics)
//   - metric: Prometheus metrics
//   - errors: Structured error handling
//   - health: Health check system
//
// Protocol Layer - Components (Input):
//   - input/udp: UDP socket input
//   - input/websocket_input: WebSocket input for federation
//
// Protocol Layer - Components (Output):
//   - output/websocket: WebSocket broadcasting
//   - output/file: File output
//   - output/httppost: HTTP POST output
//
// Protocol Layer - Components (Processor):
//   - processor/parser: JSON/CSV parsing
//   - processor/json_map: JSON field mapping
//   - processor/json_filter: JSON filtering
//   - processor/json_generic: Generic JSON processing
//
// Protocol Layer - Utilities:
//   - pkg/buffer: Ring buffer for streaming
//   - pkg/cache: LRU caching
//   - pkg/retry: Retry policies
//   - pkg/worker: Worker pools
//   - pkg/timestamp: Time utilities
//
// Semantic Layer - Components:
//   - (To be added during merge from semstreams-old)
//   - Entity extraction and tracking
//   - Semantic enrichment
//   - Graph operations
//   - Vocabulary management
//
// # Layer Independence
//
// The two layers are independent - protocol layer has NO dependencies on semantic layer:
//
//  1. Protocol components work without semantic components loaded
//  2. Semantic components extend protocol layer via standard component interfaces
//  3. Domain code (robotics, IoT) goes in separate modules
//  4. Both layers are generic and extensible
//
// Verification:
//
//	# Protocol-only flows work without semantic components
//	./bin/semstreams --config configs/protocol-flow.json
//
//	# No domain-specific code in framework
//	! grep -r 'MAVLink|ROS|robotics|drone' semstreams/ --include="*.go"
//
// # Usage Patterns
//
// Basic Flow Setup:
//
//	// Create NATS client
//	natsClient, _ := natsclient.NewClient("nats://localhost:4222")
//	natsClient.Connect(ctx)
//
//	// Create component registry with protocol and semantic components
//	registry := component.NewRegistry()
//	componentregistry.Register(registry)
//
//	// Create flow engine
//	engine := engine.NewEngine(natsClient, registry, logger)
//
//	// Load and start flow
//	flow, _ := loadFlow("udp-to-websocket.json")
//	engine.StartFlow(ctx, flow)
//
// Custom Protocol Component:
//
//	// Register a custom protocol handler
//	func RegisterTCPInput(registry *component.Registry) error {
//	    return registry.RegisterWithConfig(component.RegistrationConfig{
//	        Name:        "tcp",
//	        Factory:     CreateTCPInput,
//	        Schema:      tcpSchema,
//	        Type:        "input",
//	        Protocol:    "tcp",
//	        Domain:      "network",
//	        Description: "TCP socket input",
//	        Version:     "1.0.0",
//	    })
//	}
//
// # Extension Points
//
// SemStreams provides two extension mechanisms:
//
//  1. Protocol Layer Extension (network/data handling):
//     - Add new input types (TCP, MQTT, etc.)
//     - Add new output types (Kafka, databases, etc.)
//     - Add new processors (custom formats, transformations)
//
//  2. Domain-Specific Extensions (separate modules):
//     - Import semstreams packages
//     - Create domain-specific components (robotics, IoT, industrial)
//     - Register components in downstream module
//     - Build flows using framework + domain components
//
// See semstreams-robotics for MAVLink/ROS domain extension examples.
//
// # Design Principles
//
// Separation of Concerns:
//   - Protocol handling ≠ Protocol semantics
//   - Data transport ≠ Data interpretation
//   - Flow orchestration ≠ Domain logic
//
// Composition Over Configuration:
//   - Small, focused components
//   - Connect via NATS subjects
//   - Build complex behaviors from simple pieces
//
// Testability:
//   - Explicit dependencies (no globals)
//   - Isolated component testing
//   - Integration tests with testcontainers
//
// Performance:
//   - Zero-copy where possible
//   - Bounded buffers (backpressure)
//   - Efficient serialization
//
// # Binary
//
// Build and run SemStreams:
//
//	# Build framework binary (protocol + semantic layers)
//	task build
//
//	# Run with protocol-only config (no semantic components loaded)
//	./bin/semstreams --config configs/protocol-flow.json
//
//	# Run with semantic processing enabled
//	./bin/semstreams --config configs/semantic-flow.json
//
// The SemStreams binary uses componentregistry.Register() to register
// both protocol-level and semantic components. Protocol components work
// independently without semantic layer.
//
// # Version
//
// Current: v0.2.0-alpha (Two-layer framework)
//
// This version merges protocol and semantic capabilities into a unified
// framework with clean layer separation. Domain-specific code has been
// extracted to separate modules (semstreams-robotics, semstreams-iot).
package semstreams
