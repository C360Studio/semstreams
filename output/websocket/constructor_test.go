package websocket

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

func mustNewOutput(t *testing.T, port int, path string, subjects []string, client *natsclient.Client) *Output {
	t.Helper()
	output, err := NewOutput(port, path, subjects, client)
	require.NoError(t, err)
	return output
}

func TestNewOutputFromConfigRejectsMissingOrFalseDeclarations(t *testing.T) {
	base := DefaultConstructorConfig()
	tests := []struct {
		name   string
		mutate func(*ConstructorConfig)
	}{
		{name: "missing inputs", mutate: func(config *ConstructorConfig) { config.InputPorts = nil }},
		{name: "missing output", mutate: func(config *ConstructorConfig) { config.OutputPorts = nil }},
		{name: "extra output", mutate: func(config *ConstructorConfig) {
			config.OutputPorts = append(config.OutputPorts, config.OutputPorts[0])
		}},
		{name: "non-network output", mutate: func(config *ConstructorConfig) {
			config.OutputPorts = []component.PortDefinition{{Name: "wrong", Config: component.NATSPort{Subject: "out"}}}
		}},
		{name: "udp output", mutate: func(config *ConstructorConfig) {
			config.OutputPorts = []component.PortDefinition{{Name: "wrong", Config: component.NetworkPort{Protocol: "udp", Port: 9000}}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			config.InputPorts = append([]component.PortDefinition(nil), base.InputPorts...)
			config.OutputPorts = append([]component.PortDefinition(nil), base.OutputPorts...)
			test.mutate(&config)
			if _, err := NewOutputFromConfig(config); err == nil {
				t.Fatal("NewOutputFromConfig succeeded")
			}
		})
	}
}

func TestNewOutputFromConfigUsesCanonicalNetworkAndStreamFacts(t *testing.T) {
	config := DefaultConstructorConfig()
	config.InputPorts = []component.PortDefinition{{
		Name: "events",
		Config: component.JetStreamPort{
			StreamName: "EVENTS", Subjects: []string{"events.>"}, DeliverPolicy: "new",
		},
	}}
	config.OutputPorts = []component.PortDefinition{{
		Name: "socket", Config: component.NetworkPort{Protocol: "http", Host: "127.0.0.1", Port: 9191},
	}}
	output, err := NewOutputFromConfig(config)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", output.host)
	require.Equal(t, 9191, output.port)
	require.Equal(t, []string{"events.>"}, output.subjects)
	require.NoError(t, output.Initialize())
	require.NoError(t, output.setupHTTPServer())
	require.Equal(t, "127.0.0.1:9191", output.server.Addr)
	facts, err := output.InputPorts()[0].Facts()
	require.NoError(t, err)
	require.Equal(t, component.PortKindJetStream, facts.Kind())
	stream, ok := facts.Stream()
	require.True(t, ok)
	require.Equal(t, "EVENTS", stream.Name())
}

func mustNewOutputFromConfig(t *testing.T, config ConstructorConfig) *Output {
	t.Helper()
	output, err := NewOutputFromConfig(config)
	require.NoError(t, err)
	return output
}
