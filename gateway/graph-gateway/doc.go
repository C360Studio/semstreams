// Package graphgateway provides a hand-written GraphQL-shaped graph facade. It
// is not a general schema executor or selection-set projector. Its
// registered /mcp route is a placeholder, not an MCP graph-read contract.
//
// # Overview
//
// The graph-gateway component serves as the external access layer for the
// knowledge graph. Its query-only handler classifies supported root operations
// and routes them to NATS query/index responders.
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
//   - HTTP requests on /graphql: bounded facade operations
//   - HTTP requests on /mcp: reserved placeholder response, no MCP graph tools
//
// Outputs:
//   - Classified requests to query/index NATS subjects
//   - No mutation API; the declared mutation output port is unused debt
//
// # HTTP Endpoints
//
// GraphQL-shaped facade (/graphql):
//   - Routes a bounded, hand-written set of graph operations
//   - Uses custom argument parsing and advertised introspection
//   - Does not apply selection-set projection to NATS JSON responses
//   - Reports mutationType nil and forwards no graph mutations
//
// Reserved placeholder (/mcp):
//   - Returns a stub response
//   - Does not implement MCP handshake, tools, or graph access
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
//   - mcp_path: reserved placeholder path (default: /mcp)
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
//	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer shutdownCancel()
//	_ = comp.Stop(shutdownCtx)
package graphgateway
