package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

const (
	// searchGraphSubject is the NATS subject the graph-query
	// component handles for natural-language search with built-in
	// semantic fallback (PR #54). The fallback logic lives server-
	// side so MCP / HTTP / GraphQL callers and the agent tool layer
	// share one implementation. See project_graph_tools_gateway_first_plan
	// memory; this tool is step 2.
	searchGraphSubject = "graph.query.searchGraph"

	// searchGraphTimeout — globalSearch is a multi-step path
	// (classifier + strategy + Tier 1 semantic + Tier 2 community)
	// and the existing server-side cap on each LLM-driven leg can
	// stretch to 60s. The 90s cap here is the agent-side ceiling;
	// in practice typical responses arrive well under 30s.
	searchGraphTimeout = 90 * time.Second
)

// SearchGraphExecutor implements the search_graph tool. Thin wrapper
// over the graph.query.searchGraph server-side resolver added in
// PR #54 — natural-language search via GraphRAG with a semantic-
// search fallback when classifier-routed strategies return empty.
//
// Read-only: no graph emission. The response from the server-side
// resolver is a GlobalSearchResponse which the tool formats for LLM
// consumption (answer text + entity digests + community summaries +
// degraded-banner when the fallback fired).
type SearchGraphExecutor struct {
	natsClient NATSQuerier
	timeout    time.Duration
}

// SearchGraphOption configures a SearchGraphExecutor.
type SearchGraphOption func(*SearchGraphExecutor)

// WithSearchGraphTimeout overrides the default request timeout.
func WithSearchGraphTimeout(d time.Duration) SearchGraphOption {
	return func(e *SearchGraphExecutor) { e.timeout = d }
}

// NewSearchGraphExecutor creates the executor. natsClient must be
// non-nil — without it the tool can't reach the server-side resolver.
func NewSearchGraphExecutor(natsClient NATSQuerier, opts ...SearchGraphOption) *SearchGraphExecutor {
	e := &SearchGraphExecutor{
		natsClient: natsClient,
		timeout:    searchGraphTimeout,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ListTools returns the search_graph definition. The description
// names search-before-query as the canonical discovery pattern
// (matches semspec's policy at semspec/tools/workflow/graph.go).
// Required query argument; level + max_communities are passthrough
// tuning knobs forwarded to the server-side globalSearch path.
func (e *SearchGraphExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: "search_graph",
		Description: "Search the knowledge graph in natural language. Returns a synthesized answer, the most relevant entities, and community summaries. " +
			"Call this BEFORE query_entity / query_relationships when you don't already know specific IDs — search surfaces the IDs and context worth querying directly. " +
			"For a structural overview of the graph (entity types and predicates) call summarize_graph instead.",
		Effect: agentic.ToolEffectReadOnly,
		Parameters: map[string]any{
			"type":                 "object",
			"required":             []string{"query"},
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural-language question or keyword search (e.g. \"how does authentication work\", \"recent errors in agentic-loop\").",
				},
				"level": map[string]any{
					"type":        "integer",
					"description": "Community level 0-3. Higher levels return broader, more summarized answers. Default 1.",
				},
				"max_communities": map[string]any{
					"type":        "integer",
					"description": "Maximum communities to search. Default 10.",
				},
			},
		},
	}}
}

// searchGraphArgs is the parsed shape of the tool call arguments.
// max_communities lives under the GraphQL/REST name maxCommunities on
// the server side; the gateway transforms the variable name. For NATS
// passthrough we use the snake_case wire name since we're calling the
// resolver subject directly.
type searchGraphArgs struct {
	Query          string `json:"query"`
	Level          int    `json:"level,omitempty"`
	MaxCommunities int    `json:"max_communities,omitempty"`
}

// Execute routes the tool call.
func (e *SearchGraphExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != "search_graph" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}

	args, err := parseSearchGraphArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	reqPayload, err := json.Marshal(args)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal search_graph args: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	// gh#93 Phase 2: RequestClassified unifies transport + handler
	// errors behind one classified return. classifyRequestError maps
	// transport failures to ToolErrorNetwork and handler-side
	// classified errors to ToolErrorExternal.
	respData, err := e.natsClient.RequestClassified(ctx, searchGraphSubject, reqPayload, e.timeout)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("search_graph: %s", err.Error()),
			ErrorKind: classifyRequestError(err),
		}, nil
	}

	// The server-side resolver returns a GlobalSearchResponse-shaped
	// payload directly (NOT wrapped in QueryResponse[T]) per the
	// existing graph-query convention — see handleSearchGraph in
	// processor/graph-query/searchgraph.go.
	var resp searchGraphResponsePayload
	if err := json.Unmarshal(respData, &resp); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("parse search_graph response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: formatSearchGraphResponse(&resp),
		Metadata: map[string]any{
			"strategy":      resp.Strategy,
			"degraded":      resp.Degraded,
			"result_count":  resp.Count,
			"digest_count":  len(resp.EntityDigests),
			"summary_count": len(resp.CommunitySummaries),
		},
	}, nil
}

