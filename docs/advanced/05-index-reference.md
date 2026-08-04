# Index and Bucket Reference

SemStreams maintains multiple indexes for graph traversal and query. Understanding which indexes affect community detection helps you design triples that enable the queries you need.

## Core Indexes

| Index | Purpose | Affects Clustering? | LLM Context? |
|-------|---------|---------------------|--------------|
| **PREDICATE_INDEX** | "Find entities with this property" | YES | No |
| **INCOMING_INDEX** | "Who references this entity?" | YES | No |
| **OUTGOING_INDEX** | "What does this entity reference?" | YES | No |
| **EMBEDDING_INDEX** | Semantic similarity search | YES (Tier 2) | No |
| **ALIAS_INDEX** | Resolve friendly names | No | No |
| **SPATIAL_INDEX** | Geographic queries | No* | No* |
| **TEMPORAL_INDEX** | Time-range queries | No* | No* |

*Indexes exist and are populated. Graph providers for clustering integration are a future enhancement (see [Roadmap](../ROADMAP.md)).

## Which Indexes Feed Community Detection?

### Used by LPA

The Label Propagation Algorithm traverses edges via:

1. **OUTGOING_INDEX**: "What entities does this entity reference?"
2. **INCOMING_INDEX**: "What entities reference this entity?"
3. **PREDICATE_INDEX**: Entity filtering by property

### NOT Used in Clustering

4. **ALIAS_INDEX**: ID resolution only
5. **SPATIAL_INDEX**: Fully operational for queries; clustering provider planned
6. **TEMPORAL_INDEX**: Fully operational for queries; clustering provider planned
7. **EMBEDDING_INDEX**: Used for similarity search only

## Edge Weights

### Base Providers: Always 1.0

```go
// PredicateGraphProvider, OutgoingGraphProvider, IncomingGraphProvider
func (p *Provider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
    // ... edge lookup ...
    return 1.0, nil  // All explicit edges weighted equally
}
```

LPA treats all explicit edges the same.

## Index Details

### PREDICATE_INDEX

**Key pattern:** `{predicate}`
**Value:** List of entity IDs with that predicate

**Created by:** Any triple
**Used for:** "Find all sensors" → query predicate `entity.type` value `sensor`

#### Predicate Query API

The predicate index is exposed via GraphQL for programmatic access:

| Query | Purpose | Example |
|-------|---------|---------|
| `predicates` | List all predicates with entity counts | Discovery, schema exploration |
| `predicateStats` | Get detailed stats for one predicate | Predicate analysis, sampling |
| `entitiesByPredicate` | Find entities by predicate | Batch entity lookup |
| `compoundPredicateQuery` | AND/OR logic across predicates | Complex filtering |

**GraphQL Examples:**

```graphql
# List all predicates
query {
  predicates {
    predicates {
      predicate
      entityCount
    }
    total
  }
}

# Get stats for a specific predicate
query {
  predicateStats(predicate: "controls", sampleLimit: 5) {
    predicate
    entityCount
    sampleEntities
  }
}

# Find entities by predicate
query {
  entitiesByPredicate(predicate: "located_in", limit: 100)
}

# Compound query: entities with BOTH predicates (AND)
query {
  compoundPredicateQuery(
    predicates: ["controls", "located_in"]
    operator: "AND"
    limit: 50
  ) {
    entities
    operator
    matched
  }
}

# Compound query: entities with EITHER predicate (OR)
query {
  compoundPredicateQuery(
    predicates: ["temperature", "humidity"]
    operator: "OR"
  ) {
    entities
    matched
  }
}
```

**NATS Subjects:**

| Subject | Purpose |
|---------|---------|
| `graph.index.query.predicate` | Single predicate lookup |
| `graph.index.query.predicateList` | List all predicates |
| `graph.index.query.predicateStats` | Predicate statistics |
| `graph.index.query.predicateCompound` | Compound AND/OR queries |

### INCOMING_INDEX

**Key pattern:** `{entity_id}`
**Value:** List of entities that reference this entity

**Created by:** Triples where Object is another entity ID
**Used for:** "Who references fleet-A?" → all drones assigned to that fleet

