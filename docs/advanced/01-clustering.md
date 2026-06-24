# Community Detection and Clustering

SemStreams detects communities of related entities using graph clustering algorithms. Communities enable semantic search, relationship inference, and context-aware summarization.

## Overview

Community detection groups entities that are more connected to each other than to the rest of the graph. SemStreams uses Label Propagation Algorithm (LPA) for detection and PageRank for identifying representative entities within each community.

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Community Detection Pipeline                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Entity Changes (MessageManager → DataManager)                      │
│         │                                                           │
│         ▼                                                           │
│  Entity Count Threshold Reached                                     │
│         │                                                           │
│         ▼                                                           │
│  DetectCommunities() Called                                         │
│         │                                                           │
│         ├── EnhancementWorker.Pause()                              │
│         │                                                           │
│         ▼                                                           │
│  LPA Algorithm Runs                                                 │
│         │                                                           │
│         ├── For each level (0 → levels):                           │
│         │     ├── Initialize labels (each entity = own label)      │
│         │     ├── Iterate until convergence/max_iterations         │
│         │     │     └── Each entity adopts dominant neighbor label │
│         │     └── Group entities by label → Communities            │
│         │                                                           │
│         ▼                                                           │
│  Summary Preservation (Jaccard matching)                            │
│         │                                                           │
│         ▼                                                           │
│  Statistical Summarization (immediate)                              │
│         │                                                           │
│         ▼                                                           │
│  Save to COMMUNITY_INDEX KV                                         │
│         │                                                           │
│         ├── EnhancementWorker.Resume()                             │
│         │                                                           │
│         ▼                                                           │
│  EnhancementWorker Picks Up (async LLM)                            │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Label Propagation Algorithm

LPA detects communities by iteratively propagating labels through the graph:

1. **Initialization**: Each entity starts with its own unique label
2. **Propagation**: Each entity adopts the most common label among its neighbors
3. **Convergence**: Repeat until labels stabilize or max iterations reached
4. **Grouping**: Entities with the same label form a community

### Algorithm Properties

| Property | Characteristic |
|----------|---------------|
| Time Complexity | O(k * m) where k = iterations, m = edges |
| Space Complexity | O(n) where n = entities |
| Parallelizable | Yes, with atomic label updates |
| Deterministic | No, tie-breaking affects results |

LPA is fast and scales well to large graphs. The non-deterministic nature means results may vary slightly between runs, but the overall community structure remains stable.

### Configuration

```json
{
  "clustering": {
    "algorithm": {
      "max_iterations": 100,
      "levels": 3
    }
  }
}
```

| Option | Default | Description |
|--------|---------|-------------|
| `max_iterations` | 100 | Maximum LPA iterations per level |
| `levels` | 3 | Hierarchical community levels |

## Hierarchical Levels

Communities are organized hierarchically:

- **Level 0**: Finest granularity, smallest communities
- **Level 1**: Aggregated from Level 0
- **Level 2**: Highest level, largest communities

Higher levels are created by treating Level 0 communities as nodes and running LPA again. This creates a hierarchy useful for different query contexts:

```
Level 2:  [       Super-Community A        ]   [  Super-B  ]
             /            |           \            |
Level 1:  [Comm-1]    [Comm-2]    [Comm-3]    [Comm-4]
           /   \        |           /  \          |
Level 0: [a][b] [c]    [d]       [e]  [f]       [g][h]
```

### Level Selection Guidelines

| Query Type | Recommended Level |
|------------|-------------------|
| Specific entity context | Level 0 |
| General topic grouping | Level 1 |
| Broad organizational view | Level 2 |

## PageRank

PageRank identifies "important" entities based on incoming edge counts. Within each community, PageRank selects representative entities that best exemplify the community's characteristics.

### Representative Entity Selection

```go
// Simplified PageRank for community representatives
func (c *Community) selectRepresentatives(count int) []string {
    // 1. Calculate PageRank scores for all members
    // 2. Sort by score descending
    // 3. Return top N entities
}
```

