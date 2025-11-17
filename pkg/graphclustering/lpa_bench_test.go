package graphclustering

import (
	"context"
	"fmt"
	"testing"
)

// Benchmark small graph (10 nodes, 2 communities)
func BenchmarkLPADetector_SmallGraph(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Community 1: 5 nodes fully connected
	for i := 0; i < 5; i++ {
		provider.AddEntity(fmt.Sprintf("A%d", i))
	}
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			provider.AddEdge(fmt.Sprintf("A%d", i), fmt.Sprintf("A%d", j), 1.0)
		}
	}

	// Community 2: 5 nodes fully connected
	for i := 0; i < 5; i++ {
		provider.AddEntity(fmt.Sprintf("B%d", i))
	}
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			provider.AddEdge(fmt.Sprintf("B%d", i), fmt.Sprintf("B%d", j), 1.0)
		}
	}

	// Weak bridge
	provider.AddEdge("A4", "B0", 0.1)

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.DetectCommunities(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark medium graph (50 nodes, 5 communities)
func BenchmarkLPADetector_MediumGraph(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create 5 communities of 10 nodes each
	for c := 0; c < 5; c++ {
		// Add nodes
		for i := 0; i < 10; i++ {
			provider.AddEntity(fmt.Sprintf("C%d_N%d", c, i))
		}

		// Fully connect within community
		for i := 0; i < 10; i++ {
			for j := i + 1; j < 10; j++ {
				provider.AddEdge(
					fmt.Sprintf("C%d_N%d", c, i),
					fmt.Sprintf("C%d_N%d", c, j),
					1.0,
				)
			}
		}
	}

	// Add weak inter-community edges
	for c := 0; c < 4; c++ {
		provider.AddEdge(
			fmt.Sprintf("C%d_N9", c),
			fmt.Sprintf("C%d_N0", c+1),
			0.1,
		)
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.DetectCommunities(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark large graph (200 nodes, 10 communities)
func BenchmarkLPADetector_LargeGraph(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create 10 communities of 20 nodes each
	for c := 0; c < 10; c++ {
		// Add nodes
		for i := 0; i < 20; i++ {
			provider.AddEntity(fmt.Sprintf("C%d_N%d", c, i))
		}

		// Create random edges within community (50% connectivity)
		for i := 0; i < 20; i++ {
			for j := i + 1; j < 20; j++ {
				if (i+j+c)%2 == 0 { // Deterministic "random" for reproducibility
					provider.AddEdge(
						fmt.Sprintf("C%d_N%d", c, i),
						fmt.Sprintf("C%d_N%d", c, j),
						1.0,
					)
				}
			}
		}
	}

	// Add sparse inter-community edges
	for c := 0; c < 9; c++ {
		provider.AddEdge(
			fmt.Sprintf("C%d_N19", c),
			fmt.Sprintf("C%d_N0", c+1),
			0.1,
		)
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.DetectCommunities(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark entity community lookup
func BenchmarkLPADetector_GetEntityCommunity(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create simple graph
	for i := 0; i < 10; i++ {
		provider.AddEntity(fmt.Sprintf("N%d", i))
	}
	for i := 0; i < 10; i++ {
		for j := i + 1; j < 10; j++ {
			provider.AddEdge(fmt.Sprintf("N%d", i), fmt.Sprintf("N%d", j), 1.0)
		}
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	// Run detection once
	_, err := detector.DetectCommunities(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.GetEntityCommunity(ctx, "N5", 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark parallel entity lookups
func BenchmarkLPADetector_GetEntityCommunity_Parallel(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create simple graph
	for i := 0; i < 10; i++ {
		provider.AddEntity(fmt.Sprintf("N%d", i))
	}
	for i := 0; i < 10; i++ {
		for j := i + 1; j < 10; j++ {
			provider.AddEdge(fmt.Sprintf("N%d", i), fmt.Sprintf("N%d", j), 1.0)
		}
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	// Run detection once
	_, err := detector.DetectCommunities(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		entityID := "N5"
		for pb.Next() {
			_, err := detector.GetEntityCommunity(ctx, entityID, 0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark incremental updates
func BenchmarkLPADetector_UpdateCommunities(b *testing.B) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create initial graph
	for i := 0; i < 50; i++ {
		provider.AddEntity(fmt.Sprintf("N%d", i))
	}
	for i := 0; i < 50; i++ {
		for j := i + 1; j < 50 && j < i+5; j++ {
			provider.AddEdge(fmt.Sprintf("N%d", i), fmt.Sprintf("N%d", j), 1.0)
		}
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	// Initial detection
	_, err := detector.DetectCommunities(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate updating a few entities
		changedEntities := []string{"N10", "N11", "N12"}
		err := detector.UpdateCommunities(ctx, changedEntities)
		if err != nil {
			b.Fatal(err)
		}
	}
}
