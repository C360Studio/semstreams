// Package main provides a command-line tool for generating OpenAPI specifications.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
	"github.com/c360studio/semstreams/service"
)

func main() {
	// Parse command-line flags
	registryPkg := flag.String("registry", "./componentregistry", "Package containing RegisterAll()")
	outDir := flag.String("out", "./schemas", "Output directory for schemas")
	openapiOut := flag.String("openapi", "./specs/openapi.v3.yaml", "Output path for OpenAPI spec")
	includeGraphResearch := flag.Bool("graph-research", false, "Include the graph-research framework capability")
	includeOTEL := flag.Bool("otel", false, "Include the optional OpenTelemetry adapter")
	flag.Parse()

	log.Printf("OpenAPI Generator")
	log.Printf("  Registry: %s", *registryPkg)
	log.Printf("  Output dir: %s", *outDir)
	log.Printf("  OpenAPI spec: %s", *openapiOut)

	// Initialize component registry
	registry := component.NewRegistry()

	// Register all components
	if err := componentregistry.Register(registry); err != nil {
		log.Fatalf("Failed to register components: %v", err)
	}
	if *includeGraphResearch {
		if err := graphresearch.RegisterComponents(registry); err != nil {
			log.Fatalf("Failed to register graph research: %v", err)
		}
	}
	if *includeOTEL {
		if err := optionalotel.Register(registry); err != nil {
			log.Fatalf("Failed to register OpenTelemetry: %v", err)
		}
	}

	// Get all registered factories
	factories := registry.ListFactories()
	log.Printf("Found %d component types", len(factories))

	// Load meta-schema for validation
	metaSchemaPath, err := loadMetaSchemaPath()
	if err != nil {
		log.Printf("⚠️  Meta-schema not found, skipping validation: %v", err)
		metaSchemaPath = ""
	} else {
		log.Printf("Using meta-schema: %s", metaSchemaPath)
	}

	// Create output directory
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Extract and write component configuration schemas
	var componentSchemas []ComponentSchema
	for name, registration := range factories {
		schema := extractSchema(name, registration)

		// Validate schema against meta-schema
		if metaSchemaPath != "" {
			if err := validateSchema(schema, metaSchemaPath); err != nil {
				log.Fatalf("Schema validation failed for %s: %v", name, err)
			}
		}

		componentSchemas = append(componentSchemas, schema)

		// Write to versioned JSON file
		outFile := filepath.Join(*outDir, fmt.Sprintf("%s.v1.json", name))
		if err := writeJSONSchema(outFile, schema); err != nil {
			log.Fatalf("Failed to write schema for %s: %v", name, err)
		}

		log.Printf("  ✓ Generated component schema: %s", outFile)
	}

	// Note: Workflow definition schema generation removed - old workflow processor deprecated

	// Get all registered service OpenAPI specs
	serviceSpecs := service.GetAllOpenAPISpecs()
	log.Printf("Found %d service OpenAPI specs", len(serviceSpecs))

	// Generate OpenAPI spec
	if *openapiOut != "" {
		openapiDir := filepath.Dir(*openapiOut)
		if err := os.MkdirAll(openapiDir, 0755); err != nil {
			log.Fatalf("Failed to create OpenAPI directory: %v", err)
		}

		// Sort component schemas by ID for deterministic output
		sort.Slice(componentSchemas, func(i, j int) bool {
			return componentSchemas[i].ID < componentSchemas[j].ID
		})

		openapi := generateOpenAPISpec(componentSchemas, serviceSpecs, *outDir)

		// gh#228: fail fast on dangling $refs rather than writing a spec that
		// passes the drift gate but breaks downstream openapi-typescript codegen.
		if err := validateOpenAPIRefs(openapi); err != nil {
			log.Fatalf("OpenAPI ref validation failed: %v", err)
		}

		if err := writeYAMLFile(*openapiOut, openapi); err != nil {
			log.Fatalf("Failed to write OpenAPI spec: %v", err)
		}

		log.Printf("  ✓ Generated OpenAPI spec: %s", *openapiOut)
	}

	log.Printf("✅ OpenAPI generation complete!")
}

// ComponentSchema represents the exported component schema
type ComponentSchema struct {
	Schema      string                    `json:"$schema"`
	ID          string                    `json:"$id"`
	Type        string                    `json:"type"`
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	Properties  map[string]PropertySchema `json:"properties"`
	Required    []string                  `json:"required"`
	Metadata    ComponentMetadata         `json:"x-component-metadata"`
}

// ComponentMetadata holds component metadata for OpenAPI integration
type ComponentMetadata struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "input", "processor", "output", "storage"
	Protocol string `json:"protocol"` // "udp", "tcp", "websocket", etc.
	Domain   string `json:"domain"`   // "robotics", "semantic", "network", "storage"
	Version  string `json:"version"`
}

