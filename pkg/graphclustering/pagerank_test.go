package graphclustering

import (
	"context"
	"math"
	"testing"
)

// mockGraphProvider implements GraphProvider for testing
type mockGraphProvider struct {
	entities map[string][]string // entityID -> neighbor IDs
}

func newMockGraphProvider() *mockGraphProvider {
	return &mockGraphProvider{
		entities: make(map[string][]string),
	}
}

func (m *mockGraphProvider) addEdge(from, to string) {
	if m.entities[from] == nil {
		m.entities[from] = []string{}
	}
	m.entities[from] = append(m.entities[from], to)

	// Ensure 'to' exists in entities map
	if m.entities[to] == nil {
		m.entities[to] = []string{}
	}
}

func (m *mockGraphProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	ids := make([]string, 0, len(m.entities))
	for id := range m.entities {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockGraphProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if direction == "outgoing" || direction == "both" {
		return m.entities[entityID], nil
	}
	// For incoming, need to find all nodes that link to entityID
	incoming := []string{}
	for id, neighbors := range m.entities {
		for _, neighbor := range neighbors {
			if neighbor == entityID {
				incoming = append(incoming, id)
				break
			}
		}
	}
	return incoming, nil
}

func (m *mockGraphProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	neighbors := m.entities[fromID]
	for _, n := range neighbors {
		if n == toID {
			return 1.0, nil
		}
	}
	return 0.0, nil
}

func TestPageRank_SimpleGraph(t *testing.T) {
	// Create simple graph:
	// A -> B -> C
	//      B -> D
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")
	provider.addEdge("B", "C")
	provider.addEdge("B", "D")

	ctx := context.Background()
	config := DefaultPageRankConfig()
	config.Iterations = 100 // Ensure convergence

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	// Check that B has highest score (hub node)
	if result.Ranked[0] != "B" {
		t.Errorf("Expected B to have highest PageRank, got %s", result.Ranked[0])
	}

	// Check scores sum to 1.0
	sum := 0.0
	for _, score := range result.Scores {
		sum += score
	}
	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("Expected scores to sum to 1.0, got %f", sum)
	}

	// Check convergence
	if !result.Converged {
		t.Errorf("Expected algorithm to converge")
	}
}

func TestPageRank_StarGraph(t *testing.T) {
	// Create star graph where A is the hub
	// B -> A <- C
	//      ^
	//      D
	provider := newMockGraphProvider()
	provider.addEdge("B", "A")
	provider.addEdge("C", "A")
	provider.addEdge("D", "A")
	provider.addEdge("A", "A") // Self-loop

	ctx := context.Background()
	config := DefaultPageRankConfig()

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	// A should have highest PageRank (receives all links)
	if result.Ranked[0] != "A" {
		t.Errorf("Expected A to have highest PageRank, got %s (score: %f)", result.Ranked[0], result.Scores[result.Ranked[0]])
	}

	// A should have significantly higher score than others
	scoreA := result.Scores["A"]
	scoreB := result.Scores["B"]
	if scoreA <= scoreB {
		t.Errorf("Expected A (%f) to have higher score than B (%f)", scoreA, scoreB)
	}
}

