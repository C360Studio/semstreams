package main

import (
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// TestBuildFieldResolvers tests relationship field resolver generation
func TestBuildFieldResolvers(t *testing.T) {
	tests := []struct {
		name              string
		schema            string
		config            *Config
		expectedResolvers int
		expectedFields    map[string]int // type -> field count
		expectError       bool
	}{
		{
			name: "single type with outgoing relationship",
			schema: `
				type Spec {
					id: ID!
					title: String!
					dependencies: [Spec!]!
				}
			`,
			config: &Config{
				Fields: map[string]FieldConfig{
					"Spec.id": {
						Property: "id",
						Type:     "string",
					},
					"Spec.title": {
						Property: "properties.title",
						Type:     "string",
					},
					"Spec.dependencies": {
						Resolver:   "QueryRelationships",
						EdgeType:   "depends_on",
						Direction:  "outgoing",
						TargetType: "Spec",
					},
				},
			},
			expectedResolvers: 1,
			expectedFields: map[string]int{
				"Spec": 1,
			},
			expectError: false,
		},
		{
			name: "single type with bidirectional relationship",
			schema: `
				type Spec {
					id: ID!
					relatedDocs: [Doc!]!
				}
				type Doc {
					id: ID!
				}
			`,
			config: &Config{
				Fields: map[string]FieldConfig{
					"Spec.id": {
						Property: "id",
						Type:     "string",
					},
					"Spec.relatedDocs": {
						Resolver:   "QueryRelationships",
						EdgeType:   "related_to",
						Direction:  "both",
						TargetType: "Doc",
					},
				},
			},
			expectedResolvers: 1,
			expectedFields: map[string]int{
				"Spec": 1,
			},
			expectError: false,
		},
		{
			name: "multiple types with multiple relationships",
			schema: `
				type Spec {
					id: ID!
					dependencies: [Spec!]!
					dependents: [Spec!]!
					relatedDocs: [Doc!]!
				}
				type Doc {
					id: ID!
					referencedSpecs: [Spec!]!
				}
			`,
			config: &Config{
				Fields: map[string]FieldConfig{
					"Spec.dependencies": {
						Resolver:   "QueryRelationships",
						EdgeType:   "depends_on",
						Direction:  "outgoing",
						TargetType: "Spec",
					},
					"Spec.dependents": {
						Resolver:   "QueryRelationships",
						EdgeType:   "depends_on",
						Direction:  "incoming",
						TargetType: "Spec",
					},
					"Spec.relatedDocs": {
						Resolver:   "QueryRelationships",
						EdgeType:   "related_to",
						Direction:  "both",
						TargetType: "Doc",
					},
					"Doc.referencedSpecs": {
						Resolver:   "QueryRelationships",
						EdgeType:   "references",
						Direction:  "outgoing",
						TargetType: "Spec",
					},
				},
			},
			expectedResolvers: 2,
			expectedFields: map[string]int{
				"Spec": 3,
				"Doc":  1,
			},
			expectError: false,
		},
		{
			name: "no relationship fields",
			schema: `
				type Spec {
					id: ID!
					title: String!
				}
			`,
			config: &Config{
				Fields: map[string]FieldConfig{
					"Spec.id": {
						Property: "id",
						Type:     "string",
					},
					"Spec.title": {
						Property: "properties.title",
						Type:     "string",
					},
				},
			},
			expectedResolvers: 0,
			expectedFields:    map[string]int{},
			expectError:       false,
		},
		{
			name: "mixed property and relationship fields",
			schema: `
				type Issue {
					id: ID!
					title: String!
					fixedBy: [PullRequest!]!
					implements: [Spec!]!
				}
				type PullRequest {
					id: ID!
				}
				type Spec {
					id: ID!
				}
			`,
			config: &Config{
				Fields: map[string]FieldConfig{
					"Issue.id": {
						Property: "id",
						Type:     "string",
					},
					"Issue.title": {
						Property: "properties.title",
						Type:     "string",
					},
					"Issue.fixedBy": {
						Resolver:   "QueryRelationships",
						EdgeType:   "fixes",
						Direction:  "incoming",
						TargetType: "PullRequest",
					},
					"Issue.implements": {
						Resolver:   "QueryRelationships",
						EdgeType:   "implements",
						Direction:  "outgoing",
						TargetType: "Spec",
					},
				},
			},
			expectedResolvers: 1,
			expectedFields: map[string]int{
				"Issue": 2,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse schema
			source := &ast.Source{
				Name:  "test.graphql",
				Input: tt.schema,
			}
			schema, err := gqlparser.LoadSchema(source)
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}

			// Build SchemaInfo
			schemaInfo := &SchemaInfo{
				Schema:  schema,
				Queries: make(map[string]*ast.FieldDefinition),
				Types:   make(map[string]*ast.Definition),
			}
			for name, typeDef := range schema.Types {
				if !typeDef.BuiltIn {
					schemaInfo.Types[name] = typeDef
				}
			}

			// Build field resolvers
			resolvers, err := buildFieldResolvers(tt.config, schemaInfo)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(resolvers) != tt.expectedResolvers {
				t.Errorf("Expected %d type resolvers, got %d", tt.expectedResolvers, len(resolvers))
			}

			// Verify field counts per type
			for _, resolver := range resolvers {
				expectedCount, exists := tt.expectedFields[resolver.TypeName]
				if !exists {
					t.Errorf("Unexpected type resolver: %s", resolver.TypeName)
					continue
				}

				if len(resolver.Fields) != expectedCount {
					t.Errorf("Type %s: expected %d fields, got %d", resolver.TypeName, expectedCount, len(resolver.Fields))
				}

				// Verify each field has required metadata
				for _, field := range resolver.Fields {
					if field.FieldName == "" {
						t.Errorf("Type %s: field has empty name", resolver.TypeName)
					}
					if field.ReturnType == "" {
						t.Errorf("Type %s field %s: return type is empty", resolver.TypeName, field.FieldName)
					}
					if field.EdgeType == "" {
						t.Errorf("Type %s field %s: edge type is empty", resolver.TypeName, field.FieldName)
					}
					if field.Direction == "" {
						t.Errorf("Type %s field %s: direction is empty", resolver.TypeName, field.FieldName)
					}
					if field.TargetType == "" {
						t.Errorf("Type %s field %s: target type is empty", resolver.TypeName, field.FieldName)
					}

					// Verify direction is valid
					validDirections := map[string]bool{
						"outgoing": true,
						"incoming": true,
						"both":     true,
					}
					if !validDirections[field.Direction] {
						t.Errorf("Type %s field %s: invalid direction %q", resolver.TypeName, field.FieldName, field.Direction)
					}
				}
			}
		})
	}
}

