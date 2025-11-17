package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360/semstreams/errors"
	"github.com/vektah/gqlparser/v2/ast"
)

// Generate generates code from config and schema
func Generate(config *Config, schema *SchemaInfo, outputDir string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return errors.WrapFatal(err, "Generate", "os.MkdirAll",
			fmt.Sprintf("create output directory: %s", outputDir))
	}

	// Build template data
	templateData, err := buildTemplateData(config, schema)
	if err != nil {
		return err
	}

	// Generate generated_resolver.go
	resolverCode, err := GenerateResolverCode(templateData)
	if err != nil {
		return errors.WrapFatal(err, "Generate", "GenerateResolverCode",
			"generate resolver code")
	}
	if err := writeFile(filepath.Join(outputDir, "generated_resolver.go"), resolverCode); err != nil {
		return err
	}

	// Generate generated_models.go
	modelsCode, err := GenerateModelsCode(templateData)
	if err != nil {
		return errors.WrapFatal(err, "Generate", "GenerateModelsCode",
			"generate models code")
	}
	if err := writeFile(filepath.Join(outputDir, "generated_models.go"), modelsCode); err != nil {
		return err
	}

	// Generate generated_converters.go
	convertersCode, err := GenerateConvertersCode(templateData)
	if err != nil {
		return errors.WrapFatal(err, "Generate", "GenerateConvertersCode",
			"generate converters code")
	}
	if err := writeFile(filepath.Join(outputDir, "generated_converters.go"), convertersCode); err != nil {
		return err
	}

	return nil
}

// buildQueryTemplates builds query resolver template data
func buildQueryTemplates(config *Config, schema *SchemaInfo) ([]QueryTemplateData, error) {
	queries := []QueryTemplateData{}

	for queryName, queryConfig := range config.Queries {
		query, exists := schema.Queries[queryName]
		if !exists {
			return nil, fmt.Errorf("query %s not found in schema", queryName)
		}

		// Build arguments
		args := []ArgTemplateData{}
		for _, arg := range query.Arguments {
			args = append(args, ArgTemplateData{
				Name: arg.Name,
				Type: mapGraphQLTypeToGo(arg.Type),
			})
		}

		// Build return type
		returnType := mapGraphQLTypeToGo(query.Type)

		// Build implementation
		implementation, err := buildQueryImplementation(queryName, queryConfig, schema)
		if err != nil {
			return nil, err
		}

		queries = append(queries, QueryTemplateData{
			Name:           capitalize(queryName),
			Args:           args,
			ReturnType:     returnType,
			Implementation: implementation,
		})
	}

	return queries, nil
}

