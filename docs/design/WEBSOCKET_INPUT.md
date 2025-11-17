# WebSocket Input Component Design

**Status**: Draft
**Author**: StreamKit Team
**Date**: 2025-01-09
**Version**: 1.0

## Overview

The WebSocket Input component enables StreamKit instances to receive data over WebSocket connections, completing the federation loop started by the WebSocket Output component. This unlocks edge-to-cloud, multi-region, and hierarchical processing topologies.

**Key Insight**: WebSockets are **bidirectional**, enabling request/reply patterns that go beyond simple data streaming. This transforms federation from passive forwarding into an active distributed control plane.

## Motivation

### Current State

- ✅ WebSocket Output: Broadcasts data to connected clients
- ❌ WebSocket Input: **Missing** - cannot receive federated data
- ❌ Bidirectional control: No request/reply mechanism

### Gap

StreamKit instances cannot communicate with each other over network boundaries. This limits deployment to single-instance architectures.

### Federation Use Cases

1. **Edge-to-Cloud**: IoT devices send processed data to central hub
2. **Multi-Region**: Cross-datacenter data replication
3. **Hierarchical Processing**: Pre-process at edge, aggregate at cloud
4. **Failover**: Route data to backup instance on primary failure
5. **Load Distribution**: Distribute processing across multiple instances

## Design Goals

### Primary Goals

1. **Enable Federation**: StreamKit → StreamKit communication over WS
2. **Bidirectional Control**: Support request/reply patterns
3. **Backward Compatible**: Work with existing WebSocket Output
4. **Protocol Agnostic**: Handle any data format (JSON, binary, etc.)
5. **Observable**: Expose metrics for monitoring

### Non-Goals

1. ❌ Replace NATS: Not a general-purpose message broker
2. ❌ WebSocket Server Framework: Not a generic WS framework
3. ❌ Authentication Service: Relies on existing auth mechanisms

## Architecture

### Modes of Operation

The WebSocket Input component supports **two modes**:

#### Mode 1: Server Mode (Listen)

Component acts as WebSocket server, accepting incoming connections.

```
┌──────────────────┐          ┌──────────────────┐
│   Instance A     │          │   Instance B     │
│                  │          │  (THIS COMPONENT)│
│  WS Output       ├─ ws:// ─►│  WS Input        │
│  (client)        │          │  (server)        │
│                  │          │  :8081/ingest    │
└──────────────────┘          └──────────────────┘
```

**Use Case**: Cloud hub receiving data from edge devices

#### Mode 2: Client Mode (Connect)

Component acts as WebSocket client, connecting to remote server.

```
┌──────────────────┐          ┌──────────────────┐
│   Instance B     │          │   Instance A     │
│  (THIS COMPONENT)│          │                  │
│  WS Input        ├─ ws:// ─►│  WS Output       │
│  (client)        │          │  (server)        │
│                  │          │  :8080/stream    │
└──────────────────┘          └──────────────────┘
```

**Use Case**: Edge device pulling data from cloud hub

### Configuration Schema

```json
{
  "type": "input",
  "name": "websocket_input",
  "enabled": true,
  "config": {
    "mode": "server",
    "server": {
      "http_port": 8081,
      "path": "/ingest",
      "max_connections": 100,
      "read_buffer_size": 4096,
      "write_buffer_size": 4096,
      "enable_compression": true
    },
    "client": {
      "url": "ws://instance-a:8080/stream",
      "reconnect": {
        "enabled": true,
        "max_retries": 10,
        "initial_interval": "1s",
        "max_interval": "60s",
        "multiplier": 2.0
      }
    },
    "auth": {
      "type": "bearer",
      "bearer_token_env": "WS_INGEST_TOKEN",
      "basic_auth": {
        "username_env": "WS_USERNAME",
        "password_env": "WS_PASSWORD"
      }
    },
    "bidirectional": {
      "enabled": true,
      "request_timeout": "5s",
      "max_concurrent_requests": 10
    },
    "backpressure": {
      "enabled": true,
      "queue_size": 1000,
      "on_full": "drop_oldest"
    },
    "ports": {
      "inputs": [],
      "outputs": [
        {
          "name": "ws_data",
          "subject": "federated.data",
          "type": "nats",
          "description": "Data received via WebSocket"
        },
        {
          "name": "ws_control",
          "subject": "federated.control",
          "type": "nats",
          "description": "Control messages (requests/replies)"
        }
      ]
    }
  }
}
```

