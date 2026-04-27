package executors

import (
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerDecide wires the coordinator's decide terminal tool. The tool
// publishes triples via the graph.mutation.triple.add NATS surface (same
// path rule actions use). Registered globally for the same
// LLM-advertisement reason described on registerReadLoopResult.
func registerDecide(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) {
	publisher := agentictools.NewNATSTriplePublisher(natsClient)
	executor := agentictools.NewDecideExecutor(publisher, platform)
	if err := tools.RegisterTool(agentictools.DecideToolName, executor); err != nil {
		logger.Warn("Failed to register decide tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered decide tool (global)",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
}
