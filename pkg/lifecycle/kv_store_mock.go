package lifecycle

import (
	"context"
	"sync"
	"time"
)

// kvMockStore is an in-memory kvStore implementation for unit tests.
// Production wiring uses kvNATSStore (kv_store_nats.go); the mock
// exists so Manager behavior can be tested without spinning up NATS
// for every package run.
//
// Concurrency model: a single sync.RWMutex protects the map +
// per-key revision counter. Manager operations against this store
// are safe to call from concurrent goroutines (tests cover the
// CAS-retry path).
//
// Restart-recovery and real-CAS-semantics tests live in the
// integration-test bucket (testcontainers + kvNATSStore) — the
// mock is for fast behavior-shape coverage, not for validating
// NATS-specific semantics like delete-marker tombstoning or
// revision-history depth limits.
type kvMockStore struct {
	mu      sync.RWMutex
	entries map[string]*kvMockEntry
	// history tracks every write (Put/Update/Delete) per key in
	// chronological order. Mirrors jetstream's revision-stream
	// preservation; History() returns slice copies from this map.
	// Populated by appendHistory from Create/Update/Delete.
	history map[string][]kvEntry
	// nextRevision is a monotonically-increasing counter, NOT
	// per-key. Mirrors jetstream's stream-level sequence semantics
	// (every write across the bucket increments the global stream
	// sequence) so CAS-conflict tests see realistic revision
	// numbers between unrelated keys.
	nextRevision uint64
	// clock is the time source for kvEntry.CreatedAt. Tests can
	// override for deterministic timestamps; the default uses
	// time.Now (wallclock).
	clock func() time.Time
}

// kvMockEntry is the per-key in-memory record. Tracks value +
// revision + creation time + latest-revision time. A nil value
// represents a delete marker (so History could surface the tombstone
// if needed by the query-ops surface in the next commit).
//
// createdAt is IMMUTABLE after Create — mirrors real jetstream
// where the first-create timestamp is preserved across subsequent
// Update writes. revisionAt advances per write. The next commit's
// History query op uses createdAt to surface "when did this entity
// first appear" and revisionAt to surface "when was each revision
// written" — keeping them distinct here so the mock doesn't diverge
// from real jetstream semantics under integration-test rewrites.
type kvMockEntry struct {
	value      []byte
	revision   uint64
	createdAt  time.Time // immutable post-Create
	revisionAt time.Time // re-set on every write
	deleted    bool
}

// newKVMockStore constructs a fresh in-memory store. The optional
// clock argument is intended for tests that need deterministic
// timestamps; nil falls back to time.Now.
func newKVMockStore(clock func() time.Time) *kvMockStore {
	if clock == nil {
		clock = time.Now
	}
	return &kvMockStore{
		entries: make(map[string]*kvMockEntry),
		history: make(map[string][]kvEntry),
		clock:   clock,
	}
}

func (s *kvMockStore) Get(_ context.Context, key string) ([]byte, uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	if !ok || entry.deleted {
		return nil, 0, errKVKeyNotFound
	}
	// Defensive copy so callers mutating the returned slice can't
	// corrupt the store. Matches the contract a real KV connection
	// would provide (separate network payload per Get).
	out := make([]byte, len(entry.value))
	copy(out, entry.value)
	return out, entry.revision, nil
}

func (s *kvMockStore) Create(_ context.Context, key string, value []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[key]; ok && !entry.deleted {
		return 0, errKVKeyExists
	}
	s.nextRevision++
	stored := make([]byte, len(value))
	copy(stored, value)
	now := s.clock()
	s.entries[key] = &kvMockEntry{
		value:      stored,
		revision:   s.nextRevision,
		createdAt:  now,
		revisionAt: now,
	}
	// History snapshot — copy the value bytes so subsequent
	// Update mutations to the live entry don't retroactively
	// rewrite this revision's recorded value.
	histVal := make([]byte, len(stored))
	copy(histVal, stored)
	s.appendHistory(key, kvEntry{
		Key:       key,
		Value:     histVal,
		Revision:  s.nextRevision,
		CreatedAt: now,
	})
	return s.nextRevision, nil
}

