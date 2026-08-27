package agentic_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpsDiagnosisEntityID(t *testing.T) {
	t.Run("valid constructions", func(t *testing.T) {
		tests := []struct {
			name     string
			org      string
			platform string
			id       string
			want     string
		}{
			{
				name:     "uuid-style id with hyphens",
				org:      "acme",
				platform: "ops",
				id:       "550e8400-e29b-41d4-a716-446655440000",
				want:     "acme.ops.ops.diagnosis.finding.550e8400-e29b-41d4-a716-446655440000",
			},
			{
				name:     "short alphanumeric id",
				org:      "c360",
				platform: "prod",
				id:       "abc123",
				want:     "c360.prod.ops.diagnosis.finding.abc123",
			},
			{
				name:     "id with underscores",
				org:      "myorg",
				platform: "staging",
				id:       "finding_42",
				want:     "myorg.staging.ops.diagnosis.finding.finding_42",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := agentic.OpsDiagnosisEntityID(tt.org, tt.platform, tt.id)
				require.Equal(t, tt.want, got)
				assert.True(t, message.IsValidEntityID(got), "result %q must pass IsValidEntityID", got)
			})
		}
	})

	t.Run("shape constraints", func(t *testing.T) {
		got := agentic.OpsDiagnosisEntityID("acme", "ops", "abc123")
		parts := strings.Split(got, ".")
		require.Len(t, parts, 6, "entity ID must have exactly 6 parts")
		assert.Equal(t, "acme", parts[0], "part[0] = org")
		assert.Equal(t, "ops", parts[1], "part[1] = platform")
		assert.Equal(t, "ops", parts[2], "part[2] = domain")
		assert.Equal(t, "diagnosis", parts[3], "part[3] = system")
		assert.Equal(t, "finding", parts[4], "part[4] = type")
		assert.Equal(t, "abc123", parts[5], "part[5] = instance (the unique id)")
	})

	t.Run("panics on invalid input", func(t *testing.T) {
		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("", "ops", "abc123")
		}, "empty org should panic")

		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("acme", "", "abc123")
		}, "empty platform should panic")

		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("acme", "ops", "")
		}, "empty id should panic")

		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("acme.corp", "ops", "abc123")
		}, "dot in org should panic")

		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("acme", "ops.prod", "abc123")
		}, "dot in platform should panic")

		assert.Panics(t, func() {
			agentic.OpsDiagnosisEntityID("acme", "ops", "finding.1")
		}, "dot in id should panic")
	})
}

// --- OpsDiagnosisMessageType (gh#390 typed-origin envelope) ---

func TestOpsDiagnosisMessageType_Valid(t *testing.T) {
	mt := agentic.OpsDiagnosisMessageType()
	if mt.Domain == "" {
		t.Error("MessageType.Domain is empty")
	}
	if mt.Category == "" {
		t.Error("MessageType.Category is empty")
	}
	if mt.Version == "" {
		t.Error("MessageType.Version is empty")
	}
	if !mt.IsValid() {
		t.Errorf("MessageType %v is not valid (IsValid() returned false)", mt)
	}
}

func TestOpsDiagnosisMessageType_KeyFormat(t *testing.T) {
	key := agentic.OpsDiagnosisMessageType().Key()
	want := "agentic.ops_diagnosis.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
