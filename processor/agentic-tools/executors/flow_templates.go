package executors

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/flowtemplate"
)

// FlowTemplateManager is the subset of flowtemplate.Manager that the
// executor needs. Declared here so tests can substitute in-memory fakes.
type FlowTemplateManager interface {
	Create(ctx context.Context, t *flowtemplate.Template) error
	Update(ctx context.Context, t *flowtemplate.Template) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*flowtemplate.Template, error)
	List(ctx context.Context) (map[string]*flowtemplate.Template, error)
}

// FlowTemplateExecutor implements Pattern-B CRUD plus an
// instantiate_flow_template tool that renders a template into a concrete
// flowstore.Flow using caller-supplied parameters. Instantiate is NOT
// stored — the coordinator still has to call create_flow to persist the
// resulting flow, which goes through flowstore.Manager's validation.
// Keeping render + persist separate lets the coordinator inspect the
// rendered flow before committing.
type FlowTemplateExecutor struct {
	manager FlowTemplateManager
}

// NewFlowTemplateExecutor creates a flow-template management executor.
func NewFlowTemplateExecutor(manager FlowTemplateManager) *FlowTemplateExecutor {
	return &FlowTemplateExecutor{manager: manager}
}

// ListTools returns six tools: five CRUD + instantiate.
func (e *FlowTemplateExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "create_flow_template",
			Description: "Create a new parameterised flow template. The body is a flow-JSON with {{.ParamName}} placeholders; declare each placeholder in the parameters list.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"template": map[string]any{
						"type":        "object",
						"description": "Full template JSON: id (required), name (required), body (required, text/template over flow JSON), description (optional), parameters (optional array of {name, description, default, required}).",
					},
				},
				"required": []string{"template"},
			},
		},
		{
			Name:        "update_flow_template",
			Description: "Update an existing flow template. Last-writer-wins.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"template": map[string]any{
						"type":        "object",
						"description": "Updated template JSON with the same id as the existing record.",
					},
				},
				"required": []string{"template"},
			},
		},
		{
			Name:        "delete_flow_template",
			Description: "Delete a flow template by ID. Does not affect flows already instantiated from this template.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"template_id": map[string]any{
						"type":        "string",
						"description": "ID of the template to delete.",
					},
				},
				"required": []string{"template_id"},
			},
		},
		{
			Name:        "list_flow_templates",
			Description: "List all flow templates with ID, name, description, and parameter declarations.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_flow_template",
			Description: "Get the full definition of a specific flow template by ID, including body and parameters.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"template_id": map[string]any{
						"type":        "string",
						"description": "ID of the template to retrieve.",
					},
				},
				"required": []string{"template_id"},
			},
		},
		{
			Name:        "instantiate_flow_template",
			Description: "Render a flow template to a concrete flow definition using supplied parameters. Returns the rendered flow JSON — does NOT persist it. Use create_flow to save the rendered flow if it looks right.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"template_id": map[string]any{
						"type":        "string",
						"description": "ID of the template to instantiate.",
					},
					"parameters": map[string]any{
						"type":        "object",
						"description": "Map of parameter name → string value. Parameters not supplied here fall back to declared defaults; required parameters with no supplied value return an error.",
					},
				},
				"required": []string{"template_id"},
			},
		},
	}
}

// Execute dispatches flow-template tool calls by name.
func (e *FlowTemplateExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "create_flow_template":
		return e.createTemplate(ctx, call)
	case "update_flow_template":
		return e.updateTemplate(ctx, call)
	case "delete_flow_template":
		return e.deleteTemplate(ctx, call)
	case "list_flow_templates":
		return e.listTemplates(ctx, call)
	case "get_flow_template":
		return e.getTemplate(ctx, call)
	case "instantiate_flow_template":
		return e.instantiateTemplate(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func (e *FlowTemplateExecutor) createTemplate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	t, err := unmarshalTemplateArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Create(ctx, t); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("create failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow template %q created.", t.ID),
	}, nil
}

func (e *FlowTemplateExecutor) updateTemplate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	t, err := unmarshalTemplateArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Update(ctx, t); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("update failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow template %q updated.", t.ID),
	}, nil
}

func (e *FlowTemplateExecutor) deleteTemplate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	id, _ := call.Arguments["template_id"].(string)
	if id == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "template_id is required"}, nil
	}
	if err := e.manager.Delete(ctx, id); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("delete failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow template %q deleted.", id),
	}, nil
}

func (e *FlowTemplateExecutor) listTemplates(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	templates, err := e.manager.List(ctx)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("list failed: %v", err)}, nil
	}
	if len(templates) == 0 {
		return agentic.ToolResult{CallID: call.ID, Content: "No flow templates configured."}, nil
	}
	// Compact summary: id / name / description / declared params.
	type summary struct {
		ID          string                   `json:"id"`
		Name        string                   `json:"name"`
		Description string                   `json:"description,omitempty"`
		Parameters  []flowtemplate.Parameter `json:"parameters,omitempty"`
	}
	out := make([]summary, 0, len(templates))
	for _, t := range templates {
		out = append(out, summary{
			ID: t.ID, Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow templates (%d):\n%s", len(out), string(data)),
	}, nil
}

func (e *FlowTemplateExecutor) getTemplate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	id, _ := call.Arguments["template_id"].(string)
	if id == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "template_id is required"}, nil
	}
	t, err := e.manager.Get(ctx, id)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("get failed: %v", err)}, nil
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{CallID: call.ID, Content: string(data)}, nil
}

// instantiateTemplate renders a template with supplied params and returns
// the rendered flow JSON. Does NOT persist — the caller still has to
// call create_flow to save. Separation lets the coordinator inspect
// the result before committing and matches the "preview before deploy"
// safety model in ADR-026.
func (e *FlowTemplateExecutor) instantiateTemplate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	id, _ := call.Arguments["template_id"].(string)
	if id == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "template_id is required"}, nil
	}

	t, err := e.manager.Get(ctx, id)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("get template: %v", err)}, nil
	}

	params := extractParamsArg(call)
	flow, err := t.Instantiate(params)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("instantiate failed: %v", err)}, nil
	}

	// Return the rendered flow as pretty-printed JSON so the coordinator
	// can review before deciding to create_flow.
	data, err := json.MarshalIndent(flow, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal rendered flow: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Rendered flow (not persisted — call create_flow to save):\n%s", string(data)),
	}, nil
}

// unmarshalTemplateArg decodes the call's `template` argument.
func unmarshalTemplateArg(call agentic.ToolCall) (*flowtemplate.Template, error) {
	raw, ok := call.Arguments["template"]
	if !ok {
		return nil, fmt.Errorf("template argument is required")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid template data: %w", err)
	}
	var t flowtemplate.Template
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("invalid template definition: %w", err)
	}
	return &t, nil
}

// extractParamsArg pulls the optional `parameters` map from the call.
// Accepts map[string]any (what the LLM sends) and coerces each value to
// string via fmt.Sprint. Non-string values become their stringified form.
func extractParamsArg(call agentic.ToolCall) map[string]string {
	raw, _ := call.Arguments["parameters"].(map[string]any)
	if raw == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = fmt.Sprint(v)
	}
	return out
}
