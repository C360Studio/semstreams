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

	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
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
	Type                 string                   `json:"type"`
	Editable             bool                     `json:"editable"`
	Minimum              *int                     `json:"minimum,omitempty"`
	Enum                 []string                 `json:"enum,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	AnyRequired          [][]string               `json:"anyRequired,omitempty"`
	RequiredByDirection  map[Direction][]string   `json:"requiredByDirection,omitempty"`
	Directions           []Direction              `json:"directions,omitempty"`
	Properties           map[string]PortFieldInfo `json:"properties,omitempty"`
	Variants             map[string]PortFieldInfo `json:"variants,omitempty"`
	Items                *PortFieldInfo           `json:"items,omitempty"`
	AdditionalProperties *bool                    `json:"additionalProperties,omitempty"`
	zeroIsOmitted        bool
}

// ZeroIsOmitted reports whether numeric zero has omission semantics for this
// field. It is intentionally absent from discovery JSON: editors use the
// canonical Directions metadata, while schema projection uses this predicate
// to preserve an explicitly supplied zero as semantic absence.
func (p PortFieldInfo) ZeroIsOmitted() bool { return p.zeroIsOmitted }

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
		return directives, errs.WrapInvalid(
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
		return directives, errs.WrapInvalid(
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
		return errs.WrapInvalid(
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
		return errs.WrapInvalid(
			fmt.Errorf("invalid directive format: %s", part),
			"SchemaTag", "parseKeyValueDirective", "directive parsing",
		)
	}

	key := strings.TrimSpace(kv[0])
	value := strings.TrimSpace(kv[1])

	if value == "" {
		return errs.WrapInvalid(
			fmt.Errorf("empty value for directive: %s", key),
			"SchemaTag", "parseKeyValueDirective", "value validation",
		)
	}

	switch key {
	case "type":
		if !isValidType(value) {
			return errs.WrapInvalid(
				fmt.Errorf("invalid type: %s", value),
				"SchemaTag", "parseKeyValueDirective", "type validation",
			)
		}
		directives.Type = value

	case "description":
		directives.Description = value

	case "category":
		if value != "basic" && value != "advanced" {
			return errs.WrapInvalid(
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
			return errs.WrapInvalid(
				fmt.Errorf("invalid min value: %s", value),
				"SchemaTag", "parseKeyValueDirective", "min parsing",
			)
		}
		directives.Min = &n

	case "max":
		n, err := strconv.Atoi(value)
		if err != nil {
			return errs.WrapInvalid(
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
		return errs.WrapInvalid(
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

		// Special handling for "object" type - recursively expand nested struct
		if directives.Type == "object" {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				nestedSchema := generateNestedSchema(fieldType)
				propSchema.Properties = nestedSchema.Properties
				propSchema.Required = nestedSchema.Required
			}
		}

		// Special handling for "array" type - generate item schema from Go type
		if directives.Type == "array" {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
				propSchema.Items = generateItemSchema(fieldType.Elem())
			}
		}

		schema.Properties[fieldName] = propSchema

		// Add to required list if needed
		if directives.Required {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// generateNestedSchema generates a ConfigSchema from a nested struct type.
// This supports both `schema:` tags (preferred) and `description:` tags (fallback).
// Type is inferred from Go field types when using `description:` tags.
func generateNestedSchema(structType reflect.Type) ConfigSchema {
	schema := ConfigSchema{
		Properties: make(map[string]PropertySchema),
		Required:   []string{},
	}

	// Handle pointer types
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	// Ensure we're working with a struct
	if structType.Kind() != reflect.Struct {
		return schema
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Get json tag for field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		// Parse json tag to get field name
		jsonParts := strings.Split(jsonTag, ",")
		fieldName := jsonParts[0]
		if fieldName == "" {
			continue
		}

		var propSchema PropertySchema

		// Try schema: tag first (full directive parsing)
		schemaTag := field.Tag.Get("schema")
		if schemaTag != "" {
			directives, err := ParseSchemaTag(schemaTag)
			if err == nil {
				propSchema = PropertySchema{
					Type:        directives.Type,
					Description: directives.Description,
					Category:    directives.Category,
					Default:     convertDefault(directives.Default, directives.Type),
					Minimum:     directives.Min,
					Maximum:     directives.Max,
					Enum:        directives.Enum,
				}

				// Recursively handle nested objects
				if directives.Type == "object" {
					fieldType := field.Type
					if fieldType.Kind() == reflect.Ptr {
						fieldType = fieldType.Elem()
					}
					if fieldType.Kind() == reflect.Struct {
						nestedSchema := generateNestedSchema(fieldType)
						propSchema.Properties = nestedSchema.Properties
						propSchema.Required = nestedSchema.Required
					}
				}

				// Generate items schema for arrays
				if directives.Type == "array" {
					fieldType := field.Type
					if fieldType.Kind() == reflect.Ptr {
						fieldType = fieldType.Elem()
					}
					if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
						propSchema.Items = generateItemSchema(fieldType.Elem())
					}
				}

				if directives.Required {
					schema.Required = append(schema.Required, fieldName)
				}
			} else {
				continue // Skip fields with invalid schema tags
			}
		} else {
			// Fallback: infer type from Go type, use description: tag
			propSchema = inferPropertyFromType(field)
		}

		// Use field name as description fallback
		if propSchema.Description == "" {
			propSchema.Description = fieldName
		}

		schema.Properties[fieldName] = propSchema
	}

	return schema
}

// generateItemSchema creates a PropertySchema for array element types
func generateItemSchema(elemType reflect.Type) *PropertySchema {
	// Handle pointer types
	if elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
	}

	switch elemType.Kind() {
	case reflect.Struct:
		nested := generateNestedSchema(elemType)
		return &PropertySchema{
			Type:       "object",
			Properties: nested.Properties,
			Required:   nested.Required,
		}
	case reflect.String:
		return &PropertySchema{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &PropertySchema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &PropertySchema{Type: "number"}
	case reflect.Bool:
		return &PropertySchema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		// Nested array - recurse
		return &PropertySchema{
			Type:  "array",
			Items: generateItemSchema(elemType.Elem()),
		}
	case reflect.Map:
		return &PropertySchema{Type: "object"}
	default:
		return &PropertySchema{Type: "string"}
	}
}

// inferPropertyFromType infers a PropertySchema from a struct field's Go type.
// Uses the `description:` tag for documentation.
func inferPropertyFromType(field reflect.StructField) PropertySchema {
	propSchema := PropertySchema{
		Description: field.Tag.Get("description"),
	}

	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	switch fieldType.Kind() {
	case reflect.String:
		propSchema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		propSchema.Type = "int"
	case reflect.Float32, reflect.Float64:
		propSchema.Type = "float"
	case reflect.Bool:
		propSchema.Type = "bool"
	case reflect.Slice, reflect.Array:
		propSchema.Type = "array"
		propSchema.Items = generateItemSchema(fieldType.Elem())
	case reflect.Map:
		propSchema.Type = "object"
	case reflect.Struct:
		propSchema.Type = "object"
		// Recursively expand nested struct
		nestedSchema := generateNestedSchema(fieldType)
		propSchema.Properties = nestedSchema.Properties
		propSchema.Required = nestedSchema.Required
	default:
		propSchema.Type = "string" // Fallback
	}

	return propSchema
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

// GeneratePortFieldSchema returns the canonical common-envelope metadata.
// Kind-specific data remains inside config and is validated by the closed port
// binding table; schema generation does not reflect retired flat fields.
//
// This metadata is included in ConfigSchema for fields with type "ports",
// allowing the frontend to correctly render port configuration forms.
//
// Returns:
//   - Map of field names to PortFieldInfo with type and editability metadata
func GeneratePortFieldSchema() map[string]PortFieldInfo {
	variants := make(map[string]PortFieldInfo, len(portBindingTable))
	for _, kind := range portKinds() {
		binding, ok := portBindingTable[kind]
		if !ok {
			panic(fmt.Sprintf("canonical port kind %q has no binding", kind))
		}
		configType := reflect.TypeOf(binding.newConfig())
		for configType.Kind() == reflect.Pointer {
			configType = configType.Elem()
		}
		properties := map[string]PortFieldInfo{
			"kind": {Type: "enum", Enum: []string{string(kind)}},
		}
		for index := 0; index < configType.NumField(); index++ {
			field := configType.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			properties[name] = portConfigSchemaForType(field.Type)
		}
		for name, constraint := range binding.fieldConstraints {
			property, exists := properties[name]
			if !exists {
				panic(fmt.Sprintf("port kind %q constraint names absent field %q", kind, name))
			}
			if constraint.Type != "" && constraint.Type != property.Type {
				panic(fmt.Sprintf("port kind %q field %q constraint type %q conflicts with reflected type %q", kind, name, constraint.Type, property.Type))
			}
			property.Minimum = constraint.Minimum
			property.zeroIsOmitted = constraint.zeroIsOmitted
			property.Directions = append([]Direction(nil), constraint.Directions...)
			properties[name] = property
		}

		directions := make([]Direction, 0, len(binding.directions))
		for _, direction := range []Direction{DirectionInput, DirectionOutput} {
			if _, ok := binding.directions[direction]; ok {
				directions = append(directions, direction)
			}
		}
		closed := false
		variants[string(kind)] = PortFieldInfo{
			Type:                 "object",
			Editable:             true,
			Required:             append([]string{"kind"}, binding.required...),
			AnyRequired:          cloneStringGroups(binding.anyRequired),
			RequiredByDirection:  cloneDirectionRequirements(binding.requiredByDirection),
			Directions:           directions,
			Properties:           properties,
			AdditionalProperties: &closed,
		}
	}

	return map[string]PortFieldInfo{
		"name":        {Type: "string", Editable: false},
		"required":    {Type: "bool", Editable: false},
		"description": {Type: "string", Editable: false},
		"external":    {Type: "bool", Editable: false},
		"config":      {Type: "object", Editable: true, Variants: variants},
	}
}

func portConfigSchemaForType(fieldType reflect.Type) PortFieldInfo {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	switch fieldType.Kind() {
	case reflect.String:
		return PortFieldInfo{Type: "string", Editable: true}
	case reflect.Bool:
		return PortFieldInfo{Type: "bool", Editable: true}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return PortFieldInfo{Type: "int", Editable: true}
	case reflect.Slice, reflect.Array:
		item := portConfigSchemaForType(fieldType.Elem())
		return PortFieldInfo{Type: "array", Editable: true, Items: &item}
	case reflect.Struct:
		properties := make(map[string]PortFieldInfo, fieldType.NumField())
		for index := 0; index < fieldType.NumField(); index++ {
			field := fieldType.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			properties[name] = portConfigSchemaForType(field.Type)
		}
		closed := false
		required := []string(nil)
		if fieldType == reflect.TypeOf(InterfaceContract{}) {
			required = []string{"type"}
		}
		return PortFieldInfo{
			Type: "object", Editable: true, Required: required,
			Properties: properties, AdditionalProperties: &closed,
		}
	default:
		return PortFieldInfo{Type: "object", Editable: true}
	}
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
