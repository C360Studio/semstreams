package natsclient

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownGrowth is a measured rate for tests that care about what a rate DOES
// rather than how it was derived.
func knownGrowth(bytesPerSecond float64) Growth {
	rate := bytesPerSecond
	return Growth{State: GrowthKnown, BytesPerSecond: &rate, ObservedOver: time.Minute}
}

func defaultResolved(t *testing.T) ResolvedPressureThresholds {
	t.Helper()
	resolved, err := DefaultStoragePressureThresholds().Resolve()
	require.NoError(t, err)
	return resolved
}

// TestProject_SuppressedForEveryNonBoundedCapacity is the projection half of
// "unknown capacity MUST report as unknown": there is no headroom to have
// against an unreadable bound, and none against an absent one either.
//
// The two suppression reasons stay distinct because the states do: an
// unreadable limit is a finding an operator can act on (the server declined to
// describe the resource), while a deliberately unlimited one is a declaration.
// Collapsing them would re-merge the states the capacity model exists to keep
// apart.
func TestProject_SuppressedForEveryNonBoundedCapacity(t *testing.T) {
	thresholds := defaultResolved(t)
	growing := knownGrowth(1024)

	unknown := Project(UnknownCapacity(), growing, thresholds)
	assert.Nil(t, unknown.HeadroomBytes, "an unreadable bound projects no headroom")
	assert.Nil(t, unknown.HeadroomFraction)
	assert.Nil(t, unknown.TimeToThreshold, "an unreadable bound projects no exhaustion")
	assert.Nil(t, unknown.ThresholdBytes)
	assert.Equal(t, ProjectionUnavailableUnknownCapacity, unknown.HeadroomUnavailable)
	assert.Equal(t, ProjectionUnavailableUnknownCapacity, unknown.TimeToThresholdUnavailable)

	unbounded := Project(NewCapacity(0, mib(900), true), growing, thresholds)
	assert.Nil(t, unbounded.HeadroomBytes, "an unbounded resource is never represented as having headroom")
	assert.Nil(t, unbounded.TimeToThreshold)
	assert.Equal(t, ProjectionUnavailableUnbounded, unbounded.HeadroomUnavailable)
	assert.Equal(t, ProjectionUnavailableUnbounded, unbounded.TimeToThresholdUnavailable)

	assert.NotEqual(t, unknown.HeadroomUnavailable, unbounded.HeadroomUnavailable,
		"unreadable and deliberately unlimited are different findings")
}

// TestProject_HeadroomAndTimeAreSuppressedIndEPENDENTLY: headroom needs only a
// bound, while a time projection additionally needs a measured, positive rate.
// A resource with a known bound and an unknown rate still reports its headroom
// — suppressing both together would throw away the half that IS knowable.
func TestProject_HeadroomAndTimeAreSuppressedIndependently(t *testing.T) {
	thresholds := defaultResolved(t)
	capacity := NewCapacity(mib(1000), mib(400), true)

	t.Run("unknown rate keeps headroom and drops the time projection", func(t *testing.T) {
		projection := Project(capacity, UnknownGrowth(GrowthUnavailableNoPriorObservation), thresholds)

		require.NotNil(t, projection.HeadroomBytes)
		assert.Equal(t, mib(600), *projection.HeadroomBytes)
		require.NotNil(t, projection.HeadroomFraction)
		assert.InDelta(t, 0.6, *projection.HeadroomFraction, 0.0001)
		assert.Empty(t, projection.HeadroomUnavailable)

		assert.Nil(t, projection.TimeToThreshold)
		assert.Equal(t, ProjectionUnavailableUnknownGrowth, projection.TimeToThresholdUnavailable)
	})

	t.Run("a resource that is not growing projects no exhaustion", func(t *testing.T) {
		// The spec's churn scenario, at the projection layer: zero growth is a
		// MEASURED fact, and it means exhaustion is never reached — which is a
		// different report from "we could not measure the rate".
		projection := Project(capacity, knownGrowth(0), thresholds)

		require.NotNil(t, projection.HeadroomBytes)
		assert.Nil(t, projection.TimeToThreshold)
		assert.Equal(t, ProjectionUnavailableNotGrowing, projection.TimeToThresholdUnavailable)
		assert.NotEqual(t, ProjectionUnavailableUnknownGrowth, projection.TimeToThresholdUnavailable)
	})

	t.Run("a shrinking resource projects no exhaustion either", func(t *testing.T) {
		projection := Project(capacity, knownGrowth(-2048), thresholds)
		assert.Nil(t, projection.TimeToThreshold)
		assert.Equal(t, ProjectionUnavailableNotGrowing, projection.TimeToThresholdUnavailable)
	})
}