// buildTypeConverters builds type and converter template data
func buildTypeConverters(config *Config, schema *SchemaInfo) ([]TypeTemplateData, []ConverterTemplateData, error) {
	typeFields := make(map[string][]FieldTemplateData)
	converterMap := make(map[string]ConverterTemplateData)

	for fieldPath, fieldConfig := range config.Fields {
		typeName, fieldName, err := parseFieldPath(fieldPath)
		if err != nil {
			return nil, nil, err
		}

		// Get GraphQL field type
		gqlFieldType, err := GetFieldType(schema, typeName, fieldName)
		if err != nil {
			return nil, nil, err
		}

		var conversion string
		if fieldConfig.Property != "" {
			// Property-based field
			// Check if field type is an enum
			baseTypeName := GetBaseTypeName(gqlFieldType)
			var enumType string
			if IsEnumType(schema, baseTypeName) {
				enumType = baseTypeName
			}

			// Make converter name unique based on property + nullable + enum
			converterKey := fieldConfig.Property
			if fieldConfig.Nullable {
				converterKey += ":nullable"
			}
			if enumType != "" {
				converterKey += ":enum:" + enumType
			}

			converterName := buildConverterFuncName(fieldConfig.Property, fieldConfig.Nullable)
			if enumType != "" {
				// Add enum type suffix to function name
				// Validate total length doesn't exceed maximum
				proposedName := converterName + enumType
				if len(proposedName) > 120 {
					return nil, nil, errors.WrapInvalid(
						fmt.Errorf("converter name too long (%d chars): %s", len(proposedName), proposedName),
						"buildTemplateData", "buildConverterFuncName",
						fmt.Sprintf("name exceeds maximum length of 120 characters for field %s", fieldPath))
				}
				converterName = proposedName
			}

			// Determine Go type with nullability
			goType := fieldConfig.Type

			// Override type if it's an enum
			if enumType != "" {
				goType = enumType
			}

			if fieldConfig.Nullable && goType != "" && goType[0] != '*' {
				goType = "*" + goType
			}

			// Add converter if not already added (using unique key)
			if _, exists := converterMap[converterKey]; !exists {
				converterMap[converterKey] = ConverterTemplateData{
					FuncName:      converterName,
					PropertyPath:  fieldConfig.Property,
					GoType:        goType,
					Nullable:      fieldConfig.Nullable,
					EnumType:      enumType,
					ComplexObject: fieldConfig.ComplexObject,
				}
			}

			// Build conversion code
			conversion = converterName + "(e)"

			// Handle pointer types for object types
			if isObjectType(schema, baseTypeName) && !IsListType(gqlFieldType) {
				// For object types, we need address operator if non-nullable
				if IsNonNullType(gqlFieldType) {
					// Non-null pointer field - need to handle carefully
					conversion = fmt.Sprintf("func() *%s { v := %s(e); return &v }()",
						fieldConfig.Type, converterName)
				}
			}
		} else if fieldConfig.Resolver == "QueryRelationships" {
			// Relationship field - resolved by field resolver
			conversion = `nil /* Resolved by field resolver */`
		} else if fieldConfig.Resolver != "" {
			// Other resolver-based field (Custom, etc.)
			conversion = fmt.Sprintf(`nil /* TODO: implement %s resolver */`, fieldConfig.Resolver)
		} else {
			conversion = `"" /* No property or resolver specified */`
		}

		// Add to type fields
		if typeFields[typeName] == nil {
			typeFields[typeName] = []FieldTemplateData{}
		}
		typeFields[typeName] = append(typeFields[typeName], FieldTemplateData{
			GraphQLName: capitalize(fieldName),
			Conversion:  conversion,
		})
	}

	// Build type templates
	types := []TypeTemplateData{}
	for typeName, fields := range typeFields {
		types = append(types, TypeTemplateData{
			Name:   typeName,
			Fields: fields,
		})
	}

	// Build converter list
	converters := []ConverterTemplateData{}
	for _, converter := range converterMap {
		converters = append(converters, converter)
	}

	return types, converters, nil
}

// buildUnionTypes detects and builds union type template data from queries
func buildUnionTypes(config *Config, schema *SchemaInfo) ([]TypeTemplateData, error) {
	// Detect union/interface types used in queries
	unionTypes := make(map[string]bool)
	for queryName := range config.Queries {
		query, exists := schema.Queries[queryName]
		if !exists {
			continue
		}
		baseTypeName := GetBaseTypeName(query.Type)
		if IsUnionType(schema, baseTypeName) || IsInterfaceType(schema, baseTypeName) {
			unionTypes[baseTypeName] = true
		}
	}

	// Generate union type converters with member mappings
	types := []TypeTemplateData{}
	for unionTypeName := range unionTypes {
		// Get union member types from schema
		memberTypes := GetUnionTypes(schema, unionTypeName)
		if len(memberTypes) == 0 {
			continue
		}

		// Build union member data
		members := make([]UnionMemberData, 0, len(memberTypes))
		for _, graphqlType := range memberTypes {
			members = append(members, UnionMemberData{
				GraphQLType: graphqlType,
				EntityType:  mapGraphQLTypeToEntityType(graphqlType),
			})
		}

		// Add union type template
		types = append(types, TypeTemplateData{
			Name:         unionTypeName,
			Fields:       nil, // Union types have no fields
			UnionMembers: members,
		})
	}

	return types, nil
}

// buildFieldResolvers builds field resolver template data for relationship fields
func buildFieldResolvers(config *Config, schema *SchemaInfo) ([]TypeFieldResolverData, error) {
	// Group relationship fields by type
	typeFields := make(map[string][]RelationshipFieldData)

	for fieldPath, fieldConfig := range config.Fields {
		// Only process relationship fields
		if fieldConfig.Resolver != "QueryRelationships" {
			continue
		}

		typeName, fieldName, err := parseFieldPath(fieldPath)
		if err != nil {
			return nil, err
		}

		// Get GraphQL field type to determine return type
		gqlFieldType, err := GetFieldType(schema, typeName, fieldName)
		if err != nil {
			return nil, err
		}

		// Map to Go return type
		returnType := mapGraphQLTypeToGo(gqlFieldType)

		// Build relationship field data
		fieldData := RelationshipFieldData{
			FieldName:  capitalize(fieldName),
			ReturnType: returnType,
			EdgeType:   fieldConfig.EdgeType,
			Direction:  fieldConfig.Direction,
			TargetType: fieldConfig.TargetType,
		}

		// Add to type fields
		if typeFields[typeName] == nil {
			typeFields[typeName] = []RelationshipFieldData{}
		}
		typeFields[typeName] = append(typeFields[typeName], fieldData)
	}

	// Build TypeFieldResolverData for each type with relationship fields
	resolvers := []TypeFieldResolverData{}
	for typeName, fields := range typeFields {
		resolvers = append(resolvers, TypeFieldResolverData{
			TypeName: typeName,
			Fields:   fields,
		})
	}

	return resolvers, nil
}