Representatives are used for:
- Community summarization (sample entities for LLM context)
- Query result ranking
- Graph visualization

## Community Structure

```go
type Community struct {
    ID                 string              // Unique identifier: "comm-{level}-{hash}"
    Level              int                 // Hierarchy level (0=bottom)
    Members            []string            // Entity IDs in this community
    ParentID           *string             // Parent community at next level
    StatisticalSummary string              // Fast baseline summary
    LLMSummary         string              // Enhanced LLM description
    Keywords           []string            // Key terms from TF-IDF
    RepEntities        []string            // Representative entity IDs
    SummaryStatus      string              // Summary generation status
    Metadata           map[string]interface{}
}
```

### Summary Status Values

| Status | Meaning |
|--------|---------|
| `statistical` | Initial summary from TF-IDF, awaiting LLM |
| `llm-enhanced` | LLM narrative complete |
| `llm-failed` | LLM enhancement failed |
| `statistical-fallback` | LLM disabled, using statistical |

## Statistical Summarization

Immediate summarization using TF-IDF keyword extraction:

### TF-IDF Keywords

Term Frequency-Inverse Document Frequency identifies distinctive terms across entity content:

1. **Term Frequency (TF)**: How often a term appears in entity content
2. **Inverse Document Frequency (IDF)**: Rarity of term across all entities
3. **TF-IDF Score**: TF * IDF prioritizes common-within but rare-across terms

### Summary Generation

```go
// Statistical summarization (instant, no LLM)
func (s *StatisticalSummarizer) Summarize(ctx context.Context, community *Community) string {
    // 1. Extract text from member entities
    // 2. Calculate TF-IDF scores
    // 3. Select top keywords
    // 4. Generate template-based summary

    return fmt.Sprintf(
        "Community of %d entities focused on %s. Key topics: %s. "+
        "Representative entities: %s.",
        len(community.Members),
        community.DominantDomain(),
        strings.Join(community.Keywords, ", "),
        strings.Join(community.RepEntities[:3], ", "),
    )
}
```

## Summary Preservation

When communities evolve (members join/leave), summaries are preserved via Jaccard similarity:

```
Jaccard Index = |intersection| / |union|
```

### Preservation Rules

| Jaccard Score | Action |
|---------------|--------|
| >= 0.8 | Copy existing summary to new community |
| < 0.8 | Generate fresh summary |

This prevents re-running expensive LLM calls when community membership changes slightly.

### Example

```
Old Community A: [entity-1, entity-2, entity-3, entity-4]
New Community B: [entity-1, entity-2, entity-3, entity-5]

Intersection: [entity-1, entity-2, entity-3] = 3
Union: [entity-1, entity-2, entity-3, entity-4, entity-5] = 5

Jaccard = 3/5 = 0.6

0.6 < 0.8 → Generate new summary
```

## Storage

Communities are stored in the `COMMUNITY_INDEX` KV bucket:

### Key Patterns

| Pattern | Purpose |
|---------|---------|
| `graph.community.{level}.{id}` | Community record |
| `graph.community.entity.{level}.{entity_id}` | Entity-to-community mapping |

### NATS Commands

```bash
# Get community
nats kv get COMMUNITY_INDEX "graph.community.0.comm-0-A1"

# Find entity's community
nats kv get COMMUNITY_INDEX "graph.community.entity.0.drone-007"

# List all level-0 communities
nats kv ls COMMUNITY_INDEX | grep "graph.community.0."
```

## Graph Providers

Clustering requires a GraphProvider interface for graph traversal:

```go
type GraphProvider interface {
    GetAllEntityIDs(ctx context.Context) ([]string, error)
    GetNeighbors(ctx context.Context, entityID, direction string) ([]string, error)
    GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error)
}
```

### Provider Implementations

#### PredicateGraphProvider

