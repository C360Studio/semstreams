// storage_pressure.go turns a capacity reading plus an observed growth rate
// into the two things an operator acts on (storage-observability): a
// PROJECTION (how much room is left, and how long until it runs out) and a
// PRESSURE state (how worried to be, and which input says so).
//
// Two properties are load-bearing.
//
// Pressure is the WORSE of a proportional-headroom band and a
// time-to-threshold band, and it names which one raised it. Headroom alone
// misranks: a 2 TiB resource at 40% filling in an hour is more urgent than a
// 1 GiB resource at 85% static for a month. Naming the input is what tells an
// operator whether they have a capacity problem or a rate problem, which are
// fixed differently.
//
// Pressure is REPORT-ONLY. Nothing here rejects a write, throttles a component,
// fails a readiness or health gate, or applies retention — this file returns
// values; it holds no reference to anything that could refuse work. That is
// sequencing, not timidity: enforcement built on an unmeasured, untrusted
// signal is how this capability's predecessor went wrong, and application-level
// admission in graph-ingest (the only place a rejection can be entity-atomic
// and cross-bucket-coherent) is the correct eventual home for backpressure.
//
// Thresholds are OPERATOR CONFIGURATION resolved AT EVALUATION TIME. See
// ThresholdSource in storage_report.go: SemStreams is flow-based and its
// components are runtime-reconfigurable, so a threshold captured at
// composition-root construction would apply a stale number successfully and
// silently after a post-boot edit.

package natsclient