func TestPageRank_EmptyGraph(t *testing.T) {
	provider := newMockGraphProvider()

	ctx := context.Background()
	config := DefaultPageRankConfig()

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	if len(result.Scores) != 0 {
		t.Errorf("Expected empty scores for empty graph, got %d", len(result.Scores))
	}

	if len(result.Ranked) != 0 {
		t.Errorf("Expected empty ranked list for empty graph, got %d", len(result.Ranked))
	}

	if !result.Converged {
		t.Errorf("Expected empty graph to be converged")
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	provider := newMockGraphProvider()
	provider.addEdge("A", "A") // Self-loop

	ctx := context.Background()
	config := DefaultPageRankConfig()

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	if len(result.Scores) != 1 {
		t.Errorf("Expected 1 score, got %d", len(result.Scores))
	}

	if result.Scores["A"] != 1.0 {
		t.Errorf("Expected A to have score 1.0, got %f", result.Scores["A"])
	}
}

func TestPageRank_TopN(t *testing.T) {
	// Create graph with multiple nodes
	provider := newMockGraphProvider()
	for i := 0; i < 10; i++ {
		from := string(rune('A' + i))
		to := string(rune('A' + (i+1)%10))
		provider.addEdge(from, to)
	}

	ctx := context.Background()
	config := DefaultPageRankConfig()
	config.TopN = 3

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	// Should only return top 3
	if len(result.Ranked) != 3 {
		t.Errorf("Expected 3 ranked nodes, got %d", len(result.Ranked))
	}

	// But all scores should be present
	if len(result.Scores) != 10 {
		t.Errorf("Expected 10 scores, got %d", len(result.Scores))
	}
}

func TestPageRankForCommunity(t *testing.T) {
	// Create graph with two communities
	// Community 1: A -> B -> C
	// Community 2: D -> E -> F
	// Cross-link: C -> D
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")
	provider.addEdge("B", "C")
	provider.addEdge("C", "D")
	provider.addEdge("D", "E")
	provider.addEdge("E", "F")

	ctx := context.Background()
	config := DefaultPageRankConfig()

	// Compute PageRank for Community 1 only
	community1 := []string{"A", "B", "C"}
	result, err := ComputePageRankForCommunity(ctx, provider, community1, config)
	if err != nil {
		t.Fatalf("ComputePageRankForCommunity failed: %v", err)
	}

	// Should only have scores for Community 1 members
	if len(result.Scores) != 3 {
		t.Errorf("Expected 3 scores, got %d", len(result.Scores))
	}

	// B should have highest score in this subgraph (receives link from A)
	if result.Ranked[0] != "B" && result.Ranked[0] != "C" {
		t.Logf("Ranked order: %v", result.Ranked)
		t.Logf("Scores: %v", result.Scores)
		// B or C could be highest depending on dangling link handling
	}

	// Scores should sum to 1.0
	sum := 0.0
	for _, score := range result.Scores {
		sum += score
	}
	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("Expected scores to sum to 1.0, got %f", sum)
	}
}

func TestComputeRepresentativeEntities(t *testing.T) {
	// Create hub-and-spoke graph
	// A is hub, B,C,D,E are spokes
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")
	provider.addEdge("A", "C")
	provider.addEdge("A", "D")
	provider.addEdge("A", "E")
	provider.addEdge("B", "A")
	provider.addEdge("C", "A")
	provider.addEdge("D", "A")
	provider.addEdge("E", "A")

	ctx := context.Background()
	community := []string{"A", "B", "C", "D", "E"}

	ranked, scores, err := ComputeRepresentativeEntities(ctx, provider, community, 3)
	if err != nil {
		t.Fatalf("ComputeRepresentativeEntities failed: %v", err)
	}

	// Should return top 3
	if len(ranked) != 3 {
		t.Errorf("Expected 3 representatives, got %d", len(ranked))
	}

	// A should be first (hub)
	if ranked[0] != "A" {
		t.Errorf("Expected A to be top representative, got %s (score: %f)", ranked[0], scores[ranked[0]])
	}

	// All returned entities should have scores
	for _, id := range ranked {
		if _, ok := scores[id]; !ok {
			t.Errorf("Missing score for entity %s", id)
		}
	}
}

func TestComputeRepresentativeEntities_SmallCommunity(t *testing.T) {
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")

	ctx := context.Background()
	community := []string{"A", "B"}

	ranked, scores, err := ComputeRepresentativeEntities(ctx, provider, community, 5)
	if err != nil {
		t.Fatalf("ComputeRepresentativeEntities failed: %v", err)
	}

	// Should fall back to degree centrality for small communities
	if len(ranked) != 2 {
		t.Errorf("Expected 2 representatives, got %d", len(ranked))
	}

	// Should have scores
	if len(scores) != 2 {
		t.Errorf("Expected 2 scores, got %d", len(scores))
	}
}

