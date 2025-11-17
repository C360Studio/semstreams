package main

import (
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// TestBuildUnionTypes tests union type detection and generation
func TestBuildUnionTypes(t *testing.T) {
	tests := []struct {
		name           string
		schema         string
		config         *Config
		expectedTypes  int
		expectedUnions []string
		expectError    bool
	}{
		{
			name: "simple union type",
			schema: `
				type Query {
					entity(id: ID!): Entity
				}
				union Entity = Spec | Doc
				type Spec { id: ID! title: String! }
				type Doc { id: ID! content: String! }
			`,
			config: &Config{
				Queries: map[string]QueryConfig{
					"entity": {Resolver: "QueryEntityByID"},
				},
			},
			expectedTypes:  1,
			expectedUnions: []string{"Entity"},
			expectError:    false,
		},
		{
			name: "multiple union types",
			schema: `
				type Query {
					entity(id: ID!): Entity
					searchResult(query: String!): SearchResult
				}
				union Entity = Spec | Doc | Issue
				union SearchResult = Spec | Discussion
				type Spec { id: ID! }
				type Doc { id: ID! }
				type Issue { id: ID! }
				type Discussion { id: ID! }
			`,
			config: &Config{
				Queries: map[string]QueryConfig{
					"entity":       {Resolver: "QueryEntityByID"},
					"searchResult": {Resolver: "SemanticSearch"},
				},
			},
			expectedTypes:  2,
			expectedUnions: []string{"Entity", "SearchResult"},
			expectError:    false,
		},
		{
			name: "union in list return type",
			schema: `
				type Query {
					search(query: String!): [Entity!]!
				}
				union Entity = Spec | Doc
				type Spec { id: ID! }
				type Doc { id: ID! }
			`,
			config: &Config{
				Queries: map[string]QueryConfig{
					"search": {Resolver: "SemanticSearch"},
				},
			},
			expectedTypes:  1,
			expectedUnions: []string{"Entity"},
			expectError:    false,
		},
		{
			name: "no union types",
			schema: `
				type Query {
					spec(id: ID!): Spec
				}
				type Spec { id: ID! title: String! }
			`,
			config: &Config{
				Queries: map[string]QueryConfig{
					"spec": {Resolver: "QueryEntityByID"},
				},
			},
			expectedTypes:  0,
			expectedUnions: []string{},
			expectError:    false,
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
			if schema.Query != nil {
				for _, field := range schema.Query.Fields {
					schemaInfo.Queries[field.Name] = field
				}
			}
			for name, typeDef := range schema.Types {
				if !typeDef.BuiltIn {
					schemaInfo.Types[name] = typeDef
				}
			}

			// Build union types
			types, err := buildUnionTypes(tt.config, schemaInfo)

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

			if len(types) != tt.expectedTypes {
				t.Errorf("Expected %d union types, got %d", tt.expectedTypes, len(types))
			}

			// Verify union names
			unionNames := make(map[string]bool)
			for _, typ := range types {
				unionNames[typ.Name] = true
				if len(typ.UnionMembers) == 0 {
					t.Errorf("Union type %s has no members", typ.Name)
				}
			}

			for _, expectedUnion := range tt.expectedUnions {
				if !unionNames[expectedUnion] {
					t.Errorf("Expected union type %s not found", expectedUnion)
				}
			}
		})
	}
}

// TestMapGraphQLTypeToEntityType tests entity type mapping
func TestMapGraphQLTypeToEntityType(t *testing.T) {
	tests := []struct {
		graphqlType string
		expected    string
	}{
		{"Spec", "org.semmem.spec"},
		{"Doc", "org.semmem.doc"},
		{"Issue", "org.semmem.issue"},
		{"PullRequest", "org.semmem.pull_request"},
		{"DiscussionComment", "org.semmem.discussion_comment"},
		{"DecisionStatus", "org.semmem.decision_status"},
	}

	for _, tt := range tests {
		t.Run(tt.graphqlType, func(t *testing.T) {
			result := mapGraphQLTypeToEntityType(tt.graphqlType)
			if result != tt.expected {
				t.Errorf("mapGraphQLTypeToEntityType(%q) = %q, want %q", tt.graphqlType, result, tt.expected)
			}
		})
	}
}

