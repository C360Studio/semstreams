package file

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileOutput_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "test.input"}, Required: true},
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
	input, ok := config.Ports.Inputs[0].Config.(component.NATSPort)
	require.True(t, ok)
	assert.Equal(t, "output.>", input.Subject)
	assert.Equal(t, "/tmp/streamkit", config.Directory)
	assert.Equal(t, "jsonl", config.Format)
}

func TestFileOutput_DerivesCanonicalFileOutput(t *testing.T) {
	tests := []struct {
		name       string
		directory  string
		filePrefix string
		format     string
	}{
		{name: "default", directory: "/tmp/streamkit", filePrefix: "output", format: "jsonl"},
		{name: "custom", directory: "/var/tmp/export", filePrefix: "events", format: "raw"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Ports: &component.PortConfig{Inputs: []component.PortDefinition{{
					Name: "input", Config: component.NATSPort{Subject: "test.input"}, Required: true,
				}}},
				Directory: tt.directory, FilePrefix: tt.filePrefix, Format: tt.format,
			}
			rawConfig, err := json.Marshal(config)
			require.NoError(t, err)

			discoverable, err := NewOutput(rawConfig, component.Dependencies{})
			require.NoError(t, err)
			output := discoverable.(*Output)
			wantPath := filepath.Join(tt.directory, tt.filePrefix+"."+tt.format)
			assert.Equal(t, wantPath, output.filePath)

			ports := output.OutputPorts()
			require.Len(t, ports, 1)
			assert.Equal(t, "file_output", ports[0].Name)
			facts, err := ports[0].Facts()
			require.NoError(t, err)
			assert.Equal(t, component.PortKindFile, facts.Kind())
			assert.Equal(t, "file:"+wantPath, facts.ResourceID())
		})
	}
}

func TestFileOutput_RejectsConfiguredOutputPorts(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{{
				Name: "input", Config: component.NATSPort{Subject: "test.input"}, Required: true,
			}},
			Outputs: []component.PortDefinition{{
				Name: "invented", Config: component.FilePort{Path: "/tmp/invented.jsonl"},
			}},
		},
		Directory: "/tmp/export", FilePrefix: "events", Format: "jsonl",
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	_, err = NewOutput(rawConfig, component.Dependencies{})
	require.ErrorContains(t, err, "remove ports.outputs")
}

func TestFileOutput_Lifecycle(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "test.input"}, Required: true},
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