### OUTGOING_INDEX

**Key pattern:** `{entity_id}`
**Value:** List of entities this entity references

**Created by:** Triples where Object is another entity ID
**Used for:** "What does drone-007 reference?" → its fleet, mission, etc.

### ALIAS_INDEX

**Key pattern:** `{alias}`
**Value:** Canonical entity ID

**Created by:** Triples with `alias.*` predicates (resolvable AliasTypes only — `AliasTypeLabel` is excluded)
**Used for:** Resolve "drone-alpha" → "acme.robotics.aerial.drone.drone-007"

### NAME_INDEX

**Key pattern:** `sha256(lower(trim(name)))` (names contain non-KV-key-safe chars; case-folded for recall)
**Value:** `NameIndexEntry{name, items[]}` — every entity carrying that name, with its label predicate + salience

**Created by:** Triples whose predicate is a display-name label (`AliasType==AliasTypeLabel`, e.g. `dc.terms.title`) — exactly the set ALIAS_INDEX excludes. Register product label predicates via `vocabulary.Register(pred, WithAlias(AliasTypeLabel, priority))`.
**Used for:** Deterministic `graph.query.byName` — name/title → ranked entity IDs (exact-case first, then label salience, then ID). gh#376 ask #5; sharpens deterministic symbol/title resolution that otherwise falls back to semantic search.

### SPATIAL_INDEX

**Key pattern:** Geohash-based
**Value:** Entities at that location

**Created by:** Triples with `geo.*` predicates containing lat/lon
**Used for:** "Find entities within bounding box"
**Clustering:** Not yet integrated. Spatial queries (bounding box) are fully operational.

### TEMPORAL_INDEX

**Key pattern:** Time bucket (`YYYY.MM.DD.HH`)
**Value:** Events for entities observed in that bucket

**Created by:** Entities keyed on their **observation timestamp** by precedence —
`time.observation.recorded` (event-time) when present, else `UpdatedAt` (last-write fallback).
The `entities_indexed_total{source}` metric reports the observed-vs-fallback split.
**Used for:** "Find entities in time range"
**Clustering:** Not yet integrated. Temporal queries (time range) are fully operational.

### TEMPORAL_INDEX_REVERSE

**Key pattern:** Entity ID
**Value:** The entity's current TEMPORAL_INDEX bucket key

**Created by:** graph-index-temporal, alongside each TEMPORAL_INDEX write.
**Used for:** Removing an entity's stale event from its prior bucket when it is re-observed (its
observation timestamp changed) or deleted, so a range query never returns an entity from a window it has
since left.

### EMBEDDING_INDEX

**Key pattern:** Entity ID
**Value:** Vector embedding

**Created by:** `TextContent()` → embedding service → stored
**Used for:** Semantic similarity search

## Planned: Spatial/Temporal Clustering Providers

The architecture supports spatial/temporal clustering via graph providers. Spatial and temporal indexes are fully operational for queries; clustering integration via `SpatialGraphProvider` and `TemporalGraphProvider` is a future enhancement.

**Current state:**
- Spatial and temporal indexes exist and are populated
- Queries work (bounding box, time range)
- No `SpatialGraphProvider` or `TemporalGraphProvider` for clustering integration yet
- LLM summaries don't include geo/time context yet

## Debugging Index Issues

```bash
# Check if predicate exists
nats kv get PREDICATE_INDEX "sensor.measurement.temperature"

# Check entity relationships
nats kv get OUTGOING_INDEX "drone-007"
nats kv get INCOMING_INDEX "fleet-warehouse-7"

# Check spatial coverage
nats kv keys SPATIAL_INDEX | head -20

# Check embedding exists
nats kv get EMBEDDING_INDEX "drone-007"
```

## KV Bucket Reference

SemStreams stores all graph state in NATS JetStream KV buckets. Each bucket is created by its respective processor.

### Tier 0 Buckets

Core graph storage, available in all deployments.

#### ENTITY_STATES

Primary entity storage. Each entity is stored as a complete JSON record.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-ingest |
| **Key format** | Entity ID (6-part dotted notation) |
| **Value** | JSON entity with triples, aliases, metadata |

