package readiness

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/prometheus/client_golang/prometheus"
)

// TestGauges_MetricNamesArePinned is the external-contract gate.
//
// Emitted metric names are consumed by dashboards and operator alerts, and the
// graph-index-readiness spec explicitly protects graph-index's published output. This
// pins the EXACT set the two hand-rolled implementations emitted before the shared set
// replaced them, so "we preserved the names" is a gate rather than a claim.
//
// The revision-lag list is transcribed from the pre-existing registration blocks
// (processor/graph-index/metrics.go and processor/graph-embedding/metrics.go), which
// registered an identical set for both producers.
func TestGauges_MetricNamesArePinned(t *testing.T) {
	t.Run("revision-lag producer", func(t *testing.T) {
		// Exactly what graph-index and graph-embedding registered before, PLUS
		// bootstrap_complete — the field both independently omitted and the one
		// unmet spec requirement this closes.
		want := []string{
			"readiness",
			"lag",
			"bootstrap_complete",
			"indexed_revision",
			"target_revision",
			"readiness_state",
			"status_publish_failures_total",
		}
		got := NewGauges(
			ProducerNames{Service: "graph-index", Subsystem: "graph_index"},
			WithRevisionGauges(),
		).MetricNames()
		assertNames(t, got, want)
	})

	t.Run("backlog producer omits revision gauges", func(t *testing.T) {
		// A backlog producer SHALL NOT expose revision gauges and SHALL NOT
		// synthesize a value — a fabricated revision is worse than an absent one.
		want := []string{
			"readiness",
			"lag",
			"bootstrap_complete",
			"readiness_state",
			"status_publish_failures_total",
		}
		got := NewGauges(
			ProducerNames{Service: "graph-ingest", Subsystem: "graph_ingest"},
		).MetricNames()
		assertNames(t, got, want)
	})
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("emitted %d names, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestGauges_FullyQualifiedNamesPreserveTheWireContract asserts on the names Prometheus
// actually EXPOSES, not just the registry keys. A correct registry key with a wrong
// namespace or subsystem would still rename the series a dashboard queries.
func TestGauges_FullyQualifiedNamesPreserveTheWireContract(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGauges(
		ProducerNames{Service: "graph-index", Subsystem: "graph_index"},
		WithRevisionGauges(),
	)
	for _, c := range g.collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	g.Set(graph.IndexStatusResponse{State: graph.IndexStateReady})

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	emitted := make(map[string]bool, len(families))
	for _, f := range families {
		emitted[f.GetName()] = true
	}

	// These are the fully-qualified series the pre-existing producers emitted.
	for _, want := range []string{
		"semstreams_graph_index_readiness",
		"semstreams_graph_index_lag",
		"semstreams_graph_index_indexed_revision",
		"semstreams_graph_index_target_revision",
		"semstreams_graph_index_readiness_state",
		"semstreams_graph_index_status_publish_failures_total",
		// New, and the point of the exercise.
		"semstreams_graph_index_bootstrap_complete",
	} {
		if !emitted[want] {
			t.Errorf("missing series %q — a dashboard querying it would go dark; got %v",
				want, keys(emitted))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestGauges_SetProjectsTheEnvelope pins the projection, including the two things that
// are easy to get subtly wrong.
func TestGauges_SetProjectsTheEnvelope(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGauges(ProducerNames{Service: "t", Subsystem: "t"}, WithRevisionGauges())
	for _, c := range g.collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	g.Set(graph.IndexStatusResponse{
		Ready:             true,
		State:             graph.IndexStateReady,
		BootstrapComplete: true,
		Lag:               7,
		IndexedRevision:   100,
		TargetRevision:    107,
	})

	if got := gaugeValue(t, reg, "semstreams_t_bootstrap_complete"); got != 1 {
		t.Errorf("bootstrap_complete = %v, want 1", got)
	}
	if got := gaugeValue(t, reg, "semstreams_t_lag"); got != 7 {
		t.Errorf("lag = %v, want 7", got)
	}

	// The one-hot must iterate the CLOSED state domain, so a state this producer is
	// not currently in renders as an explicit 0 rather than an absent series —
	// absent reads as "no data", which is not the same as "not in this state".
	states := labeledValues(t, reg, "semstreams_t_readiness_state", "state")
	if len(states) != len(graph.AllIndexStates) {
		t.Errorf("one-hot emitted %d states, want all %d: %v",
			len(states), len(graph.AllIndexStates), states)
	}
	if states[graph.IndexStateReady] != 1 || states[graph.IndexStateDegraded] != 0 {
		t.Errorf("one-hot wrong: %v", states)
	}
}

// TestGauges_BacklogProducerNeverPublishesARevision is the fail-closed guard on the
// spec's explicit prohibition: a backlog producer must not synthesize a revision, and
// publishing a zero-valued gauge WOULD be synthesizing one.
func TestGauges_BacklogProducerNeverPublishesARevision(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGauges(ProducerNames{Service: "graph-ingest", Subsystem: "graph_ingest"})
	for _, c := range g.collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	// An envelope that (wrongly) carried revisions must still not produce the series.
	g.Set(graph.IndexStatusResponse{
		State:           graph.IndexStateReady,
		IndexedRevision: 42,
		TargetRevision:  99,
	})

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if strings.Contains(f.GetName(), "revision") {
			t.Errorf("backlog producer published %q — a fabricated revision is worse "+
				"than an absent one", f.GetName())
		}
	}
}

func gaugeValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		if len(f.GetMetric()) == 0 {
			t.Fatalf("%s has no metrics", name)
		}
		return f.GetMetric()[0].GetGauge().GetValue()
	}
	t.Fatalf("series %q not found", name)
	return 0
}

func labeledValues(t *testing.T, reg *prometheus.Registry, name, label string) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := map[string]float64{}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == label {
					out[lp.GetValue()] = m.GetGauge().GetValue()
				}
			}
		}
	}
	return out
}