// parseSearchGraphArgs reads the tool args and validates the required
// query field. Missing/empty query returns ToolErrorInvalidArgs so
// the LLM can self-correct rather than the request reaching the
// server-side handler which would reject it anyway.
func parseSearchGraphArgs(raw map[string]any) (searchGraphArgs, error) {
	var args searchGraphArgs
	if raw == nil {
		return args, fmt.Errorf("missing required argument %q", "query")
	}
	query, ok := raw["query"].(string)
	if !ok || query == "" {
		return args, fmt.Errorf("argument %q must be a non-empty string", "query")
	}
	args.Query = query
	if v, ok := raw["level"].(float64); ok {
		args.Level = int(v)
	}
	if v, ok := raw["max_communities"].(float64); ok {
		args.MaxCommunities = int(v)
	}
	return args, nil
}

// searchGraphResponsePayload mirrors the on-wire shape of
// processor/graph-query.GlobalSearchResponse so the executor can
// decode + format without taking a production-side dependency on
// graph-query (no current cycle, but keeping the executor decoupled
// from graph-query's internal types preserves freedom for either
// package to evolve independently). The field set is a deliberate
// subset — we decode only what formatSearchGraphResponse touches.
//
// Drift risk: a future field add to GlobalSearchResponse won't
// surface here automatically. The
// TestSearchGraphResponsePayload_DecodesFromRealGlobalSearchResponse
// round-trip test in search_graph_test.go pins the contract by
// importing the real type (test-only, no production cycle); any
// drift on either side trips it immediately.
type searchGraphResponsePayload struct {
	Strategy           string                     `json:"strategy,omitempty"`
	Answer             string                     `json:"answer,omitempty"`
	AnswerModel        string                     `json:"answer_model,omitempty"`
	Count              int                        `json:"count"`
	EntityDigests      []searchGraphEntityDigest  `json:"entity_digests,omitempty"`
	CommunitySummaries []searchGraphCommunityItem `json:"community_summaries,omitempty"`
	Degraded           bool                       `json:"degraded,omitempty"`
	DegradedReason     string                     `json:"degraded_reason,omitempty"`
}

type searchGraphEntityDigest struct {
	ID        string  `json:"id"`
	Type      string  `json:"type,omitempty"`
	Label     string  `json:"label,omitempty"`
	Relevance float64 `json:"relevance,omitempty"`
}

type searchGraphCommunityItem struct {
	Summary string `json:"summary,omitempty"`
}

// formatSearchGraphResponse renders the response in the prompt-
// injection-friendly shape semspec converged on (their
// tools/workflow/graph.go:formatSearchResult). Priority:
// degraded banner → answer → entity digests → community summaries →
// fallback to "no results".
//
// Degraded banner FIRST so the reading agent treats the answer as
// advisory when the server-side LLM synthesizer fell back to template
// or the GraphRAG path fell back to semantic — without this banner
// the agent can't tell a rich answer from a template-extracted one
// and may over-trust degraded synthesis.
func formatSearchGraphResponse(r *searchGraphResponsePayload) string {
	var sb strings.Builder

	if r.Degraded {
		reason := r.DegradedReason
		if reason == "" {
			reason = "unspecified"
		}
		fmt.Fprintf(&sb, "⚠ degraded response (reason: %s, strategy: %s) — treat as advisory; consider retrying or verifying via query_entity.\n\n", reason, r.Strategy)
	}

	if r.Answer != "" {
		sb.WriteString(r.Answer)
		if r.AnswerModel != "" {
			fmt.Fprintf(&sb, "\n\n(synthesized by %s)", r.AnswerModel)
		}
	}

	if len(r.EntityDigests) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n---\nMatched entities:\n")
		} else {
			sb.WriteString("Matched entities:\n")
		}
		for _, d := range r.EntityDigests {
			switch {
			case d.Label != "":
				fmt.Fprintf(&sb, "- %s [%s] %s\n", d.Label, d.Type, d.ID)
			case d.Type != "":
				fmt.Fprintf(&sb, "- [%s] %s\n", d.Type, d.ID)
			default:
				fmt.Fprintf(&sb, "- %s\n", d.ID)
			}
		}
	}

	if len(r.CommunitySummaries) > 0 && sb.Len() == 0 {
		sb.WriteString("Knowledge clusters:\n")
		for _, c := range r.CommunitySummaries {
			if c.Summary != "" {
				fmt.Fprintf(&sb, "\n%s\n", c.Summary)
			}
		}
	}

	if sb.Len() == 0 {
		if r.Count > 0 {
			return fmt.Sprintf("Found %d entities but no summary available. Use query_entity for specific lookups.", r.Count)
		}
		return "No results found."
	}

	return sb.String()
}
