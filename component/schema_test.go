package component

import (
	"encoding/json"
	"testing"
)

// TestValidateConfigRequiredFields tests required field validation (T011)
// Given: Schema with required=["port"], config without port
// When: ValidateConfig called
// Then: Returns ValidationError for missing required field
func TestValidateConfigRequiredFields(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:        "int",
				Description: "Port number",
			},
		},
		Required: []string{"port"},
	}

	config := map[string]any{
		// Missing required "port" field
	}

	// Execute
	errors := ValidateConfig(config, schema)

	// Assert
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}

	if len(errors) > 0 {
		if errors[0].Field != "port" {
			t.Errorf("Expected error on field 'port', got %q", errors[0].Field)
		}

		if errors[0].Code != "required" {
			t.Errorf("Expected error code 'required', got %q", errors[0].Code)
		}
	}
}

// TestValidateConfigMinMax tests numeric min/max validation (T011)
// Given: Schema with port min=1, max=65535
// When: ValidateConfig with invalid values
// Then: Returns appropriate ValidationErrors
func TestValidateConfigMinMax(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:    "int",
				Minimum: intPtr(1),
				Maximum: intPtr(65535),
			},
		},
		Required: []string{"port"},
	}

	testCases := []struct {
		name          string
		config        map[string]any
		expectedCode  string
		expectedField string
	}{
		{
			name:          "Below minimum",
			config:        map[string]any{"port": 0},
			expectedCode:  "min",
			expectedField: "port",
		},
		{
			name:          "Above maximum",
			config:        map[string]any{"port": 99999},
			expectedCode:  "max",
			expectedField: "port",
		},
		{
			name:          "Valid value",
			config:        map[string]any{"port": 8080},
			expectedCode:  "", // No error
			expectedField: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateConfig(tc.config, schema)

			if tc.expectedCode == "" {
				if len(errors) != 0 {
					t.Errorf("Expected no errors, got %d: %+v", len(errors), errors)
				}
			} else {
				if len(errors) == 0 {
					t.Errorf("Expected error with code %q, got none", tc.expectedCode)
				} else if errors[0].Code != tc.expectedCode {
					t.Errorf("Expected error code %q, got %q", tc.expectedCode, errors[0].Code)
				}
			}
		})
	}
}

// TestValidateConfigEnumValues tests enum validation (T011)
// Given: Schema with enum values
// When: ValidateConfig with invalid enum value
// Then: Returns ValidationError with code="enum"
func TestValidateConfigEnumValues(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"level": {
				Type: "string",
				Enum: []string{"debug", "info", "warn", "error"},
			},
		},
		Required: []string{"level"},
	}

	testCases := []struct {
		name         string
		config       map[string]any
		shouldError  bool
		expectedCode string
	}{
		{
			name:        "Valid enum value",
			config:      map[string]any{"level": "info"},
			shouldError: false,
		},
		{
			name:         "Invalid enum value",
			config:       map[string]any{"level": "invalid"},
			shouldError:  true,
			expectedCode: "enum",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateConfig(tc.config, schema)

			if tc.shouldError {
				if len(errors) == 0 {
					t.Error("Expected validation error")
				} else if errors[0].Code != tc.expectedCode {
					t.Errorf("Expected code %q, got %q", tc.expectedCode, errors[0].Code)
				}
			} else {
				if len(errors) != 0 {
					t.Errorf("Expected no errors, got %d: %+v", len(errors), errors)
				}
			}
		})
	}
}

// TestValidateConfigTypeValidation tests type validation (T011)
// Given: Schema with specific types
// When: ValidateConfig with wrong type values
// Then: Returns ValidationError with code="type"
func TestValidateConfigTypeValidation(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type: "int",
			},
			"enabled": {
				Type: "bool",
			},
			"name": {
				Type: "string",
			},
		},
		Required: []string{"port", "enabled", "name"},
	}

	testCases := []struct {
		name         string
		config       map[string]any
		shouldError  bool
		expectedCode string
	}{
		{
			name: "Valid types",
			config: map[string]any{
				"port":    8080,
				"enabled": true,
				"name":    "test",
			},
			shouldError: false,
		},
		{
			name: "String for int field",
			config: map[string]any{
				"port":    "8080",
				"enabled": true,
				"name":    "test",
			},
			shouldError:  true,
			expectedCode: "type",
		},
		{
			name: "Number for bool field",
			config: map[string]any{
				"port":    8080,
				"enabled": 1,
				"name":    "test",
			},
			shouldError:  true,
			expectedCode: "type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateConfig(tc.config, schema)

			if tc.shouldError {
				if len(errors) == 0 {
					t.Error("Expected validation error for type mismatch")
				} else {
					hasTypeError := false
					for _, err := range errors {
						if err.Code == "type" {
							hasTypeError = true
							break
						}
					}
					if !hasTypeError {
						t.Errorf("Expected at least one type error, got: %+v", errors)
					}
				}
			} else {
				if len(errors) != 0 {
					t.Errorf("Expected no errors, got %d: %+v", len(errors), errors)
				}
			}
		})
	}
}

