package agenticdispatch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRegistry returns a minimal model.Registry suitable for unit tests.
func newTestRegistry() model.RegistryReader {
	return &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"test-model": {Model: "test-model", MaxTokens: 128000},
		},
		Defaults: model.DefaultsConfig{Model: "test-model"},
	}
}

// TestComponent_Start_NilContext verifies that Start rejects nil context
func TestComponent_Start_NilContext(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      nil, // Not needed for this test
		ModelRegistry:   newTestRegistry(),
		PayloadRegistry: payloadregistry.NewWithSubset(t, agentic.RegisterPayloads),
	}

	comp, err := NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lifecycleComp, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	// Start with nil context should fail
	err = lifecycleComp.Start(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cannot be nil")
}

// TestComponent_Start_CancelledContext verifies that Start rejects cancelled context
func TestComponent_Start_CancelledContext(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      nil, // Not needed for this test
		ModelRegistry:   newTestRegistry(),
		PayloadRegistry: payloadregistry.NewWithSubset(t, agentic.RegisterPayloads),
	}

	comp, err := NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lifecycleComp, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Start with cancelled context should fail
	err = lifecycleComp.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context already cancelled")
}
