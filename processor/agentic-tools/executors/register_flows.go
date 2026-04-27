package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Flow CRUD tool names. One constant per tool so callers scoping via
// publish_agent.tools have a single source of truth.
const (
	flowToolCreate = "create_flow"
	flowToolUpdate = "update_flow"
	flowToolDelete = "delete_flow"
	flowToolList   = "list_flows"
	flowToolGet    = "get_flow"
)

// registerFlows wires FlowExecutor so any agent role scoped to flow tools
// (via publish_agent.tools or default_tools) can manage flow definitions.
// A nil manager is a deployment choice (skip + nil); a registry-level
// failure (duplicate name) propagates so RegisterBuiltins can surface it
// at boot.
func registerFlows(tools *agentictools.ExecutorRegistry, manager FlowManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("flow CRUD tools disabled: no FlowManager provided")
		return nil
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
		if err := tools.RegisterTool(name, executor); err != nil {
			return fmt.Errorf("register flow tool %q: %w", name, err)
		}
	}
	logger.Info("Registered flow CRUD tools",
		slog.Int("count", len(toolNames)))
	return nil
}
