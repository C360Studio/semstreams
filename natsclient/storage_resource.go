// storage_resource.go carries the REPORTED SHAPE of the account storage
// inventory (storage-observability): what one resource looks like, what one
// capacity axis looks like, and the state vocabularies each is allowed to speak.
// The collector that fills these in lives in storage_inventory.go.
//
// Every type here exists to keep two facts that operators act on differently
// from collapsing into one value:
//
//   - bounded / unbounded / unknown capacity. An unreadable limit is not an
//     absent limit, and an absent limit is not a safe one. A resource reported
//     healthy because its capacity could not be read is worse than one not
//     reported at all, because it manufactures confidence.
//   - attributed / unattributed / not-applicable ownership. "This bucket
//     escaped the descriptor catalog" is a finding; "an ordinary stream has no
//     owner registry" is not. An empty owner string cannot say which.
//
// The numeric fields are pointers and the read accessors return an ok flag for
// the same reason: an absent measurement must be ABSENT, never a zero a
// downstream reader can mistake for one.

package natsclient

import "time"

// StorageTier is the JetStream storage tier backing a resource. JetStream keeps
// SEPARATE memory and file account limits and this repository's own streams
// span both, so the tier is recorded per resource and must never be guessed:
// summing across tiers produces a number that means nothing.
type StorageTier string

// Storage tiers.
const (
	// TierFile is file-backed storage.
	TierFile StorageTier = "file"
	// TierMemory is memory-backed storage.
	TierMemory StorageTier = "memory"
	// TierUnknown is a resource whose tier could not be read, because the
	// server declined to describe it. It is reported rather than defaulted:
	// defaulting would silently file the resource under one account limit's
	// comparison.
	TierUnknown StorageTier = "unknown"
)

// CapacityState discriminates the three — not two — states a capacity axis can
// be in. Collapsing any pair produces the phantom-signal class this capability
// exists to remove: an unreadable limit is not an absent limit, and an absent
// limit is not a safe one.
type CapacityState string

// Capacity states.
const (
	// CapacityBounded means a finite configured limit was read. Only this state
	// carries a limit value, and only this state supports headroom or
	// time-to-threshold projection.
	CapacityBounded CapacityState = "bounded"
	// CapacityUnbounded means the resource is deliberately unlimited. Usage is
	// still observable; headroom is not, because there is no bound to have
	// headroom against.
	CapacityUnbounded CapacityState = "unbounded"
	// CapacityUnknown means the limit or the usage could not be determined.
	// It is NOT unlimited, NOT zero, and NOT healthy, and it suppresses
	// projection rather than emitting a fabricated one.
	CapacityUnknown CapacityState = "unknown"
)

// AttributionState discriminates why a resource does or does not name an owner.
//
// Unattributed and not-applicable are DIFFERENT facts and only one of them is a
// finding. Logical ownership is defined for KV buckets, through the descriptor
// catalog; there is no owner registry for ordinary streams or ObjectStores, and
// the inventory enumerates the ACCOUNT, so a resource another process declared
// has no declaration this process could read. Reporting both as an empty owner
// would say "the framework has no owner concept here" and "this bucket escaped
// the catalog" in the same breath.
type AttributionState string

// Attribution states.
const (
	// AttributionAttributed means the descriptor catalog declares an owner for
	// this resource's bucket, and Owner carries it.
	AttributionAttributed AttributionState = "attributed"
	// AttributionUnattributed means the resource IS a KV bucket, so an owner is
	// meaningful, but the catalog declares none. This is the finding state: a
	// bucket outside framework ownership, reported rather than omitted or
	// force-fit.
	AttributionUnattributed AttributionState = "unattributed"
	// AttributionNotApplicable means ownership is not defined for this kind of
	// resource at all. Not a finding.
	AttributionNotApplicable AttributionState = "not-applicable"
)

// Capacity is one bounded dimension of a resource — a configured limit paired
// with observed usage.
//
// The numeric fields are pointers on purpose. An absent value must be ABSENT,
// never a zero that a downstream reader can mistake for a real measurement:
// "limit 0" is the exact shape that turns an unreadable resource into a
// reported-healthy one. Read them through Limit and Usage, which return an ok
// flag, and build them through NewCapacity, which is the only classifier.
type Capacity struct {
	State CapacityState `json:"state"`

	// ConfiguredLimit is the declared finite bound. Non-nil ONLY when State is
	// CapacityBounded.
	ConfiguredLimit *int64 `json:"configured_limit,omitempty"`

	// Used is observed usage. Nil when State is CapacityUnknown, because an
	// unreadable usage must not read back as zero.
	Used *int64 `json:"used,omitempty"`
}

