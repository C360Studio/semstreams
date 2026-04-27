package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerDecide wires the coordinator's decide terminal tool. The tool
// publishes triples via the graph.mutation.triple.add NATS surface (same
// path rule actions use). A registry-level failure (duplicate name)
// returns the error so RegisterBuiltins can surface it at boot.
func registerDecide(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	publisher := agentictools.NewNATSTriplePublisher(natsClient)
	executor := agentictools.NewDecideExecutor(publisher, platform)
	if err := tools.RegisterTool(agentictools.DecideToolName, executor); err != nil {
		return fmt.Errorf("register decide: %w", err)
	}
	logger.Info("Registered decide tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
