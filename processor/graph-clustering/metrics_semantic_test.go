package graphclustering

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fully-qualified Prometheus names (Namespace_Subsystem_Name) of the THREE
// semantic-tier series that must appear ONLY on an enabled deployment (Codex P2#4/P2#5).
const (
	semanticEdgesAppliedMetricName = "semstreams_graph_clustering_semantic_edges_applied"
	semanticEdgeBuildMsMetricName  = "semstreams_graph_clustering_semantic_edge_build_ms"
	semanticEdgeQueriesMetricName  = "semstreams_graph_clustering_semantic_edge_similar_queries_total"
)

// semanticMetricNames is the closed set of series a DISABLED deployment must not export
// and an ENABLED one must export.
var semanticMetricNames = []string{
	semanticEdgesAppliedMetricName,
	semanticEdgeBuildMsMetricName,
	semanticEdgeQueriesMetricName,
}

// registryExportsMetric reports whether a scrape of the registry would carry the named
// metric family — the SCRAPE-level view an operator actually sees, not the in-process
// gauge object.
func registryExportsMetric(t *testing.T, reg *metric.MetricsRegistry, name string) bool {
	t.Helper()
	families, err := reg.PrometheusRegistry().Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() == name {
			return true
		}
	}
	return false
}

// basePortsJSON is the minimal valid port block, inline so this test drives the real
// factory config path.
const basePortsJSON = `"ports":{` +
	`"inputs":[{"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}],` +
	`"outputs":[{"name":"communities","config":{"kind":"kv-write","bucket":"COMMUNITY_INDEX"}}]}`

// TestSemanticMetrics_DisabledDeploymentExportsNoSemanticSeries is the Codex P2#4/P2#5
// scrape-level regression: a DEFAULT-OFF (disabled) deployment must export NONE of the
// three semantic-tier series, so its metric surface is byte-identical to a pre-tier
// build. A registered-but-unset semantic_edges_applied scrapes 0 — indistinguishable
// from an enabled-but-cold structural-only cycle (#618) — and the §7 refresh-cost series
// have no business appearing on a deployment that never runs a refresh (default-off
// identical). All three appear only for a deployment that opted in.
func TestSemanticMetrics_DisabledDeploymentExportsNoSemanticSeries(t *testing.T) {
	t.Run("disabled: all three semantic series absent (true n/a)", func(t *testing.T) {
		reg := metric.NewMetricsRegistry()
		deps := component.Dependencies{NATSClient: &natsclient.Client{}, MetricsRegistry: reg}
		_, err := CreateGraphClustering([]byte("{"+basePortsJSON+"}"), deps)
		require.NoError(t, err)
		for _, name := range semanticMetricNames {
			assert.False(t, registryExportsMetric(t, reg, name),
				"a disabled deployment must NOT export %s (default-off must not change the exported surface)", name)
		}
	})

	t.Run("enabled: all three semantic series exported", func(t *testing.T) {
		reg := metric.NewMetricsRegistry()
		deps := component.Dependencies{NATSClient: &natsclient.Client{}, MetricsRegistry: reg}
		cfg := "{" + basePortsJSON + `,"semantic_edges":{"enable_semantic_edges":true}}`
		_, err := CreateGraphClustering([]byte(cfg), deps)
		require.NoError(t, err)
		for _, name := range semanticMetricNames {
			assert.True(t, registryExportsMetric(t, reg, name),
				"an enabled deployment must export %s", name)
		}
	})
}
