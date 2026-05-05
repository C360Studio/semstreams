// Package rule — thin shim re-exporting the shared test mock.
//
// The previous 217-line file implemented a standalone in-memory KV mock
// satisfying the full jetstream.KeyValue interface (15+ methods, ~9 of them
// stub "not implemented" returns). That type has been replaced by
// kvbuckettest.MockKVBucket, which implements the narrower natsclient.KVBucket
// interface — no unused stubs.
//
// This shim keeps the package-private names (mockKVBucket / newMockKVBucket)
// so that the 40+ existing call sites in schedule_tracker_test.go,
// stateful_evaluator_test.go, caller_condition_test.go, etc. require no edits.
package rule

import (
	"context"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/natsclient/kvbuckettest"
)

// mockKVBucket is a thin alias so existing test call sites keep working.
// All behaviour lives in kvbuckettest.MockKVBucket.
type mockKVBucket = kvbuckettest.MockKVBucket

// newMockKVBucket creates an in-memory KVBucket for unit tests.
// Delegates to kvbuckettest.NewMockKVBucket().
func newMockKVBucket() *mockKVBucket {
	return kvbuckettest.NewMockKVBucket()
}

// mockKVEntry is retained so flakyKVBucket (in schedule_tracker_test.go) can
// return a concrete KVEntry without importing natsclient directly in the test.
// It is just a type alias for the value struct.
type mockKVEntry = natsclient.KVEntry

// compile-time guard: mockKVBucket must satisfy natsclient.KVBucket.
var _ natsclient.KVBucket = (*mockKVBucket)(nil)

// kvEntryFromBytes is a small helper for tests that need to build a KVEntry
// without going through a full Get round-trip.
func kvEntryFromBytes(key string, value []byte, rev uint64) natsclient.KVEntry {
	cp := make([]byte, len(value))
	copy(cp, value)
	return natsclient.KVEntry{Key: key, Value: cp, Revision: rev}
}

// Ensure the context import is used (some test files call Get with context.Background
// explicitly; the import here keeps vet clean).
var _ = context.Background
