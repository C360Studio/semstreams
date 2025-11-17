// Package component provides schema tag parsing and generation for component configuration.
//
// The schema tag system eliminates duplication between Config structs and ConfigSchema
// definitions by auto-generating schemas from struct tags. This provides a single source
// of truth for configuration metadata and follows Go stdlib patterns (similar to json tags).
//
// # Basic Usage
//
// Define configuration with schema tags:
//
//	type MyConfig struct {
//	    Name string `json:"name" schema:"type:string,description:Component name,category:basic"`
//	    Port int    `json:"port" schema:"type:int,description:Port,min:1,max:65535,default:8080"`
//	}
//
// Generate schema at init time:
//
//	var schema = component.GenerateConfigSchema(reflect.TypeOf(MyConfig{}))
//
// # Tag Syntax
//
// Tags use comma-separated directives with colon-separated key-value pairs:
//   - type:string - Field data type (required)
//   - description:text - Field description (recommended)
//   - category:basic - UI organization (basic or advanced)
//   - default:value - Default value
//   - min:N, max:N - Numeric constraints
//   - enum:a|b|c - Valid enum values (pipe-separated)
//   - readonly, editable - Boolean flags for PortDefinition fields
//   - required, hidden - Boolean flags for validation and UI
//
// # Performance
//
// Schema generation uses reflection but is designed for init-time execution:
//   - Call GenerateConfigSchema once at package init
//   - Cache result in package-level variable
//   - Zero reflection cost at runtime
//
// # Error Handling
//
// Invalid tags result in graceful degradation:
//   - Fields with invalid tags are skipped
//   - Errors are wrapped with context using pkg/errors
//   - Missing descriptions use field names as fallback
//
// See docs/architecture/SCHEMA_TAG_SPEC.md for complete specification.
package component

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/pkg/cache"
)

// SchemaDirectives represents parsed schema tag directives
type SchemaDirectives struct {
	// core (required)
	Type        string // REQUIRED - field type
	Description string // REQUIRED (warning if missing)

	// UI Organization
	Category string // "basic" or "advanced"
	ReadOnly bool   // For PortDefinition fields
	Editable bool   // For PortDefinition fields
	Hidden   bool   // Hide from UI

	// Constraints
	Default  any      // Type-specific default value (stored as string, converted during schema generation)
	Required bool     // Field must be provided
	Min      *int     // Numeric minimum
	Max      *int     // Numeric maximum
	Enum     []string // Valid enum values

	// Future extensions (stored but not used yet)
	Help        string
	Placeholder string
	Pattern     string
	Format      string
}

// PortFieldInfo describes metadata for PortDefinition fields
type PortFieldInfo struct {
	Type     string `json:"type"`
	Editable bool   `json:"editable"`
}

// CacheFieldInfo describes metadata for cache.Config fields
type CacheFieldInfo struct {
	Type     string   `json:"type"`
	Editable bool     `json:"editable"`
	Enum     []string `json:"enum,omitempty"` // For strategy field
	Min      *int     `json:"min,omitempty"`  // For numeric fields
}

