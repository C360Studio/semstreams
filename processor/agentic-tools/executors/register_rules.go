package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Rule CRUD tool names. One constant per tool so callers scoping via
// publish_agent.tools have a single source of truth rather than string
// literals scattered across configs.
const (
	ruleToolCreate = "create_rule"
	ruleToolUpdate = "update_rule"
	ruleToolDelete = "delete_rule"
	ruleToolList   = "list_rules"
	ruleToolGet    = "get_rule"
)

// registerRules wires the RuleExecutor so any agent role scoped to rule
// tools (via publish_agent.tools or default_tools) can call list_rules /
// get_rule / create_rule / update_rule / delete_rule.
//
// RuleExecutor is one executor that exposes five tools. We register the
// executor once under each tool name — all five dispatch into the same
// Execute() method which switches on ToolCall.Name. This matches how
// GraphQueryExecutor handles its four tools in register_graph_query.go.
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
	toolNames := []string{
		ruleToolCreate,
		ruleToolUpdate,
		ruleToolDelete,
		ruleToolList,
		ruleToolGet,
	}

	for _, name := range toolNames {
		if err := tools.RegisterTool(name, executor); err != nil {
			return fmt.Errorf("register rule tool %q: %w", name, err)
		}
	}
	logger.Info("Registered rule CRUD tools",
		slog.Int("count", len(toolNames)))
	return nil
}
