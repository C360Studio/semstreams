package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	braveSearchEndpoint    = "https://api.search.brave.com/res/v1/web/search"
	braveMaxResults        = 10
	braveResponseBodyLimit = 100 * 1024 // 100KB
	webSearchDefaultMax    = 5

	// webSearchTripleSource is the Source field on triples this tool
	// publishes. Mirrors decide.go / emit_diagnosis / write_todos so
	// operators can distinguish web_search emissions in graph queries.
	webSearchTripleSource = "agent-web-search"
)

// WebSearchExecutor implements the web_search agentic tool.
//
// Triple emission is optional: when a non-nil TriplePublisher is supplied
// via WithWebSearchTriplePublisher, each successful search additionally
// emits 6 triples per result onto an agent.web.observation entity plus a
// back-link triple onto the calling loop entity. When the publisher is
// nil the executor behaves exactly as it did pre-emission — text-only
// return, no graph writes. Per-result emission errors are logged and
// counted (semstreams.agentic_tool_web.emit_failures_total) but never
// fail the tool, because graph emission is opportunistic substrate
// observation, not the tool's primary LLM-facing contract. This diverges
// from decide / write_todos where the triples ARE the contract.
type WebSearchExecutor struct {
	apiKey     string
	httpClient *http.Client

	// Optional triple-emission machinery. publisher == nil means emission
	// is fully disabled; the three companion fields default to package
	// defaults so a partially-configured executor stays safe.
	publisher agentictools.TriplePublisher
	platform  component.PlatformMeta
	logger    *slog.Logger
	now       func() time.Time
}

// WebSearchOption configures a WebSearchExecutor. All options are
// optional; the executor works with just an apiKey.
type WebSearchOption func(*WebSearchExecutor)

// WithWebSearchTriplePublisher enables graph emission. When the
// publisher is non-nil, each successful search result mints an
// agent.web.observation entity and writes the per-URL predicates plus a
// back-link from the calling loop. nil disables emission (default).
func WithWebSearchTriplePublisher(p agentictools.TriplePublisher) WebSearchOption {
	return func(e *WebSearchExecutor) { e.publisher = p }
}

// WithWebSearchPlatform supplies the platform identity used to build
// observation entity IDs and resolve the calling loop's entity ID.
// Required when publisher is non-nil; ignored otherwise.
func WithWebSearchPlatform(p component.PlatformMeta) WebSearchOption {
	return func(e *WebSearchExecutor) { e.platform = p }
}

// WithWebSearchLogger replaces the default logger (slog.Default()).
// nil-safe.
func WithWebSearchLogger(l *slog.Logger) WebSearchOption {
	return func(e *WebSearchExecutor) {
		if l != nil {
			e.logger = l
		}
	}
}

// WithWebSearchClock replaces the time source the executor stamps onto
// observed_at / triple timestamps. nil-safe. Tests use this for
// deterministic timestamp assertions; production should not.
func WithWebSearchClock(now func() time.Time) WebSearchOption {
	return func(e *WebSearchExecutor) {
		if now != nil {
			e.now = now
		}
	}
}

// NewWebSearchExecutor creates a web search executor backed by the
// Brave Search API. The variadic options enable opt-in features
// (triple emission, custom logger/clock) without disturbing legacy
// call sites that pass only the API key.
func NewWebSearchExecutor(apiKey string, opts ...WebSearchOption) *WebSearchExecutor {
	e := &WebSearchExecutor{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: slog.Default(),
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ListTools returns the web_search tool definition.
func (e *WebSearchExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "web_search",
			Description: "Search the web for documentation, API references, libraries, or technical solutions. Returns titles, URLs, and descriptions for matching results.",
			// mutating, NOT read_only, and deliberately divergent from the
			// stub's read_only: this executor writes to the graph. Whenever a
			// NATS client is present (both shipped binaries), registerWebSearch
			// attaches a TriplePublisher and Execute calls emitObservations,
			// which publishes 7 triples per result — a loop back-link plus six
			// predicates on a minted agent.web.observation entity. Worst-effect
			// classifies on what the tool CAN do, and in-deployment state
			// mutation is exactly what mutating means.
			//
			// The outbound Brave query on its own would be read_only (an
			// external READ is read_only; metered cost is not an effect — see
			// ToolEffect docs). The graph write is what decides this.
			Effect: agentic.ToolEffectMutating,
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

// Execute dispatches tool calls.
func (e *WebSearchExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != "web_search" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}

	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	maxResults := webSearchDefaultMax
	if v, ok := call.Arguments["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}
	if maxResults > braveMaxResults {
		maxResults = braveMaxResults
	}

	results, err := e.search(ctx, query, maxResults)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("search failed: %v", err),
		}, nil
	}

	if len(results) == 0 {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: "No results found.",
		}, nil
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, r.title)
		fmt.Fprintf(&sb, "    %s\n", r.url)
		if r.description != "" {
			fmt.Fprintf(&sb, "    %s\n", r.description)
		}
		if i < len(results)-1 {
			sb.WriteByte('\n')
		}
	}

	// Opportunistic graph emission. Failures here never affect the
	// LLM-facing return — log + counter + continue per the 2026-05-11
	// design decision. Detach from the caller's ctx so a cancellation
	// mid-emission doesn't leave half-written batches; bounded timeout
	// prevents a wedged graph-ingest from leaking goroutines.
	if e.publisher != nil {
		emitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), webEmitTimeout)
		defer cancel()
		e.emitObservations(emitCtx, call, query, results)
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: sb.String(),
	}, nil
}

