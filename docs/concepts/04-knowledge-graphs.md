# Knowledge Graphs

Knowledge graphs represent information as entities connected by relationships, using a Subject-Predicate-Object (SPO) model.

## What is a Knowledge Graph?

A knowledge graph stores facts about entities and their relationships:

```text
┌─────────┐  located_in   ┌─────────┐
│sensor-01│──────────────►│ zone-A  │
└────┬────┘               └─────────┘
     │
     │ measures
     ▼
  ┌──────┐
  │ 23.5 │  (temperature)
  └──────┘
```

Each arrow is a fact: `sensor-01 located_in zone-A`, `sensor-01 measures 23.5`.

## The SPO Model (Triples)

Every fact is a triple: **Subject-Predicate-Object**.

| Subject | Predicate | Object | Type |
|---------|-----------|--------|------|
| sensor-01 | located_in | zone-A | Relationship |
| sensor-01 | measures | 23.5 | Property |
| zone-A | contains | sensor-01 | Relationship |
| zone-A | name | "Warehouse A" | Property |

**Subject**: The entity being described.
**Predicate**: The relationship or property name.
**Object**: The value (another entity ID or a literal value).

### Relationships vs Properties

| Type | Object Value | Creates | Used For |
|------|--------------|---------|----------|
| **Property** | Literal (number, string, bool) | Attribute on entity | Search, display, filtering |
| **Relationship** | Entity ID | Edge in graph | Traversal, clustering, PathRAG |

Example triples for a sensor:

| Predicate | Object | Type |
|-----------|--------|------|
| `sensor.reading.celsius` | 23.5 | Property |
| `sensor.model` | "TempPro-3000" | Property |
| `sensor.location.zone` | zone-A | Relationship |
| `sensor.monitors` | equipment-hvac-01 | Relationship |

**Relationships** create edges in the graph (for traversal, community detection).
**Properties** describe the entity (for search, display).

## Semantic vs Property Graphs

Two common graph models exist. SemStreams uses a semantic approach.

### Property Graphs (Neo4j style)

Nodes and edges both have properties. Edges are typed and carry attributes.

```text
┌────────────────┐   LOCATED_IN    ┌─────────────────┐
│ sensor-01      │────────────────►│ zone-A          │
│ model: TempPro │  since: 2024-01 │ name: Warehouse │
└────────────────┘                 └─────────────────┘
```

- Optimized for traversal queries
- Schema-optional
- Edge attributes require special handling

### Semantic Graphs (RDF style)

Everything is triples. Uniform representation enables reasoning.

```text
┌───────────┐     ┌───────────┐     ┌─────────────┐
│ sensor-01 │────►│ rdf:type  │────►│ Sensor      │
└───────────┘     └───────────┘     └─────────────┘
      │           ┌───────────┐     ┌─────────────┐
      └──────────►│ located_in│────►│ zone-A      │
                  └───────────┘     └─────────────┘
```

- Uniform representation (all facts are triples)
- Standards-based (RDF, OWL, SPARQL)
- Optimized for reasoning and inference

### SemStreams Approach

SemStreams uses **RDF-like triples** with **RDF\* (RDF-Star) extensions**:

- SPO model for all facts
- No SPARQL or OWL reasoning
- Predicate naming conventions (dotted notation) instead of formal ontologies
- Practical balance: semantic clarity without specification overhead

### RDF\* (Triple Metadata)

Standard RDF triples are just Subject-Predicate-Object. SemStreams extends this with metadata *on the triple itself*:

```text
Standard RDF:
  sensor-01 → measures → 23.5

SemStreams (RDF*):
  sensor-01 → measures → 23.5
       ├── confidence: 0.95
       ├── source: "mavlink_telemetry"
       ├── timestamp: 2024-01-15T10:30:00Z
       └── context: "batch-42"
```

This enables:

| Field | Purpose |
|-------|---------|
| **confidence** | How reliable is this assertion? (0.0-1.0) |
| **source** | Where did this fact come from? (provenance) |
| **timestamp** | When was this asserted? (temporal queries) |
| **context** | Which batch/request produced this? (correlation) |

