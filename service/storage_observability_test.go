package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

// --- helpers -----------------------------------------------------------------

func resourceRow(name string, tier natsclient.StorageTier) natsclient.ResourceReport {
	return natsclient.ResourceReport{
		Resource: natsclient.StorageResource{
			Name:        name,
			Kind:        natsclient.ResourceOrdinaryStream,
			Attribution: natsclient.AttributionNotApplicable,
			Tier:        tier,
			Bytes:       natsclient.NewCapacity(1000, 100, true),
			Messages:    natsclient.NewCapacity(0, 1, true),
		},
		CollectedAt: time.Now().UTC(),
		ProducedBy:  "unit-test",
		Growth:      natsclient.UnknownGrowth(natsclient.GrowthUnavailableNoPriorObservation),
	}
}

func withPressure(row natsclient.ResourceReport, state natsclient.PressureState) natsclient.ResourceReport {
	headroomBytes := int64(900)
	headroomFraction := 0.9
	row.Projection = natsclient.Projection{
		HeadroomBytes:    &headroomBytes,
		HeadroomFraction: &headroomFraction,
	}
	row.Pressure = natsclient.Pressure{
		Evaluated:    true,
		State:        state,
		RaisedBy:     natsclient.PressureInputHeadroom,
		FromHeadroom: state,
	}
	return row
}

func withGrowth(row natsclient.ResourceReport, bytesPerSecond float64, timeToThreshold time.Duration) natsclient.ResourceReport {
	rate := bytesPerSecond
	remaining := timeToThreshold
	row.Growth = natsclient.Growth{State: natsclient.GrowthKnown, BytesPerSecond: &rate}
	row.Projection.TimeToThreshold = &remaining
	return row
}

func unboundedRow(name string, tier natsclient.StorageTier) natsclient.ResourceReport {
	row := resourceRow(name, tier)
	row.Resource.Bytes = natsclient.NewCapacity(0, 4096, true)
	row.Projection = natsclient.Projection{
		HeadroomUnavailable:        natsclient.ProjectionUnavailableUnbounded,
		TimeToThresholdUnavailable: natsclient.ProjectionUnavailableUnbounded,
	}
	row.Pressure = natsclient.Pressure{Unavailable: natsclient.PressureUnavailableUnbounded}
	return row
}

