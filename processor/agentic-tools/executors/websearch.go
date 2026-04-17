package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

const (
	braveSearchEndpoint    = "https://api.search.brave.com/res/v1/web/search"
	braveMaxResults        = 10
	braveResponseBodyLimit = 100 * 1024 // 100KB
	webSearchDefaultMax    = 5
)

// WebSearchExecutor implements the web_search agentic tool.
type WebSearchExecutor struct {
	apiKey     string
	httpClient *http.Client
}

// NewWebSearchExecutor creates a web search executor backed by the Brave Search API.
func NewWebSearchExecutor(apiKey string) *WebSearchExecutor {
	return &WebSearchExecutor{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ListTools returns the web_search tool definition.
func (e *WebSearchExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "web_search",
			Description: "Search the web for documentation, API references, libraries, or technical solutions. Returns titles, URLs, and descriptions for matching results.",
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

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: sb.String(),
	}, nil
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
