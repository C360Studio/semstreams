package types_test

import (
	"encoding/json"
	"testing"

	pkgerrors "github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/types"
)

func TestComponentConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      types.ComponentConfig
		expectError bool
		errorType   string
	}{
		{
			name: "valid input component",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeInput,
				Name:    "udp",
				Enabled: true,
				Config:  json.RawMessage(`{"port": 14550}`),
			},
			expectError: false,
		},
		{
			name: "valid processor component",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "json_filter",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			expectError: false,
		},
		{
			name: "valid output component",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeOutput,
				Name:    "websocket",
				Enabled: false,
				Config:  nil,
			},
			expectError: false,
		},
		{
			name: "valid storage component",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeStorage,
				Name:    "objectstore",
				Enabled: true,
			},
			expectError: false,
		},
		{
			name: "valid gateway component",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeGateway,
				Name:    "http",
				Enabled: true,
				Config:  json.RawMessage(`{"read_only": true}`),
			},
			expectError: false,
		},
		{
			name: "empty type",
			config: types.ComponentConfig{
				Type:    "",
				Name:    "udp",
				Enabled: true,
			},
			expectError: true,
			errorType:   "invalid",
		},
		{
			name: "empty name",
			config: types.ComponentConfig{
				Type:    types.ComponentTypeInput,
				Name:    "",
				Enabled: true,
			},
			expectError: true,
			errorType:   "invalid",
		},
		{
			name: "invalid component type",
			config: types.ComponentConfig{
				Type:    types.ComponentType("invalid"),
				Name:    "test",
				Enabled: true,
			},
			expectError: true,
			errorType:   "invalid",
		},
		{
			name: "both empty type and name",
			config: types.ComponentConfig{
				Type:    "",
				Name:    "",
				Enabled: true,
			},
			expectError: true,
			errorType:   "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}

				// Verify error classification
				if tt.errorType == "invalid" {
					if !pkgerrors.IsInvalid(err) {
						t.Errorf("expected Invalid error classification, got: %v", err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestComponentTypeString(t *testing.T) {
	tests := []struct {
		name     string
		ct       types.ComponentType
		expected string
	}{
		{
			name:     "input type",
			ct:       types.ComponentTypeInput,
			expected: "input",
		},
		{
			name:     "processor type",
			ct:       types.ComponentTypeProcessor,
			expected: "processor",
		},
		{
			name:     "output type",
			ct:       types.ComponentTypeOutput,
			expected: "output",
		},
		{
			name:     "storage type",
			ct:       types.ComponentTypeStorage,
			expected: "storage",
		},
		{
			name:     "gateway type",
			ct:       types.ComponentTypeGateway,
			expected: "gateway",
		},
		{
			name:     "custom type",
			ct:       types.ComponentType("custom"),
			expected: "custom",
		},
		{
			name:     "empty type",
			ct:       types.ComponentType(""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ct.String()
			if got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestComponentConfig_JSONRoundTrip(t *testing.T) {
	original := types.ComponentConfig{
		Type:    types.ComponentTypeInput,
		Name:    "udp",
		Enabled: true,
		Config:  json.RawMessage(`{"port":14550,"bind":"0.0.0.0"}`),
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded types.ComponentConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.Type != original.Type {
		t.Errorf("Type: got %v, want %v", decoded.Type, original.Type)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: got %v, want %v", decoded.Name, original.Name)
	}
	if decoded.Enabled != original.Enabled {
		t.Errorf("Enabled: got %v, want %v", decoded.Enabled, original.Enabled)
	}
	if string(decoded.Config) != string(original.Config) {
		t.Errorf("Config: got %v, want %v", string(decoded.Config), string(original.Config))
	}
}
