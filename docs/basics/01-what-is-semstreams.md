# What is SemStreams?

SemStreams is a stream processor that builds a semantic knowledge graph from event data. You define a vocabulary of predicates for your domain, implement a simple interface, and the system maintains a living graph with automatic community detection.

## Why SemStreams?

Traditional knowledge graph systems assume cloud-first deployments with abundant compute and always-on connectivity. SemStreams takes a different approach: **edge-first, offline-capable, progressively enhanced**.

**Resource-adaptive**: Deploy on a Raspberry Pi with just NATS, or scale to a cluster with neural embeddings and LLM summarization. The same codebase works across the spectrum—you enable what your resources support.

**Domain-driven**: No mandatory AI dependencies. Start with explicit relationships and rules. Add BM25 search when you need it. Enable neural embeddings when the domain benefits from semantic similarity. You decide what makes sense for your use case.

**Offline-resilient**: NATS JetStream provides the foundation—local persistence, automatic sync when connectivity returns. Your knowledge graph keeps working when the network doesn't.

The core opinion: **users should decide what to enable**. SemStreams provides the building blocks; you compose them based on your constraints and requirements.

## The Core Concept

```text
Your Events → Graphable Interface → Knowledge Graph → Queries
```

1. Events arrive (telemetry, records, notifications)
2. Your processor transforms them into entities with triples
3. SemStreams maintains the graph, indexes, and communities
4. Query by relationships, predicates, or semantic similarity

## What You Provide

### 1. A Predicate Vocabulary

Define the facts you want to capture about your domain:

```go
const (
    DroneType        = "drone.classification.type"
    DroneBattery     = "drone.telemetry.battery"
    DroneAltitude    = "drone.telemetry.altitude"
    FleetMembership  = "fleet.membership.current"
)
```

Predicates follow `domain.category.property` format—a three-part dotted notation. This is not optional because:

1. **NATS Wildcard Queries**: Dotted notation maps to NATS subject wildcards (e.g., `sensor.measurement.*` finds all measurement predicates)
2. **KV Key Patterns**: Entity IDs and predicates become KV bucket keys, enabling prefix-based lookups
3. **SQL-Like Semantics**: The three-part structure gives your knowledge graph SQL-like query capabilities via wildcard matching

Inconsistent predicate names fragment your indexes and break wildcard queries.

### 2. A Graphable Implementation

Transform your domain messages into graph entities:

```go
type Graphable interface {
    EntityID() string          // 6-part federated identifier
    Triples() []message.Triple // Facts about this entity
}
```

Example:

```go
type DroneTelemetry struct {
    DroneID  string  `json:"drone_id"`
    Battery  int     `json:"battery"`
    Altitude float64 `json:"altitude"`
    FleetID  string  `json:"fleet_id"`
}

func (d *DroneTelemetry) EntityID() string {
    return fmt.Sprintf("acme.ops.robotics.gcs.drone.%s", d.DroneID)
}

func (d *DroneTelemetry) Triples() []message.Triple {
    id := d.EntityID()
    return []message.Triple{
        {Subject: id, Predicate: DroneType, Object: "cargo"},
        {Subject: id, Predicate: DroneBattery, Object: d.Battery},
        {Subject: id, Predicate: DroneAltitude, Object: d.Altitude},
        // This creates a relationship (edge) to another entity:
        {Subject: id, Predicate: FleetMembership, Object: d.FleetID},
    }
}
```

## What SemStreams Provides

### Entity Storage

Entities stored in NATS KV with version tracking:

```json
{
  "id": "acme.ops.robotics.gcs.drone.001",
  "triples": [
    {"predicate": "drone.classification.type", "object": "cargo"},
    {"predicate": "drone.telemetry.battery", "object": 78}
  ],
  "version": 5
}
```

### Typed Graph Mutations

Graphable ingestion merges event facts into the entity. Components that intentionally change current state use a local
projection contract over the typed graph mutation port: strict create, complete-set reconcile, set-valued append, or
revision-fenced delete. Local contracts validate a component's own requests; they do not grant global semantic ownership.

This matters for domains where graph facts are operational state, not just observations: lifecycle phases, resource
PATCH/PUT fields, command status, mission progress, quality records, topology, or agent-written findings. The graph
remains queryable current state, while exact reads and CAS expose real conflicts instead of predicting an exclusive
semantic author.

See [Governed Semantic State](../concepts/28-governed-semantic-state.md) for the contract.

### Indexes

