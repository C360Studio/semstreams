package file

import (
	"encoding/json"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileOutput_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
			},
		},
		Directory:  "/tmp/test",
		FilePrefix: "output",
		Format:     "jsonl",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, output)

	meta := output.Meta()
	assert.Equal(t, "file-output", meta.Name)
	assert.Equal(t, "output", meta.Type)
}

func TestFileOutput_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Ports)
	assert.Len(t, config.Ports.Inputs, 1)
	assert.Equal(t, "output.>", config.Ports.Inputs[0].Subject)
	assert.Equal(t, "/tmp/streamkit", config.Directory)
	assert.Equal(t, "jsonl", config.Format)
}

func TestFileOutput_Lifecycle(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
			},
		},
		Directory:  "/tmp/test-output",
		FilePrefix: "test",
		Format:     "jsonl",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	lifecycleComp, ok := output.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize should create directory
	err = lifecycleComp.Initialize()
	assert.NoError(t, err)

	// Health check (without starting)
	health := output.Health()
	assert.False(t, health.Healthy) // Not started yet
}
