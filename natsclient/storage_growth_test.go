package natsclient

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mib(n int64) int64 { return n << 20 }

// TestDeriveGrowth_FromSuccessiveObservations covers the rate derivation's
// whole vocabulary. Every case is a fact about SUCCESSIVE OBSERVATIONS — two
// measurements of the same resource at two moments — because that is the only
// input that can distinguish "grew to 100 MB over a year" from "grew to 100 MB
// yesterday", and those demand opposite operator responses.
func TestDeriveGrowth_FromSuccessiveObservations(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	tests := []struct {
		name       string
		current    Observation
		prior      []Observation
		wantState  GrowthState
		wantReason string
		wantRate   float64
		wantSpan   time.Duration
	}{
		{
			name:       "a single observation extrapolates nothing",
			current:    Observation{At: now, Bytes: mib(100)},
			prior:      nil,
			wantState:  GrowthUnknown,
			wantReason: GrowthUnavailableNoPriorObservation,
		},
		{
			name:      "two observations give a rate over their own interval",
			current:   Observation{At: now, Bytes: mib(101)},
			prior:     []Observation{{At: ago(time.Minute), Bytes: mib(100)}},
			wantState: GrowthKnown,
			wantRate:  float64(mib(1)) / 60,
			wantSpan:  time.Minute,
		},
		{
			// The churn case. A KV bucket under History 1 compacts superseded
			// revisions, so continuous replacement holds bytes constant while
			// the resource's own retained timestamps span a long window. The
			// rate is ZERO because the two observations are equal — nothing
			// about the resource's internal timestamps enters this computation.
			name:      "continuous replacement at a stable size is zero growth",
			current:   Observation{At: now, Bytes: mib(512)},
			prior:     []Observation{{At: ago(10 * time.Minute), Bytes: mib(512)}},
			wantState: GrowthKnown,
			wantRate:  0,
			wantSpan:  10 * time.Minute,
		},
		{
			// Reported as negative rather than clamped to zero: a shrinking
			// resource and a stable one are different facts, and clamping would
			// make "the rate is zero" stop meaning "the size did not change".
			name:      "a shrinking resource reports a negative rate",
			current:   Observation{At: now, Bytes: mib(90)},
			prior:     []Observation{{At: ago(time.Minute), Bytes: mib(100)}},
			wantState: GrowthKnown,
			wantRate:  -float64(mib(10)) / 60,
			wantSpan:  time.Minute,
		},
		{
			name:       "an observation from the future is not a prior one",
			current:    Observation{At: now, Bytes: mib(100)},
			prior:      []Observation{{At: now.Add(time.Minute), Bytes: mib(50)}},
			wantState:  GrowthUnknown,
			wantReason: GrowthUnavailableNoPriorObservation,
		},
		{
			// Two publications inside the same moment measure sampling jitter,
			// not growth: a 50 MB write between them would read as 50 MB/s and
			// project exhaustion in seconds.
			name:       "observations too close together are not a usable interval",
			current:    Observation{At: now, Bytes: mib(150)},
			prior:      []Observation{{At: ago(time.Second), Bytes: mib(100)}},
			wantState:  GrowthUnknown,
			wantReason: GrowthUnavailableObservationsTooClose,
		},
		{
			// The same collection observed twice is a cadence fact, not a
			// missing-history one: reporting it as "no prior observation" would
			// send an operator to look for a series that is right there.
			name:       "a duplicate observation is too close, not absent",
			current:    Observation{At: now, Bytes: mib(100)},
			prior:      []Observation{{At: now, Bytes: mib(100)}},
			wantState:  GrowthUnknown,
			wantReason: GrowthUnavailableObservationsTooClose,
		},
		{
			// The NEWEST usable prior wins: capacity planning needs the current
			// rate, not an average diluted by an old sample. Picking the oldest
			// here would report 200 B over 300s instead of 100 B over 30s.
			name:    "the newest usable observation is the baseline",
			current: Observation{At: now, Bytes: 300},
			prior: []Observation{
				{At: ago(5 * time.Minute), Bytes: 100},
				{At: ago(30 * time.Second), Bytes: 200},
			},
			wantState: GrowthKnown,
			wantRate:  100.0 / 30.0,
			wantSpan:  30 * time.Second,
		},
		{
			// Skips the too-close sample and keeps walking rather than giving
			// up: the older observation is a perfectly good baseline.
			name:    "a too-close sample does not hide a usable older one",
			current: Observation{At: now, Bytes: 300},
			prior: []Observation{
				{At: ago(time.Minute), Bytes: 100},
				{At: ago(time.Millisecond), Bytes: 299},
			},
			wantState: GrowthKnown,
			wantRate:  200.0 / 60.0,
			wantSpan:  time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveGrowth(tc.current, tc.prior)
			require.Equal(t, tc.wantState, got.State)

			rate, ok := got.Rate()
			if tc.wantState != GrowthKnown {
				assert.False(t, ok, "an unknown rate must never read back as a number")
				assert.Equal(t, tc.wantReason, got.Unavailable)
				assert.Zero(t, got.ObservedOver)
				return
			}
			require.True(t, ok)
			assert.InDelta(t, tc.wantRate, rate, 0.0001)
			assert.Equal(t, tc.wantSpan, got.ObservedOver)
			assert.Empty(t, got.Unavailable)
		})
	}
}