// ParseSchemaTag parses a schema struct tag into directives.
//
// Tag Syntax:
//   - Directives are comma-separated
//   - Key-value pairs use colon: "key:value"
//   - Boolean flags have no colon: "readonly", "required"
//   - Enum values are pipe-separated: "enum:val1|val2|val3"
//   - Whitespace is trimmed from all values
//
// Required Directives:
//   - type: Field data type (string, int, bool, float, enum, array, object, ports)
//
// Recommended Directives:
//   - description: Human-readable field description (used for UI and documentation)
//
// Optional Directives:
//   - category: UI organization (basic, advanced)
//   - default: Default value (converted to appropriate type)
//   - min/max: Numeric constraints
//   - enum: Valid values for enum types (pipe-separated)
//   - readonly: Field is read-only (boolean flag)
//   - editable: Field is user-editable (boolean flag)
//   - hidden: Field is hidden from UI (boolean flag)
//   - required: Field must be provided (boolean flag)
//
// Example Tags:
//
//	schema:"type:string,description:Component name,category:basic"
//	schema:"type:int,description:Port,min:1,max:65535,default:8080"
//	schema:"type:enum,description:Level,enum:debug|info|warn,default:info"
//	schema:"required,type:string,description:API key"
//	schema:"readonly,type:string,description:System ID"
//
// Returns an error if:
//   - Tag is empty
//   - Type directive is missing
//   - Type value is invalid
//   - Directive syntax is malformed
//   - Numeric values cannot be parsed
//
// See SCHEMA_TAG_SPEC.md for complete specification.
func ParseSchemaTag(tag string) (SchemaDirectives, error) {
	directives := SchemaDirectives{}

	if tag == "" {
		return directives, errors.WrapInvalid(
			fmt.Errorf("empty schema tag"),
			"SchemaTag", "ParseSchemaTag", "tag validation",
		)
	}

	// Split by commas
	parts := strings.Split(tag, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Boolean flags (no colon)
		if !strings.Contains(part, ":") {
			if err := parseBooleanFlag(part, &directives); err != nil {
				return directives, err
			}
			continue
		}

		// Key-value directives
		if err := parseKeyValueDirective(part, &directives); err != nil {
			return directives, err
		}
	}

	// Validate required fields
	if directives.Type == "" {
		return directives, errors.WrapInvalid(
			fmt.Errorf("type directive is required"),
			"SchemaTag", "ParseSchemaTag", "required field validation",
		)
	}

	// Description is strongly recommended but not fatal if missing
	// Caller can use field name as fallback

	return directives, nil
}

// parseBooleanFlag parses boolean flags from schema tags
func parseBooleanFlag(flag string, directives *SchemaDirectives) error {
	switch flag {
	case "readonly":
		directives.ReadOnly = true
	case "editable":
		directives.Editable = true
	case "hidden":
		directives.Hidden = true
	case "required":
		directives.Required = true
	default:
		return errors.WrapInvalid(
			fmt.Errorf("unknown boolean flag: %s", flag),
			"SchemaTag", "parseBooleanFlag", "flag parsing",
		)
	}
	return nil
}

// parseKeyValueDirective parses key:value directives from schema tags
func parseKeyValueDirective(part string, directives *SchemaDirectives) error {
	kv := strings.SplitN(part, ":", 2)
	if len(kv) != 2 {
		return errors.WrapInvalid(
			fmt.Errorf("invalid directive format: %s", part),
			"SchemaTag", "parseKeyValueDirective", "directive parsing",
		)
	}

	key := strings.TrimSpace(kv[0])
	value := strings.TrimSpace(kv[1])

	if value == "" {
		return errors.WrapInvalid(
			fmt.Errorf("empty value for directive: %s", key),
			"SchemaTag", "parseKeyValueDirective", "value validation",
		)
	}

	switch key {
	case "type":
		if !isValidType(value) {
			return errors.WrapInvalid(
				fmt.Errorf("invalid type: %s", value),
				"SchemaTag", "parseKeyValueDirective", "type validation",
			)
		}
		directives.Type = value

	case "description":
		directives.Description = value

	case "category":
		if value != "basic" && value != "advanced" {
			return errors.WrapInvalid(
				fmt.Errorf("invalid category: %s (must be 'basic' or 'advanced')", value),
				"SchemaTag", "parseKeyValueDirective", "category validation",
			)
		}
		directives.Category = value

	case "default":
		// Store as string - will be converted to appropriate type during schema generation
		directives.Default = value

	case "min":
		n, err := strconv.Atoi(value)
		if err != nil {
			return errors.WrapInvalid(
				fmt.Errorf("invalid min value: %s", value),
				"SchemaTag", "parseKeyValueDirective", "min parsing",
			)
		}
		directives.Min = &n

	case "max":
		n, err := strconv.Atoi(value)
		if err != nil {
			return errors.WrapInvalid(
				fmt.Errorf("invalid max value: %s", value),
				"SchemaTag", "parseKeyValueDirective", "max parsing",
			)
		}
		directives.Max = &n

	case "enum":
		// Split enum values by pipe
		directives.Enum = strings.Split(value, "|")
		for i := range directives.Enum {
			directives.Enum[i] = strings.TrimSpace(directives.Enum[i])
		}

	// Future extensions - store but don't validate yet
	case "help":
		directives.Help = value
	case "placeholder":
		directives.Placeholder = value
	case "pattern":
		directives.Pattern = value
	case "format":
		directives.Format = value

	default:
		return errors.WrapInvalid(
			fmt.Errorf("unknown directive: %s", key),
			"SchemaTag", "parseKeyValueDirective", "directive validation",
		)
	}

	return nil
}

