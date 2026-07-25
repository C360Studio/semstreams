package graphclustering

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// semanticEdgesAppliedMetricName is the fully-qualified Prometheus name the
// semantic_edges_applied gauge exports (Namespace_Subsystem_Name).
const semanticEdgesAppliedMetricName = "semstreams_graph_clustering_semantic_edges_applied"

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
	`"inputs":[{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],` +
	`"outputs":[{"name":"communities","type":"kv-write","subject":"COMMUNITY_INDEX"}]}`

// TestSemanticEdgesApplied_DisabledDeploymentDoesNotExportMisleadingZero is the Codex
// P2#4 scrape-level regression: a DEFAULT-OFF (disabled) deployment must not export
// semantic_edges_applied at all, because a registered-but-unset gauge scrapes 0 — and a
// 0 is indistinguishable from an enabled-but-cold structural-only cycle, the exact #618
// confusion the gauge exists to resolve. The series is a true n/a for disabled and only
// appears for a deployment that opted in.
func TestSemanticEdgesApplied_DisabledDeploymentDoesNotExportMisleadingZero(t *testing.T) {
	t.Run("disabled: series absent (true n/a, not a misleading 0)", func(t *testing.T) {
		reg := metric.NewMetricsRegistry()
		deps := component.Dependencies{NATSClient: &natsclient.Client{}, MetricsRegistry: reg}
		_, err := CreateGraphClustering([]byte("{"+basePortsJSON+"}"), deps)
		require.NoError(t, err)
		assert.False(t, registryExportsMetric(t, reg, semanticEdgesAppliedMetricName),
			"a disabled deployment must NOT export semantic_edges_applied — a scraped 0 reads as enabled-but-cold (#618)")
	})

	t.Run("enabled: series exported so the #618 signal is scrapeable", func(t *testing.T) {
		reg := metric.NewMetricsRegistry()
		deps := component.Dependencies{NATSClient: &natsclient.Client{}, MetricsRegistry: reg}
		cfg := "{" + basePortsJSON + `,"semantic_edges":{"enable_semantic_edges":true}}`
		_, err := CreateGraphClustering([]byte(cfg), deps)
		require.NoError(t, err)
		assert.True(t, registryExportsMetric(t, reg, semanticEdgesAppliedMetricName),
			"an enabled deployment must export semantic_edges_applied")
	})
}
