package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/google/go-cmp/cmp"
)

// TestCommittedSchemasMatchCode validates that committed schemas match generated schemas
// This ensures no schema drift between committed artifacts and source code
func TestCommittedSchemasMatchCode(t *testing.T) {
	// Get repository root
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	schemasDir := filepath.Join(repoRoot, "schemas")

	// Load committed schemas
	committedSchemas, err := loadCommittedSchemas(schemasDir)
	if err != nil {
		t.Fatalf("Failed to load committed schemas: %v", err)
	}

	if len(committedSchemas) == 0 {
		t.Fatal("No committed schemas found - expected at least one schema file")
	}

	// Initialize component registry
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Failed to register components: %v", err)
	}

	// Generate schemas from code
	factories := registry.ListFactories()
	if len(factories) == 0 {
		t.Fatal("No component factories registered")
	}

	// Compare each committed schema with generated schema
	for name := range committedSchemas {
		t.Run(name, func(t *testing.T) {
			committedSchema := committedSchemas[name]

			// Check if component is registered
			registration, exists := factories[name]
			if !exists {
				t.Fatalf("Component %s has committed schema but is not registered in code", name)
			}

			// Generate schema from code
			generatedSchema := extractSchemaFromRegistration(name, registration)

			// Compare schemas (deep equal)
			if diff := cmp.Diff(committedSchema, generatedSchema); diff != "" {
				t.Errorf("Schema mismatch for %s (-committed +generated):\n%s", name, diff)
				t.Errorf("\nThis indicates schema drift. Run 'task schema:generate' to update committed schemas.")
			}
		})
	}

	// Check for components registered but missing committed schemas
	for name := range factories {
		if _, exists := committedSchemas[name]; !exists {
			t.Errorf("Component %s is registered but has no committed schema file", name)
			t.Errorf("Run 'task schema:generate' to generate missing schema")
		}
	}
}

// TestCommittedSchemasValidStructure validates committed schema files have correct structure
func TestCommittedSchemasValidStructure(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	schemasDir := filepath.Join(repoRoot, "schemas")

	committedSchemas, err := loadCommittedSchemas(schemasDir)
	if err != nil {
		t.Fatalf("Failed to load committed schemas: %v", err)
	}

	for name, schema := range committedSchemas {
		t.Run(name, func(t *testing.T) {
			// Validate required JSON Schema fields
			if schema["$schema"] != "http://json-schema.org/draft-07/schema#" {
				t.Errorf("Invalid or missing $schema field")
			}

			id, ok := schema["$id"].(string)
			if !ok {
				t.Errorf("Missing or invalid $id field")
			} else if id != name+".v1.json" {
				t.Errorf("Invalid $id: expected %s.v1.json, got %s", name, id)
			}

			if schema["type"] != "object" {
				t.Errorf("Schema type should be 'object', got: %v", schema["type"])
			}

			// Validate component metadata exists
			metadata, ok := schema["x-component-metadata"].(map[string]interface{})
			if !ok {
				t.Errorf("Missing or invalid x-component-metadata")
			} else {
				// Check required metadata fields
				requiredFields := []string{"name", "type", "protocol", "domain", "version"}
				for _, field := range requiredFields {
					if _, exists := metadata[field]; !exists {
						t.Errorf("Missing required metadata field: %s", field)
					}
				}

				// Validate name matches
				if metaName, ok := metadata["name"].(string); ok && metaName != name {
					t.Errorf("Metadata name %s doesn't match component name %s", metaName, name)
				}
			}

			// Validate properties exist
			if _, ok := schema["properties"]; !ok {
				t.Errorf("Missing properties field")
			}

			// Validate required field exists (can be empty array)
			if _, ok := schema["required"]; !ok {
				t.Errorf("Missing required field")
			}
		})
	}
}

// TestNoOrphanedSchemaFiles ensures no schema files exist without corresponding components
func TestNoOrphanedSchemaFiles(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	schemasDir := filepath.Join(repoRoot, "schemas")

	// Get all schema files
	schemaFiles, err := filepath.Glob(filepath.Join(schemasDir, "*.v1.json"))
	if err != nil {
		t.Fatalf("Failed to glob schema files: %v", err)
	}

	// Initialize component registry
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Failed to register components: %v", err)
	}

	factories := registry.ListFactories()

	// Check each schema file has corresponding component
	for _, schemaPath := range schemaFiles {
		filename := filepath.Base(schemaPath)
		// Remove .v1.json suffix to get component name
		name := filename[:len(filename)-len(".v1.json")]

		if _, exists := factories[name]; !exists {
			t.Errorf("Orphaned schema file: %s (no corresponding component registered)", filename)
			t.Errorf("Remove this file or register the component")
		}
	}
}

// Helper functions