// TestProject_TimeToThresholdTargetsTheCriticalHeadroomLevel pins WHAT is being
// projected to. "Time to threshold" is the time until the resource crosses the
// configured critical-headroom level, not the time until the last byte fits:
// an operator needs to add capacity before the resource is full, and the
// critical level is where this capability already says that is true.
func TestProject_TimeToThresholdTargetsTheCriticalHeadroomLevel(t *testing.T) {
	thresholds := defaultResolved(t) // critical headroom 5% -> threshold at 95%
	limit := int64(1000)
	used := int64(150)

	projection := Project(NewCapacity(limit, used, true), knownGrowth(1), thresholds)

	require.NotNil(t, projection.ThresholdBytes)
	assert.Equal(t, int64(950), *projection.ThresholdBytes,
		"the projection targets the critical-headroom level, not the limit")
	require.NotNil(t, projection.TimeToThreshold)
	assert.Equal(t, 800*time.Second, *projection.TimeToThreshold,
		"(950 - 150) bytes at 1 B/s")
}

// TestProject_UsagePastTheThresholdProjectsZero: a resource already over the
// critical level has no lead time left. Zero is the honest answer; a negative
// duration would read as a time in the future to a naive consumer.
func TestProject_UsagePastTheThresholdProjectsZero(t *testing.T) {
	projection := Project(NewCapacity(1000, 990, true), knownGrowth(1), defaultResolved(t))

	require.NotNil(t, projection.TimeToThreshold)
	assert.Equal(t, time.Duration(0), *projection.TimeToThreshold)
	require.NotNil(t, projection.HeadroomBytes)
	assert.Equal(t, int64(10), *projection.HeadroomBytes)
}

