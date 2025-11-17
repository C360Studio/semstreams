# HTTP Gateway Usage Guide

This guide demonstrates how to use the HTTP Gateway component to access semantic search and graph queries via REST API.

## Configuration

The `http-gateway-semantic-search.json` configuration sets up:

1. **Graph Processor** - Indexes entities with semantic search (BM25), spatial, and temporal capabilities
2. **API Gateway** - HTTP gateway component exposing graph queries via REST endpoints

## Starting the Service

```bash
# From semstreams directory
./bin/streamkit -config configs/http-gateway-semantic-search.json
```

The HTTP Gateway will be available at `http://localhost:8080/api-gateway/`

## API Endpoints

All endpoints are prefixed with `/api-gateway/` (the component instance name).

### Semantic Search

Search for entities by semantic similarity to a query text.

```bash
curl -X POST http://localhost:8080/api-gateway/search/semantic \
  -H "Content-Type: application/json" \
  -d '{
    "query": "emergency alert",
    "threshold": 0.3,
    "limit": 10
  }'
```

**Request Fields:**
- `query` (string, required) - Search query text
- `threshold` (float, optional) - Minimum similarity score (0.0-1.0, default: 0.3)
- `limit` (int, optional) - Maximum results (default: 10)
- `types` ([]string, optional) - Filter by entity types

**Response:**
```json
{
  "data": {
    "query": "emergency alert",
    "threshold": 0.3,
    "hits": [
      {
        "entity_id": "abc123",
        "score": 0.85,
        "entity_type": "alert",
        "properties": {
          "title": "Emergency Alert System Test",
          "content": "This is a test of the emergency alert system"
        },
        "updated": "2025-11-16T10:30:00Z"
      }
    ]
  }
}
```

### Spatial Search

Search for entities within a geographic bounding box.

```bash
curl -X POST http://localhost:8080/api-gateway/search/spatial \
  -H "Content-Type: application/json" \
  -d '{
    "bounds": {
      "min_lat": 37.0,
      "max_lat": 38.0,
      "min_lon": -122.5,
      "max_lon": -121.5
    }
  }'
```

### Temporal Search

Query entities within a time range.

```bash
curl -X POST http://localhost:8080/api-gateway/search/temporal \
  -H "Content-Type: application/json" \
  -d '{
    "start": "2025-11-16T00:00:00Z",
    "end": "2025-11-16T23:59:59Z"
  }'
```

### Entity Lookup

Retrieve a single entity by ID.

```bash
curl http://localhost:8080/api-gateway/entity/abc123
```

**Response:**
```json
{
  "data": {
    "entity_id": "abc123",
    "entity_type": "alert",
    "properties": {
      "title": "System Alert",
      "severity": "high"
    },
    "updated_at": "2025-11-16T10:30:00Z",
    "created_at": "2025-11-16T10:00:00Z"
  }
}
```

### Entity Relationships

Get incoming and/or outgoing relationships for an entity.

```bash
# Get all relationships (incoming + outgoing)
curl -X GET "http://localhost:8080/api-gateway/entity/abc123/relationships?direction=both"

# Get only outgoing relationships
curl -X GET "http://localhost:8080/api-gateway/entity/abc123/relationships?direction=outgoing"

# Get only incoming relationships
curl -X GET "http://localhost:8080/api-gateway/entity/abc123/relationships?direction=incoming"
```

**Response:**
```json
{
  "data": {
    "entity_id": "abc123",
    "relationships": [
      {
        "subject_id": "abc123",
        "predicate": "triggered_by",
        "object_id": "xyz789",
        "properties": {}
      }
    ]
  }
}
```

### Graph Path Traversal (PathRAG)

Traverse graph paths from a starting entity with bounded exploration. PathRAG enables discovery of contextually relevant entities through relationship traversal, not just semantic similarity.

**Basic Path Query:**

```bash
curl -X POST http://localhost:8080/api-gateway/entity/drone-001/path \
  -H "Content-Type: application/json" \
  -d '{
    "max_depth": 3,
    "max_nodes": 100,
    "max_time": "500ms",
    "edge_filter": ["near", "communicates"],
    "decay_factor": 0.8,
    "max_paths": 10
  }'
```

