// Package ruletools exposes the rule-engine CRUD tools as agentic-tools
// executors. Living alongside the rule processor (not under
// processor/agentic-tools/executors) keeps the package graph acyclic:
// agentic-tools can depend on rule, but rule can't depend back on
// agentic-tools without breaking.
//
// Registration happens from whichever binary wires its agent loop (main
// packages own runtime wiring); see processor/rule.Processor.RegisterTools
// for the production entry point.
package ruletools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/rule"
)

// validRuleID matches kebab-case identifiers: lowercase alphanumeric with hyphens.
var validRuleID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// RuleManager is the subset of rule.ConfigManager needed by RuleExecutor.
type RuleManager interface {
	SaveRule(ctx context.Context, ruleID string, ruleDef rule.Definition) error
	DeleteRule(ctx context.Context, ruleID string) error
	GetRule(ctx context.Context, ruleID string) (*rule.Definition, error)
	ListRules(ctx context.Context) (map[string]rule.Definition, error)
}

// RuleExecutor implements CRUD tools for the rule engine.
type RuleExecutor struct {
	manager RuleManager
}

// NewRuleExecutor creates a rule management executor.
func NewRuleExecutor(manager RuleManager) *RuleExecutor {
	return &RuleExecutor{manager: manager}
}

// ListTools returns the rule management tool definitions.
func (e *RuleExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "create_rule",
			Description: "Create a new rule in the rules engine. The rule becomes active immediately after approval. Provide the full rule definition as JSON.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule_id": map[string]any{
						"type":        "string",
						"description": "Unique rule identifier (e.g. 'escalate-budget-exceeded')",
					},
					"rule": map[string]any{
						"type":        "object",
						"description": "Full rule definition JSON matching the rule schema (type, name, conditions, logic, on_enter, etc.)",
					},
				},
				"required": []string{"rule_id", "rule"},
			},
		},
		{
			Name:        "update_rule",
			Description: "Update an existing rule. Replaces the entire rule definition.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule_id": map[string]any{
						"type":        "string",
						"description": "ID of the rule to update",
					},
					"rule": map[string]any{
						"type":        "object",
						"description": "Updated rule definition JSON",
					},
				},
				"required": []string{"rule_id", "rule"},
			},
		},
		{
			Name:        "delete_rule",
			Description: "Delete a rule from the rules engine.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule_id": map[string]any{
						"type":        "string",
						"description": "ID of the rule to delete",
					},
				},
				"required": []string{"rule_id"},
			},
		},
		{
			Name:        "list_rules",
			Description: "List all active rules in the rules engine.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_rule",
			Description: "Get the full definition of a specific rule.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule_id": map[string]any{
						"type":        "string",
						"description": "ID of the rule to retrieve",
					},
				},
				"required": []string{"rule_id"},
			},
		},
	}
}

// Execute dispatches rule tool calls.
func (e *RuleExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "create_rule", "update_rule":
		return e.saveRule(ctx, call)
	case "delete_rule":
		return e.deleteRule(ctx, call)
	case "list_rules":
		return e.listRules(ctx, call)
	case "get_rule":
		return e.getRule(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func (e *RuleExecutor) saveRule(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	ruleID, _ := call.Arguments["rule_id"].(string)
	if ruleID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "rule_id is required"}, nil
	}

	// Validate rule ID format to prevent KV key injection
	if !validRuleID.MatchString(ruleID) {
		return agentic.ToolResult{CallID: call.ID, Error: "rule_id must be kebab-case (lowercase alphanumeric with hyphens, 2-64 chars)"}, nil
	}

	ruleData, ok := call.Arguments["rule"]
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "rule definition is required"}, nil
	}

	// Convert the rule argument to a Definition via JSON round-trip.
	ruleBytes, err := json.Marshal(ruleData)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("invalid rule data: %v", err)}, nil
	}

	var def rule.Definition
	if err := json.Unmarshal(ruleBytes, &def); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("invalid rule definition: %v", err)}, nil
	}

	// Always enforce ID consistency — the KV key is the source of truth
	def.ID = ruleID

	if err := e.manager.SaveRule(ctx, ruleID, def); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("save failed: %v", err)}, nil
	}

	action := "created"
	if call.Name == "update_rule" {
		action = "updated"
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Rule '%s' %s successfully. It is now active in the rules engine.", ruleID, action),
	}, nil
}

func (e *RuleExecutor) deleteRule(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	ruleID, _ := call.Arguments["rule_id"].(string)
	if ruleID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "rule_id is required"}, nil
	}
	if !validRuleID.MatchString(ruleID) {
		return agentic.ToolResult{CallID: call.ID, Error: "rule_id must be kebab-case (lowercase alphanumeric with hyphens, 2-64 chars)"}, nil
	}

	if err := e.manager.DeleteRule(ctx, ruleID); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("delete failed: %v", err)}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Rule '%s' deleted successfully.", ruleID),
	}, nil
}

func (e *RuleExecutor) listRules(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	rules, err := e.manager.ListRules(ctx)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("list failed: %v", err)}, nil
	}

	if len(rules) == 0 {
		return agentic.ToolResult{CallID: call.ID, Content: "No rules configured."}, nil
	}

	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Active rules (%d):\n%s", len(rules), string(data)),
	}, nil
}

func (e *RuleExecutor) getRule(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	ruleID, _ := call.Arguments["rule_id"].(string)
	if ruleID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "rule_id is required"}, nil
	}

	def, err := e.manager.GetRule(ctx, ruleID)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("get failed: %v", err)}, nil
	}

	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
	}, nil
}