// emitObservations writes per-result observation triples plus loop
// back-links. Each result is handled independently: an entity-ID
// failure on result N (malformed URL from the search provider) does
// not skip N+1. Publish failures likewise log + counter + continue.
func (e *WebSearchExecutor) emitObservations(ctx context.Context, call agentic.ToolCall, query string, results []searchResult) {
	if call.LoopID == "" {
		// Without a loop_id we can't build the back-link triple; rather
		// than emit dangling URL entities, skip the whole batch and log
		// once. The LLM-side result is unaffected.
		e.logger.Warn("web_search emission skipped: tool call missing loop_id",
			"call_id", call.ID, "result_count", len(results))
		return
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		e.logger.Warn("web_search emission skipped: cannot resolve loop entity",
			"call_id", call.ID, "loop_id", call.LoopID, "error", err)
		return
	}

	now := e.now()
	observedAt := now.UTC().Format(time.RFC3339Nano)

	for _, r := range results {
		urlEntity, canon, err := agentic.TryWebObservationEntityID(e.platform.Org, e.platform.Platform, r.url)
		if err != nil {
			webEmitFailuresTotal.WithLabelValues("web_search", "entity_id").Inc()
			e.logger.Warn("web_search emission skipped result: cannot build observation entity",
				"call_id", call.ID, "url", r.url, "error", err)
			continue
		}

		// The registered observation entity is the one builder of its triples
		// (ADR-103); Tool selects the web_search source and predicate set.
		observation := &agentic.WebObservationEntity{
			Org: e.platform.Org, Platform: e.platform.Platform, CanonicalURL: canon,
			Tool: agentic.WebObservationToolWebSearch, LoopEntityID: loopEntityID,
			Title: r.title, Snippet: r.description, SourceQuery: query, ObservedAt: observedAt,
		}
		if err := observation.Validate(); err != nil {
			webEmitFailuresTotal.WithLabelValues("web_search", "validate").Inc()
			e.logger.Warn("web_search emission skipped result: observation fails its contract",
				"call_id", call.ID, "url", canon, "error", err)
			continue
		}
		if err := publishWebObservation(ctx, e.publisher, urlEntity, observation.Triples()); err != nil {
			webEmitFailuresTotal.WithLabelValues("web_search", "publish").Inc()
			e.logger.Warn("web_search observation emission failed",
				"call_id", call.ID, "url", canon, "error", err)
			continue
		}
		backlink := message.Triple{
			Subject: loopEntityID, Predicate: agvocab.LoopObservedWeb, Object: urlEntity,
			Source: webSearchTripleSource, Timestamp: now, Confidence: 1.0,
		}
		if err := e.publisher.Append(ctx, []message.Triple{backlink}); err != nil {
			webEmitFailuresTotal.WithLabelValues("web_search", "publish").Inc()
			e.logger.Warn("web_search backlink emission failed",
				"call_id", call.ID, "url", canon, "error", err)
		}
	}
}

type searchResult struct {
	title       string
	url         string
	description string
}

func (e *WebSearchExecutor) search(ctx context.Context, query string, maxResults int) ([]searchResult, error) {
	reqURL, err := url.Parse(braveSearchEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("count", strconv.Itoa(maxResults))
	reqURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", e.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, braveResponseBodyLimit))
		return nil, fmt.Errorf("brave search returned %d: %s", resp.StatusCode, string(body))
	}

	var raw braveResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, braveResponseBodyLimit)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]searchResult, 0, len(raw.Web.Results))
	for _, r := range raw.Web.Results {
		results = append(results, searchResult{
			title:       r.Title,
			url:         r.URL,
			description: r.Description,
		})
	}
	return results, nil
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}
