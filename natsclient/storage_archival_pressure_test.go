package natsclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the one guarantee that makes an ARCHIVAL stream safe to
// declare: a stream exempted from finite bounds must not thereby be exempted from
// the surface that warns about capacity. Its bound is gone, so its tier's account
// ceiling is the only ceiling it has, and that is what its pressure is evaluated
// against.
//
// The guarantee is written against the HOLE CLASS rather than the motivating
// instance. Archival is a `config` classification and this package never sees it;
// what it sees is a resource with no bound of its own. Every such resource —
// archival, admitted by a migration override, or created by some other process
// entirely — is covered, which is the only version of this that cannot be walked
// around.

// archivalResource is a resource with NO BOUND OF ITS OWN: the shape an archival
// declaration produces, and the shape a migration override admits.
func archivalResource(name string, used int64) StorageResource {
	return StorageResource{
		Name:        name,
		Kind:        ResourceOrdinaryStream,
		Attribution: AttributionNotApplicable,
		Tier:        TierFile,
		Bytes:       NewCapacity(0, used, true),
		Messages:    NewCapacity(0, 10, true),
	}
}

// fillingFileTier is an account whose FILE tier is bounded and nearly full. Used
// is stated per collection so a rate can be measured across two of them.
func fillingFileTier(storeUsed int64) AccountTierLimits {
	return AccountTierLimits{
		Known:      true,
		MaxStore:   mib(1000),
		StoreUsed:  storeUsed,
		MaxMemory:  -1,
		MemoryUsed: mib(1),
	}
}

// --- the resource-level guarantee --------------------------------------------

// TestArchivalStream_IsEvaluatedAgainstItsAccountTierCeiling is the acceptance
// criterion. Against the pre-existing publisher this row reported
// Evaluated=false with reason "unbounded", which is precisely how declaring a
// stream archival used to remove it from the pressure surface.
func TestArchivalStream_IsEvaluatedAgainstItsAccountTierCeiling(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// Two collections 100s apart: 800 MiB then 900 MiB of a 1000 MiB file tier.
	// That is 10% headroom (high) and ~50s to the critical level (critical), so
	// the tier's own verdict is critical raised by the time projection.
	for i, used := range []int64{mib(800), mib(900)} {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			start.Add(time.Duration(i)*100*time.Second),
			fillingFileTier(used),
			archivalResource("CAMPAIGN_LEDGER", mib(50)),
		))
		require.NoError(t, err)
	}

	row := store.liveRow(t, "CAMPAIGN_LEDGER")

	require.True(t, row.Pressure.Evaluated,
		"an archival stream must not be invisible to the pressure surface; capacity is the only lever left for a stream that can never evict")
	assert.Equal(t, PressureBasisAccountTier, row.Pressure.EvaluatedAgainst,
		"the basis must be published: a state against a shared account ceiling is a different operator conversation from one against a bound this stream could raise")
	assert.Equal(t, PressureCritical, row.Pressure.State)
	assert.Equal(t, PressureInputTimeToThreshold, row.Pressure.RaisedBy,
		"the inherited state carries the input that raised it, which is what distinguishes a capacity problem from a rate problem")
	assert.Empty(t, row.Pressure.Unavailable)

	// The stream's OWN projection stays suppressed. Headroom against a bound it
	// does not have would be fabricated, and the tier's headroom is not that
	// number — it is shared, and it is published on the account row.
	assert.Nil(t, row.Projection.HeadroomBytes,
		"an unbounded resource must not be represented as having capacity headroom of its own")
	assert.Nil(t, row.Projection.HeadroomFraction)
	assert.Equal(t, ProjectionUnavailableUnbounded, row.Projection.HeadroomUnavailable)
}

