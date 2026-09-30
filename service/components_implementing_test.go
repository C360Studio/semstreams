package service

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type seamProbe interface{ seamName() string }

type seamComponent struct {
	baseDiscoverable
	instance string
}

func (c *seamComponent) seamName() string          { return c.instance }
func (*seamComponent) Initialize() error           { return nil }
func (*seamComponent) Start(context.Context) error { return nil }
func (*seamComponent) Stop(context.Context) error  { return nil }

type plainComponent struct{ baseDiscoverable }

func (*plainComponent) Initialize() error           { return nil }
func (*plainComponent) Start(context.Context) error { return nil }
func (*plainComponent) Stop(context.Context) error  { return nil }

// The composition root finds its rule hot-reload targets through this walk;
// it must return exactly the enabled components the ComponentManager built
// that implement the seam, and nothing when there is no component manager.
func TestComponentsImplementingReturnsTheBuiltComponentsThatImplementTheSeam(t *testing.T) {
	t.Parallel()
	registry := component.NewRegistry()
	noPorts := func(json.RawMessage, string) (component.PortConfig, error) { return component.PortConfig{}, nil }
	require.NoError(t, registry.RegisterFactory("seam", &component.Registration{
		Name: "seam", Type: "processor", Ports: noPorts,
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Instance string `json:"instance"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return &seamComponent{baseDiscoverable: baseDiscoverable{name: "seam"}, instance: cfg.Instance}, nil
		},
	}))
	require.NoError(t, registry.RegisterFactory("plain", &component.Registration{
		Name: "plain", Type: "processor", Ports: noPorts,
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &plainComponent{baseDiscoverable{name: "plain"}}, nil
		},
	}))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry,
		natsClient:  new(natsclient.Client),
		componentConfigs: config.ComponentConfigs{
			"a": {Type: types.ComponentTypeProcessor, Name: "seam", Enabled: true, Config: json.RawMessage(`{"instance":"a"}`)},
			"b": {Type: types.ComponentTypeProcessor, Name: "seam", Enabled: true, Config: json.RawMessage(`{"instance":"b"}`)},
			"off": {Type: types.ComponentTypeProcessor, Name: "seam", Enabled: false,
				Config: json.RawMessage(`{"instance":"off"}`)},
			"p": {Type: types.ComponentTypeProcessor, Name: "plain", Enabled: true, Config: json.RawMessage(`{}`)},
		},
		components: make(map[string]*component.ManagedComponent),
	}
	require.NoError(t, cm.Initialize())

	manager := NewServiceManager(NewServiceRegistry())
	require.Empty(t, ComponentsImplementing[seamProbe](manager), "no component manager registered yet")
	manager.RegisterInstance("component-manager", cm)

	found := ComponentsImplementing[seamProbe](manager)
	names := make([]string, 0, len(found))
	for _, probe := range found {
		names = append(names, probe.seamName())
	}
	sort.Strings(names)
	require.Equal(t, []string{"a", "b"}, names)
	require.Len(t, ComponentsImplementing[component.Discoverable](manager), 3)
	require.Nil(t, ComponentsImplementing[seamProbe](nil))
}
