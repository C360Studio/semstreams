package websocket

import (
	"context"
	"fmt"
	"net"
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
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())
	config := DefaultConstructorConfig()
	config.InputPorts = []component.PortDefinition{{
		Name: "events",
		Config: component.JetStreamPort{
			StreamName: "EVENTS", Subjects: []string{"events.>"}, DeliverPolicy: "new",
		},
	}}
	config.OutputPorts = []component.PortDefinition{{
		Name: "socket", Config: component.NetworkPort{Protocol: "http", Host: "127.0.0.1", Port: port},
	}}
	output, err := NewOutputFromConfig(config)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", output.host)
	require.Equal(t, port, output.port)
	require.Equal(t, []string{"events.>"}, output.subjects)
	require.NoError(t, output.Initialize())
	require.NoError(t, output.setupHTTPServer(context.Background()))
	t.Cleanup(func() { require.NoError(t, output.listener.Close()) })
	require.Equal(t, net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), output.server.Addr)
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
