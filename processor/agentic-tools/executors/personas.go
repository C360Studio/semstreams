package executors

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/persona"
)

// PersonaManager is the subset of persona.Manager that PersonaExecutor
// needs. Declared here so tests can substitute in-memory fakes —
// same pattern as RuleManager.
type PersonaManager interface {
	Create(ctx context.Context, p *persona.Persona) error
	Update(ctx context.Context, p *persona.Persona) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*persona.Persona, error)
	List(ctx context.Context) (map[string]*persona.Persona, error)
}

// PersonaExecutor implements CRUD tools for prompt personas. Same shape
// as RuleExecutor and FlowExecutor per ADR-029.
type PersonaExecutor struct {
	manager PersonaManager
}

// NewPersonaExecutor creates a persona management executor.
func NewPersonaExecutor(manager PersonaManager) *PersonaExecutor {
	return &PersonaExecutor{manager: manager}
}

// ListTools returns the five persona-CRUD tool definitions.
func (e *PersonaExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "create_persona",
			Description: "Create a new persona (prompt fragment for an agent role). The persona stores static content; runtime-only parts (dynamic content, conditions) stay in the assembler.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"persona": map[string]any{
						"type":        "object",
						"description": "Full persona JSON: id (required), content (required), category (int, 0=system/100=role/200=tools/300=domain/400=constraints/500=context), priority (int, optional), roles (string[], optional), description (string, optional).",
					},
				},
				"required": []string{"persona"},
			},
		},
		{
			Name:        "update_persona",
			Description: "Update an existing persona. Replaces the stored persona; use get_persona first if you want to preserve fields you aren't changing.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"persona": map[string]any{
						"type":        "object",
						"description": "Updated persona JSON with the same id as the existing record.",
					},
				},
				"required": []string{"persona"},
			},
		},
		{
			Name:        "delete_persona",
			Description: "Delete a persona by ID. Flows that referenced it will fall back to code-defined default fragments once the assembler integration ships.",
			Effect:      agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"persona_id": map[string]any{
						"type":        "string",
						"description": "ID of the persona to delete.",
					},
				},
				"required": []string{"persona_id"},
			},
		},
		{
			Name:        "list_personas",
			Description: "List all personas with their IDs, categories, and roles. Use get_persona for full content.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_persona",
			Description: "Get the full definition of a specific persona by ID.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"persona_id": map[string]any{
						"type":        "string",
						"description": "ID of the persona to retrieve.",
					},
				},
				"required": []string{"persona_id"},
			},
		},
	}
}

// Execute dispatches persona CRUD tool calls by name.
func (e *PersonaExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "create_persona":
		return e.createPersona(ctx, call)
	case "update_persona":
		return e.updatePersona(ctx, call)
	case "delete_persona":
		return e.deletePersona(ctx, call)
	case "list_personas":
		return e.listPersonas(ctx, call)
	case "get_persona":
		return e.getPersona(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func (e *PersonaExecutor) createPersona(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	p, err := unmarshalPersonaArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Create(ctx, p); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("create failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Persona %q created successfully.", p.ID),
	}, nil
}

func (e *PersonaExecutor) updatePersona(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	p, err := unmarshalPersonaArg(call)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	if err := e.manager.Update(ctx, p); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("update failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Persona %q updated.", p.ID),
	}, nil
}

func (e *PersonaExecutor) deletePersona(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	personaID, _ := call.Arguments["persona_id"].(string)
	if personaID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "persona_id is required"}, nil
	}
	if err := e.manager.Delete(ctx, personaID); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("delete failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Persona %q deleted.", personaID),
	}, nil
}

func (e *PersonaExecutor) listPersonas(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	personas, err := e.manager.List(ctx)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("list failed: %v", err)}, nil
	}
	if len(personas) == 0 {
		return agentic.ToolResult{CallID: call.ID, Content: "No personas configured."}, nil
	}
	// Compact summary (id/category/roles) — full Content via get_persona.
	type personaSummary struct {
		ID       string   `json:"id"`
		Category int      `json:"category"`
		Roles    []string `json:"roles,omitempty"`
	}
	summaries := make([]personaSummary, 0, len(personas))
	for _, p := range personas {
		summaries = append(summaries, personaSummary{
			ID: p.ID, Category: p.Category, Roles: p.Roles,
		})
	}
	data, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Personas (%d):\n%s", len(summaries), string(data)),
	}, nil
}

func (e *PersonaExecutor) getPersona(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	personaID, _ := call.Arguments["persona_id"].(string)
	if personaID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "persona_id is required"}, nil
	}
	p, err := e.manager.Get(ctx, personaID)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("get failed: %v", err)}, nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal failed: %v", err)}, nil
	}
	return agentic.ToolResult{CallID: call.ID, Content: string(data)}, nil
}

// unmarshalPersonaArg decodes the call's `persona` argument into a
// *persona.Persona via JSON round-trip. Mirrors unmarshalFlowArg.
func unmarshalPersonaArg(call agentic.ToolCall) (*persona.Persona, error) {
	raw, ok := call.Arguments["persona"]
	if !ok {
		return nil, fmt.Errorf("persona argument is required")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid persona data: %w", err)
	}
	var p persona.Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid persona definition: %w", err)
	}
	return &p, nil
}
