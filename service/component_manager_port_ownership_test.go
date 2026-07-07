package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
)

// exclusivePortComponent is a Discoverable that binds an exclusive network
// port (component.NetworkPort.IsExclusive() == true), for exercising the
// port-ownership bookkeeping in ComponentManager.
type exclusivePortComponent struct {
	baseDiscoverable
	port component.NetworkPort
}

func (c *exclusivePortComponent) OutputPorts() []component.Port {
	return []component.Port{{
		Name:      "net",
		Direction: component.DirectionOutput,
		Config:    c.port,
	}}
}

func newPortOwnershipCM(t *testing.T) *ComponentManager {
	t.Helper()
	deps := &Dependencies{
		Logger:            slog.Default(),
		ComponentRegistry: component.NewRegistry(),
	}
	svc, err := NewComponentManager(json.RawMessage(`{}`), deps)
	require.NoError(t, err)
	cm, ok := svc.(*ComponentManager)
	require.True(t, ok)
	return cm
}

// TestComponentManager_FreesExclusivePortOnStopAndRemove is a regression test
// for gh#417: the reconcile-stop / reconfig teardown paths deleted a component
// from cm.components WITHOUT calling unregisterPorts, leaking its exclusive
// port ownership in cm.resources. A later instance binding the same port then
// failed checkPortConflicts with "exclusive resource ... already used", and the
// reconfig silently continued on the old config. stopAndRemoveComponent and
// restartComponentWithNewConfig share the fixed Step-3 block.
func TestComponentManager_FreesExclusivePortOnStopAndRemove(t *testing.T) {
	cm := newPortOwnershipCM(t)

	const name = "net-output"
	comp := &exclusivePortComponent{
		baseDiscoverable: baseDiscoverable{name: name},
		port:             component.NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 14550},
	}

	// Simulate a created + registered component (what CreateComponent does).
	cm.mu.Lock()
	cm.components[name] = &component.ManagedComponent{Component: comp, State: component.StateInitialized}
	cm.registerPorts(name, comp)
	mc := cm.components[name]
	cm.mu.Unlock()

	// Sanity: the exclusive resource is owned, so a fresh instance conflicts.
	require.Error(t, cm.checkPortConflicts(comp),
		"exclusive port should be owned before teardown")

	// Reconcile-stop the component.
	require.NoError(t, cm.stopAndRemoveComponent(context.Background(), name, mc))

	// The exclusive resource must now be free — no stale owner (gh#417).
	require.NoError(t, cm.checkPortConflicts(comp),
		"exclusive port must be freed after stopAndRemoveComponent")
}

// TestComponentManager_UnregisterPortsForCompFreesOwnership covers the
// Initialize-failure path (gh#417): CreateComponent calls registerPorts BEFORE
// Initialize, so a component whose Initialize fails never lands in
// cm.components — unregisterPorts-by-name can't find it, and cleanup must go
// through unregisterPortsForComp.
func TestComponentManager_UnregisterPortsForCompFreesOwnership(t *testing.T) {
	cm := newPortOwnershipCM(t)

	const name = "net-init-fail"
	comp := &exclusivePortComponent{
		baseDiscoverable: baseDiscoverable{name: name},
		port:             component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14555},
	}

	// Ownership recorded before the component is committed to cm.components.
	cm.mu.Lock()
	cm.registerPorts(name, comp)
	cm.mu.Unlock()
	require.Error(t, cm.checkPortConflicts(comp), "exclusive port should be owned after registerPorts")

	cm.mu.Lock()
	cm.unregisterPortsForComp(name, comp)
	cm.mu.Unlock()

	require.NoError(t, cm.checkPortConflicts(comp),
		"unregisterPortsForComp must free ownership for a component never in cm.components")
}