**Request Fields:**
- `start_entity` (string) - Entity ID to start traversal from (optional if in URL path)
- `max_depth` (int, required) - Maximum hops from start (prevents infinite loops)
- `max_nodes` (int, required) - Maximum nodes to visit (bounds memory)
- `max_time` (duration, required) - Query timeout (e.g., "500ms", "2s")
- `edge_filter` ([]string, optional) - Only traverse these relationship types (nil = all)
- `decay_factor` (float, optional) - Relevance decay per hop (0.0-1.0, default: 0.8)
- `max_paths` (int, optional) - Limit paths tracked (default: 100)

**Response:**

```json
{
  "data": {
    "entities": [
      {
        "entity_id": "drone-001",
        "entity_type": "robotics.drone",
        "properties": {
          "name": "Alpha Drone",
          "status": "active"
        }
      },
      {
        "entity_id": "base-alpha",
        "entity_type": "robotics.base",
        "properties": {
          "name": "Base Station Alpha"
        }
      }
    ],
    "paths": [
      ["drone-001", "relay-003", "base-alpha"],
      ["drone-001", "drone-002", "base-alpha"]
    ],
    "scores": {
      "drone-001": 1.0,
      "relay-003": 0.8,
      "drone-002": 0.8,
      "base-alpha": 0.64
    },
    "truncated": false
  }
}
```

**Use Case Examples:**

**1. Dependency Chain Analysis** - Find all services affected by a configuration change:

```bash
curl -X POST http://localhost:8080/api-gateway/entity/config.db.credentials/path \
  -H "Content-Type: application/json" \
  -d '{
    "max_depth": 5,
    "max_nodes": 200,
    "max_time": "500ms",
    "edge_filter": ["depends_on", "calls"],
    "decay_factor": 0.9,
    "max_paths": 50
  }'
```

**2. Incident Impact Radius** - Trace cascading effects of a system failure:

```bash
curl -X POST http://localhost:8080/api-gateway/entity/alert.network.outage/path \
  -H "Content-Type: application/json" \
  -d '{
    "max_depth": 4,
    "max_nodes": 300,
    "max_time": "1s",
    "edge_filter": ["triggers", "affects", "depends_on"],
    "decay_factor": 0.85
  }'
```

**3. Time-Bounded Discovery** - Find as much as possible in limited time:

```bash
curl -X POST http://localhost:8080/api-gateway/entity/sensor.temp.critical/path \
  -H "Content-Type: application/json" \
  -d '{
    "max_depth": 10,
    "max_nodes": 10000,
    "max_time": "50ms",
    "decay_factor": 0.8,
    "max_paths": 50
  }'
```

**Resource Limits:**

PathRAG queries are protected by multiple bounded limits:

- **MaxDepth**: Prevents infinite loops in cyclic graphs (typical: 2-5 hops)
- **MaxNodes**: Bounds memory usage (typical: 50-500 nodes)
- **MaxTime**: Ensures predictable latency (typical: 50-500ms)
- **MaxPaths**: Prevents exponential path growth (typical: 10-100 paths)
- **DecayFactor**: Scores decay with distance (0.7 = aggressive, 0.9 = gentle)

If a query hits resource limits, the response will have `"truncated": true`. Even truncated results are useful for approximate context.

**See also:** [PathRAG Documentation](/docs/features/PATHRAG.md) for detailed guidance.

## Error Responses

All errors follow a consistent format:

```json
{
  "error": "entity not found",
  "status": 404
}
```

