package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerRules wires the RuleExecutor so any agent role scoped to rule
// tools (via publish_agent.tools or default_tools) can call list_rules /
// get_rule / create_rule / update_rule / delete_rule.
//
// RuleExecutor is one executor exposing five tools through ListTools();
// RegisterExecutor maps every advertised name to the executor so dispatch
// resolves any of the five and ListTools() emits each definition once.
//
// A nil manager is a deployment choice (callers without rule CRUD pass
// nil); registration skips and returns nil. A registry-level failure
// (duplicate name) propagates so RegisterBuiltins can surface it at boot.
func registerRules(tools *agentictools.ExecutorRegistry, manager RuleManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("rule CRUD tools disabled: no RuleManager provided")
		return nil
	}

	executor := NewRuleExecutor(manager)
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register rule tools: %w", err)
	}
	logger.Info("Registered rule CRUD tools",
		slog.Int("count", len(executor.ListTools())))
	return nil
}
