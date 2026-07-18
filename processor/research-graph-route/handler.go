package researchroute

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Router is the narrow LLM surface this component consumes.
// Production satisfies it via llmRouterAdapter wrapping a real
// graph/llm.Client; tests substitute a fake that returns a
// deterministic raw JSON response per scenario without standing up
// any LLM infrastructure.
//
// Route returns the raw response Content + a short reason string the
// adapter can use for error diagnostics (model identifier, finish
// reason, etc.). The handler parses the Content as JSON into a
// RouteDecision and validates separately so prompt iteration and
// response-shape drift surface as separate test failures.
type Router interface {
	Route(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (content string, reason string, err error)
}

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. Production wraps natsclient.KVStore; tests substitute an
// in-memory map.
type LoopStore interface {
	// GetIntent loads the research_intent payload from the
	// research.request.received.<loopID> key. Returned for the prompt's
	// topic + hints context.
	GetIntent(ctx context.Context, loopID string) (*research.Intent, error)

	// GetClassifierOutput loads the upstream ClassifierOutput from
	// the classify.complete.<loopID> trigger key. The router needs
	// the candidate set, hints, and confidence to make its decision.
	GetClassifierOutput(ctx context.Context, loopID string) (*research.ClassifierOutput, error)

	// PutRouteDecision writes the RouteDecision envelope at R2's
	// trigger key route.complete.<loopID>.
	PutRouteDecision(ctx context.Context, loopID string, envelope []byte) error

	// PutSnapshot writes the envelope at a stable non-trigger key
	// route.snapshot.<loopID> so operators / downstream queryability
	// can read the full decision without racing R2's wildcard
	// watcher. Same pattern as research-graph-classify.
	PutSnapshot(ctx context.Context, loopID string, envelope []byte) error
}

// Sentinel errors. Returned for diagnostic clarity in handler logs
// + so tests can assert specific failure modes via errors.Is.
var (
	errIntentNotFound           = fmt.Errorf("research intent not found")
	errClassifierOutputNotFound = fmt.Errorf("classifier output not found")
)

// extractLoopIDFromSubject pulls the loop_id off the NATS subject
// component.route_search.<loop_id>. Returns empty if the subject
// doesn't match — handler treats empty as a routing-config bug.
//
// Format is stable per ADR-045 §Rule chain — R1's publish action
// uses `subject: "component.route_search.{loop_id}"`. Any deviation
// is a misconfigured rule, not a parsing problem.
func extractLoopIDFromSubject(subject string) string {
	const prefix = "component.route_search."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// routeDecision is the pure routing function over injected
// interfaces — no NATS plumbing leaks in here so the test matrix can
// drive it through fakes.
//
// Returns a validated RouteDecision (action enum + per-action arg
// shape) on success. Per-action arg validation is done here, not in
// adapters.go, so a malformed model response surfaces as a specific
// "router emitted invalid X" error rather than a generic decode
// failure downstream.
//
// logger is used only for defense-in-depth Warn lines (e.g. a
// model that emitted non-empty args for synthesize_directly). nil
// is tolerated — falls back to slog.Default — so test callers can
// skip logger plumbing.
func routeDecision(ctx context.Context, router Router, intent *research.Intent, classifierOut *research.ClassifierOutput, maxResponseTokens, maxCandidatesInPrompt int, logger *slog.Logger) (*research.RouteDecision, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	if classifierOut == nil {
		return nil, fmt.Errorf("classifier output is nil")
	}
	if router == nil {
		return nil, fmt.Errorf("router is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	systemPrompt := buildSystemPrompt()
	userPrompt := buildUserPrompt(intent, classifierOut, maxCandidatesInPrompt)

	rawContent, reason, err := router.Route(ctx, systemPrompt, userPrompt, maxResponseTokens)
	if err != nil {
		return nil, fmt.Errorf("router call: %w", err)
	}
	if strings.TrimSpace(rawContent) == "" {
		return nil, fmt.Errorf("router returned empty content (finish_reason=%q)", reason)
	}

	jsonBytes, err := llmwrap.ExtractJSON(rawContent)
	if err != nil {
		return nil, fmt.Errorf("extract JSON from router content: %w (raw=%q finish_reason=%q)", err, llmwrap.Truncate(rawContent, llmwrap.ErrPayloadTruncateBytes), reason)
	}

	var decision research.RouteDecision
	if err := json.Unmarshal(jsonBytes, &decision); err != nil {
		return nil, fmt.Errorf("decode route decision: %w (raw=%q)", err, llmwrap.Truncate(string(jsonBytes), llmwrap.ErrPayloadTruncateBytes))
	}
	if err := decision.Validate(); err != nil {
		return nil, fmt.Errorf("router emitted invalid action: %w", err)
	}

	// Per-action arg shape validation. Surface a specific error per
	// branch so prompt iteration can target the failing path.
	switch decision.Action {
	case research.ActionDecompose:
		if _, err := decision.ParseDecomposeArgs(); err != nil {
			return nil, fmt.Errorf("router emitted invalid decompose args: %w", err)
		}
	case research.ActionWalkSeeds:
		if _, err := decision.ParseWalkSeedsArgs(); err != nil {
			return nil, fmt.Errorf("router emitted invalid walk_seeds args: %w", err)
		}
	case research.ActionRetighten:
		if _, err := decision.ParseRetightenArgs(); err != nil {
			return nil, fmt.Errorf("router emitted invalid retighten args: %w", err)
		}
	case research.ActionSynthesizeDirectly:
		// No args expected. Frontier models sometimes emit empty maps
		// even when told not to — that's not a routing failure. But
		// NON-empty args here means the model picked synthesize_directly
		// while emitting args that would have been valid for a
		// different action (likely confusion between branches), and the
		// downstream synthesizer will silently lose that intent. Surface
		// a Warn so operator trajectory review catches it. Keep routing
		// to synthesize_directly — rejecting would gate the chain on a
		// known-noisy frontier-model behaviour.
		if len(decision.Args) > 0 {
			logger.Warn("synthesize_directly emitted with non-empty args; possible action confusion",
				slog.String("action", decision.Action),
				slog.Int("args_count", len(decision.Args)),
				slog.String("rationale", llmwrap.Truncate(decision.Rationale, llmwrap.ErrPayloadTruncateBytes)))
		}
	}

	return &decision, nil
}

// extractJSON + truncate were promoted to processor/research-graph-
// llmwrap in PR 5 so assess_sufficiency + synthesize_answer can share
// the same balanced-brace + ellipsis-cap helpers. Route_search reaches
// for them via llmwrap.ExtractJSON / llmwrap.Truncate; same
// ErrPayloadTruncateBytes constant.
