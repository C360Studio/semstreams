package agentictools

import (
	"strings"
)

// ToolDefinition represents a tool definition for discovery responses.
//
// A deliberately narrow projection of agentic.ToolDefinition, not a
// mirror: it adds discovery-shaped fields (Provider, Available) and
// drops canonical fields that have no discovery consumer. Which
// canonical fields are projected and which are dropped is pinned by
// TestDiscoveryProjection_CoversEveryCanonicalField — adding a field to
// the canonical struct fails that test until the projection decision is
// made explicitly, so a field cannot be lost here silently.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Available   bool   `json:"available"`

	// Effect is the RESOLVED effect classification — always present,
	// never omitted, carrying the literal "unknown" for a tool that
	// declares nothing (gh#749, ADR-089).
	//
	// Serving the resolved value rather than the raw declaration puts
	// the absent-means-unknown rule at the framework boundary once, so
	// no discovery consumer re-implements it. Descriptive only: it
	// reflects no approval or admission decision.
	Effect string `json:"effect"`
}

// ToolListResponse represents the response to a tool.list request
type ToolListResponse struct {
	Tools []ToolDefinition `json:"tools"`
}

// ConsumerNameForTool generates a JetStream consumer name for a tool.
// Sanitizes dots and underscores to dashes, adds "tool-exec-" prefix.
//
// Examples:
//
//	file_read → tool-exec-file-read
//	graph.query → tool-exec-graph-query
func ConsumerNameForTool(toolName string) string {
	// Replace dots and underscores with dashes
	sanitized := strings.ReplaceAll(toolName, ".", "-")
	sanitized = strings.ReplaceAll(sanitized, "_", "-")

	// Add prefix
	return "tool-exec-" + sanitized
}
