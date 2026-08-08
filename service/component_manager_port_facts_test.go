package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

type portFactsDiscoverable struct {
	baseDiscoverable
	inputs  []component.Port
	outputs []component.Port
}

func (c portFactsDiscoverable) InputPorts() []component.Port  { return c.inputs }
func (c portFactsDiscoverable) OutputPorts() []component.Port { return c.outputs }

func TestComponentManagerPortReportingUsesCanonicalFactsWithoutDroppingKinds(t *testing.T) {
	manager := newPortOwnershipCM(t)
	instance := portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "facts"},
		inputs: []component.Port{
			{Name: "events", Direction: component.DirectionInput, Config: component.JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.>", "audit.>"}}},
			{Name: "entities", Direction: component.DirectionInput, Config: component.KVReadPort{Bucket: "ENTITY_STATES"}},
		},
		outputs: []component.Port{
			{Name: "request", Direction: component.DirectionOutput, Config: component.NATSRequestPort{Subject: "graph.mutation.>"}},
		},
	}

	info, err := manager.extractComponentPortInfo(instance)
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

func TestComponentManagerPortReportingRejectsInvalidMutablePort(t *testing.T) {
	manager := newPortOwnershipCM(t)
	instance := portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "broken"},
		inputs: []component.Port{{
			Name: "broken", Direction: component.DirectionInput, Config: component.NATSPort{},
		}},
	}
	if _, err := manager.extractComponentPortInfo(instance); err == nil {
		t.Fatal("manager reporting accepted an invalid mutable port")
	}
}

func TestComponentManagerPortRegistrationIsAtomicOnInvalidFacts(t *testing.T) {
	manager := newPortOwnershipCM(t)
	instance := portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "broken"},
		inputs: []component.Port{
			{Name: "valid", Direction: component.DirectionInput, Config: component.NATSPort{Subject: "events.valid"}},
			{Name: "broken", Direction: component.DirectionInput, Config: component.NATSPort{}},
		},
	}

	if err := manager.registerPorts("broken", instance); err == nil {
		t.Fatal("registerPorts accepted an invalid mutable port")
	}
	if len(manager.resources) != 0 {
		t.Fatalf("failed registration retained resource ownership: %#v", manager.resources)
	}
}

func TestComponentManagerFlowReportingRejectsInvalidPortWithoutCachingPartialGraph(t *testing.T) {
	manager := newPortOwnershipCM(t)
	manager.components["broken"] = &component.ManagedComponent{Component: portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "broken"},
		inputs: []component.Port{{
			Name: "broken", Direction: component.DirectionInput, Config: component.NATSPort{},
		}},
	}}

	if _, err := manager.GetFlowGraph(); err == nil || !strings.Contains(err.Error(), `component "broken" input ports`) {
		t.Fatalf("GetFlowGraph error = %v, want rejected component context", err)
	}
	manager.graphCache.mu.RLock()
	cacheValid := manager.graphCache.cacheValid
	manager.graphCache.mu.RUnlock()
	if cacheValid {
		t.Fatal("failed partial flowgraph was cached as valid")
	}
	if _, err := manager.ValidateFlowConnectivity(); err == nil {
		t.Fatal("ValidateFlowConnectivity reported success for an invalid component port")
	}
	if _, err := manager.GetFlowPaths(); err == nil {
		t.Fatal("GetFlowPaths reported success for an invalid component port")
	}
	if _, err := manager.DetectObjectStoreGaps(); err == nil {
		t.Fatal("DetectObjectStoreGaps reported success for an invalid component port")
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
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}
