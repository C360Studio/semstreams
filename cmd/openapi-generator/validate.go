package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// validateOpenAPIRefs fails if any `$ref: "#/components/schemas/<Name>"` in the
// document points at a schema not present under components.schemas. The generator
// only emits components.schemas for registered types (ResponseTypes /
// RequestBodyTypes), so a SchemaRef to an unregistered type produces a dangling
// pointer: it regenerates deterministically (no drift), ships clean through the
// schema-drift gate, and breaks downstream openapi-typescript codegen at the
// sister repos. This check turns that silent omission into a local generation
// failure at the point of the mistake (gh#228).
func validateOpenAPIRefs(doc OpenAPIDocument) error {
	defined := make(map[string]bool, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		defined[name] = true
	}

	// Round-trip through a generic tree so $refs are caught ANYWHERE — operation
	// request/response bodies, nested schema properties, array items — not just the
	// top-level operation refs. yaml.v3 yields map[string]any (not map[any]any).
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal OpenAPI doc for ref check: %w", err)
	}
	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("unmarshal OpenAPI doc for ref check: %w", err)
	}

	const prefix = "#/components/schemas/"
	unresolved := make(map[string]bool)
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			for k, val := range v {
				if k == "$ref" {
					if s, ok := val.(string); ok && strings.HasPrefix(s, prefix) {
						if name := strings.TrimPrefix(s, prefix); !defined[name] {
							unresolved[s] = true
						}
					}
				}
				walk(val)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(generic)

	if len(unresolved) > 0 {
		names := make([]string, 0, len(unresolved))
		for s := range unresolved {
			names = append(names, s)
		}
		sort.Strings(names)
		return fmt.Errorf(
			"OpenAPI spec has %d dangling $ref(s) — referenced schema(s) absent from components.schemas; "+
				"register the type in the owning service spec's ResponseTypes/RequestBodyTypes (gh#228): %s",
			len(names), strings.Join(names, ", "),
		)
	}
	return nil
}

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
