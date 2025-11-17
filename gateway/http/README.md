# HTTP Gateway Component

The HTTP Gateway component enables external HTTP/REST clients to query SemStreams via bidirectional NATS request/reply patterns.

## Overview

**Type**: Gateway
**Protocol**: HTTP/REST
**Pattern**: Request/Reply (External ↔ NATS)
**Read-Only**: Yes (default)

The HTTP Gateway bridges external HTTP requests to internal NATS request/reply subjects, enabling REST API access to SemStreams components like GraphProcessor without requiring direct NATS connections.

## Component Type: Gateway

SemStreams has 5 component types:

| Type | Pattern | Example |
|------|---------|---------|
| **Input** | External → NATS | UDP, WebSocket ingestion |
| **Processor** | NATS → Transform → NATS | JSONFilter, GraphProcessor |
| **Output** | NATS → External | File, HTTP POST, WebSocket push |
| **Storage** | NATS → Persistent Store | ObjectStore (JetStream) |
| **Gateway** | External ↔ NATS | HTTP request/reply (this component) |

**Key Difference from Output:**
- Output: Unidirectional push (NATS → External)
- Gateway: Bidirectional request/reply (External ↔ NATS ↔ External)

## Architecture

```
┌──────────────────┐
│  HTTP Client     │  POST /api-gateway/search/semantic
└────────┬─────────┘
         ↓ HTTP Request
┌────────────────────────────────────────┐
│  ServiceManager (Port 8080)            │
│  /api-gateway/* → HTTPGateway handlers │
└────────┬───────────────────────────────┘
         ↓ NATS Request/Reply
┌────────────────────────────────────────┐
│  graph-processor Component             │
│  Subscribed to graph.query.semantic    │
└────────┬───────────────────────────────┘
         ↓ NATS Reply
┌────────────────────────────────────────┐
│  ServiceManager                        │
│  Translate reply → HTTP Response       │
└────────┬───────────────────────────────┘
         ↓ HTTP Response
┌──────────────────┐
│  HTTP Client     │  Receives SearchResults JSON
└──────────────────┘
```

## Configuration

### Basic Configuration

```json
{
  "components": {
    "api-gateway": {
      "type": "gateway",
      "name": "http",
      "enabled": true,
      "config": {
        "routes": [
          {
            "path": "/search/semantic",
            "method": "POST",
            "nats_subject": "graph.query.semantic",
            "timeout": "5s",
            "description": "Semantic similarity search"
          }
        ]
      }
    }
  }
}
```

### Full Configuration

```json
{
  "components": {
    "api-gateway": {
      "type": "gateway",
      "name": "http",
      "enabled": true,
      "config": {
        "enable_cors": true,
        "cors_origins": ["http://localhost:3000", "https://app.example.com"],
        "max_request_size": 1048576,
        "routes": [
          {
            "path": "/search/semantic",
            "method": "POST",
            "nats_subject": "graph.query.semantic",
            "timeout": "5s",
            "description": "Semantic similarity search across indexed entities"
          },
          {
            "path": "/entity/:id",
            "method": "GET",
            "nats_subject": "graph.query.entity",
            "timeout": "2s",
            "description": "Retrieve single entity by ID"
          }
        ]
      }
    }
  }
}
```

## Configuration Schema

### Gateway Config

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `routes` | RouteMapping[] | Yes | - | HTTP path to NATS subject mappings |
| `enable_cors` | bool | No | `false` | Enable CORS headers (requires explicit cors_origins) |
| `cors_origins` | string[] | No | `[]` | Allowed CORS origins |
| `max_request_size` | int | No | `1048576` | Max request body size (bytes) |

### Route Mapping

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `path` | string | Yes | - | HTTP route path (supports `:param`) |
| `method` | string | Yes | - | HTTP method (GET, POST, PUT, DELETE, PATCH) |
| `nats_subject` | string | Yes | - | NATS request/reply subject |
| `timeout` | duration | No | `5s` | Request timeout (100ms-30s) |
| `description` | string | No | - | Route description (for OpenAPI docs) |

## Route Registration

Routes are automatically registered at startup:

```
Component Instance Name: "api-gateway"
URL Prefix: "/api-gateway/"

Route: "/search/semantic"
Full Path: "/api-gateway/search/semantic"
```

ServiceManager discovers gateway components via interface:

```go
type Gateway interface {
    component.Discoverable
    RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
}
```

## Mutation Control

HTTP methods (GET, POST, PUT, DELETE) don't directly map to mutation semantics.
For example, POST is commonly used for complex queries like semantic search.

**Design Principle**: Mutation control should be enforced at the NATS subject/component
level, not at the HTTP gateway layer. The gateway is protocol translation only.

## CORS

CORS is disabled by default and requires explicit configuration for security:

### Allow All Origins (Development)

```json
{
  "enable_cors": true,
  "cors_origins": ["*"]
}
```

### Restrict Origins (Production)

```json
{
  "enable_cors": true,
  "cors_origins": [
    "https://app.example.com",
    "https://dashboard.example.com"
  ]
}
```

### CORS Headers

```
Access-Control-Allow-Origin: https://app.example.com
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Max-Age: 3600
```