import (
	"fmt"
	"math"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Default pressure thresholds. Headroom bands are the FRACTION of the
// configured bound still free; horizon bands are the projected time until the
// resource crosses the critical-headroom level.
//
// The defaults are deliberately generous on time and tight on space: a
// three-day warning horizon is roughly the lead time needed to plan and apply a
// capacity change, while 25% free is late enough that it is not noise.
const (
	// DefaultWarningHeadroom is the free fraction at or below which a resource
	// reports warning.
	DefaultWarningHeadroom = 0.25
	// DefaultHighHeadroom is the free fraction at or below which a resource
	// reports high.
	DefaultHighHeadroom = 0.15
	// DefaultCriticalHeadroom is the free fraction at or below which a resource
	// reports critical. It is ALSO the level the time projection targets.
	DefaultCriticalHeadroom = 0.05

	// DefaultWarningHorizon is the projected time-to-threshold at or below
	// which a resource reports warning.
	DefaultWarningHorizon = 72 * time.Hour
	// DefaultHighHorizon is the projected time-to-threshold at or below which a
	// resource reports high.
	DefaultHighHorizon = 24 * time.Hour
	// DefaultCriticalHorizon is the projected time-to-threshold at or below
	// which a resource reports critical.
	DefaultCriticalHorizon = 4 * time.Hour
)

// StoragePressureThresholds is the OPERATOR-FACING pressure configuration.
//
// Durations are strings ("72h", "90m") following this repository's operator
// configuration convention, parsed by Resolve rather than at decode time, so a
// malformed edit is a reported configuration error rather than a decode failure
// that takes a component down.
//
// Every field is optional and an omitted field takes its documented default. An
// out-of-range or incoherent field is an ERROR, never a silent fallback: a
// silently defaulted threshold applies a number the operator did not choose and
// is indistinguishable from a working edit.
type StoragePressureThresholds struct {
	// WarningHeadroom is the free fraction (0..1) at or below which a resource
	// reports warning.
	WarningHeadroom float64 `json:"warning_headroom,omitempty" schema:"type:number,description:Free fraction at or below which a resource reports warning pressure,default:0.25,category:advanced"`

	// HighHeadroom is the free fraction at or below which a resource reports
	// high.
	HighHeadroom float64 `json:"high_headroom,omitempty" schema:"type:number,description:Free fraction at or below which a resource reports high pressure,default:0.15,category:advanced"`

	// CriticalHeadroom is the free fraction at or below which a resource
	// reports critical. It is also the level the time projection targets, so
	// "time to threshold" means "time until this fraction is all that is left".
	CriticalHeadroom float64 `json:"critical_headroom,omitempty" schema:"type:number,description:Free fraction at or below which a resource reports critical pressure (also the level time-to-threshold targets),default:0.05,category:advanced"`

	// WarningHorizon is the projected time-to-threshold at or below which a
	// resource reports warning.
	WarningHorizon string `json:"warning_horizon,omitempty" schema:"type:string,description:Projected time-to-threshold at or below which a resource reports warning pressure,default:72h,category:advanced"`

	// HighHorizon is the projected time-to-threshold at or below which a
	// resource reports high.
	HighHorizon string `json:"high_horizon,omitempty" schema:"type:string,description:Projected time-to-threshold at or below which a resource reports high pressure,default:24h,category:advanced"`

	// CriticalHorizon is the projected time-to-threshold at or below which a
	// resource reports critical.
	CriticalHorizon string `json:"critical_horizon,omitempty" schema:"type:string,description:Projected time-to-threshold at or below which a resource reports critical pressure,default:4h,category:advanced"`
}

// DefaultStoragePressureThresholds returns the shipped defaults in their
// operator-authored form, so a caller that wants them deliberately can say so
// in one line rather than passing a zero value and hoping.
func DefaultStoragePressureThresholds() StoragePressureThresholds {
	return StoragePressureThresholds{
		WarningHeadroom:  DefaultWarningHeadroom,
		HighHeadroom:     DefaultHighHeadroom,
		CriticalHeadroom: DefaultCriticalHeadroom,
		WarningHorizon:   DefaultWarningHorizon.String(),
		HighHorizon:      DefaultHighHorizon.String(),
		CriticalHorizon:  DefaultCriticalHorizon.String(),
	}
}

// ResolvedPressureThresholds is the applied form: defaults filled, durations
// parsed, ordering validated. Only Resolve produces one, so no evaluation can
// run against a threshold set nobody checked.
type ResolvedPressureThresholds struct {
	WarningHeadroom  float64
	HighHeadroom     float64
	CriticalHeadroom float64

	WarningHorizon  time.Duration
	HighHorizon     time.Duration
	CriticalHorizon time.Duration
}

// Resolve applies defaults, parses the horizon durations, and validates the
// whole set. It fails on any incoherence rather than repairing it, because a
// repaired threshold is a number the operator did not choose.
func (t StoragePressureThresholds) Resolve() (ResolvedPressureThresholds, error) {
	resolved := ResolvedPressureThresholds{
		WarningHeadroom:  defaultFloat(t.WarningHeadroom, DefaultWarningHeadroom),
		HighHeadroom:     defaultFloat(t.HighHeadroom, DefaultHighHeadroom),
		CriticalHeadroom: defaultFloat(t.CriticalHeadroom, DefaultCriticalHeadroom),
	}

	var err error
	if resolved.WarningHorizon, err = resolveHorizon(t.WarningHorizon, "warning_horizon", DefaultWarningHorizon); err != nil {
		return ResolvedPressureThresholds{}, err
	}
	if resolved.HighHorizon, err = resolveHorizon(t.HighHorizon, "high_horizon", DefaultHighHorizon); err != nil {
		return ResolvedPressureThresholds{}, err
	}
	if resolved.CriticalHorizon, err = resolveHorizon(t.CriticalHorizon, "critical_horizon", DefaultCriticalHorizon); err != nil {
		return ResolvedPressureThresholds{}, err
	}

	for _, band := range []struct {
		field string
		value float64
	}{
		{"warning_headroom", resolved.WarningHeadroom},
		{"high_headroom", resolved.HighHeadroom},
		{"critical_headroom", resolved.CriticalHeadroom},
	} {
		if band.value <= 0 || band.value >= 1 {
			return ResolvedPressureThresholds{}, thresholdError(
				fmt.Errorf("%s must be a fraction between 0 and 1 exclusive, got %v", band.field, band.value))
		}
	}

	// Equal bands are allowed — collapsing two bands is a legitimate operator
	// choice — but an INVERTED pair is not: it would make a resource report
	// critical before it reported warning, which no consumer could interpret.
	if resolved.CriticalHeadroom > resolved.HighHeadroom {
		return ResolvedPressureThresholds{}, thresholdError(
			fmt.Errorf("critical_headroom (%v) must not exceed high_headroom (%v); a smaller free fraction is worse",
				resolved.CriticalHeadroom, resolved.HighHeadroom))
	}
	if resolved.HighHeadroom > resolved.WarningHeadroom {
		return ResolvedPressureThresholds{}, thresholdError(
			fmt.Errorf("high_headroom (%v) must not exceed warning_headroom (%v); a smaller free fraction is worse",
				resolved.HighHeadroom, resolved.WarningHeadroom))
	}
	if resolved.CriticalHorizon > resolved.HighHorizon {
		return ResolvedPressureThresholds{}, thresholdError(
			fmt.Errorf("critical_horizon (%s) must not exceed high_horizon (%s); less time is worse",
				resolved.CriticalHorizon, resolved.HighHorizon))
	}
	if resolved.HighHorizon > resolved.WarningHorizon {
		return ResolvedPressureThresholds{}, thresholdError(
			fmt.Errorf("high_horizon (%s) must not exceed warning_horizon (%s); less time is worse",
				resolved.HighHorizon, resolved.WarningHorizon))
	}

	return resolved, nil
}

func defaultFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func resolveHorizon(value, field string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, thresholdError(fmt.Errorf("%s %q is not a duration: %w", field, value, err))
	}
	if parsed <= 0 {
		return 0, thresholdError(fmt.Errorf("%s must be a positive duration, got %q", field, value))
	}
	return parsed, nil
}

