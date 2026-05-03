package query

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
)

// thinkTagPattern matches <think>...</think> blocks from reasoning models (qwen3, deepseek-r1).
var thinkTagPattern = regexp.MustCompile(`(?s)<think>.*?</think>`)

// DefaultClassificationTimeout caps a single query-classification LLM
// call when no operator-configured timeout is supplied. Sized small
// because classification produces a tiny JSON envelope — a slow
// response is a stuck call, not a long generation; healthy
// classifiers respond in well under a second. The bound exists so a
// slow LLM can't eat the full HTTP gateway budget and force
// handleGlobalSearch's keyword-fallback path to land after the gateway
// has already returned an error to the client. Operators running
// reasoning models that legitimately think (qwen3, deepseek-r1) can
// raise the bound via capability.timeout in the model registry.
const DefaultClassificationTimeout = 5 * time.Second

// LLMClientAdapter adapts a graph/llm.Client to the query.LLMClient interface.
//
// This allows the existing OpenAI-compatible LLM infrastructure (ollama, seminstruct,
// shimmy, OpenAI) to be used as the T3 classifier backend.
//
// Each ClassifyQuery call is bounded by an internal timeout (see
// timeout field) so a slow upstream model cannot consume the parent
// ctx's full budget. ClassifierChain treats any LLM error as
// "fall through to default T0 classification" (see classifier_chain.go),
// so a timeout-induced error degrades gracefully instead of dropping
// the gateway's response.
type LLMClientAdapter struct {
	client  llm.Client
	timeout time.Duration
}

// NewLLMClientAdapter wraps a graph/llm.Client for use as a
// query.LLMClient with a bounded per-call LLM timeout. timeout=0
// selects DefaultClassificationTimeout. Operators configure the value
// via capability.timeout (preferred) or endpoint.request_timeout in
// the model registry; graph-query's component reads those at
// construction time.
func NewLLMClientAdapter(client llm.Client, timeout time.Duration) *LLMClientAdapter {
	if timeout <= 0 {
		timeout = DefaultClassificationTimeout
	}
	return &LLMClientAdapter{client: client, timeout: timeout}
}

// ClassifyQuery sends the classification prompt to the LLM and returns
// the raw response.
//
// The LLM call runs under a sub-context bounded by a.timeout, NOT
// under the parent ctx directly. This is the load-bearing detail: a
// slow upstream model can otherwise consume the entire gateway HTTP
// request budget; ClassifierChain's keyword-fallback then runs after
// the gateway has already returned an error to the client. Bounding
// here leaves margin for the rest of the response path so the chain's
// fallback actually reaches the HTTP layer.
//
// If the parent ctx has less budget than a.timeout, the parent's
// deadline wins (context.WithTimeout uses the earlier of the two). If
// the parent ctx is already cancelled, the LLM call returns
// immediately with the inherited error and ClassifierChain falls
// through to the next tier.
func (a *LLMClientAdapter) ClassifyQuery(ctx context.Context, prompt string) (string, error) {
	temp := 0.1 // Low temperature for deterministic classification
	llmCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	resp, err := a.client.ChatCompletion(llmCtx, llm.ChatRequest{
		SystemPrompt: "You are a query classifier. Return ONLY valid JSON. No markdown, no explanation. Do not use <think> tags.",
		UserPrompt:   prompt,
		MaxTokens:    2048, // Reasoning models need headroom for thinking tokens
		Temperature:  &temp,
	})
	if err != nil {
		return "", err
	}
	return stripThinkTags(resp.Content), nil
}

// stripThinkTags removes <think>...</think> blocks and markdown code fences
// from reasoning model output to extract the raw JSON response.
func stripThinkTags(s string) string {
	// Remove <think>...</think> blocks
	s = thinkTagPattern.ReplaceAllString(s, "")

	// Remove markdown code fences (```json ... ``` or ``` ... ```)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		// Remove first line (```json or ```) and last line (```)
		if len(lines) >= 3 {
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) == "```" {
				end--
			}
			s = strings.Join(lines[1:end+1], "\n")
		}
	}

	return strings.TrimSpace(s)
}
