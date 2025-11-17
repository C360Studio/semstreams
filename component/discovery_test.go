package component

import (
	"encoding/json"
	"testing"
)

// TestPropertySchemaCategorySerialization tests Category field JSON marshaling (T010)
// Given: PropertySchema with Category="basic"
// When: JSON marshal/unmarshal
// Then: Category preserved, omitempty works when empty
func TestPropertySchemaCategorySerialization(t *testing.T) {
	testCases := []struct {
		name     string
		schema   PropertySchema
		expected string
	}{
		{
			name: "Category basic",
			schema: PropertySchema{
				Type:        "string",
				Description: "Test field",
				Category:    "basic",
			},
			expected: `{"type":"string","description":"Test field","category":"basic"}`,
		},
		{
			name: "Category advanced",
			schema: PropertySchema{
				Type:        "int",
				Description: "Advanced field",
				Category:    "advanced",
			},
			expected: `{"type":"int","description":"Advanced field","category":"advanced"}`,
		},
		{
			name: "Category empty (omitempty)",
			schema: PropertySchema{
				Type:        "bool",
				Description: "No category",
				Category:    "",
			},
			// Should not include category field when empty
			expected: `{"type":"bool","description":"No category"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			jsonData, err := json.Marshal(tc.schema)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Verify JSON output
			if string(jsonData) != tc.expected {
				t.Errorf("Expected JSON:\n%s\nGot:\n%s", tc.expected, string(jsonData))
			}

			// Unmarshal back
			var unmarshaled PropertySchema
			if err := json.Unmarshal(jsonData, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			// Verify Category preserved
			if unmarshaled.Category != tc.schema.Category {
				t.Errorf("Expected Category %q, got %q", tc.schema.Category, unmarshaled.Category)
			}
		})
	}
}

// TestPropertySchemaBackwardCompatibility tests omitempty ensures backward compatibility (T010)
// Given: JSON without Category field (old format)
// When: Unmarshal into PropertySchema
// Then: No error, Category empty string
func TestPropertySchemaBackwardCompatibility(t *testing.T) {
	// Old JSON format without Category field
	oldJSON := `{"type":"string","description":"Legacy field","default":"value"}`

	var schema PropertySchema
	if err := json.Unmarshal([]byte(oldJSON), &schema); err != nil {
		t.Fatalf("Failed to unmarshal old format: %v", err)
	}

	// Should have empty Category, not cause errors
	if schema.Category != "" {
		t.Errorf("Expected empty Category for old format, got %q", schema.Category)
	}

	// Other fields should be present
	if schema.Type != "string" {
		t.Errorf("Expected Type string, got %q", schema.Type)
	}

	if schema.Description != "Legacy field" {
		t.Errorf("Expected Description 'Legacy field', got %q", schema.Description)
	}
}