func thresholdError(err error) error {
	return errs.WrapInvalid(err, "StoragePressure", "Resolve", "resolve storage pressure thresholds")
}

// Reasons a projection is unavailable. Each names a different missing input, so
// an operator reading a suppressed projection can tell whether to fix the
// server, the declaration, or simply wait for the next collection.
const (
	// ProjectionUnavailableUnknownCapacity means the limit or usage could not
	// be read. Projecting from it would fabricate a number.
	ProjectionUnavailableUnknownCapacity = "unknown-capacity"
	// ProjectionUnavailableUnbounded means the resource declares no bound.
	// There is nothing to have headroom against and nothing to run out of.
	ProjectionUnavailableUnbounded = "unbounded"
	// ProjectionUnavailableUnknownGrowth means the rate has not been measured
	// yet, so no time can be projected. Headroom is still reported.
	ProjectionUnavailableUnknownGrowth = "unknown-growth-rate"
	// ProjectionUnavailableNotGrowing means the rate was measured and is zero
	// or negative: the threshold is never reached. Distinct from
	// unknown-growth-rate, because "measured, and not growing" is a finding an
	// operator can rely on.
	ProjectionUnavailableNotGrowing = "not-growing"
)

// Projection is what the report can say about a resource's future.
//
// Headroom and time-to-threshold are suppressed INDEPENDENTLY, each with its
// own reason: headroom needs a readable bound, while a time projection
// additionally needs a measured, positive rate. Suppressing them together would
// discard the half that is knowable.
//
// The numeric fields are pointers so an absent projection is ABSENT rather than
// a zero that reads as "no headroom left" or "exhausted now".
type Projection struct {
	// HeadroomBytes is the distance from current usage to the configured
	// bound. It can be NEGATIVE for a resource already over its bound, which is
	// reported rather than clamped.
	HeadroomBytes *int64 `json:"headroom_bytes,omitempty"`

	// HeadroomFraction is HeadroomBytes as a fraction of the bound.
	HeadroomFraction *float64 `json:"headroom_fraction,omitempty"`

	// HeadroomUnavailable names why headroom is absent. Empty when present.
	HeadroomUnavailable string `json:"headroom_unavailable,omitempty"`

	// ThresholdBytes is the usage level the time projection targets: the
	// critical-headroom level, not the bound itself. Capacity has to be
	// corrected BEFORE the last byte fits.
	ThresholdBytes *int64 `json:"threshold_bytes,omitempty"`

	// TimeToThreshold is the projected time until usage reaches
	// ThresholdBytes at the observed rate. Zero means the resource is already
	// there; it is never negative.
	TimeToThreshold *time.Duration `json:"time_to_threshold_ns,omitempty"`

	// TimeToThresholdUnavailable names why the time projection is absent.
	TimeToThresholdUnavailable string `json:"time_to_threshold_unavailable,omitempty"`
}

