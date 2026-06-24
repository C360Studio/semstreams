# Community Detection

Community detection automatically groups entities that are more connected to each other than to the rest of the graph.

## What Communities Are

A community is a dense subgraph—entities that cluster together based on relationships:

```text
┌─────────────────────────────┐
│  Community: Warehouse-A     │
│  ┌───┐   ┌───┐   ┌───┐     │
│  │ S1│───│ S2│───│ S3│     │  Sensors monitoring
│  └─┬─┘   └─┬─┘   └─┬─┘     │  the same zone
│    │       │       │        │
│    └───────┴───────┘        │
│           Zone-A            │
└─────────────────────────────┘

┌─────────────────────────────┐
│  Community: Fleet-Alpha     │
│  ┌───┐   ┌───┐   ┌───┐     │
│  │D01│───│D02│───│D03│     │  Drones in the
│  └───┘   └───┘   └───┘     │  same fleet
└─────────────────────────────┘
```

Communities emerge from data—you don't define them manually.

## Why Communities Matter

### 1. Discovery Without Questions

Traditional queries require knowing what to ask. Communities reveal structure you didn't know existed:

- "I didn't know these 12 sensors were all monitoring related equipment"
- "These services form a natural boundary—they could be a microservice"

### 2. Context for LLMs (GraphRAG)

Communities provide pre-organized context for RAG:

- Instead of 10,000 raw entities, the LLM sees organized clusters
- Each community has a summary describing its contents
- Related entities are already grouped together

### 3. Anomaly Detection

Community membership changes signal events:

- Entity joins unexpected community → potential misconfiguration
- Community splits → system architecture change
- Entity isolated from all communities → orphan/dead code

## The Label Propagation Algorithm (LPA)

SemStreams uses LPA for community detection. Here's how it works:

### Intuition

Each entity starts with a unique label. In each iteration, entities adopt the most common label among their neighbors. Labels "propagate" through dense regions until stable communities form.

### Algorithm Steps

```text
1. Initialize: Each entity gets unique label
   
   A[1] ─── B[2] ─── C[3]
    │        │
   D[4] ─── E[5]

2. Iteration 1: Each entity adopts neighbor majority label
   
   A[2] ─── B[2] ─── C[2]    (B's label spreads)
    │        │
   D[2] ─── E[2]

3. Converged: All connected entities share label
   
   Community 2: {A, B, C, D, E}
```

### Why LPA?

| Property | Benefit |
|----------|---------|
| **Fast** | O(k × m) where k = iterations, m = edges |
| **Scalable** | Handles millions of entities |
| **No parameters** | Doesn't require specifying number of communities |
| **Intuitive** | Dense regions naturally emerge |

### LPA Limitations

**Non-deterministic**: When multiple labels tie for majority, tie-breaking is random. Running twice may produce different communities.

**Disconnected components**: Entities with no edges have no neighbors to propagate labels from, so they form singleton communities.

**Large community bias**: Dense, well-connected regions can absorb adjacent smaller communities. Unlike modularity-based methods, LPA has no resolution limit—but it can still miss fine structure when labels from large communities dominate propagation.

## Graph Edges in SemStreams

LPA operates on a graph with two edge types:

### Explicit Edges (from triples)

Relationships you define in your domain model become graph edges. When an entity declares triples like "located_in" pointing to a zone or "monitors" pointing to equipment, these create edges stored in OUTGOING_INDEX and INCOMING_INDEX.

Community detection clusters on explicit relationship edges.

See [Knowledge Graphs](04-knowledge-graphs.md) for how to define relationships in your domain model.

## Hierarchical Levels

Communities can nest at multiple levels:

```text
Level 0 (finest):
  Community A: {S1, S2, S3}
  Community B: {S4, S5}
  Community C: {S6, S7, S8, S9}

Level 1 (coarser):
  Community X: {A, B}      (merged A and B)
  Community Y: {C}         (C alone)

Level 2 (coarsest):
  Community Z: {X, Y}      (all entities)
```

### Configuring Levels

Control how many levels are computed via configuration. Default is 3 levels (0, 1, 2). Maximum is 10. More levels provide finer granularity options at query time but increase computation during detection.

### Selecting Levels at Query Time

