// storage_growth.go derives a resource's GROWTH RATE
// (storage-observability), which is the input that turns a capacity snapshot
// into a warning with lead time.
//
// The rate is Δbytes over Δt across SUCCESSIVE OBSERVATIONS of the same
// resource, and the whole file exists to keep it that way. Two implementations
// are easier and both are wrong:
//
//   - An in-process sample window is self-defeating. Every restart blanks the
//     projection for a full window, and the longer the window needed to keep a
//     burst from tripping critical, the longer the blackout — so a deploy loop
//     or crash loop, exactly when the projection matters most, would guarantee
//     there is never one. The observations here come from the published report
//     bucket (storage_report.go), which is restart-surviving by construction.
//   - Dividing a resource's current size by the span of its OWN retained
//     timestamps needs no history at all, and reports growth for a bucket whose
//     size never changes: a KV bucket under History 1 compacts superseded
//     revisions, so an actively churning bucket holds roughly constant bytes
//     while its timestamps span a long window. More fundamentally, one snapshot
//     cannot distinguish "grew to 100 MB over a year" from "grew to 100 MB
//     yesterday," and those demand opposite operator responses.
//
// Where fewer than two usable observations exist the rate is UNKNOWN, never
// extrapolated from one, and unknown suppresses projection rather than
// producing a fabricated number.

package natsclient

import "time"

// MinGrowthSampleInterval is the shortest interval between two observations
// that yields a rate worth publishing.
//
// Below it the measurement is dominated by sampling jitter rather than growth:
// two publications a second apart with one 50 MB write between them read as
// 50 MB/s and project exhaustion in seconds. That is the false-critical class
// this capability exists to remove, so a too-close pair reports UNKNOWN — and
// the caller keeps the older baseline rather than advancing it, so the series
// converges on a usable interval instead of resetting to "now" forever.
const MinGrowthSampleInterval = 5 * time.Second

// StorageReportGrowthObservations is how many observations of a resource the
// rate derivation needs: two, the current one and one retained prior one. It is
// the FLOOR on the report bucket's declared History depth, not the depth that
// bucket should carry.
//
// The distinction is worth stating because the obvious argument for depth is
// wrong. A publisher seeds from the history BEFORE it writes, so the row it is
// about to compact is the row it just read: a single restart recovers its
// baseline at any depth, including 1. Depth becomes load-bearing in the case
// the design actually cares about — a crash loop, a deploy loop, or several
// processes publishing account-wide, where the newest retained rows are all
// too close together to measure against (MinGrowthSampleInterval) and the walk
// has to reach back PAST them. At depth 1 there is nothing to reach back to and
// the rate stays unknown for exactly as long as the loop lasts.
//
// It is exported as a CROSS-PIN so a catalog editor who reconciles the
// STORAGE_REPORT row's History below the floor fails a test in graph rather
// than silently blanking every rate in the account.
const StorageReportGrowthObservations uint8 = 2

// GrowthState discriminates a known rate from an absent one. There is no third
// state: a rate is either measured across two observations or it is unknown.
type GrowthState string

// Growth states.
const (
	// GrowthKnown means a rate was measured across two observations. Zero and
	// negative rates are KNOWN — a stable resource and a shrinking one are
	// facts, not absences.
	GrowthKnown GrowthState = "known"
	// GrowthUnknown means no usable pair of observations exists. It suppresses
	// time-to-threshold rather than being treated as zero growth, because "not
	// growing" and "not measured" license different operator decisions.
	GrowthUnknown GrowthState = "unknown"
)

// Reasons a growth rate is unavailable. They are distinct because they resolve
// differently: the first resolves on the next collection, the second needs the
// resource to become describable, and the third needs the collection interval
// (or the publication cadence) to be wider than MinGrowthSampleInterval.
const (
	// GrowthUnavailableNoPriorObservation means fewer than two observations of
	// this resource exist yet — a new resource, or a fresh report bucket.
	GrowthUnavailableNoPriorObservation = "no-prior-observation"
	// GrowthUnavailableUnknownUsage means the current usage could not be read,
	// so there is nothing to difference.
	GrowthUnavailableUnknownUsage = "unknown-usage"
	// GrowthUnavailableObservationsTooClose means observations exist but none
	// is separated from the current one by MinGrowthSampleInterval.
	GrowthUnavailableObservationsTooClose = "observations-too-close"
)