// TestGenerateResolverCode_RelationshipFields tests generated resolver code
func TestGenerateResolverCode_RelationshipFields(t *testing.T) {
	// Setup schema and configuration
	schemaInfo, config := setupRelationshipTestSchema(t)

	// Build template data and generate code
	templateData, err := buildTemplateData(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildTemplateData failed: %v", err)
	}

	code, err := GenerateResolverCode(templateData)
	if err != nil {
		t.Fatalf("GenerateResolverCode failed: %v", err)
	}

	// Verify generated code structure
	verifyResolverInterface(t, code)
	verifyResolverImplementation(t, code)
	verifyOutgoingRelationship(t, code)
	verifyIncomingRelationship(t, code)
	verifyBothDirectionRelationship(t, code)
	verifyBatchLoadingAndConversions(t, code)
	verifyNullSafety(t, code)
}

// setupRelationshipTestSchema creates schema and configuration for relationship testing
func setupRelationshipTestSchema(t *testing.T) (*SchemaInfo, *Config) {
	schema := `
		type Spec {
			id: ID!
			title: String!
			dependencies: [Spec!]!
			dependents: [Spec!]!
			relatedDocs: [Doc!]!
		}
		type Doc {
			id: ID!
		}
	`

	source := &ast.Source{
		Name:  "test.graphql",
		Input: schema,
	}
	parsedSchema, err := gqlparser.LoadSchema(source)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	schemaInfo := &SchemaInfo{
		Schema:  parsedSchema,
		Queries: make(map[string]*ast.FieldDefinition),
		Types:   make(map[string]*ast.Definition),
	}
	for name, typeDef := range parsedSchema.Types {
		if !typeDef.BuiltIn {
			schemaInfo.Types[name] = typeDef
		}
	}

	config := &Config{
		Package: "test",
		Fields: map[string]FieldConfig{
			"Spec.id": {
				Property: "id",
				Type:     "string",
			},
			"Spec.title": {
				Property: "properties.title",
				Type:     "string",
			},
			"Spec.dependencies": {
				Resolver:   "QueryRelationships",
				EdgeType:   "depends_on",
				Direction:  "outgoing",
				TargetType: "Spec",
			},
			"Spec.dependents": {
				Resolver:   "QueryRelationships",
				EdgeType:   "depends_on",
				Direction:  "incoming",
				TargetType: "Spec",
			},
			"Spec.relatedDocs": {
				Resolver:   "QueryRelationships",
				EdgeType:   "related_to",
				Direction:  "both",
				TargetType: "Doc",
			},
		},
	}

	return schemaInfo, config
}

