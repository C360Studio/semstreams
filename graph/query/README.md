# Graph Query Library

A clean, read-only interface for querying graph data from NATS KV buckets. This package provides efficient graph traversal and entity queries with built-in caching.

## Overview

The query library separates read operations from write operations in the graph system:
- **GraphProcessor**: Writes entities and relationships to NATS KV
- **QueryClient**: Reads and traverses the graph from NATS KV

## Features

- **Direct NATS KV Access**: Reads from shared buckets without processor dependency
- **Built-in Caching**: LRU/TTL/Hybrid caching strategies for performance
- **Path Traversal**: Bounded graph traversal with resource limits
- **Bidirectional Queries**: Support for both outgoing and incoming edges
- **Batch Operations**: Efficient multi-entity retrieval
- **Spatial Queries**: Geohash-based entity location queries

## Usage

### Basic Setup

```go
import "github.com/c360/semstreams/pkg/graph/query"

// Create client with default config
client, err := query.NewClient(natsClient, query.DefaultConfig())
if err != nil {
    return err
}
defer client.Close()

// Or with custom config
config := &query.Config{
    EntityCache: cache.Config{
        Strategy: cache.StrategyHybrid,
        MaxSize:  1000,
        TTL:      5 * time.Minute,
    },
}
client, err := query.NewClient(natsClient, config)
```

### Entity Operations

```go
// Get single entity
entity, err := client.GetEntity(ctx, "drone_001")

// Get entities by type
drones, err := client.GetEntitiesByType(ctx, "robotics.drone")

// Batch retrieval
ids := []string{"drone_001", "drone_002", "drone_003"}
entities, err := client.GetEntitiesBatch(ctx, ids)

// Count all entities
count, err := client.CountEntities(ctx)
```

### Relationship Queries

```go
// Get outgoing edges
edges, err := client.GetOutgoingEdges(ctx, "drone_001")

// Get incoming edges (who points to this entity)
incoming, err := client.GetIncomingEdges(ctx, "drone_001")

// Get all connected entities
connections, err := client.GetEntityConnections(ctx, "drone_001")

// Verify specific relationship
exists, err := client.VerifyRelationship(ctx, "drone_001", "gcs_001", "CONTROLLED_BY")
```

### Path Traversal (PathRAG)

**Basic Path Query**:

```go
// Configure bounded traversal
query := query.PathQuery{
    StartEntity:  "drone_001",
    MaxDepth:     3,              // Maximum hops
    MaxNodes:     100,            // Maximum nodes to visit
    MaxTime:      100 * time.Millisecond,
    EdgeFilter:   []string{"NEAR", "MEMBER_OF"},
    DecayFactor:  0.8,            // Score decay per hop
    MaxPaths:     10,             // Limit result paths
}

// Execute traversal
result, err := client.ExecutePathQuery(ctx, query)

// Process results
for _, entity := range result.Entities {
    score := result.Scores[entity.Node.ID]
    fmt.Printf("Entity: %s, Score: %.2f\n", entity.Node.ID, score)
}

// Check if truncated
if result.Truncated {
    log.Println("Query hit resource limits")
}
```

### Complex Path Traversal Scenarios

**Scenario 1: Dependency Chain Analysis**

Find all services affected by a configuration change:

```go
query := query.PathQuery{
    StartEntity: "config.database.credentials",
    MaxDepth:    5,
    MaxNodes:    200,
    MaxTime:     500 * time.Millisecond,
    EdgeFilter:  []string{"depends_on", "calls"},
    DecayFactor: 0.9,  // Services are important even if distant
    MaxPaths:    50,
}

result, err := client.ExecutePathQuery(ctx, query)
if err != nil {
    return fmt.Errorf("dependency analysis failed: %w", err)
}

// Group by depth for cascade visualization
byDepth := make(map[int][]string)
for _, path := range result.Paths {
    depth := len(path) - 1
    if depth > 0 {
        affectedService := path[len(path)-1]
        byDepth[depth] = append(byDepth[depth], affectedService)
    }
}

// Display cascade
for depth := 1; depth <= query.MaxDepth; depth++ {
    if services, ok := byDepth[depth]; ok {
        fmt.Printf("Depth %d (%d services affected):\n", depth, len(services))
        for _, svc := range services {
            score := result.Scores[svc]
            fmt.Printf("  - %s (score: %.2f)\n", svc, score)
        }
    }
}
```

