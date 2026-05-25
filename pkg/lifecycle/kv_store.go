package lifecycle

import (
	"context"
	"errors"
	"time"
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

	// ListKeys returns all keys currently present in the bucket
	// (delete-marker entries excluded). Used by Manager.List's
	// linear-scan path. Streaming preferred over snapshot for
	// large buckets — see jetstream.KeyValue.ListKeys vs Keys —
	// but the v1 implementation collects the snapshot into a
	// slice for simplicity. The scaling-cliff escape hatch (v2
	// secondary index) is documented in ADR-047 § KV indexing.
	ListKeys(ctx context.Context) ([]string, error)

	// History returns every revision of key in chronological order
	// (oldest first). Used by Manager.History to reconstruct
	// TransitionEvent records from KV revision metadata. Entries
	// with IsDelete=true represent tombstone events (Delete calls);
	// callers filter them depending on whether they want only
	// successful writes or the full event stream.
	//
	// Returns errKVKeyNotFound if the key has no history at all
	// (never existed). An empty history for a deleted key returns
	// the delete-marker entry (jetstream preserves the tombstone
	// in the revision stream).
	History(ctx context.Context, key string) ([]kvEntry, error)

	// Watch streams real-time updates for every key in the bucket.
	// The returned channel emits one kvEntry per KV write
	// (including deletes via IsDelete=true); the channel closes
	// when ctx is canceled OR the watcher errors out (caller
	// treats both as "stop iterating").
	//
	// Bootstrap semantics: the first batch of entries delivered
	// are the CURRENT values for all keys (snapshot replay),
	// followed by live updates. Callers that only want forward
	// updates filter the bootstrap batch in their loop.
	//
	// No per-key filtering at this layer — Manager.Watch already
	// wants every instance of a workflow (= every key in the
	// bucket), and filtering wider/narrower would couple the
	// abstraction to NATS subject-wildcard semantics. Filter
	// caller-side on kvEntry.Key if needed.
	Watch(ctx context.Context) (<-chan kvEntry, error)
}

// kvEntry is the package-internal KV entry value type. Mirrors the
// fields Manager.List/Watch/History need from jetstream.KeyValueEntry
// without forcing the kvStore abstraction (or its mock) to depend
// on jetstream types.
type kvEntry struct {
	// Key is the key the entry lives under.
	Key string

	// Value is the most recent value bytes. Nil for delete-marker
	// entries returned by History or Watch.
	Value []byte

	// Revision is the unique sequence number for this entry.
	// Monotonic per-bucket in jetstream; the mock mirrors this
	// (single counter shared across all keys).
	Revision uint64

	// CreatedAt is when this specific revision was written.
	// Sourced from jetstream entry metadata in production;
	// kvMockStore uses its configurable clock. Authoritative
	// across restarts and clock skew.
	CreatedAt time.Time

	// IsDelete is true when the entry represents a delete-marker
	// (tombstone) rather than a put. History and Watch surface
	// these; Manager.History uses them to render terminal events
	// in the entity's audit trail.
	IsDelete bool
}

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