// PropertySchema represents a JSON Schema property definition
type PropertySchema struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description,omitempty"`
	Default     any                       `json:"default,omitempty"`
	Enum        []string                  `json:"enum,omitempty"`
	Minimum     *int                      `json:"minimum,omitempty"`
	Maximum     *int                      `json:"maximum,omitempty"`
	MinLength   *int                      `json:"minLength,omitempty"`
	MaxLength   *int                      `json:"maxLength,omitempty"`
	Pattern     string                    `json:"pattern,omitempty"`
	Items       *PropertySchema           `json:"items,omitempty"`      // For array types
	Category    string                    `json:"category,omitempty"`   // UI organization: "basic" or "advanced"
	Properties  map[string]PropertySchema `json:"properties,omitempty"` // Nested properties for object types
	Required    []string                  `json:"required,omitempty"`   // Required nested fields for object types
}

// extractSchema converts a component registration to a JSON Schema
func extractSchema(name string, registration *component.Registration) ComponentSchema {
	// Convert component.PropertySchema to JSON Schema PropertySchema
	properties := convertProperties(registration.Schema.Properties)

	// Ensure Required is an empty array instead of nil
	required := registration.Schema.Required
	if required == nil {
		required = []string{}
	}

	return ComponentSchema{
		Schema:      "http://json-schema.org/draft-07/schema#",
		ID:          fmt.Sprintf("%s.v1.json", name),
		Type:        "object",
		Title:       fmt.Sprintf("%s Configuration", name),
		Description: registration.Description,
		Properties:  properties,
		Required:    required,
		Metadata: ComponentMetadata{
			Name:     name,
			Type:     registration.Type,
			Protocol: registration.Protocol,
			Domain:   registration.Domain,
			Version:  registration.Version,
		},
	}
}

// convertProperties recursively converts component PropertySchema to JSON Schema PropertySchema
func convertProperties(props map[string]component.PropertySchema) map[string]PropertySchema {
	result := make(map[string]PropertySchema)
	for propName, propSchema := range props {
		jsonSchemaProp := PropertySchema{
			Type:        mapTypeToJSONSchema(propSchema.Type),
			Description: propSchema.Description,
			Default:     propSchema.Default,
			Enum:        propSchema.Enum,
			Minimum:     propSchema.Minimum,
			Maximum:     propSchema.Maximum,
			MinLength:   propSchema.MinLength,
			MaxLength:   propSchema.MaxLength,
			Pattern:     propSchema.Pattern,
			Category:    propSchema.Category,
		}

		// Handle array types
		if propSchema.Type == "array" {
			if propSchema.Items != nil {
				jsonSchemaProp.Items = convertPropertySchemaPtr(propSchema.Items)
			} else {
				jsonSchemaProp.Items = &PropertySchema{Type: "string"}
			}
		}

		// Handle nested object types - recursively convert properties
		if propSchema.Type == "object" && len(propSchema.Properties) > 0 {
			jsonSchemaProp.Properties = convertProperties(propSchema.Properties)
			if len(propSchema.Required) > 0 {
				jsonSchemaProp.Required = propSchema.Required
			}
		}

		result[propName] = jsonSchemaProp
	}
	return result
}

// convertPropertySchemaPtr converts a component.PropertySchema pointer to local PropertySchema
func convertPropertySchemaPtr(src *component.PropertySchema) *PropertySchema {
	if src == nil {
		return nil
	}
	result := &PropertySchema{
		Type:        mapTypeToJSONSchema(src.Type),
		Description: src.Description,
		Default:     src.Default,
		Enum:        src.Enum,
		Minimum:     src.Minimum,
		Maximum:     src.Maximum,
		MinLength:   src.MinLength,
		MaxLength:   src.MaxLength,
		Pattern:     src.Pattern,
	}
	if len(src.Properties) > 0 {
		result.Properties = convertProperties(src.Properties)
	}
	if len(src.Required) > 0 {
		result.Required = src.Required
	}
	if src.Items != nil {
		result.Items = convertPropertySchemaPtr(src.Items)
	}
	return result
}

// mapTypeToJSONSchema maps component property types to JSON Schema types.
// JSON Schema treats "integer" as a subtype of "number" — emitting
// "integer" for Go int fields gives downstream consumers (UI form
// builders, validators) the precision signal they need to reject
// fractional values like "2.5" for fire_every_n_events.
func mapTypeToJSONSchema(propType string) string {
	switch propType {
	case "int":
		return "integer"
	case "float":
		return "number"
	case "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

// writeJSONSchema writes a component schema to a JSON file
func writeJSONSchema(filename string, schema ComponentSchema) error {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// collectResponseTypes gathers all unique response types from service specs
func collectResponseTypes(specs map[string]*service.OpenAPISpec) []reflect.Type {
	seen := make(map[reflect.Type]bool)
	var types []reflect.Type

	for _, spec := range specs {
		for _, t := range spec.ResponseTypes {
			if !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}
	}

	return types
}