**Scenario 2: Spatial Mesh Network Discovery**

Find all drones reachable via mesh network from base station:

```go
query := query.PathQuery{
    StartEntity: "base.alpha",
    MaxDepth:    4,  // 4 hops max for mesh relay
    MaxNodes:    100,
    MaxTime:     300 * time.Millisecond,
    EdgeFilter:  []string{"near", "communicates", "relays_to"},
    DecayFactor: 0.7,  // Strong decay (signal strength)
    MaxPaths:    20,
}

result, err := client.ExecutePathQuery(ctx, query)

// Find weakest links (lowest scoring paths)
pathScores := make([]struct {
    path  []string
    score float64
}{}, 0, len(result.Paths))

for _, path := range result.Paths {
    // Path score = minimum entity score in path
    minScore := 1.0
    for _, entityID := range path {
        if score := result.Scores[entityID]; score < minScore {
            minScore = score
        }
    }
    pathScores = append(pathScores, struct {
        path  []string
        score float64
    }{path, minScore})
}

// Sort by score (ascending = weakest first)
sort.Slice(pathScores, func(i, j int) bool {
    return pathScores[i].score < pathScores[j].score
})

// Report weakest communication paths
fmt.Println("Weakest mesh links (need relay boost):")
for i := 0; i < 5 && i < len(pathScores); i++ {
    path := pathScores[i].path
    score := pathScores[i].score
    fmt.Printf("  %s (signal strength: %.1f%%)\n",
        strings.Join(path, " → "), score*100)
}
```

**Scenario 3: Incident Impact Radius**

Trace cascading effects of a system failure:

```go
query := query.PathQuery{
    StartEntity: "alert.network.outage.region-west",
    MaxDepth:    4,
    MaxNodes:    300,
    MaxTime:     1 * time.Second,
    EdgeFilter:  []string{"triggers", "affects", "depends_on"},
    DecayFactor: 0.85,
    MaxPaths:    100,
}

result, err := client.ExecutePathQuery(ctx, query)

// Classify impact by entity type
impactByType := make(map[string][]string)
for _, entity := range result.Entities {
    if entity.Node.ID == query.StartEntity {
        continue // Skip starting entity
    }
    impactByType[entity.Node.Type] = append(
        impactByType[entity.Node.Type],
        entity.Node.ID,
    )
}

// Report impact summary
fmt.Println("Incident Impact Analysis:")
for entityType, affected := range impactByType {
    fmt.Printf("  %s: %d affected\n", entityType, len(affected))

    // Show top 3 most impacted (highest scores = closest)
    type scored struct {
        id    string
        score float64
    }
    scoredEntities := make([]scored, 0, len(affected))
    for _, id := range affected {
        scoredEntities = append(scoredEntities, scored{id, result.Scores[id]})
    }
    sort.Slice(scoredEntities, func(i, j int) bool {
        return scoredEntities[i].score > scoredEntities[j].score
    })

    for i := 0; i < 3 && i < len(scoredEntities); i++ {
        fmt.Printf("    - %s (severity: %.2f)\n",
            scoredEntities[i].id, scoredEntities[i].score)
    }
}
```

**Scenario 4: Time-Bounded Discovery**

Find as much as possible in limited time (real-time constraint):

```go
query := query.PathQuery{
    StartEntity: "sensor.temp.critical",
    MaxDepth:    10,     // High depth
    MaxNodes:    10000,  // High limit
    MaxTime:     50 * time.Millisecond,  // PRIMARY constraint
    EdgeFilter:  nil,    // Explore all edges
    DecayFactor: 0.8,
    MaxPaths:    50,
}

result, err := client.ExecutePathQuery(ctx, query)

if result.Truncated {
    fmt.Printf("Discovery truncated after %dms\n",
        query.MaxTime.Milliseconds())
    fmt.Printf("Visited %d nodes (limit: %d)\n",
        len(result.Entities), query.MaxNodes)
}

// Even truncated results are useful for approximate context
fmt.Printf("Discovered %d related entities:\n", len(result.Entities))
```

