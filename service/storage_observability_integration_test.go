//go:build integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_StorageObservability_CollectPublishConsume drives the WHOLE
// production wire of this service against a real server: the constructor the
// services config block reaches, the account enumeration, the catalog owner
// seam acquiring the report bucket, the KV publication, the Watch that feeds
// the operator surfaces, and finally the two surfaces themselves.
//
// The detour through KV is the reason this test exists in this shape. Metrics
// and health are CONSUMERS of the published report, so a test that asserted on
// the collector's in-memory inventory would prove nothing about what an
// operator actually scrapes.
func TestIntegration_StorageObservability_CollectPublishConsume(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	js, err := client.JetStream()
	require.NoError(t, err)

	// Created BEFORE the service exists, and never touched through it, so the
	// inventory is genuinely account-scoped rather than a view of this
	// process's own handles.
	const bounded = "STORAGE_OBS_INTEGRATION_BOUNDED"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     bounded,
		Subjects: []string{"storage.obs.integration.bounded.>"},
		Storage:  jetstream.FileStorage,
		MaxBytes: 16 << 20,
	})
	require.NoError(t, err)

	const unbounded = "STORAGE_OBS_INTEGRATION_UNBOUNDED"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     unbounded,
		Subjects: []string{"storage.obs.integration.unbounded.>"},
		Storage:  jetstream.MemoryStorage,
	})
	require.NoError(t, err)

	registry := metric.NewMetricsRegistry()
	svc, err := NewStorageObservabilityService(
		json.RawMessage(`{"interval":"1s","timeout":"1s"}`),
		&Dependencies{NATSClient: client, MetricsRegistry: registry},
	)
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	require.NoError(t, svc.Start(runCtx))
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	storage, ok := svc.(*StorageObservabilityService)
	require.True(t, ok)

	// The whole chain has to complete before anything is assertable: collect,
	// publish, watch, apply.
	//
	// The wait includes the report bucket's OWN backing stream, and that is not
	// padding. The bucket is acquired lazily, on the first publication, so it
	// does not exist while the first collection enumerates the account — the
	// observer enters its own inventory on the SECOND collection. Waiting only
	// for the pre-existing streams would make every assertion below race that.
	reportStream := natsclient.KVStreamPrefix + graph.BucketStorageReport
	require.Eventually(t, func() bool {
		snapshot := storage.Snapshot()
		if !snapshot.Synced || !snapshot.AccountKnown {
			return false
		}
		var sawBounded, sawSelf bool
		for _, row := range snapshot.Resources {
			switch row.Resource.Name {
			case bounded:
				sawBounded = true
			case reportStream:
				sawSelf = true
			}
		}
		return sawBounded && sawSelf
	}, 30*time.Second, 100*time.Millisecond,
		"the report must be collected, published, and consumed back")

	snapshot := storage.Snapshot()

	t.Run("the account resource appears with its real capacity", func(t *testing.T) {
		var row natsclient.ResourceReport
		for _, candidate := range snapshot.Resources {
			if candidate.Resource.Name == bounded {
				row = candidate
			}
		}
		require.Equal(t, bounded, row.Resource.Name)
		require.Equal(t, natsclient.CapacityBounded, row.Resource.Bytes.State)
		limit, has := row.Resource.Bytes.Limit()
		require.True(t, has)
		assert.Equal(t, int64(16<<20), limit)
		assert.True(t, row.Pressure.Evaluated)
		assert.Equal(t, natsclient.PressureNormal, row.Pressure.State)
	})

	t.Run("the report bucket observes itself through the catalog seam", func(t *testing.T) {
		// The bucket the service acquired IS the one the catalog declares, and
		// it appears in the inventory it publishes. Self-observation is honest:
		// the observer is a real storage resource.
		var found bool
		for _, row := range snapshot.Resources {
			if row.Resource.Name != reportStream {
				continue
			}
			found = true
			assert.Equal(t, natsclient.AttributionAttributed, row.Resource.Attribution)
			declared, ok := graph.SpecFor(graph.BucketStorageReport)
			require.True(t, ok)
			assert.Equal(t, declared.Owner, row.Resource.Owner)
		}
		assert.True(t, found, "the report bucket's own backing stream must be inventoried")
	})

	t.Run("an unbounded account limit is not-applicable, never satisfied", func(t *testing.T) {
		// testcontainers reports -1 for both tiers, so this is the DEFAULT
		// path rather than an edge case (task 4.6).
		for _, tier := range []natsclient.StorageTier{natsclient.TierFile, natsclient.TierMemory} {
			comparison, ok := snapshot.Account.TierFor(tier)
			require.True(t, ok, "tier %s must appear in the account report", tier)
			require.Equal(t, natsclient.CapacityUnbounded, comparison.Limit.State,
				"a stock server reports no account limit")
			assert.Equal(t, natsclient.OvercommitmentNotApplicable, comparison.State)
			assert.NotEqual(t, natsclient.OvercommitmentWithin, comparison.State)
			assert.Equal(t, natsclient.OvercommitmentUnavailableUnboundedLimit, comparison.Unavailable)
		}

		// And the declared sums are still per tier, never merged.
		file, _ := snapshot.Account.TierFor(natsclient.TierFile)
		memory, _ := snapshot.Account.TierFor(natsclient.TierMemory)
		assert.GreaterOrEqual(t, file.DeclaredBytes, int64(16<<20),
			"the bounded file stream's declaration is in the file tier")
		assert.Equal(t, 0, memory.BoundedResources,
			"the memory stream declared no bound, so it is counted rather than summed")
		assert.GreaterOrEqual(t, memory.UnboundedResources, 1)
	})

	t.Run("prometheus carries the published rows", func(t *testing.T) {
		labels := map[string]string{"resource": bounded, "tier": "file", "kind": "stream"}
		require.Eventually(t, func() bool {
			_, ok := gaugeValue(t, registry, "semstreams_storage_resource_used_bytes", labels)
			return ok
		}, 10*time.Second, 100*time.Millisecond)

		assert.InDelta(t, float64(16<<20),
			requireGauge(t, registry, "semstreams_storage_resource_limit_bytes", labels), 1e-9)
		assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_resource_pressure", labels), 1e-9)

		// The unbounded stream publishes usage and NO pressure or limit.
		unboundedLabels := map[string]string{"resource": unbounded}
		_, ok := gaugeValue(t, registry, "semstreams_storage_resource_used_bytes", unboundedLabels)
		assert.True(t, ok)
		_, ok = gaugeValue(t, registry, "semstreams_storage_resource_pressure", unboundedLabels)
		assert.False(t, ok, "an unbounded resource carries no pressure state")

		// Task 4.6 on the metrics surface, against a real server's answer.
		assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_limit_state",
			map[string]string{"tier": "file", "state": "unbounded"}), 1e-9)
		assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
			map[string]string{"tier": "file", "state": "not-applicable"}), 1e-9)
		assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
			map[string]string{"tier": "file", "state": "within-limit"}), 1e-9)
		_, ok = gaugeValue(t, registry, "semstreams_storage_account_limit_bytes",
			map[string]string{"tier": "file"})
		assert.False(t, ok, "no limit number exists, so no limit series is published")
	})

	t.Run("the HTTP route serves the published rows and recomputes nothing", func(t *testing.T) {
		// Mounted at the prefix PRODUCTION computes, so the route this asserts
		// on is the route an operator reaches (task 4.4).
		manager := NewServiceManager(NewServiceRegistry())
		prefix := "/" + manager.serviceNameToPrefix(StorageObservabilityServiceName)
		require.Equal(t, "/storage-observability", prefix)

		mux := http.NewServeMux()
		storage.RegisterHTTPHandlers(prefix, mux)

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, prefix+"/report", nil))
		require.Equal(t, http.StatusOK, recorder.Code)

		var response StorageReportResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.ReportOnly)
		assert.True(t, response.Synced, "the route serves the CONSUMED bucket, not a fresh collection")
		require.NotNil(t, response.Account, "the published account row reaches the route")
		require.NotNil(t, response.UpdatedAt)

		rows := make(map[string]natsclient.ResourceReport, len(response.Resources))
		for _, row := range response.Resources {
			rows[row.Resource.Name] = row
		}

		boundedRow, ok := rows[bounded]
		require.True(t, ok, "the pre-existing bounded stream is served over HTTP")
		limit, has := boundedRow.Resource.Bytes.Limit()
		require.True(t, has)
		assert.Equal(t, int64(16<<20), limit)
		assert.True(t, boundedRow.Pressure.Evaluated)

		// Task 4.7 against a real server: the unbounded stream is NAMED, and it
		// is never represented as having headroom.
		unboundedRow, ok := rows[unbounded]
		require.True(t, ok, "an unbounded resource must stay visible, not be filtered out")
		assert.Equal(t, natsclient.CapacityUnbounded, unboundedRow.Resource.Bytes.State)
		assert.False(t, unboundedRow.Pressure.Evaluated, "no band has an input, so there is no state")
		assert.Nil(t, unboundedRow.Projection.HeadroomBytes)
		assert.Nil(t, unboundedRow.Projection.HeadroomFraction)
		assert.Nil(t, unboundedRow.Projection.TimeToThreshold)
		assert.GreaterOrEqual(t, response.Summary.NotEvaluated, 1,
			"rows carrying no pressure state are counted rather than folded into normal")

		// The two operator surfaces cannot disagree, on real data. Compared on
		// facts that do not move between the two reads — a configured bound and
		// whether a series exists at all — rather than on live byte counts,
		// which the next collection legitimately advances.
		assert.InDelta(t, float64(limit),
			requireGauge(t, registry, "semstreams_storage_resource_limit_bytes",
				map[string]string{"resource": bounded}), 1e-9)
		assert.InDelta(t, float64(pressureSeverityValue(boundedRow.Pressure.State)),
			requireGauge(t, registry, "semstreams_storage_resource_pressure",
				map[string]string{"resource": bounded}), 1e-9)
		_, hasUnboundedPressure := gaugeValue(t, registry, "semstreams_storage_resource_pressure",
			map[string]string{"resource": unbounded})
		assert.Equal(t, unboundedRow.Pressure.Evaluated, hasUnboundedPressure,
			"a pressure series exists exactly when the served row was evaluated")
	})

	t.Run("health reports the picture and gates nothing", func(t *testing.T) {
		// Driven explicitly rather than waited on: BaseService runs its FIRST
		// health check 200ms after Start, so every service reports unhealthy in
		// that window. That is shared lifecycle behaviour, not a storage
		// verdict, and a sleep here would only make the distinction invisible.
		storage.BaseService.performHealthCheck()

		status := storage.Health()
		assert.True(t, status.Healthy)
		assert.Contains(t, status.Message, "storage pressure (report-only)")
		assert.Contains(t, status.Message, "collected")

		manager := NewServiceManager(NewServiceRegistry())
		manager.RegisterInstance(StorageObservabilityServiceName, storage)

		ready := httptest.NewRecorder()
		manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assert.Equal(t, http.StatusOK, ready.Code)

		systemHealth := httptest.NewRecorder()
		manager.handleSystemHealth(systemHealth, httptest.NewRequest(http.MethodGet, "/health", nil))
		assert.Equal(t, http.StatusOK, systemHealth.Code)

		var payload health.Status
		require.NoError(t, json.Unmarshal(systemHealth.Body.Bytes(), &payload))
		assert.True(t, payload.IsHealthy())
	})

	t.Run("a growth rate appears once two observations exist", func(t *testing.T) {
		// The rate is Delta-bytes over Delta-t across SUCCESSIVE published
		// observations, and MinGrowthSampleInterval is 5s, so this is the first
		// assertion in the file that must wait on real elapsed time rather than
		// on a state transition. The window is generous because the assertion is
		// "a rate eventually exists", not "a rate exists by a deadline".
		require.Eventually(t, func() bool {
			for _, row := range storage.Snapshot().Resources {
				if row.Resource.Name == bounded && row.Growth.State == natsclient.GrowthKnown {
					return true
				}
			}
			return false
		}, 45*time.Second, 500*time.Millisecond,
			"successive collections must produce a measurable rate")

		labels := map[string]string{"resource": bounded}
		_, ok := gaugeValue(t, registry, "semstreams_storage_resource_growth_bytes_per_second", labels)
		assert.True(t, ok, "a measured rate reaches the metrics surface")
	})
}