// Project derives headroom and time-to-threshold for one resource.
func Project(capacity Capacity, growth Growth, thresholds ResolvedPressureThresholds) Projection {
	if !capacity.Bounded() {
		reason := ProjectionUnavailableUnbounded
		if capacity.State == CapacityUnknown {
			reason = ProjectionUnavailableUnknownCapacity
		}
		return Projection{HeadroomUnavailable: reason, TimeToThresholdUnavailable: reason}
	}

	// A bounded capacity carries both numbers by construction (NewCapacity);
	// the checks keep a hand-built row from projecting off a nil.
	limit, ok := capacity.Limit()
	if !ok {
		return Projection{
			HeadroomUnavailable:        ProjectionUnavailableUnknownCapacity,
			TimeToThresholdUnavailable: ProjectionUnavailableUnknownCapacity,
		}
	}
	used, ok := capacity.Usage()
	if !ok {
		return Projection{
			HeadroomUnavailable:        ProjectionUnavailableUnknownCapacity,
			TimeToThresholdUnavailable: ProjectionUnavailableUnknownCapacity,
		}
	}

	headroomBytes := limit - used
	headroomFraction := float64(headroomBytes) / float64(limit)
	thresholdBytes := int64(math.Floor(float64(limit) * (1 - thresholds.CriticalHeadroom)))

	projection := Projection{
		HeadroomBytes:    &headroomBytes,
		HeadroomFraction: &headroomFraction,
		ThresholdBytes:   &thresholdBytes,
	}

	rate, known := growth.Rate()
	switch {
	case !known:
		projection.TimeToThresholdUnavailable = ProjectionUnavailableUnknownGrowth
	case rate <= 0:
		projection.TimeToThresholdUnavailable = ProjectionUnavailableNotGrowing
	case used >= thresholdBytes:
		zero := time.Duration(0)
		projection.TimeToThreshold = &zero
	default:
		remaining := projectedDuration(float64(thresholdBytes-used) / rate)
		projection.TimeToThreshold = &remaining
	}
	return projection
}

// maxProjectableSeconds is the longest projection a time.Duration can carry
// (about 292 years).
const maxProjectableSeconds = float64(math.MaxInt64) / float64(time.Second)

// projectedDuration converts a projected number of seconds to a Duration
// WITHOUT letting the conversion overflow.
//
// This guard is load-bearing and the failure it prevents is nasty. A resource
// that grew one byte over a long window has a positive rate near 1e-5 B/s, and
// a gigabyte of headroom at that rate projects millions of years — far past
// what an int64 nanosecond count can hold. Converting an out-of-range float to
// an integer is not defined by the Go spec, and the two platforms this
// repository runs on disagree: arm64 saturates to MaxInt64, while amd64 yields
// MIN int64. A "never report a negative duration" clamp would therefore turn
// the calmest resource in the account into a ZERO time-to-threshold — critical
// pressure — on Linux only, and look perfectly fine on a developer's Mac.
//
// The honest saturation is upward: "not inside any horizon you configured".
// Every non-finite input takes that arm too. NaN and ±Inf mean the projection
// could not be computed, and an uncomputable projection must never read as
// "exhausted now" — that is the same false-critical this function exists to
// prevent, arrived at from a different direction. Project filters
// non-positive rates before calling, but Project is exported and takes an
// arbitrary Growth, so a caller handing it a NaN rate must not get a page.
func projectedDuration(seconds float64) time.Duration {
	switch {
	case math.IsNaN(seconds), math.IsInf(seconds, 0), seconds >= maxProjectableSeconds:
		return time.Duration(math.MaxInt64)
	case seconds <= 0:
		return 0
	default:
		return time.Duration(seconds * float64(time.Second))
	}
}

