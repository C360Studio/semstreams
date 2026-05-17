package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerFlowLifecycle wires FlowLifecycleExecutor so any agent role
// scoped to flow-lifecycle tools (via publish_agent.tools or
// default_tools) can deploy, start, stop, and undeploy flows at
// runtime. Companion to registerFlows — CRUD authors the definition,
// lifecycle moves the deployed instance through its state machine.
//
// A nil manager is a deployment choice (skip + nil); a registry-level
// failure (duplicate name) propagates so RegisterBuiltins can surface
// it at boot.
func registerFlowLifecycle(tools *agentictools.ExecutorRegistry, manager FlowEngineManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("flow lifecycle tools disabled: no FlowEngineManager provided")
		return nil
	}

	executor := NewFlowLifecycleExecutor(manager)
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register flow lifecycle tools: %w", err)
	}
	logger.Info("Registered flow lifecycle tools",
		slog.Int("count", len(executor.ListTools())))
	return nil
}
