package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// accountAwareLister is a StreamLister that ALSO answers AccountInfo, which is
// what a real jetstream.JetStream does. The plain fakeLister deliberately does
// not, so the two fakes together cover both sides of the optional-capability
// bridge.
type accountAwareLister struct {
	*fakeLister
	info  *jetstream.AccountInfo
	err   error
	calls int
}

func (a *accountAwareLister) AccountInfo(_ context.Context) (*jetstream.AccountInfo, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	return a.info, nil
}

func (a *accountAwareLister) source() StreamListerSource {
	return func() (StreamLister, error) { return a, nil }
}

func accountTestCollector(t *testing.T, source StreamListerSource) *StorageInventoryCollector {
	t.Helper()
	collector, err := NewStorageInventoryCollector(source, StorageInventoryConfig{
		OwnerResolver: func(string) string { return "" },
		ProducedBy:    "unit-test",
	})
	require.NoError(t, err)
	return collector
}

func fileStream(name string, maxBytes int64) *jetstream.StreamInfo {
	return &jetstream.StreamInfo{
		Config: jetstream.StreamConfig{
			Name:     name,
			Storage:  jetstream.FileStorage,
			MaxBytes: maxBytes,
		},
		State: jetstream.StreamState{Bytes: 128},
	}
}

func memoryStream(name string, maxBytes int64) *jetstream.StreamInfo {
	return &jetstream.StreamInfo{
		Config: jetstream.StreamConfig{
			Name:     name,
			Storage:  jetstream.MemoryStorage,
			MaxBytes: maxBytes,
		},
		State: jetstream.StreamState{Bytes: 64},
	}
}

func listerFor(infos ...*jetstream.StreamInfo) *fakeLister {
	return &fakeLister{
		nextInfos: func() *fakeStreamInfoLister { return &fakeStreamInfoLister{infos: infos} },
		nextNames: func() *fakeStreamNameLister { return &fakeStreamNameLister{names: infoNames(infos)} },
	}
}

// TestCollect_ComparesDeclaredBoundsAgainstTheAccountLimitPerTier is task 4.5
// through the collector: the comparison is derived from the same collection
// that produced the rows, so no operator surface has to recompute it.
func TestCollect_ComparesDeclaredBoundsAgainstTheAccountLimitPerTier(t *testing.T) {
	lister := &accountAwareLister{
		fakeLister: listerFor(
			fileStream("LOGS", 900<<20),
			fileStream("AUDIT", 900<<20),
			memoryStream("HEALTH", 64<<20),
		),
		info: &jetstream.AccountInfo{
			Tier: jetstream.Tier{
				Memory: 1 << 20,
				Store:  2 << 20,
				Limits: jetstream.AccountLimits{MaxMemory: 8 << 30, MaxStore: 1 << 30},
			},
		},
	}

	inventory, err := accountTestCollector(t, lister.source()).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, inventory.Resources, 3)

	file, ok := inventory.Account.TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, OvercommitmentOver, file.State)
	assert.Equal(t, int64(1800<<20), file.DeclaredBytes)

	memory, ok := inventory.Account.TierFor(TierMemory)
	require.True(t, ok)
	assert.Equal(t, OvercommitmentWithin, memory.State)
	assert.Equal(t, int64(64<<20), memory.DeclaredBytes,
		"the memory tier's sum contains only memory-backed resources")
}

// TestCollect_UnboundedAccountLimitIsNotApplicable is task 4.6 through the
// collector, on the path a stock server and testcontainers actually take.
func TestCollect_UnboundedAccountLimitIsNotApplicable(t *testing.T) {
	lister := &accountAwareLister{
		fakeLister: listerFor(fileStream("LOGS", 1<<40)),
		info: &jetstream.AccountInfo{
			Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxMemory: -1, MaxStore: -1}},
		},
	}

	inventory, err := accountTestCollector(t, lister.source()).Collect(context.Background())
	require.NoError(t, err)

	file, ok := inventory.Account.TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, CapacityUnbounded, file.Limit.State)
	assert.Equal(t, OvercommitmentNotApplicable, file.State)
	assert.NotEqual(t, OvercommitmentWithin, file.State)
}