### Field Descriptions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | `"server"` or `"client"` |
| `server.http_port` | int | If server mode | Port to listen on |
| `server.path` | string | If server mode | WebSocket endpoint path (default: `/`) |
| `server.max_connections` | int | No | Max concurrent connections (default: 100) |
| `client.url` | string | If client mode | WebSocket URL to connect to |
| `client.reconnect.enabled` | bool | No | Enable auto-reconnect (default: true) |
| `auth.type` | string | No | `"none"`, `"bearer"`, `"basic"` (default: `"none"`) |
| `bidirectional.enabled` | bool | No | Enable request/reply (default: true) |
| `backpressure.queue_size` | int | No | Internal queue size (default: 1000) |
| `backpressure.on_full` | string | No | `"drop_oldest"`, `"drop_newest"`, `"block"` |

## Bidirectional Communication Protocol

### Message Envelope Format

All WebSocket messages use a JSON envelope to distinguish between data and control messages:

```json
{
  "type": "data" | "request" | "reply",
  "id": "msg-uuid-1234",
  "timestamp": 1704844800000,
  "payload": { ... }
}
```

#### Message Types

**1. Data Message** (Instance A → Instance B)

```json
{
  "type": "data",
  "id": "data-001",
  "timestamp": 1704844800000,
  "payload": {
    "sensor_id": "temp-01",
    "value": 23.5,
    "unit": "celsius"
  }
}
```

Published to: `federated.data` (NATS)

**2. Request Message** (Instance B → Instance A)
```json
{
  "type": "request",
  "id": "req-001",
  "timestamp": 1704844800000,
  "payload": {
    "method": "backpressure",
    "params": {
      "rate_limit": 100,
      "unit": "msg/sec"
    }
  }
}
```

Published to: `federated.control.request` (NATS)

**3. Reply Message** (Instance A → Instance B)
```json
{
  "type": "reply",
  "id": "req-001",
  "timestamp": 1704844800050,
  "payload": {
    "status": "ok",
    "result": {
      "current_rate": 250,
      "adjusted_to": 100
    }
  }
}
```

Published to: `federated.control.reply` (NATS)

### Supported Request Methods

#### 1. Backpressure Control
**Request:**
```json
{
  "method": "backpressure",
  "params": {
    "rate_limit": 100,
    "unit": "msg/sec"
  }
}
```

**Reply:**
```json
{
  "status": "ok",
  "result": {
    "current_rate": 250,
    "adjusted_to": 100
  }
}
```

**Behavior**: Output component adjusts send rate to respect subscriber capacity.

#### 2. Selective Subscription (Filter Upstream)
**Request:**
```json
{
  "method": "subscribe",
  "params": {
    "filter": {
      "path": "$.severity",
      "operator": ">=",
      "value": "warning"
    }
  }
}
```

**Reply:**
```json
{
  "status": "ok",
  "result": {
    "subscription_id": "sub-123",
    "filter_applied": true
  }
}
```

**Behavior**: Output component applies filter before sending data.

#### 3. Historical Query (Replay Buffer)
**Request:**
```json
{
  "method": "replay",
  "params": {
    "count": 100,
    "start_time": 1704844700000
  }
}
```

**Reply:**
```json
{
  "status": "ok",
  "result": {
    "messages_available": 100,
    "replay_started": true
  }
}
```

**Behavior**: Output component replays buffered messages.

#### 4. Status Query (Observability)
**Request:**
```json
{
  "method": "status",
  "params": {}
}
```

**Reply:**
```json
{
  "status": "ok",
  "result": {
    "throughput": {
      "current": 250,
      "average_1m": 230,
      "unit": "msg/sec"
    },
    "queue_depth": 45,
    "uptime_seconds": 3600
  }
}
```

**Behavior**: Output component returns current metrics.