// TestAssessPressure_Transitions walks the pressure bands over both inputs.
//
// The load-bearing case is "a rate raises pressure before headroom does":
// proportional headroom alone MISRANKS, because a 2 TiB resource at 40% filling
// in an hour is more urgent than a 1 GiB resource at 85% static for a month.
// Taking the worse of the two bands is what surfaces the first one, and naming
// the input is what tells an operator whether they have a capacity problem or a
// rate problem.
func TestAssessPressure_Transitions(t *testing.T) {
	thresholds := defaultResolved(t)
	const limit = int64(1_000_000)

	// used yields the given proportional headroom.
	used := func(headroom float64) int64 { return int64(float64(limit) * (1 - headroom)) }
	// rateReaching yields the rate that reaches the 95% threshold in exactly d.
	rateReaching := func(u int64, d time.Duration) float64 {
		return float64(int64(0.95*float64(limit))-u) / d.Seconds()
	}

	tests := []struct {
		name         string
		headroom     float64
		growth       Growth
		wantState    PressureState
		wantRaisedBy PressureInput
		// wantFromHeadroom and wantFromTime are the individual bands, published
		// alongside the winner so an operator can see the input that did not
		// win. An empty wantFromTime means no projection was available to band.
		wantFromHeadroom PressureState
		wantFromTime     PressureState
	}{
		{
			name:             "roomy and static is normal",
			headroom:         0.80,
			growth:           knownGrowth(0),
			wantState:        PressureNormal,
			wantRaisedBy:     PressureInputNone,
			wantFromHeadroom: PressureNormal,
		},
		{
			// Both bands present, both normal. The healthy path is the common
			// one, so "nothing raised this" has to be stated rather than
			// implied: a state of normal attributed to an input is incoherent,
			// and an alert rule keying on RaisedBy would fire on every calm
			// resource in the account.
			name:             "a distant projection leaves a roomy resource normal, raised by nothing",
			headroom:         0.80,
			growth:           knownGrowth(rateReaching(used(0.80), 1000*time.Hour)),
			wantState:        PressureNormal,
			wantRaisedBy:     PressureInputNone,
			wantFromHeadroom: PressureNormal,
			wantFromTime:     PressureNormal,
		},
		{
			name:             "headroom inside the warning band warns",
			headroom:         0.20,
			growth:           knownGrowth(0),
			wantState:        PressureWarning,
			wantRaisedBy:     PressureInputHeadroom,
			wantFromHeadroom: PressureWarning,
		},
		{
			name:             "headroom inside the high band is high",
			headroom:         0.10,
			growth:           knownGrowth(0),
			wantState:        PressureHigh,
			wantRaisedBy:     PressureInputHeadroom,
			wantFromHeadroom: PressureHigh,
		},
		{
			name:             "headroom inside the critical band is critical",
			headroom:         0.02,
			growth:           knownGrowth(0),
			wantState:        PressureCritical,
			wantRaisedBy:     PressureInputHeadroom,
			wantFromHeadroom: PressureCritical,
		},
		{
			// The spec scenario: substantial headroom, but the projection lands
			// inside a configured horizon.
			name:             "a projection inside the warning horizon raises pressure the headroom band would not",
			headroom:         0.60,
			growth:           knownGrowth(rateReaching(used(0.60), 48*time.Hour)),
			wantState:        PressureWarning,
			wantRaisedBy:     PressureInputTimeToThreshold,
			wantFromHeadroom: PressureNormal,
			wantFromTime:     PressureWarning,
		},
		{
			name:             "a projection inside the high horizon outranks a normal headroom",
			headroom:         0.60,
			growth:           knownGrowth(rateReaching(used(0.60), 12*time.Hour)),
			wantState:        PressureHigh,
			wantRaisedBy:     PressureInputTimeToThreshold,
			wantFromHeadroom: PressureNormal,
			wantFromTime:     PressureHigh,
		},
		{
			name:             "a projection inside the critical horizon is critical however roomy the resource",
			headroom:         0.90,
			growth:           knownGrowth(rateReaching(used(0.90), time.Hour)),
			wantState:        PressureCritical,
			wantRaisedBy:     PressureInputTimeToThreshold,
			wantFromHeadroom: PressureNormal,
			wantFromTime:     PressureCritical,
		},
		{
			// The worse of the two, not the first of the two. Both bands are
			// genuinely present and genuinely disagree here: 10% free is high,
			// while the rate puts the threshold two days out, which is only a
			// warning.
			//
			// Note what is NOT expressible. Headroom at or below the critical
			// fraction means usage is already past the level the projection
			// targets, so the time band is necessarily critical too — critical
			// headroom and a calmer projection cannot coexist by construction.
			// Written at 0.02 first, this case passed for the wrong reason: the
			// implied rate was NEGATIVE, so there was no time band at all.
			name:             "the worse input wins when they disagree",
			headroom:         0.10,
			growth:           knownGrowth(rateReaching(used(0.10), 48*time.Hour)),
			wantState:        PressureHigh,
			wantRaisedBy:     PressureInputHeadroom,
			wantFromHeadroom: PressureHigh,
			wantFromTime:     PressureWarning,
		},
		{
			name:             "both inputs at the same band are reported as both",
			headroom:         0.20,
			growth:           knownGrowth(rateReaching(used(0.20), 48*time.Hour)),
			wantState:        PressureWarning,
			wantRaisedBy:     PressureInputBoth,
			wantFromHeadroom: PressureWarning,
			wantFromTime:     PressureWarning,
		},
		{
			name:             "an unmeasured rate cannot contribute a band",
			headroom:         0.20,
			growth:           UnknownGrowth(GrowthUnavailableNoPriorObservation),
			wantState:        PressureWarning,
			wantRaisedBy:     PressureInputHeadroom,
			wantFromHeadroom: PressureWarning,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capacity := NewCapacity(limit, used(tc.headroom), true)
			projection := Project(capacity, tc.growth, thresholds)
			pressure := AssessPressure(capacity, projection, thresholds)

			require.True(t, pressure.Evaluated, "a bounded resource always has a pressure state")
			assert.Equal(t, tc.wantState, pressure.State)
			assert.Equal(t, tc.wantRaisedBy, pressure.RaisedBy)
			assert.Empty(t, pressure.Unavailable)

			// Both bands are published, not just the winner: an operator
			// reading "high, raised by the projection" still needs to know
			// what the headroom band said.
			assert.Equal(t, tc.wantFromHeadroom, pressure.FromHeadroom)
			assert.Equal(t, tc.wantFromTime, pressure.FromTimeToThreshold,
				"an unavailable projection contributes no band at all, not a normal one")
		})
	}
}