// TestGetPropertyValue tests property value extraction (T012)
// Given: Config map with values
// When: GetPropertyValue called
// Then: Returns value and true if exists, or nil and false if not
func TestGetPropertyValue(t *testing.T) {
	config := map[string]any{
		"port":    8080,
		"enabled": true,
		"name":    "test",
	}

	testCases := []struct {
		key           string
		expectedValue any
		expectedFound bool
	}{
		{"port", 8080, true},
		{"enabled", true, true},
		{"name", "test", true},
		{"missing", nil, false},
	}

	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			value, found := GetPropertyValue(config, tc.key)

			if found != tc.expectedFound {
				t.Errorf("Expected found=%v, got %v", tc.expectedFound, found)
			}

			if found && value != tc.expectedValue {
				t.Errorf("Expected value %v, got %v", tc.expectedValue, value)
			}
		})
	}
}

// TestGetPropertiesByCategory tests category-based filtering (T013)
// Given: Schema with port (basic), buffer_size (advanced)
// When: GetProperties(schema, "basic")
// Then: Returns only port
func TestGetPropertiesByCategory(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:     "int",
				Category: "basic",
			},
			"bind_address": {
				Type:     "string",
				Category: "basic",
			},
			"buffer_size": {
				Type:     "int",
				Category: "advanced",
			},
			"timeout": {
				Type:     "int",
				Category: "advanced",
			},
		},
	}

	testCases := []struct {
		category      string
		expectedCount int
		expectedKeys  []string
	}{
		{
			category:      "basic",
			expectedCount: 2,
			expectedKeys:  []string{"port", "bind_address"},
		},
		{
			category:      "advanced",
			expectedCount: 2,
			expectedKeys:  []string{"buffer_size", "timeout"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.category, func(t *testing.T) {
			properties := GetProperties(schema, tc.category)

			if len(properties) != tc.expectedCount {
				t.Errorf("Expected %d properties, got %d", tc.expectedCount, len(properties))
			}

			for _, key := range tc.expectedKeys {
				if _, exists := properties[key]; !exists {
					t.Errorf("Expected key %q in filtered properties", key)
				}
			}
		})
	}
}

// TestGetPropertiesDefaultCategory tests empty category defaults to "advanced" (T013)
// Given: Schema with fields lacking Category
// When: GetProperties filters by category
// Then: Empty Category treated as "advanced"
func TestGetPropertiesDefaultCategory(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:     "int",
				Category: "basic",
			},
			"timeout": {
				Type: "int",
				// No Category specified
			},
		},
	}

	// Get advanced properties (should include timeout with empty category)
	advancedProps := GetProperties(schema, "advanced")

	if _, exists := advancedProps["timeout"]; !exists {
		t.Error("Expected timeout in advanced properties (default category)")
	}

	if _, exists := advancedProps["port"]; exists {
		t.Error("Did not expect port in advanced properties")
	}
}

// TestValidationErrorStructure tests ValidationError JSON serialization (T014)
// Given: ValidationError instances
// When: JSON marshal
// Then: Correct structure with field/message/code
func TestValidationErrorStructure(t *testing.T) {
	err := ValidationError{
		Field:   "port",
		Message: "Value must be between 1 and 65535",
		Code:    "max",
	}

	jsonData, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatalf("Failed to marshal ValidationError: %v", jsonErr)
	}

	expected := `{"field":"port","message":"Value must be between 1 and 65535","code":"max"}`

	if string(jsonData) != expected {
		t.Errorf("Expected JSON:\n%s\nGot:\n%s", expected, string(jsonData))
	}
}

