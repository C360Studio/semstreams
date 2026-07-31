package executors

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// StubWebSearchExecutor returns canned search results when no Brave API key
// is available. Keeps web_search in the tool list so researcher-role agents
// and E2E fixtures work without external API dependencies.
type StubWebSearchExecutor struct{}

// NewStubWebSearchExecutor creates a stub that returns canned search results.
func NewStubWebSearchExecutor() *StubWebSearchExecutor {
	return &StubWebSearchExecutor{}
}

// ListTools returns the web_search tool definition.
func (e *StubWebSearchExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "web_search",
			Description: "Search the web for documentation, API references, libraries, or technical solutions. Returns titles, URLs, and descriptions for matching results.",
			// read_only, unlike the Brave-backed executor's mutating: the
			// stub has no TriplePublisher and emits nothing. Two definitions
			// of the same tool name legitimately carry different effects
			// because they ARE different implementations; discovery reports
			// whichever one this deployment registered.
			Effect: agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query — be specific for best results",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum results to return (default 5, max 10)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// Execute returns canned search results for any query.
func (e *StubWebSearchExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != "web_search" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}

	query, _ := call.Arguments["query"].(string)
	if query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[stub] web_search results for: %s\n\n", query))
	sb.WriteString("[1] NATS Documentation — Comparison with Other Systems\n")
	sb.WriteString("    https://docs.nats.io/compare\n")
	sb.WriteString("    NATS vs MQTT, Kafka, and other messaging systems for IoT and edge.\n\n")
	sb.WriteString("[2] MQTT Specification — OASIS Standard\n")
	sb.WriteString("    https://mqtt.org/specification\n")
	sb.WriteString("    MQTT v5.0 protocol specification for lightweight pub/sub messaging.\n\n")
	sb.WriteString("[3] Edge Computing Messaging Patterns\n")
	sb.WriteString("    https://example.com/edge-messaging\n")
	sb.WriteString("    Comparison of messaging protocols for constrained IoT environments.\n")

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: sb.String(),
	}, nil
}
