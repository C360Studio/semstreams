package natsclient

import (
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tierOf finds one tier's comparison in a report. Absence is a test failure
// rather than a zero value, because a missing tier row and a tier row reporting
// nothing are exactly the two things this file exists to keep apart.
func tierIn(t *testing.T, report AccountReport, tier StorageTier) TierComparison {
	t.Helper()
	for _, comparison := range report.Tiers {
		if comparison.Tier == tier {
			return comparison
		}
	}
	require.FailNowf(t, "tier missing from the account report", "tier %q", tier)
	return TierComparison{}
}

func tieredResource(name string, tier StorageTier, limit, used int64) StorageResource {
	return StorageResource{
		Name:        name,
		Kind:        ResourceOrdinaryStream,
		Attribution: AttributionNotApplicable,
		Tier:        tier,
		Bytes:       NewCapacity(limit, used, true),
		Messages:    NewCapacity(0, 0, true),
	}
}

func tieredUnboundedResource(name string, tier StorageTier, used int64) StorageResource {
	return tieredResource(name, tier, 0, used)
}

func tierlessResource(name string) StorageResource {
	return StorageResource{
		Name:        name,
		Kind:        ResourceOrdinaryStream,
		Attribution: AttributionNotApplicable,
		Tier:        TierUnknown,
		Bytes:       UnknownCapacity(),
		Messages:    UnknownCapacity(),
	}
}

// TestAccountTierLimits_UnlimitedSentinel pins the encoding difference that
// makes a separate classifier necessary: AccountLimits says unlimited with -1
// ONLY, while a stream's MaxBytes says it with 0 or -1.
func TestAccountTierLimits_UnlimitedSentinel(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{
			Memory: 100,
			Store:  200,
			Limits: jetstream.AccountLimits{MaxMemory: -1, MaxStore: 4 << 30},
		},
	})
	require.True(t, limits.Known)

	report := DeriveAccountReport(nil, limits)

	memory := tierIn(t, report, TierMemory)
	assert.Equal(t, CapacityUnbounded, memory.Limit.State,
		"-1 is the AccountLimits sentinel for unlimited")
	used, ok := memory.Limit.Usage()
	require.True(t, ok)
	assert.Equal(t, int64(100), used)

	file := tierIn(t, report, TierFile)
	assert.Equal(t, CapacityBounded, file.Limit.State)
	limit, ok := file.Limit.Limit()
	require.True(t, ok)
	assert.Equal(t, int64(4<<30), limit)
}

// TestAccountTierLimits_ZeroIsUnknownNotUnbounded is the guard against the
// worst misreport available here. A tiered (JWT) account leaves the top-level
// limit view zeroed, and classifying that zero the way a stream's MaxBytes zero
// is classified would report an unlimited account as an account bounded at zero
// bytes — every tier instantly and permanently over-committed.
func TestAccountTierLimits_ZeroIsUnknownNotUnbounded(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: 0, MaxStore: 0}},
	})

	report := DeriveAccountReport(
		[]StorageResource{tieredResource("S", TierFile, 1<<30, 0)}, limits)

	file := tierIn(t, report, TierFile)
	assert.Equal(t, CapacityUnknown, file.Limit.State,
		"zero is ambiguous — a real zero ceiling or a tiered account's placeholder")
	assert.NotEqual(t, CapacityUnbounded, file.Limit.State)
	assert.Equal(t, OvercommitmentNotApplicable, file.State)
	assert.Equal(t, OvercommitmentUnavailableUnknownLimit, file.Unavailable)
}