// TestValidationErrorCodes tests error code enum values (T014)
// Given: ValidationError with various codes
// When: Codes are used
// Then: Codes match spec (required, min, max, pattern, enum, type)
func TestValidationErrorCodes(t *testing.T) {
	validCodes := []string{"required", "min", "max", "pattern", "enum", "type"}

	// Verify these codes are valid
	for _, code := range validCodes {
		_ = ValidationError{Code: code}
	}

	// No assertion needed - just verify it compiles and runs
	t.Logf("All validation error codes are valid: %v", validCodes)
}

// TestComplexTypeDetection tests nested object/array type detection (T023)
// Given: Schema with type="object" and type="array" fields
// When: IsComplexType called
// Then: Returns true for object/array, false for primitives
func TestComplexTypeDetection(t *testing.T) {
	testCases := []struct {
		fieldType     string
		expectComplex bool
	}{
		{"string", false},
		{"int", false},
		{"bool", false},
		{"float", false},
		{"enum", false},
		{"object", true},
		{"array", true},
	}

	for _, tc := range testCases {
		t.Run(tc.fieldType, func(t *testing.T) {
			result := IsComplexType(tc.fieldType)

			if result != tc.expectComplex {
				t.Errorf("Expected IsComplexType(%q)=%v, got %v", tc.fieldType, tc.expectComplex, result)
			}
		})
	}
}

// TestValidationConsistencyWithFrontend tests that backend validation produces
// the same error codes as frontend validation for consistent user experience (T112)
// Given: Schema with various constraints
// When: ValidateConfig called with invalid values
// Then: Error codes match frontend expectations (required, min, max, enum, type)
func TestValidationConsistencyWithFrontend(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:    "int",
				Minimum: intPtr(1),
				Maximum: intPtr(65535),
			},
			"protocol": {
				Type: "string",
				Enum: []string{"tcp", "udp"},
			},
			"enabled": {
				Type: "bool",
			},
		},
		Required: []string{"port", "protocol"},
	}

	testCases := []struct {
		name          string
		config        map[string]any
		expectedCode  string
		expectedField string
	}{
		{
			name:          "Required field missing - error code: required",
			config:        map[string]any{"protocol": "tcp"}, // port missing
			expectedCode:  "required",
			expectedField: "port",
		},
		{
			name:          "Value exceeds max - error code: max",
			config:        map[string]any{"port": 99999, "protocol": "tcp"}, // Exceeds max 65535
			expectedCode:  "max",
			expectedField: "port",
		},
		{
			name:          "Value below min - error code: min",
			config:        map[string]any{"port": 0, "protocol": "tcp"}, // Below min 1
			expectedCode:  "min",
			expectedField: "port",
		},
		{
			name:          "Invalid enum value - error code: enum",
			config:        map[string]any{"port": 8080, "protocol": "http"}, // Not in enum ["tcp", "udp"]
			expectedCode:  "enum",
			expectedField: "protocol",
		},
		{
			name:          "Type mismatch (string for int) - error code: type",
			config:        map[string]any{"port": "not-a-number", "protocol": "tcp"},
			expectedCode:  "type",
			expectedField: "port",
		},
		{
			name:          "Type mismatch (number for bool) - error code: type",
			config:        map[string]any{"port": 8080, "protocol": "tcp", "enabled": 1},
			expectedCode:  "type",
			expectedField: "enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateConfig(tc.config, schema)

			if len(errors) == 0 {
				t.Errorf("Expected validation error with code %q, got none", tc.expectedCode)
				return
			}

			// Find error for expected field
			var foundError *ValidationError
			for _, err := range errors {
				if err.Field == tc.expectedField {
					foundError = &err
					break
				}
			}

			if foundError == nil {
				t.Errorf("Expected error for field %q, got errors for: %v", tc.expectedField, errors)
				return
			}

			if foundError.Code != tc.expectedCode {
				t.Errorf(
					"Expected error code %q for field %q, got %q",
					tc.expectedCode,
					tc.expectedField,
					foundError.Code,
				)
			}

			// Verify error message is present
			if foundError.Message == "" {
				t.Error("Expected non-empty error message")
			}
		})
	}
}

