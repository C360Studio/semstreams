package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

// admitTypedTestComponent admits an instance under a chosen component TYPE.
// The shared helper hard-codes "processor", which cannot reach isInputNode's
// declared-type branch — the branch that decides whether a real `"type":
// "input"` component is an origin.
func admitTypedTestComponent(
	t *testing.T, registry *component.Registry,
	name string, componentType types.ComponentType, instance component.Discoverable,
) {
	t.Helper()
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: name, Type: string(componentType),
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return instance, nil
		},
		Ports: func(json.RawMessage, string) (component.PortConfig, error) {
			return component.PortConfigFrom(instance.InputPorts(), instance.OutputPorts()), nil
		},
	}))
	_, err := registry.CreateComponent(componentadmission.Access{}, name, types.ComponentConfig{
		Name: name, Type: componentType, Enabled: true, Config: json.RawMessage(`{}`),
	}, component.Dependencies{NATSClient: new(natsclient.Client)}, nil)
	require.NoError(t, err)
}

// TestFlowPathsTraverseTheRetainedGraph is the behavioural guard on the change
// task 3.3 made: `<components>/paths` stopped rebuilding a graph from the
// Registry and now walks the graph the composition result retains. That rewrite
// is silent unless something asserts the traversal itself — an empty map is a
// perfectly well-formed answer, and every other test on this surface only
// checks that the handler returns 200.
//
// It covers BOTH origin rules, because they are decided by different projected
// facts: "poller" is an origin by its declared component type, "listener" by an
// input port whose interaction pattern reaches outside the composition.
func TestFlowPathsTraverseTheRetainedGraph(t *testing.T) {
	poller := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "poller"},
		inputs: []component.Port{{
			Name: "tick", Direction: component.DirectionInput,
			Config: component.TimerPort{Interval: "30s"},
		}},
		outputs: []component.Port{{
			Name: "out", Direction: component.DirectionOutput,
			Config: component.NATSPort{Subject: "paths.test.data"},
		}},
	}
	listener := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "listener"},
		inputs: []component.Port{{
			Name: "wire", Direction: component.DirectionInput,
			Config: component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 34551},
		}},
		outputs: []component.Port{{
			Name: "out", Direction: component.DirectionOutput,
			Config: component.NATSPort{Subject: "paths.test.data"},
		}},
	}
	middle := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "middle"},
		inputs: []component.Port{{
			Name: "in", Direction: component.DirectionInput,
			Config: component.NATSPort{Subject: "paths.test.data"},
		}},
		outputs: []component.Port{{
			Name: "out", Direction: component.DirectionOutput,
			Config: component.NATSPort{Subject: "paths.test.derived"},
		}},
	}
	sink := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "sink"},
		inputs: []component.Port{{
			Name: "in", Direction: component.DirectionInput,
			Config: component.NATSPort{Subject: "paths.test.derived"},
		}},
	}

	registry := component.NewRegistry()
	admitTypedTestComponent(t, registry, "poller", types.ComponentTypeInput, poller)
	admitTypedTestComponent(t, registry, "listener", types.ComponentTypeProcessor, listener)
	admitTypedTestComponent(t, registry, "middle", types.ComponentTypeProcessor, middle)
	admitTypedTestComponent(t, registry, "sink", types.ComponentTypeOutput, sink)
	manager := newPortOwnershipCM(t, registry)
	require.NoError(t, manager.analyzeBootComposition())

	paths, err := manager.GetFlowPaths()
	require.NoError(t, err)

	want := map[string][]string{
		"poller":   {"poller", "middle", "sink"},
		"listener": {"listener", "middle", "sink"},
	}
	require.Len(t, paths, len(want),
		"paths = %#v; want one entry per origin — a declared input type AND a network-listening port", paths)
	for origin, expected := range want {
		reachable, ok := paths[origin]
		require.True(t, ok, "paths = %#v, want %q recognised as an origin", paths, origin)
		require.Equal(t, expected, reachable,
			"%q must reach the whole downstream chain in depth-first order; a wrong or missing edge shows up here",
			origin)
	}
}
