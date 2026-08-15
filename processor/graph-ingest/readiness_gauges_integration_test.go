//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ReadinessGaugesAreEmitted closes the #763 requirement for this
// producer on the WIRE: the spec's minimum gauge set must actually be scrapeable, not
// merely constructed. A field-level check would pass even if registration were skipped.
func gatheredValue(t *testing.T, name string) float64 {
	t.Helper()
	fams, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if g := m.GetGauge(); g != nil {
				return g.GetValue()
			}
		}
	}
	return -1
}

func TestIntegration_ReadinessGaugesAreEmitted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	defer func() { _ = tc.Terminate() }()

	cfg := DefaultConfig()
	cj, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(cj, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 100 * time.Millisecond
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(context.Background()) }()

	// Assert a VALUE this component drove, not mere presence. Presence alone is
	// satisfied by any earlier test's collectors, because DefaultRegisterer swallows
	// AlreadyRegistered — so the original form could pass before Start even ran.
	// bootstrap_complete going to 1 can only come from a real Set on a caught-up
	// producer.
	require.Eventually(t, func() bool {
		return gatheredValue(t, "semstreams_graph_ingest_bootstrap_complete") == 1
	}, 20*time.Second, 100*time.Millisecond,
		"bootstrap_complete must reach 1 — presence alone proves only that some "+
			"instance registered, not that this one projected")

	fams, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	emitted := map[string]bool{}
	for _, f := range fams {
		if strings.HasPrefix(f.GetName(), "semstreams_graph_ingest_") {
			emitted[f.GetName()] = true
		}
	}
	for _, want := range []string{
		"semstreams_graph_ingest_readiness",
		"semstreams_graph_ingest_lag",
		"semstreams_graph_ingest_bootstrap_complete",
		"semstreams_graph_ingest_readiness_state",
	} {
		require.True(t, emitted[want], "missing required series %q; emitted=%v", want, emitted)
	}
	// A backlog producer must NOT synthesize revisions.
	for name := range emitted {
		require.NotContains(t, name, "revision",
			"backlog producer emitted %q — a fabricated revision is worse than an absent one", name)
	}
}