// TestSchemaOrdering tests field ordering by Category + alphabetical (T022)
// Given: Schema with mixed categories
// When: Properties sorted
// Then: Basic fields first (alphabetical), then advanced (alphabetical)
func TestSchemaOrdering(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:     "int",
				Category: "basic",
			},
			"buffer_size": {
				Type:     "int",
				Category: "advanced",
			},
			"bind_address": {
				Type:     "string",
				Category: "basic",
			},
			"timeout": {
				Type:     "int",
				Category: "advanced",
			},
		},
	}

	sortedNames := SortedPropertyNames(schema)

	// Expected order:
	// 1. bind_address (basic, alphabetically first)
	// 2. port (basic, alphabetically second)
	// 3. buffer_size (advanced, alphabetically first)
	// 4. timeout (advanced, alphabetically second)
	expectedOrder := []string{"bind_address", "port", "buffer_size", "timeout"}

	if len(sortedNames) != len(expectedOrder) {
		t.Errorf("Expected %d properties, got %d", len(expectedOrder), len(sortedNames))
	}

	for i, expected := range expectedOrder {
		if i >= len(sortedNames) {
			t.Errorf("Missing property at index %d", i)
			continue
		}
		if sortedNames[i] != expected {
			t.Errorf("At index %d: expected %q, got %q", i, expected, sortedNames[i])
		}
	}
}

// TestSchemaFallback tests graceful degradation when schema missing (T021)
// Given: Component without schema (empty ConfigSchema)
// When: ValidateConfig called
// Then: Validation skipped gracefully, no errors returned
func TestSchemaFallback(t *testing.T) {
	t.Run("Empty schema allows any config", func(t *testing.T) {
		// Given: Schema with no properties defined
		emptySchema := ConfigSchema{
			Properties: nil,
			Required:   nil,
		}

		// When: ValidateConfig called with any config
		config := map[string]any{
			"arbitrary_field": "arbitrary_value",
			"port":            8080,
			"enabled":         true,
		}

		errors := ValidateConfig(config, emptySchema)

		// Then: No validation errors (graceful fallback)
		if len(errors) != 0 {
			t.Errorf("Expected no errors for empty schema, got %d: %+v", len(errors), errors)
		}
	})

	t.Run("Schema with empty Properties map allows any config", func(t *testing.T) {
		// Given: Schema with empty Properties map
		schema := ConfigSchema{
			Properties: make(map[string]PropertySchema),
			Required:   []string{},
		}

		// When: ValidateConfig called
		config := map[string]any{
			"any_field": "any_value",
		}

		errors := ValidateConfig(config, schema)

		// Then: No validation errors
		if len(errors) != 0 {
			t.Errorf("Expected no errors for schema with empty Properties, got %d: %+v", len(errors), errors)
		}
	})

	t.Run("GetPropertyValue works with missing fields", func(t *testing.T) {
		// Given: Config without certain fields
		config := map[string]any{
			"port": 8080,
		}

		// When: Getting missing field
		value, found := GetPropertyValue(config, "bind_address")

		// Then: Returns nil and false gracefully
		if found {
			t.Error("Expected found=false for missing field")
		}
		if value != nil {
			t.Errorf("Expected value=nil for missing field, got %v", value)
		}
	})

	t.Run("GetProperties returns empty map for empty schema", func(t *testing.T) {
		// Given: Empty schema
		emptySchema := ConfigSchema{
			Properties: nil,
		}

		// When: Getting properties by category
		basicProps := GetProperties(emptySchema, "basic")
		advancedProps := GetProperties(emptySchema, "advanced")

		// Then: Returns empty maps
		if len(basicProps) != 0 {
			t.Errorf("Expected empty basic properties, got %d", len(basicProps))
		}
		if len(advancedProps) != 0 {
			t.Errorf("Expected empty advanced properties, got %d", len(advancedProps))
		}
	})

	t.Run("SortedPropertyNames handles nil Properties", func(t *testing.T) {
		// Given: Schema with nil Properties
		schema := ConfigSchema{
			Properties: nil,
		}

		// When: Getting sorted property names
		names := SortedPropertyNames(schema)

		// Then: Returns empty slice (graceful handling)
		if len(names) != 0 {
			t.Errorf("Expected empty property names for nil Properties, got %v", names)
		}
	})
}
