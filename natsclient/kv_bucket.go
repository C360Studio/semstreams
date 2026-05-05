// Package natsclient - KVBucket is the minimal KV interface used by rule-processor trackers.
//
// KVBucket is a deliberately narrow slice of jetstream.KeyValue: only the
// methods that ScheduleTracker, WindowTracker, and StateTracker actually call.
// Keeping it narrow means test mocks only stub six methods instead of the
// full 15+ method interface, and callers that hold their own jetstream.KeyValue
// can be wrapped for free via WrapKV.
//
// Sentinel errors (ErrKeyNotFound, ErrKeyExists, ErrNoKeysFound) are re-exported
// so callers can write errors.Is checks without importing the jetstream package.
package natsclient

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

// Sentinel errors re-exported from jetstream so callers of KVBucket do not
// need a direct jetstream import for errors.Is comparisons.
var (
	// ErrKeyNotFound is returned by Get and Delete when the key does not exist.
	ErrKeyNotFound = jetstream.ErrKeyNotFound

	// ErrKeyExists is returned by Update when the supplied revision does not
	// match the current revision (optimistic-concurrency conflict). Also
	// returned by Put when a concurrent creator races a first-write.
	ErrKeyExists = jetstream.ErrKeyExists

	// ErrNoKeysFound is returned by Keys when the bucket is empty.
	ErrNoKeysFound = jetstream.ErrNoKeysFound
)

// KVEntry is re-used from kv.go (Key, Value, Revision). It is the concrete
// result type for KVBucket.Get — a simple value object, not an interface.
// (Definition lives in kv.go; do not redeclare here.)

// KVWatcher is the narrow watcher interface used by rule-processor trackers.
// Updates returns a channel that delivers one KVEntry per change, then a zero
// KVEntry (Value==nil) as the end-of-bootstrap delimiter once all current
// values have been delivered (the KV-twofer pattern). Callers detect the
// delimiter via entry.Value == nil.
type KVWatcher interface {
	// Updates returns the change channel. The channel is closed when the watcher
	// is stopped or the underlying connection is lost.
	Updates() <-chan KVEntry

	// Stop cancels the watcher and releases its subscription.
	Stop() error
}

// KVBucket is the minimal read/write/watch interface required by rule-processor
// trackers (ScheduleTracker, WindowTracker, StateTracker). It is satisfied by
// the adapter returned from WrapKV and by the test double in kvbuckettest.
type KVBucket interface {
	// Get retrieves the current value for key. Returns ErrKeyNotFound when
	// the key does not exist.
	Get(ctx context.Context, key string) (KVEntry, error)

	// Put creates or overwrites key. Last writer wins; no revision check.
	// Returns the new revision on success.
	Put(ctx context.Context, key string, value []byte) (revision uint64, err error)

	// Update performs a compare-and-swap write. Returns ErrKeyExists when
	// lastRevision does not match the stored revision (concurrent write).
	// Returns the new revision on success.
	Update(ctx context.Context, key string, value []byte, lastRevision uint64) (revision uint64, err error)

	// Delete removes key. Missing keys are silently accepted (idempotent)
	// by callers that need idempotency; the raw error is ErrKeyNotFound.
	Delete(ctx context.Context, key string) error

	// Keys returns all live keys in the bucket. Returns ErrNoKeysFound when
	// the bucket is empty.
	Keys(ctx context.Context) ([]string, error)

	// Watch subscribes to changes matching pattern (NATS wildcard syntax).
	// The returned KVWatcher delivers current values followed by a nil
	// delimiter and then live updates — the KV-twofer end-of-bootstrap
	// convention documented in docs/concepts/02-kv-twofer.md.
	Watch(ctx context.Context, pattern string) (KVWatcher, error)

	// Bucket returns the underlying KV bucket name. Used for logging and
	// diagnostics; not used for any business logic.
	Bucket() string
}

// WrapKV adapts a jetstream.KeyValue to the KVBucket interface. It is the
// only conversion point — production code obtains a jetstream.KeyValue from
// js.KeyValue / js.CreateKeyValue and then calls WrapKV before passing it
// to a tracker constructor.
//
// WrapKV(nil) returns nil so degraded-startup paths that receive a nil bucket
// preserve the nil-tolerant behaviour already documented on each tracker.
func WrapKV(kv jetstream.KeyValue) KVBucket {
	if kv == nil {
		return nil
	}
	return &kvBucketAdapter{kv: kv}
}

// kvBucketAdapter implements KVBucket by delegating to a jetstream.KeyValue.
type kvBucketAdapter struct {
	kv jetstream.KeyValue
}

func (a *kvBucketAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
	entry, err := a.kv.Get(ctx, key)
	if err != nil {
		return KVEntry{}, err
	}
	return KVEntry{
		Key:      entry.Key(),
		Value:    entry.Value(),
		Revision: entry.Revision(),
	}, nil
}

func (a *kvBucketAdapter) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	return a.kv.Put(ctx, key, value)
}

func (a *kvBucketAdapter) Update(ctx context.Context, key string, value []byte, lastRevision uint64) (uint64, error) {
	return a.kv.Update(ctx, key, value, lastRevision)
}

func (a *kvBucketAdapter) Delete(ctx context.Context, key string) error {
	return a.kv.Delete(ctx, key)
}

func (a *kvBucketAdapter) Keys(ctx context.Context) ([]string, error) {
	return a.kv.Keys(ctx)
}

func (a *kvBucketAdapter) Watch(ctx context.Context, pattern string) (KVWatcher, error) {
	w, err := a.kv.Watch(ctx, pattern)
	if err != nil {
		return nil, err
	}
	return &kvWatcherAdapter{w: w}, nil
}

func (a *kvBucketAdapter) Bucket() string {
	return a.kv.Bucket()
}

// kvWatcherAdapter bridges jetstream.KeyWatcher to KVWatcher. It converts
// jetstream.KeyValueEntry updates to KVEntry values and preserves the
// nil-entry end-of-bootstrap delimiter that the KV-twofer pattern relies on.
type kvWatcherAdapter struct {
	w       jetstream.KeyWatcher
	updates chan KVEntry
}

// Updates returns a channel of KVEntry values. The channel carries a zero
// KVEntry (Value==nil) as the end-of-bootstrap delimiter after all current
// values have been delivered, mirroring the raw jetstream.KeyWatcher nil
// convention documented in docs/concepts/02-kv-twofer.md.
//
// The adapter lazily starts a forwarding goroutine on the first call to
// Updates so the conversion is transparent to callers.
func (w *kvWatcherAdapter) Updates() <-chan KVEntry {
	if w.updates == nil {
		raw := w.w.Updates()
		ch := make(chan KVEntry, cap(raw))
		go func() {
			defer close(ch)
			for entry := range raw {
				if entry == nil {
					// Deliver the zero KVEntry as the end-of-bootstrap delimiter.
					ch <- KVEntry{}
					continue
				}
				ch <- KVEntry{
					Key:      entry.Key(),
					Value:    entry.Value(),
					Revision: entry.Revision(),
				}
			}
		}()
		w.updates = ch
	}
	return w.updates
}

// Stop cancels the watcher, closing the Updates channel.
func (w *kvWatcherAdapter) Stop() error {
	return w.w.Stop()
}
