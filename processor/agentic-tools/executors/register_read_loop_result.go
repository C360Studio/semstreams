package executors

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// loopResultBucket is the default AGENT_LOOPS KV bucket name. Keeping it a
// constant here rather than reading the bucket name from agentic-tools
// config mirrors the registration split: wiring is boot-time and doesn't
// know which flow will eventually load. Flows that want a different
// bucket name would need a separate wire call — not a concern today.
const loopResultBucket = "AGENT_LOOPS"

// registerReadLoopResult opens the AGENT_LOOPS KV bucket and registers the
// read_loop_result tool. Failure logs a warning and returns — the rest of
// the caller's tools stay available.
//
// Registration is GLOBAL because agentic-loop's discoverTools() pulls
// from the global registry when advertising tools to the LLM. A purely
// local registration makes the tool invocable but invisible to the
// model — which manifests as the LLM producing completion text instead
// of a tool call. Learned this the hard way during deep-research's
// coordinator run; the note stays so future stateful tools don't repeat
// the pattern.
func registerReadLoopResult(ctx context.Context, natsClient *natsclient.Client, logger *slog.Logger) {
	bucket, err := natsClient.GetKeyValueBucket(ctx, loopResultBucket)
	if err != nil {
		logger.Warn("read_loop_result tool disabled: could not open loops bucket",
			slog.String("bucket", loopResultBucket),
			slog.Any("error", err))
		return
	}

	store := natsClient.NewKVStore(bucket)
	executor := agentictools.NewReadLoopResultExecutor(store)
	if err := registerGlobal(agentictools.ReadLoopResultToolName, executor); err != nil {
		logger.Warn("Failed to register read_loop_result tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered read_loop_result tool (global)",
		slog.String("bucket", loopResultBucket))
}
