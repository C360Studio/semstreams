package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidFlowID(t *testing.T) {
	tests := []struct {
		name     string
		flowID   string
		expected bool
	}{
		{
			name:     "valid UUID",
			flowID:   "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "valid simple ID",
			flowID:   "flow-123",
			expected: true,
		},
		{
			name:     "empty string",
			flowID:   "",
			expected: false,
		},
		{
			name:     "contains > wildcard",
			flowID:   "flow-123>",
			expected: false,
		},
		{
			name:     "contains * wildcard",
			flowID:   "flow-*",
			expected: false,
		},
		{
			name:     "contains . separator",
			flowID:   "flow.123",
			expected: false,
		},
		{
			name:     "multiple wildcards",
			flowID:   "flow.*.>",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidFlowID(tt.flowID)
			assert.Equal(t, tt.expected, result, "isValidFlowID(%q) = %v, want %v", tt.flowID, result, tt.expected)
		})
	}
}

func TestIsValidComponentName(t *testing.T) {
	tests := []struct {
		name          string
		componentName string
		expected      bool
	}{
		{
			name:          "valid component name",
			componentName: "udp-source",
			expected:      true,
		},
		{
			name:          "empty string (no filter)",
			componentName: "",
			expected:      true,
		},
		{
			name:          "contains > wildcard",
			componentName: "component>",
			expected:      false,
		},
		{
			name:          "contains * wildcard",
			componentName: "component*",
			expected:      false,
		},
		{
			name:          "contains . separator",
			componentName: "component.name",
			expected:      false,
		},
		{
			name:          "multiple wildcards",
			componentName: "comp.*.>",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidComponentName(tt.componentName)
			assert.Equal(t, tt.expected, result, "isValidComponentName(%q) = %v, want %v", tt.componentName, result, tt.expected)
		})
	}
}