// verifyResolverInterface verifies the SpecResolver interface and its methods
func verifyResolverInterface(t *testing.T, code string) {
	if !strings.Contains(code, "type SpecResolver interface") {
		t.Error("SpecResolver interface not generated")
	}

	expectedMethods := []string{
		"Dependencies(ctx context.Context, obj *Spec) ([]*Spec, error)",
		"Dependents(ctx context.Context, obj *Spec) ([]*Spec, error)",
		"RelatedDocs(ctx context.Context, obj *Spec) ([]*Doc, error)",
	}

	for _, method := range expectedMethods {
		if !strings.Contains(code, method) {
			t.Errorf("Interface method not found: %s", method)
		}
	}
}

// verifyResolverImplementation verifies the resolver getter and field resolver struct
func verifyResolverImplementation(t *testing.T, code string) {
	if !strings.Contains(code, "func (r *GeneratedResolver) Spec() SpecResolver") {
		t.Error("Spec() resolver getter not generated")
	}

	if !strings.Contains(code, "type specFieldResolver struct{ *GeneratedResolver }") {
		t.Error("specFieldResolver struct not generated")
	}
}

// verifyOutgoingRelationship verifies the outgoing relationship (Dependencies) uses ToEntityID
func verifyOutgoingRelationship(t *testing.T, code string) {
	if !strings.Contains(code, `func (r *specFieldResolver) Dependencies(ctx context.Context, obj *Spec) ([]*Spec, error)`) {
		t.Error("Dependencies method not generated")
	}

	// Check for RelationshipFilters struct usage
	if strings.Contains(code, "Dependencies") {
		if !strings.Contains(code, `r.base.QueryRelationships(ctx, graphql.RelationshipFilters{`) {
			t.Error("Dependencies should use QueryRelationships with RelationshipFilters struct")
		}
		if !strings.Contains(code, `EntityID:  obj.ID`) && !strings.Contains(code, `EntityID: obj.ID`) {
			t.Error("Dependencies should set EntityID to obj.ID")
		}
		if !strings.Contains(code, `Direction: "outgoing"`) {
			t.Error("Dependencies should set Direction to outgoing")
		}
		if !strings.Contains(code, `EdgeTypes: []string{"depends_on"}`) {
			t.Error("Dependencies should filter by depends_on edge type")
		}
	}

	// Check for ToEntityID extraction in outgoing direction
	dependenciesStart := strings.Index(code, "func (r *specFieldResolver) Dependencies")
	if dependenciesStart != -1 {
		dependenciesEnd := strings.Index(code[dependenciesStart:], "\n}\n")
		if dependenciesEnd != -1 {
			dependenciesCode := code[dependenciesStart : dependenciesStart+dependenciesEnd]
			if !strings.Contains(dependenciesCode, "rel.ToEntityID") {
				t.Error("Dependencies should extract rel.ToEntityID for outgoing direction")
			}
		}
	}
}

