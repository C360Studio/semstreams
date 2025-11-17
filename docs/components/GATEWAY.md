# Gateway Components

Gateway components provide bidirectional protocol bridging between external clients and NATS-based SemStreams services.

## Overview

**Component Type**: Gateway (5th component type)
**Pattern**: External ↔ NATS Request/Reply ↔ External
**Purpose**: Enable external clients to query SemStreams via HTTP/WebSocket/gRPC without direct NATS connections

## Component Types Comparison

| Type | Pattern | Direction | Example Use Cases |
|------|---------|-----------|-------------------|
| **Input** | External → NATS | Unidirectional | UDP telemetry ingestion, WebSocket events |
| **Processor** | NATS → NATS | Internal | JSON filtering, entity graph indexing |
| **Output** | NATS → External | Unidirectional | File writing, webhook notifications |
| **Storage** | NATS → Store | Persistence | JetStream object storage |
| **Gateway** | External ↔ NATS | Bidirectional | REST API queries, GraphQL endpoints |

### Gateway vs Output

**Key Difference:**

- **Output**: Push-only (NATS → External)
  - Example: WebSocket broadcasts events to all connected clients
  - Pattern: Fire-and-forget, no reply expected
  - Configuration: `{type: "output", name: "websocket"}`

- **Gateway**: Request/Reply (External ↔ NATS ↔ External)
  - Example: HTTP client queries semantic search, receives results
  - Pattern: Synchronous request → NATS request/reply → HTTP response
  - Configuration: `{type: "gateway", name: "http"}`

## Gateway Implementations

### HTTP Gateway (gateway/http)

**Status**: ✅ Available
**Protocol**: HTTP/REST
**Use Case**: REST API access to graph queries, semantic search

**Example:**
```json
{
  "components": {
    "api-gateway": {
      "type": "gateway",
      "name": "http",
      "config": {
        "routes": [
          {
            "path": "/search/semantic",
            "method": "POST",
            "nats_subject": "graph.query.semantic"
          }
        ]
      }
    }
  }
}
```

**See**: [gateway/http/README.md](../../gateway/http/README.md)

### WebSocket Gateway (Future)

**Status**: 🔄 Planned
**Protocol**: WebSocket
**Use Case**: Bidirectional messaging, RPC-style queries

**Planned Configuration:**
```json
{
  "components": {
    "ws-gateway": {
      "type": "gateway",
      "name": "websocket",
      "config": {
        "port": 8081,
        "rpc_subjects": {
          "search": "graph.query.semantic",
          "entity": "graph.query.entity"
        }
      }
    }
  }
}
```

### gRPC Gateway (Future)

**Status**: 🔄 Planned
**Protocol**: gRPC
**Use Case**: High-performance RPC, microservice integration

## Architecture

### Handler Registration Flow

```
┌──────────────────────────────────────────┐
│  ServiceManager (Port 8080)              │
│                                          │
│  Start() → registerServiceHandlers()     │
│         → registerComponentHandlers()    │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│  Get ComponentManager service            │
│  componentManager.GetManagedComponents() │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│  For each component:                     │
│    if comp implements Gateway:           │
│      comp.RegisterHTTPHandlers(prefix)   │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│  HTTP Routes Registered:                 │
│  /api-gateway/search/semantic            │
│  /api-gateway/entity/:id                 │
│  /api-gateway/entity/:id/relationships   │
└──────────────────────────────────────────┘
```

### Request Flow

```
HTTP Client
    ↓ POST /api-gateway/search/semantic
ServiceManager HTTP Server (port 8080)
    ↓ Route to gateway component
HTTPGateway Component
    ↓ Translate HTTP → NATS Request
NATS (subject: graph.query.semantic)
    ↓ Request/Reply
GraphProcessor Component
    ↓ Execute semantic search
IndexManager
    ↓ Return SearchResults
NATS Reply
    ↓ Translate NATS → HTTP
HTTPGateway Component
    ↓ JSON Response
HTTP Client
```

## Gateway Interface

Gateways implement the `gateway.Gateway` interface:

```go
// Package gateway
type Gateway interface {
    component.Discoverable

    RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
}
```

**Methods:**
- `RegisterHTTPHandlers`: Called by ServiceManager to register routes
- All `Discoverable` methods: `Meta()`, `Health()`, `DataFlow()`, etc.

## Configuration Schema

### Common Gateway Config

```go
type Config struct {
    Routes         []RouteMapping `json:"routes"`           // Required
    EnableCORS     bool           `json:"enable_cors"`      // Default: false
    CORSOrigins    []string       `json:"cors_origins"`     // Default: []
    MaxRequestSize int64          `json:"max_request_size"` // Default: 1MB
}
```

### Route Mapping

```go
type RouteMapping struct {
    Path        string        `json:"path"`         // "/search/semantic"
    Method      string        `json:"method"`       // "GET", "POST", etc.
    NATSSubject string        `json:"nats_subject"` // "graph.query.semantic"
    Timeout     time.Duration `json:"timeout"`      // Default: 5s
    Description string        `json:"description"`  // For OpenAPI docs
}
```

## Mutation Control

