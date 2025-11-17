// Package component provides schema validation and helper functions
package component

import (
	"fmt"
	"sort"
)

// ValidationError represents a validation error for a specific configuration field.
// It provides structured error information that can be displayed to users
// and mapped to specific form fields in the UI.
//
// Error codes are standardized across frontend and backend:
//   - "required": Field is required but missing
//   - "min": Numeric value below minimum threshold
//   - "max": Numeric value above maximum threshold
//   - "enum": Value not in allowed enum values
//   - "type": Value doesn't match expected type (string, int, bool, etc.)
//   - "pattern": String doesn't match required pattern (future use)
type ValidationError struct {
	Field   string `json:"field"`   // Name of the field that failed validation
	Message string `json:"message"` // Human-readable error message
	Code    string `json:"code"`    // Machine-readable error code (see above)
}

// ValidateConfig validates a configuration map against a ConfigSchema.
// It checks required fields, type constraints, min/max bounds, and enum values.
//
// The validation is lenient - unknown fields are allowed to support backward
// compatibility and future schema evolution. Only explicitly defined properties
// are validated against their schema constraints.
//
// Returns a slice of ValidationError containing all validation failures found.
// An empty slice indicates the configuration is valid.
//
// Example usage:
//
//	schema := component.ConfigSchema{
//	    Properties: map[string]component.PropertySchema{
//	        "port": {
//	            Type:     "int",
//	            Minimum:  ptrInt(1),
//	            Maximum:  ptrInt(65535),
//	            Category: "basic",
//	        },
//	    },
//	    Required: []string{"port"},
//	}
//
//	config := map[string]any{"port": 99999}
//	errors := component.ValidateConfig(config, schema)
//	if len(errors) > 0 {
//	    // Handle validation errors
//	    fmt.Printf("Validation failed: %s\n", errors[0].Message)
//	}
func ValidateConfig(config map[string]any, schema ConfigSchema) []ValidationError {
	var errors []ValidationError

	// Check required fields
	for _, requiredField := range schema.Required {
		if _, exists := config[requiredField]; !exists {
			errors = append(errors, ValidationError{
				Field:   requiredField,
				Message: fmt.Sprintf("Field %q is required", requiredField),
				Code:    "required",
			})
		}
	}

	// Validate each field in config
	for fieldName, value := range config {
		propSchema, exists := schema.Properties[fieldName]
		if !exists {
			// Unknown fields are allowed (lenient validation)
			continue
		}

		// Type validation
		if err := validateType(fieldName, value, propSchema); err != nil {
			errors = append(errors, *err)
			continue // Skip further validation if type is wrong
		}

		// Enum validation
		if len(propSchema.Enum) > 0 {
			if err := validateEnum(fieldName, value, propSchema.Enum); err != nil {
				errors = append(errors, *err)
			}
		}

		// Min/Max validation for numeric types
		if propSchema.Type == "int" || propSchema.Type == "float" {
			if propSchema.Minimum != nil {
				if err := validateMin(fieldName, value, *propSchema.Minimum); err != nil {
					errors = append(errors, *err)
				}
			}
			if propSchema.Maximum != nil {
				if err := validateMax(fieldName, value, *propSchema.Maximum); err != nil {
					errors = append(errors, *err)
				}
			}
		}
	}

	return errors
}

// validateType checks if the value matches the expected type
func validateType(fieldName string, value any, propSchema PropertySchema) *ValidationError {
	switch propSchema.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("Field %q must be a string", fieldName),
				Code:    "type",
			}
		}
	case "int":
		// Accept both int and float64 (JSON numbers)
		switch value.(type) {
		case int, int32, int64, float64:
			// Valid
		default:
			return &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("Field %q must be an integer", fieldName),
				Code:    "type",
			}
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("Field %q must be a boolean", fieldName),
				Code:    "type",
			}
		}
	case "float":
		// Accept int, float32, float64
		switch value.(type) {
		case int, int32, int64, float32, float64:
			// Valid
		default:
			return &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("Field %q must be a number", fieldName),
				Code:    "type",
			}
		}
	}
	return nil
}

// validateEnum checks if the value is in the allowed enum values
func validateEnum(fieldName string, value any, enumValues []string) *ValidationError {
	strValue, ok := value.(string)
	if !ok {
		return &ValidationError{
			Field:   fieldName,
			Message: fmt.Sprintf("Field %q must be a string for enum validation", fieldName),
			Code:    "type",
		}
	}

	for _, allowed := range enumValues {
		if strValue == allowed {
			return nil // Valid
		}
	}

	return &ValidationError{
		Field:   fieldName,
		Message: fmt.Sprintf("Field %q must be one of: %v", fieldName, enumValues),
		Code:    "enum",
	}
}

// validateMin checks if numeric value meets minimum
func validateMin(fieldName string, value any, min int) *ValidationError {
	var numValue float64
	switch v := value.(type) {
	case int:
		numValue = float64(v)
	case int32:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float32:
		numValue = float64(v)
	case float64:
		numValue = v
	default:
		return &ValidationError{
			Field:   fieldName,
			Message: fmt.Sprintf("Field %q must be numeric for min validation", fieldName),
			Code:    "type",
		}
	}

	if numValue < float64(min) {
		return &ValidationError{
			Field:   fieldName,
			Message: fmt.Sprintf("Field %q must be >= %d", fieldName, min),
			Code:    "min",
		}
	}
	return nil
}

