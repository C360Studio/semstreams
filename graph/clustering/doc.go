// Package clustering provides community detection algorithms and graph clustering
// for discovering structural patterns in the knowledge graph.
//
// # Overview
//
// The clustering package implements the Label Propagation Algorithm (LPA) for
// detecting communities of related entities. It supports hierarchical clustering
// with multiple granularity levels, enabling both fine-grained local communities
// and coarse-grained global clusters.
//
// Detected communities are enriched with statistical summaries (TF-IDF keyword
// extraction) immediately, with optional LLM enhancement performed asynchronously.
// PageRank is used to identify representative entities within each community.
//
// # Architecture
//
//	                                Graph Provider
//	                                      ↓
//	┌──────────────────────────────────────────────────────────────┐
//	│                      LPA Detector                            │
//	├──────────────────────────────────────────────────────────────┤
//	│  Level 0 (fine)  →  Level 1 (mid)  →  Level 2 (coarse)      │
//	└──────────────────────────────────────────────────────────────┘
//	                                      ↓
//	┌────────────────────┐    ┌────────────────────────────────────┐
//	│ Progressive        │───→│ COMMUNITY_INDEX KV                 │
//	│ Summarizer         │    │ - {level}.{community_id}           │
//	│ (statistical)      │    │ - entity.{level}.{entity_id}       │
//	└────────────────────┘    └────────────────────────────────────┘
//	                                      ↓
//	                          ┌───────────────────────┐
//	                          │ Enhancement Worker    │
//	                          │ (async LLM via KV     │
//	                          │  watcher)             │
//	                          └───────────────────────┘
//
// # Usage
//
// Configure and run community detection:
//
//	// Create storage backed by NATS KV
//	storage := clustering.NewNATSCommunityStorage(communityBucket)
//
//	// Create LPA detector with progressive summarization
//	detector := clustering.NewLPADetector(graphProvider, storage).
//	    WithLevels(3).
//	    WithMaxIterations(100).
//	    WithProgressiveSummarization(
//	        clustering.NewProgressiveSummarizer(),
//	        entityProvider,
//	    )
//
//	// Run detection (typically after graph updates)
//	communities, err := detector.DetectCommunities(ctx)
//	// communities[0] = fine-grained, communities[1] = mid, communities[2] = coarse
//
// Query communities and entities:
//
//	// Get community for an entity at specific level
//	community, err := detector.GetEntityCommunity(ctx, entityID, 0)
//
//	// Get all communities at a level
//	allLevel0, err := detector.GetCommunitiesByLevel(ctx, 0)
//
//	// Infer relationships from community co-membership
//	triples, err := detector.InferRelationshipsFromCommunities(ctx, 0, clustering.DefaultInferenceConfig())
//
// # Label Propagation Algorithm
//
// LPA iteratively assigns entities to communities based on neighbor voting:
//
//  1. Each entity starts with its own unique label
//  2. Entities adopt the most frequent label among their neighbors (weighted by edge weight)
//  3. Process continues until convergence or max iterations reached
//  4. Shuffled processing order reduces oscillation
//
// Hierarchical levels are computed by treating communities from level N as
// super-nodes for level N+1 detection.
//
// # Community Summarization
//
// Three summarization strategies are available:
//
// StatisticalSummarizer:
//   - TF-IDF-like keyword extraction from entity types and properties
//   - PageRank-based representative entity selection
//   - Template-based summary generation
//   - Always available, no external dependencies
//
// LLMSummarizer:
//   - OpenAI-compatible LLM for natural language summaries
//   - Works with seminstruct, OpenAI, Ollama, vLLM, etc.
//   - Falls back to statistical on service unavailability
//
// ProgressiveSummarizer:
//   - Statistical summary immediately available
//   - LLM enhancement performed asynchronously
//   - Best for user-facing applications needing fast initial response
//
// # Enhancement Worker
//
// The EnhancementWorker watches COMMUNITY_INDEX KV as a TRIGGER ONLY and writes
// LLM summaries to the worker-owned, content-addressed COMMUNITY_SUMMARIES store
// keyed by {level}.{membership_hash} (ADR-087). It never writes COMMUNITY_INDEX,
// so a lagging worker can neither clobber a fresher partition nor resurrect a
// pruned community:
//
//	worker, err := clustering.NewEnhancementWorker(&clustering.EnhancementWorkerConfig{
//	    LLMSummarizer:   llmSummarizer,
//	    Querier:         queryManager,
//	    NATSClient:      natsClient,      // guarded summary write lane (payload-bounds)
//	    CommunityBucket: communityBucket, // COMMUNITY_INDEX — trigger only
//	    SummaryBucket:   summaryBucket,   // COMMUNITY_SUMMARIES — worker-owned
//	})
//
//	worker.Start(ctx)
//	defer worker.Stop()
//
// # PageRank
//
// PageRank identifies influential entities within communities:
//
//	config := clustering.DefaultPageRankConfig()
//	config.TopN = 10
//
//	result, err := clustering.ComputePageRankForCommunity(ctx, provider, memberIDs, config)
//	// result.Ranked = top 10 entities by PageRank score
//	// result.Scores = map of entity ID to normalized score
//
// Default configuration:
//   - Iterations: 20
//   - DampingFactor: 0.85
//   - Tolerance: 1e-6 (convergence threshold)
//
// # Configuration
//
// LPA detector configuration:
//
//	MaxIterations:    100       # Maximum iterations (limit: 10000)
//	Levels:           3         # Hierarchical levels (limit: 10)
//
// LLM summaries survive rebuilds by content-addressing (membership hash), not by
// a Jaccard-overlap transfer between runs — see the Enhancement Worker section
// and ADR-087.
//
// Inference configuration for relationship generation:
//
//	MinCommunitySize:        2     # Minimum size for inference
//	MaxInferredPerCommunity: 50    # Limit to prevent O(n²) explosion
//
// PageRank configuration:
//
//	Iterations:    20        # Max iterations
//	DampingFactor: 0.85      # Random walk continuation probability
//	Tolerance:     1e-6      # Convergence threshold
//	TopN:          0         # Return all (or limit to top N)
//
// # Storage
//
// Communities are stored in the COMMUNITY_INDEX KV bucket:
//
//	{level}.{community_id}        → Community JSON
//	entity.{level}.{entity_id}   → Community ID (for entity lookup)
//
// Storage can optionally create member_of triples:
//
//	config := clustering.CommunityStorageConfig{
//	    CreateTriples:   true,
//	    TriplePredicate: "graph.community.member-of",
//	}
//	storage := clustering.NewNATSCommunityStorageWithConfig(kv, config)
//
// # Thread Safety
//
// LPADetector, NATSCommunityStorage, and EnhancementWorker are safe for concurrent
// use. The enhancement worker supports pause/resume for coordinated graph updates:
//
//	worker.Pause()    // Stop processing new communities
//	// ... perform graph updates ...
//	worker.Resume()   // Continue processing
//
// # Metrics
//
// The clustering package exports Prometheus metrics under the semstreams_clustering namespace:
//   - communities_detected_total: Communities detected by level
//   - detection_duration_seconds: Detection run duration
//   - llm_enhancement_latency_seconds: LLM summarization duration
//   - community_summary_cache_hits_total: Triggers served from an existing summary (no LLM call)
//   - community_summary_generated_total: Summaries produced by a fresh LLM call
//   - community_summary_failed_total: Failed summary generations
//   - community_summaries_size: Current count of stored community summaries
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/graph]: Core graph types and Provider interface
//   - [github.com/c360studio/semstreams/graph/llm]: LLM integration for summarization
//   - [github.com/c360studio/semstreams/graph/inference]: Anomaly detection triggered by clustering
package clustering