// buildTemplateData builds template data from config and schema
func buildTemplateData(config *Config, schema *SchemaInfo) (*TemplateData, error) {
	data := &TemplateData{
		Package:        config.Package,
		Queries:        []QueryTemplateData{},
		Types:          []TypeTemplateData{},
		Converters:     []ConverterTemplateData{},
		FieldResolvers: []TypeFieldResolverData{},
	}

	// Build query resolvers
	queries, err := buildQueryTemplates(config, schema)
	if err != nil {
		return nil, err
	}
	data.Queries = queries

	// Build type converters
	types, converters, err := buildTypeConverters(config, schema)
	if err != nil {
		return nil, err
	}
	data.Types = types
	data.Converters = converters

	// Build union type converters
	unionTypes, err := buildUnionTypes(config, schema)
	if err != nil {
		return nil, err
	}
	data.Types = append(data.Types, unionTypes...)

	// Build field resolvers for relationship fields
	fieldResolvers, err := buildFieldResolvers(config, schema)
	if err != nil {
		return nil, err
	}
	data.FieldResolvers = fieldResolvers

	return data, nil
}

// mapGraphQLTypeToEntityType maps a GraphQL type name to its entity type identifier
// Example: "Spec" -> "org.semmem.spec", "Doc" -> "org.semmem.doc"
func mapGraphQLTypeToEntityType(graphqlType string) string {
	// Convert to lowercase with underscores for compound words
	entityType := ""
	for i, r := range graphqlType {
		if i > 0 && 'A' <= r && r <= 'Z' {
			entityType += "_"
		}
		entityType += string(r)
	}
	entityType = strings.ToLower(entityType)

	// Prepend org.semmem prefix (could be configurable in future)
	return "org.semmem." + entityType
}

// isObjectType checks if a type is an object type (not scalar)
func isObjectType(schema *SchemaInfo, typeName string) bool {
	// Scalar types
	scalars := map[string]bool{
		"ID":      true,
		"String":  true,
		"Int":     true,
		"Float":   true,
		"Boolean": true,
	}

	if scalars[typeName] {
		return false
	}

	// Check if it's a defined object type
	typeDef, exists := schema.Types[typeName]
	if !exists {
		return false
	}

	return typeDef.Kind == ast.Object
}

// writeFile writes content to a file
func writeFile(path, content string) error {
	// Format the content (remove extra blank lines)
	content = cleanupCode(content)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return errors.WrapFatal(err, "writeFile", "os.WriteFile",
			fmt.Sprintf("write file: %s", path))
	}

	return nil
}

// cleanupCode removes extra blank lines and cleans up formatting
func cleanupCode(code string) string {
	lines := strings.Split(code, "\n")
	result := []string{}
	prevBlank := false

	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""

		// Skip multiple consecutive blank lines
		if isBlank && prevBlank {
			continue
		}

		result = append(result, line)
		prevBlank = isBlank
	}

	return strings.Join(result, "\n")
}

// runGenerate runs the code generation process
func runGenerate(configPath, outputDir, schemaPath string) error {
	// Load config
	config, err := LoadConfig(configPath)
	if err != nil {
		return err
	}

	// Override schema path if provided
	if schemaPath != "" {
		config.SchemaPath = schemaPath
	}

	// Parse schema
	schema, err := ParseSchema(config.SchemaPath)
	if err != nil {
		return err
	}

	// Validate config against schema
	if err := ValidateConfigAgainstSchema(config, schema); err != nil {
		return err
	}

	// Generate code
	if err := Generate(config, schema, outputDir); err != nil {
		return err
	}

	fmt.Printf("Successfully generated code in %s\n", outputDir)
	fmt.Printf("  - generated_resolver.go\n")
	fmt.Printf("  - generated_models.go\n")
	fmt.Printf("  - generated_converters.go\n")

	return nil
}
