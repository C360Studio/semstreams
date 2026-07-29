// Package query provides a clean interface for reading graph data from NATS KV buckets.
//
// # Overview
//
// The query package extracts read operations from the graph processor, enabling reusable
// graph queries with caching. It provides entity retrieval, relationship traversal, spatial
// queries, and bounded path queries suitable for resource-constrained edge devices.
//
// All queries read from NATS KV buckets (ENTITY_STATES, SPATIAL_INDEX, INCOMING_INDEX)
// with an integrated cache layer for performance. The client manages bucket initialization
// lazily on first use.
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                        Query Client                             │
//	│  - Entity caching (hybrid LRU+TTL)                              │
//	│  - Lazy bucket initialization                                   │
//	│  - Bounded path traversal                                       │
//	└─────────────────────────────────────────────────────────────────┘
//	                              ↓
//	┌────────────────────┬────────────────────┬───────────────────────┐
//	│   ENTITY_STATES    │   SPATIAL_INDEX    │   INCOMING_INDEX      │
//	│   (entity data)    │   (geohash→IDs)    │   (target→sources)    │
//	└────────────────────┴────────────────────┴───────────────────────┘
//
// # Usage
//
// Create a query client:
//
//	config := query.DefaultConfig()
//	client, err := query.NewClient(ctx, natsClient, config)
//
// Basic entity operations:
//
//	// Get single entity
//	entity, err := client.GetEntity(ctx, "org.platform.domain.system.type.instance")
//
//	// Get multiple entities
//	entities, err := client.GetEntitiesBatch(ctx, []string{id1, id2, id3})
//
//	// Get entities by type (extracted from ID)
//	drones, err := client.GetEntitiesByType(ctx, "drone")
//
// Relationship queries:
//
//	// Get outgoing relationships by predicate
//	targets, err := client.GetOutgoingRelationships(ctx, entityID, "controls")
//
//	// Get incoming relationships (from INCOMING_INDEX)
//	sources, err := client.GetIncomingRelationships(ctx, entityID)
//
//	// Get all connected entities (both directions)
//	connected, err := client.GetEntityConnections(ctx, entityID)
//
//	// Verify specific relationship exists
//	exists, err := client.VerifyRelationship(ctx, fromID, toID, "controls")
//
// Bounded path traversal for edge devices:
//
//	result, err := client.ExecutePathQuery(ctx, query.PathQuery{
//	    StartEntity:     "org.platform.domain.system.drone.001",
//	    MaxDepth:        3,           // Maximum hops
//	    MaxNodes:        100,         // Maximum entities to visit
//	    MaxTime:         5*time.Second,
//	    PredicateFilter: []string{"controls", "monitors"}, // Empty = all
//	    DecayFactor:     0.8,         // Relevance decay per hop
//	    MaxPaths:        50,          // Limit path explosion
//	})
//
// Spatial queries:
//
//	// Get entities in geohash region
//	entities, err := client.GetEntitiesInRegion(ctx, "9q8yy")
//
// # Path Query
//
// The PathQuery performs bounded graph traversal with configurable limits:
//
// Parameters:
//   - StartEntity: Entity ID to begin traversal from
//   - MaxDepth: Maximum hop count (prevents infinite recursion)
//   - MaxNodes: Maximum entities to visit (memory bound)
//   - MaxTime: Query timeout (prevents runaway queries)
//   - PredicateFilter: Relationship predicates to follow (empty = all)
//   - DecayFactor: Score decay per hop (0.0-1.0)
//   - MaxPaths: Maximum complete paths to track (0 = unlimited)
//
// The result includes:
//   - Entities: All visited entity states
//   - Paths: Complete paths from start to leaves
//   - Scores: Relevance scores with decay applied
//   - Truncated: Whether query hit resource limits
//
// # Caching
//
// The client uses a hybrid cache (LRU + TTL) for entity states:
//
//	EntityCache:
//	    Strategy:        "hybrid"     # LRU eviction with TTL expiry
//	    MaxSize:         1000         # Maximum cached entities
//	    TTL:             5m           # Time-to-live
//	    CleanupInterval: 1m           # Background cleanup interval
//
// Cache statistics are available via GetCacheStats().
//
// # Configuration
//
// Client configuration:
//
//	EntityCache:              # See cache.Config
//	AllowUngatedReads: false  # Readiness-gate escape hatch (standalone deploys only)
//
// The client carries NO bucket configuration: it is a reader, binding
// ENTITY_STATES / SPATIAL_INDEX / INCOMING_INDEX must-exist through the
// framework KV catalog. An absent bucket yields a classified not-ready error
// naming the owning component; bucket shape (History, retention) is declared
// once in the catalog and enforced by each owner.
//
// # Thread Safety
//
// The Client is safe for concurrent use. Bucket initialization uses mutex protection,
// and cache operations are atomic.
//
// # Metrics
//
// When created with NewClientWithMetrics, the client exports cache metrics under
// the "query_client" namespace.
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/graph]: EntityState type
//   - [github.com/c360studio/semstreams/processor/graph-ingest]: Entity persistence
//   - [github.com/c360studio/semstreams/pkg/cache]: Cache implementation
//   - [github.com/c360studio/semstreams/natsclient]: NATS KV operations
package query
