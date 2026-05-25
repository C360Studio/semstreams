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
