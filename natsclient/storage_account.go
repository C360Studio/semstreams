// storage_account.go answers the one capacity question a per-resource row
// cannot (storage-observability): every resource can be individually within its
// own bound while the ACCOUNT they all share cannot hold the sum of those
// bounds. That comparison is the over-commitment check, and it is computed
// strictly WITHIN a storage tier.
//
// Two properties are load-bearing, and both are about not manufacturing a
// number.
//
// TIERS ARE NEVER SUMMED. JetStream keeps separate memory and file account
// limits, and this repository's own streams span both — HEALTH, METRICS and
// FLOWS are memory-backed while LOGS and GOVERNANCE_VERDICT_AUDIT are
// file-backed. A total across the two compares a sum against neither limit, and
// its slack in one tier hides exhaustion in the other.
//
// AN UNBOUNDED OR UNREADABLE ACCOUNT LIMIT MAKES THE COMPARISON NOT
// APPLICABLE, never satisfied. There is no over-commitment against a limit that
// does not exist, and "within limit" against no limit is the phantom-signal
// class this capability exists to remove. It is also the DEFAULT path rather
// than an edge case: a stock server (and testcontainers) reports -1.
//
// Nothing here rejects, throttles, or degrades anything. It returns values.

package natsclient

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// OvercommitmentState is the verdict on one tier's declared-versus-limit
// comparison.
type OvercommitmentState string

// Over-commitment states.
const (
	// OvercommitmentWithin means the declared bounds in this tier fit inside
	// the account limit for the tier. It says nothing about the tier's
	// unbounded resources, whose declarations are absent from the sum by
	// construction — read UnboundedResources alongside it.
	OvercommitmentWithin OvercommitmentState = "within-limit"
	// OvercommitmentOver means the declared bounds in this tier exceed the
	// account limit for the tier. Every resource can still be within its own
	// bound: this is the failure mode a per-resource view cannot see.
	OvercommitmentOver OvercommitmentState = "over-committed"
	// OvercommitmentNotApplicable means there is no comparison to make,
	// because the account limit for this tier is unbounded or unreadable. It
	// is NOT a passing verdict, and Unavailable says which case it is.
	OvercommitmentNotApplicable OvercommitmentState = "not-applicable"
)

// Reasons an over-commitment comparison is not applicable. They are distinct
// because they resolve differently: the first is a deliberate operator choice
// and needs no action, the second is a gap in what this process can read.
const (
	// OvercommitmentUnavailableUnboundedLimit means the server reports no
	// limit for the tier. The resources are still named and their declared sum
	// still reported; only the verdict is withheld.
	OvercommitmentUnavailableUnboundedLimit = "account-limit-unbounded"
	// OvercommitmentUnavailableUnknownLimit means the limit could not be
	// determined — AccountInfo was unreadable, or the value the server returned
	// is ambiguous.
	OvercommitmentUnavailableUnknownLimit = "account-limit-unknown"
)

// AccountLimitReader is the narrow JetStream capability the account comparison
// needs. jetstream.JetStream satisfies it.
//
// It is deliberately SEPARATE from StreamLister rather than folded into it.
// Account limits are an optional enrichment: a collector that cannot read them
// still publishes a complete resource inventory with the comparison reported
// unknown, and widening the listing interface would make every test fake and
// every future lister implementation carry a method the inventory can do
// without.
type AccountLimitReader interface {
	AccountInfo(ctx context.Context) (*jetstream.AccountInfo, error)
}

// AccountTierLimits is the server's own per-tier accounting: the ceiling for
// each tier and the usage the server measures against it.
//
// Usage comes from the ACCOUNT rather than from summing the inventory's
// per-resource usage. The account number is what the limit is actually enforced
// against, and it is the number `nats account info` shows; a sum over resources
// drifts from it (replication, per-stream overhead) and would produce a headroom
// an operator cannot reconcile with the server.
type AccountTierLimits struct {
	// Known reports that AccountInfo was read. False means every tier's limit
	// reports unknown — never unbounded.
	Known bool

	// Unavailable names why the limits could not be read. Empty when Known.
	Unavailable string

	// MaxMemory and MaxStore are the raw account ceilings as AccountLimits
	// reports them, including its -1 unlimited sentinel.
	MaxMemory int64
	MaxStore  int64

	// MemoryUsed and StoreUsed are the account's own per-tier usage.
	MemoryUsed int64
	StoreUsed  int64
}