// TestArchivalStream_SmallnessDoesNotReadAsCalm is the anti-fabrication property,
// and the reason the tier's verdict is inherited UNSCALED.
//
// The archive holds 50 MiB of a 1000 MiB tier. Any per-resource reading — its own
// share of the ceiling, or its own growth rate projected at the ceiling — reports
// it as normal with decades of runway. What is actually true is that the tier it
// cannot be evicted from fills in under a minute, and when it does, this stream is
// the one with no lever left.
func TestArchivalStream_SmallnessDoesNotReadAsCalm(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// The archive itself grows by 1 MiB across the two collections, while the tier
	// around it grows by 100 MiB. Its own rate would project centuries.
	for i, tierUsed := range []int64{mib(800), mib(900)} {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			start.Add(time.Duration(i)*100*time.Second),
			fillingFileTier(tierUsed),
			archivalResource("CAMPAIGN_LEDGER", mib(50)+int64(i)*mib(1)),
		))
		require.NoError(t, err)
	}

	row := store.liveRow(t, "CAMPAIGN_LEDGER")
	assert.Equal(t, PressureCritical, row.Pressure.State,
		"a small archive in a tier about to exhaust is not calm; scaling the tier verdict by this resource's share would manufacture exactly that false calm")

	// Its own measured rate is still published — it is a real fact about this
	// stream, and it is what tells an operator whether THIS archive is what is
	// filling the tier. It simply is not what the state was derived from.
	rate, known := row.Growth.Rate()
	require.True(t, known, "the resource's own rate is still measured and published")
	assert.InDelta(t, float64(mib(1))/100.0, rate, 1.0)
}

// TestArchivalStream_AgreesWithTheAccountRowItInherited pins that one collection
// cannot publish two different opinions about the same tier. The verdict is
// derived once, before any resource row, precisely so this holds.
func TestArchivalStream_AgreesWithTheAccountRowItInherited(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	for i, used := range []int64{mib(800), mib(900)} {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			start.Add(time.Duration(i)*100*time.Second),
			fillingFileTier(used),
			archivalResource("CAMPAIGN_LEDGER", mib(50)),
		))
		require.NoError(t, err)
	}

	row := store.liveRow(t, "CAMPAIGN_LEDGER")
	account := decodeAccountRow(t, store)
	tier, ok := account.TierFor(TierFile)
	require.True(t, ok)

	assert.Equal(t, tier.Pressure.State, row.Pressure.State,
		"the row and the account row must state the same thing about the same tier in the same collection")
	assert.Equal(t, tier.Pressure.RaisedBy, row.Pressure.RaisedBy)
	assert.Equal(t, tier.Pressure.FromHeadroom, row.Pressure.FromHeadroom)
	assert.Equal(t, tier.Pressure.FromTimeToThreshold, row.Pressure.FromTimeToThreshold)
}

// TestBoundedResource_KeepsTheOwnBoundBasis guards the other half of the basis
// field: a resource with a bound of its own is still evaluated against it, and
// says so.
func TestBoundedResource_KeepsTheOwnBoundBasis(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())

	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		fillingFileTier(mib(900)),
		boundedResource("EVENTS", mib(1000), mib(100)),
	))
	require.NoError(t, err)

	row := store.liveRow(t, "EVENTS")
	require.True(t, row.Pressure.Evaluated)
	assert.Equal(t, PressureBasisOwnBound, row.Pressure.EvaluatedAgainst)
	assert.Equal(t, PressureNormal, row.Pressure.State,
		"a resource comfortably within its OWN bound is not dragged to critical by its tier; its bound is a real lever and retention still applies")
	require.NotNil(t, row.Projection.HeadroomBytes,
		"a bounded resource still projects against its own bound")
}

// TestUndescribableResource_IsNotRoutedThroughTheTierCeiling keeps the three
// capacity states from collapsing. A resource the server declined to describe has
// UNKNOWN capacity, not absent capacity: it may well carry a bound nobody can
// read, so inheriting a tier verdict for it would attribute a state to a resource
// whose situation is genuinely unknown.
func TestUndescribableResource_IsNotRoutedThroughTheTierCeiling(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())

	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		fillingFileTier(mib(900)),
		StorageResource{
			Name:        "OFFLINE",
			Kind:        ResourceOrdinaryStream,
			Attribution: AttributionNotApplicable,
			Tier:        TierUnknown,
			Bytes:       UnknownCapacity(),
			Messages:    UnknownCapacity(),
		},
	))
	require.NoError(t, err)

	row := store.liveRow(t, "OFFLINE")
	assert.False(t, row.Pressure.Evaluated)
	assert.Equal(t, PressureUnavailableUnknownCapacity, row.Pressure.Unavailable,
		"an unreadable capacity is not an absent one, and must not borrow a ceiling")
}