// verifyIncomingRelationship verifies the incoming relationship (Dependents) uses FromEntityID
func verifyIncomingRelationship(t *testing.T, code string) {
	if !strings.Contains(code, `func (r *specFieldResolver) Dependents(ctx context.Context, obj *Spec) ([]*Spec, error)`) {
		t.Error("Dependents method not generated")
	}

	dependentsStart := strings.Index(code, "func (r *specFieldResolver) Dependents")
	if dependentsStart != -1 {
		dependentsEnd := strings.Index(code[dependentsStart:], "\n}\n")
		if dependentsEnd != -1 {
			dependentsCode := code[dependentsStart : dependentsStart+dependentsEnd]
			if !strings.Contains(dependentsCode, "rel.FromEntityID") {
				t.Error("Dependents should extract rel.FromEntityID for incoming direction")
			}
		}
	}
}

// verifyBothDirectionRelationship verifies bidirectional relationships handle both IDs
func verifyBothDirectionRelationship(t *testing.T, code string) {
	if !strings.Contains(code, `func (r *specFieldResolver) RelatedDocs(ctx context.Context, obj *Spec) ([]*Doc, error)`) {
		t.Error("RelatedDocs method not generated")
	}

	relatedDocsStart := strings.Index(code, "func (r *specFieldResolver) RelatedDocs")
	if relatedDocsStart != -1 {
		relatedDocsEnd := strings.Index(code[relatedDocsStart:], "\n}\n")
		if relatedDocsEnd != -1 {
			relatedDocsCode := code[relatedDocsStart : relatedDocsStart+relatedDocsEnd]
			if !strings.Contains(relatedDocsCode, "rel.ToEntityID") {
				t.Error("RelatedDocs should extract rel.ToEntityID for both direction")
			}
			if !strings.Contains(relatedDocsCode, "rel.FromEntityID") {
				t.Error("RelatedDocs should extract rel.FromEntityID for both direction")
			}
			if !strings.Contains(relatedDocsCode, "obj.ID") {
				t.Error("RelatedDocs should check against obj.ID for both direction")
			}
		}
	}
}

// verifyBatchLoadingAndConversions verifies batch loading and type conversion functions
func verifyBatchLoadingAndConversions(t *testing.T, code string) {
	if !strings.Contains(code, "r.base.QueryEntitiesByIDs(ctx, ids)") {
		t.Error("Batch loading with QueryEntitiesByIDs not generated")
	}

	if !strings.Contains(code, "return entitiesSpec(entities)") {
		t.Error("entitiesSpec conversion not called for Spec relationships")
	}
	if !strings.Contains(code, "return entitiesDoc(entities)") {
		t.Error("entitiesDoc conversion not called for Doc relationships")
	}
}

// verifyNullSafety verifies null safety checks are present
func verifyNullSafety(t *testing.T, code string) {
	if !strings.Contains(code, "if obj == nil") {
		t.Error("Null safety check for obj not generated")
	}
	if !strings.Contains(code, "if len(ids) == 0") {
		t.Error("Empty IDs check not generated")
	}
}

// TestGenerateModelsCode_RelationshipFields verifies relationship fields marked correctly
func TestGenerateModelsCode_RelationshipFields(t *testing.T) {
	schema := `
		type Spec {
			id: ID!
			title: String!
			dependencies: [Spec!]!
		}
	`

	source := &ast.Source{
		Name:  "test.graphql",
		Input: schema,
	}
	parsedSchema, err := gqlparser.LoadSchema(source)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	schemaInfo := &SchemaInfo{
		Schema:  parsedSchema,
		Queries: make(map[string]*ast.FieldDefinition),
		Types:   make(map[string]*ast.Definition),
	}
	for name, typeDef := range parsedSchema.Types {
		if !typeDef.BuiltIn {
			schemaInfo.Types[name] = typeDef
		}
	}

	config := &Config{
		Package: "test",
		Fields: map[string]FieldConfig{
			"Spec.id": {
				Property: "id",
				Type:     "string",
			},
			"Spec.title": {
				Property: "properties.title",
				Type:     "string",
			},
			"Spec.dependencies": {
				Resolver:   "QueryRelationships",
				EdgeType:   "depends_on",
				Direction:  "outgoing",
				TargetType: "Spec",
			},
		},
	}

	// Build template data
	templateData, err := buildTemplateData(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildTemplateData failed: %v", err)
	}

	// Generate models code
	code, err := GenerateModelsCode(templateData)
	if err != nil {
		t.Fatalf("GenerateModelsCode failed: %v", err)
	}

	// Verify relationship field is marked as resolved by field resolver
	if !strings.Contains(code, "Dependencies: nil /* Resolved by field resolver */") {
		t.Error("Relationship field not marked as 'Resolved by field resolver'")
	}

	// Verify property fields use converters
	if !strings.Contains(code, "ID: getIDDirect(e)") || !strings.Contains(code, "Title: getTitle(e)") {
		t.Error("Property fields should use converter functions")
	}
}