// TestAssessPressure_NoStateWithoutAKnownBound is the spec's "a resource with
// unknown capacity has no pressure state" scenario, plus the unbounded case the
// same reasoning covers.
//
// Reporting `normal` for either would be the phantom-signal class this program
// exists to delete: a resource called healthy because nothing could be measured
// manufactures exactly the confidence an operator should not have. The reasons
// stay distinct so "the server would not describe this" never reads the same as
// "this resource declares no bound".
func TestAssessPressure_NoStateWithoutAKnownBound(t *testing.T) {
	thresholds := defaultResolved(t)

	for _, tc := range []struct {
		name       string
		capacity   Capacity
		wantReason string
	}{
		{
			name:       "unknown capacity",
			capacity:   UnknownCapacity(),
			wantReason: PressureUnavailableUnknownCapacity,
		},
		{
			name:       "unbounded capacity",
			capacity:   NewCapacity(0, mib(4096), true),
			wantReason: PressureUnavailableUnbounded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projection := Project(tc.capacity, knownGrowth(1<<20), thresholds)
			pressure := AssessPressure(tc.capacity, projection, thresholds)

			assert.False(t, pressure.Evaluated)
			assert.Equal(t, PressureState(""), pressure.State,
				"no state at all — not normal, which would read as healthy")
			assert.Equal(t, tc.wantReason, pressure.Unavailable)
			assert.Equal(t, PressureInput(""), pressure.RaisedBy)
		})
	}

	assert.NotEqual(t, PressureUnavailableUnknownCapacity, PressureUnavailableUnbounded)
}

// TestStoragePressureThresholds_Defaults pins the shipped defaults and the
// ordering invariant they satisfy, so a later edit to one number cannot quietly
// invert a band.
func TestStoragePressureThresholds_Defaults(t *testing.T) {
	resolved := defaultResolved(t)

	assert.InDelta(t, 0.25, resolved.WarningHeadroom, 0.0001)
	assert.InDelta(t, 0.15, resolved.HighHeadroom, 0.0001)
	assert.InDelta(t, 0.05, resolved.CriticalHeadroom, 0.0001)
	assert.Equal(t, 72*time.Hour, resolved.WarningHorizon)
	assert.Equal(t, 24*time.Hour, resolved.HighHorizon)
	assert.Equal(t, 4*time.Hour, resolved.CriticalHorizon)

	assert.Greater(t, resolved.WarningHeadroom, resolved.HighHeadroom)
	assert.Greater(t, resolved.HighHeadroom, resolved.CriticalHeadroom)
	assert.Greater(t, resolved.WarningHorizon, resolved.HighHorizon)
	assert.Greater(t, resolved.HighHorizon, resolved.CriticalHorizon)
}

