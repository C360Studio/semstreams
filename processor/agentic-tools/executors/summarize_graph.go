package executors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
)

// natsErrorPrefix is the convention natsclient.SubscribeForRequests
// uses to surface handler-side Go errors back to callers — the
// response body is `error: <msg>`, NOT a transport-level error.
// Every NATS Request caller must check for this prefix before
// attempting to decode the body as the expected response shape,
// otherwise a downstream component failure surfaces to the LLM as
// an opaque "parse response" error pointing at the wrong fix.
//
// See memory feedback_natsclient_error_payload_convention.md —
// this convention bit PR #48 twice (todos.go H1 + pathrag.go
// sister-bug). Every new NATS query caller in this PR follows the
// same pattern.
var natsErrorPrefix = []byte("error: ")

// extractNATSHandlerError inspects raw NATS response bytes for the
// natsclient "error: <msg>" handler-error envelope. Returns
// (extracted-message, true) when the body matches the convention,
// otherwise ("", false). Callers should propagate the extracted
// message as ToolErrorExternal (downstream component is alive but
// reporting failure) rather than ToolErrorInternal (we can't parse
// the response).
func extractNATSHandlerError(data []byte) (string, bool) {
	if bytes.HasPrefix(data, natsErrorPrefix) {
		return string(data[len(natsErrorPrefix):]), true
	}
	return "", false
}

const (
	// summarizeGraphSubject is the NATS subject the graph-query
	// component handles to compose a graph overview (PR #54). The
	// tool is the agent-facing thin wrapper; all aggregation logic
	// (predicate counts, entity-type distribution, example IDs)
	// lives server-side so MCP/HTTP/GraphQL callers and the agent
	// tool layer share one implementation. See
	// project_graph_tools_gateway_first_plan memory for the four-
	// step plan; this tool is step 2.
	summarizeGraphSubject = "graph.query.summary"

	// summarizeGraphTimeout caps the round-trip. The server-side
	// handler can scan up to entity_sample_limit entity IDs plus one
	// predicate-list call; the existing graph-ingest prefix handler
	// uses a 5s internal cap, so 10s gives headroom for an in-process
	// hop plus aggregation.
	summarizeGraphTimeout = 10 * time.Second

	// summarizeGraphPredicateTopN bounds how many predicates land in
	// the formatted text. The full list is in the structured response
	// for programmatic callers; the agent-facing text trims to keep
	// prompt size sane. Operators wanting more flip include_predicates
	// off and call graph_query / predicateList directly for the full
	// list.
	summarizeGraphPredicateTopN = 20
)

// NATSQuerier is the narrow interface SummarizeGraphExecutor and
// SearchGraphExecutor use to round-trip NATS requests. Production
// satisfies it with *natsclient.Client; tests substitute an in-
// memory recorder.
type NATSQuerier interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// SummarizeGraphExecutor implements the summarize_graph tool. Thin
// wrapper over the graph.query.summary server-side resolver added in
// PR #54 — solves the chicken-and-egg problem where existing query_*
// tools need known IDs to start, by giving agents a discovery surface
// (entity types, predicate counts, example IDs per type) without
// requiring any pre-existing context.
//
// Read-only: emits no graph state, takes no LoopID, requires no
// platform identity. Pure retrieval + formatting.
type SummarizeGraphExecutor struct {
	natsClient NATSQuerier
	timeout    time.Duration
}

// SummarizeGraphOption configures a SummarizeGraphExecutor.
type SummarizeGraphOption func(*SummarizeGraphExecutor)

// WithSummarizeGraphTimeout overrides the default request timeout.
func WithSummarizeGraphTimeout(d time.Duration) SummarizeGraphOption {
	return func(e *SummarizeGraphExecutor) { e.timeout = d }
}

// NewSummarizeGraphExecutor creates the executor. natsClient must be
// non-nil — without it the tool can't reach the server-side resolver.
func NewSummarizeGraphExecutor(natsClient NATSQuerier, opts ...SummarizeGraphOption) *SummarizeGraphExecutor {
	e := &SummarizeGraphExecutor{
		natsClient: natsClient,
		timeout:    summarizeGraphTimeout,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ListTools returns the summarize_graph definition. The description
// teaches the discovery-before-query pattern that semspec / semteams
// rely on. Single optional bool argument; predicate facet defaults on
// because that's the more useful default for first-call discovery.
func (e *SummarizeGraphExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: "summarize_graph",
		Description: "Get a high-level overview of what's in the knowledge graph: entity types and their counts, predicates with their counts, and example entity IDs per type. " +
			"Call this BEFORE query_entity or query_relationships when you don't already know specific IDs to query — the example IDs are real and copy-pasteable. " +
			"Use search_graph for natural-language exploration once you know roughly what you're looking for.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"include_predicates": map[string]any{
					"type":        "boolean",
					"description": "Include predicate counts in the response. Default true. Pass false to skip the predicate section if you only need entity-type discovery.",
				},
			},
		},
		// Strict: false — single optional bool envelope, strict mode
		// would only bracket an already-narrow surface. Matches
		// scratchpad's 2026-05-12 reasoning.
	}}
}

// summarizeGraphArgs is the parsed shape of the tool call arguments.
type summarizeGraphArgs struct {
	IncludePredicates *bool `json:"include_predicates,omitempty"`
}

