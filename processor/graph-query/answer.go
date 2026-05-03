package graphquery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
)

// DefaultAnswerSynthesisTimeout caps a single answer-synthesis LLM call
// when no operator-configured timeout is supplied. Sized to leave
// substantial budget for the rest of the response path under typical
// graph-gateway request deadlines (30-60s); slow upstream models that
// need longer should configure capability.timeout or endpoint.request_timeout
// explicitly. The bounded sub-timeout is what makes the
// template-fallback path transparent to the HTTP layer — without it, a
// slow LLM can eat the entire request ctx and the fallback runs after
// the gateway has already returned an error to the HTTP client.
const DefaultAnswerSynthesisTimeout = 15 * time.Second

// AnswerSynthesizer produces a natural language answer from community summaries
// in response to a globalSearch query.
type AnswerSynthesizer interface {
	// Synthesize produces an answer to the query based on community summaries.
	// Returns the answer text and the model name used (empty for template fallback).
	Synthesize(ctx context.Context, query string, summaries []CommunitySummary, totalEntities int) (answer string, model string, err error)

	// Close releases any resources held by the synthesizer.
	Close() error
}

// answerSynthesisSystemPrompt is the system prompt for LLM-backed answer synthesis.
const answerSynthesisSystemPrompt = `You are a knowledge graph query assistant. Given a user query and summaries of related knowledge clusters, synthesize a concise answer that directly addresses the query.

Each cluster summary describes a group of related entities in the knowledge graph. Use the cluster summaries, representative entities, and keywords to construct your answer. Reference specific entities by name when relevant.

Be direct and factual. If the clusters don't contain enough information to fully answer the query, say what is known and what is missing. Do not speculate beyond the provided data.`

// LLMAnswerSynthesizer uses an LLM to produce query-focused answers from
// community summaries. Falls back to template synthesis on LLM error or
// timeout — see Synthesize for the bounded-timeout contract.
type LLMAnswerSynthesizer struct {
	client    llm.Client
	modelName string
	logger    *slog.Logger
	// timeout caps each Synthesize call's LLM round-trip independently
	// of the parent ctx's deadline. Zero means use DefaultAnswerSynthesisTimeout;
	// any positive value is honoured as-is. Operators set this via
	// capability.timeout or endpoint.request_timeout in the model
	// registry; the component reads those at construction time.
	timeout time.Duration
}

// NewLLMAnswerSynthesizer creates an LLM-backed answer synthesizer with a
// bounded per-call LLM timeout. timeout=0 selects DefaultAnswerSynthesisTimeout
// (15s). Operators configure the value via capability.timeout (preferred,
// applies to every endpoint that handles the capability) or endpoint.request_timeout
// (applies only to the configured endpoint); see component.initAnswerSynthesizer
// for the resolution order.
func NewLLMAnswerSynthesizer(client llm.Client, modelName string, logger *slog.Logger, timeout time.Duration) *LLMAnswerSynthesizer {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = DefaultAnswerSynthesisTimeout
	}
	return &LLMAnswerSynthesizer{client: client, modelName: modelName, logger: logger, timeout: timeout}
}

// Close releases the LLM client resources.
func (s *LLMAnswerSynthesizer) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// answerSynthesisMaxTokens is the maximum tokens for the LLM answer response.
const answerSynthesisMaxTokens = 500

// answerSynthesisTemperature controls randomness in answer generation (low = factual).
var answerSynthesisTemperature = 0.3

// Synthesize produces a query-focused answer by sending community summaries to the LLM.
// On LLM failure OR sub-timeout, falls back to template synthesis and logs
// the error internally — the fallback path is transparent to the HTTP
// layer and never propagates an error.
//
// The LLM call runs under a sub-context bounded by s.timeout, NOT under
// the parent ctx directly. This is the load-bearing detail: a slow
// upstream model (semspec hit this with seminstruct as
// answer_synthesis) can otherwise consume the entire gateway HTTP
// request budget; the fallback then runs after the gateway has already
// returned an error to the client. Bounding here leaves margin for the
// rest of the response path (response marshalling, NATS reply) so the
// template fallback actually reaches the HTTP layer.
//
// If the parent ctx has less budget than s.timeout, the parent's
// deadline wins (context.WithTimeout uses the earlier of the two). If
// the parent ctx is already cancelled, the LLM call returns immediately
// with the inherited error and we still fall back to template.
func (s *LLMAnswerSynthesizer) Synthesize(ctx context.Context, query string, summaries []CommunitySummary, totalEntities int) (string, string, error) {
	if len(summaries) == 0 {
		return "", "", nil
	}

	userPrompt := buildAnswerPrompt(query, summaries, totalEntities)

	llmCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.client.ChatCompletion(llmCtx, llm.ChatRequest{
		SystemPrompt: answerSynthesisSystemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    answerSynthesisMaxTokens,
		Temperature:  &answerSynthesisTemperature,
	})
	if err != nil {
		s.logger.Warn("LLM answer synthesis failed, using template fallback",
			slog.String("query", query),
			slog.Duration("timeout", s.timeout),
			slog.Any("error", err))
		return synthesizeAnswer(summaries, totalEntities), "", nil
	}

	return resp.Content, s.modelName, nil
}

// TemplateAnswerSynthesizer produces answers from community summaries using
// string templates. No LLM call required — used as fallback when no
// answer_synthesis endpoint is configured.
type TemplateAnswerSynthesizer struct{}

// Synthesize produces a template-based answer.
func (s *TemplateAnswerSynthesizer) Synthesize(_ context.Context, _ string, summaries []CommunitySummary, totalEntities int) (string, string, error) {
	return synthesizeAnswer(summaries, totalEntities), "", nil
}

// Close is a no-op for the template synthesizer.
func (s *TemplateAnswerSynthesizer) Close() error { return nil }

// buildAnswerPrompt constructs the user prompt for LLM answer synthesis.
func buildAnswerPrompt(query string, summaries []CommunitySummary, totalEntities int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Query: %s\n\n", query))
	b.WriteString(fmt.Sprintf("The knowledge graph contains %d matching entities across %d clusters.\n\n", totalEntities, len(summaries)))

	limit := len(summaries)
	if limit > MaxAnswerClusters {
		limit = MaxAnswerClusters
	}

	for i, s := range summaries[:limit] {
		b.WriteString(fmt.Sprintf("Cluster %d", i+1))
		if s.MemberCount > 0 {
			b.WriteString(fmt.Sprintf(" (%d entities, %.0f%% match)", s.MemberCount, s.Relevance*100))
		}
		b.WriteString(":\n")

		if s.Summary != "" {
			b.WriteString(s.Summary)
			b.WriteByte('\n')
		}

		if len(s.Entities) > 0 {
			names := make([]string, len(s.Entities))
			for j, e := range s.Entities {
				names[j] = fmt.Sprintf("%s [%s]", e.Label, e.Type)
			}
			b.WriteString(fmt.Sprintf("Representatives: %s\n", strings.Join(names, ", ")))
		}

		if len(s.Keywords) > 0 {
			kwLimit := len(s.Keywords)
			if kwLimit > 5 {
				kwLimit = 5
			}
			b.WriteString(fmt.Sprintf("Keywords: %s\n", strings.Join(s.Keywords[:kwLimit], ", ")))
		}

		b.WriteByte('\n')
	}

	b.WriteString("Synthesize a concise answer to the query based on these clusters.")
	return b.String()
}
