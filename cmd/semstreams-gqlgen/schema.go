package main

import (
	"fmt"
	"os"

	"github.com/c360/semstreams/errors"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// SchemaInfo contains parsed GraphQL schema information
type SchemaInfo struct {
	Schema *ast.Schema
	Queries map[string]*ast.FieldDefinition
	Types map[string]*ast.Definition
}

// ParseSchema loads and parses a GraphQL schema file
func ParseSchema(schemaPath string) (*SchemaInfo, error) {
	// Read schema file
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, errors.WrapInvalid(err, "ParseSchema", "os.ReadFile",
			fmt.Sprintf("read schema file: %s", schemaPath))
	}

	// Create source
	source := &ast.Source{
		Name:  schemaPath,
		Input: string(schemaContent),
	}

	// Parse schema using gqlparser
	schema, gqlErr := gqlparser.LoadSchema(source)
	if gqlErr != nil {
		return nil, errors.WrapInvalid(gqlErr, "ParseSchema", "gqlparser.LoadSchema",
			"parse GraphQL schema")
	}

	// Extract query fields
	queries := make(map[string]*ast.FieldDefinition)
	if schema.Query != nil {
		for _, field := range schema.Query.Fields {
			queries[field.Name] = field
		}
	}

	// Extract type definitions (exclude built-in types)
	types := make(map[string]*ast.Definition)
	for name, typeDef := range schema.Types {
		if !typeDef.BuiltIn {
			types[name] = typeDef
		}
	}

	return &SchemaInfo{
		Schema: schema,
		Queries: queries,
		Types: types,
	}, nil
}

// ValidateConfigAgainstSchema validates that the config matches the schema
func ValidateConfigAgainstSchema(config *Config, schema *SchemaInfo) error {
	// Validate queries exist in schema
	for queryName := range config.Queries {
		if _, exists := schema.Queries[queryName]; !exists {
			return errors.WrapInvalid(
				fmt.Errorf("query %s not found in schema", queryName),
				"ValidateConfigAgainstSchema", "query validation",
				"query not in schema")
		}
	}

	// Validate types exist in schema
	for typeName := range config.Types {
		if _, exists := schema.Types[typeName]; !exists {
			return errors.WrapInvalid(
				fmt.Errorf("type %s not found in schema", typeName),
				"ValidateConfigAgainstSchema", "type validation",
				"type not in schema")
		}
	}

	// Validate fields belong to existing types
	for fieldPath := range config.Fields {
		// Parse field path (e.g., "Robot.name")
		typeName, _, err := parseFieldPath(fieldPath)
		if err != nil {
			return errors.WrapInvalid(err, "ValidateConfigAgainstSchema",
				"parseFieldPath", "invalid field path")
		}

		// Check type exists
		typeDef, exists := schema.Types[typeName]
		if !exists {
			return errors.WrapInvalid(
				fmt.Errorf("type %s for field %s not found in schema", typeName, fieldPath),
				"ValidateConfigAgainstSchema", "field validation",
				"field type not in schema")
		}

		// For object types, validate field exists
		if typeDef.Kind == ast.Object {
			fieldName := fieldPath[len(typeName)+1:] // Skip "TypeName."
			fieldExists := false
			for _, field := range typeDef.Fields {
				if field.Name == fieldName {
					fieldExists = true
					break
				}
			}
			if !fieldExists {
				return errors.WrapInvalid(
					fmt.Errorf("field %s not found in type %s", fieldName, typeName),
					"ValidateConfigAgainstSchema", "field validation",
					"field not in type")
			}
		}
	}

	return nil
}

// parseFieldPath parses a field path like "Robot.name" into type and field
func parseFieldPath(path string) (typeName string, fieldName string, err error) {
	// Find the first dot
	for i, c := range path {
		if c == '.' {
			if i == 0 {
				return "", "", fmt.Errorf("invalid field path: %s (missing type)", path)
			}
			if i == len(path)-1 {
				return "", "", fmt.Errorf("invalid field path: %s (missing field)", path)
			}
			return path[:i], path[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid field path: %s (no dot separator)", path)
}

// GetFieldType returns the GraphQL type for a field
func GetFieldType(schema *SchemaInfo, typeName, fieldName string) (*ast.Type, error) {
	typeDef, exists := schema.Types[typeName]
	if !exists {
		return nil, fmt.Errorf("type %s not found", typeName)
	}

	for _, field := range typeDef.Fields {
		if field.Name == fieldName {
			return field.Type, nil
		}
	}

	return nil, fmt.Errorf("field %s not found in type %s", fieldName, typeName)
}

// IsListType checks if a GraphQL type is a list
func IsListType(t *ast.Type) bool {
	if t == nil {
		return false
	}
	return t.Elem != nil
}

// IsNonNullType checks if a GraphQL type is non-nullable
func IsNonNullType(t *ast.Type) bool {
	if t == nil {
		return false
	}
	return t.NonNull
}

// GetBaseTypeName returns the base type name (unwrapping lists and non-null)
func GetBaseTypeName(t *ast.Type) string {
	if t == nil {
		return ""
	}

	// Unwrap list
	if t.Elem != nil {
		return GetBaseTypeName(t.Elem)
	}

	// Return name
	if t.NamedType != "" {
		return t.NamedType
	}

	return ""
}

// IsEnumType checks if a type name is an enum in the schema
func IsEnumType(schema *SchemaInfo, typeName string) bool {
	if typeDef, exists := schema.Types[typeName]; exists {
		return typeDef.Kind == ast.Enum
	}
	return false
}

// IsUnionType checks if a type name is a union in the schema
func IsUnionType(schema *SchemaInfo, typeName string) bool {
	if typeDef, exists := schema.Types[typeName]; exists {
		return typeDef.Kind == ast.Union
	}
	return false
}

// IsInterfaceType checks if a type name is an interface in the schema
func IsInterfaceType(schema *SchemaInfo, typeName string) bool {
	if typeDef, exists := schema.Types[typeName]; exists {
		return typeDef.Kind == ast.Interface
	}
	return false
}

// GetUnionTypes returns the possible types for a union
func GetUnionTypes(schema *SchemaInfo, typeName string) []string {
	typeDef, exists := schema.Types[typeName]
	if !exists || typeDef.Kind != ast.Union {
		return nil
	}

	types := make([]string, len(typeDef.Types))
	for i, t := range typeDef.Types {
		types[i] = t
	}
	return types
}