**Status Codes:**
- `200` - Success
- `400` - Bad Request (invalid JSON, missing required fields)
- `404` - Not Found (entity doesn't exist)
- `405` - Method Not Allowed (wrong HTTP method)
- `500` - Internal Server Error
- `503` - Service Unavailable (NATS connection issue)
- `504` - Gateway Timeout (query took too long)

## CORS

CORS is enabled by default with all origins allowed (`*`). To restrict origins:

```json
{
  "components": {
    "api-gateway": {
      "config": {
        "enable_cors": true,
        "cors_origins": ["https://example.com", "https://app.example.com"]
      }
    }
  }
}
```

## Mutation Control

**Design Principle**: HTTP methods (GET, POST, PUT, DELETE) don't directly map to mutation semantics. For example, POST is commonly used for complex queries like semantic search.

Mutation control should be enforced at the NATS subject/component level, not at the HTTP gateway layer. The gateway provides protocol translation only.

```json
// Gateway routes can use any HTTP method
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

## Request Size Limits

Requests are limited to 1MB by default:

```json
{
  "config": {
    "max_request_size": 1048576  // 1MB in bytes
  }
}
```

## Monitoring

The gateway exports Prometheus metrics at `http://localhost:9090/metrics`:

```
# Gateway-specific metrics
gateway_requests_total{component="api-gateway",route="/search/semantic"}
gateway_requests_failed_total{component="api-gateway",route="/search/semantic"}
gateway_request_duration_seconds{component="api-gateway",route="/search/semantic"}

# Graph processor metrics
indexengine_embeddings_active{component="graph-processor"}
indexengine_queries_total{component="graph-processor",query_type="semantic"}
```

## Health Check

Check service health:

```bash
curl http://localhost:8080/health
```

## OpenAPI Documentation

Interactive API documentation (Swagger UI) available at:

```
http://localhost:8080/docs
```

OpenAPI JSON specification:

```
http://localhost:8080/openapi.json
```

## Integration Example

JavaScript/TypeScript client:

```typescript
interface PathQuery {
  max_depth: number;
  max_nodes: number;
  max_time: string;
  edge_filter?: string[];
  decay_factor?: number;
  max_paths?: number;
}

interface PathResult {
  entities: Array<{
    entity_id: string;
    entity_type: string;
    properties: Record<string, any>;
  }>;
  paths: string[][];
  scores: Record<string, number>;
  truncated: boolean;
}

class SemStreamsClient {
  constructor(private baseURL: string = 'http://localhost:8080/api-gateway') {}

  async semanticSearch(query: string, limit = 10, threshold = 0.3) {
    const response = await fetch(`${this.baseURL}/search/semantic`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query, limit, threshold })
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error);
    }

    return await response.json();
  }

  async getEntity(entityId: string) {
    const response = await fetch(`${this.baseURL}/entity/${entityId}`);

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error);
    }

    return await response.json();
  }

  async pathQuery(startEntity: string, query: PathQuery): Promise<{ data: PathResult }> {
    const response = await fetch(`${this.baseURL}/entity/${startEntity}/path`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(query)
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error);
    }

    return await response.json();
  }
}

// Usage Examples

const client = new SemStreamsClient();

// Semantic search
const results = await client.semanticSearch('emergency alert', 10);
console.log(results.data.hits);

// Path traversal - find affected services
const pathResult = await client.pathQuery('config.db.credentials', {
  max_depth: 5,
  max_nodes: 200,
  max_time: '500ms',
  edge_filter: ['depends_on', 'calls'],
  decay_factor: 0.9,
  max_paths: 50
});

console.log('Affected services:', pathResult.data.entities.length);
console.log('Dependency paths:', pathResult.data.paths);

if (pathResult.data.truncated) {
  console.warn('Query hit resource limits - results incomplete');
}

// Hybrid approach: semantic + path expansion
const semanticHits = await client.semanticSearch('drone battery failure', 5);
if (semanticHits.data.hits.length > 0) {
  const topHit = semanticHits.data.hits[0];
  const relatedEntities = await client.pathQuery(topHit.entity_id, {
    max_depth: 3,
    max_nodes: 100,
    max_time: '300ms',
    edge_filter: ['related_to', 'triggered_by']
  });

  console.log('Semantically similar:', topHit.entity_id);
  console.log('Structurally connected:', relatedEntities.data.entities.map(e => e.entity_id));
}
```

## Security Considerations

1. **TLS**: Deploy behind reverse proxy (nginx, Caddy) with TLS for production
2. **Authentication**: Add auth middleware in reverse proxy layer
3. **Rate Limiting**: Implement rate limiting in reverse proxy
4. **CORS**: Restrict origins in production environments
5. **Mutation Control**: Enforce mutation permissions at NATS subject/component level

## Troubleshooting

### "NATS connection not available" (503)

**Cause**: NATS server not running or unreachable

**Solution:**
```bash
# Check NATS status
docker ps | grep nats

# Start NATS
task integration:start
```

### "timeout" errors (504)

**Cause**: Query taking longer than configured timeout

**Solution:** Increase route timeout:
```json
{
  "routes": [{
    "timeout": "30s"  // Increase from default 5s
  }]
}
```

### No search results

**Cause**: No entities indexed yet

**Solution:**
1. Ensure graph-processor is receiving entity events
2. Check embedding configuration is enabled
3. Verify retention window hasn't expired
