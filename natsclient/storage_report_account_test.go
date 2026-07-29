package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// value returns a key's current value, or false when the key has no revisions
// or its newest revision is a delete marker.
func (s *fakeReportStore) value(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.history[key]
	if len(entries) == 0 {
		return nil, false
	}
	last := entries[len(entries)-1]
	if last.op != jetstream.KeyValuePut {
		return nil, false
	}
	return last.value, true
}

func (s *fakeReportStore) deletedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletes...)
}

func accountInventoryAt(at time.Time, account AccountReport, resources ...StorageResource) StorageInventory {
	inv := inventoryAt(at, resources...)
	inv.Account = account
	return inv
}

func decodeAccountRow(t *testing.T, store *fakeReportStore) AccountReport {
	t.Helper()
	value, ok := store.value(StorageAccountReportKey)
	require.True(t, ok, "the account row must be published")
	var report AccountReport
	require.NoError(t, json.Unmarshal(value, &report))
	return report
}

// TestPublish_AccountRowCarriesTheTierComparison keeps the per-tier comparison
// inside the produced truth. Every operator surface reads this bucket, so a
// comparison computed anywhere else is a second producer that can disagree.
func TestPublish_AccountRowCarriesTheTierComparison(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())

	at := time.Now().UTC().Truncate(time.Second)
	account := DeriveAccountReport(
		[]StorageResource{tieredResource("LOGS", TierFile, 900<<20, 1)},
		AccountTierLimitsFrom(&jetstream.AccountInfo{
			Tier: jetstream.Tier{Store: 5, Limits: jetstream.AccountLimits{MaxStore: 1 << 20, MaxMemory: -1}},
		}))

	result, err := publisher.Publish(context.Background(),
		accountInventoryAt(at, account, tieredResource("LOGS", TierFile, 900<<20, 1)))
	require.NoError(t, err)
	assert.True(t, result.AccountPublished)
	assert.Equal(t, 1, result.Published,
		"the resource count stays a resource count; the account row is its own field")

	row := decodeAccountRow(t, store)
	assert.Equal(t, at, row.CollectedAt.UTC(),
		"the account row carries the collection it was derived alongside")
	assert.Equal(t, "unit-test", row.ProducedBy)

	file, ok := row.TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, OvercommitmentOver, file.State)
	assert.Equal(t, int64(900<<20), file.DeclaredBytes)

	memory, ok := row.TierFor(TierMemory)
	require.True(t, ok)
	assert.Equal(t, OvercommitmentNotApplicable, memory.State,
		"an unbounded tier limit publishes as not-applicable, not as satisfied")
}

// TestStorageAccountReportKey_CannotCollideWithAResourceKey is the guard on the
// reserved key. A resource key is always exactly ONE key token — a JetStream
// stream name may not contain a dot, and the opaque codec emits a single token
// too — so a key carrying a dot is unreachable from any resource name.
func TestStorageAccountReportKey_CannotCollideWithAResourceKey(t *testing.T) {
	require.NoError(t, ValidateKVLiteralKey(StorageAccountReportKey),
		"the reserved key must itself be a legal KV key")
	require.Contains(t, StorageAccountReportKey, ".",
		"the separation rests on the dot; without it the guard is only a convention")

	for _, name := range []string{
		"LOGS", "KV_ENTITY_STATES", "OBJ_CONTENT", "_account", "tiers",
		"_account_tiers", "x1_5f6163636f756e74", strings.Repeat("A", 200),
		"weird$name", "plus+name", "STORAGE_REPORT",
	} {
		key, err := StorageReportKey(name)
		require.NoError(t, err, name)
		assert.NotEqual(t, StorageAccountReportKey, key,
			"resource %q must not address the reserved account row", name)
		assert.NotContains(t, key, ".",
			"a resource key is one token; a dot in it would break the reservation")
	}
}

// TestPublish_ReclaimNeverDeletesTheAccountRow is the interlock between the two
// key kinds. Reclamation deletes every key the current collection did not name,
// and the account row is named by no resource — so without an explicit claim it
// would be published and deleted on every single collection.
func TestPublish_ReclaimNeverDeletesTheAccountRow(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	ctx := context.Background()

	at := time.Now().UTC()
	inv := accountInventoryAt(at, AccountReport{Tiers: []TierComparison{{Tier: TierFile}}},
		tieredResource("LOGS", TierFile, 1<<30, 1))

	_, err := publisher.Publish(ctx, inv)
	require.NoError(t, err)

	// A second collection with the same resources: nothing is gone, so nothing
	// may be reclaimed.
	inv.CollectedAt = at.Add(time.Minute)
	result, err := publisher.Publish(ctx, inv)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Deleted)
	assert.NotContains(t, store.deletedKeys(), StorageAccountReportKey)

	// And when a resource DOES disappear, the account row still survives the
	// reclamation that removes it.
	gone := accountInventoryAt(at.Add(2*time.Minute), AccountReport{})
	result, err = publisher.Publish(ctx, gone)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Deleted)
	assert.NotContains(t, store.deletedKeys(), StorageAccountReportKey)
	_, ok := store.value(StorageAccountReportKey)
	assert.True(t, ok, "the account row outlives the resources it summarizes")
}

// TestPublish_StaleInventoryPublishesNoAccountRow keeps the account row on the
// same freshness rule as every other row: last-good is not a new observation.
func TestPublish_StaleInventoryPublishesNoAccountRow(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())

	stale := accountInventoryAt(time.Now(), AccountReport{Tiers: []TierComparison{{Tier: TierFile}}})
	stale.Stale = true

	result, err := publisher.Publish(context.Background(), stale)
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.False(t, result.AccountPublished)
	_, ok := store.value(StorageAccountReportKey)
	assert.False(t, ok)
}

// TestPublish_AccountRowFailureDoesNotLoseTheResourceRows keeps one failed
// write from shrinking the report: the failure is reported, the rest publishes.
func TestPublish_AccountRowFailureDoesNotLoseTheResourceRows(t *testing.T) {
	store := newFakeReportStore(10)
	store.putErrForKey = map[string]error{StorageAccountReportKey: errors.New("boom")}
	publisher := newTestPublisher(t, store, defaultSource())

	result, err := publisher.Publish(context.Background(),
		accountInventoryAt(time.Now(), AccountReport{Tiers: []TierComparison{{Tier: TierFile}}},
			tieredResource("LOGS", TierFile, 1<<30, 1)))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, 1, result.Published, "the resource row still landed")
	assert.False(t, result.AccountPublished)
	assert.NotContains(t, store.deletedKeys(), StorageAccountReportKey,
		"a failed account write is not a decision that the account is gone")
}