#### 5. Dynamic Routing (Capability Announcement)
**Request:**
```json
{
  "method": "announce",
  "params": {
    "capabilities": {
      "region": "us-west",
      "message_types": ["sensor", "telemetry"],
      "max_rate": 1000
    }
  }
}
```

**Reply:**
```json
{
  "status": "ok",
  "result": {
    "registered": true,
    "routing_updated": true
  }
}
```

**Behavior**: Output component routes messages based on announced capabilities.

### Request/Reply Correlation

**How it works:**
1. Client generates unique `id` for request
2. Server echoes same `id` in reply
3. Client matches reply to pending request using `id`
4. Timeout if no reply within `request_timeout` (default: 5s)

**NATS Integration:**
```go
// Input component receives request from WS
request := parseWebSocketMessage(wsMsg)

// Publish request to NATS (for local processing or forwarding)
nats.Publish("federated.control.request", request)

// Wait for reply on reply subject
reply := nats.RequestWithContext(ctx, "ws.request.backpressure", request, 5*time.Second)

// Send reply back over WebSocket
sendWebSocketMessage(ws, reply)
```

## State Management

### Server Mode State Machine

```
┌──────────┐
│  INIT    │
└────┬─────┘
     │ Start()
     ↓
┌──────────┐
│ LISTENING│◄─┐
└────┬─────┘  │
     │        │ client connects
     ↓        │
┌──────────┐  │
│CONNECTED │──┘
│(N clients)
└────┬─────┘
     │ Stop()
     ↓
┌──────────┐
│ STOPPED  │
└──────────┘
```

### Client Mode State Machine

```
┌──────────┐
│  INIT    │
└────┬─────┘
     │ Start()
     ↓
┌──────────┐
│CONNECTING│
└────┬─────┘
     │ success
     ↓
┌──────────┐
│CONNECTED │
└────┬─────┘
     │ disconnect
     ↓
┌──────────┐
│RECONNECTING│─┐
└────┬─────┘   │ backoff
     │          │ retry
     │◄─────────┘
     │ max retries
     ↓
┌──────────┐
│  FAILED  │
└──────────┘
```

## Error Handling

### Server Mode Errors

| Error | Classification | Handling |
|-------|----------------|----------|
| Port already in use | Fatal | Fail to start, log error |
| Invalid path | Fatal | Fail to start, log error |
| Client connection error | Transient | Log warning, continue serving |
| Message parse error | Invalid | Drop message, increment metric |
| NATS publish error | Transient | Retry with backoff |

### Client Mode Errors

| Error | Classification | Handling |
|-------|----------------|----------|
| Invalid URL | Fatal | Fail to start |
| Connection refused | Transient | Retry with exponential backoff |
| Authentication failed | Fatal | Fail after max retries |
| Connection dropped | Transient | Reconnect automatically |
| Message parse error | Invalid | Drop message, increment metric |

### Backpressure Handling

When internal queue is full:

**Option 1: Drop Oldest** (default)
```
Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
New:   msg6
Result: [msg2, msg3, msg4, msg5, msg6]
Lost:   msg1
```

**Option 2: Drop Newest**
```
Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
New:   msg6
Result: [msg1, msg2, msg3, msg4, msg5]
Lost:   msg6
```

**Option 3: Block** (not recommended for real-time)
```
Queue: [msg1, msg2, msg3, msg4, msg5]  ← FULL
New:   msg6
Wait until queue has space...
```

## Observability

### Metrics

**Prometheus metrics:**
```
# Message throughput
websocket_input_messages_received_total{component="websocket_input"} 12450
websocket_input_messages_published_total{component="websocket_input"} 12448
websocket_input_messages_dropped_total{component="websocket_input",reason="queue_full"} 2

# Connection state (server mode)
websocket_input_connections_active{component="websocket_input"} 5
websocket_input_connections_total{component="websocket_input"} 37

# Connection state (client mode)
websocket_input_connection_state{component="websocket_input",state="connected"} 1
websocket_input_reconnect_attempts_total{component="websocket_input"} 3

# Request/Reply
websocket_input_requests_sent_total{component="websocket_input",method="backpressure"} 15
websocket_input_replies_received_total{component="websocket_input",status="ok"} 14
websocket_input_request_timeouts_total{component="websocket_input"} 1
websocket_input_request_duration_seconds{method="status",quantile="0.5"} 0.023
websocket_input_request_duration_seconds{method="status",quantile="0.99"} 0.145

# Queue depth
websocket_input_queue_depth{component="websocket_input"} 45
websocket_input_queue_utilization{component="websocket_input"} 0.045

# Errors
websocket_input_errors_total{component="websocket_input",type="parse_error"} 3
websocket_input_errors_total{component="websocket_input",type="publish_error"} 0
```

