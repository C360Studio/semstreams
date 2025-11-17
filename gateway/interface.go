package gateway

import (
	"net/http"

	"github.com/c360/semstreams/component"
)

// Gateway defines the interface for protocol bridge components that enable
// external clients to interact with NATS-based services via request/reply.
//
// Gateway components are bidirectional - they accept external requests,
// translate them to NATS requests, and return the NATS response to the client.
//
// This differs from Output components which are unidirectional (NATS → External).
type Gateway interface {
	// Discoverable interface provides component metadata, ports, schema, health
	component.Discoverable

	// RegisterHTTPHandlers registers the gateway's HTTP routes with the
	// ServiceManager's central HTTP server.
	//
	// The prefix parameter is the URL path prefix for this gateway instance,
	// typically derived from the component instance name (e.g., "/api-gateway/").
	//
	// The mux parameter is ServiceManager's http.ServeMux where handlers are registered.
	//
	// Example registration:
	//   func (g *HTTPGateway) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	//       mux.HandleFunc(prefix + "search/semantic", g.handleSemanticSearch)
	//       mux.HandleFunc(prefix + "entity/:id", g.handleEntity)
	//   }
	RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
}

// HTTPHandler is the interface for services/components that can register
// HTTP handlers with ServiceManager's central HTTP server.
//
// This interface is used by ServiceManager to discover components that
// need HTTP endpoint exposure.
type HTTPHandler interface {
	RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
}
