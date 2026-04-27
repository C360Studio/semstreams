package executors

import (
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Flow-template tool names. Single source of truth for publish_agent.tools
// scoping.
const (
	flowTemplateToolCreate      = "create_flow_template"
	flowTemplateToolUpdate      = "update_flow_template"
	flowTemplateToolDelete      = "delete_flow_template"
	flowTemplateToolList        = "list_flow_templates"
	flowTemplateToolGet         = "get_flow_template"
	flowTemplateToolInstantiate = "instantiate_flow_template"
)

// registerFlowTemplates wires FlowTemplateExecutor globally. Nil manager
// skips registration.
func registerFlowTemplates(tools *agentictools.ExecutorRegistry, manager FlowTemplateManager, logger *slog.Logger) {
	if manager == nil {
		logger.Debug("flow-template CRUD tools disabled: no FlowTemplateManager provided")
		return
	}

	executor := NewFlowTemplateExecutor(manager)
	toolNames := []string{
		flowTemplateToolCreate,
		flowTemplateToolUpdate,
		flowTemplateToolDelete,
		flowTemplateToolList,
		flowTemplateToolGet,
		flowTemplateToolInstantiate,
	}
	for _, name := range toolNames {
		if err := tools.RegisterTool(name, executor); err != nil {
			logger.Warn("Failed to register flow-template tool",
				slog.String("tool", name),
				slog.Any("error", err))
			return
		}
	}
	logger.Info("Registered flow-template tools (global)",
		slog.Int("count", len(toolNames)))
}