### Health Checks

**Component health response:**
```json
{
  "healthy": true,
  "status": "connected",
  "details": {
    "mode": "server",
    "connections": {
      "active": 5,
      "total": 37
    },
    "queue": {
      "depth": 45,
      "utilization": 0.045
    },
    "throughput": {
      "messages_per_second": 250,
      "average_1m": 230
    }
  }
}
```

**Unhealthy states:**
- Server mode: No active connections for > 5 minutes
- Client mode: Not connected and max retries exceeded
- Queue full for > 30 seconds (backpressure issue)

## Security

### Authentication

**Bearer Token** (recommended for service-to-service):
```
Authorization: Bearer <token>
```

Token read from environment variable:
```bash
export WS_INGEST_TOKEN="sk-1234567890abcdef"
```

**Basic Auth** (legacy support):
```
Authorization: Basic <base64(username:password)>
```

Credentials from environment:
```bash
export WS_USERNAME="streamkit"
export WS_PASSWORD="secret123"
```

### TLS/SSL

**Recommendation**: Use reverse proxy (nginx, Caddy) for TLS termination:
```
Client ───HTTPS───► Nginx ───HTTP───► StreamKit
        (TLS)               (localhost)
```

**Future**: Native TLS support in component config.

### Authorization

Component does NOT implement authorization (which clients can connect, what they can request).
Recommendation: Use API gateway or service mesh for authz.

## Implementation Notes

### Dependencies

```go
import (
    "github.com/gorilla/websocket"  // WebSocket implementation
    "github.com/c360/streamkit/component"
    "github.com/c360/streamkit/natsclient"
    "github.com/c360/streamkit/metric"
)
```

### Component Interface

```go
type WebSocketInputComponent struct {
    component.BaseComponent

    mode            Mode // server or client
    config          *Config
    natsClient      *natsclient.Client
    metricsRegistry *metric.Registry

    // Server mode
    httpServer *http.Server
    upgrader   *websocket.Upgrader
    clients    map[string]*websocket.Conn
    clientsMu  sync.RWMutex

    // Client mode
    wsClient *websocket.Conn
    clientMu sync.Mutex

    // Common
    messageQueue chan []byte
    requestMap   map[string]chan []byte // request ID → reply channel
    requestMu    sync.RWMutex

    // Lifecycle
    started atomic.Bool
    wg      sync.WaitGroup
}

func (c *WebSocketInputComponent) Start(ctx context.Context) error {
    if c.mode == ModeServer {
        return c.startServer(ctx)
    }
    return c.startClient(ctx)
}

func (c *WebSocketInputComponent) Stop(ctx context.Context) error {
    // Graceful shutdown: close connections, drain queue
}

func (c *WebSocketInputComponent) Process(data any) error {
    // Not used - input component doesn't process, it receives
    return nil
}
```

### Message Flow (Server Mode)

```
WebSocket Connection
        │
        ↓
┌────────────────┐
│ ReadMessages() │ goroutine per client
└───────┬────────┘
        │ parse envelope
        ↓
   ┌─────────┐
   │ Data?   │───Yes──► messageQueue ──► NATS.Publish("federated.data")
   └────┬────┘
        │ No
        ↓
   ┌─────────┐
   │Request? │───Yes──► requestQueue ──► Handle request ──► Send reply
   └────┬────┘
        │ No
        ↓
   ┌─────────┐
   │ Reply?  │───Yes──► requestMap[id] ──► Unblock waiting goroutine
   └─────────┘
```

## Open Questions

