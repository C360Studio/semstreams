package lifecycle

import (
	"context"
	"errors"
)

// kvStore is the internal abstraction the Manager uses for KV
// operations against a single NATS KV bucket. Scoped per-bucket
// so call sites don't pass the bucket name on every method.
//
// The interface is INTERNAL (lowercase) on purpose — apps wire up
// the Manager via NewManager and never interact with kvStore
// directly. Two implementations live in this package:
//   - kvMockStore (kv_store_mock.go) — in-memory map for unit tests
//   - kvNATSStore (kv_store_nats.go) — thin wrapper over
//     jetstream.KeyValue for production
//
// Method semantics deliberately mirror jetstream.KeyValue's
// surface (Get / Create / Update with revision-CAS / Delete /
// ListKeys / History / Watch) so the NATS adapter is a thin
// translation, NOT a re-implementation of KV semantics.
type kvStore interface {
	// Get returns the latest value for key with its current revision.
	// Returns ErrKVKeyNotFound when the key does not exist (callers
	// branch on this to surface ErrEntityNotFound at the Manager
	// layer).
	Get(ctx context.Context, key string) (value []byte, revision uint64, err error)

	// Create writes value at key with create-only semantics: returns
	// ErrKVKeyExists when the key already exists. Returns the
	// revision the new entry was written at.
	Create(ctx context.Context, key string, value []byte) (revision uint64, err error)

	// Update writes value at key only if the current revision
	// matches expectedRevision (CAS). Returns ErrKVRevisionMismatch
	// when the current revision is different — caller must Get-then-
	// retry with the fresh revision. Returns the new revision on
	// success.
	Update(ctx context.Context, key string, value []byte, expectedRevision uint64) (newRevision uint64, err error)

	// Delete places a delete marker at key. Subsequent Get on the
	// key returns ErrKVKeyNotFound (matching jetstream semantics —
	// the entry is tombstoned, History still sees the deletion in
	// the revision stream).
	Delete(ctx context.Context, key string) error
}

// (kvEntry, the value type used by Watch/History, lands in the next
// commit on this branch alongside the query-ops surface that
// consumes it. Keeping it out of this commit avoids unused-type
// lint warnings on the foundation slice.)

// Package-private KV sentinel errors. The kvStore interface returns
// these; the Manager translates them into the public errors.go
// sentinels (ErrEntityNotFound, ErrInvalidTransition etc.) so apps
// only see the public surface.
var (
	// ErrKVKeyNotFound is returned by kvStore.Get when the key
	// has no value (never existed or was deleted). Mirrors
	// jetstream.ErrKeyNotFound.
	errKVKeyNotFound = errors.New("lifecycle: KV key not found")

	// ErrKVKeyExists is returned by kvStore.Create when the key
	// already exists. Mirrors jetstream.ErrKeyExists.
	errKVKeyExists = errors.New("lifecycle: KV key already exists")

	// ErrKVRevisionMismatch is returned by kvStore.Update when the
	// CAS revision check fails. Manager.Update retries on this
	// (Get + mutator + Update) up to a bounded retry budget.
	errKVRevisionMismatch = errors.New("lifecycle: KV revision mismatch (CAS conflict)")
)
