package executors

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// newLoopResultBucketConfig mirrors agentic-loop/component.go initializeKVBuckets
// (History=10, TTL=24h). Tool registration races with component Start — whichever
// side gets here first creates the bucket; the other side gets the existing
// handle via CreateKeyValueBucket's idempotent Create-or-Get. Config must match
// so the bucket's actual config matches the component's intent regardless of
// which side created it.
func newLoopResultBucketConfig(bucket string) jetstream.KeyValueConfig {
	return jetstream.KeyValueConfig{
		Bucket:  bucket,
		History: 10,
		TTL:     24 * time.Hour,
	}
}

// refResolvingContentFetcher resolves an offloaded result body from the
// bucket the REF ITSELF names (payload-size-chokepoints HIGH-5).
// StorageReference.StorageInstance carries the writer's store identity —
// agentic-loop constructs its content store with no InstanceName, so the
// stamped instance IS the bucket name (objectstore gh#400 default). Resolving
// per-ref means a deployment whose loop writes to a non-default
// content_bucket reads back correctly with no boot-time wiring; the
// registration-time fallback bucket covers only refs with an empty instance
// (defensive; the production writer always stamps one). Stores are opened
// lazily and cached per bucket.
type refResolvingContentFetcher struct {
	client         *natsclient.Client
	fallbackBucket string
	logger         *slog.Logger

	mu     sync.Mutex
	stores map[string]*objectstore.Store
}

func (f *refResolvingContentFetcher) FetchContent(ctx context.Context, ref *message.StorageReference) (*objectstore.StoredContent, error) {
	bucket := ref.StorageInstance
	if bucket == "" {
		bucket = f.fallbackBucket
	}
	store, err := f.storeFor(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("open content store %q for ref %q: %w", bucket, ref.Key, err)
	}
	return store.FetchContent(ctx, ref)
}

// storeFor returns (opening and caching on first use) the store for a bucket.
// Open failures are NOT cached: a transient NATS hiccup at first hydration
// must not disable the bucket for the process lifetime.
func (f *refResolvingContentFetcher) storeFor(ctx context.Context, bucket string) (*objectstore.Store, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if store, ok := f.stores[bucket]; ok {
		return store, nil
	}
	store, err := objectstore.NewStoreWithConfig(ctx, f.client, objectstore.Config{
		BucketName: bucket,
	})
	if err != nil {
		return nil, err
	}
	f.stores[bucket] = store
	return store, nil
}

// registerReadLoopResult opens (or creates) the given KV bucket and
// registers the read_loop_result tool. The bucket open is wrapped in
// retry.Quick (10 attempts over ~6s) so a transient NATS hiccup during
// boot — circuit breaker open from a recent flap, JetStream API
// momentarily unavailable — doesn't silently disable the tool for the
// lifetime of the process. After retries are exhausted we fall through
// to the warn-and-skip path: a misconfigured deployment shouldn't block
// the binary's boot, but the operator gets a loud log line. A
// registry-level failure (duplicate name) propagates so RegisterBuiltins
// can surface it at boot.
//
// The tool needs to live on the shared registry that agentic-loop's
// discoverTools advertises to the LLM — registering it only on a
// component-local registry would make the tool invocable but invisible to
// the model, which manifests as the LLM producing completion text instead
// of a tool call. Learned this the hard way during deep-research's
// coordinator run; the note stays so future stateful tools don't repeat
// the pattern.
//
// The loops bucket name is frozen at registration time (boot). The CONTENT
// bucket is resolved per-ref from StorageReference.StorageInstance
// (refResolvingContentFetcher), with contentBucketName as the fallback for
// instance-less refs only.
func registerReadLoopResult(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger, bucketName, contentBucketName string) error {
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return natsClient.CreateKeyValueBucket(ctx, newLoopResultBucketConfig(bucketName))
	})
	if err != nil {
		logger.Warn("read_loop_result tool disabled: could not open loops bucket after retries",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return nil
	}

	// Content fetcher for OFFLOADED results (payload-size-chokepoints D4):
	// per-ref bucket resolution, lazily opened. Unavailability degrades, not
	// disables: inline results still serve, and a storage_ref-bearing value
	// whose bucket cannot be opened returns a typed hydration error naming
	// the gap — loud for the operator, never a preview passed off as the
	// whole.
	content := &refResolvingContentFetcher{
		client:         natsClient,
		fallbackBucket: contentBucketName,
		logger:         logger,
		stores:         make(map[string]*objectstore.Store),
	}

	store := natsClient.NewKVStore(bucket)
	executor := agentictools.NewReadLoopResultExecutor(store, content)
	if err := tools.RegisterTool(agentictools.ReadLoopResultToolName, executor); err != nil {
		return fmt.Errorf("register read_loop_result: %w", err)
	}
	logger.Info("Registered read_loop_result tool",
		slog.String("bucket", bucketName),
		slog.String("fallback_content_bucket", contentBucketName),
		slog.Bool("hydration_enabled", true))
	return nil
}
