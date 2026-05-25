package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// kvNATSStore is the production kvStore implementation — a thin
// wrapper over jetstream.KeyValue. Most of the body is type
// translation from jetstream's sentinel errors to the package-
// internal errKV* sentinels so the Manager doesn't need to import
// jetstream directly.
//
// Bucket lifecycle: this adapter does NOT create the bucket.
// Apps provision their own KV buckets per their deployment
// topology — the harness only consumes them. newKVNATSStore
// resolves the existing bucket via natsclient.GetKeyValueBucket;
// missing buckets surface a wrapped error at Manager.Register time.
type kvNATSStore struct {
	bucket jetstream.KeyValue
	name   string // bucket name; kept for log/error messages
}

// newKVNATSStore opens an existing KV bucket via the natsclient.
// Returns an error if the bucket does not exist or the client is
// nil — both surface at Manager.Register time so wiring bugs
// don't bury themselves until first Get.
func newKVNATSStore(client *natsclient.Client, bucket string) (kvStore, error) {
	if client == nil {
		return nil, errors.New("lifecycle: newKVNATSStore requires non-nil natsclient.Client")
	}
	if bucket == "" {
		return nil, errors.New("lifecycle: newKVNATSStore requires non-empty bucket name")
	}
	// Resolve at Register time. Apps must provision the bucket
	// before any workflow registration; the harness doesn't manage
	// bucket creation (operator topology choice — replication,
	// history depth, max-bytes are per-bucket NATS-level decisions
	// the framework shouldn't make).
	kv, err := client.GetKeyValueBucket(context.Background(), bucket)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: open KV bucket %q: %w", bucket, err)
	}
	return &kvNATSStore{bucket: kv, name: bucket}, nil
}

func (s *kvNATSStore) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	entry, err := s.bucket.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			return nil, 0, errKVKeyNotFound
		}
		return nil, 0, fmt.Errorf("lifecycle: KV Get %s/%s: %w", s.name, key, err)
	}
	return entry.Value(), entry.Revision(), nil
}

func (s *kvNATSStore) Create(ctx context.Context, key string, value []byte) (uint64, error) {
	rev, err := s.bucket.Create(ctx, key, value)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			return 0, errKVKeyExists
		}
		return 0, fmt.Errorf("lifecycle: KV Create %s/%s: %w", s.name, key, err)
	}
	return rev, nil
}

func (s *kvNATSStore) Update(ctx context.Context, key string, value []byte, expectedRevision uint64) (uint64, error) {
	rev, err := s.bucket.Update(ctx, key, value, expectedRevision)
	if err != nil {
		// Classifier hierarchy — narrow to known shapes, refuse to
		// silently wrap transport/cancellation errors as CAS
		// conflicts (Manager.Update would otherwise retry a real
		// network timeout up to updateRetries=5 times, amplifying
		// one failed write into five).
		//
		// 1) Context errors propagate unwrapped — caller cancelled
		//    or timed out, retrying makes no sense.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, fmt.Errorf("lifecycle: KV Update %s/%s: %w", s.name, key, err)
		}
		// 2) Transport-level failures propagate unwrapped — these
		//    are NOT CAS conflicts; retrying without backoff just
		//    re-hammers a degraded connection.
		if errors.Is(err, nats.ErrTimeout) || errors.Is(err, nats.ErrNoResponders) ||
			errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, nats.ErrConnectionDraining) {
			return 0, fmt.Errorf("lifecycle: KV Update %s/%s (transport): %w", s.name, key, err)
		}
		// 3) Key gone during Update (deleted by concurrent writer)
		//    surfaces as ErrEntityNotFound at the Manager layer.
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			return 0, errKVKeyNotFound
		}
		// 4) Known CAS-shape errors — jetstream models CAS-fail-on-
		//    Update as ErrKeyExists internally (same "next revision
		//    write would collide" shape as Create on existing key).
		//    Plus substring fallback against the canonical "wrong
		//    last sequence" message jetstream emits today, so this
		//    classifier doesn't silently regress if the sentinel
		//    coverage drifts. TODO(integration-test): pin this
		//    against real jetstream in the testcontainers slice;
		//    swap to a dedicated sentinel if/when jetstream adds one.
		if errors.Is(err, jetstream.ErrKeyExists) ||
			strings.Contains(err.Error(), "wrong last sequence") {
			return 0, fmt.Errorf("%w: %w", errKVRevisionMismatch, err)
		}
		// 5) Default — surface the raw error wrapped, NOT classified
		//    as CAS. Unclassified errors hitting Manager.Update's
		//    retry loop will fail-fast (the retry only triggers on
		//    errors.Is(errKVRevisionMismatch)).
		return 0, fmt.Errorf("lifecycle: KV Update %s/%s: %w", s.name, key, err)
	}
	return rev, nil
}

func (s *kvNATSStore) Delete(ctx context.Context, key string) error {
	if err := s.bucket.Delete(ctx, key); err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			// Idempotent: already gone is not an error from the
			// Manager's perspective.
			return nil
		}
		return fmt.Errorf("lifecycle: KV Delete %s/%s: %w", s.name, key, err)
	}
	return nil
}

func (s *kvNATSStore) ListKeys(ctx context.Context) ([]string, error) {
	// jetstream's ListKeys returns a streaming KeyLister to avoid
	// holding the whole keyset in memory for huge buckets. v1
	// collects to a slice for the simpler Manager.List contract;
	// the v2 secondary-index work (ADR-047 § KV indexing) replaces
	// this scan path for high-cardinality workflows.
	lister, err := s.bucket.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: KV ListKeys %s: %w", s.name, err)
	}
	defer lister.Stop()
	var keys []string
	for key := range lister.Keys() {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *kvNATSStore) History(ctx context.Context, key string) ([]kvEntry, error) {
	entries, err := s.bucket.History(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, errKVKeyNotFound
		}
		return nil, fmt.Errorf("lifecycle: KV History %s/%s: %w", s.name, key, err)
	}
	out := make([]kvEntry, len(entries))
	for i, entry := range entries {
		// Operation distinguishes Put/Delete/Purge. Anything
		// other than Put is a tombstone-shaped event for History
		// consumers.
		op := entry.Operation()
		out[i] = kvEntry{
			Key:       entry.Key(),
			Value:     entry.Value(),
			Revision:  entry.Revision(),
			CreatedAt: entry.Created(),
			IsDelete:  op == jetstream.KeyValueDelete || op == jetstream.KeyValuePurge,
		}
	}
	return out, nil
}

func (s *kvNATSStore) Watch(ctx context.Context) (<-chan kvEntry, error) {
	watcher, err := s.bucket.WatchAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: KV WatchAll %s: %w", s.name, err)
	}
	out := make(chan kvEntry, 16)
	go func() {
		// Channel-close on goroutine exit; caller treats close
		// as "stop iterating" per the kvStore.Watch contract.
		defer close(out)
		defer func() { _ = watcher.Stop() }()
		for entry := range watcher.Updates() {
			if entry == nil {
				// jetstream signals end-of-bootstrap with a nil
				// entry; we don't expose that signal at the
				// kvStore layer — Manager.Watch doesn't need it
				// (it processes each entry uniformly). Skip the
				// nil and keep iterating.
				continue
			}
			op := entry.Operation()
			ev := kvEntry{
				Key:       entry.Key(),
				Value:     entry.Value(),
				Revision:  entry.Revision(),
				CreatedAt: entry.Created(),
				IsDelete:  op == jetstream.KeyValueDelete || op == jetstream.KeyValuePurge,
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