// TestStoragePressureThresholds_ResolveRejectsIncoherentConfiguration: an
// operator edit that cannot be applied must FAIL rather than silently fall back
// to a default, because a silently-defaulted threshold applies a number the
// operator did not choose and looks identical to a working edit.
func TestStoragePressureThresholds_ResolveRejectsIncoherentConfiguration(t *testing.T) {
	valid := DefaultStoragePressureThresholds()

	tests := []struct {
		name   string
		mutate func(*StoragePressureThresholds)
	}{
		{"headroom above one", func(c *StoragePressureThresholds) { c.WarningHeadroom = 1.5 }},
		{"negative headroom", func(c *StoragePressureThresholds) { c.CriticalHeadroom = -0.1 }},
		{"critical headroom above high", func(c *StoragePressureThresholds) { c.CriticalHeadroom = 0.5 }},
		{"high headroom above warning", func(c *StoragePressureThresholds) { c.HighHeadroom = 0.9 }},
		{"unparseable horizon", func(c *StoragePressureThresholds) { c.WarningHorizon = "soon" }},
		{"non-positive horizon", func(c *StoragePressureThresholds) { c.CriticalHorizon = "0s" }},
		{"critical horizon beyond high", func(c *StoragePressureThresholds) { c.CriticalHorizon = "48h" }},
		{"high horizon beyond warning", func(c *StoragePressureThresholds) { c.HighHorizon = "500h" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			_, err := cfg.Resolve()
			require.Error(t, err, "an incoherent threshold set must not resolve")
		})
	}
}

// TestStoragePressureThresholds_JSONRoundTrip is the operator-surface proof
// (task 3.5): the thresholds are a JSON configuration an operator edits, so
// every field must survive the encode/decode the config loader performs. A
// field that marshals but does not unmarshal reads back as the default and
// applies a number the operator did not choose, successfully and silently.
func TestStoragePressureThresholds_JSONRoundTrip(t *testing.T) {
	original := StoragePressureThresholds{
		WarningHeadroom:  0.40,
		HighHeadroom:     0.30,
		CriticalHeadroom: 0.10,
		WarningHorizon:   "96h",
		HighHorizon:      "36h",
		CriticalHorizon:  "90m",
	}

	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded StoragePressureThresholds
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, original, decoded, "every operator-authored field survives the round trip")

	// The decoded form must resolve to the SAME applied values as the original:
	// a round trip that loses a duration string would resolve to a default here.
	fromOriginal, err := original.Resolve()
	require.NoError(t, err)
	fromDecoded, err := decoded.Resolve()
	require.NoError(t, err)
	assert.Equal(t, fromOriginal, fromDecoded)
	assert.Equal(t, 90*time.Minute, fromDecoded.CriticalHorizon)

	// A partially specified configuration keeps its operator-authored fields and
	// defaults only the ones it omits.
	var partial StoragePressureThresholds
	require.NoError(t, json.Unmarshal([]byte(`{"critical_headroom":0.01}`), &partial))
	resolved, err := partial.Resolve()
	require.NoError(t, err)
	assert.InDelta(t, 0.01, resolved.CriticalHeadroom, 0.0001, "the authored field is applied")
	assert.InDelta(t, 0.25, resolved.WarningHeadroom, 0.0001, "an omitted field takes the default")
	assert.Equal(t, 4*time.Hour, resolved.CriticalHorizon)
}

