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
	return nil
}
