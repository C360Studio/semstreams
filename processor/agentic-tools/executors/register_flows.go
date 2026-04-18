package executors

import "log/slog"

// Flow CRUD tool names. One constant per tool so callers scoping via
// publish_agent.tools have a single source of truth.
const (
	flowToolCreate = "create_flow"
	flowToolUpdate = "update_flow"
	flowToolDelete = "delete_flow"
	flowToolList   = "list_flows"
	flowToolGet    = "get_flow"
)

// registerFlows wires FlowExecutor globally so any agent role scoped to
// flow tools (via publish_agent.tools or default_tools) can manage flow
// definitions. Nil manager is a legal skip, matching the RuleManager path
// in registerRules.
func registerFlows(manager FlowManager, logger *slog.Logger) {
	if manager == nil {
		logger.Debug("flow CRUD tools disabled: no FlowManager provided")
		return
	}

	executor := NewFlowExecutor(manager)
	toolNames := []string{
		flowToolCreate,
		flowToolUpdate,
		flowToolDelete,
		flowToolList,
		flowToolGet,
	}

	for _, name := range toolNames {
		if err := registerGlobal(name, executor); err != nil {
			logger.Warn("Failed to register flow tool",
				slog.String("tool", name),
				slog.Any("error", err))
			return
		}
	}
	logger.Info("Registered flow CRUD tools (global)",
		slog.Int("count", len(toolNames)))
}
