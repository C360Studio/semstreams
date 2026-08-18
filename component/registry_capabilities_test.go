package component

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
)

func TestPortsToCapabilitiesUsesResolvedFacts(t *testing.T) {
	t.Parallel()

	ports := []Port{
		{
			Name:      "events",
			Direction: DirectionInput,
			Config: NATSPort{
				Subject:   "events.>",
				Interface: &InterfaceContract{Type: "example.events", Version: "v1"},
			},
		},
		{
			Name:      "requests",
			Direction: DirectionOutput,
			Config:    NATSRequestPort{Subject: "request.run"},
		},
		{
			Name:      "durable",
			Direction: DirectionInput,
			Config:    JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.created", "events.updated"}},
		},
	}

	resolved, facts, err := cloneAndProjectPorts(ports)
	if err != nil {
		t.Fatalf("cloneAndProjectPorts() error = %v", err)
	}
	got, err := portsToCapabilities(resolved, facts)
	if err != nil {
		t.Fatalf("portsToCapabilities() error = %v", err)
	}
	want := []PortCapability{
		{Name: "events", Subject: "events.>", Type: "stream", Interface: "example.events"},
		{Name: "requests", Subject: "request.run", Type: "request"},
		{Name: "durable", Subject: "events.created", Type: "stream"},
	}
	if len(got) != len(want) {
		t.Fatalf("len(portsToCapabilities()) = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("portsToCapabilities()[%d] = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestPortsToCapabilitiesRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	_, _, err := cloneAndProjectPorts([]Port{{
		Name:      "broken",
		Direction: DirectionInput,
		Config:    NATSPort{},
	}})
	if err == nil {
		t.Fatal("portsToCapabilities() error = nil, want invalid port error")
	}
}

func TestCapabilityPreparationUsesRetainedDeclarationWithoutPortReread(t *testing.T) {
	registry := NewRegistry()
	component := &declarationTestComponent{outputs: []Port{declarationTestPort("events.original")}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "source-factory", Type: "processor", Version: "v1",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return component, nil },
	}))
	_, err := registry.CreateComponent(
		componentadmission.Access{}, "source", declarationTestConfig("source-factory", `{}`), declarationTestDeps(), nil)
	requireNoError(t, err)
	if component.inputCalls != 1 || component.outputCalls != 1 {
		t.Fatalf("admission port calls = input %d output %d, want one each", component.inputCalls, component.outputCalls)
	}

	component.outputs[0] = declarationTestPort("events.mutated")
	registry.natsClient = &natsclient.Client{}
	registry.nodeID = "node-a"
	declaration, ok := registry.declaration("source")
	if !ok {
		t.Fatal("admitted declaration missing")
	}
	publication, err := registry.prepareCapabilityPublication(declaration)
	requireNoError(t, err)
	if component.inputCalls != 1 || component.outputCalls != 1 {
		t.Fatalf("capability preparation reread component ports: input %d output %d", component.inputCalls, component.outputCalls)
	}
	var announcement CapabilityAnnouncement
	if err := json.Unmarshal(publication.data, &announcement); err != nil {
		t.Fatalf("unmarshal prepared capability: %v", err)
	}
	if got := announcement.OutputPorts[0].Subject; got != "events.original" {
		t.Fatalf("prepared capability subject = %q, want retained events.original", got)
	}
}
