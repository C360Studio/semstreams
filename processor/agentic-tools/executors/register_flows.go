package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerFlows wires FlowExecutor so any agent role scoped to flow tools
// (via publish_agent.tools or default_tools) can manage flow definitions.
// FlowExecutor exposes its tool set via ListTools(); RegisterExecutor maps
// each advertised name to the executor.
//
// A nil manager is a deployment choice (skip + nil); a registry-level
// failure (duplicate name) propagates so RegisterBuiltins can surface it
// at boot.
func registerFlows(tools *agentictools.ExecutorRegistry, manager FlowManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("flow CRUD tools disabled: no FlowManager provided")
		return nil
	}

	executor := NewFlowExecutor(manager)
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register flow tools: %w", err)
	}
	logger.Info("Registered flow CRUD tools",
		slog.Int("count", len(executor.ListTools())))
	return nil
}
