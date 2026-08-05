package executors

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

var errWriteTodosMutationClientRequired = errors.New(
	"register write_todos: projection mutation client is required",
)

// registerWriteTodos wires the ADR-036 write_todos tool. The tool
// writes agent-private todo state on the calling loop's entity via one
// contract-bound reconcile mutation. Available to all roles
// (per ADR-036 §First Instance — opacity is per-loop, not per-role);
// persona authors opt out via tool allowlists for tiny-model
// deployments.
func registerWriteTodos(reg *agentictools.ExecutorRegistry, client projection.PredicateReconciler, platform component.PlatformMeta, logger *slog.Logger) error {
	if client == nil {
		return errWriteTodosMutationClientRequired
	}
	executor := agentictools.NewWriteTodosExecutor(client, platform)
	executor.SetLogger(logger)
	if err := reg.RegisterTool(agentictools.WriteTodosToolName, executor); err != nil {
		return fmt.Errorf("register write_todos: %w", err)
	}
	logger.Info("Registered write_todos tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