// TestCollect_AccountLimitsAreOptionalEnrichment proves the bridge fails soft.
// A client that does not expose AccountInfo costs the comparison and NOTHING
// else: the resource inventory, which is the larger and more actionable half,
// is still complete.
func TestCollect_AccountLimitsAreOptionalEnrichment(t *testing.T) {
	plain := listerFor(fileStream("LOGS", 1<<30), memoryStream("HEALTH", 0))

	inventory, err := accountTestCollector(t, plain.source()).Collect(context.Background())
	require.NoError(t, err, "an unreadable account limit must not fail the collection")
	assert.Len(t, inventory.Resources, 2)
	assert.False(t, inventory.Stale)

	assert.NotEmpty(t, inventory.Account.LimitsUnavailable)
	for _, comparison := range inventory.Account.Tiers {
		assert.Equal(t, CapacityUnknown, comparison.Limit.State, comparison.Tier)
		assert.Equal(t, OvercommitmentNotApplicable, comparison.State, comparison.Tier)
	}
}

// TestCollect_AccountInfoFailureReportsUnknownAndKeepsTheInventory is the same
// soft failure through a real error rather than a missing method.
func TestCollect_AccountInfoFailureReportsUnknownAndKeepsTheInventory(t *testing.T) {
	lister := &accountAwareLister{
		fakeLister: listerFor(fileStream("LOGS", 1<<30)),
		err:        errors.New("connection refused"),
	}

	inventory, err := accountTestCollector(t, lister.source()).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, inventory.Resources, 1)
	assert.False(t, inventory.Stale)
	assert.Contains(t, inventory.Account.LimitsUnavailable, "connection refused")

	file, ok := inventory.Account.TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, CapacityUnknown, file.Limit.State,
		"a failed read is unknown, never unbounded")
}

// TestReadAccountTierLimits_UnreadableIsNotKnown pins the flag the whole
// three-state model at the account level hangs off.
//
// The derived report is NOT enough to prove this on its own, and that is the
// point of asserting the flag directly: a failure path that wrongly reported
// Known would still land on "unknown" today, because its zero MaxStore takes
// the ambiguous-zero arm by accident. The moment a server answers an error
// alongside a partially populated response, an accidentally-Known result would
// classify tiers from numbers nobody could read.
func TestReadAccountTierLimits_UnreadableIsNotKnown(t *testing.T) {
	t.Run("the call failed", func(t *testing.T) {
		lister := &accountAwareLister{fakeLister: listerFor(), err: errors.New("connection refused")}
		limits := readAccountTierLimits(context.Background(), lister)

		assert.False(t, limits.Known, "a failed read is never a read")
		assert.Contains(t, limits.Unavailable, "connection refused")
	})

	t.Run("the client cannot answer at all", func(t *testing.T) {
		limits := readAccountTierLimits(context.Background(), listerFor())

		assert.False(t, limits.Known, "a client with no account capability has read nothing")
		assert.NotEmpty(t, limits.Unavailable)
	})

	t.Run("a successful read is known", func(t *testing.T) {
		lister := &accountAwareLister{
			fakeLister: listerFor(),
			info:       &jetstream.AccountInfo{Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxStore: 1 << 30}}},
		}
		limits := readAccountTierLimits(context.Background(), lister)

		assert.True(t, limits.Known)
		assert.Empty(t, limits.Unavailable)
		assert.Equal(t, int64(1<<30), limits.MaxStore)
	})
}

// TestCollect_AccountLimitsAreReadOncePerCollection keeps the enrichment inside
// the cost bound the inventory is built to respect: one account call per
// collection, never one per resource.
func TestCollect_AccountLimitsAreReadOncePerCollection(t *testing.T) {
	lister := &accountAwareLister{
		fakeLister: listerFor(fileStream("A", 1<<30), fileStream("B", 1<<30), fileStream("C", 1<<30)),
		info:       &jetstream.AccountInfo{Tier: jetstream.Tier{Limits: jetstream.AccountLimits{MaxStore: 1 << 40}}},
	}
	collector := accountTestCollector(t, lister.source())

	_, err := collector.Collect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, lister.calls)

	_, err = collector.Collect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, lister.calls, "one call per collection, not per resource")
}