// --- the tier ceiling's own capacity picture ---------------------------------

// TestAccountTier_CarriesItsOwnGrowthAndProjection covers the input the
// inheritance needs. It also covers a gap the over-commitment verdict cannot see:
// a tier can be comfortably within-limit on DECLARED bytes while the bytes
// actually stored are about to exhaust it, because declarations are not usage.
func TestAccountTier_CarriesItsOwnGrowthAndProjection(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	for i, used := range []int64{mib(800), mib(900)} {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			start.Add(time.Duration(i)*100*time.Second),
			fillingFileTier(used),
			archivalResource("CAMPAIGN_LEDGER", mib(50)),
		))
		require.NoError(t, err)
	}

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)

	rate, known := tier.Growth.Rate()
	require.True(t, known, "the tier's rate is measurable from the account row's own retained history")
	assert.InDelta(t, float64(mib(100))/100.0, rate, 1.0)
	assert.Equal(t, 100*time.Second, tier.Growth.ObservedOver)

	require.NotNil(t, tier.Projection.HeadroomBytes)
	assert.Equal(t, mib(100), *tier.Projection.HeadroomBytes)
	require.NotNil(t, tier.Projection.TimeToThreshold)
	assert.InDelta(t, 50*time.Second, *tier.Projection.TimeToThreshold, float64(2*time.Second))

	require.True(t, tier.Pressure.Evaluated)
	assert.Equal(t, PressureBasisAccountTier, tier.Pressure.EvaluatedAgainst,
		"a tier's ceiling IS the account limit, so its own state carries that basis")
	assert.Equal(t, PressureCritical, tier.Pressure.State)

	// The over-commitment verdict is untouched and still answers its own
	// question. Only one resource is declared here and it declares nothing, so
	// the declared sum is zero and the tier is trivially within-limit — while its
	// stored bytes are minutes from exhaustion. The two must not be conflated.
	assert.Equal(t, OvercommitmentWithin, tier.State,
		"over-commitment is about declared bounds; it can pass while the tier's real usage is critical")
}