// AccountTierLimitsFrom reads one AccountInfo response. A nil response is
// UNKNOWN rather than a zero-valued account: a zeroed limit set would classify
// as an account bounded at zero bytes and report every tier permanently
// over-committed.
func AccountTierLimitsFrom(info *jetstream.AccountInfo) AccountTierLimits {
	if info == nil {
		return UnknownAccountTierLimits("the server returned no account information")
	}
	return AccountTierLimits{
		Known:      true,
		MaxMemory:  info.Limits.MaxMemory,
		MaxStore:   info.Limits.MaxStore,
		MemoryUsed: clampToInt64(info.Memory),
		StoreUsed:  clampToInt64(info.Store),
	}
}

// UnknownAccountTierLimits is the explicit unreadable account, naming why.
func UnknownAccountTierLimits(reason string) AccountTierLimits {
	return AccountTierLimits{Unavailable: reason}
}

// TierComparison is one storage tier's account-level capacity picture.
type TierComparison struct {
	// Tier is the storage tier this row compares within. Memory and file have
	// separate account limits; the unknown tier has none at all.
	Tier StorageTier `json:"tier"`

	// Limit is the account ceiling for this tier paired with the account's
	// usage against it — bounded, unbounded, or unknown, on the same three-state
	// model every capacity in this capability uses.
	Limit Capacity `json:"limit"`

	// DeclaredBytes is the sum of the configured bounds of the BOUNDED
	// resources in this tier. Unbounded and unknown-capacity resources
	// contribute nothing, which makes this a FLOOR rather than a total whenever
	// UnboundedResources or UnknownResources is non-zero.
	DeclaredBytes int64 `json:"declared_bytes"`

	// The resource counts behind DeclaredBytes, published so a consumer can see
	// how much of the tier the sum actually covers.
	BoundedResources   int `json:"bounded_resources"`
	UnboundedResources int `json:"unbounded_resources"`
	UnknownResources   int `json:"unknown_resources"`

	// State is the verdict. Report-only: nothing rejects, throttles, or
	// degrades because a tier is over-committed.
	State OvercommitmentState `json:"state"`

	// Unavailable names why State is not applicable. Empty otherwise.
	Unavailable string `json:"unavailable,omitempty"`

	// Growth, Projection and Pressure are the tier ceiling's own capacity
	// picture, measured against the account's usage of this tier rather than
	// against any resource's declaration.
	//
	// They are here because a tier limit is a CEILING like any other, and an
	// unbounded resource has no other one. Over-commitment answers a different
	// question — "do the declarations filed against this tier fit inside it" —
	// and answers it about declared bounds, so a tier can be comfortably
	// within-limit while the bytes actually stored are about to exhaust it. Both
	// are published because they fail independently.
	//
	// The rate is measured across the account row's own retained history, which
	// carries this tier's usage at every past collection (storage_report.go). It
	// is the tier's rate, not the sum of any subset of resource rates: resources
	// appear and disappear between collections, and a sum over the ones that
	// happen to be present would move for reasons that are not growth.
	Growth     Growth     `json:"growth"`
	Projection Projection `json:"projection"`
	Pressure   Pressure   `json:"pressure"`
}

// AccountReport is the account-level half of the storage report: what each
// storage tier's ceiling is, and whether the declarations filed against it fit.
type AccountReport struct {
	// CollectedAt is when the account was read. Stamped by the publisher from
	// the inventory this report was derived with, so it always matches the
	// resource rows it was computed alongside.
	CollectedAt time.Time `json:"collected_at"`

	// ProducedBy names the process that produced this report.
	ProducedBy string `json:"produced_by"`

	// Tiers holds file and memory always, and the unknown tier only when some
	// resource could not be filed under either. An always-present unknown row
	// would publish a permanently empty comparison for a limit that does not
	// exist.
	Tiers []TierComparison `json:"tiers"`

	// LimitsUnavailable names why the account limits could not be read. Empty
	// when they were.
	LimitsUnavailable string `json:"limits_unavailable,omitempty"`
}

// TierFor returns one tier's comparison. ok is false when the report carries no
// row for that tier, which is a real answer rather than a zero comparison.
func (r AccountReport) TierFor(tier StorageTier) (TierComparison, bool) {
	for _, comparison := range r.Tiers {
		if comparison.Tier == tier {
			return comparison, true
		}
	}
	return TierComparison{}, false
}

