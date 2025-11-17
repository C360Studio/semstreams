// Package service provides OpenAPI specification types for HTTP endpoint documentation
package service

import "net/http"

// HTTPHandler is an optional interface for services that want to expose HTTP endpoints
type HTTPHandler interface {
	RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
	OpenAPISpec() *OpenAPISpec // Returns OpenAPI specification for this service
}

// OpenAPIDocument represents the complete OpenAPI 3.0 specification
type OpenAPIDocument struct {
	OpenAPI string              `json:"openapi"`
	Info    InfoSpec            `json:"info"`
	Servers []ServerSpec        `json:"servers"`
	Paths   map[string]PathSpec `json:"paths"`
	Tags    []TagSpec           `json:"tags,omitempty"`
}