// validateMax checks if numeric value meets maximum
func validateMax(fieldName string, value any, max int) *ValidationError {
	var numValue float64
	switch v := value.(type) {
	case int:
		numValue = float64(v)
	case int32:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float32:
		numValue = float64(v)
	case float64:
		numValue = v
	default:
		return &ValidationError{
			Field:   fieldName,
			Message: fmt.Sprintf("Field %q must be numeric for max validation", fieldName),
			Code:    "type",
		}
	}

	if numValue > float64(max) {
		return &ValidationError{
			Field:   fieldName,
			Message: fmt.Sprintf("Field %q must be <= %d", fieldName, max),
			Code:    "max",
		}
	}
	return nil
}

// GetPropertyValue safely extracts a property value from a configuration map.
//
// Returns the value and true if the key exists, or nil and false if the key
// is not present in the map. This function is nil-safe - passing a nil config
// will return (nil, false).
//
// Example:
//
//	config := map[string]any{"port": 8080, "host": "localhost"}
//	if port, exists := component.GetPropertyValue(config, "port"); exists {
//	    fmt.Printf("Port: %v\n", port)
//	}
func GetPropertyValue(config map[string]any, key string) (any, bool) {
	if config == nil {
		return nil, false
	}
	value, exists := config[key]
	return value, exists
}

// GetProperties filters schema properties by category for UI organization.
//
// Components can categorize their configuration properties as "basic" (shown by default)
// or "advanced" (hidden in collapsible section). This function extracts properties
// belonging to a specific category.
//
// Parameters:
//   - schema: The component's configuration schema
//   - category: Filter by "basic" or "advanced", or empty string for all properties
//
// Properties without an explicit Category field default to "advanced".
//
// Returns a map of property names to PropertySchema definitions matching the category.
//
// Example:
//
//	schema := component.ConfigSchema{
//	    Properties: map[string]component.PropertySchema{
//	        "port":        {Type: "int", Category: "basic"},
//	        "buffer_size": {Type: "int", Category: "advanced"},
//	        "timeout":     {Type: "int"}, // Defaults to "advanced"
//	    },
//	}
//
//	basicProps := component.GetProperties(schema, "basic")
//	// Returns: map["port": {...}]
//
//	advancedProps := component.GetProperties(schema, "advanced")
//	// Returns: map["buffer_size": {...}, "timeout": {...}]
func GetProperties(schema ConfigSchema, category string) map[string]PropertySchema {
	filtered := make(map[string]PropertySchema)

	for name, prop := range schema.Properties {
		// Default empty Category to "advanced"
		propCategory := prop.Category
		if propCategory == "" {
			propCategory = "advanced"
		}

		// If no category filter specified, return all
		if category == "" {
			filtered[name] = prop
			continue
		}

		// Filter by category
		if propCategory == category {
			filtered[name] = prop
		}
	}

	return filtered
}

// IsComplexType returns true if a property type requires complex rendering.
//
// Complex types (object, array) cannot be rendered as simple form inputs and
// require specialized UI components like JSON editors or nested form builders.
//
// Currently identifies "object" and "array" types as complex. In MVP, the UI
// falls back to a JSON editor for these types.
//
// Example:
//
//	if component.IsComplexType(propSchema.Type) {
//	    // Use JSON editor fallback
//	    renderJSONEditor(propSchema)
//	} else {
//	    // Render type-specific input field
//	    renderInputField(propSchema)
//	}
func IsComplexType(propType string) bool {
	return propType == "object" || propType == "array"
}

// SortedPropertyNames returns property names in UI display order.
//
// Properties are sorted by:
// 1. Category: "basic" properties first, then "advanced" properties
// 2. Alphabetically within each category
//
// This ensures consistent, predictable ordering in configuration UIs.
// Properties without an explicit Category default to "advanced".
//
// Example:
//
//	schema := component.ConfigSchema{
//	    Properties: map[string]component.PropertySchema{
//	        "port":         {Category: "basic"},
//	        "bind_address": {Category: "basic"},
//	        "buffer_size":  {Category: "advanced"},
//	        "timeout":      {}, // Defaults to "advanced"
//	    },
//	}
//
//	names := component.SortedPropertyNames(schema)
//	// Returns: ["bind_address", "port", "buffer_size", "timeout"]
//	//          ^-- basic (alpha) --^  ^---- advanced (alpha) ----^
func SortedPropertyNames(schema ConfigSchema) []string {
	type propertyWithName struct {
		name     string
		category string
	}

	// Collect all properties with their names
	var props []propertyWithName
	for name, prop := range schema.Properties {
		category := prop.Category
		if category == "" {
			category = "advanced"
		}
		props = append(props, propertyWithName{
			name:     name,
			category: category,
		})
	}

	// Sort by category (basic first) then alphabetically within category
	sort.Slice(props, func(i, j int) bool {
		// Basic comes before advanced
		if props[i].category != props[j].category {
			return props[i].category == "basic"
		}
		// Within same category, sort alphabetically
		return props[i].name < props[j].name
	})

	// Extract just the names
	names := make([]string, len(props))
	for i, prop := range props {
		names[i] = prop.name
	}

	return names
}