// TestBuildFieldResolvers_InvalidFieldPath tests error handling for invalid field paths
func TestBuildFieldResolvers_InvalidFieldPath(t *testing.T) {
	schema := `
		type Spec {
			id: ID!
		}
	`

	source := &ast.Source{
		Name:  "test.graphql",
		Input: schema,
	}
	parsedSchema, err := gqlparser.LoadSchema(source)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	schemaInfo := &SchemaInfo{
		Schema:  parsedSchema,
		Queries: make(map[string]*ast.FieldDefinition),
		Types:   make(map[string]*ast.Definition),
	}
	for name, typeDef := range parsedSchema.Types {
		if !typeDef.BuiltIn {
			schemaInfo.Types[name] = typeDef
		}
	}

	config := &Config{
		Fields: map[string]FieldConfig{
			"InvalidFieldPath": {
				Resolver:   "QueryRelationships",
				EdgeType:   "test",
				Direction:  "outgoing",
				TargetType: "Spec",
			},
		},
	}

	_, err = buildFieldResolvers(config, schemaInfo)

	if err == nil {
		t.Error("Expected error for invalid field path, got none")
	}
}

// TestBuildFieldResolvers_ReturnTypeMapping tests correct return type mapping
func TestBuildFieldResolvers_ReturnTypeMapping(t *testing.T) {
	tests := []struct {
		name               string
		schema             string
		fieldType          string
		expectedReturnType string
	}{
		{
			name: "non-null list of non-null Spec",
			schema: `
				type Spec {
					id: ID!
					dependencies: [Spec!]!
				}
			`,
			fieldType:          "dependencies",
			expectedReturnType: "[]*Spec",
		},
		{
			name: "nullable list of non-null Doc",
			schema: `
				type Spec {
					id: ID!
					relatedDocs: [Doc!]
				}
				type Doc { id: ID! }
			`,
			fieldType:          "relatedDocs",
			expectedReturnType: "[]*Doc",
		},
		{
			name: "non-null list of nullable Issue",
			schema: `
				type Spec {
					id: ID!
					relatedIssues: [Issue]!
				}
				type Issue { id: ID! }
			`,
			fieldType:          "relatedIssues",
			expectedReturnType: "[]*Issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &ast.Source{
				Name:  "test.graphql",
				Input: tt.schema,
			}
			parsedSchema, err := gqlparser.LoadSchema(source)
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}

			schemaInfo := &SchemaInfo{
				Schema:  parsedSchema,
				Queries: make(map[string]*ast.FieldDefinition),
				Types:   make(map[string]*ast.Definition),
			}
			for name, typeDef := range parsedSchema.Types {
				if !typeDef.BuiltIn {
					schemaInfo.Types[name] = typeDef
				}
			}

			config := &Config{
				Fields: map[string]FieldConfig{
					"Spec." + tt.fieldType: {
						Resolver:   "QueryRelationships",
						EdgeType:   "test",
						Direction:  "outgoing",
						TargetType: "Spec",
					},
				},
			}

			resolvers, err := buildFieldResolvers(config, schemaInfo)
			if err != nil {
				t.Fatalf("buildFieldResolvers failed: %v", err)
			}

			if len(resolvers) != 1 {
				t.Fatalf("Expected 1 resolver, got %d", len(resolvers))
			}

			if len(resolvers[0].Fields) != 1 {
				t.Fatalf("Expected 1 field, got %d", len(resolvers[0].Fields))
			}

			actualReturnType := resolvers[0].Fields[0].ReturnType
			if actualReturnType != tt.expectedReturnType {
				t.Errorf("Expected return type %q, got %q", tt.expectedReturnType, actualReturnType)
			}
		})
	}
}
