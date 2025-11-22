// Package main provides a GraphQL code generator for semstreams.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/c360/semstreams/errors"
)

// Config represents the code generation configuration
type Config struct {
	Package    string                 `json:"package"`
	SchemaPath string                 `json:"schema_path"`
	Queries    map[string]QueryConfig `json:"queries"`
	Fields     map[string]FieldConfig `json:"fields"`
	Types      map[string]TypeConfig  `json:"types"`
}

// QueryConfig defines how to resolve a GraphQL query
type QueryConfig struct {
	Resolver   string `json:"resolver"`              // BaseResolver method name
	Subject    string `json:"subject"`               // NATS subject for the query
	EntityType string `json:"entity_type,omitempty"` // Entity type for QueryEntitiesByType
}

// FieldConfig defines how to resolve a GraphQL field
type FieldConfig struct {
	Property      string `json:"property,omitempty"`       // Property path in Entity (e.g., "properties.name")
	Type          string `json:"type,omitempty"`           // Go type (string, int, float64, bool)
	Nullable      bool   `json:"nullable,omitempty"`       // Whether field is nullable (returns pointer type)
	ComplexObject bool   `json:"complex_object,omitempty"` // Whether this is a complex nested object type
	Resolver      string `json:"resolver,omitempty"`       // Optional: BaseResolver method for relationship fields
	Subject       string `json:"subject,omitempty"`        // Optional: NATS subject for relationship queries
	EdgeType      string `json:"edge_type,omitempty"`      // For QueryRelationships: edge type to filter (e.g., "depends_on")
	Direction     string `json:"direction,omitempty"`      // For QueryRelationships: "outgoing", "incoming", or "both"
	TargetType    string `json:"target_type,omitempty"`    // For QueryRelationships: target GraphQL type name
}

// TypeConfig defines entity type mapping
type TypeConfig struct {
	EntityType string `json:"entity_type"` // Entity type name in the graph
}

// LoadConfig loads and validates the configuration file
func LoadConfig(configPath string) (*Config, error) {
	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.WrapInvalid(err, "LoadConfig", "os.ReadFile",
			fmt.Sprintf("read config file: %s", configPath))
	}

	// Parse JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, errors.WrapInvalid(err, "LoadConfig", "json.Unmarshal",
			"parse config JSON")
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Package == "" {
		return errors.WrapInvalid(fmt.Errorf("package name is required"),
			"Config", "Validate", "package name missing")
	}

	if c.SchemaPath == "" {
		return errors.WrapInvalid(fmt.Errorf("schema_path is required"),
			"Config", "Validate", "schema_path missing")
	}

	// Validate query configs
	for queryName, queryConfig := range c.Queries {
		if queryConfig.Resolver == "" {
			return errors.WrapInvalid(
				fmt.Errorf("resolver required for query %s", queryName),
				"Config", "Validate", "query resolver missing")
		}

		// Validate resolver method exists
		validResolvers := map[string]bool{
			"QueryEntityByID":     true,
			"QueryEntityByAlias":  true,
			"QueryEntitiesByIDs":  true,
			"QueryEntitiesByType": true,
			"QueryRelationships":  true,
			"SemanticSearch":      true,
			"LocalSearch":         true,
			"GlobalSearch":        true,
			"GetCommunity":        true,
			"GetEntityCommunity":  true,
			"Custom":              true, // Custom resolvers handled manually
		}
		if !validResolvers[queryConfig.Resolver] {
			return errors.WrapInvalid(
				fmt.Errorf("invalid resolver %s for query %s", queryConfig.Resolver, queryName),
				"Config", "Validate", "invalid resolver method")
		}
	}

	// Validate field configs
	for fieldName, fieldConfig := range c.Fields {
		// Field must have either property or resolver
		if fieldConfig.Property == "" && fieldConfig.Resolver == "" {
			return errors.WrapInvalid(
				fmt.Errorf("field %s must have either property or resolver", fieldName),
				"Config", "Validate", "field config missing property/resolver")
		}

		// If resolver is specified, validate it
		if fieldConfig.Resolver != "" {
			validResolvers := map[string]bool{
				"QueryEntityByID":     true,
				"QueryEntityByAlias":  true,
				"QueryEntitiesByIDs":  true,
				"QueryEntitiesByType": true,
				"QueryRelationships":  true,
				"SemanticSearch":      true,
				"LocalSearch":         true,
				"GlobalSearch":        true,
				"GetCommunity":        true,
				"GetEntityCommunity":  true,
				"Custom":              true, // Custom field resolvers handled manually
			}
			if !validResolvers[fieldConfig.Resolver] {
				return errors.WrapInvalid(
					fmt.Errorf("invalid resolver %s for field %s", fieldConfig.Resolver, fieldName),
					"Config", "Validate", "invalid resolver method")
			}

			// If resolver is QueryRelationships, validate relationship metadata
			if fieldConfig.Resolver == "QueryRelationships" {
				if fieldConfig.EdgeType == "" {
					return errors.WrapInvalid(
						fmt.Errorf("field %s with QueryRelationships must specify edge_type", fieldName),
						"Config", "Validate", "relationship edge_type missing")
				}
				if fieldConfig.Direction == "" {
					return errors.WrapInvalid(
						fmt.Errorf("field %s with QueryRelationships must specify direction", fieldName),
						"Config", "Validate", "relationship direction missing")
				}
				validDirections := map[string]bool{
					"outgoing": true,
					"incoming": true,
					"both":     true,
				}
				if !validDirections[fieldConfig.Direction] {
					return errors.WrapInvalid(
						fmt.Errorf("field %s has invalid direction %s (must be outgoing, incoming, or both)",
							fieldName, fieldConfig.Direction),
						"Config", "Validate", "invalid relationship direction")
				}
				if fieldConfig.TargetType == "" {
					return errors.WrapInvalid(
						fmt.Errorf("field %s with QueryRelationships must specify target_type", fieldName),
						"Config", "Validate", "relationship target_type missing")
				}
			}
		}

		// If property is specified, validate type
		if fieldConfig.Property != "" && fieldConfig.Type == "" {
			return errors.WrapInvalid(
				fmt.Errorf("field %s with property must specify type", fieldName),
				"Config", "Validate", "field type missing")
		}

		// Validate type is supported (skip validation for complex objects - they use GraphQL types)
		if fieldConfig.Type != "" && !fieldConfig.ComplexObject {
			validTypes := map[string]bool{
				"string":                 true,
				"int":                    true,
				"float64":                true,
				"bool":                   true,
				"[]string":               true,
				"map[string]interface{}": true,
			}
			if !validTypes[fieldConfig.Type] {
				return errors.WrapInvalid(
					fmt.Errorf("invalid type %s for field %s", fieldConfig.Type, fieldName),
					"Config", "Validate", "invalid field type")
			}
		}
	}

	// Validate type configs
	for typeName, typeConfig := range c.Types {
		if typeConfig.EntityType == "" {
			return errors.WrapInvalid(
				fmt.Errorf("entity_type required for type %s", typeName),
				"Config", "Validate", "entity_type missing")
		}
	}

	return nil
}