// TestDeriveAccountReport_UnboundedLimitIsNotApplicableNotSatisfied is task 4.6
// stated as an assertion: the DEFAULT integration path (testcontainers reports
// -1) must not read as a comparison that passed.
func TestDeriveAccountReport_UnboundedLimitIsNotApplicableNotSatisfied(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: -1, MaxStore: -1}},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("BIG", TierFile, 1<<40, 0),
	}, limits)

	file := tierIn(t, report, TierFile)
	assert.Equal(t, CapacityUnbounded, file.Limit.State)
	assert.Equal(t, OvercommitmentNotApplicable, file.State,
		"an unbounded account limit answers the over-commitment question with 'not applicable'")
	assert.NotEqual(t, OvercommitmentWithin, file.State,
		"reporting 'within limit' against no limit manufactures confidence")
	assert.Equal(t, OvercommitmentUnavailableUnboundedLimit, file.Unavailable)
	assert.Equal(t, int64(1<<40), file.DeclaredBytes,
		"the declared sum is still reported; only the verdict is withheld")
}

func TestDeriveAccountReport_OverCommitmentIsPerTier(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{
			Memory: 1 << 20,
			Store:  2 << 20,
			Limits: jetstream.AccountLimits{MaxMemory: 8 << 30, MaxStore: 1 << 30},
		},
	})

	// File is over-committed on its own; memory is comfortably within. If the
	// two tiers were summed, memory's slack would hide the file tier's problem.
	report := DeriveAccountReport([]StorageResource{
		tieredResource("LOGS", TierFile, 900<<20, 10),
		tieredResource("AUDIT", TierFile, 900<<20, 10),
		tieredResource("HEALTH", TierMemory, 64<<20, 10),
	}, limits)

	file := tierIn(t, report, TierFile)
	assert.Equal(t, OvercommitmentOver, file.State)
	assert.Equal(t, int64(1800<<20), file.DeclaredBytes)
	assert.Equal(t, 2, file.BoundedResources)

	memory := tierIn(t, report, TierMemory)
	assert.Equal(t, OvercommitmentWithin, memory.State)
	assert.Equal(t, int64(64<<20), memory.DeclaredBytes,
		"the memory tier's sum must contain only memory-backed resources")
	assert.Equal(t, 1, memory.BoundedResources)
}

// TestDeriveAccountReport_TiersAreNeverSummed is the same invariant approached
// from the failure direction: a file tier that is within its limit must stay
// within it no matter how large the memory tier's declarations are.
func TestDeriveAccountReport_TiersAreNeverSummed(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: 1 << 30, MaxStore: 1 << 30}},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("SMALL_FILE", TierFile, 1<<20, 0),
		tieredResource("HUGE_MEMORY", TierMemory, 4<<30, 0),
	}, limits)

	assert.Equal(t, OvercommitmentWithin, tierIn(t, report, TierFile).State,
		"a memory declaration must not push the file tier over its own limit")
	assert.Equal(t, OvercommitmentOver, tierIn(t, report, TierMemory).State)
}

// TestDeriveAccountReport_UnboundedResourcesAreCountedNotSummed keeps the
// declared sum honest: an unbounded resource contributes no number, so it must
// not silently contribute zero either — it is counted, and the count is what
// tells an operator the sum is a floor.
func TestDeriveAccountReport_UnboundedResourcesAreCountedNotSummed(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: -1, MaxStore: 1 << 30}},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("BOUND", TierFile, 100<<20, 0),
		tieredUnboundedResource("FREE", TierFile, 500<<20),
	}, limits)

	file := tierIn(t, report, TierFile)
	assert.Equal(t, int64(100<<20), file.DeclaredBytes)
	assert.Equal(t, 1, file.BoundedResources)
	assert.Equal(t, 1, file.UnboundedResources)
	assert.Equal(t, OvercommitmentWithin, file.State)
}

// TestDeriveAccountReport_UnknownTierResourcesJoinNoAccountTier is the
// misfiling guard: a resource the server declined to describe has no readable
// tier, so it counts against neither account limit and appears in its own row
// instead of being defaulted into one.
func TestDeriveAccountReport_UnknownTierResourcesJoinNoAccountTier(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: 1 << 30, MaxStore: 1 << 30}},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("KNOWN", TierFile, 1<<20, 0),
		tierlessResource("OFFLINE"),
	}, limits)

	assert.Equal(t, 1, tierIn(t, report, TierFile).BoundedResources)
	assert.Equal(t, 0, tierIn(t, report, TierFile).UnknownResources)
	assert.Equal(t, 0, tierIn(t, report, TierMemory).BoundedResources)

	unknown := tierIn(t, report, TierUnknown)
	assert.Equal(t, 1, unknown.UnknownResources)
	assert.Equal(t, CapacityUnknown, unknown.Limit.State,
		"there is no account limit for a tier nobody could read")
	assert.Equal(t, OvercommitmentNotApplicable, unknown.State)
}

