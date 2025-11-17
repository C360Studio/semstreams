package types_test

import (
	"encoding/json"
	"testing"

	pkgerrors "github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/types"
)

func TestServiceConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      types.ServiceConfig
		expectError bool
		errorType   string
	}{
		{
			name: "valid service with config",
			config: types.ServiceConfig{
				Name:    "flow",
				Enabled: true,
				Config:  json.RawMessage(`{"max_flows": 100}`),
			},
			expectError: false,
		},
		{
			name: "valid service without config",
			config: types.ServiceConfig{
				Name:    "discovery",
				Enabled: true,
				Config:  nil,
			},
			expectError: false,
		},
		{
			name: "valid disabled service",
			config: types.ServiceConfig{
				Name:    "health",
				Enabled: false,
			},
			expectError: false,
		},
		{
			name: "empty name",
			config: types.ServiceConfig{
				Name:    "",
				Enabled: true,
			},
			expectError: true,
			errorType:   "invalid",
		},
		{
			name: "whitespace only name",
			config: types.ServiceConfig{
				Name:    "   ",
				Enabled: true,
			},
			expectError: false, // Validation doesn't trim whitespace
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

func TestServiceConfig_JSONRoundTrip(t *testing.T) {
	original := types.ServiceConfig{
		Name:    "flow",
		Enabled: true,
		Config:  json.RawMessage(`{"max_flows":100,"timeout":"30s"}`),
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded types.ServiceConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify fields
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

func TestPlatformMeta(t *testing.T) {
	// PlatformMeta is a simple struct with no validation
	// Just verify it can be created and used
	meta := types.PlatformMeta{
		Org:      "c360",
		Platform: "platform1",
	}

	if meta.Org != "c360" {
		t.Errorf("Org: got %v, want c360", meta.Org)
	}
	if meta.Platform != "platform1" {
		t.Errorf("Platform: got %v, want platform1", meta.Platform)
	}

	// Test zero values
	var zero types.PlatformMeta
	if zero.Org != "" || zero.Platform != "" {
		t.Error("zero value should have empty strings")
	}
}
