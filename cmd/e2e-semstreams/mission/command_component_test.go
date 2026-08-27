package mission

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validComponentConfig() ComponentConfig {
	return ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{{
				Name: "commands", Description: "mission commands", Required: true,
				Config: component.NATSPort{Subject: "mission.command.input"},
			}},
			Outputs: []component.PortDefinition{{
				Name: "graphables", Description: "graphable mission commands", Required: true,
				Config: component.NATSPort{Subject: "graph.ingest.mission"},
			}},
		},
	}
}

func newMissionComponent(t *testing.T, config ComponentConfig) *Component {
	t.Helper()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	discoverable, err := NewComponent(rawConfig, component.Dependencies{Platform: types.PlatformMeta{Org: "acme", Platform: "ops"}})
	require.NoError(t, err)
	return discoverable.(*Component)
}

func TestNewComponent_PreservesResolvedPortDeclarations(t *testing.T) {
	mission := newMissionComponent(t, validComponentConfig())

	inputs := mission.InputPorts()
	require.Len(t, inputs, 1)
	assert.Equal(t, "commands", inputs[0].Name)
	assert.Equal(t, "mission commands", inputs[0].Description)
	assert.True(t, inputs[0].Required)
	inputFacts, err := inputs[0].Facts()
	require.NoError(t, err)
	assert.Equal(t, component.PortKindNATS, inputFacts.Kind())
	assert.Equal(t, []string{"mission.command.input"}, inputFacts.NATSSubjects())

	outputs := mission.OutputPorts()
	require.Len(t, outputs, 1)
	assert.Equal(t, "graphables", outputs[0].Name)
	assert.Equal(t, "graphable mission commands", outputs[0].Description)
	assert.True(t, outputs[0].Required)
	outputFacts, err := outputs[0].Facts()
	require.NoError(t, err)
	assert.Equal(t, component.PortKindNATS, outputFacts.Kind())
	assert.Equal(t, []string{"graph.ingest.mission"}, outputFacts.NATSSubjects())
}

func TestNewComponent_RejectsNonCanonicalPortShape(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ComponentConfig)
	}{
		{name: "extra input", mutate: func(config *ComponentConfig) {
			config.Ports.Inputs = append(config.Ports.Inputs, component.PortDefinition{
				Name: "second", Config: component.NATSPort{Subject: "mission.command.second"},
			})
		}},
		{name: "extra output", mutate: func(config *ComponentConfig) {
			config.Ports.Outputs = append(config.Ports.Outputs, component.PortDefinition{
				Name: "second", Config: component.NATSPort{Subject: "graph.ingest.second"},
			})
		}},
		{name: "wrong input kind", mutate: func(config *ComponentConfig) {
			config.Ports.Inputs[0].Config = component.TimerPort{Interval: "1s"}
		}},
		{name: "wrong output kind", mutate: func(config *ComponentConfig) {
			config.Ports.Outputs[0].Config = component.FilePort{Path: "/tmp/mission.json"}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validComponentConfig()
			tt.mutate(&config)
			rawConfig, err := json.Marshal(config)
			require.NoError(t, err)
			_, err = NewComponent(rawConfig, component.Dependencies{})
			require.Error(t, err)
		})
	}
}

func TestComponentPorts_ReturnDefensiveSlices(t *testing.T) {
	mission := newMissionComponent(t, validComponentConfig())

	inputs := mission.InputPorts()
	outputs := mission.OutputPorts()
	inputs[0].Name = "mutated-input"
	outputs[0].Name = "mutated-output"

	assert.Equal(t, "commands", mission.InputPorts()[0].Name)
	assert.Equal(t, "graphables", mission.OutputPorts()[0].Name)
}
