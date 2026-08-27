package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Composition tool names. Both are read-only, need no NATS client, and carry
// no payload: they run composition.Validate over a configuration document the
// agent supplies and return the library's result.
const (
	validateCompositionToolName = "validate_composition"
	compositionGraphToolName    = "composition_graph"
)

// compositionExecutor serves validate_composition and composition_graph over
// the process's component registry.
type compositionExecutor struct {
	registry *component.Registry
	logger   *slog.Logger
}

func newCompositionExecutor(registry *component.Registry, logger *slog.Logger) *compositionExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &compositionExecutor{registry: registry, logger: logger}
}

// ListTools describes the two composition tools.
func (e *compositionExecutor) ListTools() []agentic.ToolDefinition {
	configParameter := map[string]any{
		"type":        "object",
		"description": "A complete SemStreams configuration document (the same JSON a config file holds: platform, services, components).",
	}
	return []agentic.ToolDefinition{
		{
			Name:        validateCompositionToolName,
			Description: "Validate a configuration document's component composition offline against this process's component catalog: unknown factories, port declarations, exclusive resources, connections, stream requirements, and interface contracts. Returns the composition result (status, errors, warnings, graph). Use it before proposing a configuration change; nothing is constructed or written.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"config": configParameter},
				"required":   []string{"config"},
			},
		},
		{
			Name:        compositionGraphToolName,
			Description: "Project a configuration document's component composition as a graph (nodes with resolved ports, derived edges) as JSON or Mermaid. Use format=mermaid when you want a diagram to show a human; json when you need the port and edge detail.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"config": configParameter,
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"json", "mermaid"},
						"description": "Output format; json (default) or mermaid.",
					},
				},
				"required": []string{"config"},
			},
		},
	}
}

// Execute routes the tool call.
func (e *compositionExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != validateCompositionToolName && call.Name != compositionGraphToolName {
		return agentic.ToolResult{
			CallID: call.ID, Error: fmt.Sprintf("unknown tool: %s", call.Name), ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "compositionExecutor", "Execute", "route tool")
	}
	cfg, err := configFromArguments(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID, Error: err.Error(), ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}
	result, err := composition.Validate(e.registry, cfg)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID, Error: err.Error(), ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "compositionExecutor", "Execute", "validate composition")
	}

	var content []byte
	switch {
	case call.Name == compositionGraphToolName && formatArgument(call.Arguments) == "mermaid":
		content = []byte(composition.Mermaid(result.Graph))
	case call.Name == compositionGraphToolName:
		content, err = json.MarshalIndent(result.Graph, "", "  ")
	default:
		content, err = json.MarshalIndent(result, "", "  ")
	}
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "compositionExecutor", "Execute", "marshal")
	}
	return agentic.ToolResult{CallID: call.ID, Content: string(content)}, nil
}

// configFromArguments decodes the `config` argument through the production
// configuration loader so the tool judges exactly what a config file would.
func configFromArguments(arguments map[string]any) (*config.Config, error) {
	document, ok := arguments["config"]
	if !ok || document == nil {
		return nil, fmt.Errorf("config is required: a configuration document object")
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("config must be a JSON object: %w", err)
	}
	cfg, err := config.NewLoader().LoadFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("config could not be loaded: %w", err)
	}
	return cfg, nil
}

func formatArgument(arguments map[string]any) string {
	format, _ := arguments["format"].(string)
	if format == "" {
		return "json"
	}
	return format
}