// TestIntegration_StorageObservability_BootsWithoutAReachableBucket is the
// resourceless-deploy guarantee. A deployment that declares this service must
// start even when the report bucket cannot be acquired: a monitoring surface
// that can take down the system it monitors is a worse bug than the blindness
// it fixes.
func TestIntegration_StorageObservability_BootsWithoutAReachableBucket(t *testing.T) {
	ctx := context.Background()
	// No JetStream at all: neither the account listing nor the bucket
	// acquisition can succeed.
	tc := natsclient.NewTestClient(t)

	svc, err := NewStorageObservabilityService(
		json.RawMessage(`{"interval":"1s","timeout":"1s"}`),
		&Dependencies{NATSClient: tc.Client, MetricsRegistry: metric.NewMetricsRegistry()},
	)
	require.NoError(t, err, "construction must not depend on account state")

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	require.NoError(t, svc.Start(runCtx), "Start must not depend on account state")

	storage, ok := svc.(*StorageObservabilityService)
	require.True(t, ok)

	// Give the loops several intervals to fail and retry, then confirm the
	// service is still up and still honest about what it does not know.
	time.Sleep(3 * time.Second)

	status := storage.Health()
	assert.True(t, status.Healthy, "an unreadable account must not make the service unhealthy")
	assert.False(t, storage.Snapshot().Synced, "nothing was read, and nothing claims otherwise")

	require.NoError(t, svc.Stop(context.Background()))
}