// Observation is one measurement of a resource's size at one moment. At is when
// the size was READ from the account, not when the row was written: the write
// happens later by the publication latency, and using it would inflate every
// interval by that latency.
type Observation struct {
	At    time.Time `json:"at"`
	Bytes int64     `json:"bytes"`
}

// Growth is a resource's observed rate of change.
//
// BytesPerSecond is a pointer for the same reason the capacity numbers are: an
// absent rate must be ABSENT, never a zero a downstream reader can mistake for
// "measured, and not growing".
type Growth struct {
	State GrowthState `json:"state"`

	// BytesPerSecond is the measured rate. Non-nil ONLY when State is
	// GrowthKnown. Negative means the resource is shrinking.
	BytesPerSecond *float64 `json:"bytes_per_second,omitempty"`

	// ObservedOver is the interval between the two observations the rate was
	// measured across. Published so a consumer can weigh a rate measured over
	// one minute against one measured over a day; a rate with no interval is
	// not interpretable.
	ObservedOver time.Duration `json:"observed_over_ns,omitempty"`

	// ObservedFrom is when the baseline observation was taken. A POINTER, like
	// the numeric absences: `omitempty` does nothing for a time.Time, so a
	// value field would publish "observed_from":"0001-01-01T00:00:00Z" on every
	// unknown rate, and a consumer could read that zero as a real baseline.
	ObservedFrom *time.Time `json:"observed_from,omitempty"`

	// Unavailable names why the rate is unknown. Empty when State is
	// GrowthKnown.
	Unavailable string `json:"unavailable,omitempty"`
}

// UnknownGrowth is the explicit unmeasured rate, naming why.
func UnknownGrowth(reason string) Growth {
	return Growth{State: GrowthUnknown, Unavailable: reason}
}

// Rate returns the measured rate in bytes per second. ok is false unless the
// rate is known, so an unknown rate can never yield a number.
func (g Growth) Rate() (float64, bool) {
	if g.BytesPerSecond == nil {
		return 0, false
	}
	return *g.BytesPerSecond, true
}

// DeriveGrowth measures the rate between the current observation and the most
// recent usable prior one.
//
// prior may be in any order and may contain observations that are unusable
// (from the future, by clock skew across producers, or too close to the current
// one to measure). The NEWEST usable one is the baseline: capacity planning
// needs the current rate, and averaging in an old sample dilutes exactly the
// recent acceleration an operator needs to see.
func DeriveGrowth(current Observation, prior []Observation) Growth {
	baseline, ok := newestUsableBaseline(current, prior)
	if !ok {
		return UnknownGrowth(unusableBaselineReason(current, prior))
	}

	span := current.At.Sub(baseline.At)
	rate := float64(current.Bytes-baseline.Bytes) / span.Seconds()
	observedFrom := baseline.At
	return Growth{
		State:          GrowthKnown,
		BytesPerSecond: &rate,
		ObservedOver:   span,
		ObservedFrom:   &observedFrom,
	}
}

// newestUsableBaseline picks the most recent observation that is strictly older
// than the current one by at least MinGrowthSampleInterval.
func newestUsableBaseline(current Observation, prior []Observation) (Observation, bool) {
	var best Observation
	found := false
	for _, candidate := range prior {
		if current.At.Sub(candidate.At) < MinGrowthSampleInterval {
			continue
		}
		if found && !candidate.At.After(best.At) {
			continue
		}
		best = candidate
		found = true
	}
	return best, found
}

// unusableBaselineReason distinguishes "nothing to compare against" from
// "something to compare against, but too close to measure". They resolve
// differently, so collapsing them would send an operator to the wrong place.
func unusableBaselineReason(current Observation, prior []Observation) string {
	for _, candidate := range prior {
		// gap == 0 counts as too close, not as absent: an identical timestamp
		// is the same collection observed twice, which is a cadence fact rather
		// than a missing-history one.
		gap := current.At.Sub(candidate.At)
		if gap >= 0 && gap < MinGrowthSampleInterval {
			return GrowthUnavailableObservationsTooClose
		}
	}
	return GrowthUnavailableNoPriorObservation
}
