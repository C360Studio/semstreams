package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPIDocument represents the complete OpenAPI 3.0 specification
type OpenAPIDocument struct {
	OpenAPI    string                  `yaml:"openapi"`
	Info       InfoObject              `yaml:"info"`
	Servers    []ServerObject          `yaml:"servers"`
	Paths      map[string]PathItem     `yaml:"paths"`
	Components ComponentsObject        `yaml:"components"`
	Tags       []TagObject             `yaml:"tags"`
}

// InfoObject contains API metadata
type InfoObject struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

// ServerObject defines an API server
type ServerObject struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

// PathItem describes operations available on a path
type PathItem struct {
	Get    *Operation `yaml:"get,omitempty"`
	Post   *Operation `yaml:"post,omitempty"`
	Put    *Operation `yaml:"put,omitempty"`
	Delete *Operation `yaml:"delete,omitempty"`
}

// Operation describes a single API operation
type Operation struct {
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description,omitempty"`
	Tags        []string            `yaml:"tags,omitempty"`
	Parameters  []Parameter         `yaml:"parameters,omitempty"`
	Responses   map[string]Response `yaml:"responses"`
}

// Parameter describes an operation parameter
type Parameter struct {
	Name        string      `yaml:"name"`
	In          string      `yaml:"in"` // "query", "path", "header"
	Required    bool        `yaml:"required,omitempty"`
	Description string      `yaml:"description,omitempty"`
	Schema      SchemaRef   `yaml:"schema"`
}

// Response describes an operation response
type Response struct {
	Description string                `yaml:"description"`
	Content     map[string]MediaType  `yaml:"content,omitempty"`
}

// MediaType describes a media type and schema
type MediaType struct {
	Schema SchemaRef `yaml:"schema"`
}

// SchemaRef references a schema
type SchemaRef struct {
	Ref   string      `yaml:"$ref,omitempty"`
	Type  string      `yaml:"type,omitempty"`
	Items *SchemaRef  `yaml:"items,omitempty"`
	OneOf []SchemaRef `yaml:"oneOf,omitempty"`
}

// ComponentsObject holds reusable objects
type ComponentsObject struct {
	Schemas map[string]interface{} `yaml:"schemas"`
}

// TagObject defines an API tag
type TagObject struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// generateOpenAPISpec generates an OpenAPI 3.0 specification from component schemas
func generateOpenAPISpec(components []ComponentSchema, schemaDir string) OpenAPIDocument {
	return OpenAPIDocument{
		OpenAPI: "3.0.3",
		Info: InfoObject{
			Title:       "SemStreams Component API",
			Description: "HTTP API for component discovery, configuration, and flow management",
			Version:     "1.0.0",
		},
		Servers: []ServerObject{
			{URL: "http://localhost:8080", Description: "Development server"},
			{URL: "http://localhost", Description: "Production server (via reverse proxy)"},
		},
		Paths: buildPaths(),
		Components: ComponentsObject{
			Schemas: buildComponentSchemas(components, schemaDir),
		},
		Tags: []TagObject{
			{Name: "Components", Description: "Component management endpoints"},
			{Name: "FlowGraph", Description: "Flow analysis and validation endpoints"},
		},
	}
}

