package graphembedding

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/stretchr/testify/require"
)

// componentFromConfig builds a graph-embedding Component from a Config (no NATS
// connection needed — InputPorts is pure).
func componentFromConfig(t *testing.T, config Config, registry ...*storeregistry.Registry) *Component {
	t.Helper()
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	deps := component.Dependencies{NATSClient: natsClient}
	if len(registry) != 0 {
		deps.StoreRegistry = registry[0]
	}
	comp, err := CreateGraphEmbedding(configJSON, deps)
	require.NoError(t, err)
	return comp.(*Component)
}

func countStoreReadPorts(ports []component.Port) (count int, names []string) {
	for _, p := range ports {
		facts, err := p.Facts()
		if err == nil {
			_, ok := facts.StoreReadBucket()
			if !ok {
				continue
			}
			count++
			names = append(names, p.Name)
		}
	}
	return count, names
}

// ADR-063 Phase 2: graph-embedding consumes the store federation only when a
// store-read port explicitly names its backing store. A missing declaration
// cannot truthfully be synthesized because the framework does not own a bucket
// name to put in it.

func TestInputPorts_DoesNotInventFederationPortWhenConfigHasNone(t *testing.T) {
	// DefaultConfig wires a store-read content_store; drop it to simulate a config
	// that wires none (e.g. the semantic tier).
	config := DefaultConfig()
	kept := config.Ports.Inputs[:0:0]
	for _, p := range config.Ports.Inputs {
		resolved, err := p.Resolve(component.DirectionInput)
		require.NoError(t, err)
		facts, err := resolved.Facts()
		require.NoError(t, err)
		if _, isStoreRead := facts.StoreReadBucket(); !isStoreRead {
			kept = append(kept, p)
		}
	}
	config.Ports.Inputs = kept

	registry := storeregistry.New()
	comp := componentFromConfig(t, config, registry)
	require.NoError(t, comp.Initialize())
	require.Nil(t, comp.storeRegistry, "an injected registry is not admitted without a store-read declaration")

	count, names := countStoreReadPorts(comp.InputPorts())
	require.Equal(t, 0, count, "no undeclared federation store-read port may be invented, got %v", names)
}

func TestInputPorts_DoesNotDoubleWhenConfigHasStoreRead(t *testing.T) {
	// DefaultConfig already wires a store-read content_store — no synthesis.
	registry := storeregistry.New()
	comp := componentFromConfig(t, DefaultConfig(), registry)
	require.NoError(t, comp.Initialize())
	require.Same(t, registry, comp.storeRegistry)

	count, names := countStoreReadPorts(comp.InputPorts())
	require.Equal(t, 1, count, "config store-read must not be duplicated by a synthesized one, got %v", names)
	require.NotContains(t, names, "store-federation", "must expose the configured store-read without inventing another")
	for _, port := range comp.InputPorts() {
		facts, err := port.Facts()
		require.NoError(t, err)
		if bucket, ok := facts.StoreReadBucket(); ok {
			require.Equal(t, "MESSAGES", bucket)
		}
	}
}
