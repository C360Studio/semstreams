package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/service"
)

// ComponentCatalogToolName is the tool name agents use to invoke list_components.
const ComponentCatalogToolName = "list_components"

// ComponentCatalogExecutor introspects the component factory registry and
// returns available component types. The optional type_filter arg narrows
// the response to a single entry, keeping LLM context pressure low for
// large registries (30+ factory types produce substantial JSON).
type ComponentCatalogExecutor struct {
	registry *component.Registry
	logger   *slog.Logger
}

// NewComponentCatalogExecutor constructs the executor. registry must be
// non-nil; callers that can't provide one should skip registration entirely
// (see executors.registerComponentCatalog).
func NewComponentCatalogExecutor(registry *component.Registry, logger *slog.Logger) *ComponentCatalogExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ComponentCatalogExecutor{registry: registry, logger: logger}
}

// ListTools describes the list_components tool.
func (e *ComponentCatalogExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        ComponentCatalogToolName,
			Description: "List registered component factory types with their schemas, metadata (type, protocol, domain, version), and configuration schema. Use type_filter to fetch a single type when you only need its schema — avoids sending the full catalog into the model context. Returns an array of component type objects.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type_filter": map[string]any{
						"type":        "string",
						"description": "Optional component type ID (e.g. 'graph-processor', 'rule-processor'). When supplied, returns only that one entry. Omit to return all registered types.",
					},
				},
			},
		},
	}
}

// Execute routes the tool call to the handler.
func (e *ComponentCatalogExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != ComponentCatalogToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "ComponentCatalogExecutor", "Execute", "route tool")
	}
	return e.listComponents(ctx, call)
}

func (e *ComponentCatalogExecutor) listComponents(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	catalog := service.BuildComponentTypeCatalog(e.registry, e.logger)

	typeFilter, _ := call.Arguments["type_filter"].(string)
	if typeFilter != "" {
		// Narrow to matching entry only.
		var matched []map[string]any
		for _, entry := range catalog {
			if id, _ := entry["id"].(string); id == typeFilter {
				matched = append(matched, entry)
				break
			}
		}
		if len(matched) == 0 {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("component type %q not found in registry", typeFilter),
				ErrorKind: agentic.ToolErrorNotFound,
			}, nil
		}
		catalog = matched
	}

	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal catalog: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ComponentCatalogExecutor", "listComponents", "marshal")
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
	}, nil
}