## Error Handling

HTTP status codes are mapped from SemStreams error types:

| Error Type | HTTP Status | Example |
|------------|-------------|---------|
| Invalid | `400 Bad Request` | Malformed JSON, missing fields |
| Transient (timeout) | `504 Gateway Timeout` | NATS request timeout |
| Transient (other) | `503 Service Unavailable` | NATS connection down |
| Fatal | `500 Internal Server Error` | Unexpected errors |
| Not Found | `404 Not Found` | Entity doesn't exist |
| Unauthorized | `403 Forbidden` | Permission denied |

**Error Response Format:**

```json
{
  "error": "entity not found",
  "status": 404
}
```

## Request Size Limits

Prevent DoS attacks with request size limits:

```json
{
  "max_request_size": 1048576  // 1MB (default)
}
```

**Range**: 0 to 100MB (104857600 bytes)

Requests exceeding the limit are truncated to `max_request_size`.

## Timeouts

Per-route timeout configuration:

```json
{
  "routes": [
    {
      "path": "/search/semantic",
      "timeout": "5s"   // Quick queries
    },
    {
      "path": "/entity/:id/path",
      "timeout": "30s"  // Complex graph traversal
    }
  ]
}
```

**Range**: 100ms to 30s

If NATS doesn't reply within timeout, returns `504 Gateway Timeout`.

## Metrics

Prometheus metrics exported at `:9090/metrics`:

### Gateway Metrics

```
# Request totals
gateway_requests_total{component="api-gateway", route="/search/semantic"}

# Failed requests
gateway_requests_failed_total{component="api-gateway", route="/search/semantic"}

# Request latency
gateway_request_duration_seconds{component="api-gateway", route="/search/semantic"}
```

### Component Health

```
# Gateway health
component_healthy{component="api-gateway", type="gateway"}

# Error count
component_errors_total{component="api-gateway"}
```

## Usage Examples

### Semantic Search

```bash
curl -X POST http://localhost:8080/api-gateway/search/semantic \
  -H "Content-Type: application/json" \
  -d '{
    "query": "emergency alert system",
    "threshold": 0.3,
    "limit": 10
  }'
```

**Response:**

```json
{
  "data": {
    "query": "emergency alert system",
    "threshold": 0.3,
    "hits": [
      {
        "entity_id": "alert-001",
        "score": 0.87,
        "entity_type": "alert",
        "properties": {
          "title": "Emergency Alert Test",
          "content": "Testing the emergency alert system"
        }
      }
    ]
  }
}
```

### Entity Lookup

```bash
curl http://localhost:8080/api-gateway/entity/device-123
```

### TypeScript Client

```typescript
class SemStreamsClient {
  constructor(private baseURL = 'http://localhost:8080/api-gateway') {}

  async semanticSearch(query: string, limit = 10, threshold = 0.3) {
    const res = await fetch(`${this.baseURL}/search/semantic`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query, limit, threshold })
    });

    if (!res.ok) {
      const error = await res.json();
      throw new Error(error.error);
    }

    return await res.json();
  }
}

const client = new SemStreamsClient();
const results = await client.semanticSearch('drone battery');
```

## OpenAPI Integration

The gateway automatically contributes routes to OpenAPI specification:

**Access OpenAPI:**
- JSON Spec: `http://localhost:8080/openapi.json`
- Swagger UI: `http://localhost:8080/docs`

## Security Considerations

### Production Deployment

1. **TLS**: Deploy behind reverse proxy with TLS

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

2. **Authentication**: Add auth middleware in reverse proxy

```nginx
location /api-gateway/ {
    auth_request /auth;
    proxy_pass http://localhost:8080;
}
```

3. **Rate Limiting**: Prevent abuse

```nginx
limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

location /api-gateway/ {
    limit_req zone=api burst=20;
    proxy_pass http://localhost:8080;
}
```

4. **CORS**: Restrict origins

```json
{
  "cors_origins": ["https://app.example.com"]
}
```

## Troubleshooting

### "NATS connection not available" (503)

**Cause**: NATS server down or unreachable

**Fix:**

```bash
# Check NATS status
docker ps | grep nats

# Restart NATS
task integration:start
```

### "timeout" errors (504)

**Cause**: Query exceeds route timeout

**Fix:** Increase timeout:

```json
{
  "routes": [{
    "timeout": "30s"  // Increased from 5s
  }]
}
```

### CORS errors in browser

**Cause**: Origin not in `cors_origins` list

**Fix:**

```json
{
  "cors_origins": ["http://localhost:3000"]
}
```

### Method not allowed (405)

**Cause**: Wrong HTTP method for route

**Fix:** Use correct method from route config (GET vs POST)

## Related Documentation

- [Gateway Package](../README.md) - Gateway interface and types
- [EMBEDDING_ARCHITECTURE.md](../../docs/EMBEDDING_ARCHITECTURE.md) - Semantic search architecture
- [configs/HTTP_GATEWAY_USAGE.md](../../configs/HTTP_GATEWAY_USAGE.md) - Usage guide with curl examples
- [configs/http-gateway-semantic-search.json](../../configs/http-gateway-semantic-search.json) - Example configuration
