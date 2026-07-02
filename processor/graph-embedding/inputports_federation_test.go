package graphembedding

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// componentFromConfig builds a graph-embedding Component from a Config (no NATS
// connection needed — InputPorts is pure).
func componentFromConfig(t *testing.T, config Config) *Component {
	t.Helper()
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	return comp.(*Component)
}

func countStoreReadPorts(ports []component.Port) (count int, names []string) {
	for _, p := range ports {
		if _, ok := p.Config.(component.StoreReadPort); ok {
			count++
			names = append(names, p.Name)
		}
	}
	return count, names
}

// ADR-063 Phase 2: graph-embedding always consumes the store federation, so
// InputPorts must declare exactly one store-read (federation) port — synthesized
// when the config wires none, and NOT doubled when the config wires one.

func TestInputPorts_SynthesizesFederationPortWhenConfigHasNone(t *testing.T) {
	// DefaultConfig wires a store-read content_store; drop it to simulate a config
	// that wires none (e.g. the semantic tier).
	config := DefaultConfig()
	kept := config.Ports.Inputs[:0:0]
	for _, p := range config.Ports.Inputs {
		if p.Type != "store-read" {
			kept = append(kept, p)
		}
	}
	config.Ports.Inputs = kept

	comp := componentFromConfig(t, config)
	require.NoError(t, comp.Initialize())

	count, names := countStoreReadPorts(comp.InputPorts())
	require.Equal(t, 1, count, "exactly one federation store-read port must be synthesized, got %v", names)
	require.Equal(t, "store-federation", names[0])
}

func TestInputPorts_DoesNotDoubleWhenConfigHasStoreRead(t *testing.T) {
	// DefaultConfig already wires a store-read content_store — no synthesis.
	comp := componentFromConfig(t, DefaultConfig())
	require.NoError(t, comp.Initialize())

	count, names := countStoreReadPorts(comp.InputPorts())
	require.Equal(t, 1, count, "config store-read must not be duplicated by a synthesized one, got %v", names)
	require.NotContains(t, names, "store-federation", "must reuse the config store-read, not synthesize")
}