**Why this matters:**
- **Conflict resolution**: When two sources disagree, confidence helps decide
- **Audit trails**: Track how facts entered the system
- **Time travel**: Query facts as they were at a point in time
- **Debugging**: Trace facts back to their originating events

## Entity Identifiers

SemStreams uses 6-part federated identifiers:

```text
organization.domain.category.type.subtype.instance
└────────────┬───────────────┘ └────────┬────────┘
        Namespace                   Identity

Example: acme.logistics.sensor.environmental.temperature.sensor-042
```

**Why 6 parts?**
- Globally unique without central registry
- Hierarchical queries via NATS wildcards
- Self-describing (namespace carries meaning)

> **Note:** Examples in this documentation may use abbreviated IDs (e.g., `sensor-01`) for readability. In practice, all entity IDs must be 6-part.

## Predicates (Relationship Names)

Predicates follow a **three-part dotted notation**: `domain.category.property`

```text
sensor.reading.celsius
sensor.reading.humidity
sensor.location.zone
geo.location.latitude
alert.status.severity
```

### Why Three Parts?

The structure enables **NATS wildcard queries** on index keys:

```text
sensor.reading.*      → All reading predicates (celsius, humidity, etc.)
sensor.>              → All sensor predicates (reading.*, location.*, etc.)
*.location.*          → All location predicates across domains
```

| Wildcard | Meaning | Example Match |
|----------|---------|---------------|
| `*` | Single token | `sensor.*.zone` matches `sensor.location.zone` |
| `>` | One or more tokens | `sensor.>` matches `sensor.reading.celsius` |

**This matters because:**
- PREDICATE_INDEX keys are the predicates themselves
- Query "all temperature readings" → `sensor.reading.*`
- Query "everything about sensors" → `sensor.>`
- More specific predicates = more efficient queries

### Benefits

- **Wildcard queries**: Find related predicates without knowing exact names
- **Consistent naming**: `domain.category.property` pattern across all entities
- **Standard mappings**: Maps to RDF vocabularies (Dublin Core, Schema.org)

### Standard Vocabulary Mapping

SemStreams provides mappings to established vocabularies:

| SemStreams Predicate | Standard Equivalent |
|----------------------|---------------------|
| `meta.timestamp.created` | dc:created |
| `meta.content.title` | dc:title |
| `geo.location.lat` | schema:latitude |
| `entity.class.type` | rdf:type |

Use standard mappings when interoperability matters.

## Indexes from Triples

Triples automatically populate indexes:

| Triple Component | Index | Query Enabled |
|------------------|-------|---------------|
| Predicate | PREDICATE_INDEX | "All entities with this property" |
| Object (if entity) | INCOMING_INDEX | "Who references this entity?" |
| Subject | OUTGOING_INDEX | "What does this entity reference?" |

```text
Triple: sensor-01 → located_in → zone-A

Creates:
  PREDICATE_INDEX[located_in] += sensor-01
  INCOMING_INDEX[zone-A] += sensor-01
  OUTGOING_INDEX[sensor-01] += zone-A
```

### Spatial & Temporal Indexes

SemStreams includes built-in spatial and temporal indexing—uncommon in most graph databases.

```text
Standard Graph:
  sensor-01 → located_in → zone-A

SemStreams:
  sensor-01 → located_in → zone-A
       ├── SPATIAL: geohash "9q8yyk" (lat/lon)
       └── TEMPORAL: bucket "2024.01.15.10" (hour)
```

| Index | Key Format | Enables |
|-------|------------|---------|
| **SPATIAL_INDEX** | Geohash (e.g., "9q8yyk") | "All sensors in this bounding box" |
| **TEMPORAL_INDEX** | Hour bucket (YYYY.MM.DD.HH) | "All events between 10:00 and 11:00" |

**Spatial:** Uses geohash encoding with configurable precision (~30m default). Extracts coordinates from `geo.location.latitude/longitude` predicates.