**Example key**: `acme.ops.sensors.warehouse.temperature.001`

#### OUTGOING_INDEX

Forward relationship lookup. Maps entity to its outgoing relationships.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index |
| **Key format** | Entity ID |
| **Value** | Array of `{predicate, to_entity_id}` |

**Use case**: "What entities does X connect to?"

#### INCOMING_INDEX

Reverse relationship lookup. Maps entity to entities pointing at it.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index |
| **Key format** | Entity ID |
| **Value** | Array of `{predicate, from_entity_id}` |

**Use case**: "What entities point to X?"

#### ALIAS_INDEX

Entity alias resolution. Maps alias values to entity IDs.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index |
| **Key format** | Alias value |
| **Value** | Entity ID |

**Use case**: Resolve "sensor-001" to full entity ID.

#### PREDICATE_INDEX

Predicate-based entity lookup. Maps predicates to entities that have them.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index |
| **Key format** | Predicate (dotted notation) |
| **Value** | Array of entity IDs |

**Use case**: "Find all entities with `located_in` predicate."

#### SPATIAL_INDEX

Geographic bounds lookup. Maps geohash prefixes to entities.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index-spatial |
| **Key format** | Geohash prefix |
| **Value** | Array of entity IDs with coordinates |

**Use case**: "Find entities within geographic bounds."

#### TEMPORAL_INDEX

Time range lookup. Maps time buckets to entities.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-index-temporal |
| **Key format** | Time bucket (e.g., `2024-01-15T10`) |
| **Value** | Array of entity IDs with timestamps |

**Use case**: "Find entities active in time range."

Triple provenance is not duplicated into a durable index. `message.Triple.Context`
remains stored with each authoritative entity in `ENTITY_STATES`. There is no
production query-by-context contract; operator and E2E diagnostics may inspect
bounded authoritative state directly.

#### COMPONENT_STATUS

Component lifecycle status. Tracks current processing stage of long-running components.

| Attribute | Value |
|-----------|-------|
| **Created by** | Any component implementing LifecycleReporter |
| **Key format** | Component name |
| **Value** | Status JSON (stage, cycle_id, timestamps) |

**Use case**: Operational monitoring, "What stage is graph-clustering in?"

### Tier 1+ Buckets

Buckets requiring embeddings or community detection, created by statistical/semantic tier processors.

#### EMBEDDING_INDEX

Embedding vectors for similarity search.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-embedding |
| **Key format** | Entity ID |
| **Value** | Vector array + metadata (model, dimensions) |

**Use case**: Semantic similarity search.

#### EMBEDDING_DEDUP

Deduplication tracking to avoid re-embedding unchanged content.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-embedding |
| **Key format** | Content hash |
| **Value** | Entity ID + timestamp |

**Use case**: Skip embedding if content unchanged.

#### COMMUNITY_INDEX

Community membership and metadata.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-clustering |
| **Key format** | Community ID |
| **Value** | Community JSON (members, level, summary) |

**Use case**: GraphRAG queries, community-based search.

#### ANOMALY_INDEX

Detected anomalies awaiting review.

| Attribute | Value |
|-----------|-------|
| **Created by** | graph-clustering |
| **Key format** | Anomaly ID |
| **Value** | Anomaly JSON (type, entities, confidence, suggestion) |

**Use case**: Anomaly approval workflow, gap detection results.

### Bucket Lifecycle

Buckets are created on-demand when their processor starts. They persist across restarts via JetStream.

**Watching for Changes**

All buckets support reactive watching:

```go
watcher, _ := kv.Watch("ENTITY_STATES.>")
for entry := range watcher.Updates() {
    // React to entity changes
}
```

**Retention**

Bucket retention is configured per-processor. Default is to keep latest value only (KV semantics), but history can be enabled for audit trails.

## Next Steps

- [Community Detection](../concepts/07-community-detection.md) - How indexes enable clustering
- [Vocabulary](../basics/04-vocabulary.md) - Predicate naming conventions
- [Clustering Configuration](01-clustering.md) - LPA and hierarchical detection
- [Event-Driven Basics](../concepts/01-event-driven-basics.md) - How KV buckets fit into the architecture
