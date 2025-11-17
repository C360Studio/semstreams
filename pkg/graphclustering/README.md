# Graph Clustering Package

## Overview

The `graphclustering` package implements community detection for knowledge graphs using the **Label Propagation Algorithm (LPA)**. This enables GraphRAG capabilities by identifying semantically related entity clusters for improved query performance and semantic search.

## Architecture

### Components

```
┌──────────────────────┐
│  CommunityDetector   │  ← Interface for community detection
└──────────────────────┘
          ▲
          │ implements
          │
┌──────────────────────┐
│    LPADetector       │  ← Label Propagation Algorithm
└──────────────────────┘
          │
          ├─────────────┐─────────────┐─────────────┐
          ▼             ▼             ▼             ▼
  ┌─────────────┐ ┌──────────┐ ┌─────────────┐ ┌────────────────┐
  │GraphProvider│ │Community │ │   Storage   │ │  Summarizers   │
  └─────────────┘ └──────────┘ └─────────────┘ └────────────────┘
                                                         │
                    ┌────────────────────────────────────┤
                    ▼                                    ▼
          ┌──────────────────┐              ┌──────────────────────┐
          │ProgressiveSummarizer │          │ EnhancementWorker    │
          │ (Statistical)        │          │ (Async LLM)          │
          └──────────────────┘              └──────────────────────┘
```

### Progressive Summarization Flow

```
Community Detection → Statistical Summary (immediate)
                              │
                              ├─ Save to COMMUNITY_INDEX with status="statistical"
                              │
                              ▼
                    [KV Bucket: COMMUNITY_INDEX]
                              │
                              ▼ (KV Watch detects change)
                    EnhancementWorker (async)
                              │
                              ├─ Fetch entities
                              ├─ Generate LLM summary
                              ├─ Preserve statistical summary
                              └─ Update with status="llm-enhanced"
```

### Interfaces

- **CommunityDetector**: Main interface for community detection operations
- **GraphProvider**: Abstracts graph data source (QueryManager, etc.)
- **CommunityStorage**: Persistence layer (NATS KV)
- **CommunitySummarizer**: Interface for generating community summaries
- **EntityProvider**: Interface for fetching full entity states

## Usage

### Basic Community Detection

```go
import (
    "context"
    "github.com/c360/semstreams/pkg/graphclustering"
    "github.com/c360/semstreams/processor/graph/querymanager"
)

// Create graph provider (using QueryManager)
provider := graphclustering.NewPredicateGraphProvider(queryMgr, "document.technical")

// Create storage (using NATS KV)
storage := graphclustering.NewNATSCommunityStorage(communityKV)

// Create detector
detector := graphclustering.NewLPADetector(provider, storage)

// Configure (optional)
detector.WithMaxIterations(100).WithLevels(3)

// Detect communities
communities, err := detector.DetectCommunities(ctx)
if err != nil {
    return err
}

// Access hierarchical levels
level0 := communities[0]  // Fine-grained communities
level1 := communities[1]  // Mid-level aggregation
level2 := communities[2]  // Top-level clusters
```

### Progressive Summarization (Statistical + LLM)

Enable progressive summarization for immediate statistical summaries with async LLM enhancement:

