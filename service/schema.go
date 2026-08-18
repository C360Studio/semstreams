package service

import (
	"reflect"
	"strings"
	"time"
)

type closedJSONSchema interface {
	closedJSONSchema()
}

var closedJSONSchemaType = reflect.TypeOf((*closedJSONSchema)(nil)).Elem()

// SchemaFromType generates a JSON Schema from a reflect.Type.
// It handles primitives, structs, slices, maps, pointers, and time.Time.
func SchemaFromType(t reflect.Type) map[string]any {
	// Handle pointers - use JSON Schema Draft-07 anyOf pattern for nullable
	if t.Kind() == reflect.Pointer {
		elemSchema := SchemaFromType(t.Elem())
		return map[string]any{
			"anyOf": []any{
				elemSchema,
				map[string]any{"type": "null"},
			},
		}
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}

	case reflect.Bool:
		return map[string]any{"type": "boolean"}

	case reflect.Struct:
		// Special case: time.Time
		if t == reflect.TypeOf(time.Time{}) {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		return schemaFromStruct(t)

	case reflect.Slice:
		// Special case: []byte
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{
			"type":  "array",
			"items": SchemaFromType(t.Elem()),
		}

	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": SchemaFromType(t.Elem()),
		}

	case reflect.Interface:
		// interface{} / any - allow anything
		return map[string]any{}

	default:
		// Fallback for unknown types
		return map[string]any{"type": "string"}
	}
}

// schemaFromStruct generates a JSON Schema object definition from a struct type.
func schemaFromStruct(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	required := []string{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Parse json tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		name, opts := parseJSONTag(jsonTag)
		if name == "" {
			name = field.Name
		}

		// Generate schema for field type
		fieldSchema := SchemaFromType(field.Type)

		// Add description from struct tag if available
		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema["description"] = desc
		}

		properties[name] = fieldSchema

		// Field is required if not omitempty and not a pointer
		if !strings.Contains(opts, "omitempty") && field.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if t.Implements(closedJSONSchemaType) || reflect.PointerTo(t).Implements(closedJSONSchemaType) {
		schema["additionalProperties"] = false
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// parseJSONTag parses a json struct tag and returns the name and options.
func parseJSONTag(tag string) (name string, opts string) {
	if tag == "" {
		return "", ""
	}

	parts := strings.Split(tag, ",")
	name = parts[0]

	if len(parts) > 1 {
		opts = strings.Join(parts[1:], ",")
	}

	return name, opts
}

// TypeNameFromReflect extracts a clean type name from a reflect.Type.
// For example: "service.RuntimeHealthResponse" -> "RuntimeHealthResponse"
func TypeNameFromReflect(t reflect.Type) string {
	// Handle pointers
	if t.Kind() == reflect.Pointer {
		return TypeNameFromReflect(t.Elem())
	}

	name := t.Name()
	if name == "" {
		// Anonymous type, use the string representation
		name = t.String()
	}

	// Remove package prefix if present
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}

	return name
}