func (s *kvMockStore) Update(_ context.Context, key string, value []byte, expectedRevision uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok || entry.deleted {
		return 0, errKVKeyNotFound
	}
	if entry.revision != expectedRevision {
		return 0, errKVRevisionMismatch
	}
	s.nextRevision++
	stored := make([]byte, len(value))
	copy(stored, value)
	entry.value = stored
	entry.revision = s.nextRevision
	entry.revisionAt = s.clock()
	// createdAt is intentionally NOT touched here — see the type
	// comment for the rationale.
	// History snapshot — see Create for the value-copy rationale.
	histVal := make([]byte, len(stored))
	copy(histVal, stored)
	s.appendHistory(key, kvEntry{
		Key:       key,
		Value:     histVal,
		Revision:  s.nextRevision,
		CreatedAt: entry.revisionAt,
	})
	return s.nextRevision, nil
}

func (s *kvMockStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok || entry.deleted {
		// Idempotent delete — already gone is success, matching
		// jetstream's tombstone-or-noop behavior.
		return nil
	}
	s.nextRevision++
	entry.value = nil
	entry.deleted = true
	entry.revision = s.nextRevision
	entry.revisionAt = s.clock()
	// Append a tombstone snapshot to history so History() can
	// surface the delete event. Real jetstream preserves the
	// delete marker in the revision stream; the mock has to
	// snapshot explicitly because the live entries map only
	// holds the latest state.
	s.appendHistory(key, kvEntry{
		Key:       key,
		Value:     nil,
		Revision:  s.nextRevision,
		CreatedAt: entry.revisionAt,
		IsDelete:  true,
	})
	return nil
}

// ListKeys returns all keys currently present (non-deleted) in
// sorted order. Sorted output keeps List's iteration deterministic
// across processes so paginated operator queries don't see flap.
func (s *kvMockStore) ListKeys(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.entries))
	for k, entry := range s.entries {
		if !entry.deleted {
			keys = append(keys, k)
		}
	}
	// Cheap stable order — sort.Strings rather than introducing
	// a sort import here when one's already in scope elsewhere.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys, nil
}

// History returns every revision of the given key in chronological
// order (oldest first). Includes delete-marker entries with
// IsDelete=true. Returns errKVKeyNotFound if the key has never
// existed (no history records at all).
func (s *kvMockStore) History(_ context.Context, key string) ([]kvEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hist, ok := s.history[key]
	if !ok || len(hist) == 0 {
		return nil, errKVKeyNotFound
	}
	// Defensive copy so callers iterating concurrently with
	// future writes don't see the slice grow under them.
	out := make([]kvEntry, len(hist))
	copy(out, hist)
	return out, nil
}

// Watch returns a channel emitting every KV write that lands
// after the snapshot replay. The mock's snapshot is the current
// live state (non-deleted entries); subsequent writes go through
// the watcher.
//
// The mock does NOT track watchers across calls — each call gets
// a fresh channel that delivers the snapshot then closes on ctx.
// Real jetstream's Watch handles concurrent watchers via NATS
// pub-sub; integration tests cover that behavior. For unit tests,
// the snapshot-then-close shape is enough to verify Manager.Watch
// processes entries correctly.
func (s *kvMockStore) Watch(ctx context.Context) (<-chan kvEntry, error) {
	out := make(chan kvEntry, 16)
	// Snapshot under read-lock; deliver outside the lock so
	// listeners don't pin the mock during slow consumers.
	s.mu.RLock()
	snapshot := make([]kvEntry, 0, len(s.entries))
	for k, entry := range s.entries {
		if entry.deleted {
			continue
		}
		// Defensive value copy — same contract as Get.
		val := make([]byte, len(entry.value))
		copy(val, entry.value)
		snapshot = append(snapshot, kvEntry{
			Key:       k,
			Value:     val,
			Revision:  entry.revision,
			CreatedAt: entry.revisionAt,
		})
	}
	s.mu.RUnlock()

	go func() {
		defer close(out)
		for _, entry := range snapshot {
			select {
			case out <- entry:
			case <-ctx.Done():
				return
			}
		}
		// Live updates beyond the snapshot are NOT delivered by
		// the mock — production NATS would stream them. The
		// integration-test slice covers that path. For unit
		// tests, the snapshot is enough to verify Manager.Watch's
		// decode + dispatch shape.
		<-ctx.Done()
	}()
	return out, nil
}

// appendHistory records a snapshot of the current entry state in
// the per-key history slice. Called from Create/Update/Delete to
// build up the revision stream. Caller holds s.mu (write lock).
func (s *kvMockStore) appendHistory(key string, entry kvEntry) {
	if s.history == nil {
		s.history = make(map[string][]kvEntry)
	}
	s.history[key] = append(s.history[key], entry)
}
