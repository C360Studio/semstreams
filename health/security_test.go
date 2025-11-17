package health

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Unix file path",
			input:    "failed to open /etc/semstreams/config.json",
			expected: "failed to open [PATH]",
		},
		{
			name:     "Windows file path",
			input:    "cannot read C:\\Users\\Admin\\config.json",
			expected: "cannot read [PATH]",
		},
		{
			name:     "HTTP URL",
			input:    "connection failed to https://api.example.com/v1/health",
			expected: "connection failed to [URL]",
		},
		{
			name:     "NATS URL",
			input:    "cannot connect to nats://localhost:4222",
			expected: "cannot connect to [URL]",
		},
		{
			name:     "IP address",
			input:    "timeout connecting to 192.168.1.100",
			expected: "timeout connecting to [IP]",
		},
		{
			name:     "Port number",
			input:    "failed to bind to :8080",
			expected: "failed to bind to [PORT]",
		},
		{
			name:     "Credentials in error",
			input:    "auth failed with password:secretpass123",
			expected: "auth failed with [REDACTED]",
		},
		{
			name:     "Complex error with multiple sensitive items",
			input:    "failed to connect to https://192.168.1.1:8080/api with token=abc123def",
			expected: "failed to connect to [URL] with [REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeErrorMessage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithSubStatus_SliceIsolation(t *testing.T) {
	// Create original status with sub-statuses
	original := Status{
		Component: "parent",
		Status:    "healthy",
		SubStatuses: []Status{
			{Component: "child1", Status: "healthy"},
		},
	}

	// Add a new sub-status
	modified := original.WithSubStatus(Status{
		Component: "child2",
		Status:    "unhealthy",
	})

	// Verify original is unchanged
	assert.Len(t, original.SubStatuses, 1, "Original should still have 1 sub-status")
	assert.Len(t, modified.SubStatuses, 2, "Modified should have 2 sub-statuses")

	// Verify they don't share the underlying array
	assert.Equal(t, "child1", original.SubStatuses[0].Component)
	assert.Equal(t, "child1", modified.SubStatuses[0].Component)
	assert.Equal(t, "child2", modified.SubStatuses[1].Component)

	// Modify the original's sub-status
	original.SubStatuses[0].Status = "degraded"

	// Verify modified is unaffected
	assert.Equal(t, "degraded", original.SubStatuses[0].Status)
	assert.Equal(t, "healthy", modified.SubStatuses[0].Status, "Modified should not be affected by changes to original")
}