// PressureState is how worried an operator should be about one resource.
type PressureState string

// Pressure states, worst last.
const (
	// PressureNormal means neither input is inside a configured band.
	PressureNormal PressureState = "normal"
	// PressureWarning means an input crossed the widest band.
	PressureWarning PressureState = "warning"
	// PressureHigh means an input crossed the middle band.
	PressureHigh PressureState = "high"
	// PressureCritical means an input crossed the tightest band. It still
	// rejects nothing: pressure is report-only in this capability.
	PressureCritical PressureState = "critical"
)

// PressureInput names which input raised a pressure state, so an operator can
// tell a capacity problem from a rate problem.
type PressureInput string

// Pressure inputs.
const (
	// PressureInputNone means nothing raised the state above normal.
	PressureInputNone PressureInput = "none"
	// PressureInputHeadroom means proportional headroom raised it.
	PressureInputHeadroom PressureInput = "headroom"
	// PressureInputTimeToThreshold means the projection raised it — the case
	// proportional headroom alone would have missed.
	PressureInputTimeToThreshold PressureInput = "time-to-threshold"
	// PressureInputBoth means both inputs landed on the same band. Reported
	// rather than tie-broken: an arbitrary winner would misreport the cause.
	PressureInputBoth PressureInput = "both"
)

// Reasons no pressure state exists for a resource.
const (
	// PressureUnavailableUnknownCapacity means the capacity could not be read.
	// Reporting normal here would manufacture confidence about a resource
	// nobody can measure.
	PressureUnavailableUnknownCapacity = "unknown-capacity"
	// PressureUnavailableUnbounded means the capacity handed to AssessPressure
	// declares no bound, so neither band has an input.
	//
	// For a RESOURCE it is not the final word: PressureAgainstAccountTier
	// re-evaluates it against the only ceiling it has, and the more specific
	// unbounded-* reasons below are what a resource ends up carrying. This value
	// survives on a TIER row — AssessPressure over an unbounded account limit —
	// and as the fallback when a bounded tier reports no reason of its own.
	PressureUnavailableUnbounded = "unbounded"
	// PressureUnavailableUnboundedNoTierCeiling means the resource declares no
	// bound AND its storage tier's account limit is itself unbounded. Nothing
	// anywhere constrains it, so there is genuinely nothing to project — as
	// distinct from a ceiling that exists and could not be read.
	PressureUnavailableUnboundedNoTierCeiling = "unbounded-no-account-tier-ceiling"
	// PressureUnavailableUnboundedTierUnknown means the resource declares no
	// bound and its tier's account limit could not be read. The ceiling may well
	// exist; this process cannot see it, which is a gap rather than a licence to
	// report the resource as fine.
	PressureUnavailableUnboundedTierUnknown = "unbounded-account-tier-unknown"
	// PressureUnavailableUnboundedTierUnfiled means the resource declares no
	// bound and could not be filed under any tier, so no account ceiling applies
	// to it that this process can name.
	//
	// NOT REACHABLE from the production collector: DeriveAccountReport emits a row
	// for every tier any resource reports, creating the unknown-tier row on demand,
	// so a collected resource always finds one. It exists as the fail-closed arm
	// for a hand-built inventory, and reporting no state is the right answer there
	// rather than borrowing an arbitrary tier's.
	PressureUnavailableUnboundedTierUnfiled = "unbounded-account-tier-unfiled"
)

// PressureBasis names WHICH CEILING a pressure state was evaluated against.
//
// It exists because this capability now evaluates two different ceilings and an
// operator acts on them differently. A resource at high pressure against its own
// declared bound is fixed by raising that bound or letting retention do its job.
// A resource at high pressure against its ACCOUNT TIER has no bound of its own to
// raise: the tier is filling, the finding is about every resource sharing it, and
// the levers are account capacity or deleting data. Publishing the state without
// the basis would leave those indistinguishable in the one field every alert rule
// reads.
type PressureBasis string