When querying, specify which hierarchical level to use. Level 0 retrieves fine-grained communities, while higher levels (1, 2) return progressively coarser groupings.

Both local search (finding related entities) and global search (broad pattern queries) accept a level parameter to control community granularity.

### When to Use Each Level

| Level | Granularity | Use Case |
|-------|-------------|----------|
| 0 | Fine | Detailed analysis, small context windows |
| 1 | Medium | Balanced for most GraphRAG queries |
| 2+ | Coarse | High-level overview, large-scale patterns |

**Tip**: Start with level 1 for general queries. Drop to level 0 when you need specific details. Use level 2+ when asking about overall system patterns.

## Configuration

Community detection is controlled through clustering configuration. Key parameters fall into three categories:

### Basic Clustering

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | true | Enable/disable community detection |
| `detection_interval` | 30s | How often to run detection |
| `entity_change_threshold` | 100 | Trigger detection after N new entities |

### LPA Parameters

| Parameter | Default | Effect |
|-----------|---------|--------|
| `max_iterations` | 10 | More iterations = better convergence, slower |
| `levels` | 2 | Number of hierarchical levels to compute |
| `min_community_size` | 2 | Ignore communities smaller than this |

See the [Clustering Configuration](../advanced/01-clustering.md) guide for full configuration reference.

## Community Summaries

After detection, communities get summaries for GraphRAG. The summary tier depends on your configuration:

### Statistical Summary (Tier 1)

Computed without LLM, this summary includes:

- **Keywords**: TF-IDF extraction from entity content (e.g., "temperature", "sensor", "warehouse")
- **Representative entities**: PageRank-identified hub entities
- **Entity count**: Number of entities in the community
- **Status**: Indicates "statistical" summary type

This provides useful context without external dependencies.

### LLM-Enhanced Summary (Tier 2)

Builds on statistical summary by adding an LLM-generated narrative. The narrative describes the community's purpose, contents, and notable characteristics in natural language—ideal for GraphRAG prompts where human-readable context improves LLM responses.

## Tuning Guide

### "Communities are too large"

1. **Review explicit relationships**: Look for hub nodes with unusually high connectivity
2. **Increase min_community_size**: Filter small bridges

### "Communities are too small / fragmented"

1. **Check explicit relationships**: Are entities actually connected?

### "Detection is too slow"

1. **Reduce max_iterations** (10 → 5): Faster but less converged
2. **Increase detection_interval**: Run less frequently
3. **Raise entity_change_threshold**: Trigger less often

### "Communities don't match domain boundaries"

1. **Improve explicit relationships**: Add domain-meaningful triples
2. **Refine text content**: Make ContentFields return domain-specific text
3. **Consider: communities reveal structure you didn't see**

## When Detection Runs

Detection is triggered by:

1. **Time interval**: Every `detection_interval` (default: 30s)
2. **Entity threshold**: After `entity_change_threshold` new entities (default: 100)
3. **Manual trigger**: API call for immediate detection

After clustering, there's an optional **enhancement window** where LLM summaries are generated before the next detection cycle.

## Failure Modes

### Oscillation

Labels keep switching between iterations without converging.

**Cause**: Highly symmetric graph structure.
**Mitigation**: Limited by `max_iterations`.

### Monster Communities

One community absorbs most entities.

**Cause**: Dense hub nodes connecting everything.
**Mitigation**: Reduce hub over-connection in explicit edges.

### Singleton Explosion

Many single-entity communities.

**Cause**: Sparse explicit relationships.
**Mitigation**: Add more explicit triples, or accept that entities are genuinely disconnected.

## Related

**Concepts**
- [Knowledge Graphs](04-knowledge-graphs.md) - Defining relationships that create explicit edges
- [Embeddings](05-embeddings.md) - How embeddings enable semantic similarity search
- [Anomaly Detection](08-anomaly-detection.md) - K-core decomposition complements community clustering
- [GraphRAG Pattern](09-graphrag-pattern.md) - How communities enable retrieval-augmented generation
- [Similarity Metrics](06-similarity-metrics.md) - TF-IDF and threshold tuning

**Configuration**
- [Clustering Configuration](../advanced/01-clustering.md) - Full parameter reference