// buildPaths creates the OpenAPI paths for component endpoints
func buildPaths() map[string]PathItem {
	return map[string]PathItem{
		"/components/types": {
			Get: &Operation{
				Summary:     "List available component types",
				Description: "Returns array of component metadata including schemas",
				Tags:        []string{"Components"},
				Responses: map[string]Response{
					"200": {
						Description: "Array of component types",
						Content: map[string]MediaType{
							"application/json": {
								Schema: SchemaRef{
									Type: "array",
									Items: &SchemaRef{
										Ref: "#/components/schemas/ComponentType",
									},
								},
							},
						},
					},
				},
			},
		},
		"/components/types/{id}": {
			Get: &Operation{
				Summary:     "Get component type by ID",
				Description: "Returns metadata and schema for a specific component type",
				Tags:        []string{"Components"},
				Parameters: []Parameter{
					{
						Name:        "id",
						In:          "path",
						Required:    true,
						Description: "Component type ID",
						Schema:      SchemaRef{Type: "string"},
					},
				},
				Responses: map[string]Response{
					"200": {
						Description: "Component type metadata",
						Content: map[string]MediaType{
							"application/json": {
								Schema: SchemaRef{
									Ref: "#/components/schemas/ComponentType",
								},
							},
						},
					},
					"404": {
						Description: "Component type not found",
					},
				},
			},
		},
		"/components/status/{name}": {
			Get: &Operation{
				Summary:     "Get component status",
				Description: "Returns detailed status for a specific component instance",
				Tags:        []string{"Components"},
				Parameters: []Parameter{
					{
						Name:        "name",
						In:          "path",
						Required:    true,
						Description: "Component instance name",
						Schema:      SchemaRef{Type: "string"},
					},
				},
				Responses: map[string]Response{
					"200": {
						Description: "Component status",
					},
					"404": {
						Description: "Component not found",
					},
				},
			},
		},
		"/components/flowgraph": {
			Get: &Operation{
				Summary:     "Get component flow graph",
				Description: "Returns the complete flow graph with nodes and edges",
				Tags:        []string{"FlowGraph"},
				Responses: map[string]Response{
					"200": {
						Description: "Flow graph with nodes and edges",
					},
				},
			},
		},
		"/components/validate": {
			Get: &Operation{
				Summary:     "Validate component flow connectivity",
				Description: "Performs flow graph connectivity analysis",
				Tags:        []string{"FlowGraph"},
				Responses: map[string]Response{
					"200": {
						Description: "Flow connectivity analysis results",
					},
				},
			},
		},
	}
}

// buildComponentSchemas creates the OpenAPI component schemas
func buildComponentSchemas(components []ComponentSchema, schemaDir string) map[string]interface{} {
	// Build oneOf array with references to all component schemas
	var schemaRefs []SchemaRef
	for _, comp := range components {
		// Use relative path from OpenAPI spec to schema files
		// OpenAPI spec is in specs/, schemas are in schemas/ (siblings)
		schemaRefs = append(schemaRefs, SchemaRef{
			Ref: fmt.Sprintf("../%s/%s", filepath.Base(schemaDir), comp.ID),
		})
	}

	schemas := map[string]interface{}{
		"ComponentType": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":          map[string]string{"type": "string", "description": "Component ID"},
				"name":        map[string]string{"type": "string", "description": "Human-readable name"},
				"type":        map[string]string{"type": "string", "description": "Component type (input/processor/output/storage)"},
				"protocol":    map[string]string{"type": "string", "description": "Technical protocol (udp, tcp, etc.)"},
				"domain":      map[string]string{"type": "string", "description": "Business domain (robotics, semantic, etc.)"},
				"description": map[string]string{"type": "string", "description": "Component description"},
				"version":     map[string]string{"type": "string", "description": "Component version"},
				"category":    map[string]string{"type": "string", "description": "Component category"},
				"schema": map[string]interface{}{
					"description": "Component configuration schema",
					"oneOf":       schemaRefs,
				},
			},
			"required": []string{"id", "name", "type"},
		},
	}

	return schemas
}

// writeYAMLFile writes a struct to a YAML file
func writeYAMLFile(filename string, data interface{}) error {
	// Marshal to YAML with proper indentation
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	// Add header comment
	header := []byte(strings.TrimSpace(`
# OpenAPI 3.0 Specification for SemStreams Component API
# Generated by schema-exporter tool
# DO NOT EDIT MANUALLY - This file is auto-generated from component registrations
`) + "\n\n")

	content := append(header, yamlData...)

	if err := os.WriteFile(filename, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