// TestProject_AGlacialRateDoesNotOverflowIntoCritical is a false-critical the
// arithmetic produces on its own.
//
// A resource that grew one byte over a long observation window has a positive
// rate of about 1e-5 B/s. Projecting a gigabyte of remaining headroom at that
// rate is roughly three million years, which does not fit in a time.Duration —
// the float-to-int64 conversion saturates NEGATIVE, and a naive "never report a
// negative duration" clamp turns it into ZERO. Zero time-to-threshold is
// critical pressure, so the calmest resource in the account would page someone.
//
// The honest answer is the largest representable duration: "not in any horizon
// you configured".
//
// NOTE FOR ANYONE RUNNING THIS LOCALLY: on darwin/arm64 this test is a no-op.
// ARM's float-to-int conversion saturates, so even the buggy version returns
// MaxInt64 and the test passes. It bites only where the conversion wraps —
// linux/amd64, which is CI and production. A local green here is not evidence;
// the amd64 run is.
func TestProject_AGlacialRateDoesNotOverflowIntoCritical(t *testing.T) {
	thresholds := defaultResolved(t)
	capacity := NewCapacity(2<<30, 1<<30, true) // half of 2 GiB used

	projection := Project(capacity, knownGrowth(0.00001), thresholds)

	require.NotNil(t, projection.TimeToThreshold)
	assert.Positive(t, *projection.TimeToThreshold,
		"a glacial rate projects a very long time, never a zero one")
	assert.Greater(t, *projection.TimeToThreshold, 100*365*24*time.Hour,
		"a rate this small cannot exhaust anything on a horizon an operator cares about")

	pressure := AssessPressure(capacity, projection, thresholds)
	require.True(t, pressure.Evaluated)
	assert.Equal(t, PressureNormal, pressure.State,
		"the projection must not raise pressure for a resource that is barely moving")
	assert.Equal(t, PressureInputNone, pressure.RaisedBy,
		"and nothing raised it — a normal state attributed to an input is incoherent")
}

// TestPressureBands_AreInclusiveAtTheirThreshold pins the boundary semantics
// the band comparisons claim: a threshold an operator SETS is the point they
// want to hear about, so a resource sitting exactly on it is already in that
// band rather than one step below it.
//
// The bands are driven directly here because an end-to-end boundary is not
// reproducible through floating-point capacity arithmetic — `int64(limit *
// (1 - 0.15))` truncates to one byte off the threshold, which lands in the
// neighbouring band and would test nothing. Both comparison sets get an exact
// on-threshold case, one strictly inside, and one strictly outside.
func TestPressureBands_AreInclusiveAtTheirThreshold(t *testing.T) {
	thresholds := defaultResolved(t)

	t.Run("headroom", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			fraction float64
			want     PressureState
		}{
			{"exactly the warning threshold", thresholds.WarningHeadroom, PressureWarning},
			{"a hair above the warning threshold", thresholds.WarningHeadroom + 0.001, PressureNormal},
			{"exactly the high threshold", thresholds.HighHeadroom, PressureHigh},
			{"a hair above the high threshold", thresholds.HighHeadroom + 0.001, PressureWarning},
			{"exactly the critical threshold", thresholds.CriticalHeadroom, PressureCritical},
			{"a hair above the critical threshold", thresholds.CriticalHeadroom + 0.001, PressureHigh},
			{"no headroom at all", 0, PressureCritical},
			{"over the bound", -0.1, PressureCritical},
		} {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, headroomBandFor(tc.fraction, thresholds))
			})
		}
	})

	t.Run("time to threshold", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			remaining time.Duration
			want      PressureState
		}{
			{"exactly the warning horizon", thresholds.WarningHorizon, PressureWarning},
			{"a second beyond the warning horizon", thresholds.WarningHorizon + time.Second, PressureNormal},
			{"exactly the high horizon", thresholds.HighHorizon, PressureHigh},
			{"a second beyond the high horizon", thresholds.HighHorizon + time.Second, PressureWarning},
			{"exactly the critical horizon", thresholds.CriticalHorizon, PressureCritical},
			{"a second beyond the critical horizon", thresholds.CriticalHorizon + time.Second, PressureHigh},
			{"no time left", 0, PressureCritical},
		} {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, horizonBandFor(tc.remaining, thresholds))
			})
		}
	})
}

