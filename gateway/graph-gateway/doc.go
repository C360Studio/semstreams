// Package graphgateway provides the graph-gateway component for exposing
// graph operations via HTTP protocols including GraphQL and MCP.
//
// # Overview
//
// The graph-gateway component serves as the external access layer for the
// knowledge graph. It provides HTTP endpoints for querying entities, triples,
// and graph analytics, as well as submitting mutations via NATS.
//
// # Component Interface
//
// This component implements the semstreams component framework:
//   - component.Discoverable (6 methods): Meta, InputPorts, OutputPorts,
//     ConfigSchema, Health, DataFlow
//   - component.LifecycleComponent (3 methods): Initialize, Start, Stop
//   - gateway.Gateway (1 method): RegisterHTTPHandlers
//
// # Communication Patterns
//
// Inputs:
//   - HTTP requests on /graphql: GraphQL queries and mutations
//   - HTTP requests on /mcp: Model Context Protocol operations
//
// Outputs:
//   - NATS requests to graph.mutation.*: Mutations forwarded to graph-ingest
//
// Internal Reads (via QueryManager):
//   - ENTITY_STATES: Entity data
//   - OUTGOING_INDEX, INCOMING_INDEX: Graph traversal
//   - ALIAS_INDEX, PREDICATE_INDEX: Lookups
//   - COMMUNITY_INDEX: Clustering results (semantic tier)
//   - ANOMALY_INDEX: Structural anomalies for inference endpoints
//
// # HTTP Endpoints
//
// GraphQL (/graphql):
//   - Query entities, triples, and relationships
//   - Perform graph traversals and analytics
//   - Submit mutations (forwarded to graph-ingest via NATS)
//
// MCP (/mcp):
//   - Model Context Protocol for LLM tool integration
//   - Provides structured graph operations for AI agents
//
// Inference (/inference/*):
//   - List pending anomalies for human review
//   - Get anomaly details and submit review decisions
//   - View inference statistics
//
// Playground (/ when enabled):
//   - Interactive GraphQL IDE for development
//
// # Configuration
//
// Key configuration options:
//   - graphql_path: GraphQL endpoint path (default: /graphql)
//   - mcp_path: MCP endpoint path (default: /mcp)
//   - bind_address: HTTP server address (default: localhost:8080)
//   - enable_playground: Enable GraphQL playground (default: false)
//
// # Tiered Deployment
//
// The graph-gateway component is typically required in all tiers as the
// external access point. In production, it should be deployed behind a
// load balancer with appropriate authentication.
//
// # Usage
//
//	// Register the component
//	registry := component.NewRegistry()
//	graphgateway.Register(registry)
//
//	// Create via factory
//	comp, err := graphgateway.CreateGraphGateway(configJSON, deps)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Register HTTP handlers
//	mux := http.NewServeMux()
//	comp.(gateway.Gateway).RegisterHTTPHandlers("/api", mux)
//
//	// Lifecycle management
//	comp.Initialize()
//	comp.Start(ctx)
//	defer comp.Stop(5 * time.Second)
package graphgateway
