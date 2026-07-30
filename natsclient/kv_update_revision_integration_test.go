//go:build integration

package natsclient

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UpdateWithRetryRev must return the EXACT revision its own commit produced.
//
// The whole reason it exists is that a post-hoc `Get` is not equivalent:
// another writer can commit between the CAS and the re-read, and the re-read
// then reports that writer's revision. A caller attributing the reported
// revision to its own write — the rule engine's per-rule feedback-loop tracker
// does exactly that — would then record someone else's revision and, because
// the skip consumes it once and returns true, silently DROP that writer's
// genuine change.
//
// This test makes the external writer win that window deterministically: it
// commits between the CAS returning and the live read being taken.
func TestUpdateWithRetryRev_ReturnsOwnCommitNotALaterWriters(t *testing.T) {
	ctx := context.Background()
	testClient := NewTestClient(t, WithKV())
	bucket, err := testClient.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-update-rev", History: 5,
	})
	require.NoError(t, err)
	kv := testClient.Client.NewKVStore(bucket)

	const key = "subject.one"
	_, err = kv.Put(ctx, key, []byte(`{"v":0}`))
	require.NoError(t, err)

	ownRevision, err := kv.UpdateWithRetryRev(ctx, key, func([]byte) ([]byte, error) {
		return []byte(`{"v":1}`), nil
	})
	require.NoError(t, err)
	require.NotZero(t, ownRevision, "a committed write must yield a revision")

	// Another writer commits AFTER our CAS. This is the window.
	externalRevision, err := kv.Put(ctx, key, []byte(`{"v":2}`))
	require.NoError(t, err)
	require.Greater(t, externalRevision, ownRevision, "fixture sanity: the external write moved the revision")

	entry, err := kv.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, externalRevision, entry.Revision,
		"fixture sanity: a post-hoc re-read now returns the EXTERNAL writer's revision")
	assert.NotEqual(t, entry.Revision, ownRevision,
		"which is precisely why the committed revision must come from the CAS, not a re-read")
}

// The create path (key absent) must report its revision too — UpdateWithRetry
// creates when the key does not exist, and that commit is just as attributable.
func TestUpdateWithRetryRev_ReportsRevisionOnCreatePath(t *testing.T) {
	ctx := context.Background()
	testClient := NewTestClient(t, WithKV())
	bucket, err := testClient.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-update-rev-create", History: 5,
	})
	require.NoError(t, err)
	kv := testClient.Client.NewKVStore(bucket)

	revision, err := kv.UpdateWithRetryRev(ctx, "fresh.key", func(current []byte) ([]byte, error) {
		require.Empty(t, current, "fixture sanity: the key must be absent")
		return []byte(`{"born":true}`), nil
	})
	require.NoError(t, err)
	assert.NotZero(t, revision, "a create is a commit and must report its revision")

	entry, err := kv.Get(ctx, "fresh.key")
	require.NoError(t, err)
	assert.Equal(t, entry.Revision, revision)
}

// A closure that fails commits nothing, so there is no revision to report.
func TestUpdateWithRetryRev_ReportsNoRevisionWhenNothingCommits(t *testing.T) {
	ctx := context.Background()
	testClient := NewTestClient(t, WithKV())
	bucket, err := testClient.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-update-rev-fail", History: 5,
	})
	require.NoError(t, err)
	kv := testClient.Client.NewKVStore(bucket)

	_, err = kv.Put(ctx, "subject.one", []byte(`{"v":0}`))
	require.NoError(t, err)

	revision, err := kv.UpdateWithRetryRev(ctx, "subject.one", func([]byte) ([]byte, error) {
		return nil, assert.AnError
	})
	require.Error(t, err)
	assert.Zero(t, revision, "nothing committed, so nothing may be attributed")
}
