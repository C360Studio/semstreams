package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	gtypes "github.com/c360/semstreams/graph"
)

// BenchmarkPathQuery_SmallGraph benchmarks path queries on a small graph (100 entities)
func BenchmarkPathQuery_SmallGraph_Depth2(b *testing.B) {
	benchmarkPathQuery(b, 100, 2, 50)
}

func BenchmarkPathQuery_SmallGraph_Depth3(b *testing.B) {
	benchmarkPathQuery(b, 100, 3, 100)
}

// BenchmarkPathQuery_MediumGraph benchmarks path queries on a medium graph (1000 entities)
func BenchmarkPathQuery_MediumGraph_Depth2(b *testing.B) {
	benchmarkPathQuery(b, 1000, 2, 100)
}

func BenchmarkPathQuery_MediumGraph_Depth3(b *testing.B) {
	benchmarkPathQuery(b, 1000, 3, 200)
}

// BenchmarkPathQuery_LargeGraph benchmarks path queries on a large graph (10000 entities)
func BenchmarkPathQuery_LargeGraph_Depth2(b *testing.B) {
	benchmarkPathQuery(b, 10000, 2, 150)
}

func BenchmarkPathQuery_LargeGraph_Depth3(b *testing.B) {
	benchmarkPathQuery(b, 10000, 3, 500)
}

// benchmarkPathQuery is a helper function that creates a graph and benchmarks PathQuery execution
func benchmarkPathQuery(b *testing.B, entityCount, maxDepth, maxNodes int) {
	// Skip if INTEGRATION_TESTS not set (requires NATS)
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// TODO: Once PathQuery is fully integrated with test infrastructure:
	// 1. Create a NATS test client
	// 2. Generate a synthetic graph with entityCount entities
	// 3. Connect entities with relationships (e.g., create a mesh or tree structure)
	// 4. Execute PathQuery benchmarks

	// For now, document the benchmark structure and expected usage
	b.Logf("Benchmark: PathQuery with %d entities, depth %d, maxNodes %d", entityCount, maxDepth, maxNodes)

	// Placeholder benchmark structure
	ctx := context.Background()
	query := PathQuery{
		StartEntity: "entity-001",
		MaxDepth:    maxDepth,
		MaxNodes:    maxNodes,
		MaxTime:     1 * time.Second,
		EdgeFilter:  []string{"related_to", "near"},
		DecayFactor: 0.8,
		MaxPaths:    20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// TODO: Execute actual PathQuery once integrated
		_ = query
		_ = ctx
		// result, err := client.ExecutePathQuery(ctx, &query)
		// if err != nil {
		//     b.Fatal(err)
		// }
	}
}

// Example benchmark output format for documentation:
// BenchmarkPathQuery_SmallGraph_Depth2-8     1000  1234567 ns/op  (1.2ms)
// BenchmarkPathQuery_SmallGraph_Depth3-8      500  2345678 ns/op  (2.3ms)
// BenchmarkPathQuery_MediumGraph_Depth2-8     800  1567890 ns/op  (1.5ms)
// BenchmarkPathQuery_MediumGraph_Depth3-8     300  3456789 ns/op  (3.4ms)
// BenchmarkPathQuery_LargeGraph_Depth2-8      600  2123456 ns/op  (2.1ms)
// BenchmarkPathQuery_LargeGraph_Depth3-8      100  6789012 ns/op  (6.7ms)

// BenchmarkPathQuery_EdgeFilterEffectiveness measures impact of edge filtering
func BenchmarkPathQuery_EdgeFilter_None(b *testing.B) {
	benchmarkWithEdgeFilter(b, nil)
}

func BenchmarkPathQuery_EdgeFilter_Single(b *testing.B) {
	benchmarkWithEdgeFilter(b, []string{"related_to"})
}

func BenchmarkPathQuery_EdgeFilter_Multiple(b *testing.B) {
	benchmarkWithEdgeFilter(b, []string{"related_to", "near", "depends_on"})
}

func benchmarkWithEdgeFilter(b *testing.B, edgeFilter []string) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	b.Logf("Benchmark: EdgeFilter with %d types", len(edgeFilter))

	ctx := context.Background()
	query := PathQuery{
		StartEntity: "entity-001",
		MaxDepth:    3,
		MaxNodes:    200,
		MaxTime:     500 * time.Millisecond,
		EdgeFilter:  edgeFilter,
		DecayFactor: 0.8,
		MaxPaths:    20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = query
		_ = ctx
		// TODO: Execute actual PathQuery
	}
}

// BenchmarkPathQuery_DecayFactor measures impact of different decay factors
func BenchmarkPathQuery_Decay_0_7(b *testing.B) {
	benchmarkWithDecayFactor(b, 0.7)
}

func BenchmarkPathQuery_Decay_0_85(b *testing.B) {
	benchmarkWithDecayFactor(b, 0.85)
}

func BenchmarkPathQuery_Decay_0_95(b *testing.B) {
	benchmarkWithDecayFactor(b, 0.95)
}

func benchmarkWithDecayFactor(b *testing.B, decayFactor float64) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx := context.Background()
	query := PathQuery{
		StartEntity: "entity-001",
		MaxDepth:    3,
		MaxNodes:    200,
		MaxTime:     500 * time.Millisecond,
		EdgeFilter:  []string{"related_to"},
		DecayFactor: decayFactor,
		MaxPaths:    20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = query
		_ = ctx
		// TODO: Execute actual PathQuery
	}
}

// Benchmark helper: Generate synthetic graph for testing
func generateSyntheticGraph(entityCount int) map[string]*gtypes.EntityState {
	entities := make(map[string]*gtypes.EntityState, entityCount)

	for i := 0; i < entityCount; i++ {
		id := fmt.Sprintf("entity-%03d", i)
		entity := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   id,
				Type: "test.entity",
				Properties: map[string]any{
					"index": i,
					"name":  fmt.Sprintf("Test Entity %d", i),
				},
				Status: gtypes.StatusActive,
			},
			Edges: []gtypes.Edge{},
		}

		// Create relationships to next 3 entities (mesh-like structure)
		for j := 1; j <= 3 && i+j < entityCount; j++ {
			targetID := fmt.Sprintf("entity-%03d", i+j)
			edge := gtypes.Edge{
				EdgeType:   "related_to",
				ToEntityID: targetID,
				Weight:     1.0,
				Properties: map[string]any{},
			}
			entity.Edges = append(entity.Edges, edge)
		}

		entities[id] = entity
	}

	return entities
}

// Instructions for running benchmarks:
//
// # Run all PathRAG benchmarks
// go test -bench=BenchmarkPathQuery -benchmem -benchtime=10s ./graph/query/
//
// # Run specific benchmark
// go test -bench=BenchmarkPathQuery_MediumGraph_Depth3 -benchmem ./graph/query/
//
// # Generate CPU profile
// go test -bench=BenchmarkPathQuery_LargeGraph -cpuprofile=cpu.prof ./graph/query/
//
// # Generate memory profile
// go test -bench=BenchmarkPathQuery_LargeGraph -memprofile=mem.prof ./graph/query/
//
// # Analyze profiles
// go tool pprof cpu.prof
// go tool pprof mem.prof
//
// # Compare benchmarks (requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest)
// go test -bench=. -count=10 ./graph/query/ > old.txt
// # Make changes
// go test -bench=. -count=10 ./graph/query/ > new.txt
// benchstat old.txt new.txt