// TestDeriveAccountReport_UnknownTierRowIsOmittedWhenEmpty keeps the report
// from carrying a permanently empty row: the file and memory tiers exist on
// every account, the unreadable one exists only when something is unreadable.
func TestDeriveAccountReport_UnknownTierRowIsOmittedWhenEmpty(t *testing.T) {
	report := DeriveAccountReport(
		[]StorageResource{tieredResource("S", TierFile, 1<<20, 0)},
		AccountTierLimitsFrom(&jetstream.AccountInfo{}))

	for _, comparison := range report.Tiers {
		assert.NotEqual(t, TierUnknown, comparison.Tier)
	}
	assert.Len(t, report.Tiers, 2, "file and memory always; unknown only when populated")
}

// TestDeriveAccountReport_UnreadableLimitsReportUnknownNotUnbounded is the
// third state doing its job at the account level: a failed AccountInfo call
// must not read as "no limit".
func TestDeriveAccountReport_UnreadableLimitsReportUnknownNotUnbounded(t *testing.T) {
	limits := UnknownAccountTierLimits("account info unavailable: connection refused")
	require.False(t, limits.Known)

	report := DeriveAccountReport(
		[]StorageResource{tieredResource("S", TierFile, 1<<20, 0)}, limits)

	assert.Contains(t, report.LimitsUnavailable, "connection refused")
	for _, comparison := range report.Tiers {
		assert.Equal(t, CapacityUnknown, comparison.Limit.State, comparison.Tier)
		assert.Equal(t, OvercommitmentNotApplicable, comparison.State, comparison.Tier)
		assert.Equal(t, OvercommitmentUnavailableUnknownLimit, comparison.Unavailable)
	}
}

// TestAccountTierLimitsFrom_NilInfoIsUnknown covers the defensive path: a
// server response the client parsed into nothing must not become a zeroed
// "bounded at zero" account.
func TestAccountTierLimitsFrom_NilInfoIsUnknown(t *testing.T) {
	limits := AccountTierLimitsFrom(nil)
	assert.False(t, limits.Known)
	assert.NotEmpty(t, limits.Unavailable)
}

// TestDeriveAccountReport_UsageComesFromTheAccountNotTheResourceSum pins where
// the tier's usage number comes from. The server's own per-tier accounting is
// what its limits are enforced against; summing per-resource usage would drift
// from it (replicas, per-stream overhead) and produce a headroom nobody can
// reconcile with `nats account info`.
func TestDeriveAccountReport_UsageComesFromTheAccountNotTheResourceSum(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{
			Store:  7 << 20,
			Limits: jetstream.AccountLimits{MaxStore: 1 << 30},
		},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("A", TierFile, 1<<20, 1),
		tieredResource("B", TierFile, 1<<20, 2),
	}, limits)

	used, ok := tierIn(t, report, TierFile).Limit.Usage()
	require.True(t, ok)
	assert.Equal(t, int64(7<<20), used, "the account's own number, not 1+2")
}

func TestDeriveAccountReport_DeclaredSumEqualToLimitIsWithin(t *testing.T) {
	limits := AccountTierLimitsFrom(&jetstream.AccountInfo{
		Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxStore: 2 << 20}},
	})

	report := DeriveAccountReport([]StorageResource{
		tieredResource("A", TierFile, 1<<20, 0),
		tieredResource("B", TierFile, 1<<20, 0),
	}, limits)

	assert.Equal(t, OvercommitmentWithin, tierIn(t, report, TierFile).State,
		"declaring exactly the account limit is not over-committing it")
}
