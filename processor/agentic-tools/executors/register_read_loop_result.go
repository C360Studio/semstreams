package executors

import (
	"context"
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

// registerReadLoopResult opens (or creates) the given KV bucket and registers
// the read_loop_result tool. Failure logs a warning and returns — the rest
// of the caller's tools stay available.
//
// Registration is GLOBAL because agentic-loop's discoverTools() pulls
// from the global registry when advertising tools to the LLM. A purely
// local registration makes the tool invocable but invisible to the
// model — which manifests as the LLM producing completion text instead
// of a tool call. Learned this the hard way during deep-research's
// coordinator run; the note stays so future stateful tools don't repeat
// the pattern.
//
// The bucket name is frozen at registration time (boot). One bucket per
// process; products wanting isolated buckets wire different ToolDependencies
// per process.
func registerReadLoopResult(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger, bucketName string) {
	bucket, err := natsClient.CreateKeyValueBucket(ctx, newLoopResultBucketConfig(bucketName))
	if err != nil {
		logger.Warn("read_loop_result tool disabled: could not open loops bucket",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return
	}

	store := natsClient.NewKVStore(bucket)
	executor := agentictools.NewReadLoopResultExecutor(store)
	if err := tools.RegisterTool(agentictools.ReadLoopResultToolName, executor); err != nil {
		logger.Warn("Failed to register read_loop_result tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered read_loop_result tool (global)",
		slog.String("bucket", bucketName))
}
