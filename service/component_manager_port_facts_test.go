package service

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func newPortOwnershipCM(t *testing.T, registry *component.Registry) *ComponentManager {
	t.Helper()
	if registry == nil {
		registry = component.NewRegistry()
	}
	return &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager-test", nil, WithLogger(slog.Default())),
		registry:    registry,
	}
}

func TestComponentManagerAbsentBootModelRegistryRemainsNil(t *testing.T) {
	manager := newPortOwnershipCM(t, nil)
	if manager.bootModelRegistry != nil {
		t.Fatalf("bootModelRegistry = %T, want nil", manager.bootModelRegistry)
	}
}

// TestComponentManagerPortReportingUsesCanonicalFactsWithoutDroppingKinds
// follows the property to its surviving home. It used to read
// ComponentManager.extractComponentPortInfo — a second port interpreter that
// lived beside the composition library and was reachable only from the retired
// /gaps analysis. The projection carries the same canonical facts, so the
// assertion moves rather than being deleted: every declared port appears, with
// the kind its PortFacts report, on the result Initialize retains.
func TestComponentManagerPortReportingUsesCanonicalFactsWithoutDroppingKinds(t *testing.T) {
	instance := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "facts"},
		inputs: []component.Port{
			{Name: "events", Direction: component.DirectionInput, Config: component.JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.>", "audit.>"}}},
			{Name: "entities", Direction: component.DirectionInput, Config: component.KVReadPort{Bucket: "ENTITY_STATES"}},
		},
		outputs: []component.Port{
			{Name: "request", Direction: component.DirectionOutput, Config: component.NATSRequestPort{Subject: "svc.echo"}},
		},
	}
	registry := component.NewRegistry()
	admitTestRegistryComponent(t, registry, "facts", instance)
	manager := newPortOwnershipCM(t, registry)
	if err := manager.analyzeBootComposition(); err != nil {
		t.Fatalf("analyzeBootComposition: %v", err)
	}

	result := manager.bootCompositionResult()
	if result == nil || len(result.Graph.Nodes) != 1 {
		t.Fatalf("projection nodes = %+v, want exactly the one admitted instance", result)
	}
	node := result.Graph.Nodes[0]
	if node.Instance != "facts" {
		t.Fatalf("projected instance = %q, want the admitted name", node.Instance)
	}
	if len(node.Inputs) != 2 || len(node.Outputs) != 1 {
		t.Fatalf("projected ports = inputs:%d outputs:%d", len(node.Inputs), len(node.Outputs))
	}
	if got := node.Inputs[0]; got.Kind != "jetstream" || got.Name != "events" {
		t.Fatalf("JetStream input view = %+v", got)
	}
	if got := node.Inputs[1]; got.Kind != "kv-read" || got.Name != "entities" {
		t.Fatalf("KV read input view = %+v", got)
	}
	if got := node.Outputs[0]; got.Kind != "nats-request" || got.ConnectionID != "svc.echo" {
		t.Fatalf("request output view = %+v", got)
	}
}

// TestComponentManagerProjectionCarriesOnlyAdmittedInstances replaces the
// rejection guard the removed extractComponentPortInfo carried: an instance the
// Registry never admitted has no declaration, so it cannot appear in a
// projection derived from admitted declarations.
func TestComponentManagerProjectionCarriesOnlyAdmittedInstances(t *testing.T) {
	manager := newPortOwnershipCM(t, nil)
	if err := manager.analyzeBootComposition(); err != nil {
		t.Fatalf("analyzeBootComposition: %v", err)
	}
	result := manager.bootCompositionResult()
	if result == nil {
		t.Fatal("boot composition result missing")
	}
	if len(result.Graph.Nodes) != 0 {
		t.Fatalf("projection nodes = %+v, want none for an empty registry", result.Graph.Nodes)
	}
	if _, err := manager.GetFlowPaths(); err != nil {
		t.Fatalf("GetFlowPaths over an empty composition: %v", err)
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
	// The validate/flowgraph projections serve the result Initialize retains
	// (ADR-100 P5); compute it here from the retained declarations, after the
	// live instance was mutated, so the projections prove the same point.
	if err := manager.analyzeBootComposition(); err != nil {
		t.Fatalf("analyzeBootComposition: %v", err)
	}

	result := manager.bootCompositionResult()
	if result == nil || len(result.Graph.Nodes) != 1 {
		t.Fatalf("projection = %+v, want the one retained declaration", result)
	}
	node := result.Graph.Nodes[0]
	if node.Instance != "source" || len(node.Inputs) != 1 || node.Inputs[0].ConnectionID != "events.original" {
		t.Fatalf("projected node = %#v, want retained events.original declaration", node)
	}

	handlers := map[string]http.HandlerFunc{
		"graph":      manager.handleFlowGraph,
		"validation": manager.handleFlowValidation,
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