1. **Buffer Management**: Should output component maintain a replay buffer? How large?
2. **Multiplexing**: Support multiple logical streams over one WS connection?
3. **Compression**: Per-message compression or connection-level?
4. **Binary Protocol**: Support msgpack/protobuf in addition to JSON?
5. **Authentication Refresh**: Token rotation for long-lived connections?

## Alternative Designs Considered

### Alternative 1: Separate Control Channel
Create two components: `websocket_input_data` and `websocket_input_control`.

**Rejected**: Adds complexity, breaks single connection abstraction.

### Alternative 2: NATS Over WebSocket
Expose NATS protocol directly over WebSocket.

**Rejected**: Requires NATS client in browser/edge, overkill for simple federation.

### Alternative 3: gRPC Streaming
Use gRPC bidirectional streams instead of WebSocket.

**Rejected**: gRPC is heavier, WebSocket is web-native and simpler for browser clients.

## Future Enhancements

1. **Client Pool Mode**: Connect to multiple servers, load balance
2. **Message Batching**: Bundle multiple messages per WS frame
3. **Compression**: Per-connection compression negotiation
4. **Binary Formats**: Support msgpack, protobuf, CBOR
5. **TLS Native**: Built-in TLS without reverse proxy
6. **Stream Multiplexing**: Multiple logical streams per connection
7. **Heartbeat/Ping**: Configurable keepalive mechanism

## References

- [RFC 6455: The WebSocket Protocol](https://tools.ietf.org/html/rfc6455)
- [NATS Request/Reply Pattern](https://docs.nats.io/nats-concepts/core-nats/reqreply)
- [Gorilla WebSocket Documentation](https://pkg.go.dev/github.com/gorilla/websocket)
- StreamKit Architecture: `streamkit/doc.go`
- WebSocket Output Component: `streamkit/output/websocket/`

## Appendix: Federation Scenario Example

### Configuration: Two-Instance Setup

**Instance A (Edge) - `edge-config.json`:**
```json
{
  "components": {
    "udp": {
      "type": "input",
      "name": "udp",
      "config": {
        "port": 14550,
        "ports": {
          "outputs": [{"name": "out", "subject": "raw.data"}]
        }
      }
    },
    "filter": {
      "type": "processor",
      "name": "json_filter",
      "config": {
        "criteria": {"path": "$.value", "operator": ">", "value": 50},
        "ports": {
          "inputs": [{"name": "in", "subject": "raw.data"}],
          "outputs": [{"name": "out", "subject": "filtered.data"}]
        }
      }
    },
    "ws_out": {
      "type": "output",
      "name": "websocket",
      "config": {
        "http_port": 8080,
        "path": "/federation",
        "ports": {
          "inputs": [{"name": "in", "subject": "filtered.data"}]
        }
      }
    }
  }
}
```

**Instance B (Cloud) - `cloud-config.json`:**
```json
{
  "components": {
    "ws_in": {
      "type": "input",
      "name": "websocket_input",
      "config": {
        "mode": "client",
        "client": {
          "url": "ws://edge-instance:8080/federation"
        },
        "ports": {
          "outputs": [{"name": "out", "subject": "federated.data"}]
        }
      }
    },
    "storage": {
      "type": "storage",
      "name": "objectstore",
      "config": {
        "ports": {
          "inputs": [{"name": "in", "subject": "federated.data"}]
        }
      }
    }
  }
}
```

### Data Flow

```
Edge Instance A:
  UDP :14550 → raw.data → JSONFilter → filtered.data → WebSocket Out :8080

Network:
  ws://edge-instance:8080/federation

Cloud Instance B:
  WebSocket In (client) → federated.data → ObjectStore
```

### Bidirectional Control Example

Cloud instance requests backpressure:
```javascript
// Instance B → Instance A
{
  "type": "request",
  "id": "req-bp-001",
  "payload": {
    "method": "backpressure",
    "params": {"rate_limit": 100, "unit": "msg/sec"}
  }
}

// Instance A → Instance B
{
  "type": "reply",
  "id": "req-bp-001",
  "payload": {
    "status": "ok",
    "result": {"adjusted_to": 100}
  }
}
```

---

**End of Design Document**
