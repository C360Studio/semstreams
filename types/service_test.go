package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/types"
)

func TestServiceConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config types.ServiceConfig
	}{
		{
			name: "valid service with config",
			config: types.ServiceConfig{
				Enabled: true,
				Config:  json.RawMessage(`{"max_flows": 100}`),
			},
		},
		{
			name: "valid service without config",
			config: types.ServiceConfig{
				Enabled: true,
				Config:  nil,
			},
		},
		{
			name: "valid disabled service",
			config: types.ServiceConfig{
				Enabled: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.config.Validate(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestServiceConfig_JSONRoundTrip(t *testing.T) {
	original := types.ServiceConfig{
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
	if decoded.Enabled != original.Enabled {
		t.Errorf("Enabled: got %v, want %v", decoded.Enabled, original.Enabled)
	}
	if string(decoded.Config) != string(original.Config) {
		t.Errorf("Config: got %v, want %v", string(decoded.Config), string(original.Config))
	}
}

func TestServiceConfigRejectsRetiredName(t *testing.T) {
	var decoded types.ServiceConfig
	err := json.Unmarshal([]byte(`{"name":"metrics","enabled":true,"config":{}}`), &decoded)
	if err == nil || !strings.Contains(err.Error(), "unknown field \"name\"") {
		t.Fatalf("retired name error = %v", err)
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
