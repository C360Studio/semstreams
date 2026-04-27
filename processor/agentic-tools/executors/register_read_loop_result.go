package executors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
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
// registers the read_loop_result tool. A bucket-open failure is a
// non-fatal skip — the tool stays unregistered and the rest of the
// caller's registrations proceed; the agent loop continues without the
// loop-result-read capability. A registry-level failure (duplicate name)
// propagates so RegisterBuiltins can surface it at boot.
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
func registerReadLoopResult(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger, bucketName string) error {
	bucket, err := natsClient.CreateKeyValueBucket(ctx, newLoopResultBucketConfig(bucketName))
	if err != nil {
		logger.Warn("read_loop_result tool disabled: could not open loops bucket",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return nil
	}

	store := natsClient.NewKVStore(bucket)
	executor := agentictools.NewReadLoopResultExecutor(store)
	if err := tools.RegisterTool(agentictools.ReadLoopResultToolName, executor); err != nil {
		return fmt.Errorf("register read_loop_result: %w", err)
	}
	logger.Info("Registered read_loop_result tool",
		slog.String("bucket", bucketName))
	return nil
}
