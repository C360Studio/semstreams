package composition

import (
	"fmt"
	"sort"
	"strings"
)

// Mermaid renders the projection as a `flowchart LR`: one node per component
// and one edge per derived connection, both in a stable order. Layout is the
// viewer's job.
func Mermaid(graph Graph) string {
	nodes := append([]Node(nil), graph.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Instance < nodes[j].Instance })
	ids := make(map[string]string, len(nodes))
	var builder strings.Builder
	builder.WriteString("flowchart LR\n")
	for index, node := range nodes {
		id := fmt.Sprintf("n%d", index)
		ids[node.Instance] = id
		fmt.Fprintf(&builder, "    %s[\"%s (%s)\"]\n", id, mermaidLabel(node.Instance), mermaidLabel(node.Factory))
	}
	edges := append([]Edge(nil), graph.Edges...)
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
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
	for _, edge := range edges {
		from, ok := ids[edge.From]
		if !ok {
			continue
		}
		to, ok := ids[edge.To]
		if !ok {
			continue
		}
		fmt.Fprintf(&builder, "    %s -- \"%s → %s [%s]\" --> %s\n",
			from, mermaidLabel(edge.FromPort), mermaidLabel(edge.ToPort), mermaidLabel(edge.Pattern), to)
	}
	return builder.String()
}

func mermaidLabel(text string) string {
	return strings.ReplaceAll(text, `"`, "#quot;")
}
