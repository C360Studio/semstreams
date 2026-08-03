package graphembedding

import (
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph/embedding"
)

// TestWorkerMetricsAdapter_NewSignalsHaveRealConsumers proves the dedup_skipped and
// text_truncated metrics are not phantom registrations: the production
// WorkerMetrics consumer (workerMetricsAdapter) forwards the worker's calls to
// registered Prometheus counters that actually move. The worker-side consumer is
// asserted separately in graph/embedding; this closes the prometheus half (#623/#602).
func TestWorkerMetricsAdapter_NewSignalsHaveRealConsumers(t *testing.T) {
	m := getMetrics(nil)
	failuresVec := resolveEmbeddingFailuresVec(nil)
	adapter := newWorkerMetricsAdapter(m, failuresVec, nil)

	// failures_total{reason} moves through the per-registry counter (#613).
	const failReason = "connection_refused"
	fbefore := testutil.ToFloat64(failuresVec.WithLabelValues(failReason))
	adapter.IncFailedReason(failReason)
	require.Equal(t, fbefore+1, testutil.ToFloat64(failuresVec.WithLabelValues(failReason)),
		"IncFailedReason must increment the per-registry failures_total counter")

	// dedup_skipped_total{reason}
	const reason = "identity_unresolved"
	before := testutil.ToFloat64(m.dedupSkipped.WithLabelValues(reason))
	adapter.IncDedupSkipped(reason)
	require.Equal(t, before+1, testutil.ToFloat64(m.dedupSkipped.WithLabelValues(reason)),
		"IncDedupSkipped must increment the registered dedup_skipped_total counter")

	// text_truncated_total
	tbefore := testutil.ToFloat64(m.textTruncated)
	adapter.IncTruncated()
	require.Equal(t, tbefore+1, testutil.ToFloat64(m.textTruncated),
		"IncTruncated must increment the registered text_truncated_total counter")

	// offloaded_identity_included_total (#601)
	ibefore := testutil.ToFloat64(m.offloadedIdentityIncluded)
	adapter.IncOffloadedIdentityIncluded()
	require.Equal(t, ibefore+1, testutil.ToFloat64(m.offloadedIdentityIncluded),
		"IncOffloadedIdentityIncluded must increment the registered offloaded_identity_included_total counter")

	// offloaded_identity_absent_total (#601)
	abefore := testutil.ToFloat64(m.offloadedIdentityAbsent)
	adapter.IncOffloadedIdentityAbsent()
	require.Equal(t, abefore+1, testutil.ToFloat64(m.offloadedIdentityAbsent),
		"IncOffloadedIdentityAbsent must increment the registered offloaded_identity_absent_total counter")
}

// TestWorkerMetricsAdapter_ContentExcludedRoutesToTheComponentReporter proves hop 2's
// unresolvable-instance report reaches the SAME exclusion reporter hop 1's gate uses
// (gh#875) rather than a second spelling of it: content_unresolved_total moves, and the
// one-shot operator warning naming the instance and the remedy fires exactly once.
//
// The wiring under test is the constructor argument in Start
// (newWorkerMetricsAdapter(..., c.reportOffloadedContentExcluded)); this asserts the
// adapter half, and the worker half (that hop 2 actually calls it) is asserted in
// graph/embedding.
func TestWorkerMetricsAdapter_ContentExcludedRoutesToTheComponentReporter(t *testing.T) {
	wc := &warnCounter{}
	c := &Component{metrics: getMetrics(nil), logger: slog.New(wc)}

	var reporter embedding.WorkerMetrics = newWorkerMetricsAdapter(
		c.metrics, resolveEmbeddingFailuresVec(nil), c.reportOffloadedContentExcluded)

	before := testutil.ToFloat64(c.metrics.contentUnresolved)
	reporter.ReportContentExcluded("c360.platform.test.sys.doc.001", "AGENT_CONTENT")
	reporter.ReportContentExcluded("c360.platform.test.sys.doc.002", "AGENT_CONTENT")

	require.Equal(t, float64(2), testutil.ToFloat64(c.metrics.contentUnresolved)-before,
		"every hop-2 exclusion must count on content_unresolved_total, the same counter hop 1 uses")
	require.Equal(t, int64(1), wc.n.Load(),
		"the operator warning is one-shot per component, not per entity")

	// A nil reporter (a directly-constructed adapter) must be a no-op, not a panic.
	require.NotPanics(t, func() {
		newWorkerMetricsAdapter(nil, nil, nil).ReportContentExcluded("e", "i")
	})
}
