// Package graphql provides a GraphQL gateway component for SemStreams.
//
// The GraphQL gateway enables schema-driven, token-efficient querying of NATS-based
// services through GraphQL. This solves the under/over-fetch problem common in
// agentic AI workflows, reducing token usage by up to 99% compared to REST APIs.
//
// # Architecture
//
// The gateway consists of two phases:
//
// Phase 1 (Current): Generic Infrastructure
//   - HTTP server with GraphQL Playground
//   - NATS integration layer for request/reply
//   - Base resolver with generic query methods
//   - Error mapping (NATS → GraphQL)
//   - Property extraction helpers
//
// Phase 2 (Future): Code Generation
//   - gqlgen-based schema code generation
//   - Domain-specific resolvers
//   - Type-safe query execution
//   - DataLoader for N+1 query prevention
//   - Subscription support
//
// # Usage
//
// Configuration example:
//
//	{
//	  "name": "my-graphql-gateway",
//	  "type": "graphql",
//	  "config": {
//	    "bind_address": ":8080",
//	    "path": "/graphql",
//	    "enable_playground": true,
//	    "enable_cors": true,
//	    "timeout": "30s",
//	    "nats_subjects": {
//	      "entity_query": "graph.query.entity",
//	      "entities_query": "graph.query.entities",
//	      "relationship_query": "graph.query.relationships",
//	      "semantic_search": "graph.query.semantic"
//	    }
//	  }
//	}
//
// # NATS Integration
//
// The gateway communicates with backend services via NATS request/reply:
//
//	Entity Query:        graph.query.entity       (single entity by ID)
//	Entities Query:      graph.query.entities     (batch query by IDs)
//	Relationship Query:  graph.query.relationships (entity relationships)
//	Semantic Search:     graph.query.semantic      (vector similarity search)
//
// # GraphRAG Community Queries
//
// The gateway supports GraphRAG search patterns using hierarchical community detection:
//
//	GetCommunity:        Retrieve a specific community by ID
//	GetEntityCommunity:  Get the community containing a specific entity at a level
//	LocalSearch:         Search within an entity's community (fast, focused)
//	GlobalSearch:        Search across community summaries (comprehensive)
//
// Community queries require QueryManager backend (not available via NATS).
//
// Example - Get community details:
//
//	query {
//	  community(id: "community-1") {
//	    id
//	    level
//	    members
//	    summary
//	    keywords
//	    summaryStatus
//	  }
//	}
//
// Example - Find entity's community:
//
//	query {
//	  entityCommunity(entityID: "entity-123", level: 1) {
//	    id
//	    summary
//	    keywords
//	  }
//	}
//
// Example - Local search (within community):
//
//	query {
//	  localSearch(entityID: "robot-1", query: "navigation", level: 1) {
//	    entities {
//	      id
//	      type
//	      properties
//	    }
//	    communityID
//	    count
//	  }
//	}
//
// Example - Global search (across communities):
//
//	query {
//	  globalSearch(query: "robotics", level: 1, maxCommunities: 5) {
//	    entities {
//	      id
//	      type
//	    }
//	    communitySummaries {
//	      communityID
//	      summary
//	      keywords
//	      relevance
//	    }
//	    count
//	  }
//	}
//
// Community Summary Status:
//   - "statistical"   - Fast TF-IDF based summary (~1ms)
//   - "llm-enhanced"  - LLM-generated summary (~3s, async)
//   - "llm-failed"    - LLM enhancement failed, statistical only
//
// # Error Handling
//
// NATS errors are mapped to GraphQL errors with appropriate error codes:
//
//	TIMEOUT             - Query timeout (retry recommended)
//	SERVICE_UNAVAILABLE - No NATS responders available
//	CONNECTION_CLOSED   - NATS connection lost
//	INVALID_INPUT       - Malformed query or parameters
//	INTERNAL_ERROR      - Server-side error
//
// # Property Extraction
//
// Generic property extraction helpers support dot notation for nested properties:
//
//	GetStringProp(entity, "metadata.title")
//	GetIntProp(entity, "count")
//	GetFloatProp(entity, "price")
//	GetBoolProp(entity, "active")
//	GetArrayProp(entity, "tags")
//	GetMapProp(entity, "metadata")
//
// # Performance
//
// The gateway is designed for high performance:
//   - Batch queries prevent N+1 problems
//   - Connection pooling for NATS
//   - Configurable timeouts and query depth limits
//   - CORS support for browser-based clients
//
// # Future Enhancements (Phase 2)
//
//   - GraphQL schema-driven code generation
//   - DataLoader pattern for efficient batching
//   - GraphQL subscriptions for real-time updates
//   - Custom directives for authorization
//   - Query complexity analysis
//   - APQ (Automatic Persisted Queries)
package graphql
