package executors

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/flowstore"
)

// FlowManager is the subset of flowstore.Manager that FlowExecutor needs.
// Declared here so tests can substitute an in-memory fake without
// depending on the full *flowstore.Manager type. *flowstore.Manager
// satisfies it by duck typing.
type FlowManager interface {
	Create(ctx context.Context, flow *flowstore.Flow) error
	Update(ctx context.Context, flow *flowstore.Flow) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*flowstore.Flow, error)
	List(ctx context.Context) ([]*flowstore.Flow, error)
}

// FlowExecutor implements CRUD tools for flow definitions. Mirrors
// RuleExecutor's shape (one executor per Pattern-B type, dispatching
// on ToolCall.Name to the right Manager method).
type FlowExecutor struct {
	manager FlowManager
}

// NewFlowExecutor creates a flow management executor.
func NewFlowExecutor(manager FlowManager) *FlowExecutor {
	return &FlowExecutor{manager: manager}
}

// ListTools returns the five flow-CRUD tool definitions.
// Flow is a structured object (nodes, connections, metadata, timestamps);
// the tool schema accepts a JSON object via `flow` parameter so LLMs can
// construct or edit definitions without a hand-crafted schema per field.
// Validation happens when the Manager unmarshals + Validate()s the payload.
func (e *FlowExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "create_flow",
			Description: "Create a new saved flow diagram. Provide the full flow JSON matching the flow schema (id, name, nodes, connections, ...).",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow": map[string]any{
						"type":        "object",
						"description": "Full flow definition JSON. Must include id, name, nodes, connections. Version/timestamps are managed by the system.",
					},
				},
				"required": []string{"flow"},
			},
		},
		{
			Name:        "update_flow",
			Description: "Update an existing flow definition. Uses optimistic concurrency — the flow JSON must carry the current version number; mismatch returns an error so the caller can re-fetch and retry.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow": map[string]any{
						"type":        "object",
						"description": "Updated flow definition JSON including the current version number.",
					},
				},
				"required": []string{"flow"},
			},
		},
		{
			Name:        "delete_flow",
			Description: "Delete a saved flow diagram by ID. This does not change process runtime configuration.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to delete.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
		{
			Name:        "list_flows",
			Description: "List all saved flow diagrams with their IDs, names, and versions.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_flow",
			Description: "Get a saved flow diagram by ID, including nodes, connections, metadata, and version.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to retrieve.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
	}
}

// Execute dispatches flow CRUD tool calls by name.
func (e *FlowExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "create_flow":
		return e.createFlow(ctx, call)
	case "update_flow":
		return e.updateFlow(ctx, call)
	case "delete_flow":
		return e.deleteFlow(ctx, call)
	case "list_flows":
		return e.listFlows(ctx, call)
	case "get_flow":
		return e.getFlow(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func (e *FlowExecutor) createFlow(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flow, err := unmarshalFlowArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Create(ctx, flow); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("create failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow %q created successfully (version %d).", flow.ID, flow.Version),
	}, nil
}

func (e *FlowExecutor) updateFlow(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flow, err := unmarshalFlowArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Update(ctx, flow); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("update failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow %q updated (now at version %d).", flow.ID, flow.Version),
	}, nil
}

func (e *FlowExecutor) deleteFlow(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flowID, _ := call.Arguments["flow_id"].(string)
	if flowID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "flow_id is required"}, nil
	}
	if err := e.manager.Delete(ctx, flowID); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("delete failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow %q deleted.", flowID),
	}, nil
}

func (e *FlowExecutor) listFlows(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flows, err := e.manager.List(ctx)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("list failed: %v", err)}, nil
	}
	if len(flows) == 0 {
		return agentic.ToolResult{CallID: call.ID, Content: "No flows configured."}, nil
	}
	// Return a compact summary (id/name/version) rather than full
	// flow bodies — reduces LLM context pressure. Use get_flow for detail.
	type flowSummary struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	summaries := make([]flowSummary, 0, len(flows))
	for _, f := range flows {
		summaries = append(summaries, flowSummary{
			ID:      f.ID,
			Name:    f.Name,
			Version: f.Version,
		})
	}
	data, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flows (%d):\n%s", len(summaries), string(data)),
	}, nil
}

func (e *FlowExecutor) getFlow(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flowID, _ := call.Arguments["flow_id"].(string)
	if flowID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "flow_id is required"}, nil
	}
	flow, err := e.manager.Get(ctx, flowID)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("get failed: %v", err)}, nil
	}
	data, err := json.MarshalIndent(flow, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
	}, nil
}

// unmarshalFlowArg converts the call's `flow` argument into a *flowstore.Flow
// via JSON round-trip. The LLM provides a map/any payload; we re-marshal
// and decode into the typed struct so downstream Validate catches shape
// mistakes cleanly.
func unmarshalFlowArg(call agentic.ToolCall) (*flowstore.Flow, error) {
	raw, ok := call.Arguments["flow"]
	if !ok {
		return nil, fmt.Errorf("flow argument is required")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid flow data: %w", err)
	}
	var flow flowstore.Flow
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("invalid flow definition: %w", err)
	}
	return &flow, nil
}
