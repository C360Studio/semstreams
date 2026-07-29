package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/natsclient"
)

// The tier ceiling's own series exist because it is the ONLY ceiling an unbounded
// resource has. Such a resource inherits the tier's pressure state, so without
// these an operator could see that an archive is critical and have no way to see
// the number that made it critical, or how long they have.

// fillingTier is a bounded tier with a measured rate and a projection.
func fillingTier(tier natsclient.StorageTier, state natsclient.PressureState) natsclient.TierComparison {
	rate := 4096.0
	headroom := int64(1 << 20)
	remaining := 90 * time.Minute
	return natsclient.TierComparison{
		Tier:          tier,
		Limit:         natsclient.NewCapacity(1<<30, 1<<20, true),
		DeclaredBytes: 1 << 24,
		State:         natsclient.OvercommitmentWithin,
		Growth: natsclient.Growth{
			State:          natsclient.GrowthKnown,
			BytesPerSecond: &rate,
			ObservedOver:   30 * time.Minute,
		},
		Projection: natsclient.Projection{
			HeadroomBytes:   &headroom,
			TimeToThreshold: &remaining,
		},
		Pressure: natsclient.Pressure{
			Evaluated:        true,
			State:            state,
			RaisedBy:         natsclient.PressureInputTimeToThreshold,
			EvaluatedAgainst: natsclient.PressureBasisAccountTier,
		},
	}
}

// inheritedPressureRow is a resource with no bound of its own whose pressure came
// from its tier — the published shape of an archival stream.
func inheritedPressureRow(name string, tier natsclient.StorageTier, state natsclient.PressureState) natsclient.ResourceReport {
	row := unboundedRow(name, tier)
	row.Pressure = natsclient.Pressure{
		Evaluated:        true,
		State:            state,
		RaisedBy:         natsclient.PressureInputTimeToThreshold,
		EvaluatedAgainst: natsclient.PressureBasisAccountTier,
	}
	return row
}

func TestAccountTierMetrics_PublishTheCeilingsOwnCapacity(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveAccount(natsclient.AccountReport{
		Tiers: []natsclient.TierComparison{fillingTier(natsclient.TierFile, natsclient.PressureHigh)},
	})

	file := map[string]string{"tier": "file"}
	assert.Equal(t, 4096.0, requireGauge(t, registry, "semstreams_storage_account_growth_bytes_per_second", file))
	assert.Equal(t, float64(1<<20), requireGauge(t, registry, "semstreams_storage_account_headroom_bytes", file))
	assert.Equal(t, (90 * time.Minute).Seconds(),
		requireGauge(t, registry, "semstreams_storage_account_time_to_threshold_seconds", file))

	// The state enum is emitted in FULL, so "high" is a value an operator alerts
	// on rather than the absence of the other series.
	assert.Equal(t, 1.0, requireGauge(t, registry, "semstreams_storage_account_pressure_state",
		map[string]string{"tier": "file", "state": "high"}))
	for _, quiet := range []string{"normal", "warning", "critical", "not-evaluated"} {
		assert.Equal(t, 0.0, requireGauge(t, registry, "semstreams_storage_account_pressure_state",
			map[string]string{"tier": "file", "state": quiet}),
			"state %q must report zero rather than leaving its last count standing", quiet)
	}
}

// TestAccountTierMetrics_AnUnevaluableCeilingIsExplicit keeps the absent-versus-zero
// rule at the tier level. A stock server bounds neither tier, and a zero headroom
// there would read as an exhausted account.
func TestAccountTierMetrics_AnUnevaluableCeilingIsExplicit(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveAccount(natsclient.AccountReport{
		Tiers: []natsclient.TierComparison{{
			Tier:     natsclient.TierFile,
			Limit:    natsclient.Capacity{State: natsclient.CapacityUnbounded},
			State:    natsclient.OvercommitmentNotApplicable,
			Growth:   natsclient.UnknownGrowth(natsclient.GrowthUnavailableUnknownUsage),
			Pressure: natsclient.Pressure{Unavailable: natsclient.PressureUnavailableUnbounded},
		}},
	})

	file := map[string]string{"tier": "file"}
	_, ok := gaugeValue(t, registry, "semstreams_storage_account_headroom_bytes", file)
	assert.False(t, ok, "an unbounded tier has no headroom; a zero would read as exhausted")
	_, ok = gaugeValue(t, registry, "semstreams_storage_account_time_to_threshold_seconds", file)
	assert.False(t, ok, "no ceiling means nothing to project toward")
	_, ok = gaugeValue(t, registry, "semstreams_storage_account_growth_bytes_per_second", file)
	assert.False(t, ok, "an unmeasured rate is absent, never zero")

	assert.Equal(t, 1.0, requireGauge(t, registry, "semstreams_storage_account_pressure_state",
		map[string]string{"tier": "file", "state": "not-evaluated"}))
}