func findRepoRoot() (string, error) {
	// Check environment variable first
	if envRoot := os.Getenv("SEMSTREAMS_ROOT"); envRoot != "" {
		schemasPath := filepath.Join(envRoot, "schemas")
		if info, err := os.Stat(schemasPath); err == nil && info.IsDir() {
			return envRoot, nil
		}
		return "", &PathResolutionError{
			Message: "SEMSTREAMS_ROOT is set but schemas/ directory not found",
			Path:    schemasPath,
			Solutions: []string{
				"Verify SEMSTREAMS_ROOT points to semstreams repository root",
				"Run 'task schema:generate' to create schemas directory",
				"Unset SEMSTREAMS_ROOT to use automatic detection",
			},
		}
	}

	// Start from current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up until we find schemas/ directory or reach root
	dir := cwd
	for {
		schemasPath := filepath.Join(dir, "schemas")
		if info, err := os.Stat(schemasPath); err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding schemas/
			break
		}
		dir = parent
	}

	// If not found by walking up, assume we're in test/contract and go up two levels
	assumedRoot, _ := filepath.Abs(filepath.Join(cwd, "..", ".."))
	schemasPath := filepath.Join(assumedRoot, "schemas")

	if info, err := os.Stat(schemasPath); err == nil && info.IsDir() {
		return assumedRoot, nil
	}

	return "", &PathResolutionError{
		Message: "Could not find semstreams repository root",
		Path:    cwd,
		Solutions: []string{
			"Run tests from within semstreams repository",
			"Set SEMSTREAMS_ROOT environment variable",
			"Ensure schemas/ directory exists (run 'task schema:generate')",
		},
		Docs: "docs/CONTRACT_TESTING.md",
	}
}

// PathResolutionError provides clear error messages for path resolution failures
type PathResolutionError struct {
	Message   string
	Path      string
	Solutions []string
	Docs      string
}

func (e *PathResolutionError) Error() string {
	msg := e.Message + "\n\n"
	msg += "Current path: " + e.Path + "\n\n"
	msg += "Solutions:\n"
	for i, solution := range e.Solutions {
		msg += "  " + string(rune(i+1)) + ". " + solution + "\n"
	}
	if e.Docs != "" {
		msg += "\nFor more info, see: " + e.Docs + "\n"
	}
	return msg
}

func loadCommittedSchemas(schemasDir string) (map[string]map[string]interface{}, error) {
	// Check if schemas directory exists
	if info, err := os.Stat(schemasDir); err != nil || !info.IsDir() {
		return nil, &PathResolutionError{
			Message: "Schemas directory not found",
			Path:    schemasDir,
			Solutions: []string{
				"Run 'task schema:generate' to create schemas",
				"Verify repository structure is correct",
				"Set SEMSTREAMS_ROOT if running from unusual location",
			},
			Docs: "docs/SCHEMA_GENERATION.md",
		}
	}

	schemas := make(map[string]map[string]interface{})

	schemaFiles, err := filepath.Glob(filepath.Join(schemasDir, "*.v1.json"))
	if err != nil {
		return nil, &PathResolutionError{
			Message: "Failed to search for schema files",
			Path:    schemasDir,
			Solutions: []string{
				"Verify directory permissions",
				"Check that schemas directory is readable",
			},
		}
	}

	if len(schemaFiles) == 0 {
		return nil, &PathResolutionError{
			Message: "No schema files found in schemas directory",
			Path:    schemasDir,
			Solutions: []string{
				"Run 'task schema:generate' to generate schemas",
				"Ensure components are registered in componentregistry/register.go",
			},
			Docs: "docs/SCHEMA_GENERATION.md",
		}
	}

	for _, schemaPath := range schemaFiles {
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			return nil, &PathResolutionError{
				Message: "Failed to read schema file: " + filepath.Base(schemaPath),
				Path:    schemaPath,
				Solutions: []string{
					"Check file permissions",
					"Regenerate schemas with 'task schema:generate'",
				},
			}
		}

		var schema map[string]interface{}
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, &PathResolutionError{
				Message: "Invalid JSON in schema file: " + filepath.Base(schemaPath),
				Path:    schemaPath,
				Solutions: []string{
					"Regenerate schemas with 'task schema:generate'",
					"Do not manually edit schema files",
				},
				Docs: "docs/SCHEMA_GENERATION.md",
			}
		}

		// Extract component name from filename (remove .v1.json)
		filename := filepath.Base(schemaPath)
		name := filename[:len(filename)-len(".v1.json")]

		schemas[name] = schema
	}

	return schemas, nil
}

func extractSchemaFromRegistration(name string, reg *component.Registration) map[string]interface{} {
	schema := make(map[string]interface{})

	schema["$schema"] = "http://json-schema.org/draft-07/schema#"
	schema["$id"] = name + ".v1.json"
	schema["type"] = "object"
	schema["title"] = name + " Configuration"
	schema["description"] = reg.Description

	// Convert properties
	properties := make(map[string]interface{})
	for propName, propSchema := range reg.Schema.Properties {
		prop := make(map[string]interface{})
		prop["type"] = mapTypeToJSONSchema(propSchema.Type)
		prop["description"] = propSchema.Description

		if propSchema.Default != nil {
			prop["default"] = propSchema.Default
		}
		if propSchema.Minimum != nil {
			prop["minimum"] = *propSchema.Minimum
		}
		if propSchema.Maximum != nil {
			prop["maximum"] = *propSchema.Maximum
		}
		if len(propSchema.Enum) > 0 {
			prop["enum"] = propSchema.Enum
		}
		if propSchema.Category != "" {
			prop["category"] = propSchema.Category
		}

		properties[propName] = prop
	}
	schema["properties"] = properties

	// Required fields
	required := make([]interface{}, len(reg.Schema.Required))
	for i, r := range reg.Schema.Required {
		required[i] = r
	}
	schema["required"] = required

	// Component metadata
	metadata := map[string]interface{}{
		"name":     name,
		"type":     reg.Type,
		"protocol": reg.Protocol,
		"domain":   reg.Domain,
		"version":  reg.Version,
	}
	schema["x-component-metadata"] = metadata

	return schema
}

func mapTypeToJSONSchema(goType string) string {
	switch goType {
	case "string":
		return "string"
	case "int", "int64", "float", "float64":
		return "number"
	case "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string" // Default to string for unknown types
	}
}