// Execute routes the tool call.
func (e *SummarizeGraphExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != "summarize_graph" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}

	args := parseSummarizeGraphArgs(call.Arguments)
	reqPayload, err := json.Marshal(args)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal summarize_graph args: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	respData, err := e.natsClient.Request(ctx, summarizeGraphSubject, reqPayload, e.timeout)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("summarize_graph NATS request failed: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, nil
	}

	// natsclient convention: handler-side Go errors come back in the
	// body as "error: <msg>" rather than the err return. Check FIRST,
	// before json.Unmarshal — otherwise downstream failures surface
	// as opaque "parse response" errors and the LLM tries to fix the
	// wrong thing. See feedback_natsclient_error_payload_convention.md.
	if msg, ok := extractNATSHandlerError(respData); ok {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("summarize_graph server error: %s", msg),
			ErrorKind: agentic.ToolErrorExternal,
		}, nil
	}

	var resp graph.QueryResponse[graph.SummaryData]
	if err := json.Unmarshal(respData, &resp); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("parse summarize_graph response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}
	// Belt-and-suspenders: the server-side handler today always returns
	// (nil, err) on failure rather than NewQueryError, so resp.Error
	// is effectively unreachable. Keeping the check guards against a
	// future refactor that switches the handler to wrapped error
	// responses without breaking the agent-facing semantics.
	if resp.Error != "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("summarize_graph server error: %s", resp.Error),
			ErrorKind: agentic.ToolErrorExternal,
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: formatSummary(resp.Data),
		Metadata: map[string]any{
			"total_entities":          resp.Data.TotalEntities,
			"entity_sample_truncated": resp.Data.EntitySampleTruncated,
			"predicate_total":         resp.Data.PredicateTotal,
		},
	}, nil
}

// parseSummarizeGraphArgs reads the tool args into the typed shape.
// Missing fields stay nil (server-side applies defaults — single
// source of truth for the default semantics, matching the V1
// parseSummaryRequest implementation in processor/graph-query/).
func parseSummarizeGraphArgs(raw map[string]any) summarizeGraphArgs {
	var args summarizeGraphArgs
	if raw == nil {
		return args
	}
	if v, ok := raw["include_predicates"].(bool); ok {
		args.IncludePredicates = &v
	}
	return args
}

// formatSummary turns the structured response into LLM-friendly text.
// Three sections: header line with totals, entity-type table, optional
// predicate table. Counts are right-aligned in fixed-width columns so
// the table reads cleanly in monospace agent contexts.
//
// The example-ID lines are load-bearing for discovery — semspec's bug
// log (gemini-duplicate-graph-summary.md) showed agents invent IDs
// when no examples are visible. Always include at least one example
// per type.
func formatSummary(data graph.SummaryData) string {
	var sb strings.Builder

	// Header — total count + truncation marker.
	if data.EntitySampleTruncated {
		fmt.Fprintf(&sb, "Knowledge graph: at least %d entities scanned (sample truncated; counts approximate)\n", data.TotalEntities)
	} else {
		fmt.Fprintf(&sb, "Knowledge graph: %d entities\n", data.TotalEntities)
	}

	// Entity-type section.
	if len(data.EntityTypes) > 0 {
		sb.WriteString("\nBy type (domain.system.type):\n")
		// Pad type names so counts align. Cap padding so a single
		// pathologically-long type name doesn't blow out every row.
		typeWidth := 0
		for _, et := range data.EntityTypes {
			if l := len(et.Type); l > typeWidth {
				typeWidth = l
			}
		}
		if typeWidth > 50 {
			typeWidth = 50
		}
		for _, et := range data.EntityTypes {
			fmt.Fprintf(&sb, "  %-*s : %6d", typeWidth, et.Type, et.Count)
			if len(et.Examples) > 0 {
				fmt.Fprintf(&sb, "  (e.g. %s", et.Examples[0])
				if len(et.Examples) > 1 {
					fmt.Fprintf(&sb, ", %s", et.Examples[1])
				}
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}
	}

	// Predicate section. Top-N by count; full list available in the
	// structured response for callers wanting the rest.
	if len(data.Predicates) > 0 {
		topN := summarizeGraphPredicateTopN
		predicates := sortedPredicatesByCount(data.Predicates)
		if topN > len(predicates) {
			topN = len(predicates)
		}
		if data.PredicateTotal > topN {
			fmt.Fprintf(&sb, "\nPredicates (top %d of %d by count):\n", topN, data.PredicateTotal)
		} else {
			fmt.Fprintf(&sb, "\nPredicates (%d):\n", data.PredicateTotal)
		}
		predWidth := 0
		for i := 0; i < topN; i++ {
			if l := len(predicates[i].Predicate); l > predWidth {
				predWidth = l
			}
		}
		if predWidth > 50 {
			predWidth = 50
		}
		for i := 0; i < topN; i++ {
			fmt.Fprintf(&sb, "  %-*s : %6d\n", predWidth, predicates[i].Predicate, predicates[i].EntityCount)
		}
	}

	return sb.String()
}

// sortedPredicatesByCount returns a copy sorted by count desc with
// alphabetical tie-break. The server-side response order isn't
// guaranteed sorted, so we re-sort here rather than rely on the wire
// format.
func sortedPredicatesByCount(predicates []graph.PredicateSummary) []graph.PredicateSummary {
	out := make([]graph.PredicateSummary, len(predicates))
	copy(out, predicates)
	sort.Slice(out, func(i, j int) bool {
		if out[i].EntityCount != out[j].EntityCount {
			return out[i].EntityCount > out[j].EntityCount
		}
		return out[i].Predicate < out[j].Predicate
	})
	return out
}
