package config

import (
	"strings"
	"testing"

	"github.com/c360/semstreams/component"
)

// TestConfigValidationErrors tests enhanced validation error structure (T020)
// Given: UDP component with invalid port
// When: PUT config with port=99999
// Then: 400 with errors[0].field="port", code="max", message clear
func TestConfigValidationErrors(t *testing.T) {
	// Setup
	schema := component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"port": {
				Type:    "int",
				Minimum: intPtr(1),
				Maximum: intPtr(65535),
			},
		},
		Required: []string{"port"},
	}

	invalidConfig := map[string]any{
		"port": 99999, // Exceeds max
	}

	// Execute
	errors := component.ValidateConfig(invalidConfig, schema)

	// Verify error structure
	if len(errors) == 0 {
		t.Fatal("Expected validation error")
	}

	err := errors[0]
	if err.Field != "port" {
		t.Errorf("Expected error on field 'port', got %q", err.Field)
	}

	if err.Code != "max" {
		t.Errorf("Expected error code 'max', got %q", err.Code)
	}

	if err.Message == "" {
		t.Error("Expected clear error message")
	}

	// Message should be user-friendly and mention the max value
	if !strings.Contains(err.Message, "65535") {
		t.Errorf("Expected message to contain max value 65535, got %q", err.Message)
	}
}

// TestConfigValidationMultipleErrors tests multiple validation errors (T020)
// Given: Config with multiple invalid fields
// When: Validation performed
// Then: All errors returned in array
func TestConfigValidationMultipleErrors(t *testing.T) {
	schema := component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
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

	invalidConfig := map[string]any{
		"port": 99999, // Exceeds max
		// Missing bind_address (required)
	}

	// Execute
	errors := component.ValidateConfig(invalidConfig, schema)

	// Should have at least 2 errors
	if len(errors) < 2 {
		t.Fatalf("Expected at least 2 errors, got %d", len(errors))
	}

	// Check for both expected errors
	var hasMaxError, hasRequiredError bool
	for _, err := range errors {
		if err.Field == "port" && err.Code == "max" {
			hasMaxError = true
		}
		if err.Field == "bind_address" && err.Code == "required" {
			hasRequiredError = true
		}
	}

	if !hasMaxError {
		t.Error("Expected max validation error for port")
	}
	if !hasRequiredError {
		t.Error("Expected required validation error for bind_address")
	}
}

// Helper function for creating int pointers
func intPtr(i int) *int {
	return &i
}
