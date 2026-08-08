package graphgateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShippedGraphGatewayOverridesCarryAgenticQueryInterface(t *testing.T) {
	paths := []string{
		"configs/e2e-structural.json",
		"configs/hello-world.json",
		"configs/protocol-flow.json",
		"configs/semantic-8b.json",
		"configs/semantic-frontier.json",
		"configs/semantic.json",
		"configs/statistical.json",
		"configs/structural.json",
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join("..", "..", path))
			require.NoError(t, err)
			var assembly struct {
				Components map[string]struct {
					Config json.RawMessage `json:"config"`
				} `json:"components"`
			}
			require.NoError(t, json.Unmarshal(encoded, &assembly))
			gatewayConfig, ok := assembly.Components["graph-gateway"]
			require.True(t, ok, "graph-gateway missing")

			client, clientErr := natsclient.NewClient("nats://localhost:4222")
			require.NoError(t, clientErr)
			discoverable, createErr := CreateGraphGateway(gatewayConfig.Config, component.Dependencies{NATSClient: client})
			require.NoError(t, createErr)
			outputs := discoverable.OutputPorts()
			require.Len(t, outputs, 3)
			facts, factsErr := outputs[2].Facts()
			require.NoError(t, factsErr)
			contract, hasContract := facts.Interface()
			require.True(t, hasContract)
			assert.Equal(t, "agentic.query", contract.Type)
			assert.Equal(t, "v1", contract.Version)
		})
	}
}

func canonicalQueryOutputs(graph, index, agentic string) []component.PortDefinition {
	return []component.PortDefinition{
		{Name: "graph_queries", Required: true, Config: component.NATSRequestPort{Subject: graph}},
		{Name: "graph_index_queries", Required: true, Config: component.NATSRequestPort{Subject: index}},
		{Name: "agentic_queries", Required: true, Config: component.NATSRequestPort{Subject: agentic, Interface: agenticQueryInterface()}},
	}
}

func createGatewayFromPortConfig(t *testing.T, config Config) *Component {
	t.Helper()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	client, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	discoverable, err := CreateGraphGateway(rawConfig, component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	return discoverable.(*Component)
}

func TestGraphGatewayCanonicalPortContract(t *testing.T) {
	gateway := createGatewayFromPortConfig(t, DefaultConfig())

	assert.Empty(t, gateway.InputPorts(), "shared-mux gateway owns no composition input")
	outputs := gateway.OutputPorts()
	require.Len(t, outputs, 3)

	want := canonicalQueryOutputs("graph.query.*", "graph.index.query.*", "agentic.query.*")
	for index, port := range outputs {
		assert.Equal(t, want[index].Name, port.Name)
		assert.True(t, port.Required)
		facts, err := port.Facts()
		require.NoError(t, err)
		assert.Equal(t, component.PortKindNATSRequest, facts.Kind())
		assert.Equal(t, []string{want[index].Config.(component.NATSRequestPort).Subject}, facts.NATSSubjects())
	}
}

func TestGraphGatewayCustomRequestFamiliesDriveRouting(t *testing.T) {
	config := DefaultConfig()
	config.Ports.Outputs = canonicalQueryOutputs("tenant.graph.*", "tenant.index.*", "tenant.agentic.query.*")
	gateway := createGatewayFromPortConfig(t, config)

	tests := []struct {
		query string
		want  string
		field string
	}{
		{query: `{ entity(id: "acme.ops.demo.one.type.001") { id } }`, want: "tenant.graph.entity", field: "entity"},
		{query: `{ predicateStats(predicate: "demo.kind.value") { count } }`, want: "tenant.index.predicateStats", field: "predicateStats"},
		{query: `{ trajectory(loopId: "loop-1") { id } }`, want: "tenant.agentic.query.trajectory", field: "trajectory"},
	}
	for _, test := range tests {
		subject := gateway.mapGraphQLQueryToNATSSubject(test.query)
		assert.Equal(t, test.want, subject)
		assert.Equal(t, test.field, gateway.subjectToGraphQLField(subject))
	}
}

func TestGraphGatewayRejectsInvalidPortContracts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "legacy input", mutate: func(config *Config) {
			config.Ports.Inputs = []component.PortDefinition{{
				Name: "http", Config: component.NetworkPort{Protocol: "http", Host: "localhost", Port: 8080},
			}}
		}},
		{name: "missing output", mutate: func(config *Config) {
			config.Ports.Outputs = config.Ports.Outputs[:2]
		}},
		{name: "duplicate output", mutate: func(config *Config) {
			config.Ports.Outputs[2] = config.Ports.Outputs[1]
		}},
		{name: "duplicate output family", mutate: func(config *Config) {
			config.Ports.Outputs[2].Config = component.NATSRequestPort{Subject: "graph.index.query.*"}
		}},
		{name: "extra output", mutate: func(config *Config) {
			config.Ports.Outputs = append(config.Ports.Outputs, component.PortDefinition{
				Name: "extra", Required: true, Config: component.NATSRequestPort{Subject: "extra.query.*"},
			})
		}},
		{name: "wrong name", mutate: func(config *Config) {
			config.Ports.Outputs[0].Name = "queries"
		}},
		{name: "wrong kind", mutate: func(config *Config) {
			config.Ports.Outputs[0].Config = component.NATSPort{Subject: "graph.query.*"}
		}},
		{name: "not required", mutate: func(config *Config) {
			config.Ports.Outputs[0].Required = false
		}},
		{name: "agentic interface missing", mutate: func(config *Config) {
			config.Ports.Outputs[2].Config = component.NATSRequestPort{Subject: "agentic.query.*"}
		}},
		{name: "agentic interface wrong version", mutate: func(config *Config) {
			config.Ports.Outputs[2].Config = component.NATSRequestPort{
				Subject: "agentic.query.*", Interface: &component.InterfaceContract{Type: "agentic.query", Version: "v2"},
			}
		}},
		{name: "not a family pattern", mutate: func(config *Config) {
			config.Ports.Outputs[0].Config = component.NATSRequestPort{Subject: "graph.query.entity"}
		}},
		{name: "malformed family pattern", mutate: func(config *Config) {
			config.Ports.Outputs[0].Config = component.NATSRequestPort{Subject: "graph.>.query.*"}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			test.mutate(&config)
			rawConfig, err := json.Marshal(config)
			require.NoError(t, err)
			client, err := natsclient.NewClient("nats://localhost:4222")
			require.NoError(t, err)
			_, err = CreateGraphGateway(rawConfig, component.Dependencies{NATSClient: client})
			require.Error(t, err)
		})
	}
}

func TestGraphGatewayRejectsMissingPortConfiguration(t *testing.T) {
	client, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	_, err = CreateGraphGateway(json.RawMessage(`{"graphql_path":"/graphql","mcp_path":"/mcp"}`), component.Dependencies{NATSClient: client})
	require.Error(t, err)
}
