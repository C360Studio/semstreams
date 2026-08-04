# PathRAG Pattern

PathRAG performs bounded graph traversal from a known starting entity, discovering connected entities through explicit relationships.

## What PathRAG Does

Given a starting entity, PathRAG answers: "What's connected to this, and how?"

```text
Starting Entity: config-db-primary
        │
        ▼
    ┌───────┐
    │ PathRAG│──► service-auth (depends_on, score: 1.0)
    │  Query │──► service-api (depends_on, score: 1.0)
    └───────┘──► service-worker (depends_on → service-api, score: 0.8)
                 └──► cache-redis (uses, score: 0.64)
```

Unlike GraphRAG (semantic search), PathRAG follows the actual structure of your knowledge graph.

## When to Use PathRAG

**Strong fit:**
- **Dependency analysis**: "What breaks if this config changes?"
- **Impact radius**: "What's affected by this incident?"
- **Mesh discovery**: "What nodes can reach this base station?"
- **Audit trails**: "How did this entity connect to that one?"

**Weak fit:**
- Topic-based search (use GraphRAG)
- No known starting entity (use GraphRAG global search)
- Semantic similarity (use embeddings)

## PathRAG vs GraphRAG

| Aspect | PathRAG | GraphRAG |
|--------|---------|----------|
| Input | Entity ID | Query text |
| Traverses | Explicit relationships (triples) | Community structure |
| Output | Paths + scored entities | Summaries + context |
| Deterministic | Yes | No (community detection varies) |
| Min tier | Tier 0 (rules only) | Tier 1 (clustering required) |
| Speed | Fast (local traversal) | Medium (community lookup) |

## How It Works

### Bounded Graph Traversal

PathRAG performs breadth-first traversal with resource limits:

```text
config-db ──depends_on──► service-auth ──uses──► cache-redis
    │                          │
    └──depends_on──► service-api ──uses──► cache-redis
                          │
                          └──calls──► service-worker
```

Starting from `config-db`:
- Depth 0: `config-db` (score: 1.0)
- Depth 1: `service-auth`, `service-api` (score: 0.8)
- Depth 2: `cache-redis`, `service-worker` (score: 0.64)

### Decay Function

Relevance decreases with distance:

```text
score = 0.8 ^ depth
```

| Decay Factor | Depth 1 | Depth 2 | Depth 3 | Use Case |
|--------------|---------|---------|---------|----------|
| 0.9 | 0.90 | 0.81 | 0.73 | Gentle: distant entities still relevant |
| 0.8 | 0.80 | 0.64 | 0.51 | Balanced (default) |
| 0.6 | 0.60 | 0.36 | 0.22 | Aggressive: focus on immediate neighbors |

### Resource Bounds

PathRAG guarantees bounded execution:

| Limit | Purpose | Default |
|-------|---------|---------|
| `max_depth` | Prevents infinite loops | 10 hops |
| `max_nodes` | Bounds memory | 100 nodes |

If any limit is hit, results are marked `truncated: true`.

## Configuration

### Basic PathRAG Query

A PathRAG query requires a starting entity and accepts optional bounds. Key parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `start_entity` | (required) | Entity ID to start traversal from |
| `max_depth` | 10 | Maximum hops from start entity |
| `max_nodes` | 100 | Maximum entities to return |
| `max_paths` | 0 (unlimited) | Maximum paths to return |
| `timeout` | (none) | Per-request timeout (e.g., `5s`) |

The decay factor is currently fixed at `0.8`; it is not a request knob.

### Predicate Filtering

Limit traversal to specific relationship types. For example, filtering to only `depends_on` and `uses` predicates will ignore relationships like `located_in` or `owned_by` that aren't relevant to dependency analysis.

| Parameter | Type | Description |
|-----------|------|-------------|
| `predicates` | `[]string` | Only follow edges with these predicate types |

> **Note**: Implemented by internal `graph.query.pathSearch` and exposed through
> the provisional HTTP `pathSearch` facade operation.

### Direction Control

Control which edges are followed during traversal:

