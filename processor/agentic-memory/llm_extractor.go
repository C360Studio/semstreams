package agenticmemory

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// LLMClient defines the interface for LLM operations
type LLMClient interface {
	ExtractFacts(ctx context.Context, model string, content string, maxTokens int) ([]message.Triple, error)
}

// LLMExtractor extracts semantic facts from agent responses using an LLM
type LLMExtractor struct {
	config    ExtractionConfig
	llmClient LLMClient
}

// NewLLMExtractor creates a new LLMExtractor instance.
//
// Fail loud (gh#317): enabling LLM-assisted extraction without a client is a
// silent no-op — ExtractFacts returns zero triples and never errors, so the
// config advertises a feature that produces nothing. Refuse it at construction.
// When extraction is genuinely wired (ADR-059, routing facts through the claim
// surface — never direct add_triples), the client is non-nil and this passes.
func NewLLMExtractor(config ExtractionConfig, llmClient LLMClient) (*LLMExtractor, error) {
	if config.LLMAssisted.Enabled && llmClient == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("extraction.llm_assisted.enabled=true but no LLM client is wired (silent no-op); set enabled=false until extraction is wired"),
			"LLMExtractor",
			"NewLLMExtractor",
			"validate llm client",
		)
	}
	return &LLMExtractor{
		config:    config,
		llmClient: llmClient,
	}, nil
}

// ExtractFacts extracts semantic triples from response content
func (e *LLMExtractor) ExtractFacts(ctx context.Context, loopID, responseContent string) ([]message.Triple, error) {
	// Validate inputs
	if loopID == "" {
		return nil, errs.WrapInvalid(
			fmt.Errorf("loopID cannot be empty"),
			"LLMExtractor",
			"ExtractFacts",
			"validate loopID",
		)
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Handle empty content gracefully
	if responseContent == "" {
		return []message.Triple{}, nil
	}

	// If LLM client is available, use it for extraction. The "enabled ⇒ client
	// present" invariant is enforced at construction (NewLLMExtractor, gh#317), so
	// reaching this with enabled=true and a nil client is unreachable today; the
	// nil branch below remains only for the disabled / not-yet-wired case.
	if e.llmClient != nil {
		triples, err := e.llmClient.ExtractFacts(
			ctx,
			e.config.LLMAssisted.Model,
			responseContent,
			e.config.LLMAssisted.MaxTokens,
		)
		if err != nil {
			return nil, errs.WrapTransient(err, "LLMExtractor", "ExtractFacts", "llm extraction")
		}
		return triples, nil
	}

	// No LLM client available, return empty triples
	return []message.Triple{}, nil
}
