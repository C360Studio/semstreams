// Package structural provides structural graph indexing algorithms for query optimization
// and inference detection.
//
// # Overview
//
// The structural package implements two complementary indexing strategies that enable
// efficient graph queries and anomaly detection:
//
//   - K-core decomposition: identifies the dense backbone of the graph
//   - Pivot-based distance indexing: enables O(1) distance estimation
//
// These indices support query optimization (filtering noise, pruning traversals) and
// structural inference (detecting anomalies like semantic-structural gaps).
//
// # Architecture
//
//	                           Graph Provider
//	                                 ↓
//	┌────────────────────────────────────────────────────────────────┐
//	│                    Structural Computers                        │
//	├───────────────────────────┬────────────────────────────────────┤
//	│     KCoreComputer         │        PivotComputer               │
//	│   (peeling algorithm)     │   (PageRank + BFS)                 │
//	└───────────────────────────┴────────────────────────────────────┘
//	                                 ↓
//	                  Ephemeral in-memory indices
//	                    (same-cycle consumers)
//
// # K-Core Decomposition
//
// K-core decomposition identifies nested subgraphs where each node has at least k
// neighbors within the same subgraph. Higher core numbers indicate more central,
// densely connected nodes.
//
// Use cases:
//   - Filter noise: exclude low-core (peripheral) nodes from search results
//   - Hub detection: high-core nodes form the graph backbone
//   - Anomaly detection: core demotion signals structural changes
//
// Algorithm (peeling):
//  1. Initialize degree for all vertices
//  2. Repeatedly remove the vertex with minimum degree
//  3. The core number of a vertex is its degree when removed
//
// Time complexity: O(V + E)
//
// Usage:
//
//	computer := structural.NewKCoreComputer(provider, logger)
//	index, err := computer.Compute(ctx)
//
//	// Filter results to include only hub entities
//	filtered := index.FilterByMinCore(entityIDs, 3)
//
//	// Get all entities in core level 5+
//	hubs := index.GetEntitiesAboveCore(5)
//
// # Pivot-Based Distance Indexing
//
// The pivot index pre-computes shortest path distances from every node to a small
// set of "pivot" nodes selected by PageRank centrality. Distance between any two
// nodes can then be bounded using the triangle inequality:
//
//	lower_bound = max |d(A,pivot) - d(B,pivot)| over all pivots
//	upper_bound = min d(A,pivot) + d(B,pivot) over all pivots
//
// Use cases:
//   - Multi-hop filtering: quickly determine if two entities are within N hops
//   - PathRAG optimization: prune candidates before expensive traversal
//   - Semantic-structural gap detection: find semantically similar but structurally distant pairs
//
// Algorithm:
//  1. Select k pivot nodes using PageRank (central nodes make better pivots)
//  2. Run BFS from each pivot to compute distances to all reachable nodes
//  3. Store distance vectors for each node
//
// Time complexity: O(k × (V + E)) where k = pivot count
//
// Usage:
//
//	computer := structural.NewPivotComputer(provider, 16, logger)
//	index, err := computer.Compute(ctx)
//
//	// Estimate distance between two entities
//	lower, upper := index.EstimateDistance(entityA, entityB)
//
//	// Quick check if within N hops
//	if index.IsWithinHops(entityA, entityB, 3) {
//	    // Proceed with expensive traversal
//	}
//
//	// Get candidates potentially within N hops
//	candidates := index.GetReachableCandidates(source, maxHops)
//
// # Configuration
//
// Default parameters:
//
//	MaxHopDistance:            255   # Sentinel for unreachable nodes
//	DefaultPivotCount:         16    # Landmark nodes (10-20 is typical)
//	DefaultPageRankIterations: 20    # PageRank convergence iterations
//	DefaultPageRankDamping:    0.85  # Random walk continuation probability
//
// # Thread Safety
//
// KCoreComputer and PivotComputer are safe for concurrent use. The index types
// (KCoreIndex, PivotIndex) are safe for concurrent
// reads but not concurrent writes.
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/graph]: Provider interface and graph types
//   - [github.com/c360studio/semstreams/graph/inference]: Anomaly detection using structural indices
//   - [github.com/c360studio/semstreams/graph/clustering]: Community detection (triggers recomputation)
package structural
