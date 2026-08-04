# graph-gateway

HTTP gateway for a bounded GraphQL-shaped facade over graph operations. The
required gateway remains provisional until GS-12 makes it conformant GraphQL.
The registered `/mcp` route is a placeholder, not an MCP graph contract.

## Overview

The `/graphql` surface is a provisional, query-only facade. Its handler classifies
and routes supported root operations to NATS query/index subjects. It exposes no
GraphQL mutations and does not read graph KV through a `QueryManager`. Separate
inference-review HTTP commands currently mutate `ANOMALY_INDEX` directly; that is
GS-10 ownership debt, not part of the GraphQL contract.

## Architecture

```
                                   ┌─────────────────────┐
HTTP /graphql ──────────────────►  │                     │
                                   │   graph-gateway     │ ──► NATS query/index subjects
HTTP /mcp (placeholder) ────────►  │                     │
                                   │   (query-only)      │
HTTP / (playground) ────────────►  │                     │
                                   └─────────────────────┘
```

## Features

- **GraphQL-shaped facade**: hand-written routing for a bounded operation set
- **Reserved MCP placeholder**: returns a stub response; no protocol or graph tools
- **GraphQL Playground**: Interactive development IDE
- **Classified query routing**: forwards supported operations to NATS responders
- **Query-only contract**: introspection reports `mutationType: null`

## Configuration

```json
{
  "name": "graph-gateway",
  "type": "gateway",
  "config": {
    "graphql_path": "/graphql",
    "mcp_path": "/mcp",
    "bind_address": "localhost:8080",
    "enable_playground": true
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `graphql_path` | string | `/graphql` | GraphQL endpoint path |
| `mcp_path` | string | `/mcp` | Reserved placeholder path; not an MCP contract |
| `bind_address` | string | `localhost:8080` | HTTP server bind address |
| `enable_playground` | bool | `false` | Enable GraphQL playground UI |

## HTTP Endpoints

### GraphQL (`/graphql`)

Hand-written GraphQL-shaped, query-only endpoint.
It is not a general parser/schema executor and does not project response fields
from a selection set.

The operation inventory is code-defined and can drift: compare
`buildIntrospectionSchema` with `mapGraphQLQueryToNATSSubject` and its handler
tests before relying on an operation. Current examples include `entity`,
`entityByAlias`, `entitiesByPrefix`, `relationships`, and `pathSearch`.
Introspection reports `mutationType: null`; the facade exposes no mutation API.

### Reserved placeholder (`/mcp`)

The current handler returns only a stub JSON message. It implements no MCP
handshake, tool discovery, graph tool, or audit contract. Do not configure or
advertise it as agent graph access. GS-12 must remove the placeholder surface or
replace it with a separately specified implementation before the foundation tag.

### Playground (`/`)

When enabled, serves an interactive request UI for:
- Query composition and testing
- Advertised-operation exploration; introspection may drift from handlers
- Response visualization

## Prefix Scoping Best Practices

`entitiesByPrefix(prefix: String!, limit: Int)` returns up to `limit` entities whose IDs start with the given prefix. There is **no cursor or offset** — if matches exceed the limit, the excess is silently dropped. Design queries around this constraint.

### Avoid empty-prefix queries in production UIs

```graphql
# Don't do this — enumerates every entity in the graph, then truncates to limit
{ entitiesByPrefix(prefix: "", limit: 1000) { id } }
```

Empty prefix forces a full KV scan (`KeysByPrefix("")` walks every key in `ENTITY_STATES`) before applying the limit. At ~10K entities this is fine; at 100K it becomes the dominant cost of the request. UIs that paint a tree should start at a narrower prefix matching a known level of the 6-part entity ID and drill down on user interaction:

```graphql
# First paint — scoped to a known platform/domain root
{ entitiesByPrefix(prefix: "acme.ops.robotics", limit: 200) { id } }

# User expands a node — scope tightens further
{ entitiesByPrefix(prefix: "acme.ops.robotics.gcs.drone", limit: 200) { id } }
```

### Choose `limit` deliberately

Default is **100** when `limit` is 0 or omitted. The gateway caps the default to keep replies under NATS's default 1MB `max_payload` ceiling for entities with substantial triple sets (gh#172). For larger result sets, set `limit` explicitly up to the internal cap. Keep it at the smallest value your UI actually needs to render — a 5K-entity response wastes bandwidth and JSON decode time for no gain if the user sees 200 rows.

### Exhaustive enumeration has no API

If you need every entity under a prefix, there is no "next page" token today. The options are:

1. Query a narrower prefix that fits under the limit.
2. Query multiple narrower prefixes (e.g., enumerate level-5 types under a level-4 system and query each).
3. Raise the limit to cover your expected maximum — acceptable for admin tooling, not for user-facing UI.

Cursor-based pagination is a tracked follow-up. It is not planned for the beta line; request it explicitly if your use case pushes past these workarounds.

### Prefix matching rules

- The backend appends a trailing `.` to non-empty prefixes (so `acme.ops` matches `acme.ops.robotics.*` but not `acme.ops-extended.*`).
- Exact full-ID queries (all six parts) fall back to a direct `Get` when the prefix scan returns empty — so `entitiesByPrefix(prefix: "acme.ops.robotics.gcs.drone.001", limit: 1)` works for single-entity lookup even though the trailing-dot rule would otherwise miss.
- Empty-string prefix is the only case where no trailing `.` is added — which is exactly why it walks the whole bucket.

## Security Considerations

Production deployments should:
1. Disable playground (`enable_playground: false`)
2. Deploy behind authentication proxy
3. Implement rate limiting
4. Use TLS termination at load balancer

## Deployment

### All Tiers

The gateway is required in all deployment tiers:

```json
{
  "components": [
    {"type": "processor", "name": "graph-ingest"},
    {"type": "processor", "name": "graph-index"},
    {"type": "gateway", "name": "graph-gateway"}
  ]
}
```

### High Availability

For production, deploy multiple gateway instances behind a load balancer. Each
instance owns its HTTP facade state and NATS requester.