// Pressure bases.
const (
	// PressureBasisOwnBound means the state came from the resource's own
	// declared limit.
	PressureBasisOwnBound PressureBasis = "own-bound"
	// PressureBasisAccountTier means the state came from the account limit of
	// the storage tier the resource lives in. For an unbounded resource that is
	// the only ceiling there is; for a tier row it is the tier's own limit.
	PressureBasisAccountTier PressureBasis = "account-tier"
)

// Pressure is one resource's derived pressure state.
//
// Evaluated is explicit rather than inferred from an empty State, so a consumer
// cannot read "no state" as normal. A resource whose capacity is unknown or
// unbounded has NO pressure state at all — reporting normal for it would be the
// phantom-signal class this capability exists to remove.
type Pressure struct {
	// Evaluated reports whether a state could be derived at all.
	Evaluated bool `json:"evaluated"`

	// State is the worse of the two bands. Empty when Evaluated is false.
	State PressureState `json:"state,omitempty"`

	// RaisedBy names the input that produced State.
	RaisedBy PressureInput `json:"raised_by,omitempty"`

	// FromHeadroom and FromTimeToThreshold are the individual bands, published
	// so a consumer can see the input that did NOT win.
	// FromTimeToThreshold is empty when no projection was available.
	//
	// They describe whichever ceiling EvaluatedAgainst names. On a row whose basis
	// is the account tier these are the TIER's bands, not this resource's — the
	// resource has no bound of its own to produce any — so they will not agree with
	// the row's own Projection, which reports headroom unavailable. Read the basis
	// first; the numbers behind these bands are on the account row.
	FromHeadroom        PressureState `json:"from_headroom,omitempty"`
	FromTimeToThreshold PressureState `json:"from_time_to_threshold,omitempty"`

	// EvaluatedAgainst names the ceiling State was derived from. Set whenever
	// Evaluated, so no consumer has to infer the basis from the resource's
	// capacity state.
	EvaluatedAgainst PressureBasis `json:"evaluated_against,omitempty"`

	// Unavailable names why no state was derived. Empty when Evaluated.
	Unavailable string `json:"unavailable,omitempty"`
}

// AgainstAccountTier restates an evaluated pressure as having come from an
// account tier ceiling rather than a resource's own bound.
//
// It only relabels the basis. The bands are whatever the comparison produced, and
// an unevaluated pressure passes through untouched: there is no state to
// attribute to any ceiling.
func (p Pressure) AgainstAccountTier() Pressure {
	if !p.Evaluated {
		return p
	}
	p.EvaluatedAgainst = PressureBasisAccountTier
	return p
}

// PressureAgainstAccountTier evaluates an UNBOUNDED resource against the only
// ceiling it has: the account limit of the storage tier it lives in.
//
// This exists so that declaring a stream archival cannot remove it from the
// surface that would warn about it. Capacity matters MORE for a resource that can
// never evict — capacity is then the only lever an operator has — so reporting it
// permanently unevaluable would put the least recoverable resources in the account
// behind the one field every alert rule keys on.
//
// The state is the TIER's, unmodified and deliberately so. The ceiling is shared,
// the usage measured against it is the account's own, and the rate is the tier's:
// every resource sharing a filling tier is genuinely in the same trouble. Scaling
// the tier's verdict by this resource's share would be a fabrication — it would
// report a stream as calm because it is individually small, while the ceiling that
// governs it fills from elsewhere.
//
// found reports whether the resource could be filed under a tier at all. It is a
// parameter rather than a zero-value check because an absent tier row and a tier
// row with no ceiling are different findings.
func PressureAgainstAccountTier(tier TierComparison, found bool) Pressure {
	if !found {
		return Pressure{Unavailable: PressureUnavailableUnboundedTierUnfiled}
	}
	if tier.Pressure.Evaluated {
		return tier.Pressure.AgainstAccountTier()
	}

	// The tier could not be evaluated either. Report why in the RESOURCE's terms:
	// "unbounded, and the ceiling that would have governed it is unbounded too" is
	// a different operator conversation from "unbounded, and nobody can read the
	// ceiling".
	switch tier.Limit.State {
	case CapacityUnbounded:
		return Pressure{Unavailable: PressureUnavailableUnboundedNoTierCeiling}
	case CapacityUnknown:
		return Pressure{Unavailable: PressureUnavailableUnboundedTierUnknown}
	default:
		// A bounded tier limit whose pressure still did not evaluate: the tier's
		// own reason is the accurate one, and inventing a different one here would
		// disagree with the account row a consumer can read alongside this.
		if tier.Pressure.Unavailable != "" {
			return Pressure{Unavailable: tier.Pressure.Unavailable}
		}
		return Pressure{Unavailable: PressureUnavailableUnbounded}
	}
}

