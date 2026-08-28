package agentic_test

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentLessonEntityID(t *testing.T) {
	t.Run("valid constructions", func(t *testing.T) {
		tests := []struct {
			name     string
			org      string
			platform string
			id       string
			want     string
		}{
			{
				name:     "uuid5-style id with hyphens",
				org:      "acme",
				platform: "ops",
				id:       "2c5acb9b-8283-5b34-a4d1-4b1c9f8502ca",
				want:     "acme.ops.lesson.agent.record.2c5acb9b-8283-5b34-a4d1-4b1c9f8502ca",
			},
			{
				name:     "short alphanumeric id",
				org:      "c360",
				platform: "prod",
				id:       "abc123",
				want:     "c360.prod.lesson.agent.record.abc123",
			},
			{
				name:     "id with underscores",
				org:      "myorg",
				platform: "staging",
				id:       "lesson_42",
				want:     "myorg.staging.lesson.agent.record.lesson_42",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := agentic.AgentLessonEntityID(tt.org, tt.platform, tt.id)
				require.Equal(t, tt.want, got)
				assert.True(t, message.IsValidEntityID(got), "result %q must pass IsValidEntityID", got)
			})
		}
	})

	t.Run("shape constraints — exactly 6 parts, lesson.agent.record axes", func(t *testing.T) {
		got := agentic.AgentLessonEntityID("acme", "ops", "abc123")
		parsed, err := semtypes.ParseEntityID(got)
		require.NoError(t, err, "entity ID must be canonical")
		assert.Equal(t, "acme", parsed.Org, "position 1 = org")
		assert.Equal(t, "ops", parsed.Platform, "position 2 = platform (the minting authority)")
		assert.Equal(t, "lesson", parsed.System, "position 3 = system (the source that produced it)")
		assert.Equal(t, "agent", parsed.Domain, "position 4 = domain (framework-reserved)")
		assert.Equal(t, "record", parsed.Type, "position 5 = type")
		assert.Equal(t, "abc123", parsed.Instance, "position 6 = instance (the content-derived id)")
	})

	t.Run("panics on invalid input", func(t *testing.T) {
		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("", "ops", "abc123")
		}, "empty org should panic")

		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("acme", "", "abc123")
		}, "empty platform should panic")

		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("acme", "ops", "")
		}, "empty id should panic")

		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("acme.corp", "ops", "abc123")
		}, "dot in org should panic")

		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("acme", "ops.prod", "abc123")
		}, "dot in platform should panic")

		assert.Panics(t, func() {
			agentic.AgentLessonEntityID("acme", "ops", "record.1")
		}, "dot in id should panic")
	})
}

// --- AgentLessonMessageType (ADR-080 typed-origin envelope) ---

func TestAgentLessonMessageType_Valid(t *testing.T) {
	mt := agentic.AgentLessonMessageType()
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

func TestAgentLessonMessageType_KeyFormat(t *testing.T) {
	key := agentic.AgentLessonMessageType().Key()
	want := "agentic.agent_lesson.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