// NewCapacity classifies one capacity axis. It is the ONE place a limit/usage
// pair becomes a state, so no caller can invent a fourth interpretation.
//
// known reports whether the resource's configuration AND state were readable.
// They arrive together in a single listing entry today; a future collector that
// reads them from separate calls MUST pass false when either fails, because the
// requirement is "limit OR usage cannot be determined".
//
// A non-positive limit is JetStream's unlimited encoding (both 0 and the -1
// sentinel appear in practice) and yields CapacityUnbounded — never a bounded
// zero.
func NewCapacity(limit, used int64, known bool) Capacity {
	if !known {
		return Capacity{State: CapacityUnknown}
	}
	if limit <= 0 {
		return Capacity{State: CapacityUnbounded, Used: &used}
	}
	return Capacity{State: CapacityBounded, ConfiguredLimit: &limit, Used: &used}
}

// UnknownCapacity is the explicit unreadable capacity.
func UnknownCapacity() Capacity { return Capacity{State: CapacityUnknown} }

// Limit returns the configured finite bound. ok is false unless the capacity is
// bounded, so an unbounded or unknown resource can never yield a limit number.
func (c Capacity) Limit() (int64, bool) {
	if c.ConfiguredLimit == nil {
		return 0, false
	}
	return *c.ConfiguredLimit, true
}

// Usage returns observed usage. ok is false when the capacity is unknown.
func (c Capacity) Usage() (int64, bool) {
	if c.Used == nil {
		return 0, false
	}
	return *c.Used, true
}

// Bounded reports whether headroom and time-to-threshold are projectable for
// this axis at all. Both an unbounded and an unknown capacity answer false, for
// different reasons the State field keeps distinct.
func (c Capacity) Bounded() bool { return c.State == CapacityBounded }

// StorageResource is one account storage resource as the inventory sees it.
type StorageResource struct {
	// Name is the physical JetStream stream name. Always present: a listing
	// entry the collector cannot name fails the collection rather than becoming
	// an unlookupable row.
	Name string `json:"name"`

	// Kind is the logical resource the physical name backs. Derived from the
	// name alone, so it is available even for a resource the server declines to
	// describe.
	Kind ResourceKind `json:"kind"`

	// Bucket is the logical bucket name behind a backing stream, with exactly
	// one reserved prefix stripped. Empty for an ordinary stream.
	Bucket string `json:"bucket,omitempty"`

	// Attribution says whether an owner is defined, undeclared, or not a
	// meaningful question for this kind of resource.
	Attribution AttributionState `json:"attribution"`

	// Owner is the logical owner as the descriptor catalog declares it.
	// Non-empty only when Attribution is AttributionAttributed.
	Owner string `json:"owner,omitempty"`

	// Tier is the storage tier this resource's usage counts against.
	Tier StorageTier `json:"tier"`

	// Bytes and Messages are the two capacity axes JetStream bounds.
	Bytes    Capacity `json:"bytes"`
	Messages Capacity `json:"messages"`
}

// Attributed reports whether the resource resolved to a declared owner. It
// reads the Attribution state rather than testing Owner for emptiness, so a
// not-applicable resource is never mistaken for an escaped bucket.
func (r StorageResource) Attributed() bool { return r.Attribution == AttributionAttributed }

// Undescribable reports whether the server declined to describe this resource —
// it appeared in the account's name listing but the info listing omitted it,
// which the server does for any stream carrying an offline reason.
//
// Derived rather than stored so a hand-built row cannot claim otherwise: such a
// resource is exactly the one with no readable tier and no readable capacity on
// either axis.
func (r StorageResource) Undescribable() bool {
	return r.Tier == TierUnknown &&
		r.Bytes.State == CapacityUnknown &&
		r.Messages.State == CapacityUnknown
}

// StorageInventory is one account-wide collection result.
//
// CollectedAt is when the RESOURCES were read, not when the report was
// rendered: a degraded inventory keeps the timestamp its data actually came
// from, so an operator reading a stale report can tell how stale it is.
type StorageInventory struct {
	// ProducedBy names the process that collected this inventory. Never empty,
	// because a fleet of processes each polling account-wide produces reports
	// that cannot be reconciled without it.
	ProducedBy string `json:"produced_by"`

	// CollectedAt is when Resources were read from the account. Zero when no
	// collection has ever succeeded.
	CollectedAt time.Time `json:"collected_at"`

	// Resources is the complete account listing, deduplicated and sorted by
	// physical name. It is never a partial listing: a walk that fails part way
	// through leaves the previous result in place rather than reporting a
	// subset of the account as if it were all of it.
	Resources []StorageResource `json:"resources"`

	// Stale reports that the most recent collection attempt did not succeed, so
	// Resources and CollectedAt describe an earlier moment. It is also true
	// before the first successful collection, when Resources is legitimately
	// empty but the account is NOT known to be empty.
	Stale bool `json:"stale"`

	// StaleSince is when the most recent failed attempt happened. A POINTER for
	// the same reason the capacity numbers are: `omitempty` is a no-op on a
	// time.Time, so a value field would publish a zero timestamp on a healthy
	// inventory and invite a consumer to read it as a real failure time.
	StaleSince *time.Time `json:"stale_since,omitempty"`

	// StaleReason explains the failure in operator terms.
	StaleReason string `json:"stale_reason,omitempty"`
}
