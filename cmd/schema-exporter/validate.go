package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xeipuuv/gojsonschema"
)

// validateSchema validates a component schema against the meta-schema
func validateSchema(schema ComponentSchema, metaSchemaPath string) error {
	// If meta-schema path is not provided, skip validation
	if metaSchemaPath == "" {
		return nil
	}

	// Load meta-schema
	metaSchemaLoader := gojsonschema.NewReferenceLoader("file://" + metaSchemaPath)

	// Convert schema to JSON for validation
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema for validation: %w", err)
	}

	documentLoader := gojsonschema.NewBytesLoader(schemaBytes)

	// Validate
	result, err := gojsonschema.Validate(metaSchemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	if !result.Valid() {
		// Build error message from validation errors
		errMsg := fmt.Sprintf("Schema validation failed for %s:\n", schema.ID)
		for _, desc := range result.Errors() {
			errMsg += fmt.Sprintf("  - %s: %s\n", desc.Field(), desc.Description())
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// loadMetaSchemaPath determines the path to the meta-schema file
func loadMetaSchemaPath() (string, error) {
	// Try to find the meta-schema in the specs directory
	possiblePaths := []string{
		"./specs/component-schema-meta.json",
		"../specs/component-schema-meta.json",
		"../../specs/component-schema-meta.json",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			// Get absolute path
			absPath, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf("failed to get absolute path: %w", err)
			}
			return absPath, nil
		}
	}

	return "", fmt.Errorf("meta-schema not found in any of: %v", possiblePaths)
}
