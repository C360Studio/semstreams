package agentictools

import (
	"context"
	"log/slog"
)

// registerReadLoopResult opens the AGENT_LOOPS KV bucket and registers the
// read_loop_result tool. Failure logs a warning and returns — the rest of
// the component's tools stay available.
//
// Registration is GLOBAL (via agentictools.RegisterTool) rather than
// component-local because agentic-loop's discoverTools() pulls from the
// global registry when advertising tools to the LLM. A purely local
// registration makes the tool invocable but invisible to the model —
// which manifests as the LLM producing completion text instead of a tool
// call. Fixed this the hard way in deep-research's coordinator run; the
// note stays so future stateful tools don't repeat the pattern.
func (c *Component) registerReadLoopResult(ctx context.Context) {
	loopsBucketName := c.config.LoopsBucket
	if loopsBucketName == "" {
		loopsBucketName = "AGENT_LOOPS"
	}

	bucket, err := c.natsClient.GetKeyValueBucket(ctx, loopsBucketName)
	if err != nil {
		c.logger.Warn("read_loop_result tool disabled: could not open loops bucket",
			slog.String("bucket", loopsBucketName),
			slog.Any("error", err))
		return
	}

	store := c.natsClient.NewKVStore(bucket)
	executor := NewReadLoopResultExecutor(store)
	if err := registerGlobalTool(loopResultToolName, executor); err != nil {
		c.logger.Warn("Failed to register read_loop_result tool",
			slog.Any("error", err))
		return
	}
	c.logger.Info("Registered read_loop_result tool (global)",
		slog.String("bucket", loopsBucketName))
}