// TestDeriveGrowth_IsNotBytesOverTheResourcesOwnSpan is the falsifying test for
// the design's corrected decision 7: the tempting implementation reads a
// stream's own FirstTime/LastTime span and divides its byte count by it, which
// needs no history at all — and reports sustained growth, and projects
// exhaustion, for a bucket whose size never changes.
//
// The two observations here are of a 512 MiB resource whose size has not moved
// in ten minutes, and whose retained content spans a year. Bytes-over-span
// would report ~16 B/s and a finite time to exhaustion. Δbytes-over-Δt reports
// exactly zero. Only the second can be told apart from real growth.
func TestDeriveGrowth_IsNotBytesOverTheResourcesOwnSpan(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	size := mib(512)

	growth := DeriveGrowth(
		Observation{At: now, Bytes: size},
		[]Observation{{At: now.Add(-10 * time.Minute), Bytes: size}},
	)

	require.Equal(t, GrowthKnown, growth.State)
	rate, ok := growth.Rate()
	require.True(t, ok)
	assert.Zero(t, rate, "equal successive observations are zero growth, whatever the resource retains")

	overOwnSpan := float64(size) / (365 * 24 * time.Hour).Seconds()
	require.Greater(t, overOwnSpan, 0.0,
		"test premise: the falsified implementation would report a positive rate here")
	assert.NotEqual(t, overOwnSpan, rate,
		"a rate derived from the resource's own retained span would be non-zero here")
}

// TestGrowth_UnknownRatePublishesNoBaselineTimestamp: an unknown rate must not
// serialize a zero timestamp. `omitempty` does nothing for a time.Time, so a
// value field would put "observed_from":"0001-01-01T00:00:00Z" on the wire for
// every resource without a baseline, and a consumer reading the report has no
// way to tell that apart from a real observation taken in year one.
func TestGrowth_UnknownRatePublishesNoBaselineTimestamp(t *testing.T) {
	unknown, err := json.Marshal(UnknownGrowth(GrowthUnavailableNoPriorObservation))
	require.NoError(t, err)
	assert.NotContains(t, string(unknown), "observed_from",
		"an absent baseline is absent from the wire, not a zero value on it")
	assert.NotContains(t, string(unknown), "bytes_per_second")
	assert.NotContains(t, string(unknown), "0001-01-01")

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	known := DeriveGrowth(
		Observation{At: now, Bytes: 200},
		[]Observation{{At: now.Add(-time.Minute), Bytes: 100}},
	)
	encoded, err := json.Marshal(known)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "observed_from")

	var decoded Growth
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.NotNil(t, decoded.ObservedFrom)
	assert.Equal(t, now.Add(-time.Minute), decoded.ObservedFrom.UTC(),
		"a real baseline survives the round trip a consumer decodes it through")
}