**Scenario 5: Multi-Relationship Pattern**

Explore complex relationship patterns (e.g., "Find experts on this topic"):

```go
// Find entities with multiple relationship types
query := query.PathQuery{
    StartEntity: "topic.graph-rag",
    MaxDepth:    3,
    MaxNodes:    150,
    MaxTime:     200 * time.Millisecond,
    EdgeFilter:  []string{
        "authored_by",      // Documents authored
        "contributed_to",   // Code contributions
        "references",       // Citations
        "implements",       // Implementations
    },
    DecayFactor: 0.9,
    MaxPaths:    30,
}

result, err := client.ExecutePathQuery(ctx, query)

// Count relationship types per entity
entityRelCounts := make(map[string]map[string]int)
for _, entity := range result.Entities {
    if entity.Node.ID == query.StartEntity {
        continue
    }

    relCounts := make(map[string]int)
    for _, edge := range entity.Edges {
        // Check if edge type in filter
        for _, filterType := range query.EdgeFilter {
            if edge.Predicate == filterType {
                relCounts[filterType]++
            }
        }
    }

    if len(relCounts) > 0 {
        entityRelCounts[entity.Node.ID] = relCounts
    }
}

// Find entities with most diverse relationships (likely experts)
type expertise struct {
    id         string
    diversity  int
    totalRels  int
    score      float64
}

experts := make([]expertise, 0)
for id, relCounts := range entityRelCounts {
    total := 0
    for _, count := range relCounts {
        total += count
    }
    experts = append(experts, expertise{
        id:        id,
        diversity: len(relCounts),
        totalRels: total,
        score:     result.Scores[id],
    })
}

// Sort by diversity first, then total relationships
sort.Slice(experts, func(i, j int) bool {
    if experts[i].diversity != experts[j].diversity {
        return experts[i].diversity > experts[j].diversity
    }
    return experts[i].totalRels > experts[j].totalRels
})

fmt.Println("Top experts on topic:")
for i := 0; i < 5 && i < len(experts); i++ {
    e := experts[i]
    fmt.Printf("  %s: %d rel types, %d total (score: %.2f)\n",
        e.id, e.diversity, e.totalRels, e.score)
}
```

**Scenario 6: EdgeFilter Tuning**

Compare results with different edge filters:

```go
baseQuery := query.PathQuery{
    StartEntity: "alert.security.breach",
    MaxDepth:    3,
    MaxNodes:    100,
    MaxTime:     200 * time.Millisecond,
    DecayFactor: 0.85,
}

// Try different filter strategies
filters := map[string][]string{
    "all":        nil,
    "direct":     []string{"triggers", "causes"},
    "indirect":   []string{"related_to", "similar_to"},
    "structural": []string{"depends_on", "contains"},
}

for name, filter := range filters {
    baseQuery.EdgeFilter = filter
    result, err := client.ExecutePathQuery(ctx, &baseQuery)
    if err != nil {
        continue
    }

    fmt.Printf("Filter '%s': %d entities, %d paths\n",
        name, len(result.Entities), len(result.Paths))
}
```

**Scenario 7: DecayFactor Comparison**

Tune relevance decay for use case:

```go
decayFactors := []float64{0.6, 0.75, 0.85, 0.95, 1.0}

for _, decay := range decayFactors {
    query := query.PathQuery{
        StartEntity: "service.api",
        MaxDepth:    4,
        MaxNodes:    150,
        EdgeFilter:  []string{"calls"},
        DecayFactor: decay,
    }

    result, _ := client.ExecutePathQuery(ctx, &query)

    // Calculate average score at each depth
    depthScores := make(map[int][]float64)
    for _, path := range result.Paths {
        for i, entityID := range path {
            depthScores[i] = append(depthScores[i], result.Scores[entityID])
        }
    }

    fmt.Printf("DecayFactor %.2f:\n", decay)
    for depth := 0; depth <= query.MaxDepth; depth++ {
        if scores, ok := depthScores[depth]; ok && len(scores) > 0 {
            avg := 0.0
            for _, s := range scores {
                avg += s
            }
            avg /= float64(len(scores))
            fmt.Printf("  Depth %d: avg score %.2f\n", depth, avg)
        }
    }
}
```