**Temporal:** Hour-precision time buckets tracking entity updates. Every entity automatically indexed by update time.

This enables hybrid queries combining semantic similarity, spatial bounds, and time ranges.

## Why Triples for SemStreams?

### 1. Flexible Schema

No upfront schema definition. Add new predicates anytime without migrations.

Today your sensor has temperature. Tomorrow add humidity, calibration dates, or any new property—just create triples with new predicates. No schema changes, no downtime.

### 2. Query by Structure

Find entities by relationship patterns:

```text
// "All sensors in zone-A"
INCOMING_INDEX["zone-A"] → {sensor-01, sensor-02, sensor-03}

// "All temperature readings"
PREDICATE_INDEX["sensor.reading.celsius"] → {sensor-01, sensor-05}
```

### 3. Natural Clustering

Relationships create graph edges. Community detection finds dense regions automatically:

```text
Sensors ──located_in──► Zone-A ◄──located_in── Equipment
                            │
            Community emerges from shared relationships
```

### 4. PathRAG Traversal

Explicit relationships enable bounded graph traversal:

```text
Start: config-db-001
Traverse: depends_on, uses
Result: All services affected by config change
```

## Implementing Entities

Your domain types implement the `Graphable` interface to become graph entities:

1. **EntityID()** - Returns the 6-part federated identifier
2. **Triples()** - Returns properties and relationships as triples

See [Implementing Graphable](../basics/03-graphable-interface.md) for code examples and patterns.

## Querying the Graph

Three query patterns enabled by indexes:

| Query Type | Index Used | Example |
|------------|------------|---------|
| By ID | Direct KV lookup | "Get sensor-042" |
| By predicate | PREDICATE_INDEX | "All entities with temperature readings" |
| By relationship | INCOMING_INDEX | "All sensors in zone-A" |

## Graph vs Relational

| Concern | Relational (SQL) | Graph (Triples) |
|---------|------------------|-----------------|
| Schema | Fixed, upfront | Flexible, emergent |
| Relationships | JOIN tables | First-class edges |
| Traversal | Expensive (recursive CTEs) | Native (indexes) |
| Discovery | Requires knowing schema | Query by structure |
| Scale | Vertical | Horizontal (KV) |

**When to use graph:** Relationships are central, schema evolves, discovery matters.
**When to use relational:** Fixed schema, ACID transactions, complex aggregations.

## Common Patterns

### Bidirectional Relationships

SemStreams automatically indexes both directions:

```text
┌───────────┐  located_in  ┌─────────┐
│ sensor-01 │─────────────►│ zone-A  │
└───────────┘              └─────────┘

Forward:  OUTGOING_INDEX[sensor-01] → zone-A
Reverse:  INCOMING_INDEX[zone-A] → sensor-01  (automatic)
```

No need to create explicit reverse triples—the INCOMING_INDEX handles reverse lookups.

### Temporal Properties

Every triple carries a timestamp, enabling time-range queries. For explicit temporal data, the TEMPORAL_INDEX allows queries like "all readings between 10:00 and 11:00."

See [Temporal Index](../advanced/05-index-reference.md#temporal_index) for details.

### Hierarchical Entities

Use entity ID hierarchy:

```text
acme.logistics.zone.facility.area.warehouse-7
acme.logistics.zone.facility.area.warehouse-7.section.north
acme.logistics.zone.facility.area.warehouse-7.section.south
```

Query with wildcards: `acme.logistics.zone.facility.area.warehouse-7.*`

## Related

**Basics (how to implement):**
- [Implementing Graphable](../basics/03-graphable-interface.md) - Writing entity types
- [Vocabulary](../basics/04-vocabulary.md) - Predicate design patterns

**Reference (API details):**
- [Index and Bucket Reference](../advanced/05-index-reference.md) - Index storage schema

**Concepts (mental models):**
- [Community Detection](07-community-detection.md) - Clustering from edges
- [PathRAG Pattern](10-pathrag-pattern.md) - Traversing relationships
- [Real-Time Inference](00-real-time-inference.md) - How triples flow through tiers
