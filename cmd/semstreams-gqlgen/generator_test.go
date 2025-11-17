package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {
					"robot": {
						"resolver": "QueryEntityByID",
						"subject": "graph.robot.get"
					}
				},
				"fields": {
					"Robot.id": {
						"property": "id",
						"type": "string"
					}
				},
				"types": {
					"Robot": {
						"entity_type": "robot"
					}
				}
			}`,
			wantErr: false,
		},
		{
			name: "missing package",
			config: `{
				"schema_path": "schema.graphql",
				"queries": {},
				"fields": {},
				"types": {}
			}`,
			wantErr: true,
		},
		{
			name: "invalid resolver",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {
					"robot": {
						"resolver": "InvalidResolver",
						"subject": "graph.robot.get"
					}
				},
				"fields": {},
				"types": {}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.json")
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			// Load config
			_, err := LoadConfig(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSchema(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		wantErr    bool
		wantQueries int
		wantTypes   int
	}{
		{
			name: "simple robot schema",
			schema: `
				type Query {
					robot(id: ID!): Robot
					robots(ids: [ID!]!): [Robot!]!
				}

				type Robot {
					id: ID!
					name: String!
					status: String
				}
			`,
			wantErr:    false,
			wantQueries: 2, // Only custom queries (may include introspection)
			wantTypes:   1, // Robot (Query is not counted in Types)
		},
		{
			name: "invalid schema",
			schema: `
				type Query {
					robot(id ID!): Robot  # Missing colon
				}
			`,
			wantErr:    true,
			wantQueries: 0,
			wantTypes:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp schema file
			tmpDir := t.TempDir()
			schemaPath := filepath.Join(tmpDir, "schema.graphql")
			if err := os.WriteFile(schemaPath, []byte(tt.schema), 0644); err != nil {
				t.Fatalf("failed to write schema: %v", err)
			}

			// Parse schema
			schema, err := ParseSchema(schemaPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Count only non-introspection queries
			nonIntrospectionQueries := 0
			for name := range schema.Queries {
				if !strings.HasPrefix(name, "__") {
					nonIntrospectionQueries++
				}
			}

			if nonIntrospectionQueries != tt.wantQueries {
				t.Errorf("ParseSchema() got %d queries, want %d", nonIntrospectionQueries, tt.wantQueries)
			}

			// Count only non-introspection types
			nonIntrospectionTypes := 0
			for name := range schema.Types {
				if !strings.HasPrefix(name, "__") && name != "Query" {
					nonIntrospectionTypes++
				}
			}

			if nonIntrospectionTypes != tt.wantTypes {
				t.Errorf("ParseSchema() got %d types, want %d", nonIntrospectionTypes, tt.wantTypes)
			}
		})
	}
}

func TestValidateConfigAgainstSchema(t *testing.T) {
	schema := `
		type Query {
			robot(id: ID!): Robot
		}

		type Robot {
			id: ID!
			name: String!
		}
	`

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {
					"robot": {
						"resolver": "QueryEntityByID",
						"subject": "graph.robot.get"
					}
				},
				"fields": {
					"Robot.id": {
						"property": "id",
						"type": "string"
					},
					"Robot.name": {
						"property": "properties.name",
						"type": "string"
					}
				},
				"types": {
					"Robot": {
						"entity_type": "robot"
					}
				}
			}`,
			wantErr: false,
		},
		{
			name: "query not in schema",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {
					"task": {
						"resolver": "QueryEntityByID",
						"subject": "graph.task.get"
					}
				},
				"fields": {},
				"types": {}
			}`,
			wantErr: true,
		},
		{
			name: "type not in schema",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {},
				"fields": {},
				"types": {
					"Task": {
						"entity_type": "task"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "field not in type",
			config: `{
				"package": "generated",
				"schema_path": "schema.graphql",
				"queries": {},
				"fields": {
					"Robot.unknown": {
						"property": "properties.unknown",
						"type": "string"
					}
				},
				"types": {}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()

			// Write schema
			schemaPath := filepath.Join(tmpDir, "schema.graphql")
			if err := os.WriteFile(schemaPath, []byte(schema), 0644); err != nil {
				t.Fatalf("failed to write schema: %v", err)
			}

			// Write config
			configPath := filepath.Join(tmpDir, "config.json")
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			// Load config and schema
			config, err := LoadConfig(configPath)
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}

			parsedSchema, err := ParseSchema(schemaPath)
			if err != nil {
				t.Fatalf("failed to parse schema: %v", err)
			}

			// Validate
			err = ValidateConfigAgainstSchema(config, parsedSchema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfigAgainstSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	schema := `
		type Query {
			robot(id: ID!): Robot
			robots(ids: [ID!]!): [Robot!]!
		}

		type Robot {
			id: ID!
			name: String!
			status: String
		}
	`

	configTemplate := `{
		"package": "generated",
		"schema_path": "%s",
		"queries": {
			"robot": {
				"resolver": "QueryEntityByID",
				"subject": "graph.robot.get"
			},
			"robots": {
				"resolver": "QueryEntitiesByIDs",
				"subject": "graph.robot.list"
			}
		},
		"fields": {
			"Robot.id": {
				"property": "id",
				"type": "string"
			},
			"Robot.name": {
				"property": "properties.name",
				"type": "string"
			},
			"Robot.status": {
				"property": "properties.status",
				"type": "string"
			}
		},
		"types": {
			"Robot": {
				"entity_type": "robot"
			}
		}
	}`

	t.Run("generate all files", func(t *testing.T) {
		// Create temp directory
		tmpDir := t.TempDir()

		// Write schema
		schemaPath := filepath.Join(tmpDir, "schema.graphql")
		if err := os.WriteFile(schemaPath, []byte(schema), 0644); err != nil {
			t.Fatalf("failed to write schema: %v", err)
		}

		// Write config with schema path
		configPath := filepath.Join(tmpDir, "config.json")
		config := fmt.Sprintf(configTemplate, schemaPath)
		if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		// Create output directory
		outputDir := filepath.Join(tmpDir, "generated")

		// Run generation
		err := runGenerate(configPath, outputDir, "")
		if err != nil {
			t.Fatalf("runGenerate() failed: %v", err)
		}

		// Verify files were created
		expectedFiles := []string{
			"generated_resolver.go",
			"generated_models.go",
			"generated_converters.go",
		}

		for _, file := range expectedFiles {
			path := filepath.Join(outputDir, file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("expected file %s was not created", file)
			}
		}

		// Verify generated_resolver.go contains expected content
		resolverPath := filepath.Join(outputDir, "generated_resolver.go")
		resolverContent, err := os.ReadFile(resolverPath)
		if err != nil {
			t.Fatalf("failed to read generated_resolver.go: %v", err)
		}

		expectedContent := []string{
			"package generated",
			"type GeneratedResolver struct",
			"func NewGeneratedResolver",
			"func (r *generatedQueryResolver) Robot",
			"func (r *generatedQueryResolver) Robots",
		}

		for _, expected := range expectedContent {
			if !contains(string(resolverContent), expected) {
				t.Errorf("generated_resolver.go missing expected content: %s", expected)
			}
		}

		// Verify generated_models.go contains expected content
		modelsPath := filepath.Join(outputDir, "generated_models.go")
		modelsContent, err := os.ReadFile(modelsPath)
		if err != nil {
			t.Fatalf("failed to read generated_models.go: %v", err)
		}

		expectedModels := []string{
			"package generated",
			"func entityRobot",
			"func entitiesRobot",
		}

		for _, expected := range expectedModels {
			if !contains(string(modelsContent), expected) {
				t.Errorf("generated_models.go missing expected content: %s", expected)
			}
		}

		// Verify generated_converters.go contains expected content
		convertersPath := filepath.Join(outputDir, "generated_converters.go")
		convertersContent, err := os.ReadFile(convertersPath)
		if err != nil {
			t.Fatalf("failed to read generated_converters.go: %v", err)
		}

		expectedConverters := []string{
			"package generated",
			"func getID",  // ID is capitalized per GraphQL conventions
			"func getName",
			"func getStatus",
		}

		for _, expected := range expectedConverters {
			if !contains(string(convertersContent), expected) {
				t.Errorf("generated_converters.go missing expected content: %s", expected)
			}
		}
	})
}

func TestParseFieldPath(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantType      string
		wantField     string
		wantErr       bool
	}{
		{
			name:      "valid path",
			path:      "Robot.name",
			wantType:  "Robot",
			wantField: "name",
			wantErr:   false,
		},
		{
			name:      "missing type",
			path:      ".name",
			wantType:  "",
			wantField: "",
			wantErr:   true,
		},
		{
			name:      "missing field",
			path:      "Robot.",
			wantType:  "",
			wantField: "",
			wantErr:   true,
		},
		{
			name:      "no dot",
			path:      "Robot",
			wantType:  "",
			wantField: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotField, err := parseFieldPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFieldPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotType != tt.wantType {
				t.Errorf("parseFieldPath() gotType = %v, want %v", gotType, tt.wantType)
			}
			if gotField != tt.wantField {
				t.Errorf("parseFieldPath() gotField = %v, want %v", gotField, tt.wantField)
			}
		})
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		containsInMiddle(s, substr)))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
