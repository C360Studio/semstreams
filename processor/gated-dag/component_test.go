package gateddagexec

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/require"
)

func TestNewComponent_HappyPath(t *testing.T) {
	raw := json.RawMessage(`{"unit_entity_prefix":"acme.ops.plan.fanout.unit","dispatch_subject":"gateddag.dispatch.unit"}`)
	c, err := NewComponent(raw, component.Dependencies{})
	require.NoError(t, err)
	require.Equal(t, FanOutWorkflow, c.cfg.FanOutWorkflow)
	require.Equal(t, defaultClaimPredicate, c.cfg.ClaimPredicate)
}

func TestNewComponent_BadJSON(t *testing.T) {
	_, err := NewComponent(json.RawMessage(`{not json`), component.Dependencies{})
	require.Error(t, err)
}

func TestNewComponent_MissingRequiredFails(t *testing.T) {
	// No unit_entity_prefix / dispatch_subject → Validate rejects.
	_, err := NewComponent(json.RawMessage(`{}`), component.Dependencies{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestComponent_Discoverable(t *testing.T) {
	raw := json.RawMessage(`{"unit_entity_prefix":"acme.ops.plan.fanout.unit","dispatch_subject":"gateddag.dispatch.unit"}`)
	c, err := NewComponent(raw, component.Dependencies{})
	require.NoError(t, err)

	require.Equal(t, componentName, c.Meta().Name)
	require.Equal(t, "processor", c.Meta().Type)
	require.Empty(t, c.InputPorts())

	out := c.OutputPorts()
	require.Len(t, out, 1)
	require.Equal(t, component.DirectionOutput, out[0].Direction)
	require.Equal(t, "gateddag.dispatch.unit", out[0].Config.(component.NATSPort).Subject)

	require.Contains(t, c.ConfigSchema().Required, "unit_entity_prefix")
	require.Contains(t, c.ConfigSchema().Required, "dispatch_subject")

	// Health before start.
	h := c.Health()
	require.False(t, h.Healthy)
	require.Equal(t, "stopped", h.Status)
}

func TestComponent_InitializeRequiresManager(t *testing.T) {
	raw := json.RawMessage(`{"unit_entity_prefix":"acme.ops.plan.fanout.unit","dispatch_subject":"gateddag.dispatch.unit"}`)
	c, err := NewComponent(raw, component.Dependencies{}) // nil LifecycleManager
	require.NoError(t, err)
	err = c.Initialize()
	require.Error(t, err)
	require.Contains(t, err.Error(), "LifecycleManager is required")
}

func TestComponent_StopBeforeStartIsNoop(t *testing.T) {
	raw := json.RawMessage(`{"unit_entity_prefix":"acme.ops.plan.fanout.unit","dispatch_subject":"gateddag.dispatch.unit"}`)
	c, err := NewComponent(raw, component.Dependencies{})
	require.NoError(t, err)
	require.NoError(t, c.Stop(time.Second))
}

func TestRegister(t *testing.T) {
	reg := component.NewRegistry()
	require.NoError(t, Register(reg))
}
