package service

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/stretchr/testify/require"
)

type portFactsDiscoverable struct {
	baseDiscoverable
	inputs  []component.Port
	outputs []component.Port
}

func (c portFactsDiscoverable) InputPorts() []component.Port  { return c.inputs }
func (c portFactsDiscoverable) OutputPorts() []component.Port { return c.outputs }

func newPortOwnershipCM(t *testing.T, registry *component.Registry) *ComponentManager {
	t.Helper()
	if registry == nil {
		registry = component.NewRegistry()
	}
	selection, err := flowstore.SelectBoot(&config.Config{}, nil)
	require.NoError(t, err)
	serviceInstance, err := NewComponentManager(json.RawMessage(`{}`), &Dependencies{
		Logger: slog.Default(), ComponentRegistry: registry, BootSelection: selection,
	})
	require.NoError(t, err)
	return serviceInstance.(*ComponentManager)
}

func TestComponentManagerAbsentBootModelRegistryRemainsNil(t *testing.T) {
	manager := newPortOwnershipCM(t, nil)
	if manager.bootModelRegistry != nil {
		t.Fatalf("bootModelRegistry = %T, want nil", manager.bootModelRegistry)
	}
}

func TestComponentManagerPortReportingUsesCanonicalFactsWithoutDroppingKinds(t *testing.T) {
	instance := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "facts"},
		inputs: []component.Port{
			{Name: "events", Direction: component.DirectionInput, Config: component.JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.>", "audit.>"}}},
			{Name: "entities", Direction: component.DirectionInput, Config: component.KVReadPort{Bucket: "ENTITY_STATES"}},
		},
		outputs: []component.Port{
			{Name: "request", Direction: component.DirectionOutput, Config: component.NATSRequestPort{Subject: "graph.mutation.>"}},
		},
	}
	registry := component.NewRegistry()
	admitTestRegistryComponent(t, registry, "facts", instance)
	manager := newPortOwnershipCM(t, registry)

	info, err := manager.extractComponentPortInfo("facts")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.InputPorts) != 2 || len(info.OutputPorts) != 1 {
		t.Fatalf("reported ports = inputs:%d outputs:%d", len(info.InputPorts), len(info.OutputPorts))
	}
	if got := info.InputPorts[0]; got.PortType != "jetstream" || got.Subject != "events.>" {
		t.Fatalf("JetStream detail = %+v", got)
	}
	if got := info.InputPorts[1]; got.PortType != "kv-read" || got.Subject != "" {
		t.Fatalf("KV read detail = %+v", got)
	}
	if got := info.OutputPorts[0]; got.PortType != "nats-request" || got.Subject != "graph.mutation.>" {
		t.Fatalf("request detail = %+v", got)
	}
}

func TestComponentManagerPortReportingRejectsUnadmittedInstance(t *testing.T) {
	manager := newPortOwnershipCM(t, nil)
	if _, err := manager.extractComponentPortInfo("missing"); err == nil {
		t.Fatal("manager reporting accepted an unadmitted instance")
	}
}

func TestComponentManagerFlowReportingUsesRetainedPortsAfterComponentMutation(t *testing.T) {
	instance := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "source"},
		inputs: []component.Port{{
			Name: "events", Direction: component.DirectionInput,
			Config: component.NATSPort{Subject: "events.original"},
		}},
	}
	registry := component.NewRegistry()
	admitTestRegistryComponent(t, registry, "source", instance)
	manager := newPortOwnershipCM(t, registry)
	instance.inputs[0].Config = component.NATSPort{}

	graph, err := manager.GetFlowGraph()
	if err != nil {
		t.Fatalf("GetFlowGraph reread mutated component declaration: %v", err)
	}
	node := graph.GetNodes()["source"]
	if node == nil || len(node.InputPorts) != 1 || node.InputPorts[0].ConnectionID != "events.original" {
		t.Fatalf("flowgraph node = %#v, want retained events.original declaration", node)
	}

	handlers := map[string]http.HandlerFunc{
		"graph":      manager.handleFlowGraph,
		"validation": manager.handleFlowValidation,
		"gaps":       manager.handleFlowGaps,
		"paths":      manager.handleFlowPaths,
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/flow/"+name, nil)
			handler(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusOK)
			}
		})
	}
}
