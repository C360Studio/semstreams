package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerReadLoopResult registers the read_loop_result tool without opening
// AGENT_LOOPS. The executor binds the bucket must-exist when each call runs, so
// clean boot ordering cannot permanently disable the tool and this reader never
// provisions or configures agentic-loop's bucket.
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
func registerReadLoopResult(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger, bucketName string) error {
	executor := agentictools.NewReadLoopResultExecutor(lazyLoopsKV{client: natsClient, bucket: bucketName})
	if err := tools.RegisterTool(agentictools.ReadLoopResultToolName, executor); err != nil {
		return fmt.Errorf("register read_loop_result: %w", err)
	}
	logger.Info("Registered read_loop_result tool",
		slog.String("bucket", bucketName))
	return nil
}
