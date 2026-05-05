// Package kvbuckettest provides an in-memory KVBucket implementation for unit tests.
//
// MockKVBucket is the single shared implementation. It replaces the per-package
// mock types (mockKVBucket, casKVBucket) that were duplicated across the rule
// package tests, each carrying 15+ stub methods for the full jetstream.KeyValue
// interface. MockKVBucket implements natsclient.KVBucket — the narrow interface
// trackers actually use — so there are no unused stubs.
//
// CAS-conflict simulation is opt-in via SetUpdateHook, keeping simple tests
// free of CAS wiring complexity.
package kvbuckettest

import (
	"context"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
)

// UpdateHook is a function called by Update before the write. It receives the
// key and expected revision and returns the revision to use for the conflict
// check. Return a revision different from lastRevision to simulate a CAS
// conflict (natsclient.ErrKeyExists); return lastRevision to allow the write.
//
// Use SetUpdateHook to inject a hook for tests that exercise the CAS retry path.
type UpdateHook func(key string, lastRevision uint64) (effectiveRevision uint64)

// MockKVBucket is a concurrent-safe, in-memory natsclient.KVBucket for unit tests.
// It supports all KVBucket operations including Watch with the KV-twofer
// end-of-bootstrap nil delimiter.
//
// CAS-conflict simulation: set a hook with SetUpdateHook. Without a hook, Update
// behaves correctly (write succeeds when revision matches, ErrKeyExists otherwise).
type MockKVBucket struct {
	mu         sync.Mutex
	data       map[string][]byte
	revision   map[string]uint64
	nextRev    uint64
	bucketName string
	updateHook UpdateHook

	// watchers holds channels for active Watch subscriptions.
	watchers []chan natsclient.KVEntry
}

// NewMockKVBucket returns an initialised MockKVBucket with bucket name "mock-bucket".
func NewMockKVBucket() *MockKVBucket {
	return &MockKVBucket{
		data:       make(map[string][]byte),
		revision:   make(map[string]uint64),
		nextRev:    1,
		bucketName: "mock-bucket",
	}
}

// NewMockKVBucketNamed returns an initialised MockKVBucket with a custom bucket name.
// Use this when tests need to distinguish bucket names in log output or assertions.
func NewMockKVBucketNamed(name string) *MockKVBucket {
	m := NewMockKVBucket()
	m.bucketName = name
	return m
}

// SetUpdateHook installs a hook called before each Update. The hook receives
// the key and the caller's lastRevision and returns the revision used for the
// conflict check. Return a value != lastRevision to inject a CAS conflict.
func (m *MockKVBucket) SetUpdateHook(hook UpdateHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateHook = hook
}

// Get retrieves the current value for key. Returns natsclient.ErrKeyNotFound
// when the key does not exist.
func (m *MockKVBucket) Get(_ context.Context, key string) (natsclient.KVEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	val, ok := m.data[key]
	if !ok {
		return natsclient.KVEntry{}, natsclient.ErrKeyNotFound
	}
	// Copy to prevent the caller from mutating internal state.
	cp := make([]byte, len(val))
	copy(cp, val)
	return natsclient.KVEntry{Key: key, Value: cp, Revision: m.revision[key]}, nil
}

// Put creates or overwrites key unconditionally. Returns the new revision.
func (m *MockKVBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	rev := m.nextRev
	m.revision[key] = rev
	m.nextRev++
	m.notifyWatchersLocked(key, cp, rev)
	return rev, nil
}

// Update performs a compare-and-swap write. Returns natsclient.ErrKeyExists when
// lastRevision does not match the stored revision. When a hook is installed via
// SetUpdateHook, the hook's returned revision is used for the conflict check,
// allowing tests to inject CAS conflicts on demand.
func (m *MockKVBucket) Update(_ context.Context, key string, value []byte, lastRevision uint64) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	checkRevision := lastRevision
	if m.updateHook != nil {
		checkRevision = m.updateHook(key, lastRevision)
	}

	cur := m.revision[key]
	if cur != checkRevision {
		return 0, natsclient.ErrKeyExists
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	rev := m.nextRev
	m.revision[key] = rev
	m.nextRev++
	m.notifyWatchersLocked(key, cp, rev)
	return rev, nil
}