**Core Indexes** (always available):

| Index | Question It Answers |
|-------|---------------------|
| PREDICATE_INDEX | "All entities with this property" |
| INCOMING_INDEX | "Who references this entity?" |
| OUTGOING_INDEX | "What does this entity reference?" |
| ALIAS_INDEX | "Resolve friendly name to entity ID" |
| SPATIAL_INDEX | "Entities near this location" |
| TEMPORAL_INDEX | "Entities in this time range" |

**Optional Indexes** (enabled via configuration):

| Index | Question It Answers | Requirements |
|-------|---------------------|--------------|
| EMBEDDING_INDEX | "Semantically similar entities" | Tier 2 (Semantic) |
| COMMUNITY_INDEX | "What community does this entity belong to?" | Tier 1+ (Statistical/Semantic) |

### Structural Anomaly Inputs

When anomaly detection is enabled, graph-clustering computes these inputs in memory for the current cycle:

- **K-core decomposition**: Identifies the dense backbone of the graph. Higher core numbers indicate more central,
  densely connected entities.
- **Pivot-based distance**: Estimates structural separation from landmark nodes for anomaly detectors.

These values are internal anomaly prerequisites, not a durable or queryable structural index.

### Automatic Community Detection

Entities that reference each other cluster into communities. You don't define communities—they emerge from relationships in your data.

### Anomaly Detection

With anomaly detection enabled on graph-clustering, SemStreams can detect anomalies:

- **Core isolation**: Entities disconnected from their expected peer group
- **Core demotion**: Entities losing connectivity over time
- **Semantic-structural gaps**: Semantically similar entities that lack graph connections (requires Tier 2)

### Progressive Enhancement (Tiers)

SemStreams supports three capability tiers. Start minimal, add capabilities as your resources and requirements grow.

| Tier | Name | Capabilities | Requirements |
|------|------|--------------|--------------|
| 0 | **Structural** | Rules engine and explicit graph relationships | NATS only |
| 1 | **Statistical** | + BM25 search, lexical similarity, statistical communities | Same |
| 2 | **Semantic** | + Neural embeddings, meaning-based similarity, LLM summaries | + Embedding service |

**Why tiers?** Edge deployments can't run neural models. Cloud deployments want semantic search. Tiers let you match capabilities to constraints—same codebase, different configs.

Start with Tier 0 (Structural). Add Statistical for keyword search. Add Semantic when you need "machine" to match "equipment".

For details on what each tier provides, see [Real-Time Inference](../concepts/00-real-time-inference.md). Each configuration is controlled via JSON—see [Configuration](06-configuration.md) for details.

## Entity ID Format

Use 6-part hierarchical identifiers:

```text
org.platform.domain.system.type.instance
 │      │       │      │     │      │
 │      │       │      │     │      └─ Instance (001, rescue, sensor-a)
 │      │       │      │     └─ Entity type (drone, fleet, sensor)
 │      │       │      └─ Source system (gcs, hq, factory)
 │      │       └─ Data domain (robotics, ops, iot)
 │      └─ Platform (rescue, warehouse, production)
 └─ Organization (acme, globex)
```

Benefits:

- Federation across organizations
- Queryable by prefix (`acme.ops.*`)
- Self-documenting entity provenance

## Relationships Create Edges

When a triple's Object is another entity ID, it creates a traversable edge:

```go
// This creates an edge in the graph:
{Subject: droneID, Predicate: "fleet.membership.current", Object: "acme.ops.fleet.rescue"}

// This is just a property (no edge):
{Subject: droneID, Predicate: "drone.metadata.name", Object: "Alpha-1"}
```

Edges are what community detection uses to cluster related entities.

## When to Use SemStreams

**Good fit:**

- Real-time entity tracking with relationships
- Need to discover connected entities automatically
- Want community-based grouping without manual rules
- Progressive enhancement from basic to AI-powered

**Not designed for:**

- Pure time-series metrics (use InfluxDB, Prometheus)
- Full-text search only (use Elasticsearch)
- Batch ETL pipelines (use Airflow + dbt)
- Simple key-value lookups (use Redis)

## Next Steps

- [Architecture](02-architecture.md) - How the system works
- [Graphable Interface](03-graphable-interface.md) - Implementation details
- [Vocabulary](04-vocabulary.md) - Designing your predicates
- [First Processor](05-first-processor.md) - Step-by-step tutorial
- [Configuration](06-configuration.md) - Progressive enhancement options
