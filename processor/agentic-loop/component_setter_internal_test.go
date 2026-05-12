package agenticloop

// Internal tests for Component setter pass-throughs that need to read
// private fields on MessageHandler. Public-API tests for the underlying
// SetToolCallFilter behaviour live in handlers_test.go.

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// internalMockFilter is the smallest possible agentic.ToolCallFilter for
// pass-through verification — we never invoke it, we just confirm the
// Component's SetToolCallFilter wired it into the handler.
type internalMockFilter struct{}

func (f *internalMockFilter) FilterToolCalls(_ string, calls []agentic.ToolCall) (agentic.ToolCallFilterResult, error) {
	return agentic.ToolCallFilterResult{Approved: calls}, nil
}

// TestComponent_SetToolCallFilter_PassThrough verifies that
// Component.SetToolCallFilter installs the filter on the underlying
// MessageHandler (and that passing nil clears it). The Component
// method is a thin shim over MessageHandler.SetToolCallFilter — the
// filter's behavioral semantics are tested in handlers_test.go.
func TestComponent_SetToolCallFilter_PassThrough(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := NewComponent(rawConfig, component.Dependencies{})
	require.NoError(t, err)
	c := comp.(*Component)

	require.NotNil(t, c.handler, "handler should be constructed by NewComponent")
	assert.Nil(t, c.handler.toolCallFilter, "handler starts with nil tool-call filter")

	filter := &internalMockFilter{}
	c.SetToolCallFilter(filter)
	assert.Same(t, filter, c.handler.toolCallFilter,
		"SetToolCallFilter must pass filter through to the handler")

	c.SetToolCallFilter(nil)
	assert.Nil(t, c.handler.toolCallFilter,
		"SetToolCallFilter(nil) must clear the handler's filter")
}

// TestComponent_SetToolCallFilter_NilReceiver_Noop guards the nil-safety
// branch on the setter. Callers passing through a possibly-nil Component
// (e.g. configuration paths that may skip the agentic-loop component)
// shouldn't panic.
func TestComponent_SetToolCallFilter_NilReceiver_Noop(t *testing.T) {
	var c *Component
	assert.NotPanics(t, func() { c.SetToolCallFilter(&internalMockFilter{}) })
	assert.NotPanics(t, func() { c.SetToolCallFilter(nil) })
}