```go
import (
    "context"
    "github.com/c360/semstreams/pkg/graphclustering"
)

// 1. Create progressive summarizer (statistical)
progressiveSummarizer := graphclustering.NewProgressiveSummarizer()

// 2. Create LLM summarizer (for async enhancement)
llmSummarizer := graphclustering.NewHTTPLLMSummarizer("http://localhost:8084")

// 3. Start enhancement worker with KV watch
enhancementWorker, err := graphclustering.NewEnhancementWorker(
    &graphclustering.EnhancementWorkerConfig{
        LLMSummarizer:   llmSummarizer,
        Storage:         communityStorage,
        GraphProvider:   graphProvider,
        Querier:         queryManager,       // For fetching entities
        CommunityBucket: communityBucket,    // KV bucket for watching
    })
if err != nil {
    return err
}

// Start worker in background (watches COMMUNITY_INDEX via KV watcher)
workerCtx, cancel := context.WithCancel(ctx)
defer cancel()
go func() {
    _ = enhancementWorker.Start(workerCtx)
}()

// 4. Configure detector with progressive summarization
detector.WithProgressiveSummarization(
    progressiveSummarizer,
    queryManager, // Implements EntityProvider
)

// 5. Run detection - statistical summaries available immediately!
communities, err := detector.DetectCommunities(ctx)

// Access immediate statistical summary
for _, comm := range communities[0] {
    fmt.Printf("Statistical: %s\n", comm.StatisticalSummary)
    fmt.Printf("Status: %s\n", comm.SummaryStatus) // "statistical"
    // LLMSummary is empty initially, populated asynchronously
}

// 6. Wait for async LLM enhancement (optional)
time.Sleep(5 * time.Second)
enhancedComm, _ := communityStorage.GetCommunity(ctx, communityID)
fmt.Printf("LLM Enhanced: %s\n", enhancedComm.LLMSummary)
fmt.Printf("Status: %s\n", enhancedComm.SummaryStatus) // "llm-enhanced"
```

### Community Summary Structure

Communities have dual summaries:

```go
type Community struct {
    ID      string
    Level   int
    Members []string

    // Dual summaries
    StatisticalSummary string // Always present (TF-IDF + templates)
    LLMSummary         string // Populated async by EnhancementWorker
    SummaryStatus      string // "statistical" | "llm-enhanced" | "llm-failed"

    // Supporting fields
    Keywords    []string // Extracted keywords
    RepEntities []string // Representative entity IDs

    // Backward compatibility (deprecated)
    Summary    string // Returns StatisticalSummary or LLMSummary
    Summarizer string // "statistical" or "llm"
}
```

### Query Entity Community

```go
// Find which community an entity belongs to
community, err := detector.GetEntityCommunity(ctx, entityID, level)
if err != nil {
    return err
}

fmt.Printf("Entity %s is in community %s with %d members\n",
    entityID, community.ID, len(community.Members))
```

### Incremental Updates

```go
// Update communities after graph changes
changedEntities := []string{"entity1", "entity2", "entity3"}
err := detector.UpdateCommunities(ctx, changedEntities)
```

## Label Propagation Algorithm

### How It Works

1. **Initialization**: Each node starts with a unique label (its own ID)
2. **Iteration**: Nodes adopt the most common label among their neighbors
3. **Convergence**: Process repeats until labels stabilize
4. **Hierarchical Clustering**: Re-run algorithm on communities to create levels

### Algorithm Properties

- **Time Complexity**: O(m + n) per iteration, where m = edges, n = nodes
- **Typical Convergence**: 10-20 iterations for most graphs
- **Max Iterations**: 100 (configurable)
- **Determinism**: Randomized node processing order reduces oscillation

### Performance Characteristics

Benchmark results (Apple M3 Pro):

```
BenchmarkLPADetector_SmallGraph-12          48,750 ns/op    (10 nodes)
BenchmarkLPADetector_MediumGraph-12        ~500 µs/op       (50 nodes)
BenchmarkLPADetector_LargeGraph-12         ~5 ms/op         (200 nodes)
BenchmarkLPADetector_GetEntityCommunity    ~100 ns/op       (lookup)
```

## GraphRAG Integration

### Search Modes

Communities enable three GraphRAG search strategies:

1. **Local Search**: Query entities within same community
   ```go
   community, _ := detector.GetEntityCommunity(ctx, seedEntity, 0)
   // Search only community.Members
   ```

2. **Global Search**: Query across top-level communities
   ```go
   topCommunities, _ := detector.GetCommunitiesByLevel(ctx, 2)
   // Search representatives from each top community
   ```