| Direction | Follows | Use Case |
|-----------|---------|----------|
| `outgoing` | Entity → references | "What does this depend on?" (default) |
| `incoming` | References → entity | "What depends on this?" |
| `both` | Bidirectional | "What's connected either way?" |

> **Note**: Implemented by internal `graph.query.pathSearch` and exposed through
> the provisional HTTP `pathSearch` facade operation.

## API and Response

PathRAG is implemented on internal `graph.query.pathSearch` and exposed by the
provisional HTTP `pathSearch` facade operation. A controlled remote caller may use
that documented operation; raw NATS is not an adopter fallback. SemStreams has no
MCP graph contract. The response contains:

| Field | Description |
|-------|-------------|
| `entities` | Discovered `{id, type, score}` records; score is per entity |
| `paths` | Paths as ordered `{from, predicate, to}` step arrays |
| `truncated` | Whether any resource limit was hit |

The `paths` field is particularly useful for understanding *how* entities are connected—not just *that* they're connected.

## Use Case Examples

### Dependency Chain Analysis

**Question:** "What services break if the database config changes?"

**Approach:** Start from the config entity, filter to `depends_on` and `reads_from` predicates, traverse incoming edges (what depends on this config), and go up to 4 hops deep to capture transitive dependencies.

### Incident Impact Radius

**Question:** "What's affected by this failing sensor?"

**Approach:** Start from the failing sensor, filter to `monitors`, `alerts`, and
`triggers` predicates, and limit depth to 3 hops.

### Mesh Network Topology

**Question:** "What drones can reach this base station?"

**Approach:** Start from the base station, filter to `communicates_with` and `relays_to` predicates, traverse incoming edges (what can reach this), allow deeper traversal (5 hops) and more nodes (200) for mesh discovery.

## Performance considerations

No repository-owned cross-scale latency contract exists. Measure against the
deployed graph and request timeout.

**Bounding tips:**
- Use `predicates` to reduce edge count
- Lower `max_depth` if you only need immediate neighbors

## Combining with GraphRAG

PathRAG and GraphRAG complement each other:

```text
1. GraphRAG: "Find entities related to authentication issues"
   └─► Returns: [service-auth, auth-config, user-db]

2. PathRAG: "What depends on service-auth?"
   └─► Returns: Impact graph showing affected services
```

**Pattern:** Use GraphRAG for discovery, PathRAG for impact analysis.

## Common Issues

### "Traversal times out"

1. Reduce `max_depth` (most impact)
2. Add `predicates` to limit edge types
3. Set the request `timeout` within the caller's latency budget
4. Check for dense hub nodes (many connections)

### "Missing expected entities"

1. Verify relationships exist as triples (check OUTGOING_INDEX)
2. Check `predicates` is not excluding the relationship type
3. Increase `max_depth` if entities are further than expected
4. Confirm `direction` is correct (incoming vs outgoing)

### "Results are truncated"

1. Check which limit was hit (depth, nodes, time, paths)
2. Increase the relevant limit
3. Use `predicates` to focus traversal
4. Consider whether you need all results or just top-scored

## Index Requirements

PathRAG requires relationship indexes to be enabled:

| Index | Purpose |
|-------|---------|
| OUTGOING_INDEX | Entity → what it references |
| INCOMING_INDEX | Entity → what references it |

PathRAG checks entity existence, traverses incoming/outgoing relationships, and
applies predicate filters in memory. It does not depend on `PREDICATE_INDEX`.

## Related

**Concepts**
- [Real-Time Inference](00-real-time-inference.md) - PathRAG works at Tier 0 (no ML required)
- [GraphRAG Pattern](09-graphrag-pattern.md) - Semantic search alternative for topic-based queries
- [Knowledge Graphs](04-knowledge-graphs.md) - How triples create the relationships PathRAG traverses
- [Community Detection](07-community-detection.md) - How communities differ from structural paths
- [Anomaly Detection](08-anomaly-detection.md) - Background topology analysis (Tier 1+ feature)

**Configuration**
- [Configuration Guide](../basics/06-configuration.md) - Index and traversal settings
