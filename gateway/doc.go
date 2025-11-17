// Package gateway provides bidirectional protocol bridging for SemStreams.
//
// Gateway components enable external clients (HTTP, WebSocket, gRPC) to query
// and interact with the NATS-based SemStreams event bus using request/reply patterns.
//
// # Gateway vs Output
//
// - Gateway: Bidirectional request/reply (External ↔ NATS ↔ External)
// - Output: Unidirectional push (NATS → External)
//
// # Architecture
//
// Gateways translate between external protocols and NATS request/reply:
//
//	┌─────────────────┐
//	│  HTTP Client    │  GET /api/search/semantic?query=foo
//	└────────┬────────┘
//	         ↓
//	┌────────────────────────────────────────┐
//	│  ServiceManager (Port 8080)            │
//	│  /api-gateway/* → HTTPGateway handlers │
//	└────────┬───────────────────────────────┘
//	         ↓ NATS Request/Reply
//	┌────────────────────────────────────────┐
//	│  graph-processor Component             │
//	│  Subscribed to graph.query.semantic    │
//	└────────────────────────────────────────┘
//
// # Mutation Control
//
// HTTP methods (GET, POST, PUT, DELETE) don't directly map to mutation semantics
// (e.g., POST is used for complex queries like semantic search). Mutation control
// should be enforced at the NATS subject/component level, not the HTTP gateway.
//
// # Protocol Support
//
// Gateway implementations by protocol:
//
//   - HTTP: REST/JSON request/reply (gateway/http/)
//   - WebSocket: Bidirectional messaging (future)
//   - gRPC: RPC request/reply (future)
//
// # Handler Registration
//
// Gateways register HTTP handlers via ServiceManager's central HTTP server:
//
//	type Gateway interface {
//	    component.Discoverable
//	    RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
//	}
//
// ServiceManager discovers Gateway implementers and registers their handlers
// at startup using the component instance name as URL prefix.
//
// # Example Configuration
//
//	{
//	  "components": {
//	    "api-gateway": {
//	      "type": "gateway",
//	      "name": "http",
//	      "enabled": true,
//	      "config": {
//	        "routes": [
//	          {
//	            "path": "/search/semantic",
//	            "method": "POST",
//	            "nats_subject": "graph.query.semantic",
//	            "timeout": "5s"
//	          },
//	          {
//	            "path": "/entity/:id",
//	            "method": "GET",
//	            "nats_subject": "graph.query.entity",
//	            "timeout": "2s"
//	          }
//	        ]
//	      }
//	    }
//	  }
//	}
//
// # Usage
//
// With the above configuration, external clients can query via HTTP:
//
//	# Semantic search
//	curl -X POST http://localhost:8080/api-gateway/search/semantic \
//	  -H "Content-Type: application/json" \
//	  -d '{"query": "emergency alert", "limit": 10}'
//
//	# Entity lookup
//	curl http://localhost:8080/api-gateway/entity/abc123
//
// # Security
//
// Gateways support:
//   - TLS encryption (via ServiceManager HTTP server)
//   - CORS headers
//   - Request timeout limits
//   - Rate limiting (future)
//   - Authentication/authorization (future)
package gateway
