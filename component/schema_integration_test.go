//go:build integration

package component

import (
	"testing"
)

// TestSchemaBasedConfigValidation tests config validation against schema (T017)
// Given: Component schema with validation rules
// When: Config validated against schema
// Then: Structured errors returned for invalid configs
func TestSchemaBasedConfigValidation(t *testing.T) {
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:    "int",
				Minimum: intPtr(1),
				Maximum: intPtr(65535),
			},
			"bind_address": {
				Type: "string",
			},
		},
		Required: []string{"port", "bind_address"},
	}

	testCases := []struct {
		name          string
		config        map[string]any
		shouldPass    bool
		expectedField string
		expectedCode  string
	}{
		{
			name: "Valid config passes",
			config: map[string]any{
				"port":         8080,
				"bind_address": "0.0.0.0",
			},
			shouldPass: true,
		},
		{
			name: "Invalid port exceeds max",
			config: map[string]any{
				"port":         99999,
				"bind_address": "0.0.0.0",
			},
			shouldPass:    false,
			expectedField: "port",
			expectedCode:  "max",
		},
		{
			name: "Missing required field",
			config: map[string]any{
				"port": 8080,
				// Missing bind_address
			},
			shouldPass:    false,
			expectedField: "bind_address",
			expectedCode:  "required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateConfig(tc.config, schema)

			if tc.shouldPass {
				if len(errors) != 0 {
					t.Errorf("Expected no errors, got %d: %v", len(errors), errors)
				}
			} else {
				if len(errors) == 0 {
					t.Error("Expected validation error")
				}
				found := false
				for _, err := range errors {
					if err.Field == tc.expectedField && err.Code == tc.expectedCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error on field %q with code %q, got errors: %v", tc.expectedField, tc.expectedCode, errors)
				}
			}
		})
	}
}

// Helper function for creating int pointers
func intPtr(i int) *int {
	return &i
}