// isValidType checks if a type string is valid
func isValidType(t string) bool {
	validTypes := []string{
		"string", "int", "bool", "float",
		"enum", "array", "object", "ports", "cache",
	}
	for _, valid := range validTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// GenerateConfigSchema generates a ConfigSchema from a struct type using reflection.
// This function performs one-time reflection at initialization to extract schema metadata
// from struct field tags, eliminating the need for manual schema definitions.
//
// Usage Pattern:
//
//	type MyComponentConfig struct {
//	    Name string `json:"name" schema:"type:string,description:Name,category:basic"`
//	    Port int    `json:"port" schema:"type:int,description:Port,min:1,max:65535"`
//	}
//
//	var schema = component.GenerateConfigSchema(reflect.TypeOf(MyComponentConfig{}))
//
// Field Processing:
//   - Only exported fields with both 'json' and 'schema' tags are included
//   - json:"-" fields are skipped
//   - Fields without schema tags are skipped
//   - Invalid schema tags result in skipped fields (graceful degradation)
//
// Special Handling:
//   - Fields with type "ports" automatically include PortFieldSchema metadata
//   - Default values are converted from strings to appropriate types
//   - Required fields are added to the schema's Required list
//
// Performance:
//   - Call once at init() time - reflection cost is paid only once
//   - Generated schemas are cached in package-level variables
//   - Zero reflection overhead at runtime
//
// Parameters:
//   - configType: The reflect.Type of the config struct (use reflect.TypeOf(ConfigStruct{}))
//     Pointer types are automatically dereferenced
//
// Returns:
//   - ConfigSchema with Properties map and Required list populated from struct tags
//   - Empty schema for non-struct types
func GenerateConfigSchema(configType reflect.Type) ConfigSchema {
	schema := ConfigSchema{
		Properties: make(map[string]PropertySchema),
		Required:   []string{},
	}

	// Handle pointer types
	if configType.Kind() == reflect.Ptr {
		configType = configType.Elem()
	}

	// Ensure we're working with a struct
	if configType.Kind() != reflect.Struct {
		// Return empty schema for non-struct types
		return schema
	}

	// Iterate struct fields
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)

		// Get json tag for field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue // Skip unexported or omitted fields
		}

		// Parse json tag to get field name (ignore omitempty, etc)
		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName == "" {
			continue
		}

		// Parse schema tag
		schemaTag := field.Tag.Get("schema")
		if schemaTag == "" {
			continue // No schema tag - skip field
		}

		directives, err := ParseSchemaTag(schemaTag)
		if err != nil {
			// Log error and skip field - graceful degradation
			// In production, could use a logger here
			continue
		}

		// Use field name as description fallback if not provided
		description := directives.Description
		if description == "" {
			description = fieldName
		}

		// Build PropertySchema
		propSchema := PropertySchema{
			Type:        directives.Type,
			Description: description,
			Category:    directives.Category,
			Default:     convertDefault(directives.Default, directives.Type),
			Minimum:     directives.Min,
			Maximum:     directives.Max,
			Enum:        directives.Enum,
		}

		// Special handling for "ports" type
		if directives.Type == "ports" {
			propSchema.PortFields = GeneratePortFieldSchema()
		}

		// Special handling for "cache" type
		if directives.Type == "cache" {
			propSchema.CacheFields = GenerateCacheFieldSchema()
		}

		schema.Properties[fieldName] = propSchema

		// Add to required list if needed
		if directives.Required {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// convertDefault converts a default value string to the appropriate type
func convertDefault(value any, fieldType string) any {
	if value == nil {
		return nil
	}

	// Value is stored as string from tag parsing
	valueStr, ok := value.(string)
	if !ok {
		return value // Already converted or wrong type
	}

	switch fieldType {
	case "string", "enum":
		return valueStr

	case "int":
		n, err := strconv.Atoi(valueStr)
		if err != nil {
			return nil // Invalid conversion
		}
		return n

	case "bool":
		b, err := strconv.ParseBool(valueStr)
		if err != nil {
			return nil // Invalid conversion
		}
		return b

	case "float":
		f, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil // Invalid conversion
		}
		return f

	case "array":
		// Simple array default - split by comma if needed
		// For more complex arrays, user should use proper JSON in config
		if valueStr == "" {
			return []string{}
		}
		return []string{valueStr}

	case "object", "ports":
		// Objects and ports don't typically have defaults
		return nil

	default:
		return valueStr
	}
}

