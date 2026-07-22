package graphembedding

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestWorkerMetricsAdapter_NewSignalsHaveRealConsumers proves the dedup_skipped and
// text_truncated metrics are not phantom registrations: the production
// WorkerMetrics consumer (workerMetricsAdapter) forwards the worker's calls to
// registered Prometheus counters that actually move. The worker-side consumer is
// asserted separately in graph/embedding; this closes the prometheus half (#623/#602).
func TestWorkerMetricsAdapter_NewSignalsHaveRealConsumers(t *testing.T) {
	m := getMetrics(nil)
	adapter := newWorkerMetricsAdapter(m)

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
}
