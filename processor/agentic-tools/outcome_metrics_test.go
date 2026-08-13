package agentictools

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcomeMetricVocabularyIsExactAndBounded(t *testing.T) {
	m := newToolsMetrics()
	for _, path := range []outcomePath{outcomePathNew, outcomePathReplay, outcomePathRejection, outcomePathCompact} {
		m.recordOutcome(path)
	}
	for _, operation := range []outcomeStoreOperation{storeOperationGet, storeOperationCreate, storeOperationReadWinner} {
		for _, reason := range []outcomeStoreFailureReason{storeReasonTransport, storeReasonOversize, storeReasonCorrupt} {
			m.recordStoreFailure(operation, reason)
		}
	}
	m.recordCollision()
	for _, reason := range []resultPublishFailureReason{publishReasonTransport, publishReasonOversize, publishReasonMarshal} {
		m.recordPublishFailure(reason)
	}
	for _, cause := range []ambiguousRedeliveryCause{
		ambiguousCauseStoreFailure, ambiguousCauseShutdown, ambiguousCauseHeartbeat, ambiguousCausePanic,
	} {
		m.recordAmbiguous(cause)
	}

	registry := prometheus.NewRegistry()
	for _, collector := range []prometheus.Collector{
		m.outcomeTotal, m.outcomeStoreFailures, m.outcomeCollisions, m.resultPublishFailures, m.ambiguousRedeliveries,
	} {
		require.NoError(t, registry.Register(collector))
	}
	families, err := registry.Gather()
	require.NoError(t, err)
	got := make(map[string][]string)
	for _, family := range families {
		for _, metric := range family.Metric {
			labels := make([]string, 0, len(metric.Label))
			for _, pair := range metric.Label {
				labels = append(labels, pair.GetName()+"="+pair.GetValue())
			}
			sort.Strings(labels)
			got[family.GetName()] = append(got[family.GetName()], labels...)
		}
		sort.Strings(got[family.GetName()])
	}
	assert.Equal(t, []string{"path=compact", "path=new", "path=rejection", "path=replay"},
		got["semstreams_agentic_tools_outcome_total"])
	assert.Equal(t, []string{
		"operation=create", "operation=create", "operation=create",
		"operation=get", "operation=get", "operation=get",
		"operation=read_winner", "operation=read_winner", "operation=read_winner",
		"reason=corrupt", "reason=corrupt", "reason=corrupt",
		"reason=oversize", "reason=oversize", "reason=oversize",
		"reason=transport", "reason=transport", "reason=transport",
	}, got["semstreams_agentic_tools_outcome_store_failures_total"])
	assert.Empty(t, got["semstreams_agentic_tools_outcome_collisions_total"])
	assert.Equal(t, []string{"reason=marshal", "reason=oversize", "reason=transport"},
		got["semstreams_agentic_tools_result_publish_failures_total"])
	assert.Equal(t, []string{"cause=heartbeat", "cause=panic", "cause=shutdown", "cause=store_failure"},
		got["semstreams_agentic_tools_ambiguous_redeliveries_total"])
}

func TestAmbiguousShutdownAndHeartbeatTelemetry(t *testing.T) {
	metrics := newToolsMetrics()
	var logs bytes.Buffer
	component := &Component{metrics: metrics, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	component.recordHandlerError(context.Background(), errors.Join(natsclient.ErrHeartbeatFailed, errors.New("lost")))
	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	component.recordHandlerError(shutdownCtx, context.Canceled)
	assert.Equal(t, float64(1), testutil.ToFloat64(
		metrics.ambiguousRedeliveries.WithLabelValues(string(ambiguousCauseHeartbeat))))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		metrics.ambiguousRedeliveries.WithLabelValues(string(ambiguousCauseShutdown))))
	assert.Equal(t, 2, bytes.Count(logs.Bytes(), []byte(`"ambiguous_effect":true`)))
}
