package agenticloop

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
)

// TestBuildInitialMessages_NoRegistry confirms the pre-ADR-029-step-3b
// behaviour survives when the registry is unset: no assembled system
// message, just the optional Context.Content and the user prompt. This
// is the path every agent takes today and must not regress.
func TestBuildInitialMessages_NoRegistry(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())

	messages := h.buildInitialMessages(TaskMessage{
		Role:   "general",
		Prompt: "hello",
	})

	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, "hello", messages[0].Content)
}

// TestBuildInitialMessages_AssembledSystemPrompt covers the headline
// wiring: with DefaultFragments loaded, a task with Role="general"
// produces a leading system message that contains the role fragment's
// static text and the shared system-identity preamble. Order is
// assembled-system → user prompt.
func TestBuildInitialMessages_AssembledSystemPrompt(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	reg := prompt.NewRegistry()
	reg.AddAll(prompt.DefaultFragments())
	h.SetPromptRegistry(reg)

	messages := h.buildInitialMessages(TaskMessage{
		Role:   "general",
		Prompt: "do the thing",
	})

	require.GreaterOrEqual(t, len(messages), 2)
	assert.Equal(t, "system", messages[0].Role)
	assert.Contains(t, messages[0].Content, "AI agent", "system-identity fragment must appear")
	assert.Contains(t, messages[0].Content, "general-purpose agent", "role-general fragment must appear for role=general")

	userMsg := messages[len(messages)-1]
	assert.Equal(t, "user", userMsg.Role)
	assert.Equal(t, "do the thing", userMsg.Content)
}

// TestBuildInitialMessages_SystemBeforeContext locks the order: assembled
// system prompt must come before task.Context.Content when both are
// present. A caller relying on the existing Context.Content system
// message to appear first would otherwise see silent reordering.
func TestBuildInitialMessages_SystemBeforeContext(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	reg := prompt.NewRegistry()
	reg.Add(prompt.Fragment{
		ID:       "fixture-system",
		Category: prompt.CategorySystem,
		Content:  "ASSEMBLED-SYSTEM",
	})
	h.SetPromptRegistry(reg)

	messages := h.buildInitialMessages(TaskMessage{
		Prompt: "user text",
		Context: &agentic.ConstructedContext{
			Content: "embedded-context-body",
		},
	})

	require.Len(t, messages, 3)
	assert.Equal(t, "system", messages[0].Role)
	assert.Contains(t, messages[0].Content, "ASSEMBLED-SYSTEM")
	assert.Equal(t, "system", messages[1].Role)
	assert.Contains(t, messages[1].Content, "[Context]")
	assert.Contains(t, messages[1].Content, "embedded-context-body")
	assert.Equal(t, "user", messages[2].Role)
	assert.Equal(t, "user text", messages[2].Content)
}

// TestBuildInitialMessages_PersonaOverride is the end-to-end plumbing
// test for ADR-029 step 3b: a persona-style Fragment upserted onto the
// registry with the same ID as a DefaultFragment replaces the default's
// content for the matching role. Regression guard — if the wiring ever
// stops respecting Upsert semantics, this fails loudly.
func TestBuildInitialMessages_PersonaOverride(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	reg := prompt.NewRegistry()
	reg.AddAll(prompt.DefaultFragments())
	reg.Upsert(prompt.Fragment{
		ID:       "role-researcher",
		Category: prompt.CategoryRole,
		Content:  "CUSTOM researcher persona from KV",
		Roles:    []string{"researcher"},
	})
	h.SetPromptRegistry(reg)

	messages := h.buildInitialMessages(TaskMessage{
		Role:   "researcher",
		Prompt: "investigate",
	})

	require.GreaterOrEqual(t, len(messages), 1)
	assert.Equal(t, "system", messages[0].Role)
	assert.Contains(t, messages[0].Content, "CUSTOM researcher persona from KV")
	assert.False(t,
		strings.Contains(messages[0].Content, "Research methodology:"),
		"overridden fragment must not appear alongside the KV version")
}

// TestAssembleSystemPrompt_RespectsContextFields confirms TaskMessage
// fields propagate into the AssemblyContext so fragments gated on
// ParentLoopID, WorkflowSlug, etc. see the task's values. The
// constraint-child-agent fragment in DefaultFragments gates on
// ParentLoopID non-empty — easy smoke test without stubbing our own
// fragment.
func TestAssembleSystemPrompt_RespectsContextFields(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	reg := prompt.NewRegistry()
	reg.AddAll(prompt.DefaultFragments())
	h.SetPromptRegistry(reg)

	// Without a parent, the child-agent constraint is suppressed.
	out := h.assembleSystemPrompt(TaskMessage{Role: "general", Prompt: "x"})
	assert.NotContains(t, out, "child agent")

	// With parent, it appears and names the parent loop.
	out = h.assembleSystemPrompt(TaskMessage{
		Role:         "general",
		Prompt:       "x",
		ParentLoopID: "loop_parent_999",
		Depth:        1,
		MaxDepth:     3,
	})
	assert.Contains(t, out, "child agent")
	assert.Contains(t, out, "loop_parent_999")
}

// TestAssembleSystemPrompt_NilRegistryReturnsEmpty keeps the nil path
// explicit. Handlers without SetPromptRegistry called must not crash
// and must not emit a stray system message.
func TestAssembleSystemPrompt_NilRegistryReturnsEmpty(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	got := h.assembleSystemPrompt(TaskMessage{Role: "general", Prompt: "x"})
	assert.Equal(t, "", got)
}

// TestToolNames covers the simple ToolDefinition → []string projection
// since it's reachable from assembleSystemPrompt even though the
// default registry doesn't read ctx.Tools today. Locks in nil-vs-empty
// behaviour so a future fragment that reads ctx.Tools isn't surprised.
func TestToolNames(t *testing.T) {
	assert.Nil(t, toolNames(nil))
	assert.Nil(t, toolNames([]agentic.ToolDefinition{}))
	assert.Equal(t, []string{"a", "b"}, toolNames([]agentic.ToolDefinition{
		{Name: "a"},
		{Name: "b"},
	}))
}