Clusters entities matching a specific predicate (recommended):

```go
// Cluster only drone entities
provider := clustering.NewPredicateGraphProvider(queryManager, "entity.type=drone")
```

Caches the entity set for efficient repeated queries.

## Detection Scheduling

Detection runs are triggered by:

1. **Time-based**: Every `detection_interval` (default: 30s)
2. **Change-based**: After `entity_change_threshold` entity updates (default: 100)

### Configuration

```json
{
  "schedule": {
    "initial_delay": "10s",
    "detection_interval": "30s",
    "min_detection_interval": "5s",
    "entity_change_threshold": 100,
    "min_entities": 10,
    "min_embedding_coverage": 0.5
  }
}
```

| Option | Default | Description |
|--------|---------|-------------|
| `initial_delay` | 10s | Wait before first detection |
| `detection_interval` | 30s | Maximum time between runs |
| `min_detection_interval` | 5s | Burst protection |
| `entity_change_threshold` | 100 | Trigger after N changes |
| `min_entities` | 10 | Minimum for detection |
| `min_embedding_coverage` | 0.5 | Required embedding ratio |

## Enhancement Window

The enhancement window pauses detection to allow LLM enhancement to complete:

```json
{
  "schedule": {
    "enhancement_window": "120s",
    "enhancement_window_mode": "blocking"
  }
}
```

### Window Modes

| Mode | Behavior |
|------|----------|
| `blocking` | Hard pause until window expires or all communities complete |
| `soft` | Allow detection if changes exceed threshold during window |
| `none` | No window, detection continues immediately |

### Pause/Resume Coordination

```go
// In processor.go DetectCommunities():
if p.enhancementWorker != nil {
    p.enhancementWorker.Pause()
    defer p.enhancementWorker.Resume()
}
```

This ensures:
1. No concurrent writes to COMMUNITY_INDEX during detection
2. In-flight LLM work completes before pause
3. Worker resumes automatically after detection

## Inference

Community-based inference creates implicit relationships:

```json
{
  "inference": {
    "enabled": true,
    "min_community_size": 2,
    "max_inferred_per_community": 50
  }
}
```

When enabled, entities in the same community receive inferred `community.co_member` triples connecting them. This creates explicit edges where only semantic similarity existed.

### Limits

- `min_community_size`: Skip singleton communities
- `max_inferred_per_community`: Prevent O(n^2) explosion in large communities

## Metrics

Community detection exposes Prometheus metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `graph_processor_communities_detected_total` | Counter | Communities detected |
| `graph_processor_detection_duration_seconds` | Histogram | Detection run latency |
| `graph_processor_enhancement_queue_depth` | Gauge | Pending LLM enhancements |

## Best Practices

### Tuning Detection Frequency

- **High update rate**: Increase `entity_change_threshold`
- **Real-time needs**: Decrease `detection_interval`
- **LLM cost concerns**: Increase `min_detection_interval`

### Optimizing Cluster Quality

- **More levels**: Better hierarchy, more compute
- **Higher iterations**: Better convergence, longer detection

### Production Recommendations

1. Start with statistical-only (no LLM) to validate clustering
2. Enable LLM enhancement after confirming cluster quality
3. Use `soft` enhancement window for high-update environments
4. Monitor `enhancement_queue_depth` for LLM backlog

## Related Documentation

- [LLM Enhancement](02-llm-enhancement.md) - LLM integration details
- [Performance Tuning](03-performance.md) - Optimization strategies
- [Community Detection](../concepts/07-community-detection.md) - Community overview

### Background Concepts

For foundational understanding of the algorithms and metrics used here:

- [Community Detection Concepts](../concepts/07-community-detection.md) - LPA intuition, failure modes, tuning guidance
- [Embeddings](../concepts/05-embeddings.md) - What vectors are, cosine similarity explained
- [Similarity Metrics](../concepts/06-similarity-metrics.md) - TF-IDF, Jaccard, threshold tuning
