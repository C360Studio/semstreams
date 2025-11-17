package contract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"gopkg.in/yaml.v3"
)

// OpenAPISpec represents the OpenAPI 3.0 specification structure
type OpenAPISpec struct {
	OpenAPI    string                       `yaml:"openapi"`
	Info       OpenAPIInfo                  `yaml:"info"`
	Paths      map[string]interface{}       `yaml:"paths"`
	Components OpenAPIComponents            `yaml:"components"`
}

type OpenAPIInfo struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

type OpenAPIComponents struct {
	Schemas map[string]interface{} `yaml:"schemas"`
}

// loadOpenAPISpec loads and parses the OpenAPI spec with clear error messages
func loadOpenAPISpec(t *testing.T) *OpenAPISpec {
	t.Helper()

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	openapiPath := filepath.Join(repoRoot, "specs", "openapi.v3.yaml")

	// Check file exists
	if _, err := os.Stat(openapiPath); err != nil {
		t.Fatalf(`
OpenAPI spec not found at: %s

Solutions:
  1. Run 'task schema:generate' to generate OpenAPI spec
  2. Verify specs/ directory exists
  3. Set SEMSTREAMS_ROOT if running from unusual location

For more info, see: docs/OPENAPI_INTEGRATION.md
`, openapiPath)
	}

	// Load and parse OpenAPI spec
	data, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	var spec OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf(`
Failed to parse OpenAPI spec: %v

This indicates corrupted or invalid YAML in the OpenAPI spec.

Solutions:
  1. Regenerate: task schema:generate
  2. Do not manually edit specs/openapi.v3.yaml
  3. Report issue if regeneration doesn't fix it

For more info, see: docs/SCHEMA_GENERATION.md
`, err)
	}

	return &spec
}

// TestCommittedOpenAPISpecValid validates the committed OpenAPI spec structure
func TestCommittedOpenAPISpecValid(t *testing.T) {
	spec := loadOpenAPISpec(t)

	// Validate OpenAPI version
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("Invalid OpenAPI version: expected 3.0.3, got %s", spec.OpenAPI)
	}

	// Validate info section
	if spec.Info.Title == "" {
		t.Error("OpenAPI spec missing title")
	}
	if spec.Info.Version == "" {
		t.Error("OpenAPI spec missing version")
	}

	// Validate paths exist
	if len(spec.Paths) == 0 {
		t.Error("OpenAPI spec has no paths defined")
	}

	// Validate components/schemas exist
	if len(spec.Components.Schemas) == 0 {
		t.Error("OpenAPI spec has no component schemas defined")
	}
}

// TestOpenAPISpecContainsAllComponents validates all registered components are in OpenAPI spec
func TestOpenAPISpecContainsAllComponents(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	openapiPath := filepath.Join(repoRoot, "specs", "openapi.v3.yaml")

	// Load OpenAPI spec as raw structure
	data, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	var spec map[string]interface{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	// Initialize component registry
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Failed to register components: %v", err)
	}

	factories := registry.ListFactories()

	// Extract schema references from ComponentType.schema.oneOf
	components, ok := spec["components"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing components section")
	}

	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing schemas section")
	}

	componentType, ok := schemas["ComponentType"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing ComponentType schema")
	}

	properties, ok := componentType["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("ComponentType missing properties field")
	}

	schemaField, ok := properties["schema"].(map[string]interface{})
	if !ok {
		t.Fatal("ComponentType.properties missing schema field")
	}

	oneOf, ok := schemaField["oneOf"].([]interface{})
	if !ok {
		t.Fatal("ComponentType.properties.schema missing oneOf array")
	}

	// Build set of referenced schema files
	referencedSchemas := make(map[string]bool)
	for _, refItem := range oneOf {
		refMap, ok := refItem.(map[string]interface{})
		if !ok {
			continue
		}
		refStr, ok := refMap["$ref"].(string)
		if !ok {
			continue
		}
		// Extract filename from path like "../schemas/udp.v1.json"
		filename := filepath.Base(refStr)
		// Remove .v1.json to get component name
		if len(filename) > 8 && filename[len(filename)-8:] == ".v1.json" {
			componentName := filename[:len(filename)-8]
			referencedSchemas[componentName] = true
		}
	}

	// Verify each registered component is referenced in OpenAPI spec
	for name := range factories {
		if !referencedSchemas[name] {
			t.Errorf("Component %s not referenced in OpenAPI spec ComponentType.schema.oneOf", name)
		}
	}

	// Verify no extra references
	for name := range referencedSchemas {
		if _, exists := factories[name]; !exists {
			t.Errorf("OpenAPI spec references schema %s but component is not registered", name)
		}
	}
}

// TestOpenAPISpecPaths validates required API paths exist in OpenAPI spec
func TestOpenAPISpecPaths(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	openapiPath := filepath.Join(repoRoot, "specs", "openapi.v3.yaml")

	// Load OpenAPI spec
	data, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	var spec map[string]interface{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing paths section")
	}

	// Verify required paths exist
	requiredPaths := []string{
		"/components/types",
		"/components/types/{id}",
		"/components/status/{name}",
		"/components/flowgraph",
		"/components/validate",
	}

	for _, path := range requiredPaths {
		if _, exists := paths[path]; !exists {
			t.Errorf("OpenAPI spec missing required path: %s", path)
		}
	}
}

// TestOpenAPISchemaReferences validates schema references point to existing files
func TestOpenAPISchemaReferences(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	openapiPath := filepath.Join(repoRoot, "specs", "openapi.v3.yaml")
	schemasDir := filepath.Join(repoRoot, "schemas")

	// Load OpenAPI spec as raw structure
	data, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	var spec map[string]interface{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	// Extract schema references from ComponentType.schema.oneOf
	components, ok := spec["components"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing components section")
	}

	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing schemas section")
	}

	componentType, ok := schemas["ComponentType"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAPI spec missing ComponentType schema")
	}

	properties, ok := componentType["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("ComponentType missing properties field")
	}

	schemaField, ok := properties["schema"].(map[string]interface{})
	if !ok {
		t.Fatal("ComponentType.properties missing schema field")
	}

	oneOf, ok := schemaField["oneOf"].([]interface{})
	if !ok {
		t.Fatal("ComponentType.properties.schema missing oneOf array")
	}

	// Verify each referenced schema file exists
	for _, refItem := range oneOf {
		refMap, ok := refItem.(map[string]interface{})
		if !ok {
			continue
		}
		refStr, ok := refMap["$ref"].(string)
		if !ok {
			continue
		}

		// Build full path to referenced file
		// Reference is like "../schemas/udp.v1.json"
		schemaPath := filepath.Join(schemasDir, filepath.Base(refStr))

		if _, err := os.Stat(schemaPath); err != nil {
			t.Errorf("OpenAPI spec references non-existent schema file: %s (resolved to %s)",
				refStr, schemaPath)
		}
	}
}