// TestAccountTier_RateSurvivesARestart is the restart property, on the same terms
// the resource rates already had it. The observations live in the published bucket,
// so a fresh process measures a rate on its FIRST publication rather than blanking
// the projection — which matters most in exactly the case where it would be lost, a
// deploy or crash loop.
func TestAccountTier_RateSurvivesARestart(t *testing.T) {
	store := newFakeReportStore(10)
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	first := newTestPublisher(t, store, defaultSource())
	_, err := first.Publish(context.Background(), inventoryWithAccountAt(
		start, fillingFileTier(mib(800)), archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	// A DIFFERENT publisher, with an empty in-process baseline, as after a restart.
	restarted := newTestPublisher(t, store, defaultSource())
	_, err = restarted.Publish(context.Background(), inventoryWithAccountAt(
		start.Add(100*time.Second), fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	rate, known := tier.Growth.Rate()
	require.True(t, known,
		"a restarted process seeds the tier baseline from the account row's history rather than starting blind")
	assert.InDelta(t, float64(mib(100))/100.0, rate, 1.0)

	// And the archive is evaluable on that first post-restart publication, not one
	// collection later.
	row := store.liveRow(t, "CAMPAIGN_LEDGER")
	assert.True(t, row.Pressure.Evaluated)
	assert.Equal(t, PressureCritical, row.Pressure.State)
}

// TestAccountTier_SeedsTheHistoryOnceForEveryTier pins the read cost. The tiers
// share one key, so a per-tier seed would re-read the same history once per tier
// on every publication that needs it.
func TestAccountTier_SeedsTheHistoryOnceForEveryTier(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// Both tiers bounded, so both need a baseline on the same publication.
	limits := AccountTierLimits{
		Known:    true,
		MaxStore: mib(1000), StoreUsed: mib(800),
		MaxMemory: mib(500), MemoryUsed: mib(100),
	}
	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		start, limits, archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	before := store.historyReads(StorageAccountReportKey)
	fresh := newTestPublisher(t, store, defaultSource())
	_, err = fresh.Publish(context.Background(), inventoryWithAccountAt(
		start.Add(100*time.Second), limits, archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	assert.Equal(t, 1, store.historyReads(StorageAccountReportKey)-before,
		"one history read serves every tier that needs a baseline")
}

// --- when the tier offers no ceiling either ----------------------------------

// TestArchivalStream_WhenNoCeilingExistsAnywhere covers the honest limit of this
// feature. On a stock server neither the stream nor its tier is bounded, so there
// is nothing to project — and the reason must say WHICH, because "no ceiling
// exists" and "the ceiling is unreadable" resolve differently.
func TestArchivalStream_WhenNoCeilingExistsAnywhere(t *testing.T) {
	cases := []struct {
		name   string
		limits AccountTierLimits
		want   string
	}{
		{
			name:   "the tier limit is unbounded",
			limits: AccountTierLimits{Known: true, MaxStore: -1, MaxMemory: -1},
			want:   PressureUnavailableUnboundedNoTierCeiling,
		},
		{
			name:   "the account limits could not be read",
			limits: UnknownAccountTierLimits("account info unavailable"),
			want:   PressureUnavailableUnboundedTierUnknown,
		},
		{
			// A zero limit is the ambiguous tiered-JWT case storage_account.go
			// reads as unknown rather than as a ceiling of zero bytes.
			name:   "the tier limit is ambiguous",
			limits: AccountTierLimits{Known: true, MaxStore: 0, MaxMemory: 0},
			want:   PressureUnavailableUnboundedTierUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeReportStore(10)
			publisher := newTestPublisher(t, store, defaultSource())

			_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
				time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC), tc.limits,
				archivalResource("CAMPAIGN_LEDGER", mib(50))))
			require.NoError(t, err)

			row := store.liveRow(t, "CAMPAIGN_LEDGER")
			assert.False(t, row.Pressure.Evaluated)
			assert.Equal(t, tc.want, row.Pressure.Unavailable)
		})
	}
}

// TestUnusableThresholds_SuppressTheTierVerdictToo keeps the tier row on the same
// honesty rule as every other row: no bands means no state, naming the
// configuration, rather than a comparison against numbers no operator chose.
func TestUnusableThresholds_SuppressTheTierVerdictToo(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, func() StoragePressureThresholds {
		return StoragePressureThresholds{WarningHeadroom: 5} // out of range
	})

	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	assert.False(t, tier.Pressure.Evaluated)
	assert.Contains(t, tier.Pressure.Unavailable, "unusable pressure threshold configuration")

	// And the archive does not inherit a state from a tier that has none.
	row := store.liveRow(t, "CAMPAIGN_LEDGER")
	assert.False(t, row.Pressure.Evaluated)
	assert.Contains(t, row.Pressure.Unavailable, "unusable pressure threshold configuration")
}

// --- the inheritance rule in isolation ---------------------------------------

func TestPressureAgainstAccountTier(t *testing.T) {
	evaluated := Pressure{
		Evaluated:        true,
		State:            PressureHigh,
		RaisedBy:         PressureInputHeadroom,
		FromHeadroom:     PressureHigh,
		EvaluatedAgainst: PressureBasisOwnBound,
	}

	t.Run("an evaluated tier verdict is inherited and relabelled", func(t *testing.T) {
		got := PressureAgainstAccountTier(TierComparison{Tier: TierFile, Pressure: evaluated}, true)

		assert.True(t, got.Evaluated)
		assert.Equal(t, PressureHigh, got.State, "the state is the tier's, unscaled")
		assert.Equal(t, PressureInputHeadroom, got.RaisedBy)
		assert.Equal(t, PressureBasisAccountTier, got.EvaluatedAgainst,
			"the basis is restated: from the resource's side this ceiling is the account's, not its own")
	})

	t.Run("a resource filed under no tier reports that", func(t *testing.T) {
		got := PressureAgainstAccountTier(TierComparison{}, false)

		assert.False(t, got.Evaluated)
		assert.Equal(t, PressureUnavailableUnboundedTierUnfiled, got.Unavailable)
	})

	t.Run("an unbounded tier limit means no ceiling anywhere", func(t *testing.T) {
		got := PressureAgainstAccountTier(TierComparison{
			Tier:  TierFile,
			Limit: Capacity{State: CapacityUnbounded},
		}, true)

		assert.False(t, got.Evaluated)
		assert.Equal(t, PressureUnavailableUnboundedNoTierCeiling, got.Unavailable)
	})

	t.Run("an unknown tier limit is a gap, not a clean bill", func(t *testing.T) {
		got := PressureAgainstAccountTier(TierComparison{
			Tier:  TierFile,
			Limit: UnknownCapacity(),
		}, true)

		assert.False(t, got.Evaluated)
		assert.Equal(t, PressureUnavailableUnboundedTierUnknown, got.Unavailable)
	})

	t.Run("a bounded tier whose own state is unavailable reports the tier's reason", func(t *testing.T) {
		limit, used := mib(1000), mib(100)
		got := PressureAgainstAccountTier(TierComparison{
			Tier:     TierFile,
			Limit:    Capacity{State: CapacityBounded, ConfiguredLimit: &limit, Used: &used},
			Pressure: Pressure{Unavailable: "unusable pressure threshold configuration: nope"},
		}, true)

		assert.False(t, got.Evaluated)
		assert.Equal(t, "unusable pressure threshold configuration: nope", got.Unavailable,
			"the resource row must not explain the same failure differently from the account row a consumer reads beside it")
	})
}

func TestPressure_AgainstAccountTier_LeavesAnUnevaluatedStateAlone(t *testing.T) {
	got := Pressure{Unavailable: PressureUnavailableUnknownCapacity}.AgainstAccountTier()

	assert.False(t, got.Evaluated)
	assert.Empty(t, got.EvaluatedAgainst,
		"there is no state to attribute to any ceiling, so attributing one would be a claim about nothing")
	assert.Equal(t, PressureUnavailableUnknownCapacity, got.Unavailable)
}

func TestAssessPressure_StampsTheOwnBoundBasis(t *testing.T) {
	thresholds, err := DefaultStoragePressureThresholds().Resolve()
	require.NoError(t, err)

	capacity := NewCapacity(mib(1000), mib(100), true)
	projection := Project(capacity, UnknownGrowth(GrowthUnavailableNoPriorObservation), thresholds)
	got := AssessPressure(capacity, projection, thresholds)

	require.True(t, got.Evaluated)
	assert.Equal(t, PressureBasisOwnBound, got.EvaluatedAgainst)
}

// TestDeriveAgainstBaseline covers the shared advance rule directly, because both
// series depend on it and the too-close case is the one that wedges a projection
// permanently if it advances.
func TestDeriveAgainstBaseline(t *testing.T) {
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	current := Observation{At: start.Add(time.Minute), Bytes: mib(200)}

	t.Run("a usable pair advances the baseline to the current observation", func(t *testing.T) {
		growth, next, advance := deriveAgainstBaseline(
			current, []Observation{{At: start, Bytes: mib(100)}}, false)

		require.Equal(t, GrowthKnown, growth.State)
		assert.True(t, advance)
		assert.Equal(t, current, next)
	})

	t.Run("no priors at all starts the series here", func(t *testing.T) {
		growth, next, advance := deriveAgainstBaseline(current, nil, true)

		assert.Equal(t, GrowthUnknown, growth.State)
		assert.True(t, advance)
		assert.Equal(t, current, next)
	})

	t.Run("a too-close in-memory baseline is left exactly where it is", func(t *testing.T) {
		// Advancing here is the bug: a process publishing faster than
		// MinGrowthSampleInterval would reset the target every time and report an
		// unknown rate forever.
		tooClose := Observation{At: current.At.Add(-time.Second), Bytes: mib(199)}

		growth, _, advance := deriveAgainstBaseline(current, []Observation{tooClose}, false)

		assert.Equal(t, GrowthUnavailableObservationsTooClose, growth.Unavailable)
		assert.False(t, advance)
	})

	t.Run("a too-close seeded series retains the oldest usable target", func(t *testing.T) {
		oldest := Observation{At: current.At.Add(-2 * time.Second), Bytes: mib(190)}
		newer := Observation{At: current.At.Add(-time.Second), Bytes: mib(199)}

		growth, next, advance := deriveAgainstBaseline(current, []Observation{newer, oldest}, true)

		assert.Equal(t, GrowthUnavailableObservationsTooClose, growth.Unavailable)
		require.True(t, advance)
		assert.Equal(t, oldest, next,
			"the furthest observation becomes measurable soonest and over the longest span")
	})
}

// TestTierGrowth_ADegradedFirstCollectionIsNotAnObservation covers the case where
// no baseline exists yet: a tier whose usage could not be read has nothing to
// difference, so it contributes no observation and the next readable collection
// still reports an unknown rate for want of a PRIOR one — not a rate measured
// against the degraded collection.
func TestTierGrowth_ADegradedFirstCollectionIsNotAnObservation(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// First collection: limits unreadable, so the file tier has no usage.
	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		start, UnknownAccountTierLimits("account info unavailable"),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, GrowthUnavailableUnknownUsage, tier.Growth.Unavailable)

	// Second collection reads them: the rate must be unknown for want of a prior
	// observation, NOT measured against the degraded collection.
	_, err = publisher.Publish(context.Background(), inventoryWithAccountAt(
		start.Add(100*time.Second), fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	tier, ok = decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	_, known := tier.Growth.Rate()
	assert.False(t, known, "a collection with no readable usage is not an observation to measure against")
	assert.Equal(t, GrowthUnavailableNoPriorObservation, tier.Growth.Unavailable)
}

// TestTierGrowth_AnExistingBaselineSurvivesABlindGap pins the OTHER half, which
// the code comment describes and nothing exercised.
//
// A baseline already held is kept across collections whose usage could not be read,
// so the rate is eventually measured ACROSS the gap. That is deliberate: both
// endpoints are genuine account readings, the number is real rather than
// fabricated, and ObservedOver publishes the span so a long average is
// distinguishable from a short one. It also keeps this path consistent with the
// resource path, which retains on the same condition.
//
// The test exists to make the choice explicit — if someone later decides a blind
// gap should reset the series, this is the assertion they have to change on
// purpose rather than a behavior they can flip unnoticed.
func TestTierGrowth_AnExistingBaselineSurvivesABlindGap(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// A real observation establishes the baseline.
	_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		start, fillingFileTier(mib(800)), archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	// Two collections where the account limits could not be read at all.
	for i := 1; i <= 2; i++ {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			start.Add(time.Duration(i)*time.Hour),
			UnknownAccountTierLimits("account info unavailable"),
			archivalResource("CAMPAIGN_LEDGER", mib(50))))
		require.NoError(t, err)

		tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
		require.True(t, ok)
		assert.Equal(t, GrowthUnavailableUnknownUsage, tier.Growth.Unavailable,
			"a degraded collection reports no rate of its own")
	}

	// Readable again: the rate spans the gap, and says so.
	_, err = publisher.Publish(context.Background(), inventoryWithAccountAt(
		start.Add(3*time.Hour), fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))
	require.NoError(t, err)

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	rate, known := tier.Growth.Rate()
	require.True(t, known, "the retained baseline is still a genuine account reading")
	assert.InDelta(t, float64(mib(100))/(3*time.Hour).Seconds(), rate, 1.0)
	assert.Equal(t, 3*time.Hour, tier.Growth.ObservedOver,
		"the span MUST be published, or a three-hour average is indistinguishable from a fresh one")
}

