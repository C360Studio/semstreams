// Package rule - KV write action support for rule-driven state machine orchestration
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
)

// KVWriter performs read-modify-write operations on domain KV buckets.
// It abstracts the underlying NATS KV infrastructure for testability.
type KVWriter interface {
	// UpdateJSON performs a CAS read-modify-write on a JSON document in the named bucket.
	// The updateFn receives the current value (empty map if key doesn't exist) and mutates it.
	// Uses exponential backoff retry on CAS conflicts (via natsclient.KVStore.UpdateJSON).
	UpdateJSON(ctx context.Context, bucket, key string, updateFn func(current map[string]any) error) error

	// PutJSON writes a JSON value to the named bucket (last writer wins, no CAS).
	PutJSON(ctx context.Context, bucket, key string, value map[string]any) error
}

// natsKVWriter implements KVWriter using natsclient.
// It caches KVStore instances per bucket name to avoid repeated lookups.
type natsKVWriter struct {
	natsClient *natsclient.Client
	logger     *slog.Logger
	stores     cache.Cache[*natsclient.KVStore]
}

// newNATSKVWriter creates a KVWriter backed by NATS KV.
func newNATSKVWriter(natsClient *natsclient.Client, logger *slog.Logger) KVWriter {
	if logger == nil {
		logger = slog.Default()
	}
	stores, err := cache.NewSimple[*natsclient.KVStore]()
	if err != nil {
		// NewSimple only fails if metrics registration fails, which we're not using
		logger.Error("Failed to create KV store cache, using noop", "error", err)
		stores = cache.NewNoop[*natsclient.KVStore]()
	}
	return &natsKVWriter{
		natsClient: natsClient,
		logger:     logger,
		stores:     stores,
	}
}

func (w *natsKVWriter) getStore(ctx context.Context, bucketName string) (*natsclient.KVStore, error) {
	if store, ok := w.stores.Get(bucketName); ok {
		return store, nil
	}

	// The bucket name arrives from a rule pack AFTER variable substitution, so
	// this is the one acquisition site in the tree that can resolve to any name
	// at all — including a catalogued one, whose storage policy the framework
	// guarantees. Spelling a local jetstream.KeyValueConfig for such a name
	// forks that policy from the descriptor that owns it, which is the
	// split-owner shape framework-bucket-catalog exists to remove.
	bucket, err := w.acquireBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}

	store := w.natsClient.NewKVStore(bucket)
	if _, err := w.stores.Set(bucketName, store); err != nil {
		w.logger.Debug("Failed to cache KV store", "bucket", bucketName, "error", err)
	}
	return store, nil
}

// acquireBucket binds one bucket for the generic update_kv writer, resolving a
// catalogued name through its descriptor and anything else the way this writer
// always has.
//
// An owner-only catalogued name is refused outright rather than acquired: the
// action-level guard (executeUpdateKV) already rejects it, and this is the belt
// to that guard's suspenders — an acquisition path that cannot be reached by a
// legitimate write should not silently provision the bucket either.
func (w *natsKVWriter) acquireBucket(ctx context.Context, bucketName string) (jetstream.KeyValue, error) {
	if _, catalogued := gtypes.SpecFor(bucketName); catalogued {
		if gtypes.IsFrameworkOwnedBucket(bucketName) {
			return nil, fmt.Errorf(
				"update_kv cannot acquire framework-owned bucket %q (owned by %s); use graph mutation APIs",
				bucketName, gtypes.OwnerOf(bucketName))
		}
		// Descriptor-derived: History, replicas and retention come from the one
		// catalog row, whichever component reaches the bucket first.
		bucket, err := gtypes.EnsureCatalogBucket(ctx, w.natsClient, bucketName)
		if err != nil {
			return nil, fmt.Errorf("acquire catalogued bucket %s: %w", bucketName, err)
		}
		return bucket, nil
	}

	// Producer creates the bucket. CreateKeyValueBucket is idempotent —
	// returns the existing bucket if it already exists.
	bucket, err := w.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      bucketName,
		Description: "Created by rule engine update_kv action",
		History:     5,
	})
	if err != nil {
		return nil, fmt.Errorf("get or create bucket %s: %w", bucketName, err)
	}
	return bucket, nil
}

func (w *natsKVWriter) UpdateJSON(ctx context.Context, bucket, key string, updateFn func(current map[string]any) error) error {
	store, err := w.getStore(ctx, bucket)
	if err != nil {
		return err
	}
	return store.UpdateJSON(ctx, key, updateFn)
}

func (w *natsKVWriter) PutJSON(ctx context.Context, bucket, key string, value map[string]any) error {
	store, err := w.getStore(ctx, bucket)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	_, err = store.Put(ctx, key, data)
	return err
}
