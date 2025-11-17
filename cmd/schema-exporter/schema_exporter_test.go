package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// TestSchemaGeneration tests the complete schema generation pipeline
func TestSchemaGeneration(t *testing.T) {
	// Create temporary directory for test output
	tempDir := t.TempDir()
	schemasDir := filepath.Join(tempDir, "schemas")
	specsDir := filepath.Join(tempDir, "specs")
	openapiPath := filepath.Join(specsDir, "openapi.v3.yaml")

	// Create directories
	if err := os.MkdirAll(schemasDir, 0755); err != nil {
		t.Fatalf("Failed to create schemas directory: %v", err)
	}
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		t.Fatalf("Failed to create specs directory: %v", err)
	}

	// Initialize registry and register components
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Failed to register components: %v", err)
	}

	// Get all registered factories
	factories := registry.ListFactories()
	if len(factories) == 0 {
		t.Fatal("No component factories registered")
	}

	// Extract and write schemas
	var componentSchemas []ComponentSchema
	for name, registration := range factories {
		schema := extractSchema(name, registration)

		// Validate schema structure
		if schema.Schema != "http://json-schema.org/draft-07/schema#" {
			t.Errorf("Component %s: invalid $schema value: %s", name, schema.Schema)
		}
		if schema.ID != name+".v1.json" {
			t.Errorf("Component %s: invalid $id value: %s", name, schema.ID)
		}
		if schema.Type != "object" {
			t.Errorf("Component %s: invalid type value: %s", name, schema.Type)
		}
		if schema.Required == nil {
			t.Errorf("Component %s: required field should not be nil", name)
		}

		componentSchemas = append(componentSchemas, schema)

		// Write schema file
		outFile := filepath.Join(schemasDir, schema.ID)
		if err := writeJSONSchema(outFile, schema); err != nil {
			t.Fatalf("Failed to write schema for %s: %v", name, err)
		}
	}

	// Verify schema files exist and are valid JSON
	for _, schema := range componentSchemas {
		schemaFile := filepath.Join(schemasDir, schema.ID)

		// Check file exists
		if _, err := os.Stat(schemaFile); err != nil {
			t.Errorf("Schema file not found: %s", schemaFile)
			continue
		}

		// Verify valid JSON
		data, err := os.ReadFile(schemaFile)
		if err != nil {
			t.Errorf("Failed to read schema file %s: %v", schemaFile, err)
			continue
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("Schema file %s is not valid JSON: %v", schemaFile, err)
		}
	}

	// Generate OpenAPI spec
	openapi := generateOpenAPISpec(componentSchemas, schemasDir)
	if err := writeYAMLFile(openapiPath, openapi); err != nil {
		t.Fatalf("Failed to write OpenAPI spec: %v", err)
	}

	// Verify OpenAPI spec exists and is valid YAML
	if _, err := os.Stat(openapiPath); err != nil {
		t.Fatalf("OpenAPI spec file not found: %s", openapiPath)
	}

	data, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("OpenAPI spec is not valid YAML: %v", err)
	}

	// Verify OpenAPI spec structure
	if openapi.OpenAPI != "3.0.3" {
		t.Errorf("Invalid OpenAPI version: %s", openapi.OpenAPI)
	}
	if openapi.Info.Title == "" {
		t.Error("OpenAPI spec missing title")
	}
	if len(openapi.Paths) == 0 {
		t.Error("OpenAPI spec has no paths")
	}
	if len(openapi.Components.Schemas) == 0 {
		t.Error("OpenAPI spec has no component schemas")
	}
}

// TestSchemaValidationWithMetaSchema tests schema validation against meta-schema
func TestSchemaValidationWithMetaSchema(t *testing.T) {
	// Try to find meta-schema
	metaSchemaPath, err := loadMetaSchemaPath()
	if err != nil {
		t.Skipf("Meta-schema not found, skipping validation test: %v", err)
	}

	// Initialize registry
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Failed to register components: %v", err)
	}

	// Test each component schema
	factories := registry.ListFactories()
	for name, registration := range factories {
		t.Run(name, func(t *testing.T) {
			schema := extractSchema(name, registration)

			// Validate against meta-schema
			if err := validateSchema(schema, metaSchemaPath); err != nil {
				t.Errorf("Schema validation failed for %s: %v", name, err)
			}
		})
	}
}

// TestMetaSchemaValidity tests that the meta-schema itself is valid JSON Schema
func TestMetaSchemaValidity(t *testing.T) {
	metaSchemaPath, err := loadMetaSchemaPath()
	if err != nil {
		t.Skipf("Meta-schema not found, skipping: %v", err)
	}

	// Load meta-schema
	data, err := os.ReadFile(metaSchemaPath)
	if err != nil {
		t.Fatalf("Failed to read meta-schema: %v", err)
	}

	// Verify valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Meta-schema is not valid JSON: %v", err)
	}

	// Validate meta-schema against JSON Schema draft-07
	draft07SchemaURL := "http://json-schema.org/draft-07/schema#"
	metaSchemaLoader := gojsonschema.NewReferenceLoader(draft07SchemaURL)
	documentLoader := gojsonschema.NewBytesLoader(data)

	result, err := gojsonschema.Validate(metaSchemaLoader, documentLoader)
	if err != nil {
		t.Fatalf("Failed to validate meta-schema: %v", err)
	}

	if !result.Valid() {
		t.Error("Meta-schema validation failed:")
		for _, desc := range result.Errors() {
			t.Errorf("  - %s: %s", desc.Field(), desc.Description())
		}
	}
}

// TestExtractSchema tests the schema extraction logic
func TestExtractSchema(t *testing.T) {
	// Create a test registration
	testReg := &component.Registration{
		Description: "Test component",
		Type:        "processor",
		Protocol:    "test",
		Domain:      "testing",
		Version:     "1.0.0",
		Schema: component.ConfigSchema{
			Properties: map[string]component.PropertySchema{
				"testProp": {
					Type:        "string",
					Description: "Test property",
					Default:     "default value",
				},
				"numberProp": {
					Type:        "int",
					Description: "Number property",
					Minimum:     intPtr(0),
					Maximum:     intPtr(100),
				},
			},
			Required: []string{"testProp"},
		},
	}

	schema := extractSchema("test-component", testReg)

	// Verify schema structure
	if schema.Schema != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("Invalid $schema: %s", schema.Schema)
	}
	if schema.ID != "test-component.v1.json" {
		t.Errorf("Invalid $id: %s", schema.ID)
	}
	if schema.Type != "object" {
		t.Errorf("Invalid type: %s", schema.Type)
	}
	if len(schema.Properties) != 2 {
		t.Errorf("Expected 2 properties, got %d", len(schema.Properties))
	}
	if len(schema.Required) != 1 {
		t.Errorf("Expected 1 required field, got %d", len(schema.Required))
	}
	if schema.Metadata.Name != "test-component" {
		t.Errorf("Invalid metadata name: %s", schema.Metadata.Name)
	}
}

// TestMapTypeToJSONSchema tests the type mapping function
func TestMapTypeToJSONSchema(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"string", "string"},
		{"int", "number"},
		{"float", "number"},
		{"bool", "boolean"},
		{"array", "array"},
		{"object", "object"},
		{"unknown", "string"}, // Default to string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapTypeToJSONSchema(tt.input)
			if result != tt.expected {
				t.Errorf("mapTypeToJSONSchema(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