// TestAssessPressure_OnTheHeadroomThresholdExactly is the same inclusivity
// through the production path, using the one capacity where the arithmetic is
// exact: 250 bytes free of 1000 is the warning threshold to the bit.
func TestAssessPressure_OnTheHeadroomThresholdExactly(t *testing.T) {
	thresholds := defaultResolved(t)
	capacity := NewCapacity(1000, 750, true)

	projection := Project(capacity, UnknownGrowth(GrowthUnavailableNoPriorObservation), thresholds)
	require.NotNil(t, projection.HeadroomFraction)
	require.Equal(t, thresholds.WarningHeadroom, *projection.HeadroomFraction,
		"test premise: this capacity sits exactly on the threshold")

	pressure := AssessPressure(capacity, projection, thresholds)
	assert.Equal(t, PressureWarning, pressure.State,
		"a resource exactly on a configured threshold is in that band, not below it")
	assert.Equal(t, PressureInputHeadroom, pressure.RaisedBy)
}

// TestProject_ANonFiniteRateIsNotAnExhaustedResource: Project is exported and
// takes an arbitrary Growth, so a caller can hand it a rate that is not a
// finite positive number. Each kind resolves in its own direction, and only one
// of them is allowed to look urgent.
func TestProject_ANonFiniteRateIsNotAnExhaustedResource(t *testing.T) {
	thresholds := defaultResolved(t)
	capacity := NewCapacity(2<<30, 1<<30, true)

	t.Run("a NaN rate is uncomputable, not exhausted", func(t *testing.T) {
		// NaN fails every ordered comparison, so it slips past the
		// rate <= 0 guard and reaches the conversion. Reading it as zero time
		// to threshold would page someone about a resource nobody measured.
		projection := Project(capacity, knownGrowth(math.NaN()), thresholds)
		require.NotNil(t, projection.TimeToThreshold)
		assert.Greater(t, *projection.TimeToThreshold, 100*365*24*time.Hour,
			"an uncomputable projection saturates upward, never to zero")
		assert.Equal(t, PressureNormal, AssessPressure(capacity, projection, thresholds).State)
	})

	t.Run("a negative or negative-zero rate is not growing", func(t *testing.T) {
		for _, rate := range []float64{math.Copysign(0, -1), math.Inf(-1)} {
			projection := Project(capacity, knownGrowth(rate), thresholds)
			assert.Nil(t, projection.TimeToThreshold)
			assert.Equal(t, ProjectionUnavailableNotGrowing, projection.TimeToThresholdUnavailable)
		}
	})

	t.Run("an infinite rate really is exhausted now", func(t *testing.T) {
		// The one direction that SHOULD look urgent: something growing
		// infinitely fast reaches the threshold immediately. Pinned so the
		// upward saturation above is never widened into "any odd rate is fine".
		projection := Project(capacity, knownGrowth(math.Inf(1)), thresholds)
		require.NotNil(t, projection.TimeToThreshold)
		assert.Equal(t, time.Duration(0), *projection.TimeToThreshold)
		assert.Equal(t, PressureCritical, AssessPressure(capacity, projection, thresholds).State)
	})

	t.Run("the conversion saturates upward on every non-finite input", func(t *testing.T) {
		assert.Equal(t, time.Duration(math.MaxInt64), projectedDuration(math.NaN()))
		assert.Equal(t, time.Duration(math.MaxInt64), projectedDuration(math.Inf(1)))
		assert.Equal(t, time.Duration(math.MaxInt64), projectedDuration(math.Inf(-1)),
			"an infinitely distant threshold is never reached, which is not the same as reached now")
		assert.Equal(t, time.Duration(math.MaxInt64), projectedDuration(maxProjectableSeconds))
		assert.Equal(t, time.Duration(0), projectedDuration(0))
		assert.Equal(t, time.Duration(0), projectedDuration(-5))
		assert.Equal(t, time.Second, projectedDuration(1))
	})
}
