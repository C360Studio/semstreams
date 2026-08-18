package flowstore

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

func validTestFlow() Flow {
	return Flow{
		ID:   "flow-1",
		Name: "Test flow",
		Nodes: []FlowNode{{
			ID:        "node-1",
			Component: "udp",
			Type:      types.ComponentTypeInput,
			Name:      "udp-main",
			Config:    map[string]any{"port": 5000},
		}},
	}
}

func TestFlowValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Flow)
	}{
		{name: "empty ID", mutate: func(f *Flow) { f.ID = "" }},
		{name: "empty name", mutate: func(f *Flow) { f.Name = "" }},
		{name: "empty node ID", mutate: func(f *Flow) { f.Nodes[0].ID = "" }},
		{name: "empty component", mutate: func(f *Flow) { f.Nodes[0].Component = "" }},
		{name: "empty component type", mutate: func(f *Flow) { f.Nodes[0].Type = "" }},
		{name: "empty instance name", mutate: func(f *Flow) { f.Nodes[0].Name = "" }},
		{name: "duplicate node ID", mutate: func(f *Flow) { f.Nodes = append(f.Nodes, f.Nodes[0]) }},
		{name: "missing source", mutate: func(f *Flow) {
			f.Connections = []FlowConnection{{ID: "c", SourceNodeID: "missing", SourcePort: "out", TargetNodeID: "node-1", TargetPort: "in"}}
		}},
		{name: "missing target", mutate: func(f *Flow) {
			f.Connections = []FlowConnection{{ID: "c", SourceNodeID: "node-1", SourcePort: "out", TargetNodeID: "missing", TargetPort: "in"}}
		}},
		{name: "empty connection ID", mutate: func(f *Flow) {
			f.Connections = []FlowConnection{{SourceNodeID: "node-1", SourcePort: "out", TargetNodeID: "node-1", TargetPort: "in"}}
		}},
		{name: "empty source port", mutate: func(f *Flow) {
			f.Connections = []FlowConnection{{ID: "c", SourceNodeID: "node-1", TargetNodeID: "node-1", TargetPort: "in"}}
		}},
		{name: "empty target port", mutate: func(f *Flow) {
			f.Connections = []FlowConnection{{ID: "c", SourceNodeID: "node-1", SourcePort: "out", TargetNodeID: "node-1"}}
		}},
	}

	valid := validTestFlow()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid flow rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := validTestFlow()
			tt.mutate(&flow)
			err := flow.Validate()
			if err == nil || !errs.IsInvalid(err) {
				t.Fatalf("expected invalid error, got %v", err)
			}
		})
	}
}