// Delete removes key. Returns natsclient.ErrKeyNotFound when the key is absent.
func (m *MockKVBucket) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.data[key]; !ok {
		return natsclient.ErrKeyNotFound
	}
	delete(m.data, key)
	delete(m.revision, key)
	return nil
}

// Keys returns all live keys in the bucket. Returns natsclient.ErrNoKeysFound
// when the bucket is empty, mirroring NATS JetStream behaviour.
func (m *MockKVBucket) Keys(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.data) == 0 {
		return nil, natsclient.ErrNoKeysFound
	}
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// Watch returns a KVWatcher that immediately delivers all current values,
// then a zero KVEntry (Value==nil) as the end-of-bootstrap delimiter, and
// then any subsequent Put/Update changes — the KV-twofer pattern described
// in docs/concepts/02-kv-twofer.md.
//
// The pattern argument is accepted for interface compatibility but is not
// filtered — all keys are delivered. Tests that need pattern-filtered watches
// should structure their data to avoid noise keys.
func (m *MockKVBucket) Watch(_ context.Context, _ string) (natsclient.KVWatcher, error) {
	m.mu.Lock()

	// Buffer is current snapshot size + 1 for the delimiter.
	ch := make(chan natsclient.KVEntry, len(m.data)+1)

	// Deliver the current snapshot.
	for key, val := range m.data {
		cp := make([]byte, len(val))
		copy(cp, val)
		ch <- natsclient.KVEntry{Key: key, Value: cp, Revision: m.revision[key]}
	}
	// Deliver the end-of-bootstrap nil delimiter.
	ch <- natsclient.KVEntry{}

	// Register for live updates.
	m.watchers = append(m.watchers, ch)

	m.mu.Unlock()

	return &mockKVWatcher{bucket: m, ch: ch}, nil
}

// Bucket returns the bucket name supplied at construction.
func (m *MockKVBucket) Bucket() string {
	return m.bucketName
}

// notifyWatchersLocked delivers a KVEntry to all active watchers. Called
// under m.mu. Watchers with full channels are dropped (non-blocking send).
func (m *MockKVBucket) notifyWatchersLocked(key string, value []byte, revision uint64) {
	entry := natsclient.KVEntry{Key: key, Value: value, Revision: revision}
	live := m.watchers[:0]
	for _, ch := range m.watchers {
		select {
		case ch <- entry:
			live = append(live, ch)
		default:
			// Channel full — watcher is too slow; drop it.
		}
	}
	m.watchers = live
}

// removeWatcher unregisters ch from the live watcher list.
func (m *MockKVBucket) removeWatcher(ch chan natsclient.KVEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := m.watchers[:0]
	for _, w := range m.watchers {
		if w != ch {
			live = append(live, w)
		}
	}
	m.watchers = live
}

// mockKVWatcher implements natsclient.KVWatcher for MockKVBucket.
type mockKVWatcher struct {
	bucket *MockKVBucket
	ch     chan natsclient.KVEntry
	once   sync.Once
}

// Updates returns the entry channel. The channel carries KV-twofer end-of-
// bootstrap semantics: a zero KVEntry (Value==nil) is delivered after the
// initial snapshot, followed by live updates.
func (w *mockKVWatcher) Updates() <-chan natsclient.KVEntry {
	return w.ch
}

// Stop deregisters the watcher from live updates and closes the channel.
func (w *mockKVWatcher) Stop() error {
	w.once.Do(func() {
		w.bucket.removeWatcher(w.ch)
		close(w.ch)
	})
	return nil
}
