package agentic_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
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
				want:     "acme.ops.agent.lesson.record.2c5acb9b-8283-5b34-a4d1-4b1c9f8502ca",
			},
			{
				name:     "short alphanumeric id",
				org:      "c360",
				platform: "prod",
				id:       "abc123",
				want:     "c360.prod.agent.lesson.record.abc123",
			},
			{
				name:     "id with underscores",
				org:      "myorg",
				platform: "staging",
				id:       "lesson_42",
				want:     "myorg.staging.agent.lesson.record.lesson_42",
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

	t.Run("shape constraints — exactly 6 parts, agent.lesson.record axes", func(t *testing.T) {
		got := agentic.AgentLessonEntityID("acme", "ops", "abc123")
		parts := strings.Split(got, ".")
		require.Len(t, parts, 6, "entity ID must have exactly 6 parts")
		assert.Equal(t, "acme", parts[0], "part[0] = org")
		assert.Equal(t, "ops", parts[1], "part[1] = platform")
		assert.Equal(t, "agent", parts[2], "part[2] = domain")
		assert.Equal(t, "lesson", parts[3], "part[3] = system")
		assert.Equal(t, "record", parts[4], "part[4] = type")
		assert.Equal(t, "abc123", parts[5], "part[5] = instance (the content-derived id)")
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

func TestAgentLessonMessageType_Distinct(t *testing.T) {
	// Must not collide with any other agentic category constant — a collision
	// would let two entity kinds share an origin envelope and silently break
	// typed-origin ownership arbitration.
	mt := agentic.AgentLessonMessageType()
	others := []string{
		agentic.CategoryOpsDiagnosis,
		agentic.CategoryLoopExecution,
		agentic.CategoryModelEndpoint,
		agentic.CategoryTrajectoryStep,
		agentic.CategoryLoopCreated,
		agentic.CategoryLoopCompleted,
		agentic.CategoryLoopFailed,
		agentic.CategoryLoopCancelled,
		agentic.CategoryTask,
	}
	for _, cat := range others {
		if mt.Category == cat {
			t.Errorf("CategoryAgentLesson collides with existing category %q", cat)
		}
	}
}

func TestAgentLessonMessageType_KeyFormat(t *testing.T) {
	key := agentic.AgentLessonMessageType().Key()
	want := "agentic.agent_lesson.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
