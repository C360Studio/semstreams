package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type exclusiveLifecycleComponent struct {
	baseDiscoverable
	port          component.NetworkPort
	initializeErr error
}

func (c *exclusiveLifecycleComponent) OutputPorts() []component.Port {
	return []component.Port{{
		Name: "net", Direction: component.DirectionOutput, Config: c.port,
	}}
}

func (c *exclusiveLifecycleComponent) Initialize() error           { return c.initializeErr }
func (c *exclusiveLifecycleComponent) Start(context.Context) error { return nil }
func (c *exclusiveLifecycleComponent) Stop(context.Context) error  { return nil }

func exclusiveComponentConfig(factory string) types.ComponentConfig {
	return types.ComponentConfig{
		Name: factory, Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`),
	}
}

func TestComponentManagerRegistryReleasesExclusiveResourceAfterRemove(t *testing.T) {
	registry := component.NewRegistry()
	port := component.NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 14550}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "exclusive", Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &exclusiveLifecycleComponent{baseDiscoverable: baseDiscoverable{name: "exclusive"}, port: port}, nil
		},
	}))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry, components: make(map[string]*component.ManagedComponent),
	}
	deps := component.Dependencies{NATSClient: new(natsclient.Client)}
	require.NoError(t, cm.CreateComponent(context.Background(), "owner", exclusiveComponentConfig("exclusive"), deps))
	require.NoError(t, cm.RemoveComponent(context.Background(), "owner"))
	require.NoError(t, cm.CreateComponent(context.Background(), "successor", exclusiveComponentConfig("exclusive"), deps),
		"Registry must release the removed generation's exclusive claim")
}

func TestComponentManagerRegistryReleasesExclusiveResourceAfterInitializeFailure(t *testing.T) {
	registry := component.NewRegistry()
	port := component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14555}
	initializeErr := errors.New("initialize failed")
	fail := true
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "exclusive", Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			candidate := &exclusiveLifecycleComponent{baseDiscoverable: baseDiscoverable{name: "exclusive"}, port: port}
			if fail {
				candidate.initializeErr = initializeErr
			}
			return candidate, nil
		},
	}))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry, components: make(map[string]*component.ManagedComponent),
	}
	deps := component.Dependencies{NATSClient: new(natsclient.Client)}
	err := cm.CreateComponent(context.Background(), "failed", exclusiveComponentConfig("exclusive"), deps)
	require.ErrorIs(t, err, initializeErr)
	fail = false
	require.NoError(t, cm.CreateComponent(context.Background(), "successor", exclusiveComponentConfig("exclusive"), deps),
		"Registry must release an admitted claim when manager initialization rolls back")
}