// GeneratePortFieldSchema generates metadata for PortDefinition fields.
// This describes which fields in PortDefinition are editable vs read-only,
// enabling the UI to render appropriate controls for port configuration.
//
// The function examines PortDefinition struct tags to determine:
//   - Field types (for appropriate UI controls)
//   - Editability (whether users can modify the field)
//
// Fields marked with "editable" tag are user-modifiable (e.g., Subject, Timeout).
// Fields marked with "readonly" tag are display-only (e.g., Name, Type).
// Fields without schema tags default to read-only string type.
//
// This metadata is included in ConfigSchema for fields with type "ports",
// allowing the frontend to correctly render port configuration forms.
//
// Returns:
//   - Map of field names to PortFieldInfo with type and editability metadata
func GeneratePortFieldSchema() map[string]PortFieldInfo {
	portType := reflect.TypeOf(PortDefinition{})
	fields := make(map[string]PortFieldInfo)

	for i := 0; i < portType.NumField(); i++ {
		field := portType.Field(i)

		// Get json tag for field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName == "" {
			continue
		}

		// Parse schema tag
		schemaTag := field.Tag.Get("schema")
		if schemaTag == "" {
			// No schema tag - default to read-only string
			fields[fieldName] = PortFieldInfo{
				Type:     "string",
				Editable: false,
			}
			continue
		}

		directives, err := ParseSchemaTag(schemaTag)
		if err != nil {
			// Skip fields with invalid tags
			continue
		}

		fields[fieldName] = PortFieldInfo{
			Type:     directives.Type,
			Editable: directives.Editable,
		}
	}

	return fields
}

// GenerateCacheFieldSchema generates metadata for cache.Config fields.
// This describes which fields in cache.Config are editable and their constraints,
// enabling the UI to render appropriate controls for cache configuration.
//
// The function examines cache.Config struct tags to determine:
//   - Field types (for appropriate UI controls)
//   - Editability (whether users can modify the field)
//   - Enum values (for strategy field)
//   - Numeric constraints (for size limits)
//
// All cache.Config fields are marked as "editable" to allow runtime configuration.
//
// This metadata is included in ConfigSchema for fields with type "cache",
// allowing the frontend to correctly render cache configuration forms.
//
// Returns:
//   - Map of field names to CacheFieldInfo with type, editability, and constraint metadata
func GenerateCacheFieldSchema() map[string]CacheFieldInfo {
	cacheType := reflect.TypeOf(cache.Config{})
	fields := make(map[string]CacheFieldInfo)

	for i := 0; i < cacheType.NumField(); i++ {
		field := cacheType.Field(i)

		// Get json tag for field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName == "" {
			continue
		}

		// Parse schema tag
		schemaTag := field.Tag.Get("schema")
		if schemaTag == "" {
			// No schema tag - default to read-only string
			fields[fieldName] = CacheFieldInfo{
				Type:     "string",
				Editable: false,
			}
			continue
		}

		directives, err := ParseSchemaTag(schemaTag)
		if err != nil {
			// Skip fields with invalid tags
			continue
		}

		info := CacheFieldInfo{
			Type:     directives.Type,
			Editable: directives.Editable,
			Enum:     directives.Enum,
			Min:      directives.Min,
		}

		fields[fieldName] = info
	}

	return fields
}
