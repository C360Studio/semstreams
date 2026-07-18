package researchassess

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Assessor is the narrow LLM surface this component consumes.
// Production satisfies it via llmAssessorAdapter wrapping a real
// graph/llm.Client; tests substitute a fake that returns a
// deterministic raw JSON response per scenario without standing up
// any LLM infrastructure.
//
// Assess returns the raw response Content + a short reason string the
// adapter can use for error diagnostics (model identifier, finish
// reason, etc.). The handler parses the Content as JSON into an
// AssessmentOutput and validates separately so prompt iteration and
// response-shape drift surface as separate test failures.
type Assessor interface {
	Assess(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (content string, reason string, err error)
}

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. Production wraps natsclient.KVStore; tests substitute an
// in-memory map.
type LoopStore interface {
	// GetIntent loads the research_intent payload from the
	// research.request.received.<loopID> key. Returned for the prompt's
	// topic + hints context.
	GetIntent(ctx context.Context, loopID string) (*research.Intent, error)

	// GetExecutionOutput loads the upstream ExecutionOutput from the
	// execute.complete.<loopID> trigger key. The assessor needs the
	// evidence array to decide sufficient / refine.
	GetExecutionOutput(ctx context.Context, loopID string) (*research.ExecutionOutput, error)

	// PutAssessmentOutput writes the AssessmentOutput envelope at R3's
	// trigger key assess.complete.<loopID>.
	PutAssessmentOutput(ctx context.Context, loopID string, envelope []byte) error

	// PutSnapshot writes the envelope at a stable non-trigger key
	// assess.snapshot.<loopID> so operators / downstream queryability
	// can read the full assessment without racing R3's wildcard
	// watcher.
	PutSnapshot(ctx context.Context, loopID string, envelope []byte) error
}

// Sentinel errors. Returned for diagnostic clarity in handler logs
// + so tests can assert specific failure modes via errors.Is.
var (
	errIntentNotFound          = fmt.Errorf("research intent not found")
	errExecutionOutputNotFound = fmt.Errorf("execution output not found")
)

// extractLoopIDFromSubject pulls the loop_id off the NATS subject
// component.assess_sufficiency.<loop_id>. Returns empty if the
// subject doesn't match — handler treats empty as a routing-config
// bug.
//
// Format is stable per ADR-045 §Rule chain — R3's publish action uses
// `subject: "component.assess_sufficiency.{loop_id}"`. Any deviation
// is a misconfigured rule, not a parsing problem.
func extractLoopIDFromSubject(subject string) string {
	const prefix = "component.assess_sufficiency."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// assessSufficiency is the pure assessment function over injected
// interfaces — no NATS plumbing leaks in here so the test matrix can
// drive it through fakes.
//
// Returns a validated AssessmentOutput on success. The handler
// surfaces Sufficient + RefinedQueries as a tuple, but a true
// Sufficient with non-empty RefinedQueries surfaces a Warn (the
// assessor shouldn't refine what it just declared sufficient — a
// frontier-model action-confusion signal). Keeps Sufficient as the
// load-bearing decision; rejecting would gate the chain on a
// known-noisy frontier-model behaviour.
//
// logger is used only for defense-in-depth Warn lines. nil is
// tolerated — falls back to slog.Default — so test callers can skip
// logger plumbing.
func assessSufficiency(ctx context.Context, assessor Assessor, intent *research.Intent, exec *research.ExecutionOutput, maxResponseTokens, maxEvidenceInPrompt, maxSnippetCharsInPrompt int, logger *slog.Logger) (*research.AssessmentOutput, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	if exec == nil {
		return nil, fmt.Errorf("execution output is nil")
	}
	if assessor == nil {
		return nil, fmt.Errorf("assessor is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	systemPrompt := buildSystemPrompt()
	userPrompt := buildUserPrompt(intent, exec, maxEvidenceInPrompt, maxSnippetCharsInPrompt)

	rawContent, reason, err := assessor.Assess(ctx, systemPrompt, userPrompt, maxResponseTokens)
	if err != nil {
		return nil, fmt.Errorf("assessor call: %w", err)
	}
	if strings.TrimSpace(rawContent) == "" {
		return nil, fmt.Errorf("assessor returned empty content (finish_reason=%q)", reason)
	}

	jsonBytes, err := llmwrap.ExtractJSON(rawContent)
	if err != nil {
		return nil, fmt.Errorf("extract JSON from assessor content: %w (raw=%q finish_reason=%q)", err, llmwrap.Truncate(rawContent, llmwrap.ErrPayloadTruncateBytes), reason)
	}

	// The wire shape is intentionally a subset of AssessmentOutput —
	// the assessor doesn't emit Topic/EvidenceCount/Degraded; those
	// are populated server-side from the upstream context. Decode
	// into a narrow struct first to keep the LLM contract honest,
	// then fold into the full payload.
	var raw struct {
		Sufficient     bool     `json:"sufficient"`
		RefinedQueries []string `json:"refined_queries,omitempty"`
		Rationale      string   `json:"rationale,omitempty"`
		Confidence     float64  `json:"confidence,omitempty"`
	}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode assessment output: %w (raw=%q)", err, llmwrap.Truncate(string(jsonBytes), llmwrap.ErrPayloadTruncateBytes))
	}

	// Drop empty / whitespace-only refined queries the model may emit
	// before validation reaches Validate. Empty strings here are
	// frontier-model noise, not assessor intent; preserving them
	// would block Validate downstream.
	cleaned := raw.RefinedQueries[:0]
	for _, q := range raw.RefinedQueries {
		if strings.TrimSpace(q) != "" {
			cleaned = append(cleaned, q)
		}
	}
	raw.RefinedQueries = cleaned

	// Clamp confidence to [0,1] silently — a frontier model emitting
	// 1.05 or -0.1 is a known noise pattern; rejecting would gate the
	// chain. Clamp keeps the typed field meaningful for trajectory
	// review.
	if raw.Confidence < 0 {
		raw.Confidence = 0
	}
	if raw.Confidence > 1 {
		raw.Confidence = 1
	}

	// Sufficient=true with non-empty RefinedQueries → Warn, keep
	// Sufficient as the load-bearing decision. The downstream R3
	// will route to synthesize on Sufficient; refine path won't see
	// the queries.
	if raw.Sufficient && len(raw.RefinedQueries) > 0 {
		logger.Warn("Sufficient=true emitted with non-empty refined_queries; possible action confusion",
			slog.Int("refined_query_count", len(raw.RefinedQueries)),
			slog.String("rationale", llmwrap.Truncate(raw.Rationale, llmwrap.ErrPayloadTruncateBytes)))
	}

	// Sufficient=false with empty RefinedQueries → also Warn. The
	// refine loop has nothing to dispatch; R4 will spin a no-op pass.
	// Don't override Sufficient — let the operator catch the prompt
	// shape via the Warn.
	if !raw.Sufficient && len(raw.RefinedQueries) == 0 {
		logger.Warn("Sufficient=false emitted with empty refined_queries; refine loop will no-op",
			slog.String("rationale", llmwrap.Truncate(raw.Rationale, llmwrap.ErrPayloadTruncateBytes)))
	}

	out := &research.AssessmentOutput{
		Topic:          intent.Topic,
		Sufficient:     raw.Sufficient,
		RefinedQueries: raw.RefinedQueries,
		Rationale:      raw.Rationale,
		Confidence:     raw.Confidence,
		EvidenceCount:  len(exec.Evidence),
		Degraded:       exec.Degraded,
		DegradedReason: exec.DegradedReason,
	}
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("validate assessment output: %w", err)
	}
	return out, nil
}
