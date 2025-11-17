// Package websocket provides WebSocket input component for StreamKit federation.
//
// # Overview
//
// The WebSocket Input component enables StreamKit instances to receive data over
// WebSocket connections, completing the federation loop started by the WebSocket
// Output component. This unlocks edge-to-cloud, multi-region, and hierarchical
// processing topologies.
//
// # Key Features
//
//   - Dual-mode operation: Server (listen) or Client (connect)
//   - Bidirectional communication: Request/reply patterns
//   - Authentication: Bearer token or Basic auth
//   - Automatic reconnection: Exponential backoff for client mode
//   - Backpressure handling: Drop oldest/newest or block
//   - Prometheus metrics: Comprehensive observability
//
// # Modes of Operation
//
// Server Mode (Listen):
//
// Component acts as WebSocket server, accepting incoming connections from
// multiple remote StreamKit instances.
//
//	┌──────────────────┐          ┌──────────────────┐
//	│   Instance A     │          │   Instance B     │
//	│  (Edge Device)   │          │  (THIS COMPONENT)│
//	│                  │          │                  │
//	│  WS Output       ├─ ws:// ─►│  WS Input        │
//	│  (client)        │          │  (server)        │
//	│                  │          │  :8081/ingest    │
//	└──────────────────┘          └──────────────────┘
//
// Use case: Cloud hub receiving data from edge devices
//
// Client Mode (Connect):
//
// Component acts as WebSocket client, connecting to a remote WebSocket server.
//
//	┌──────────────────┐          ┌──────────────────┐
//	│   Instance B     │          │   Instance A     │
//	│  (Edge Device)   │          │  (Cloud Hub)     │
//	│  (THIS COMPONENT)│          │                  │
//	│  WS Input        ├─ ws:// ─►│  WS Output       │
//	│  (client)        │          │  (server)        │
//	│                  │          │  :8080/stream    │
//	└──────────────────┘          └──────────────────┘
//
// Use case: Edge device pulling data from cloud hub
//
// # Message Protocol
//
// All WebSocket messages use a JSON envelope to distinguish between data
// and control messages:
//
//	type MessageEnvelope struct {
//	    Type      string          // "data", "request", "reply", "ack", "nack", "slow"
//	    ID        string          // Unique message ID
//	    Timestamp int64           // Unix milliseconds
//	    Payload   json.RawMessage // Actual message content (optional for ack/nack)
//	}
//
// Data Message:
//
//	{
//	  "type": "data",
//	  "id": "data-001",
//	  "timestamp": 1704844800000,
//	  "payload": {"sensor_id": "temp-01", "value": 23.5}
//	}
//
// Request Message:
//
//	{
//	  "type": "request",
//	  "id": "req-001",
//	  "timestamp": 1704844800000,
//	  "payload": {
//	    "method": "backpressure",
//	    "params": {"rate_limit": 100, "unit": "msg/sec"}
//	  }
//	}
//
// Reply Message:
//
//	{
//	  "type": "reply",
//	  "id": "req-001",  // Matches request ID
//	  "timestamp": 1704844800050,
//	  "payload": {"status": "ok", "result": {...}}
//	}
//
// Ack Message (Reliable Delivery):
//
//	{
//	  "type": "ack",
//	  "id": "data-001",  // Matches data message ID
//	  "timestamp": 1704844800100
//	}
//
// Nack Message (Delivery Failed):
//
//	{
//	  "type": "nack",
//	  "id": "data-001",  // Matches data message ID
//	  "timestamp": 1704844800100,
//	  "payload": {"reason": "publish_failed", "error": "..."}
//	}
//
// Slow Message (Backpressure):
//
//	{
//	  "type": "slow",
//	  "id": "bp-001",
//	  "timestamp": 1704844800200,
//	  "payload": {"queue_depth": 850, "queue_capacity": 1000, "utilization": 0.85}
//	}
//
// # Configuration
//
// Server Mode Example:
//
//	{
//	  "type": "input",
//	  "name": "websocket_input",
//	  "config": {
//	    "mode": "server",
//	    "server": {
//	      "http_port": 8081,
//	      "path": "/ingest",
//	      "max_connections": 100,
//	      "enable_compression": true
//	    },
//	    "auth": {
//	      "type": "bearer",
//	      "bearer_token_env": "WS_INGEST_TOKEN"
//	    },
//	    "backpressure": {
//	      "enabled": true,
//	      "queue_size": 1000,
//	      "on_full": "drop_oldest"
//	    },
//	    "ports": {
//	      "outputs": [
//	        {
//	          "name": "ws_data",
//	          "subject": "federated.data",
//	          "type": "nats"
//	        }
//	      ]
//	    }
//	  }
//	}
//
// Client Mode Example:
//
//	{
//	  "type": "input",
//	  "name": "websocket_input",
//	  "config": {
//	    "mode": "client",
//	    "client": {
//	      "url": "ws://edge-instance:8080/stream",
//	      "reconnect": {
//	        "enabled": true,
//	        "max_retries": 10,
//	        "initial_interval": "1s",
//	        "max_interval": "60s",
//	        "multiplier": 2.0
//	      }
//	    },
//	    "ports": {
//	      "outputs": [
//	        {
//	          "name": "ws_data",
//	          "subject": "federated.data",
//	          "type": "nats"
//	        }
//	      ]
//	    }
//	  }
//	}
//
// # Bidirectional Communication
//
// When enabled, the component supports request/reply patterns over the
// WebSocket connection. This allows the receiving instance to send control
// messages back to the sender.
//
// Supported Request Methods:
//
//  1. Backpressure Control: Adjust upstream send rate
//  2. Selective Subscription: Filter data at source
//  3. Historical Query: Request replay of buffered messages
//  4. Status Query: Request observability metrics
//  5. Dynamic Routing: Announce capabilities for routing
//
// Example: Backpressure Request
//
//	// Instance B sends request to Instance A
//	request := MessageEnvelope{
//	    Type: "request",
//	    ID: "req-bp-001",
//	    Payload: json.RawMessage(`{
//	        "method": "backpressure",
//	        "params": {"rate_limit": 100, "unit": "msg/sec"}
//	    }`),
//	}
//
//	// Instance A replies
//	reply := MessageEnvelope{
//	    Type: "reply",
//	    ID: "req-bp-001",
//	    Payload: json.RawMessage(`{
//	        "status": "ok",
//	        "result": {"adjusted_to": 100}
//	    }`),
//	}
//
// # Backpressure Handling
//
// The component maintains an internal message queue to decouple WebSocket
// reception from NATS publishing. When the queue fills up, backpressure
// policies are applied:
//
// Drop Oldest (default):
//
//	Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
//	New:   msg6
//	Result: [msg2, msg3, msg4, msg5, msg6]
//	Lost:   msg1
//
// Drop Newest:
//
//	Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
//	New:   msg6
//	Result: [msg1, msg2, msg3, msg4, msg5]
//	Lost:   msg6
//
// Block:
//
//	Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
//	New:   msg6
//	Wait until queue has space...
//
// # Reconnection Logic (Client Mode)
//
// When operating in client mode, the component automatically reconnects
// if the connection is lost:
//
//	Attempt 1: Wait 1 second
//	Attempt 2: Wait 2 seconds (1 * 2.0)
//	Attempt 3: Wait 4 seconds (2 * 2.0)
//	Attempt 4: Wait 8 seconds (4 * 2.0)
//	...
//	Attempt N: Wait up to max_interval (60s)
//
// Reconnection stops when:
//   - max_retries reached (if configured)
//   - component stopped
//   - connection succeeds
//
// # Metrics
//
// Prometheus metrics exposed:
//
//	# Message throughput
//	websocket_input_messages_received_total{component,type} - Total received
//	websocket_input_messages_published_total{component,subject} - Total published
//	websocket_input_messages_dropped_total{component,reason} - Total dropped
//
//	# Connection state
//	websocket_input_connections_active{component} - Active connections
//	websocket_input_connections_total{component} - Total connections
//	websocket_input_reconnect_attempts_total{component} - Reconnection attempts
//
//	# Request/Reply (bidirectional mode)
//	websocket_input_requests_sent_total{component,method} - Requests sent
//	websocket_input_replies_received_total{component,status} - Replies received
//	websocket_input_request_timeouts_total{component} - Request timeouts
//	websocket_input_request_duration_seconds{component,method} - Round-trip time
//
//	# Queue state
//	websocket_input_queue_depth{component} - Current queue depth
//	websocket_input_queue_utilization{component} - Queue utilization (0.0-1.0)
//
//	# Errors
//	websocket_input_errors_total{component,type} - Errors by type
//
// # Health Checks
//
// Component health response:
//
//	{
//	  "healthy": true,
//	  "status": "connected",
//	  "details": {
//	    "mode": "server",
//	    "connections": {
//	      "active": 5,
//	      "total": 37
//	    },
//	    "queue": {
//	      "depth": 45,
//	      "utilization": 0.045
//	    },
//	    "throughput": {
//	      "messages_per_second": 250
//	    }
//	  }
//	}
//
// Unhealthy states:
//   - Server mode: No active connections for > 5 minutes
//   - Client mode: Not connected and max retries exceeded
//   - Queue full for > 30 seconds (backpressure issue)
//
// # Security
//
// Authentication:
//
// Bearer Token (recommended):
//
//	export WS_INGEST_TOKEN="sk-1234567890abcdef"
//
// Basic Auth (legacy):
//
//	export WS_USERNAME="semstreams"
//	export WS_PASSWORD="secret123"
//
// TLS/SSL:
//
// Recommendation: Use reverse proxy (nginx, Caddy) for TLS termination.
//
//	Client ───HTTPS───► Nginx ───HTTP───► StreamKit
//	        (TLS)               (localhost)
//
// # Error Handling
//
// Errors are classified using the StreamKit error framework:
//
//   - Fatal: Invalid mode, missing required config
//   - Transient: Connection errors, read errors, publish errors
//   - Invalid: Message parse errors, unknown message types
//
// Fatal errors prevent component startup. Transient errors trigger
// reconnection (client mode) or are logged (server mode). Invalid
// messages are dropped and counted in metrics.
//
// # Thread Safety
//
// All public methods are safe for concurrent use:
//
//   - Start(): Protected by lifecycleMu
//   - Stop(): Protected by lifecycleMu
//   - Internal state: Protected by appropriate mutexes
//
// Message processing is handled by dedicated goroutines:
//   - Server mode: One goroutine per client connection
//   - Client mode: One read goroutine + one reconnect goroutine
//   - Common: One message processor goroutine
//
// # Federation Use Cases
//
// Edge-to-Cloud:
//
//	Edge Instance: UDP → Processors → WebSocket Output
//	Cloud Instance: WebSocket Input → Storage/Analytics
//
// Multi-Region Replication:
//
//	Region A: WebSocket Output → Region B: WebSocket Input
//	Region B: WebSocket Output → Region A: WebSocket Input
//
// Hierarchical Processing:
//
//	Edge: Raw data → Pre-processing → WebSocket Output
//	Regional: WebSocket Input → Aggregation → WebSocket Output
//	Cloud: WebSocket Input → Analytics/Storage
//
// # Integration Example
//
//	import (
//	    "github.com/c360/semstreams/input/websocket_input"
//	    "github.com/c360/semstreams/component"
//	    "github.com/c360/semstreams/natsclient"
//	    "github.com/c360/semstreams/metric"
//	)
//
//	// Create NATS client
//	nats, _ := natsclient.NewClient("nats://localhost:4222")
//	nats.Connect(ctx)
//
//	// Create metrics registry
//	metrics := metric.NewMetricsRegistry()
//
//	// Create component
//	config := websocket_input.DefaultConfig()
//	config.Mode = websocket_input.ModeServer
//	config.ServerConfig.HTTPPort = 8081
//
//	input, _ := websocket_input.NewInput("ws_in", nats, config, metrics)
//
//	// Start component
//	input.Start(ctx)
//	defer input.Stop(ctx)
//
//	// Messages automatically published to NATS subject
//
// # See Also
//
//   - output/websocket: WebSocket Output component (sends data)
//   - component: Component lifecycle interfaces
//   - natsclient: NATS client wrapper
//   - metric: Metrics registry and Prometheus integration
package websocket
