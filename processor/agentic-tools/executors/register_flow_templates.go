package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerFlowTemplates wires FlowTemplateExecutor. FlowTemplateExecutor
// exposes its tool set via ListTools(); RegisterExecutor maps each
// advertised name to the executor.
//
// A nil manager is a deployment choice (skip + nil); a registry-level
// failure (duplicate name) propagates so RegisterBuiltins can surface it
// at boot.
func registerFlowTemplates(tools *agentictools.ExecutorRegistry, manager FlowTemplateManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("flow-template CRUD tools disabled: no FlowTemplateManager provided")
		return nil
	}

	executor := NewFlowTemplateExecutor(manager)
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register flow-template tools: %w", err)
	}
	logger.Info("Registered flow-template tools",
		slog.Int("count", len(executor.ListTools())))
	return nil
}
