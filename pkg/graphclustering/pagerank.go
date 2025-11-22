package graphclustering

import (
	"context"
	"math"
	"sort"
)

// PageRankConfig holds configuration for PageRank computation
type PageRankConfig struct {
	// Iterations is the number of iterations to run (default: 20)
	Iterations int

	// DampingFactor is the probability of continuing the random walk (default: 0.85)
	DampingFactor float64

	// Tolerance is the convergence threshold (default: 1e-6)
	Tolerance float64

	// TopN is the number of top-ranked nodes to return (0 = all)
	TopN int
}

// DefaultPageRankConfig returns the standard PageRank configuration
func DefaultPageRankConfig() PageRankConfig {
	return PageRankConfig{
		Iterations:    20,
		DampingFactor: 0.85,
		Tolerance:     1e-6,
		TopN:          0, // Return all
	}
}

// PageRankResult holds the results of PageRank computation
type PageRankResult struct {
	// Scores maps entity ID to PageRank score
	Scores map[string]float64

	// Ranked contains entity IDs sorted by PageRank score (descending)
	Ranked []string

	// Iterations is the actual number of iterations run
	Iterations int

	// Converged indicates whether the algorithm converged before max iterations
	Converged bool
}

// ComputePageRank computes PageRank scores for all nodes in the graph
func ComputePageRank(ctx context.Context, provider GraphProvider, config PageRankConfig) (*PageRankResult, error) {
	// Get all entity IDs
	entityIDs, err := provider.GetAllEntityIDs(ctx)
	if err != nil {
		return nil, err
	}

	return computePageRankForSubset(ctx, provider, entityIDs, config)
}

// ComputePageRankForCommunity computes PageRank for entities within a community
// This is more efficient than full graph PageRank for large graphs
func ComputePageRankForCommunity(ctx context.Context, provider GraphProvider, communityMembers []string, config PageRankConfig) (*PageRankResult, error) {
	return computePageRankForSubset(ctx, provider, communityMembers, config)
}

// computePageRankForSubset computes PageRank for a subset of nodes
func computePageRankForSubset(ctx context.Context, provider GraphProvider, nodeIDs []string, config PageRankConfig) (*PageRankResult, error) {
	n := len(nodeIDs)
	if n == 0 {
		return &PageRankResult{
			Scores:     make(map[string]float64),
			Ranked:     []string{},
			Iterations: 0,
			Converged:  true,
		}, nil
	}

	// Build graph structure
	nodeIndex := make(map[string]int, n)
	for i, id := range nodeIDs {
		nodeIndex[id] = i
	}

	outLinks, outLinkCount, err := buildAdjacencyLists(ctx, provider, nodeIDs, nodeIndex)
	if err != nil {
		return nil, err
	}

	// Initialize and run PageRank iterations
	scores, newScores := initializeScores(n)
	converged, iterations := runPageRankIterations(ctx, scores, newScores, outLinks, outLinkCount, n, config)

	// Convert to map and rank nodes
	scoreMap := scoresToMap(nodeIDs, scores, n)
	rankedIDs := rankNodes(scoreMap, n, config.TopN)

	return &PageRankResult{
		Scores:     scoreMap,
		Ranked:     rankedIDs,
		Iterations: iterations + 1,
		Converged:  converged,
	}, nil
}

// buildAdjacencyLists builds the adjacency structure from the provider
func buildAdjacencyLists(ctx context.Context, provider GraphProvider, nodeIDs []string, nodeIndex map[string]int) ([][]int, []int, error) {
	n := len(nodeIDs)
	outLinks := make([][]int, n)
	outLinkCount := make([]int, n)

	for i, fromID := range nodeIDs {
		neighbors, err := provider.GetNeighbors(ctx, fromID, "outgoing")
		if err != nil {
			return nil, nil, err
		}

		// Filter neighbors to only those in our subset
		for _, toID := range neighbors {
			if toIdx, ok := nodeIndex[toID]; ok {
				outLinks[i] = append(outLinks[i], toIdx)
				outLinkCount[i]++
			}
		}
	}

	return outLinks, outLinkCount, nil
}

// initializeScores creates score arrays with uniform distribution
func initializeScores(n int) ([]float64, []float64) {
	scores := make([]float64, n)
	newScores := make([]float64, n)
	initialScore := 1.0 / float64(n)
	for i := range scores {
		scores[i] = initialScore
	}
	return scores, newScores
}

