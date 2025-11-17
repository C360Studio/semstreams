package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformConfig_OrgValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError string
	}{
		{
			name: "valid org",
			config: &Config{
				Platform: PlatformConfig{
					Org: "c360",
					ID:  "platform1",
				},
			},
			wantError: "",
		},
		{
			name: "org normalized to lowercase",
			config: &Config{
				Platform: PlatformConfig{
					Org: "C360",
					ID:  "platform1",
				},
			},
			wantError: "",
		},
		{
			name: "missing org",
			config: &Config{
				Platform: PlatformConfig{
					ID: "platform1",
				},
			},
			wantError: "platform.org is required",
		},
		{
			name: "org with invalid characters",
			config: &Config{
				Platform: PlatformConfig{
					Org: "c360@corp",
					ID:  "platform1",
				},
			},
			wantError: "platform.org 'c360@corp' is not valid for NATS subjects",
		},
		{
			name: "org with spaces",
			config: &Config{
				Platform: PlatformConfig{
					Org: "c360 corp",
					ID:  "platform1",
				},
			},
			wantError: "platform.org 'c360 corp' is not valid for NATS subjects",
		},
		{
			name: "valid org with dots and dashes",
			config: &Config{
				Platform: PlatformConfig{
					Org: "c360-corp.dev",
					ID:  "platform1",
				},
			},
			wantError: "",
		},
		{
			name: "valid org with underscore s",
			config: &Config{
				Platform: PlatformConfig{
					Org: "c360_corp",
					ID:  "platform1",
				},
			},
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantError == "" {
				assert.NoError(t, err)
				// Verify normalization to lowercase
				if tt.name == "org normalized to lowercase" {
					assert.Equal(t, "c360", tt.config.Platform.Org, "org should be normalized to lowercase")
				}
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			}
		})
	}
}

func TestIsValidNATSSubjectPart(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"c360", true},
		{"C360", true}, // Will be lowercased before validation
		{"c360-corp", true},
		{"c360_corp", true},
		{"c360.corp", true},
		{"123org", true},
		{"", false},
		{"c360@corp", false},
		{"c360 corp", false},
		{"c360#corp", false},
		{"c360!corp", false},
		{"c360*", false},
		{"c360>", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isValidNATSSubjectPart(tt.input)
			assert.Equal(t, tt.valid, result, "isValidNATSSubjectPart(%q) = %v, want %v", tt.input, result, tt.valid)
		})
	}
}
