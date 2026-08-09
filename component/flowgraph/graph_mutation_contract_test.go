package flowgraph

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

func TestGraphMutationFlowAllowsManyRequestersAndOneProvider(t *testing.T) {
	flow := NewFlowGraph()
	addMutationProvider(t, flow, "graph-ingest", mutationPort(component.DirectionInput))
	for _, name := range []string{"rules", "lifecycle", "tools"} {
		addMutationRequester(t, flow, name, mutationPort(component.DirectionOutput))
	}
	if err := flow.ConnectComponentsByPatterns(); err != nil {
		t.Fatalf("ConnectComponentsByPatterns: %v", err)
	}
	if got := len(flow.GetEdges()); got != 3 {
		t.Fatalf("mutation edges = %d, want 3", got)
	}
}

func TestGraphMutationFlowRejectsInvalidProviderTopology(t *testing.T) {
	tests := []struct {
		name      string
		providers []component.Port
		want      string
	}{
		{name: "missing", want: "exactly one compatible provider"},
		{name: "multiple", providers: []component.Port{mutationPort(component.DirectionInput), mutationPort(component.DirectionInput)}, want: "found 2"},
		{name: "wrong interface", providers: []component.Port{mutationPortWith(component.DirectionInput, graphmutation.SubjectFamily, "other.interface", graphmutation.InterfaceVersion)}, want: "not a required nats-request"},
		{name: "wrong version", providers: []component.Port{mutationPortWith(component.DirectionInput, graphmutation.SubjectFamily, graphmutation.InterfaceType, "v2")}, want: "not a required nats-request"},
		{name: "wrong family", providers: []component.Port{mutationPortWith(component.DirectionInput, "graph.mutation.*", graphmutation.InterfaceType, graphmutation.InterfaceVersion)}, want: "not a required nats-request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := NewFlowGraph()
			addMutationRequester(t, flow, "requester", mutationPort(component.DirectionOutput))
			for index, port := range tt.providers {
				addMutationProvider(t, flow, "provider-"+string(rune('a'+index)), port)
			}
			err := flow.ConnectComponentsByPatterns()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestGraphMutationFlowRejectsUntypedLegacyRequester(t *testing.T) {
	for _, subject := range []string{"graph.mutation.*", "graph.mutation.entity.create"} {
		t.Run(subject, func(t *testing.T) {
			flow := NewFlowGraph()
			addMutationProvider(t, flow, "graph-ingest", mutationPort(component.DirectionInput))
			legacy := component.Port{
				Name:      "legacy",
				Direction: component.DirectionOutput,
				Required:  true,
				Config:    component.NATSRequestPort{Subject: subject},
			}
			addMutationRequester(t, flow, "requester", legacy)
			err := flow.ConnectComponentsByPatterns()
			if err == nil || !strings.Contains(err.Error(), "not a required nats-request") {
				t.Fatalf("error = %v, want typed-port rejection", err)
			}
		})
	}
}

func mutationPort(direction component.Direction) component.Port {
	return mutationPortWith(direction, graphmutation.SubjectFamily, graphmutation.InterfaceType, graphmutation.InterfaceVersion)
}

func mutationPortWith(direction component.Direction, family, interfaceType, version string) component.Port {
	return component.Port{
		Name:      "mutations",
		Direction: direction,
		Required:  true,
		Config: component.NATSRequestPort{
			Subject: family,
			Interface: &component.InterfaceContract{
				Type: interfaceType, Version: version,
			},
		},
	}
}

func addMutationProvider(t *testing.T, flow *FlowGraph, name string, port component.Port) {
	t.Helper()
	provider := createMockComponentWithPorts(name, "processor", []component.Port{port}, nil)
	if err := addTestComponentNode(flow, name, provider); err != nil {
		t.Fatalf("AddComponentNode(%s): %v", name, err)
	}
}

func addMutationRequester(t *testing.T, flow *FlowGraph, name string, port component.Port) {
	t.Helper()
	requester := createMockComponentWithPorts(name, "processor", nil, []component.Port{port})
	if err := addTestComponentNode(flow, name, requester); err != nil {
		t.Fatalf("AddComponentNode(%s): %v", name, err)
	}
}