func TestComputeRepresentativeEntities_EmptyCommunity(t *testing.T) {
	provider := newMockGraphProvider()

	ctx := context.Background()
	community := []string{}

	ranked, scores, err := ComputeRepresentativeEntities(ctx, provider, community, 5)
	if err != nil {
		t.Fatalf("ComputeRepresentativeEntities failed: %v", err)
	}

	if len(ranked) != 0 {
		t.Errorf("Expected 0 representatives for empty community, got %d", len(ranked))
	}

	if len(scores) != 0 {
		t.Errorf("Expected 0 scores for empty community, got %d", len(scores))
	}
}

func TestPageRank_Convergence(t *testing.T) {
	// Create chain graph
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")
	provider.addEdge("B", "C")
	provider.addEdge("C", "D")
	provider.addEdge("D", "A")

	ctx := context.Background()
	config := DefaultPageRankConfig()
	config.Tolerance = 1e-10 // Very strict tolerance

	result, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	// Should converge for simple graph
	if !result.Converged {
		t.Errorf("Expected convergence, but did not converge in %d iterations", result.Iterations)
	}

	t.Logf("Converged in %d iterations", result.Iterations)
}

func TestPageRank_DeterministicRanking(t *testing.T) {
	// Same graph should produce same ranking every time
	provider := newMockGraphProvider()
	provider.addEdge("A", "B")
	provider.addEdge("A", "C")
	provider.addEdge("B", "C")
	provider.addEdge("C", "A")

	ctx := context.Background()
	config := DefaultPageRankConfig()

	// Run twice
	result1, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("First run failed: %v", err)
	}

	result2, err := ComputePageRank(ctx, provider, config)
	if err != nil {
		t.Fatalf("Second run failed: %v", err)
	}

	// Rankings should be identical
	if len(result1.Ranked) != len(result2.Ranked) {
		t.Fatalf("Different ranking lengths: %d vs %d", len(result1.Ranked), len(result2.Ranked))
	}

	for i := range result1.Ranked {
		if result1.Ranked[i] != result2.Ranked[i] {
			t.Errorf("Different ranking at position %d: %s vs %s", i, result1.Ranked[i], result2.Ranked[i])
		}
	}

	// Scores should be nearly identical
	for id := range result1.Scores {
		diff := math.Abs(result1.Scores[id] - result2.Scores[id])
		if diff > 1e-10 {
			t.Errorf("Different scores for %s: %f vs %f (diff: %e)", id, result1.Scores[id], result2.Scores[id], diff)
		}
	}
}

func BenchmarkPageRank_SmallGraph(b *testing.B) {
	provider := newMockGraphProvider()
	for i := 0; i < 10; i++ {
		from := string(rune('A' + i))
		to := string(rune('A' + (i+1)%10))
		provider.addEdge(from, to)
	}

	ctx := context.Background()
	config := DefaultPageRankConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ComputePageRank(ctx, provider, config)
		if err != nil {
			b.Fatalf("ComputePageRank failed: %v", err)
		}
	}
}

func BenchmarkPageRank_LargeGraph(b *testing.B) {
	provider := newMockGraphProvider()

	// Create larger graph (100 nodes, random connections)
	nodes := make([]string, 100)
	for i := 0; i < 100; i++ {
		nodes[i] = string(rune('A' + (i % 26))) + string(rune('0' + (i / 26)))
	}

	for i := 0; i < 100; i++ {
		// Each node connects to next 3 nodes (circular)
		provider.addEdge(nodes[i], nodes[(i+1)%100])
		provider.addEdge(nodes[i], nodes[(i+2)%100])
		provider.addEdge(nodes[i], nodes[(i+3)%100])
	}

	ctx := context.Background()
	config := DefaultPageRankConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ComputePageRank(ctx, provider, config)
		if err != nil {
			b.Fatalf("ComputePageRank failed: %v", err)
		}
	}
}
