// Package service provides OpenAPI specification types for HTTP endpoint documentation
package service

import "encoding/json"

// OpenAPISpec represents a service's OpenAPI specification fragment
type OpenAPISpec struct {
	Paths      map[string]PathSpec `json:"paths"`
	Components map[string]any      `json:"components,omitempty"`
	Tags       []TagSpec           `json:"tags,omitempty"`
}

// PathSpec defines HTTP operations for a specific path
type PathSpec struct {
	GET    *OperationSpec `json:"get,omitempty"`
	POST   *OperationSpec `json:"post,omitempty"`
	PUT    *OperationSpec `json:"put,omitempty"`
	DELETE *OperationSpec `json:"delete,omitempty"`
}

// OperationSpec defines a single HTTP operation
type OperationSpec struct {
	Summary     string                  `json:"summary"`
	Description string                  `json:"description,omitempty"`
	Parameters  []ParameterSpec         `json:"parameters,omitempty"`
	Responses   map[string]ResponseSpec `json:"responses"`
	Tags        []string                `json:"tags,omitempty"`
}

// ParameterSpec defines an operation parameter
type ParameterSpec struct {
	Name        string `json:"name"`
	In          string `json:"in"` // "query", "path", "header"
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      Schema `json:"schema,omitempty"`
}

// ResponseSpec defines an operation response
type ResponseSpec struct {
	Description string `json:"description"`
	ContentType string `json:"content_type,omitempty"`
}

// Schema defines parameter or response schema
type Schema struct {
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
}

// InfoSpec contains API metadata
type InfoSpec struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// ServerSpec defines an API server
type ServerSpec struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// TagSpec defines an API tag for grouping operations
type TagSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// NewOpenAPISpec creates a new OpenAPI specification fragment for a service
func NewOpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Paths:      make(map[string]PathSpec),
		Components: make(map[string]any),
		Tags:       make([]TagSpec, 0),
	}
}

// AddPath adds a path specification to the OpenAPI spec
func (spec *OpenAPISpec) AddPath(path string, pathSpec PathSpec) {
	spec.Paths[path] = pathSpec
}

// AddTag adds a tag to the OpenAPI spec
func (spec *OpenAPISpec) AddTag(name, description string) {
	spec.Tags = append(spec.Tags, TagSpec{
		Name:        name,
		Description: description,
	})
}

// MarshalJSON implements json.Marshaler for OpenAPISpec
func (spec *OpenAPISpec) MarshalJSON() ([]byte, error) {
	type alias OpenAPISpec
	return json.Marshal((*alias)(spec))
}

// UnmarshalJSON implements json.Unmarshaler for OpenAPISpec
func (spec *OpenAPISpec) UnmarshalJSON(data []byte) error {
	type alias OpenAPISpec
	return json.Unmarshal(data, (*alias)(spec))
}
