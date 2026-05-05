package graphquery

import (
	"context"
	"errors"
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

// SynthesisOutcome carries the result of an answer-synthesis attempt.
// Splitting it out from a return-value tuple keeps the
// degraded/non-degraded distinction self-describing at every call
// site and reserves a place for future signals (e.g., partial-match
// indicators, retry counts) without re-shaping the interface.
type SynthesisOutcome struct {
	// Answer is the natural-language answer text. Always populated
	// when len(summaries) > 0 — either by the LLM (canonical) or by
	// the template fallback (degraded). Callers can render this
	// directly without checking Degraded; the flag is for routing,
	// not for "should I show the text?"
	Answer string

	// Model names the LLM endpoint that produced Answer. Empty when
	// Answer came from the template fallback (either because the
	// synthesizer is configured as a TemplateAnswerSynthesizer with
	// no LLM, or because an LLM-configured synthesizer fell back).
	Model string

	// Degraded is true only when an LLM-configured synthesizer fell
	// back to the template path due to LLM failure or timeout.
	// Pure-template (no LLM ever configured) calls return
	// Degraded=false because the template IS the canonical answer
	// for that operator's deployment.
	//
	// The agent / caller MUST surface this flag to the LLM or
	// human consumer downstream — degraded answers are still
	// useful (entity hits + community summary text) but lose the
	// per-query LLM synthesis the operator paid for.
	Degraded bool

	// Reason classifies WHY synthesis was degraded. Empty when
	// Degraded=false. Concrete values:
	//   - "answer_synthesis_timeout": LLM call exceeded the bounded
	//     sub-timeout (the seminstruct-under-load case semspec hit).
	//   - "answer_synthesis_error":   any other LLM error
	//     (transport failure, provider-rejected payload, etc.).
	// Operator dashboards can group by Reason to distinguish
	// "model is overloaded" from "model is misconfigured."
	Reason string
}

// AnswerSynthesizer produces a natural language answer from community summaries
// in response to a globalSearch query.
type AnswerSynthesizer interface {
	// Synthesize produces an answer to the query based on community
	// summaries. Returns a SynthesisOutcome carrying the answer text,
	// the model name used (empty for template fallback), and a
	// Degraded flag set when an LLM-configured synthesizer fell back
	// to the template path. The error return is reserved for
	// programmer-error-shaped failures; LLM transport/timeout errors
	// are absorbed into Degraded=true so the caller can surface a
	// useful response instead of failing the request.
	Synthesize(ctx context.Context, query string, summaries []CommunitySummary, totalEntities int) (SynthesisOutcome, error)

	// Close releases any resources held by the synthesizer.
	Close() error
}

// Degraded reason constants. Kept in sync with the SynthesisOutcome
// doc-comment so adding a new reason value forces a single-source
// update.
const (
	// ReasonAnswerSynthesisTimeout means the LLM call exceeded the
	// bounded sub-timeout — typically "model is overloaded / network
	// slow" under sustained load. context.DeadlineExceeded surfaces
	// here when the sub-context bounded by s.timeout fires before
	// the LLM responds.
	ReasonAnswerSynthesisTimeout = "answer_synthesis_timeout"

	// ReasonAnswerSynthesisCancelled covers the parent-cancellation
	// shape: the inbound request ctx was cancelled (gateway client
	// disconnected, surrounding handler bailed) before the LLM call
	// could complete. context.Canceled surfaces here. Operationally
	// near-identical to a timeout — caller didn't get a useful answer
	// in time — but distinct so dashboards can separate
	// upstream-pressure ("model slow") from request-side abandonment
	// ("client gave up").
	ReasonAnswerSynthesisCancelled = "answer_synthesis_cancelled"

	// ReasonAnswerSynthesisError means any other LLM error: transport
	// failure, provider rejection, malformed response. Different
	// signal class from timeouts — typically misconfiguration or
	// upstream-API breaking change rather than load-shaped.
	ReasonAnswerSynthesisError = "answer_synthesis_error"
)

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
// On LLM failure OR sub-timeout, falls back to template synthesis with
// Degraded=true on the SynthesisOutcome — the fallback path is
// transparent to the HTTP layer and never propagates an error, but
// the Degraded flag lets downstream callers surface the lossy path
// to the agent / human consumer (the user's explicit constraint
// 2026-05-05 for the seminstruct-under-load case).
//
// The LLM call runs under a sub-context bounded by s.timeout, NOT under
// the parent ctx directly. This is the load-bearing detail: a slow
// upstream model can otherwise consume the entire gateway HTTP
// request budget; the fallback then runs after the gateway has already
// returned an error to the client. Bounding here leaves margin for the
// rest of the response path (response marshalling, NATS reply) so the
// template fallback actually reaches the HTTP layer.
//
// If the parent ctx has less budget than s.timeout, the parent's
// deadline wins (context.WithTimeout uses the earlier of the two). If
// the parent ctx is already cancelled, the LLM call returns immediately
// with the inherited error and we still fall back to template.
func (s *LLMAnswerSynthesizer) Synthesize(ctx context.Context, query string, summaries []CommunitySummary, totalEntities int) (SynthesisOutcome, error) {
	if len(summaries) == 0 {
		return SynthesisOutcome{}, nil
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
		// Classify so operators can group dashboards by failure shape.
		// Order matters: DeadlineExceeded check first because a
		// pre-cancelled parent ctx wrapped in WithTimeout can surface
		// either Canceled or DeadlineExceeded depending on timing —
		// favour the timeout label since the operational impact is
		// "didn't finish in time" regardless of which channel fired.
		reason := classifyDegradedReason(err)
		s.logger.Warn("LLM answer synthesis failed, using template fallback",
			slog.String("query", query),
			slog.Duration("timeout", s.timeout),
			slog.String("degraded_reason", reason),
			slog.Any("error", err))
		return SynthesisOutcome{
			Answer:   synthesizeAnswer(summaries, totalEntities),
			Degraded: true,
			Reason:   reason,
		}, nil
	}

	return SynthesisOutcome{Answer: resp.Content, Model: s.modelName}, nil
}

// classifyDegradedReason maps an LLM-call error to one of the
// ReasonAnswerSynthesis* constants. Centralised so synthesizer impls
// and the response-side fallback (synthesizeQueryAnswer) classify
// consistently.
func classifyDegradedReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonAnswerSynthesisTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ReasonAnswerSynthesisCancelled
	}
	return ReasonAnswerSynthesisError
}

// TemplateAnswerSynthesizer produces answers from community summaries using
// string templates. No LLM call required — used as default when no
// answer_synthesis endpoint is configured.
//
// Returns Degraded=false because the template path IS the canonical
// answer for an operator who didn't configure an LLM synthesizer —
// not a fallback. The Degraded flag signals "you expected LLM
// synthesis and didn't get it"; pure-template deployments never had
// that expectation.
type TemplateAnswerSynthesizer struct{}

// Synthesize produces a template-based answer.
func (s *TemplateAnswerSynthesizer) Synthesize(_ context.Context, _ string, summaries []CommunitySummary, totalEntities int) (SynthesisOutcome, error) {
	return SynthesisOutcome{Answer: synthesizeAnswer(summaries, totalEntities)}, nil
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