// DeriveAccountReport compares the inventory's declared bounds against the
// account limits, WITHIN each tier.
//
// It is derived at collection time, where both inputs are in hand, so the
// published report carries the verdict rather than leaving every operator
// surface to recompute it from the rows and risk disagreeing.
func DeriveAccountReport(resources []StorageResource, limits AccountTierLimits) AccountReport {
	tiers := map[StorageTier]*TierComparison{
		TierFile:   {Tier: TierFile, Limit: accountTierCapacity(limits.MaxStore, limits.StoreUsed, limits.Known)},
		TierMemory: {Tier: TierMemory, Limit: accountTierCapacity(limits.MaxMemory, limits.MemoryUsed, limits.Known)},
	}

	for _, resource := range resources {
		tier := resource.Tier
		comparison, ok := tiers[tier]
		if !ok {
			// A resource the server declined to describe has no readable tier,
			// so it counts against NEITHER account limit. Filing it under a
			// default would silently move bytes into a comparison they do not
			// belong to.
			comparison = &TierComparison{
				Tier:  TierUnknown,
				Limit: UnknownCapacity(),
			}
			if existing, seen := tiers[TierUnknown]; seen {
				comparison = existing
			} else {
				tiers[TierUnknown] = comparison
			}
		}

		switch resource.Bytes.State {
		case CapacityBounded:
			if limit, has := resource.Bytes.Limit(); has {
				comparison.DeclaredBytes += limit
				comparison.BoundedResources++
			}
		case CapacityUnbounded:
			comparison.UnboundedResources++
		case CapacityUnknown:
			comparison.UnknownResources++
		}
	}

	report := AccountReport{
		LimitsUnavailable: limits.Unavailable,
		Tiers:             make([]TierComparison, 0, len(tiers)),
	}
	// Fixed order rather than map order: the report is read by an operator and
	// diffed between collections.
	for _, tier := range []StorageTier{TierFile, TierMemory, TierUnknown} {
		comparison, ok := tiers[tier]
		if !ok {
			continue
		}
		comparison.State, comparison.Unavailable = compareTier(*comparison)
		report.Tiers = append(report.Tiers, *comparison)
	}
	return report
}

// compareTier renders one tier's verdict.
//
// The not-applicable arms come FIRST and unconditionally. An unbounded or
// unreadable limit has no comparison to make, and answering "within limit"
// there would report the most common configuration in existence — a server with
// no account limits — as a capacity check that passed.
func compareTier(comparison TierComparison) (OvercommitmentState, string) {
	switch comparison.Limit.State {
	case CapacityUnbounded:
		return OvercommitmentNotApplicable, OvercommitmentUnavailableUnboundedLimit
	case CapacityUnknown:
		return OvercommitmentNotApplicable, OvercommitmentUnavailableUnknownLimit
	}

	limit, ok := comparison.Limit.Limit()
	if !ok {
		return OvercommitmentNotApplicable, OvercommitmentUnavailableUnknownLimit
	}
	if comparison.DeclaredBytes > limit {
		return OvercommitmentOver, ""
	}
	// Declaring exactly the limit is not over-committing it.
	return OvercommitmentWithin, ""
}

// accountTierCapacity classifies ONE tier's account ceiling.
//
// It is separate from NewCapacity on purpose: the two speak DIFFERENT unlimited
// encodings, and sharing one classifier would misreport whichever caller lost.
// A stream's MaxBytes uses 0 and -1 interchangeably for "no limit", while
// AccountLimits uses the -1 sentinel alone.
//
// A ZERO account limit is therefore reported UNKNOWN rather than unbounded, and
// that is the deliberate conservative reading of an ambiguous value. Zero is
// either a real ceiling of zero bytes (`max_file_store: 0` disables the tier) or
// the placeholder a tiered JWT account leaves in the top-level limit view while
// its real limits live under per-replication tiers. Reading it as unbounded
// would hide a tier that can store nothing; reading it as bounded-at-zero would
// report every tiered account as permanently, catastrophically over-committed.
// Unknown is the only reading that is wrong in neither direction, and it
// suppresses the verdict rather than fabricating one.
func accountTierCapacity(limit, used int64, known bool) Capacity {
	switch {
	case !known:
		return UnknownCapacity()
	case limit < 0:
		return Capacity{State: CapacityUnbounded, Used: &used}
	case limit == 0:
		return Capacity{State: CapacityUnknown}
	default:
		return Capacity{State: CapacityBounded, ConfiguredLimit: &limit, Used: &used}
	}
}

// readAccountTierLimits reads the account's limits through the narrow optional
// capability, returning UNKNOWN limits rather than an error.
//
// An unreadable account limit costs the over-commitment comparison; it must not
// cost the resource inventory, which is the larger and more actionable half. A
// monitoring surface that fails its whole collection because one enrichment call
// failed is a worse bug than the blindness it fixes.
func readAccountTierLimits(ctx context.Context, lister StreamLister) AccountTierLimits {
	reader, ok := lister.(AccountLimitReader)
	if !ok {
		return UnknownAccountTierLimits(
			"this JetStream client does not expose account limits")
	}
	info, err := reader.AccountInfo(ctx)
	if err != nil {
		return UnknownAccountTierLimits(fmt.Sprintf("account info unavailable: %v", err))
	}
	return AccountTierLimitsFrom(info)
}
