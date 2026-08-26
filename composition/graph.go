package composition

import (
	"sort"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/component/flowgraph"
)

// Graph is the projection of a composition: one node per component with its
// resolved ports, one edge per derived connection. It is what a diagram is
// drawn from and is never stored by the framework.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Node is one component instance with its resolved ports.
type Node struct {
	Instance string     `json:"instance"`
	Factory  string     `json:"factory"`
	Type     string     `json:"type"`
	Inputs   []PortView `json:"inputs"`
	Outputs  []PortView `json:"outputs"`
}

// PortView is one resolved port as the projection shows it.
type PortView struct {
	Name         string `json:"name"`
	Direction    string `json:"direction"`
	Type         string `json:"type"` // interface contract type, empty when undeclared
	Required     bool   `json:"required"`
	ConnectionID string `json:"connection_id"`
	Pattern      string `json:"pattern"`
	Description  string `json:"description"`
	Kind         string `json:"kind"`
}

// Edge is one derived connection between two ports.
type Edge struct {
	From         string `json:"from"`
	FromPort     string `json:"from_port"`
	To           string `json:"to"`
	ToPort       string `json:"to_port"`
	Pattern      string `json:"pattern"`
	ConnectionID string `json:"connection_id"`
}

// portViews projects one lane of resolved ports with their facts.
func portViews(ports []component.Port, facts []component.PortFacts) []PortView {
	views := make([]PortView, 0, len(ports))
	for index, port := range ports {
		view := PortView{
			Name:        port.Name,
			Direction:   string(port.Direction),
			Required:    port.Required,
			Description: port.Description,
		}
		if index < len(facts) {
			fact := facts[index]
			if contract, ok := fact.Interface(); ok {
				view.Type = contract.Type
			}
			if ids := fact.ConnectionIDs(); len(ids) > 0 {
				view.ConnectionID = ids[0]
			}
			view.Pattern = string(fact.InteractionPattern())
			view.Kind = string(fact.Kind())
		}
		views = append(views, view)
	}
	return views
}

// graphOf projects sorted declarations and, when the graph connected, its
// edges in a stable order.
func graphOf(declarations []component.Declaration, graph *flowgraph.FlowGraph) Graph {
	projection := Graph{Nodes: make([]Node, 0, len(declarations)), Edges: []Edge{}}
	for _, declaration := range declarations {
		projection.Nodes = append(projection.Nodes, Node{
			Instance: declaration.InstanceName,
			Factory:  declaration.FactoryIdentity,
			Type:     string(declaration.ComponentType),
			Inputs:   portViews(declaration.InputPorts, declaration.InputFacts),
			Outputs:  portViews(declaration.OutputPorts, declaration.OutputFacts),
		})
	}
	if graph == nil {
		return projection
	}
	for _, edge := range graph.GetEdges() {
		projection.Edges = append(projection.Edges, Edge{
			From:         edge.From.ComponentName,
			FromPort:     edge.From.PortName,
			To:           edge.To.ComponentName,
			ToPort:       edge.To.PortName,
			Pattern:      string(edge.Pattern),
			ConnectionID: edge.ConnectionID,
		})
	}
	sort.SliceStable(projection.Edges, func(i, j int) bool {
		a, b := projection.Edges[i], projection.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.FromPort != b.FromPort {
			return a.FromPort < b.FromPort
		}
		if a.To != b.To {
			return a.To < b.To
		}
		if a.ToPort != b.ToPort {
			return a.ToPort < b.ToPort
		}
		if a.Pattern != b.Pattern {
			return a.Pattern < b.Pattern
		}
		return a.ConnectionID < b.ConnectionID
	})
	return projection
}