// TestAccountTierMetrics_ATierThatLeavesTheReportStopsReporting extends the
// existing retraction rule to the new series. The unknown tier disappears once
// every resource is describable again, and a frozen last reading would page
// someone forever.
func TestAccountTierMetrics_ATierThatLeavesTheReportStopsReporting(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveAccount(natsclient.AccountReport{
		Tiers: []natsclient.TierComparison{
			fillingTier(natsclient.TierFile, natsclient.PressureHigh),
			fillingTier(natsclient.TierUnknown, natsclient.PressureCritical),
		},
	})
	unknown := map[string]string{"tier": "unknown"}
	require.NotZero(t, requireGauge(t, registry, "semstreams_storage_account_headroom_bytes", unknown))

	metrics.ObserveAccount(natsclient.AccountReport{
		Tiers: []natsclient.TierComparison{fillingTier(natsclient.TierFile, natsclient.PressureHigh)},
	})

	for _, name := range []string{
		"semstreams_storage_account_headroom_bytes",
		"semstreams_storage_account_time_to_threshold_seconds",
		"semstreams_storage_account_growth_bytes_per_second",
	} {
		_, ok := gaugeValue(t, registry, name, unknown)
		assert.False(t, ok, "%s must be retracted for a tier the report no longer carries", name)
	}
	_, ok := gaugeValue(t, registry, "semstreams_storage_account_pressure_state",
		map[string]string{"tier": "unknown", "state": "critical"})
	assert.False(t, ok, "the retracted tier's pressure enum must go with it")
}

// TestInheritedPressure_HealthMessageNamesTheCeilingItCameFrom keeps the health
// message from sending an operator to raise a bound the resource does not have.
// "CAMPAIGN_LEDGER critical (time-to-threshold)" unqualified reads as this
// stream's own limit filling; the fix for an account-tier finding is a different
// one, and it is about every resource in the tier.
func TestInheritedPressure_HealthMessageNamesTheCeilingItCameFrom(t *testing.T) {
	surface := &storageHealthSurface{snapshotOf: staticSnapshot(natsclient.StorageReportSnapshot{
		Synced: true,
		Resources: []natsclient.ResourceReport{
			inheritedPressureRow("CAMPAIGN_LEDGER", natsclient.TierFile, natsclient.PressureCritical),
			withPressure(resourceRow("EVENTS", natsclient.TierFile), natsclient.PressureHigh),
		},
		PressureCounts: map[natsclient.PressureState]int{
			natsclient.PressureCritical: 1,
			natsclient.PressureHigh:     1,
		},
		WorstPressure: natsclient.PressureCritical,
	})}

	status := surface.describe(health.NewHealthy(StorageObservabilityServiceName, "Service operating normally"))

	assert.Contains(t, status.Message, "CAMPAIGN_LEDGER critical (time-to-threshold) vs file account tier")
	assert.Contains(t, status.Message, "EVENTS high (headroom)")
	assert.NotContains(t, status.Message, "EVENTS high (headroom) vs",
		"a resource evaluated against its OWN bound must not be qualified as an account-tier finding")
	assert.True(t, status.Healthy, "pressure remains report-only however it was derived")
}

// TestInheritedPressure_ReachesThePressureGaugeWithoutFabricatingHeadroom is the
// metric-level statement of the archival guarantee. The severity gauge is what
// every pressure alert reads, so the row has to reach it — while the headroom
// series must stay absent, because the resource still has no bound of its own.
func TestInheritedPressure_ReachesThePressureGaugeWithoutFabricatingHeadroom(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	metrics.ObserveResource(inheritedPressureRow("CAMPAIGN_LEDGER", natsclient.TierFile, natsclient.PressureCritical))

	labels := map[string]string{"resource": "CAMPAIGN_LEDGER", "tier": "file"}
	assert.Equal(t, 3.0, requireGauge(t, registry, "semstreams_storage_resource_pressure", labels),
		"an archival stream must reach the gauge every pressure alert reads")

	_, ok := gaugeValue(t, registry, "semstreams_storage_resource_headroom_bytes", labels)
	assert.False(t, ok, "the tier's headroom is shared and is not this resource's; publishing it here would be a fabricated per-resource number")
	_, ok = gaugeValue(t, registry, "semstreams_storage_resource_limit_bytes", labels)
	assert.False(t, ok, "an unbounded resource publishes no limit")

	// And it counts under its tier's band rather than under not-evaluated, which
	// is what keeps a dashboard from showing archives as uncovered.
	assert.Equal(t, 1.0, requireGauge(t, registry, "semstreams_storage_pressure_resources",
		map[string]string{"tier": "file", "pressure_state": "critical"}))
	assert.Equal(t, 0.0, requireGauge(t, registry, "semstreams_storage_pressure_resources",
		map[string]string{"tier": "file", "pressure_state": "not-evaluated"}))
}