// AssessPressure derives the pressure state from a resource's projection.
//
// It returns a value and touches nothing else: no write is rejected, no
// component is throttled, no readiness gate is failed, and no retention is
// applied as a consequence of what it returns.
func AssessPressure(
	capacity Capacity, projection Projection, thresholds ResolvedPressureThresholds,
) Pressure {
	if !capacity.Bounded() {
		reason := PressureUnavailableUnbounded
		if capacity.State == CapacityUnknown {
			reason = PressureUnavailableUnknownCapacity
		}
		return Pressure{Unavailable: reason}
	}
	if projection.HeadroomFraction == nil {
		return Pressure{Unavailable: PressureUnavailableUnknownCapacity}
	}

	headroomBand := headroomBandFor(*projection.HeadroomFraction, thresholds)
	pressure := Pressure{
		Evaluated:        true,
		State:            headroomBand,
		RaisedBy:         PressureInputNone,
		FromHeadroom:     headroomBand,
		EvaluatedAgainst: PressureBasisOwnBound,
	}
	if headroomBand != PressureNormal {
		pressure.RaisedBy = PressureInputHeadroom
	}

	// An unavailable projection contributes NOTHING. It is not a normal band:
	// treating "not measured" as "fine" is how a rate problem hides.
	if projection.TimeToThreshold == nil {
		return pressure
	}

	timeBand := horizonBandFor(*projection.TimeToThreshold, thresholds)
	pressure.FromTimeToThreshold = timeBand

	switch {
	case pressureSeverity(timeBand) > pressureSeverity(headroomBand):
		pressure.State = timeBand
		pressure.RaisedBy = PressureInputTimeToThreshold
	case timeBand == headroomBand && timeBand != PressureNormal:
		pressure.RaisedBy = PressureInputBoth
	}
	return pressure
}

// headroomBandFor maps a free fraction to a band. Bands are inclusive at the
// threshold: exactly 25% free IS a warning, because a threshold an operator
// sets is the point they want to hear about.
func headroomBandFor(fraction float64, thresholds ResolvedPressureThresholds) PressureState {
	switch {
	case fraction <= thresholds.CriticalHeadroom:
		return PressureCritical
	case fraction <= thresholds.HighHeadroom:
		return PressureHigh
	case fraction <= thresholds.WarningHeadroom:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// horizonBandFor maps a projected time-to-threshold to a band, inclusive at the
// threshold for the same reason.
func horizonBandFor(remaining time.Duration, thresholds ResolvedPressureThresholds) PressureState {
	switch {
	case remaining <= thresholds.CriticalHorizon:
		return PressureCritical
	case remaining <= thresholds.HighHorizon:
		return PressureHigh
	case remaining <= thresholds.WarningHorizon:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// pressureSeverity orders the states so "the worse of the two" is a comparison
// rather than a nest of conditionals. An unknown state ranks lowest so it can
// never silently outrank a measured one.
func pressureSeverity(state PressureState) int {
	switch state {
	case PressureWarning:
		return 1
	case PressureHigh:
		return 2
	case PressureCritical:
		return 3
	case PressureNormal:
		return 0
	default:
		return 0
	}
}
