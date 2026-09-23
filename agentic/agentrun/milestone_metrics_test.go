package agentrun

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
)

// milestoneDecisionSeries reads the decisions counter back out of the registry
// a /metrics scrape would gather, keyed by its label values. Reading the vec
// the subscriber holds would prove only that the vec works; what must be proven
// is that the series reached the registry.
func milestoneDecisionSeries(t *testing.T, registry *metric.MetricsRegistry) map[string]float64 {
	t.Helper()
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	series := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "semstreams_agentrun_milestone_decisions_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			key := ""
			for _, label := range m.GetLabel() {
				key += label.GetName() + "=" + label.GetValue() + " "
			}
			series[key] = m.GetCounter().GetValue()
		}
	}
	return series
}

// TestMilestoneDecisionsCounterIsRegisteredOnce is task 5.2. The counter vec is
// built in the constructor so a delivery never nil-dereferences, which also
// means every increment before registration is a LOCAL no-op — counts that look
// recorded and reach no scrape. This drives real deliveries through the
// production lane and reads the counts back through the registry, so the
// registration and the increment are proven on the same path.
func TestMilestoneDecisionsCounterIsRegisteredOnce(t *testing.T) {
	t.Parallel()
	registry := metric.NewMetricsRegistry()
	runEntityID := agentic.ChainExecutionEntityID("acme", "ops", "counted-run")
	sub := NewMilestoneSubscriberWithRunStateReader(
		&stubRunReader{participant: &AgentRun{EntityIDField: runEntityID, PhaseField: "executing"}},
		stubTripleReader{}, "acme", "ops", slog.New(slog.NewTextHandler(discard{}, nil)),
	)
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		return semerrs.WrapTransient(errors.New("not ready"), "product", "OnLoopTerminal", "commit")
	}))

	require.NoError(t, sub.RegisterMetrics(registry))
	require.Empty(t, milestoneDecisionSeries(t, registry),
		"a registered vec with no observations publishes no series")

	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)
	data := terminalBytes(t, "counted-loop", runEntityID)
	lane.deliver(t, data)

	const retried = "decision=retry lane=complete reason=" + reasonHandlerTransient + " "
	assert.Equal(t, map[string]float64{retried: 1}, milestoneDecisionSeries(t, registry),
		"the increment must be readable where /metrics reads")

	// Registration is idempotent AND does not fork a second series: a root that
	// registers twice (or a second registrar over the same registry) must not
	// split one counter into two.
	require.NoError(t, sub.RegisterMetrics(registry), "a repeat registration is a no-op, not a failure")
	lane.deliver(t, data)
	assert.Equal(t, map[string]float64{retried: 2}, milestoneDecisionSeries(t, registry),
		"re-registration must keep counting on the same series")
}

// TestMilestoneRegisterMetricsRefusesANilRegistrar keeps the method from
// answering "registered" to a root that holds no registry. Accepting nil would
// leave the counter local and silent — the phantom-signal shape the method
// exists to remove — with a nil error saying it worked.
func TestMilestoneRegisterMetricsRefusesANilRegistrar(t *testing.T) {
	t.Parallel()
	sub := quietSubscriber(&stubRunReader{}, stubTripleReader{}, "acme")
	err := sub.RegisterMetrics(nil)
	require.Error(t, err)
	assert.True(t, semerrs.IsInvalid(err), "a nil registrar is a wiring defect, not a transient failure")
}

// TestMilestoneDecisionsCounterCarriesTheRuledIdentity pins the metric name and
// label set the owner ruled (Q3, 2026-09-18). The name and labels are the
// operator contract; a rename is a spec change, so a silent drift here would
// break every dashboard without breaking a test.
func TestMilestoneDecisionsCounterCarriesTheRuledIdentity(t *testing.T) {
	t.Parallel()
	registry := metric.NewMetricsRegistry()
	sub := quietSubscriber(&stubRunReader{}, stubTripleReader{}, "acme")
	require.NoError(t, sub.RegisterMetrics(registry))

	sub.decisions.WithLabelValues(milestoneLaneFailed, "terminate", reasonDecode).Inc()
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	var found *prometheus.Labels
	for _, family := range families {
		if family.GetName() != "semstreams_agentrun_milestone_decisions_total" {
			continue
		}
		labels := prometheus.Labels{}
		for _, label := range family.GetMetric()[0].GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		found = &labels
	}
	require.NotNil(t, found, "semstreams_agentrun_milestone_decisions_total is not published")
	assert.Equal(t, prometheus.Labels{
		"lane": milestoneLaneFailed, "decision": "terminate", "reason": reasonDecode,
	}, *found)
}