// TestBuildTypeConverters_EnumTypes tests enum type conversion generation
func TestBuildTypeConverters_EnumTypes(t *testing.T) {
	schema := `
		enum DecisionStatus { PROPOSED, ACCEPTED, REJECTED }
		type Decision {
			status: DecisionStatus!
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
			"Decision.status": {
				Property: "properties.status",
				Type:     "DecisionStatus",
				Nullable: false,
			},
		},
	}

	types, converters, err := buildTypeConverters(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildTypeConverters failed: %v", err)
	}

	// Verify enum converter generated
	foundEnumConverter := false
	for _, converter := range converters {
		if strings.Contains(converter.FuncName, "DecisionStatus") {
			foundEnumConverter = true
			if converter.EnumType != "DecisionStatus" {
				t.Errorf("Expected EnumType=DecisionStatus, got %q", converter.EnumType)
			}
			if converter.GoType != "DecisionStatus" {
				t.Errorf("Expected GoType=DecisionStatus, got %q", converter.GoType)
			}
			if converter.PropertyPath != "properties.status" {
				t.Errorf("Expected PropertyPath=properties.status, got %q", converter.PropertyPath)
			}
		}
	}

	if !foundEnumConverter {
		t.Error("Enum converter not generated")
	}

	// Verify type field uses enum converter
	foundDecisionType := false
	for _, typ := range types {
		if typ.Name == "Decision" {
			foundDecisionType = true
			foundStatusField := false
			for _, field := range typ.Fields {
				if field.GraphQLName == "Status" {
					foundStatusField = true
					if !strings.Contains(field.Conversion, "DecisionStatus") {
						t.Errorf("Expected conversion to use enum converter, got %q", field.Conversion)
					}
				}
			}
			if !foundStatusField {
				t.Error("Status field not found in Decision type")
			}
		}
	}

	if !foundDecisionType {
		t.Error("Decision type not generated")
	}
}

// TestBuildTypeConverters_NullableFields tests nullable field pointer generation
func TestBuildTypeConverters_NullableFields(t *testing.T) {
	schema := `
		type Task {
			title: String!
			category: String
			closedAt: Int
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
			"Task.title": {
				Property: "properties.title",
				Type:     "string",
				Nullable: false,
			},
			"Task.category": {
				Property: "properties.category",
				Type:     "string",
				Nullable: true,
			},
			"Task.closedAt": {
				Property: "properties.closed_at",
				Type:     "int",
				Nullable: true,
			},
		},
	}

	_, converters, err := buildTypeConverters(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildTypeConverters failed: %v", err)
	}

	// Verify nullable converters have Ptr suffix
	foundCategoryPtr := false
	foundClosedAtPtr := false
	foundTitle := false

	for _, converter := range converters {
		switch {
		case converter.PropertyPath == "properties.title":
			foundTitle = true
			if converter.Nullable {
				t.Error("Title should not be nullable")
			}
			if strings.Contains(converter.FuncName, "Ptr") {
				t.Error("Non-nullable field should not have Ptr suffix")
			}
			if converter.GoType != "string" {
				t.Errorf("Expected GoType=string, got %q", converter.GoType)
			}

		case converter.PropertyPath == "properties.category":
			foundCategoryPtr = true
			if !converter.Nullable {
				t.Error("Category should be nullable")
			}
			if !strings.Contains(converter.FuncName, "Ptr") {
				t.Error("Nullable field should have Ptr suffix")
			}
			if converter.GoType != "*string" {
				t.Errorf("Expected GoType=*string, got %q", converter.GoType)
			}

		case converter.PropertyPath == "properties.closed_at":
			foundClosedAtPtr = true
			if !converter.Nullable {
				t.Error("ClosedAt should be nullable")
			}
			if !strings.Contains(converter.FuncName, "Ptr") {
				t.Error("Nullable field should have Ptr suffix")
			}
			if converter.GoType != "*int" {
				t.Errorf("Expected GoType=*int, got %q", converter.GoType)
			}
		}
	}

	if !foundTitle {
		t.Error("Title converter not found")
	}
	if !foundCategoryPtr {
		t.Error("Category pointer converter not found")
	}
	if !foundClosedAtPtr {
		t.Error("ClosedAt pointer converter not found")
	}
}