3. **DRIFT Search**: Dynamic expansion from local to global
   ```go
   // Start with local community
   // Expand to neighboring communities if needed
   // Continue until sufficient results
   ```

## Representative Entity Selection

Communities include representative entities - the most important nodes that best exemplify the community's characteristics. These are computed using **PageRank** algorithm for graph centrality.

### PageRank for RepEntities

Representative entities are identified using the PageRank algorithm, which measures structural importance within the community graph:

```go
// Automatically computed by StatisticalSummarizer
summarizer := graphclustering.NewStatisticalSummarizer()
summarizer.MaxRepEntities = 5

community, err := summarizer.SummarizeCommunity(ctx, comm, entities)
fmt.Printf("Top representatives: %v\n", community.RepEntities)
```

**How it works**:
1. PageRank algorithm runs on the community subgraph
2. Entities with high incoming edge counts rank higher
3. Top N entities by PageRank score become representatives
4. Fallback to degree centrality for very small communities (< 3 members)

**Example**: In a backend services community:
- API Gateway (many services point to it) → High PageRank
- Authentication Service (many services depend on it) → High PageRank
- Internal utility service (few dependencies) → Low PageRank

**Why PageRank vs Degree Centrality**:
- **PageRank**: Graph-native importance (entities that are referenced by many others)
- **Degree Centrality**: Simple edge count (doesn't distinguish direction or importance)
- **Result**: PageRank identifies true "hub" entities, not just highly-connected ones

**Configuration**:
```go
summarizer.MaxRepEntities = 5 // Top 5 representatives (default)
```

**Performance**: ~1ms for communities up to 100 entities

### Keywords Extraction (TF-IDF)

While representative entities use **PageRank**, keywords use **TF-IDF** (Term Frequency-Inverse Document Frequency):

```go
// Also computed by StatisticalSummarizer
community, err := summarizer.SummarizeCommunity(ctx, comm, entities)
fmt.Printf("Key themes: %v\n", community.Keywords)
```

**Complementary Approaches**:
- **RepEntities (PageRank)**: Structural importance in graph
- **Keywords (TF-IDF)**: Content-based importance from entity properties

**Example Community Summary**:
```json
{
  "id": "comm-0-auth",
  "repEntities": ["auth-service", "user-db", "jwt-validator"],  // PageRank
  "keywords": ["authentication", "jwt", "oauth", "session"],    // TF-IDF
  "summary": "Authentication and authorization services..."
}
```

## Community Summarization

### Summarizer Types

Three summarizers are available:

#### 1. StatisticalSummarizer (Fast Baseline)

Generates summaries using TF-IDF keyword extraction and templates:

```go
summarizer := graphclustering.NewStatisticalSummarizer()
community, err := summarizer.SummarizeCommunity(ctx, comm, entities)
```

**Output Example**:
```
"Community of 3 entities including 1 drone, 1 sensor, 1 controller.
Key themes: autonomous, navigation, robotics, sensor, control."
```

**Performance**: ~1ms per community
**Use case**: Immediate results, always available

#### 2. HTTPLLMSummarizer (High Quality)

Generates natural language summaries via LLM service (semsummarize):

```go
summarizer := graphclustering.NewHTTPLLMSummarizer("http://localhost:8084")
community, err := summarizer.SummarizeCommunity(ctx, comm, entities)
```

**Output Example**:
```
"This community represents an autonomous drone delivery system with
integrated LiDAR sensing and real-time flight control capabilities."
```

**Performance**: ~3s per community (model inference)
**Use case**: High-quality summaries for user-facing applications
**Fallback**: Returns statistical summary if LLM unavailable

#### 3. ProgressiveSummarizer (Recommended)

Combines both approaches for progressive enhancement:

```go
summarizer := graphclustering.NewProgressiveSummarizer()
community, err := summarizer.SummarizeCommunity(ctx, comm, entities)
// Returns statistical summary immediately
// LLM enhancement happens asynchronously via EnhancementWorker
```

**Flow**:
1. Returns statistical summary immediately (< 1ms)
2. Saves community to COMMUNITY_INDEX with status="statistical"
3. EnhancementWorker detects via KV watch
4. Worker generates LLM summary asynchronously
5. Community updated with status="llm-enhanced" and both summaries preserved

**Use case**: Production systems requiring immediate response + quality

### Storage Schema

Communities stored in NATS KV bucket `COMMUNITY_INDEX`:

```
Keys:
  graph.community.{level}.{id}              → Community data (JSON)
  graph.community.entity.{level}.{entityID} → Entity → Community mapping

Example:
  graph.community.0.comm-0-A1               → {"id": "comm-0-A1", "level": 0, "members": [...]}
  graph.community.entity.0.doc123           → "comm-0-A1"
```

## Implementation Details

### Hierarchical Levels

- **Level 0** (Bottom): Fine-grained communities (tight clusters)
- **Level 1** (Mid): Intermediate aggregation
- **Level 2** (Top): High-level organization

### GraphProvider Implementations

#### PredicateGraphProvider

Clusters entities matching a specific predicate/type:

```go
provider := graphclustering.NewPredicateGraphProvider(queryMgr, "document.technical")
```

**Use case**: Cluster documents, specifications, or domain-specific entities

#### QueryManagerGraphProvider

Full graph access (requires GetAllEntityIDs implementation):

```go
provider := graphclustering.NewQueryManagerGraphProvider(queryMgr)
```

**Note**: Currently returns error - use PredicateGraphProvider instead

### Edge Weights

- **Current**: Unweighted (all edges = 1.0)
- **Future**: Extract weights from relationship properties
- **Impact**: Weighted edges improve community quality

## Testing

### Unit Tests

```bash
go test ./pkg/graphclustering -v
```

Tests cover:
- Simple graphs (2 communities)
- Isolated nodes
- Fully connected graphs
- Hierarchical levels
- Entity community lookup
- Empty graphs

### Benchmarks

```bash
go test ./pkg/graphclustering -bench=. -benchmem
```

Benchmarks:
- Small graph (10 nodes)
- Medium graph (50 nodes)
- Large graph (200 nodes)
- Entity lookup (single)
- Entity lookup (parallel)
- Incremental updates

## Future Enhancements

### Phase 1 (MVP) ✅

- [x] Label Propagation Algorithm
- [x] Hierarchical clustering (3 levels)
- [x] NATS KV storage
- [x] QueryManager integration
- [x] Unit tests and benchmarks
- [x] **Progressive community summarization**
  - [x] Statistical summarizer (TF-IDF + templates)
  - [x] LLM enhancement worker (async via NATS)
  - [x] Dual summary support (StatisticalSummary + LLMSummary)
  - [x] Graceful degradation (works without LLM service)
- [x] **PageRank for representative entities**
  - [x] Graph centrality-based entity selection
  - [x] Replaces simple degree counting
  - [x] Fallback to degree centrality for small communities
  - [x] Complementary to TF-IDF keywords

### Phase 2 (Optimization)

- [ ] Incremental updates (local propagation only)
- [ ] Weighted edges (extract from relationship properties)
- [ ] Parallel LPA (concurrent label updates)
- [ ] Community quality metrics (modularity score)
- [ ] Summary quality metrics (coherence, coverage)

### Phase 3 (Advanced)

- [ ] Leiden algorithm (if permissive Go library becomes available)
- [ ] Overlapping communities (nodes in multiple communities)
- [ ] Temporal communities (track evolution over time)
- [ ] Multi-modal summarization (combine entity types, relationships, metadata)

## References

- **Label Propagation Algorithm**: Raghavan, Albert, and Kumara (2007)
- **GraphRAG**: Microsoft Research (2024)
- **Community Detection Survey**: Fortunato and Hric (2016)

## License

Part of the SemStreams framework. See repository LICENSE for details.