See [PathRAG Documentation](../docs/features/PATHRAG.md) for comprehensive guide.

### Advanced Queries

```go
// Query with criteria
criteria := map[string]any{
    "type":     "robotics.drone",
    "armed":    true,
    "battery":  75.0,
}
entities, err := client.QueryEntities(ctx, criteria)

// Spatial queries
geohash := "gbsuv7"  // San Francisco area
nearbyEntities, err := client.GetEntitiesInRegion(ctx, geohash)
```

### Cache Management

```go
// Get cache statistics
stats := client.GetCacheStats()
fmt.Printf("Cache hit rate: %.2f%% (%d hits, %d misses)\n", 
    stats.HitRate*100, stats.Hits, stats.Misses)

// Clear cache
err := client.Clear()
```

## Component Integration

The query library is used by components that need to read graph data:

```go
// In ContextProcessor
func NewFactory(natsClient *natsclient.Client, objectStore *objectstore.Store) component.Factory {
    return func(ctx context.Context, config map[string]any) (component.Discoverable, error) {
        // Create query client internally
        queryClient, err := query.NewClient(natsClient, query.DefaultConfig())
        if err != nil {
            return nil, fmt.Errorf("failed to create query client: %w", err)
        }
        
        return NewProcessor(natsClient, objectStore, queryClient, &config)
    }
}
```

## Configuration

### Default Configuration

```go
config := query.DefaultConfig()
// Returns:
// - EntityCache: Hybrid strategy, 1000 items, 5min TTL
// - EntityStates bucket: 24h TTL, 3 history, 1 replica
// - SpatialIndex bucket: 1h TTL, 1 history, 1 replica  
// - IncomingIndex bucket: 24h TTL, 1 history, 1 replica
```

### Custom Configuration

```go
config := &query.Config{
    EntityCache: cache.Config{
        Strategy:        cache.StrategyLRU,
        MaxSize:        5000,
        TTL:            10 * time.Minute,
        CleanupInterval: 1 * time.Minute,
        EnableStats:    true,
    },
    EntityStates: struct {
        TTL      time.Duration
        History  uint8
        Replicas int
    }{
        TTL:      7 * 24 * time.Hour,  // 1 week
        History:  5,
        Replicas: 3,
    },
}
```

## Performance Considerations

1. **Caching**: The hybrid cache strategy provides optimal performance for most use cases
2. **Batch Operations**: Use `GetEntitiesBatch()` for multiple entities
3. **Path Limits**: Always set reasonable limits on `PathQuery` to prevent runaway traversals
4. **Context Timeouts**: Use context with timeout for all operations

## Testing

```go
// Create mock client for testing
type mockClient struct {
    entities map[string]*gtypes.EntityState
}

func (m *mockClient) GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error) {
    entity, exists := m.entities[id]
    if !exists {
        return nil, fmt.Errorf("entity not found")
    }
    return entity, nil
}

// Use in tests
mock := &mockClient{
    entities: map[string]*gtypes.EntityState{
        "test_001": testEntity,
    },
}
```

## Thread Safety

All Client methods are thread-safe and can be called concurrently. The implementation uses:
- Atomic operations for statistics
- Mutex protection for bucket initialization
- Thread-safe cache implementation

## Error Handling

The library returns descriptive errors:
- Entity not found: Returns nil entity, no error
- Network issues: Wrapped errors with context
- Invalid parameters: Validation errors with details
- Resource limits: Sets `Truncated` flag in results

## Best Practices

1. **Reuse Clients**: Create one client and share across goroutines
2. **Set Timeouts**: Always use context with timeout for queries
3. **Monitor Cache**: Check cache stats periodically for tuning
4. **Validate Parameters**: Check query parameters before execution
5. **Handle Truncation**: Check `Truncated` flag in path results