**Design Principle**: HTTP methods (GET, POST, PUT, DELETE) don't directly map to mutation semantics. For example, POST is commonly used for complex queries like semantic search.

Mutation control should be enforced at the NATS subject/component level, not at the HTTP gateway layer. The gateway provides protocol translation only.

## Best Practices

### URL Prefix = Component Instance Name

Gateway routes are prefixed with component instance name:

```json
{
  "components": {
    "api-gateway": {  // Instance name
      "type": "gateway",
      "name": "http",
      "config": {
        "routes": [{"path": "/search"}]
      }
    }
  }
}
```

**Result**: `http://localhost:8080/api-gateway/search`

### Semantic Naming

Use descriptive instance names:

```json
{
  "semantic-api": {    // ✅ Clear purpose
    "type": "gateway"
  },
  "admin-gateway": {   // ✅ Clear purpose
    "type": "gateway"
  },
  "gw1": {             // ❌ Unclear
    "type": "gateway"
  }
}
```

### Mutation Control at NATS Level

Gateway routes can use any HTTP method. Mutation control should be enforced
at the NATS subject/component level:

```json
// ✅ GOOD: Gateway provides protocol translation only
{
  "routes": [
    {"method": "GET",  "path": "/entity/:id", "nats_subject": "graph.query.entity"},
    {"method": "POST", "path": "/search/semantic", "nats_subject": "graph.query.semantic"},
    {"method": "POST", "path": "/admin/restart", "nats_subject": "admin.control.restart"}
  ]
}

// The receiving component enforces mutation permissions via NATS subject ACLs
// or internal authorization checks, not the HTTP gateway.
```

### Timeout Configuration

Match timeout to operation complexity:

```json
{
  "routes": [
    {
      "path": "/entity/:id",
      "timeout": "2s"   // Simple lookup
    },
    {
      "path": "/search/semantic",
      "timeout": "5s"   // Search query
    },
    {
      "path": "/entity/:id/path",
      "timeout": "30s"  // Complex graph traversal
    }
  ]
}
```

## Security

### Production Checklist

- [ ] Deploy behind TLS-terminating reverse proxy (nginx, Caddy)
- [ ] Add authentication middleware in reverse proxy
- [ ] Implement rate limiting per client/IP
- [ ] Restrict CORS origins (no `*` in production)
- [ ] Enforce mutation control at NATS subject/component level
- [ ] Monitor gateway metrics for abuse
- [ ] Set appropriate request size limits

### Example: Caddy Reverse Proxy

```caddyfile
api.example.com {
    # TLS automatically via ACME
    tls your-email@example.com

    # Rate limiting
    rate_limit {
        zone api 10r/s
    }

    # Authentication
    basicauth /api-gateway/* {
        user $2a$14$hashed_password
    }

    # Proxy to SemStreams
    reverse_proxy localhost:8080
}
```

## Monitoring

### Gateway Metrics

```prometheus
# Request totals by route
gateway_requests_total{component="api-gateway", route="/search/semantic"}

# Failed requests
gateway_requests_failed_total{component="api-gateway", route="/search/semantic"}

# Request latency distribution
gateway_request_duration_seconds{component="api-gateway", route="/search/semantic"}

# Active gateway components
component_healthy{component="api-gateway", type="gateway"}
```

### Health Checks

```bash
# Component health
curl http://localhost:8080/components/health

# Gateway-specific health
curl http://localhost:8080/components/status/api-gateway
```

## Use Cases

### Semantic Search API

Expose graph semantic search to web/mobile clients:

```json
{
  "semantic-api": {
    "type": "gateway",
    "name": "http",
    "config": {
      "routes": [
        {
          "path": "/search/semantic",
          "nats_subject": "graph.query.semantic"
        },
        {
          "path": "/search/spatial",
          "nats_subject": "graph.query.spatial"
        }
      ]
    }
  }
}
```

### Admin Control Plane

Separate gateway for admin operations:

```json
{
  "admin-api": {
    "type": "gateway",
    "name": "http",
    "config": {
      "routes": [
        {
          "path": "/admin/shutdown",
          "method": "POST",
          "nats_subject": "admin.control.shutdown"
        },
        {
          "path": "/admin/config/reload",
          "method": "POST",
          "nats_subject": "admin.config.reload"
        }
      ]
    }
  }
}
```

### GraphQL Gateway (Future)

```json
{
  "graphql-api": {
    "type": "gateway",
    "name": "graphql",
    "config": {
      "schema_file": "./schema.graphql",
      "resolver_subjects": {
        "Query.entity": "graph.query.entity",
        "Query.search": "graph.query.semantic"
      }
    }
  }
}
```

## Related Documentation

- [HTTP Gateway README](../../gateway/http/README.md) - HTTP gateway implementation
- [Gateway Package](../../gateway/doc.go) - Gateway package documentation
- [HTTP Gateway Usage Guide](../../configs/HTTP_GATEWAY_USAGE.md) - Usage examples
- [Component Architecture](../ARCHITECTURE.md) - Overall component system
- [API Integration](../OPENAPI_INTEGRATION.md) - OpenAPI spec generation
