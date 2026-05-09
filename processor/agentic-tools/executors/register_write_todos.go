package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerWriteTodos wires the ADR-036 write_todos tool. The tool
// writes agent-private todo state on the calling loop's entity via
// the graph.mutation.triple.add_batch (Stage 2) and
// graph.mutation.triple.remove NATS surfaces. Available to all roles
// (per ADR-036 §First Instance — opacity is per-loop, not per-role);
// persona authors opt out via tool allowlists for tiny-model
// deployments.
func registerWriteTodos(reg *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	writer := agentictools.NewNATSTodoWriter(natsClient)
	executor := agentictools.NewWriteTodosExecutor(writer, platform)
	executor.SetLogger(logger)
	if err := reg.RegisterTool(agentictools.WriteTodosToolName, executor); err != nil {
		return fmt.Errorf("register write_todos: %w", err)
	}
	logger.Info("Registered write_todos tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