// TestBuildTypeConverters_ReservedWords tests handling of Go reserved words
func TestBuildTypeConverters_ReservedWords(t *testing.T) {
	schema := `
		type Test {
			type: String!
			func: String!
			interface: String!
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
			"Test.type": {
				Property: "properties.type",
				Type:     "string",
				Nullable: false,
			},
			"Test.func": {
				Property: "properties.func",
				Type:     "string",
				Nullable: false,
			},
			"Test.interface": {
				Property: "properties.interface",
				Type:     "string",
				Nullable: false,
			},
		},
	}

	types, _, err := buildTypeConverters(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildTypeConverters failed: %v", err)
	}

	// Verify reserved words handled in field names
	foundTestType := false
	for _, typ := range types {
		if typ.Name == "Test" {
			foundTestType = true
			for _, field := range typ.Fields {
				// Field names should be capitalized (Type_, Func_, Interface_)
				if field.GraphQLName == "type" || field.GraphQLName == "func" || field.GraphQLName == "interface" {
					t.Errorf("Reserved word not sanitized in GraphQL name: %q", field.GraphQLName)
				}
			}
		}
	}

	if !foundTestType {
		t.Error("Test type not generated")
	}
}

// TestBuildTypeConverters_LongIdentifiers tests identifier length validation
func TestBuildTypeConverters_LongIdentifiers(t *testing.T) {
	// Create a property name that would exceed 120 chars when combined with enum type
	longProperty := "properties.very_long_property_name_that_is_quite_lengthy_and_will_cause_problems_when_combined_with_enum_type_suffix"

	schema := `
		enum VeryLongEnumTypeNameThatWillCauseTheConverterNameToExceedTheMaximumAllowedLength { VALUE1, VALUE2 }
		type Test {
			field: VeryLongEnumTypeNameThatWillCauseTheConverterNameToExceedTheMaximumAllowedLength!
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
			"Test.field": {
				Property: longProperty,
				Type:     "VeryLongEnumTypeNameThatWillCauseTheConverterNameToExceedTheMaximumAllowedLength",
				Nullable: false,
			},
		},
	}

	_, _, err = buildTypeConverters(config, schemaInfo)

	// Should fail with identifier length validation error
	if err == nil {
		t.Error("Expected error for overly long identifier, got none")
		return
	}

	// Verify error message mentions the 120 character limit
	if !strings.Contains(err.Error(), "120 characters") {
		t.Errorf("Error should mention 120 character limit, got: %v", err)
	}

	// Verify error message mentions it's about converter name length
	if !strings.Contains(err.Error(), "converter name too long") {
		t.Errorf("Error should mention converter name, got: %v", err)
	}
}

// TestBuildQueryTemplates tests query resolver template generation
func TestBuildQueryTemplates(t *testing.T) {
	schema := `
		type Query {
			spec(id: ID!): Spec
			specs(limit: Int): [Spec!]!
			searchSpecs(query: String!, limit: Int): [Spec!]!
		}
		type Spec { id: ID! title: String! }
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
	if parsedSchema.Query != nil {
		for _, field := range parsedSchema.Query.Fields {
			schemaInfo.Queries[field.Name] = field
		}
	}
	for name, typeDef := range parsedSchema.Types {
		if !typeDef.BuiltIn {
			schemaInfo.Types[name] = typeDef
		}
	}

	config := &Config{
		Queries: map[string]QueryConfig{
			"spec":        {Resolver: "QueryEntityByID"},
			"specs":       {Resolver: "QueryEntitiesByType", EntityType: "org.semmem.spec"},
			"searchSpecs": {Resolver: "SemanticSearch"},
		},
	}

	queries, err := buildQueryTemplates(config, schemaInfo)
	if err != nil {
		t.Fatalf("buildQueryTemplates failed: %v", err)
	}

	if len(queries) != 3 {
		t.Errorf("Expected 3 queries, got %d", len(queries))
	}

	// Verify query names are capitalized
	queryNames := make(map[string]bool)
	for _, query := range queries {
		queryNames[query.Name] = true
		if query.Name != capitalize(query.Name) {
			t.Errorf("Query name not capitalized: %q", query.Name)
		}
	}

	expectedQueries := []string{"Spec", "Specs", "SearchSpecs"}
	for _, expected := range expectedQueries {
		if !queryNames[expected] {
			t.Errorf("Expected query %q not found", expected)
		}
	}
}
