package flowgraph

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-063 Phase 2: the store federation renders as store-provide → store-read
// edges (advisory/visibility), and store ports are never reported as orphaned.

func storeProvider(name, instance string) component.Discoverable {
	return createMockComponentWithPorts(name, "storage", nil,
		[]component.Port{{
			Name:      "store-provide",
			Direction: component.DirectionOutput,
			Config:    component.StoreProvidePort{Instance: instance},
		}})
}

func federationConsumer(name string) component.Discoverable {
	return createMockComponentWithPorts(name, "processor",
		[]component.Port{{
			Name:      "store-federation",
			Direction: component.DirectionInput,
			Config:    component.StoreReadPort{Bucket: "CONTENT"},
		}}, nil)
}

func storeOrphans(res *FlowAnalysisResult) []OrphanedPort {
	var out []OrphanedPort
	for _, o := range res.OrphanedPorts {
		if o.Pattern == component.PatternStore {
			out = append(out, o)
		}
	}
	return out
}

func TestStoreFederation_ProviderToConsumerEdge(t *testing.T) {
	graph := NewFlowGraph()
	require.NoError(t, addTestComponentNode(graph, "objectstore", storeProvider("objectstore", "objectstore")))
	require.NoError(t, addTestComponentNode(graph, "graph-embedding", federationConsumer("graph-embedding")))
	require.NoError(t, graph.ConnectComponentsByPatterns())

	edges := graph.GetEdges()
	require.Len(t, edges, 1)
	e := edges[0]
	assert.Equal(t, component.PatternStore, e.Pattern)
	assert.Equal(t, "objectstore", e.From.ComponentName)
	assert.Equal(t, "graph-embedding", e.To.ComponentName)
	assert.Equal(t, "store:objectstore", e.ConnectionID)

	assert.Empty(t, storeOrphans(graph.AnalyzeConnectivity()), "connected store ports must not be orphaned")
}

func TestStoreFederation_LoneProviderNotOrphaned(t *testing.T) {
	graph := NewFlowGraph()
	require.NoError(t, addTestComponentNode(graph, "objectstore", storeProvider("objectstore", "objectstore")))
	require.NoError(t, graph.ConnectComponentsByPatterns())

	assert.Empty(t, graph.GetEdges(), "no consumer → no edge")
	// A store with no reader is legitimate, not an orphan (this was the L1 wart).
	assert.Empty(t, storeOrphans(graph.AnalyzeConnectivity()),
		"a store-provide with no consumer must not be reported orphaned")
}

func TestStoreFederation_FanIn(t *testing.T) {
	// Two providers + one federation consumer → the consumer connects to BOTH
	// (advisory: it may resolve any registered instance at runtime).
	graph := NewFlowGraph()
	require.NoError(t, addTestComponentNode(graph, "objectstore", storeProvider("objectstore", "objectstore")))
	require.NoError(t, addTestComponentNode(graph, "filestore", storeProvider("filestore", "filestore-media")))
	require.NoError(t, addTestComponentNode(graph, "graph-embedding", federationConsumer("graph-embedding")))
	require.NoError(t, graph.ConnectComponentsByPatterns())

	edges := graph.GetEdges()
	require.Len(t, edges, 2, "federation consumer connects to both providers")
	got := map[string]bool{}
	for _, e := range edges {
		assert.Equal(t, component.PatternStore, e.Pattern)
		assert.Equal(t, "graph-embedding", e.To.ComponentName)
		got[e.ConnectionID] = true
	}
	assert.True(t, got["store:objectstore"], "edge from objectstore instance")
	assert.True(t, got["store:filestore-media"], "edge from filestore instance")
}
