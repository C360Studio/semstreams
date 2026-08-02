package executors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

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
// The bucket name is frozen at registration time (boot). One bucket per
// process; products wanting isolated buckets wire different ToolDependencies
// per process.
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

	// Content store for OFFLOADED results (payload-size-chokepoints D4).
	// Unavailability degrades, not disables: inline results still serve, and
	// a storage_ref-bearing value returns a typed hydration error naming the
	// gap — loud for the operator, never a preview passed off as the whole.
	var content agentictools.LoopContentFetcher
	contentStore, cerr := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{
		BucketName: contentBucketName,
	})
	if cerr != nil {
		logger.Warn("read_loop_result: content store unavailable; offloaded results will return a typed error until it is",
			slog.String("bucket", contentBucketName),
			slog.Any("error", cerr))
	} else {
		content = contentStore
	}

	store := natsClient.NewKVStore(bucket)
	executor := agentictools.NewReadLoopResultExecutor(store, content)
	if err := tools.RegisterTool(agentictools.ReadLoopResultToolName, executor); err != nil {
		return fmt.Errorf("register read_loop_result: %w", err)
	}
	logger.Info("Registered read_loop_result tool",
		slog.String("bucket", bucketName),
		slog.String("content_bucket", contentBucketName),
		slog.Bool("hydration_enabled", content != nil))
	return nil
}