// runPageRankIterations runs the iterative PageRank computation
func runPageRankIterations(ctx context.Context, scores, newScores []float64, outLinks [][]int, outLinkCount []int, n int, config PageRankConfig) (bool, int) {
	d := config.DampingFactor
	teleport := (1.0 - d) / float64(n)
	converged := false
	iterations := 0

	for iterations = 0; iterations < config.Iterations; iterations++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return false, iterations
		default:
		}

		// Compute new scores
		computePageRankScores(newScores, scores, outLinks, outLinkCount, n, d, teleport)

		// Check convergence
		if hasConverged(scores, newScores, n, config.Tolerance) {
			converged = true
			copy(scores, newScores)
			break
		}

		// Swap score arrays
		scores, newScores = newScores, scores
	}

	return converged, iterations
}

// computePageRankScores computes one iteration of PageRank scores
func computePageRankScores(newScores, scores []float64, outLinks [][]int, outLinkCount []int, n int, d, teleport float64) {
	for i := range newScores {
		sum := 0.0

		// Sum contributions from all nodes linking to i
		for j := 0; j < n; j++ {
			if containsInt(outLinks[j], i) && outLinkCount[j] > 0 {
				sum += scores[j] / float64(outLinkCount[j])
			}
		}

		newScores[i] = teleport + d*sum
	}
}

// hasConverged checks if PageRank scores have converged
func hasConverged(scores, newScores []float64, _ int, tolerance float64) bool {
	maxDiff := 0.0
	for i := range scores {
		diff := math.Abs(newScores[i] - scores[i])
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	return maxDiff < tolerance
}

// scoresToMap converts score array to map with normalization
func scoresToMap(nodeIDs []string, scores []float64, n int) map[string]float64 {
	scoreMap := make(map[string]float64, n)
	sum := 0.0
	for i, id := range nodeIDs {
		scoreMap[id] = scores[i]
		sum += scores[i]
	}

	// Normalize scores to sum to 1.0
	if sum > 0 {
		for id := range scoreMap {
			scoreMap[id] /= sum
		}
	}

	return scoreMap
}

// rankNodes returns the top N nodes sorted by score
func rankNodes(scoreMap map[string]float64, _ int, topN int) []string {
	type scoredNode struct {
		id    string
		score float64
	}

	ranked := make([]scoredNode, 0, len(scoreMap))
	for id, score := range scoreMap {
		ranked = append(ranked, scoredNode{id, score})
	}

	sort.Slice(ranked, func(i, j int) bool {
		// Sort descending by score
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// Tie-break by ID for determinism
		return ranked[i].id < ranked[j].id
	})

	// Extract ranked IDs
	limit := len(ranked)
	if topN > 0 && topN < limit {
		limit = topN
	}

	rankedIDs := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		rankedIDs = append(rankedIDs, ranked[i].id)
	}

	return rankedIDs
}

// ComputeRepresentativeEntities computes representative entities for a community using PageRank
// Returns the top N entities by PageRank score
func ComputeRepresentativeEntities(ctx context.Context, provider GraphProvider, communityMembers []string, topN int) ([]string, map[string]float64, error) {
	if len(communityMembers) == 0 {
		return []string{}, make(map[string]float64), nil
	}

	// Use PageRank if community is large enough
	if len(communityMembers) >= 3 {
		config := DefaultPageRankConfig()
		config.TopN = topN

		result, err := ComputePageRankForCommunity(ctx, provider, communityMembers, config)
		if err != nil {
			// Fall back to degree centrality on error
			return computeDegreeCentrality(ctx, provider, communityMembers, topN)
		}

		return result.Ranked, result.Scores, nil
	}

	// For very small communities, just return all members by degree
	return computeDegreeCentrality(ctx, provider, communityMembers, topN)
}

// computeDegreeCentrality is a fallback method using degree centrality
func computeDegreeCentrality(ctx context.Context, provider GraphProvider, nodeIDs []string, topN int) ([]string, map[string]float64, error) {
	type degreeNode struct {
		id     string
		degree int
	}

	nodes := make([]degreeNode, 0, len(nodeIDs))

	// Count degrees
	for _, id := range nodeIDs {
		neighbors, err := provider.GetNeighbors(ctx, id, "both")
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, degreeNode{id, len(neighbors)})
	}

	// Sort by degree (descending)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].degree != nodes[j].degree {
			return nodes[i].degree > nodes[j].degree
		}
		return nodes[i].id < nodes[j].id
	})

	// Extract top N
	limit := len(nodes)
	if topN > 0 && topN < limit {
		limit = topN
	}

	ranked := make([]string, 0, limit)
	scores := make(map[string]float64, limit)

	for i := 0; i < limit; i++ {
		ranked = append(ranked, nodes[i].id)
		// Normalize degree to [0, 1] range
		if len(nodes) > 0 && nodes[0].degree > 0 {
			scores[nodes[i].id] = float64(nodes[i].degree) / float64(nodes[0].degree)
		} else {
			scores[nodes[i].id] = 0.0
		}
	}

	return ranked, scores, nil
}

// containsInt checks if slice contains value
func containsInt(slice []int, value int) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
