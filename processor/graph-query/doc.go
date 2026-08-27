// Package graphquery implements the query coordinator component for the graph subsystem.
//
// # Overview
//
// The graphquery package orchestrates queries across graph subsystem components
// (graph-ingest, graph-index, graph-embedding, graph-clustering) and provides
// advanced retrieval capabilities:
//
//   - PathRAG: Graph traversal with path tracking and relevance scoring
//   - GraphRAG: Community-aware retrieval (local and global search)
//   - Static routing: Query-type-to-subject mapping for distributed components
//
// The component handles graceful degradation when optional features (like community
// detection) are unavailable, enabling partial functionality during cluster startup.
//
// # Architecture
//
//	┌──────────────────────────────────────────────────────────────────────────┐
//	│                       graph-query Component                              │
//	├──────────────────────────────────────────────────────────────────────────┤
//	│  StaticRouter    │  PathSearcher   │  community cache │  GraphRAG       │
//	│  (query routing) │  (BFS traversal)│  (KV watcher)    │  (search)       │
//	└──────────────────────────────────────────────────────────────────────────┘
//	           ↓                ↓                  ↓                 ↓
//	┌────────────────┬────────────────┬────────────────┬────────────────────────┐
//	│  graph-ingest  │  graph-index   │  graph-embed   │  graph-clustering      │
//	│  (entity data) │  (relationships)│  (semantic)   │  (communities)         │
//	└────────────────┴────────────────┴────────────────┴────────────────────────┘
//
// # Usage
//
// The component is created via the factory pattern:
//
//	err := graphquery.Register(registry)
//
// Or configured in a flow definition:
//
//	{
//	    "type": "processor",
//	    "name": "graph-query",
//	    "config": {
//	        "ports": {
//	            "inputs": [
//	                {"name":"query_entity","config":{"kind":"nats-request","subject":"graph.query.entity"}},
//	                {"name":"query_relationships","config":{"kind":"nats-request","subject":"graph.query.relationships"}},
//	                {"name":"query_path_search","config":{"kind":"nats-request","subject":"graph.query.pathSearch"}}
//	            ]
//	        },
//	        "query_timeout": "5s",
//	        "max_depth": 10
//	    }
//	}
//
// # Query Types
//
// PathRAG (graph.query.pathSearch):
//
// BFS-based graph traversal with path tracking and relevance scoring:
//
//	{
//	    "start_entity": "org.platform.system.domain.type.instance",
//	    "max_depth": 3,
//	    "max_nodes": 100,
//	    "include_siblings": false
//	}
//
// Response includes discovered entities, full paths from start, and decay-weighted scores.
//
// Local Search (graph.query.localSearch):
//
// Community-scoped search for entities related to a starting entity:
//
//	{
//	    "entity_id": "org.platform.system.domain.type.instance",
//	    "query": "navigation system",
//	    "level": 0
//	}
//
// Returns entities from the same community that match the query.
//
// Global Search (graph.query.globalSearch):
//
// Tiered search across all communities:
//
//	{
//	    "query": "autonomous navigation",
//	    "level": 1,
//	    "max_communities": 5
//	}
//
// Uses semantic search first (if available), then falls back to text-based scoring.
//
// # Static Routing
//
// Query types are routed to fixed NATS subjects:
//
//	Entity queries → graph.ingest.query.*
//	Relationship queries → graph.index.query.*
//	Semantic queries → graph.embedding.query.*
//	Community queries → graph.clustering.query.*
//
// # Graceful Degradation
//
// The component keeps every responder installed and supervises the optional
// COMMUNITY_INDEX serving projection for its lifetime:
//
//   - Fresh watch maps publish only after initial enumeration completes
//   - Every watch exit unpublishes the exact generation; unexpected loss retries must-exist open
//   - Local/community-only reads return transient index_not_ready without a generation
//   - Independent lower-tier global/searchGraph results preserve their data and
//     report community_cache_not_ready when requested enrichment is unavailable
//
// # Configuration
//
// Configuration options:
//
//	QueryTimeout:     5s     # Timeout for inter-component requests
//	MaxDepth:         10     # Maximum BFS traversal depth
//	RecheckInterval:  5s     # Interval for rechecking missing buckets
//
// # PathRAG Algorithm
//
// The PathSearcher uses BFS with parent tracking:
//
//  1. Verify start entity exists via graph-ingest
//  2. BFS traversal following outgoing relationships via graph-index
//  3. Track parent info for each discovered entity
//  4. Calculate relevance scores with exponential decay (0.8 per hop by default)
//  5. Reconstruct full paths from start to each discovered entity
//
// Limits prevent unbounded traversal:
//   - MaxDepth: stops traversal at depth limit
//   - MaxNodes: stops after visiting N entities
//
// # GraphRAG Search
//
// Global search uses a tiered approach:
//
//  1. Tier 1: Semantic search via graph-embedding (embedding similarity)
//  2. Tier 2: Text-based scoring of community summaries
//  3. Load entities from top-N matching communities
//  4. Filter entities by query terms
//
// Local search:
//  1. Look up entity's community from cache
//  2. Fallback to storage query if cache miss
//  3. Fallback to semantic search if no community
//  4. Load and filter community members
//
// # Thread Safety
//
// The Component is safe for concurrent use. Query handlers process requests
// concurrently via NATS subscription workers.
//
// # Metrics
//
// The package exports Prometheus metrics:
//   - graph_query_duration_seconds: Query latency histogram
//   - graph_query_cache_hits_total: Community cache hits
//   - graph_query_cache_misses_total: Community cache misses
//   - graph_query_storage_hits_total: Storage fallback hits
//   - graph_query_storage_misses_total: Storage fallback misses
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/graph]: EntityState, Graphable interface
//   - [github.com/c360studio/semstreams/graph/clustering]: Community detection
//   - [github.com/c360studio/semstreams/graph/embedding]: Semantic search
//   - [github.com/c360studio/semstreams/pkg/resource]: Resource availability watching
package graphquery
