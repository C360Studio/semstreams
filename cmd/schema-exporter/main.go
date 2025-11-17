package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
)

func main() {
	// Parse command-line flags
	registryPkg := flag.String("registry", "./componentregistry", "Package containing RegisterAll()")
	outDir := flag.String("out", "./schemas", "Output directory for schemas")
	openapiOut := flag.String("openapi", "./specs/openapi.v3.yaml", "Output path for OpenAPI spec")
	flag.Parse()

	log.Printf("Schema Exporter")
	log.Printf("  Registry: %s", *registryPkg)
	log.Printf("  Output dir: %s", *outDir)
	log.Printf("  OpenAPI spec: %s", *openapiOut)

	// Initialize component registry
	registry := component.NewRegistry()

	// Register all components
	if err := componentregistry.Register(registry); err != nil {
		log.Fatalf("Failed to register components: %v", err)
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

	// Extract and write schemas
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

		log.Printf("  ✓ Generated: %s", outFile)
	}

	// Generate OpenAPI spec
	if *openapiOut != "" {
		openapiDir := filepath.Dir(*openapiOut)
		if err := os.MkdirAll(openapiDir, 0755); err != nil {
			log.Fatalf("Failed to create OpenAPI directory: %v", err)
		}

		openapi := generateOpenAPISpec(componentSchemas, *outDir)
		if err := writeYAMLFile(*openapiOut, openapi); err != nil {
			log.Fatalf("Failed to write OpenAPI spec: %v", err)
		}

		log.Printf("  ✓ Generated OpenAPI spec: %s", *openapiOut)
	}

	log.Printf("✅ Schema generation complete!")
}

// ComponentSchema represents the exported component schema
type ComponentSchema struct {
	Schema      string                       `json:"$schema"`
	ID          string                       `json:"$id"`
	Type        string                       `json:"type"`
	Title       string                       `json:"title"`
	Description string                       `json:"description"`
	Properties  map[string]PropertySchema    `json:"properties"`
	Required    []string                     `json:"required"`
	Metadata    ComponentMetadata            `json:"x-component-metadata"`
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
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *int     `json:"minimum,omitempty"`
	Maximum     *int     `json:"maximum,omitempty"`
	Items       *PropertySchema `json:"items,omitempty"` // For array types
}

// extractSchema converts a component registration to a JSON Schema
func extractSchema(name string, registration *component.Registration) ComponentSchema {
	// Convert component.PropertySchema to JSON Schema PropertySchema
	properties := make(map[string]PropertySchema)
	for propName, propSchema := range registration.Schema.Properties {
		jsonSchemaProp := PropertySchema{
			Type:        mapTypeToJSONSchema(propSchema.Type),
			Description: propSchema.Description,
			Default:     propSchema.Default,
			Enum:        propSchema.Enum,
			Minimum:     propSchema.Minimum,
			Maximum:     propSchema.Maximum,
		}

		// Handle array types
		if propSchema.Type == "array" {
			jsonSchemaProp.Items = &PropertySchema{
				Type: "string", // Default to string items, can be enhanced later
			}
		}

		properties[propName] = jsonSchemaProp
	}

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

// mapTypeToJSONSchema maps component property types to JSON Schema types
func mapTypeToJSONSchema(propType string) string {
	switch propType {
	case "int", "float":
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
