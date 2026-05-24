package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
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
		// jetstream surfaces CAS conflicts as a wrong-revision
		// error (no dedicated sentinel — pattern matched by
		// natsclient consumers elsewhere). Substring-classify
		// against jetstream's error text is fragile; the safer
		// classifier is "any non-found error on Update with a
		// non-zero expectedRevision is a CAS conflict OR a key-
		// gone-during-update." Both reduce to "Manager retries
		// via Get + mutator + Update under updateRetries budget."
		//
		// TODO(integration-test): the testcontainers slice should
		// exercise this exact path and assert errKVRevisionMismatch
		// classification against real jetstream sentinels — if
		// jetstream adds a dedicated CAS-conflict error in a future
		// release, swap this classifier to errors.Is.
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			return 0, errKVKeyNotFound
		}
		// Default to CAS-conflict classification — Manager.Update's
		// retry loop is the right response for the common case
		// (concurrent writer beat us). Real-error paths surface
		// via the wrapped error after retries exhaust.
		return 0, fmt.Errorf("%w: %w", errKVRevisionMismatch, err)
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
