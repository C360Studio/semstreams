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

`entitiesByPrefix(prefix: String!, limit: Int, cursor: String)` returns an
`EntityPage`. The page's `entities` field contains full entities whose IDs start
with the prefix. When `next_cursor` is present, pass that opaque value back as
`cursor` to continue; an absent or empty cursor means the scan is complete.

### Avoid empty-prefix queries in production UIs

```graphql
# Don't do this for a routine UI paint — each page scans every graph key
{ entitiesByPrefix(prefix: "", limit: 1000) { entities { id } next_cursor } }
```

Empty prefix forces a full KV scan (`KeysByPrefix("")` walks every key in `ENTITY_STATES`) before applying the limit. At ~10K entities this is fine; at 100K it becomes the dominant cost of the request. UIs that paint a tree should start at a narrower prefix matching a known level of the 6-part entity ID and drill down on user interaction:

```graphql
# First paint — scoped to a known platform/domain root
{ entitiesByPrefix(prefix: "acme.ops.robotics", limit: 200) { entities { id } next_cursor } }

# User expands a node — scope tightens further
{ entitiesByPrefix(prefix: "acme.ops.robotics.gcs.drone", limit: 200) { entities { id } next_cursor } }
```

### Choose `limit` deliberately

Default and maximum page counts are **1000**. Graph ingest may return fewer
entities when the complete encoded page reaches the active NATS payload limit;
that page carries `next_cursor`, so callers continue without predicting a safe
byte size. Keep `limit` at the smallest count the UI needs to render.

### Follow continuation to enumerate exhaustively

Request the first page without `cursor`. For each response, append `entities`
and send `next_cursor` verbatim as the next request's `cursor`. Stop only when
the token is absent or empty:

```graphql
query PrefixPage($prefix: String!, $limit: Int, $cursor: String) {
  entitiesByPrefix(prefix: $prefix, limit: $limit, cursor: $cursor) {
    entities { id }
    next_cursor
  }
}
```

Treat the cursor as opaque: do not parse, construct, or modify it. Each page
performs a fresh prefix-key scan, so narrower prefixes remain preferable for
large graphs.

### Prefix matching rules

- The backend appends a trailing `.` to non-empty prefixes (so `acme.ops` matches `acme.ops.robotics.*` but not `acme.ops-extended.*`).
- Exact full-ID queries (all six parts) fall back to a direct `Get` when the prefix scan returns empty — so `entitiesByPrefix(prefix: "acme.ops.robotics.gcs.drone.001", limit: 1) { entities { id } next_cursor }` works for single-entity lookup even though the trailing-dot rule would otherwise miss.
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
