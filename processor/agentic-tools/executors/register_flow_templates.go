package executors

import (
	"fmt"
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

// registerFlowTemplates wires FlowTemplateExecutor. A nil manager is a
// deployment choice (skip + nil); a registry-level failure (duplicate
// name) propagates so RegisterBuiltins can surface it at boot.
func registerFlowTemplates(tools *agentictools.ExecutorRegistry, manager FlowTemplateManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("flow-template CRUD tools disabled: no FlowTemplateManager provided")
		return nil
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
			return fmt.Errorf("register flow-template tool %q: %w", name, err)
		}
	}
	logger.Info("Registered flow-template tools",
		slog.Int("count", len(toolNames)))
	return nil
}