func gaugeValue(t *testing.T, registry *metric.MetricsRegistry, name string, labels map[string]string) (float64, bool) {
	t.Helper()
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, m := range family.GetMetric() {
			matched := len(labels) > 0 || len(m.GetLabel()) == 0
			for key, want := range labels {
				found := false
				for _, label := range m.GetLabel() {
					if label.GetName() == key && label.GetValue() == want {
						found = true
						break
					}
				}
				if !found {
					matched = false
					break
				}
			}
			if matched {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func requireGauge(t *testing.T, registry *metric.MetricsRegistry, name string, labels map[string]string) float64 {
	t.Helper()
	value, ok := gaugeValue(t, registry, name, labels)
	require.True(t, ok, "metric %s%v must be published", name, labels)
	return value
}

// staticSnapshot renders a fixed report, so health rendering is exercised on
// the states that matter without standing up an account to produce them.
func staticSnapshot(snapshot natsclient.StorageReportSnapshot) func() natsclient.StorageReportSnapshot {
	return func() natsclient.StorageReportSnapshot { return snapshot }
}

func newTestStorageMetrics(t *testing.T) (*storageObservabilityMetrics, *metric.MetricsRegistry) {
	t.Helper()
	registry := metric.NewMetricsRegistry()
	metrics := newStorageObservabilityMetrics()
	require.NoError(t, metrics.register(registry))
	return metrics, registry
}

// --- configuration (task 2.4b) ----------------------------------------------

// TestStorageObservabilityConfig_JSONRoundTrip is this project's stated bar for
// an operator surface. Before this service existed StoragePressureThresholds
// carried json tags that nothing embedded, so neither the collection interval
// nor a single threshold was reachable from a config file.
func TestStorageObservabilityConfig_JSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"interval": "30s",
		"timeout": "9s",
		"pressure_thresholds": {
			"warning_headroom": 0.4,
			"high_headroom": 0.3,
			"critical_headroom": 0.2,
			"warning_horizon": "48h",
			"high_horizon": "12h",
			"critical_horizon": "1h"
		}
	}`)

	var decoded StorageObservabilityConfig
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, "30s", decoded.Interval)
	assert.Equal(t, "9s", decoded.Timeout)
	assert.InDelta(t, 0.4, decoded.PressureThresholds.WarningHeadroom, 1e-9)
	assert.InDelta(t, 0.3, decoded.PressureThresholds.HighHeadroom, 1e-9)
	assert.InDelta(t, 0.2, decoded.PressureThresholds.CriticalHeadroom, 1e-9)
	assert.Equal(t, "48h", decoded.PressureThresholds.WarningHorizon)
	assert.Equal(t, "12h", decoded.PressureThresholds.HighHorizon)
	assert.Equal(t, "1h", decoded.PressureThresholds.CriticalHorizon)

	// Re-encode and decode again: a field that survives one direction but not
	// the other is still unreachable to the operator who edits it back.
	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)

	var again StorageObservabilityConfig
	require.NoError(t, json.Unmarshal(encoded, &again))
	assert.Equal(t, decoded, again)

	// And the operator's numbers survive into the applied form, which is what
	// the pressure evaluation actually reads.
	resolved, err := again.PressureThresholds.Resolve()
	require.NoError(t, err)
	assert.InDelta(t, 0.4, resolved.WarningHeadroom, 1e-9)
	assert.Equal(t, 48*time.Hour, resolved.WarningHorizon)
	assert.Equal(t, time.Hour, resolved.CriticalHorizon)
}

func TestStorageObservabilityConfig_OmittedFieldsTakeDocumentedDefaults(t *testing.T) {
	var cfg StorageObservabilityConfig
	require.NoError(t, json.Unmarshal([]byte(`{}`), &cfg))

	applied, err := cfg.applied()
	require.NoError(t, err)
	assert.Equal(t, natsclient.DefaultStorageInventoryInterval, applied.interval)
	assert.Equal(t, natsclient.DefaultStorageInventoryTimeout, applied.timeout)

	resolved, err := applied.thresholds.Resolve()
	require.NoError(t, err)
	assert.InDelta(t, natsclient.DefaultWarningHeadroom, resolved.WarningHeadroom, 1e-9)
	assert.Equal(t, natsclient.DefaultCriticalHorizon, resolved.CriticalHorizon)
}

func TestStorageObservabilityConfig_RejectsUnusableOperatorInput(t *testing.T) {
	cases := map[string]string{
		"unparseable interval":  `{"interval":"soon"}`,
		"non-positive interval": `{"interval":"0s"}`,
		"unparseable timeout":   `{"timeout":"later"}`,
		"non-positive timeout":  `{"timeout":"-1s"}`,
		"inverted headroom":     `{"pressure_thresholds":{"warning_headroom":0.1,"high_headroom":0.5}}`,
		"headroom out of range": `{"pressure_thresholds":{"warning_headroom":1.5}}`,
		"unparseable horizon":   `{"pressure_thresholds":{"warning_horizon":"soon"}}`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var cfg StorageObservabilityConfig
			require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
			_, err := cfg.applied()
			require.Error(t, err, "an unusable operator value must be a loud config error, not a silent default")
		})
	}
}

// TestStorageObservabilityConfig_TimeoutMustFitTheInterval catches the
// configuration that quietly serializes collections on top of each other.
func TestStorageObservabilityConfig_TimeoutMustFitTheInterval(t *testing.T) {
	var cfg StorageObservabilityConfig
	require.NoError(t, json.Unmarshal([]byte(`{"interval":"5s","timeout":"30s"}`), &cfg))
	_, err := cfg.applied()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

// TestStorageObservabilityService_IsRegistered keeps the service reachable from
// the services config block, which is the only way an operator turns it on.
func TestStorageObservabilityService_IsRegistered(t *testing.T) {
	registry := NewServiceRegistry()
	require.NoError(t, RegisterAll(registry))

	constructor, ok := registry.Constructor(StorageObservabilityServiceName)
	require.True(t, ok, "the service must be registered under its config-block name")
	require.NotNil(t, constructor)
}

// TestStorageObservabilityService_RequiresNATS fails closed rather than running
// a storage collector that can never read an account.
func TestStorageObservabilityService_RequiresNATS(t *testing.T) {
	_, err := NewStorageObservabilityService(nil, &Dependencies{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NATS")
}

// --- health (task 4.3) -------------------------------------------------------

// TestStorageObservabilityHealth_CriticalPressureFailsNoGate is the report-only
// guarantee stated where it would break first. Readiness reads Status() and
// IsHealthy(); /health aggregates Health(). None of the three may move because
// a resource is under pressure.
func TestStorageObservabilityHealth_CriticalPressureFailsNoGate(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced: true,
		Resources: []natsclient.ResourceReport{
			withPressure(resourceRow("DOOMED", natsclient.TierFile), natsclient.PressureCritical),
			withPressure(resourceRow("FINE", natsclient.TierFile), natsclient.PressureNormal),
		},
		PressureCounts: map[natsclient.PressureState]int{
			natsclient.PressureCritical: 1,
			natsclient.PressureNormal:   1,
		},
		WorstPressure: natsclient.PressureCritical,
	})}

	base := health.NewHealthy(StorageObservabilityServiceName, "Service operating normally")
	status := surface.describe(base)

	assert.True(t, status.Healthy, "pressure is report-only; it cannot make a service unhealthy")
	assert.Equal(t, "healthy", status.Status)
	assert.False(t, status.IsDegraded())
	assert.False(t, status.IsUnhealthy())

	// And the same status aggregated the way /health aggregates it.
	system := health.Aggregate("system", []health.Status{status})
	assert.True(t, system.IsHealthy(), "a critical resource must not degrade system health")

	assert.Contains(t, status.Message, "critical=1", "the state is still visible in health STATUS")
	assert.Contains(t, status.Message, "report-only")
}

// TestStorageObservabilityReadiness_IsUnmovedByCriticalPressure drives the
// PRODUCTION gate handlers rather than asserting on the value they read.
// /readyz reads Status() and IsHealthy(); /health aggregates Health(). A
// report-only guarantee that held for the struct but not for the endpoints an
// orchestrator polls would not be a guarantee at all.
func TestStorageObservabilityReadiness_IsUnmovedByCriticalPressure(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced: true,
		Resources: []natsclient.ResourceReport{
			withPressure(resourceRow("DOOMED", natsclient.TierFile), natsclient.PressureCritical),
		},
		PressureCounts: map[natsclient.PressureState]int{natsclient.PressureCritical: 1},
		WorstPressure:  natsclient.PressureCritical,
	})}

	svc := &StorageObservabilityService{
		BaseService: NewBaseServiceWithOptions(StorageObservabilityServiceName, nil),
		metrics:     newStorageObservabilityMetrics(),
		surface:     surface,
		logger:      slog.Default(),
		stopChan:    make(chan struct{}),
	}
	// BaseService.Start alone: the collection loops need a NATS account and are
	// covered by the integration test. What is under test here is the gate.
	require.NoError(t, svc.BaseService.Start(context.Background()))
	svc.BaseService.performHealthCheck()
	t.Cleanup(func() { _ = svc.BaseService.Stop(time.Second) })

	manager := NewServiceManager(NewServiceRegistry())
	manager.RegisterInstance(StorageObservabilityServiceName, svc)

	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, ready.Code, "critical pressure must not fail readiness")
	assert.Equal(t, "READY", ready.Body.String())

	systemHealth := httptest.NewRecorder()
	manager.handleSystemHealth(systemHealth, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, systemHealth.Code, "critical pressure must not fail /health")

	var payload health.Status
	require.NoError(t, json.Unmarshal(systemHealth.Body.Bytes(), &payload))
	assert.True(t, payload.IsHealthy())
	require.Len(t, payload.SubStatuses, 1)
	assert.Contains(t, payload.SubStatuses[0].Message, "critical=1",
		"the state an operator needs is still in the payload")
}

// TestStorageObservabilityHealth_NeverCarriesANonHealthySubStatus stops the
// gate from re-appearing one level down. health.Aggregate does not currently
// recurse, so a degraded sub-status would be invisible today and become a
// readiness failure the day it does.
func TestStorageObservabilityHealth_NeverCarriesANonHealthySubStatus(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced:         true,
		Resources:      []natsclient.ResourceReport{withPressure(resourceRow("DOOMED", natsclient.TierFile), natsclient.PressureCritical)},
		PressureCounts: map[natsclient.PressureState]int{natsclient.PressureCritical: 1},
		WorstPressure:  natsclient.PressureCritical,
	})}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	for _, sub := range status.SubStatuses {
		assert.True(t, sub.IsHealthy(), "no pressure verdict may be expressed as a health verdict")
	}
}

// TestStorageObservabilityHealth_NamesUnevaluatedResources keeps unbounded and
// unreadable resources from being the ones nobody sees: they carry no pressure
// state at all, so any summary that only counted states would omit them.
func TestStorageObservabilityHealth_NamesUnevaluatedResources(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced: true,
		Resources: []natsclient.ResourceReport{
			unboundedRow("FREE", natsclient.TierFile),
			withPressure(resourceRow("FINE", natsclient.TierFile), natsclient.PressureNormal),
		},
		PressureCounts: map[natsclient.PressureState]int{natsclient.PressureNormal: 1},
		NotEvaluated:   1,
		WorstPressure:  natsclient.PressureNormal,
	})}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	assert.Contains(t, status.Message, "not-evaluated=1")
	assert.True(t, status.Healthy)
}

// TestStorageObservabilityHealth_ReportsWhenNothingHasBeenReadYet keeps an
// unread report from looking like an empty account.
func TestStorageObservabilityHealth_ReportsWhenNothingHasBeenReadYet(t *testing.T) {
	surface := &storageHealthSurface{}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	assert.True(t, status.Healthy, "a report nobody has read yet is not a failure")
	assert.Contains(t, strings.ToLower(status.Message), "not been read")
}

// TestStorageObservabilityHealth_PreservesTheBaseVerdictsReason pins the one
// axis the storage picture must never overwrite. BaseService carries the REASON
// for a non-healthy lifecycle verdict only in Message ("Service is stopped",
// "Service is unhealthy (failed checks: N)"), so replacing it would leave an
// operator reading `status: unhealthy` next to a cheerful pressure line and no
// explanation — this capability's own phantom-signal class, turned on its own
// health report.
func TestStorageObservabilityHealth_PreservesTheBaseVerdictsReason(t *testing.T) {
	surface := &storageHealthSurface{
		snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{Synced: true}),
	}

	base := health.NewUnhealthy(StorageObservabilityServiceName, "Service is stopped")
	status := surface.describe(base)

	assert.Contains(t, status.Message, "Service is stopped",
		"the reason for the verdict must survive the storage picture")
	assert.Contains(t, strings.ToLower(status.Message), "storage pressure",
		"the storage picture is still appended, not dropped")
	assert.False(t, status.Healthy, "describe must not touch the status axis")
}

// TestStorageObservabilityHealth_ReportsCollectionFreshness puts staleness on
// the ANSWER rather than in a decision to withhold it. A readiness gate says
// whether a view is sound to read; how far behind it is belongs on the report.
func TestStorageObservabilityHealth_ReportsCollectionFreshness(t *testing.T) {
	collectedAt := time.Now().Add(-90 * time.Second)
	surface := &storageHealthSurface{
		snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
			Synced:         true,
			PressureCounts: map[natsclient.PressureState]int{},
		}),
		inventoryOf: func() natsclient.StorageInventory {
			return natsclient.StorageInventory{
				ProducedBy:  "collector-1",
				CollectedAt: collectedAt,
			}
		},
	}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	assert.Contains(t, status.Message, "collected 1m30s ago by collector-1")
}

// TestStorageObservabilityHealth_FailedCollectionIsReportedNotGated is the
// other half of the monitoring-safety rule: a collector that cannot read the
// account must say so LOUDLY in its message and still fail nothing. A
// monitoring surface that can take down the system it monitors is a worse bug
// than the blindness it fixes.
func TestStorageObservabilityHealth_FailedCollectionIsReportedNotGated(t *testing.T) {
	failedAt := time.Now()
	surface := &storageHealthSurface{
		snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
			Synced:         true,
			PressureCounts: map[natsclient.PressureState]int{},
		}),
		inventoryOf: func() natsclient.StorageInventory {
			return natsclient.StorageInventory{
				ProducedBy:  "collector-1",
				Stale:       true,
				StaleSince:  &failedAt,
				StaleReason: "jetstream unavailable",
			}
		},
	}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	assert.True(t, status.Healthy, "an unreadable account is a finding, not a verdict")
	assert.Equal(t, "healthy", status.Status)
	assert.Contains(t, status.Message, "last collection did not succeed")
	assert.Contains(t, status.Message, "jetstream unavailable")
	assert.Contains(t, status.Message, "serving last known result")
}

// TestStorageObservabilityHealth_ReportsAnOverCommittedTier surfaces task 4.5's
// finding where an operator already looks.
func TestStorageObservabilityHealth_ReportsAnOverCommittedTier(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced:       true,
		AccountKnown: true,
		Account: natsclient.AccountReport{Tiers: []natsclient.TierComparison{
			{Tier: natsclient.TierFile, State: natsclient.OvercommitmentOver, DeclaredBytes: 100},
			{Tier: natsclient.TierMemory, State: natsclient.OvercommitmentNotApplicable,
				Unavailable: natsclient.OvercommitmentUnavailableUnboundedLimit},
		}},
		PressureCounts: map[natsclient.PressureState]int{},
	})}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "ok"))
	assert.True(t, status.Healthy, "over-commitment is a finding, not a gate")
	assert.Contains(t, status.Message, "file over-committed")
}

// --- metrics (task 4.1) ------------------------------------------------------

func TestStorageMetrics_PublishesPerResourceSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	row := withGrowth(withPressure(resourceRow("LOGS", natsclient.TierFile), natsclient.PressureWarning),
		1024, 90*time.Minute)
	row.Resource.Kind = natsclient.ResourceKeyValue
	row.Resource.Bucket = "ENTITY_STATES"
	row.Resource.Attribution = natsclient.AttributionAttributed
	row.Resource.Owner = "graph-ingest"
	metrics.ObserveResource(row)

	labels := map[string]string{
		"resource": "LOGS", "owner": "graph-ingest", "kind": "kv", "tier": "file",
	}
	assert.InDelta(t, 100, requireGauge(t, registry, "semstreams_storage_resource_used_bytes", labels), 1e-9)
	assert.InDelta(t, 1000, requireGauge(t, registry, "semstreams_storage_resource_limit_bytes", labels), 1e-9)
	assert.InDelta(t, 900, requireGauge(t, registry, "semstreams_storage_resource_headroom_bytes", labels), 1e-9)
	assert.InDelta(t, 0.9, requireGauge(t, registry, "semstreams_storage_resource_headroom_ratio", labels), 1e-9)
	assert.InDelta(t, 1024, requireGauge(t, registry,
		"semstreams_storage_resource_growth_bytes_per_second", labels), 1e-9)
	assert.InDelta(t, 5400, requireGauge(t, registry,
		"semstreams_storage_resource_time_to_threshold_seconds", labels), 1e-9)
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_resource_pressure", labels), 1e-9,
		"warning is severity 1")
}

// TestStorageMetrics_UnattributedOwnerIsNamedNotBlank keeps a bucket that
// escaped the catalog from arriving as an empty label an operator cannot group
// on — and keeps it distinct from a resource kind that has no owner concept.
func TestStorageMetrics_UnattributedOwnerIsNamedNotBlank(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	escaped := withPressure(resourceRow("KV_STRAY", natsclient.TierFile), natsclient.PressureNormal)
	escaped.Resource.Kind = natsclient.ResourceKeyValue
	escaped.Resource.Attribution = natsclient.AttributionUnattributed
	metrics.ObserveResource(escaped)

	ordinary := withPressure(resourceRow("EVENTS", natsclient.TierFile), natsclient.PressureNormal)
	metrics.ObserveResource(ordinary)

	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_used_bytes",
		map[string]string{"resource": "KV_STRAY", "owner": "unattributed"})
	assert.True(t, ok, "an escaped bucket reports its finding state as its owner")

	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_used_bytes",
		map[string]string{"resource": "EVENTS", "owner": "not-applicable"})
	assert.True(t, ok, "a kind with no owner concept is distinct from an escaped bucket")
}

// TestStorageMetrics_AbsentMeasurementsPublishNoSeries is the whole discipline
// of this capability expressed in Prometheus: a zero is a measurement, and a
// resource with no measured rate must not report a rate of zero.
func TestStorageMetrics_AbsentMeasurementsPublishNoSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	// Bounded, evaluated, but the rate has never been measured.
	metrics.ObserveResource(withPressure(resourceRow("NEW", natsclient.TierFile), natsclient.PressureNormal))

	labels := map[string]string{"resource": "NEW"}
	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_growth_bytes_per_second", labels)
	assert.False(t, ok, "an unmeasured rate is absent, never zero")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_time_to_threshold_seconds", labels)
	assert.False(t, ok, "no projection without a rate")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_used_bytes", labels)
	assert.True(t, ok, "what IS measured is still published")
}

// TestStorageMetrics_UnboundedResourceCarriesNoPressureSeries is task 4.6's
// sibling at the resource level: an unbounded resource has no pressure state at
// all, and inventing `normal` for it is the phantom this capability removes.
func TestStorageMetrics_UnboundedResourceCarriesNoPressureSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)
	metrics.ObserveResource(unboundedRow("FREE", natsclient.TierFile))

	labels := map[string]string{"resource": "FREE"}
	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_pressure", labels)
	assert.False(t, ok, "no band has an input, so there is no state to publish")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_limit_bytes", labels)
	assert.False(t, ok, "an unbounded resource has no limit to report")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_headroom_bytes", labels)
	assert.False(t, ok, "and no headroom, because there is nothing to have headroom against")

	// It stays COUNTABLE, which is what keeps it from being the resource nobody
	// can see.
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_resources",
		map[string]string{"tier": "file", "capacity_state": "unbounded"}), 1e-9)
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_pressure_resources",
		map[string]string{"tier": "file", "pressure_state": "not-evaluated"}), 1e-9)
}

// TestStorageMetrics_StateTransitionRetractsTheStaleSeries covers the classic
// Prometheus footgun from the other direction: a resource that stops being
// bounded must stop reporting a limit, not keep the last one forever.
func TestStorageMetrics_StateTransitionRetractsTheStaleSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveResource(withPressure(resourceRow("MUTABLE", natsclient.TierFile), natsclient.PressureHigh))
	require.InDelta(t, 1000, requireGauge(t, registry, "semstreams_storage_resource_limit_bytes",
		map[string]string{"resource": "MUTABLE"}), 1e-9)

	metrics.ObserveResource(unboundedRow("MUTABLE", natsclient.TierFile))

	labels := map[string]string{"resource": "MUTABLE"}
	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_limit_bytes", labels)
	assert.False(t, ok, "the bound is gone; so is the series")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_pressure", labels)
	assert.False(t, ok)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_resources",
		map[string]string{"tier": "file", "capacity_state": "bounded"}), 1e-9)
}

// TestStorageMetrics_OwnerChangeDoesNotStrandTheOldSeries covers a label tuple
// changing under a resource — a bucket added to the descriptor catalog between
// two collections.
func TestStorageMetrics_OwnerChangeDoesNotStrandTheOldSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	before := withPressure(resourceRow("KV_LATE", natsclient.TierFile), natsclient.PressureNormal)
	before.Resource.Kind = natsclient.ResourceKeyValue
	before.Resource.Attribution = natsclient.AttributionUnattributed
	metrics.ObserveResource(before)

	after := before
	after.Resource.Attribution = natsclient.AttributionAttributed
	after.Resource.Owner = "graph-ingest"
	metrics.ObserveResource(after)

	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_used_bytes",
		map[string]string{"resource": "KV_LATE", "owner": "unattributed"})
	assert.False(t, ok, "the resource has one owner at a time; the old label tuple must go")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_used_bytes",
		map[string]string{"resource": "KV_LATE", "owner": "graph-ingest"})
	assert.True(t, ok)
}

// TestStorageMetrics_ForgetRemovesEveryOneOfItsSeries is the reclamation
// interlock: a stream that no longer exists must stop reporting, or its last
// pressure reading pages someone forever.
func TestStorageMetrics_ForgetRemovesEveryOneOfItsSeries(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	row := withGrowth(withPressure(resourceRow("GOES", natsclient.TierFile), natsclient.PressureCritical),
		10, time.Hour)
	metrics.ObserveResource(row)
	require.InDelta(t, 3, requireGauge(t, registry, "semstreams_storage_resource_pressure",
		map[string]string{"resource": "GOES"}), 1e-9)

	metrics.ForgetResource(row)

	for _, name := range []string{
		"semstreams_storage_resource_used_bytes",
		"semstreams_storage_resource_limit_bytes",
		"semstreams_storage_resource_headroom_bytes",
		"semstreams_storage_resource_headroom_ratio",
		"semstreams_storage_resource_growth_bytes_per_second",
		"semstreams_storage_resource_time_to_threshold_seconds",
		"semstreams_storage_resource_pressure",
	} {
		_, ok := gaugeValue(t, registry, name, map[string]string{"resource": "GOES"})
		assert.False(t, ok, "%s must be retracted with the resource", name)
	}
}

// TestStorageMetrics_AggregateCountsAreFullyEmitted keeps the closed enum
// honest: every combination is published on every rebuild, so a state that
// dropped to zero reports zero rather than leaving a stale series behind.
func TestStorageMetrics_AggregateCountsAreFullyEmitted(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveResource(withPressure(resourceRow("A", natsclient.TierFile), natsclient.PressureCritical))
	metrics.ObserveResource(withPressure(resourceRow("B", natsclient.TierMemory), natsclient.PressureNormal))

	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_pressure_resources",
		map[string]string{"tier": "file", "pressure_state": "critical"}), 1e-9)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_pressure_resources",
		map[string]string{"tier": "file", "pressure_state": "normal"}), 1e-9)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_resources",
		map[string]string{"tier": "unknown", "capacity_state": "unknown"}), 1e-9)

	// 3 tiers x 4 pressure states + not-evaluated = 15; 3 tiers x 3 capacity
	// states = 9. Fixed, and that is the point: the categorical axes never
	// multiply by resource count.
	assert.Equal(t, 15, testutil.CollectAndCount(metrics.resourcesByPressure))
	assert.Equal(t, 9, testutil.CollectAndCount(metrics.resourcesByCapacity))
}

// --- account metrics (tasks 4.5 and 4.6) -------------------------------------

func TestStorageAccountMetrics_OverCommittedTier(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	limit := int64(1 << 30)
	used := int64(1 << 20)
	metrics.ObserveAccount(natsclient.AccountReport{Tiers: []natsclient.TierComparison{{
		Tier:             natsclient.TierFile,
		Limit:            natsclient.Capacity{State: natsclient.CapacityBounded, ConfiguredLimit: &limit, Used: &used},
		DeclaredBytes:    2 << 30,
		BoundedResources: 2,
		State:            natsclient.OvercommitmentOver,
	}}})

	file := map[string]string{"tier": "file"}
	assert.InDelta(t, float64(1<<30), requireGauge(t, registry, "semstreams_storage_account_limit_bytes", file), 1e-9)
	assert.InDelta(t, float64(1<<20), requireGauge(t, registry, "semstreams_storage_account_used_bytes", file), 1e-9)
	assert.InDelta(t, float64(2<<30), requireGauge(t, registry,
		"semstreams_storage_account_declared_bytes", file), 1e-9)
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
		map[string]string{"tier": "file", "state": "over-committed"}), 1e-9)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
		map[string]string{"tier": "file", "state": "within-limit"}), 1e-9)
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "file", "state": "bounded"}), 1e-9)
}

// TestStorageAccountMetrics_UnboundedLimitIsNotApplicableNotSatisfied is task
// 4.6 on the metrics surface, and on the DEFAULT path: a stock server reports
// no account limit, so this is what most deployments will scrape.
func TestStorageAccountMetrics_UnboundedLimitIsNotApplicableNotSatisfied(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	used := int64(4096)
	metrics.ObserveAccount(natsclient.AccountReport{Tiers: []natsclient.TierComparison{{
		Tier:          natsclient.TierFile,
		Limit:         natsclient.Capacity{State: natsclient.CapacityUnbounded, Used: &used},
		DeclaredBytes: 1 << 40,
		State:         natsclient.OvercommitmentNotApplicable,
		Unavailable:   natsclient.OvercommitmentUnavailableUnboundedLimit,
	}}})

	_, ok := gaugeValue(t, registry, "semstreams_storage_account_limit_bytes", map[string]string{"tier": "file"})
	assert.False(t, ok, "there is no limit number, so there is no limit series")

	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "file", "state": "unbounded"}), 1e-9,
		"unbounded is published EXPLICITLY rather than as an absent series")
	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
		map[string]string{"tier": "file", "state": "not-applicable"}), 1e-9)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_account_overcommitment",
		map[string]string{"tier": "file", "state": "within-limit"}), 1e-9,
		"an unbounded limit must never read as a comparison that passed")
	assert.InDelta(t, float64(4096), requireGauge(t, registry,
		"semstreams_storage_account_used_bytes", map[string]string{"tier": "file"}), 1e-9,
		"usage is still observable without a bound")
}

func TestStorageAccountMetrics_UnknownLimitIsDistinctFromUnbounded(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveAccount(natsclient.AccountReport{
		LimitsUnavailable: "account info unavailable",
		Tiers: []natsclient.TierComparison{{
			Tier:        natsclient.TierFile,
			Limit:       natsclient.UnknownCapacity(),
			State:       natsclient.OvercommitmentNotApplicable,
			Unavailable: natsclient.OvercommitmentUnavailableUnknownLimit,
		}},
	})

	assert.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "file", "state": "unknown"}), 1e-9)
	assert.InDelta(t, 0, requireGauge(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "file", "state": "unbounded"}), 1e-9,
		"unreadable is not unlimited")
	_, ok := gaugeValue(t, registry, "semstreams_storage_account_used_bytes", map[string]string{"tier": "file"})
	assert.False(t, ok, "an unreadable tier reports no usage either")
}

// TestStorageAccountMetrics_TierThatDisappearsIsRetracted keeps the unknown
// tier — which exists only while something is undescribable — from reporting
// forever once every resource can be read again.
func TestStorageAccountMetrics_TierThatDisappearsIsRetracted(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveAccount(natsclient.AccountReport{Tiers: []natsclient.TierComparison{
		{Tier: natsclient.TierFile, Limit: natsclient.UnknownCapacity(), State: natsclient.OvercommitmentNotApplicable},
		{Tier: natsclient.TierUnknown, Limit: natsclient.UnknownCapacity(), State: natsclient.OvercommitmentNotApplicable},
	}})
	require.InDelta(t, 1, requireGauge(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "unknown", "state": "unknown"}), 1e-9)

	metrics.ObserveAccount(natsclient.AccountReport{Tiers: []natsclient.TierComparison{
		{Tier: natsclient.TierFile, Limit: natsclient.UnknownCapacity(), State: natsclient.OvercommitmentNotApplicable},
	}})

	_, ok := gaugeValue(t, registry, "semstreams_storage_account_limit_state",
		map[string]string{"tier": "unknown", "state": "unknown"})
	assert.False(t, ok, "a tier that is no longer reported stops reporting")
}

// TestStorageMetrics_RegisterIsIdempotent matters because the constructor
// registers through the injected registry AND the Service interface exposes
// RegisterMetrics, so both may run against the same registrar.
func TestStorageMetrics_RegisterIsIdempotent(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)
	require.NoError(t, metrics.register(registry))
	require.NoError(t, metrics.register(registry))
}

// TestStorageMetrics_FreshnessGaugeCarriesCollectionTime pins the one series
// that can report the collector STOPPED. Every other series here is stamped
// with scrape time, so a dead collector's numbers keep arriving looking fresh
// forever; only a gauge whose VALUE is the collection time turns that into
// `time() - gauge > horizon`.
func TestStorageMetrics_FreshnessGaugeCarriesCollectionTime(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	collected := time.Now().UTC().Add(-90 * time.Second)
	row := resourceRow("FRESH", natsclient.TierFile)
	row.CollectedAt = collected
	metrics.ObserveResource(row)

	got := requireGauge(t, registry, "semstreams_storage_report_collected_timestamp_seconds", nil)
	assert.InDelta(t, float64(collected.UnixNano())/float64(time.Second), got, 0.001,
		"the gauge must carry COLLECTION time, not scrape time — that difference is the whole point")
}

// TestStorageMetrics_FreshnessGaugeNeverMovesBackwards is the replay interlock.
// A watch delivers in no guaranteed order and a reconnect can hand back an
// older row after a newer one; letting the gauge regress would manufacture
// staleness that never happened and page someone for a healthy collector.
func TestStorageMetrics_FreshnessGaugeNeverMovesBackwards(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	newest := time.Now().UTC()
	fresh := resourceRow("A", natsclient.TierFile)
	fresh.CollectedAt = newest
	metrics.ObserveResource(fresh)

	stale := resourceRow("B", natsclient.TierFile)
	stale.CollectedAt = newest.Add(-10 * time.Minute)
	metrics.ObserveResource(stale)

	got := requireGauge(t, registry, "semstreams_storage_report_collected_timestamp_seconds", nil)
	assert.InDelta(t, float64(newest.UnixNano())/float64(time.Second), got, 0.001,
		"a replayed older row must not drag freshness backwards")
}

// TestStorageMetrics_FreshnessGaugeIgnoresAZeroTimestamp keeps an unstamped row
// from reading as 1970 — which would render as ~56 years of staleness and fire
// every horizon at once.
func TestStorageMetrics_FreshnessGaugeIgnoresAZeroTimestamp(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	collected := time.Now().UTC()
	row := resourceRow("A", natsclient.TierFile)
	row.CollectedAt = collected
	metrics.ObserveResource(row)

	unstamped := resourceRow("B", natsclient.TierFile)
	unstamped.CollectedAt = time.Time{}
	metrics.ObserveResource(unstamped)

	got := requireGauge(t, registry, "semstreams_storage_report_collected_timestamp_seconds", nil)
	assert.InDelta(t, float64(collected.UnixNano())/float64(time.Second), got, 0.001)
}