// TestPublishAccount_StillReportsWhenAResourceRowFails guards the ordering the
// inheritance introduced: the tier verdicts are derived before the resource loop,
// so a resource failure must not cost the account row.
func TestPublishAccount_StillReportsWhenAResourceRowFails(t *testing.T) {
	store := newFakeReportStore(10)
	store.putErrForKey = map[string]error{"CAMPAIGN_LEDGER": errors.New("transient")}
	publisher := newTestPublisher(t, store, defaultSource())

	result, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(50))))

	require.Error(t, err, "a lost row is reported rather than silently shrinking the report")
	assert.True(t, result.AccountPublished, "the tier ceilings are still published")
	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	assert.True(t, tier.Pressure.Evaluated)
}

// TestPublishFailure_DoesNotAdvanceTheBaselinePastAnUnpublishedRow is the
// restart-parity property, and it is the one a failed write used to break.
//
// The rate is defined as Δbytes over Δt across successive PUBLISHED observations,
// and the in-memory baseline is a cache of exactly that. If it advanced for a row
// whose Put failed, the running process would measure the next rate against a
// sample that is in no history — while a restarted process, seeding from the
// bucket, measures against the last published one. The projection would then differ
// solely because a process restarted, which is what the published-series design
// exists to prevent.
//
// A=100 MiB publishes, B=110 MiB fails, C=160 MiB publishes. The correct answer is
// C-A over the full span. C-B over the short span is the bug.
func TestPublishFailure_DoesNotAdvanceTheBaselinePastAnUnpublishedRow(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	publish := func(at time.Time, used int64) error {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			at, fillingFileTier(mib(900)), archivalResource("CAMPAIGN_LEDGER", used)))
		return err
	}

	require.NoError(t, publish(start, mib(100)))

	// The middle collection's row write fails. The observation is real; it is just
	// not in the bucket, so it must not become anyone's baseline.
	store.putErrForKey = map[string]error{"CAMPAIGN_LEDGER": errors.New("transient")}
	require.Error(t, publish(start.Add(time.Minute), mib(110)))
	store.putErrForKey = nil

	require.NoError(t, publish(start.Add(2*time.Minute), mib(160)))

	row := store.liveRow(t, "CAMPAIGN_LEDGER")
	rate, known := row.Growth.Rate()
	require.True(t, known)

	// C-A: 60 MiB over 120s. NOT C-B (50 MiB over 60s), which is what advancing
	// past the unpublished row would produce.
	assert.InDelta(t, float64(mib(60))/120.0, rate, 1.0,
		"the rate must be measured against the last PUBLISHED observation, not one whose write failed")
	assert.Equal(t, 2*time.Minute, row.Growth.ObservedOver,
		"and the span must be the published one")

	// A restarted process seeds from the bucket and must reach the same answer.
	// Before the fix these diverged, so the projection depended on restart.
	restarted := newTestPublisher(t, store, defaultSource())
	_, err := restarted.Publish(context.Background(), inventoryWithAccountAt(
		start.Add(3*time.Minute), fillingFileTier(mib(900)),
		archivalResource("CAMPAIGN_LEDGER", mib(190))))
	require.NoError(t, err)

	restartedRow := store.liveRow(t, "CAMPAIGN_LEDGER")
	restartedRate, known := restartedRow.Growth.Rate()
	require.True(t, known)
	assert.InDelta(t, float64(mib(30))/60.0, restartedRate, 1.0,
		"a restarted process measures against the same published series the running one did")
}

// TestAccountRowFailure_DoesNotAdvanceTheTierBaseline is the same property for the
// tier series, whose observations live in the account row's history.
func TestAccountRowFailure_DoesNotAdvanceTheTierBaseline(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	publish := func(at time.Time, tierUsed int64) error {
		_, err := publisher.Publish(context.Background(), inventoryWithAccountAt(
			at, fillingFileTier(tierUsed), archivalResource("CAMPAIGN_LEDGER", mib(50))))
		return err
	}

	require.NoError(t, publish(start, mib(700)))

	store.putErrForKey = map[string]error{StorageAccountReportKey: errors.New("transient")}
	require.Error(t, publish(start.Add(time.Minute), mib(750)))
	store.putErrForKey = nil

	require.NoError(t, publish(start.Add(2*time.Minute), mib(820)))

	tier, ok := decodeAccountRow(t, store).TierFor(TierFile)
	require.True(t, ok)
	rate, known := tier.Growth.Rate()
	require.True(t, known)
	assert.InDelta(t, float64(mib(120))/120.0, rate, 1.0,
		"the tier rate must span from the last PUBLISHED account row, not the one that failed to write")
	assert.Equal(t, 2*time.Minute, tier.Growth.ObservedOver)
}
