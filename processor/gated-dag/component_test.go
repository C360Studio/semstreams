package gateddagexec

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	semtypes "github.com/c360studio/semstreams/pkg/types"
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
	require.Len(t, out, 2)
	require.Equal(t, component.DirectionOutput, out[0].Direction)
	dispatchFacts, err := out[0].Facts()
	require.NoError(t, err)
	require.Equal(t, component.PortKindJetStream, dispatchFacts.Kind())
	dispatch, ok := dispatchFacts.Stream()
	require.True(t, ok)
	require.Equal(t, defaultDispatchStream, dispatch.Name())
	require.Equal(t, []string{"gateddag.dispatch.unit"}, dispatch.Subjects())
	require.Equal(t, "file", dispatch.Storage())
	require.Equal(t, "work_queue", dispatch.RetentionPolicy())
	mutationFacts, err := out[1].Facts()
	require.NoError(t, err)
	require.Equal(t, component.PortKindNATSRequest, mutationFacts.Kind())
	require.True(t, out[1].Required)
	require.Equal(t, []string{graphmutation.SubjectFamily}, mutationFacts.NATSSubjects())
	mutation, ok := mutationFacts.Interface()
	require.True(t, ok)
	require.Equal(t, graphmutation.InterfaceType, mutation.Type)
	require.Equal(t, graphmutation.InterfaceVersion, mutation.Version)

	require.Contains(t, c.ConfigSchema().Required, "unit_entity_prefix")
	require.Contains(t, c.ConfigSchema().Required, "dispatch_subject")
	prefixSchema := c.ConfigSchema().Properties["unit_entity_prefix"]
	require.NotNil(t, prefixSchema.MinLength)
	require.Equal(t, 1, *prefixSchema.MinLength)
	require.NotNil(t, prefixSchema.MaxLength)
	require.Equal(t, 256, *prefixSchema.MaxLength)
	require.Equal(t, semtypes.EntityIDLiteralPrefixPattern, prefixSchema.Pattern)
	prefixPattern := regexp.MustCompile(prefixSchema.Pattern)
	require.True(t, prefixPattern.MatchString("acme.ops.plan.fanout.unit"))
	require.False(t, prefixPattern.MatchString(`acme\.ops`))
	require.False(t, prefixPattern.MatchString("acme.*"))
	instanceSchema := c.ConfigSchema().Properties["fan_out_instance_id"]
	require.Nil(t, instanceSchema.MinLength, "empty is the explicit no-lifecycle sentinel")
	require.Equal(t, semtypes.OptionalEntityIDLiteralPattern, instanceSchema.Pattern)
	instancePattern := regexp.MustCompile(instanceSchema.Pattern)
	require.True(t, instancePattern.MatchString(""))
	require.True(t, instancePattern.MatchString("acme.ops.plan.fanout.instance.1"))
	require.False(t, instancePattern.MatchString(`acme\.ops.plan.fanout.instance.1`))

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
