package graphclustering

import (
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type incompleteDetector struct {
	err error
}

func (d incompleteDetector) DetectCommunities(context.Context) (map[int][]*clustering.Community, error) {
	return nil, d.err
}

func (incompleteDetector) UpdateCommunities(context.Context, []string) error { return nil }

func (incompleteDetector) GetCommunity(context.Context, string) (*clustering.Community, error) {
	return nil, nil
}

func (incompleteDetector) GetEntityCommunity(context.Context, string, int) (*clustering.Community, error) {
	return nil, nil
}

func (incompleteDetector) GetCommunitiesByLevel(context.Context, int) ([]*clustering.Community, error) {
	return nil, nil
}

func (incompleteDetector) InferRelationshipsFromCommunities(
	context.Context,
	int,
	clustering.InferenceConfig,
) ([]clustering.InferredTriple, error) {
	return nil, nil
}

type countingGraphProvider struct {
	reads atomic.Int64
}

func (p *countingGraphProvider) GetAllEntityIDs(context.Context) ([]string, error) {
	p.reads.Add(1)
	return nil, nil
}

func (p *countingGraphProvider) GetNeighbors(context.Context, string, string) ([]string, error) {
	p.reads.Add(1)
	return nil, nil
}

func (p *countingGraphProvider) GetEdgeWeight(context.Context, string, string) (float64, error) {
	p.reads.Add(1)
	return 0, nil
}

// TestRunCommunityDetection_IncompleteCandidateHasNoSuccessAccounting proves
// the component-level half of #855. DetectCommunities' classified error is the
// boundary: processed/activity/duration/completion and the dependent
// structural/anomaly pass are all success-only effects after that boundary.
func TestRunCommunityDetection_IncompleteCandidateHasNoSuccessAccounting(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "test_detection_duration_seconds"})
	provider := &countingGraphProvider{}
	baselineActivity := time.Unix(1, 0)

	c := &Component{
		logger: logger,
		detector: incompleteDetector{err: errs.WrapInvalid(
			nats.ErrMaxPayload,
			"LPADetector",
			"DetectCommunities",
			"candidate incomplete",
		)},
		metrics:             &clusteringMetrics{detectionDuration: duration},
		graphProvider:       provider,
		anomalyOrchestrator: &inference.Orchestrator{},
	}
	c.lastActivity.Store(baselineActivity)

	c.runCommunityDetection(context.Background())

	assert.Zero(t, atomic.LoadInt64(&c.messagesProcessed),
		"an incomplete candidate must not increment processed communities")
	assert.Equal(t, baselineActivity, c.lastActivity.Load(),
		"an incomplete candidate must not update activity state")

	metric := &dto.Metric{}
	require.NoError(t, duration.Write(metric))
	require.NotNil(t, metric.Histogram)
	assert.Zero(t, metric.Histogram.GetSampleCount(),
		"an incomplete candidate must not record a completed-run duration")

	assert.Zero(t, provider.reads.Load(),
		"an incomplete candidate must not enter structural or anomaly processing")
	assert.NotContains(t, logs.String(), "community detection complete",
		"an incomplete candidate must not emit the complete-success log")
	assert.Contains(t, logs.String(), "detection failed",
		"the classified incomplete result must remain loudly observable")
}
