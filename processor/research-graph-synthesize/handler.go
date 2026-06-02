package researchsynthesize

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Synthesizer is the narrow LLM surface this component consumes.
// Production satisfies it via llmSynthesizerAdapter wrapping a real
// graph/llm.Client; tests substitute a fake.
//
// Synthesize returns the raw response Content + a short reason string
// the adapter can use for error diagnostics. The handler parses the
// Content as JSON, applies quote-back validation, and folds the
// result into a research.SearchResult.
type Synthesizer interface {
	Synthesize(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (content string, reason string, err error)
}

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. The RouteDecision read is optional — when present, the
// DecompTrace captures the router's structural choice; when missing
// (synthesize_directly fast-path, or a misordered chain), the trace
// is built from the ExecutionOutput alone.
type LoopStore interface {
	GetIntent(ctx context.Context, loopID string) (*research.Intent, error)
	GetExecutionOutput(ctx context.Context, loopID string) (*research.ExecutionOutput, error)
	// GetRouteDecision returns (nil, nil) when the key is absent so
	// the synthesizer can degrade gracefully — RouteDecision is
	// observability-grade, not load-bearing for synthesis itself.
	GetRouteDecision(ctx context.Context, loopID string) (*research.RouteDecision, error)

	PutSearchResult(ctx context.Context, loopID string, envelope []byte) error
	PutSnapshot(ctx context.Context, loopID string, envelope []byte) error

	// PutLoopCompletion writes the SearchResult envelope at the
	// COMPLETE_<loopID> key — the convention the existing
	// read_loop_result tool uses (see
	// processor/agentic-tools/loop_result.go completeKeyPrefix). Lets
	// the R6 continuation rule's spawned parent agent fetch the
	// SearchResult via read_loop_result(loop_id=<rg_xxx>) without a
	// new tool. The duplicate-write cost is one extra KV put per
	// chain; the alternative (custom read_research_result tool) is
	// Phase 2 scope.
	//
	// Failure is BEST-EFFORT: the handler logs Warn and continues so
	// the orchestration triple still lands, R6 still fires, and the
	// parent's read_loop_result call surfaces a clean key-not-found
	// (which the parent's persona can degrade against — e.g.,
	// "synthesis arrived but the body wasn't fetchable, here's what
	// we know from the trajectory"). The chain-advance invariant
	// belongs at the triple stamp, not the read-side envelope. A
	// fatal-here treatment would short-circuit R6 and leave the
	// parent waiting forever — strictly worse than degraded read.
	PutLoopCompletion(ctx context.Context, loopID string, envelope []byte) error
}

// Sentinel errors. Returned for diagnostic clarity in handler logs.
var (
	errIntentNotFound          = fmt.Errorf("research intent not found")
	errExecutionOutputNotFound = fmt.Errorf("execution output not found")
)

// fallbackEchoCount caps how many input evidence items the chain
// echoes back when quote-back validation strips ALL of the
// synthesizer's emitted refs (a synthesis-prompt failure mode). 8 is
// enough to give the caller usable citations for trajectory review
// without flooding the SearchResult; higher would mostly add noise
// since the synthesizer's prose only referenced refs it couldn't
// ground in the evidence in the first place.
const fallbackEchoCount = 8

// extractLoopIDFromSubject pulls the loop_id off the NATS subject
// component.synthesize_answer.<loop_id>.
func extractLoopIDFromSubject(subject string) string {
	const prefix = "component.synthesize_answer."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// synthesizeAnswer is the pure synthesis function over injected
// interfaces — no NATS plumbing leaks in here so the test matrix can
// drive it through fakes.
//
// Returns a validated SearchResult on success. Quote-back validation
// strips refs the model emitted that don't appear in the input
// evidence; if the model emits zero valid refs against a non-empty
// evidence array the result is downgraded but still returned (caller
// surfaces as warning). The synthesis prose is preserved verbatim —
// quote-back applies to the evidence_refs field, not free-form prose.
//
// route may be nil — synthesis is the chain's terminal output and
// should still emit useful content when the upstream RouteDecision
// is unavailable (the DecompTrace falls back to ExecutionOutput-only
// shape in that case).
func synthesizeAnswer(ctx context.Context, synthesizer Synthesizer, intent *research.Intent, exec *research.ExecutionOutput, route *research.RouteDecision, maxResponseTokens, maxEvidenceInPrompt, maxSnippetCharsInPrompt int, logger *slog.Logger) (*research.SearchResult, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	if exec == nil {
		return nil, fmt.Errorf("execution output is nil")
	}
	if synthesizer == nil {
		return nil, fmt.Errorf("synthesizer is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	raw, err := callAndDecodeSynthesis(ctx, synthesizer, intent, exec, maxResponseTokens, maxEvidenceInPrompt, maxSnippetCharsInPrompt)
	if err != nil {
		return nil, err
	}

	keptRefs, droppedRefs := quoteBackValidate(raw.EvidenceRefs, exec.Evidence)
	if len(droppedRefs) > 0 {
		logger.Warn("synthesizer emitted refs not in input evidence; stripped",
			slog.Int("dropped_count", len(droppedRefs)),
			slog.String("first_dropped", llmwrap.Truncate(droppedRefs[0], llmwrap.ErrPayloadTruncateBytes)))
	}

	echoed := selectEchoedEvidence(exec.Evidence, keptRefs)
	synthesis := raw.Synthesis
	if len(exec.Evidence) > 0 && len(keptRefs) == 0 {
		logger.Warn("synthesizer emitted zero valid evidence refs against non-empty evidence; echoing top items as fallback",
			slog.Int("evidence_total", len(exec.Evidence)),
			slog.Int("emitted_refs", len(raw.EvidenceRefs)))
		// SearchResult has no Degraded field; embed the reason in a
		// trailing line of the synthesis prose so operator trajectory
		// review surfaces the drift without a schema migration. The
		// appended marker is unambiguous in trajectory captures.
		synthesis = synthesis + "\n\n[note: synthesizer emitted no quote-back refs; chain echoed top evidence]"
	}

	result := &research.SearchResult{
		Evidence:    echoed,
		Synthesis:   synthesis,
		DecompTrace: buildDecompTrace(route, exec),
	}
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("validate search result: %w", err)
	}
	return result, nil
}

// synthesisWire is the narrow wire shape the synthesizer LLM emits.
// Server-constructed fields (Evidence array, DecompTrace) are folded
// in after validation, not asked of the model.
type synthesisWire struct {
	Synthesis    string   `json:"synthesis"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// callAndDecodeSynthesis runs the LLM call and decodes its response
// into the narrow wire shape. Empty content / extract / decode
// failures surface as typed errors so the handler's degraded path
// can categorise them.
func callAndDecodeSynthesis(ctx context.Context, synthesizer Synthesizer, intent *research.Intent, exec *research.ExecutionOutput, maxResponseTokens, maxEvidenceInPrompt, maxSnippetCharsInPrompt int) (synthesisWire, error) {
	var raw synthesisWire
	systemPrompt := buildSystemPrompt()
	userPrompt := buildUserPrompt(intent, exec, maxEvidenceInPrompt, maxSnippetCharsInPrompt)

	rawContent, reason, err := synthesizer.Synthesize(ctx, systemPrompt, userPrompt, maxResponseTokens)
	if err != nil {
		return raw, fmt.Errorf("synthesizer call: %w", err)
	}
	if strings.TrimSpace(rawContent) == "" {
		return raw, fmt.Errorf("synthesizer returned empty content (finish_reason=%q)", reason)
	}

	jsonBytes, err := llmwrap.ExtractJSON(rawContent)
	if err != nil {
		return raw, fmt.Errorf("extract JSON from synthesizer content: %w (raw=%q finish_reason=%q)", err, llmwrap.Truncate(rawContent, llmwrap.ErrPayloadTruncateBytes), reason)
	}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return raw, fmt.Errorf("decode synthesis output: %w (raw=%q)", err, llmwrap.Truncate(string(jsonBytes), llmwrap.ErrPayloadTruncateBytes))
	}
	if strings.TrimSpace(raw.Synthesis) == "" {
		return raw, fmt.Errorf("synthesizer emitted empty synthesis text")
	}
	return raw, nil
}

// quoteBackValidate partitions emitted refs into (kept, dropped) by
// checking each against the set of valid refs in the input evidence
// (every EntityID + every non-empty ObjectStoreRef). Duplicates and
// whitespace-only refs are silently dropped.
func quoteBackValidate(emittedRefs []string, evidence []research.Evidence) (kept, dropped []string) {
	validRefs := make(map[string]struct{}, len(evidence)*2)
	for _, e := range evidence {
		if e.EntityID != "" {
			validRefs[e.EntityID] = struct{}{}
		}
		if e.ObjectStoreRef != "" {
			validRefs[e.ObjectStoreRef] = struct{}{}
		}
	}
	kept = make([]string, 0, len(emittedRefs))
	dropped = make([]string, 0)
	seen := make(map[string]struct{}, len(emittedRefs))
	for _, r := range emittedRefs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		if _, ok := validRefs[r]; ok {
			kept = append(kept, r)
		} else {
			dropped = append(dropped, r)
		}
	}
	return kept, dropped
}

// selectEchoedEvidence picks the Evidence items the chain echoes
// back into the SearchResult. If keptRefs is non-empty, return only
// the matching items (preserving input order); otherwise echo the
// top-by-score items as a fallback (capped at len(in) since smaller
// fixtures should round-trip cleanly).
func selectEchoedEvidence(in []research.Evidence, keptRefs []string) []research.Evidence {
	if len(in) == 0 {
		return nil
	}
	if len(keptRefs) == 0 {
		// Fallback: echo the head of the input slice. Input order
		// matches execute_subqueries' canonical ranking (tier ascending,
		// score descending within a tier, EntityID ascending for
		// tie-break — see processor/research-graph-execute/handler.go's
		// sortEvidence). Capped at fallbackEchoCount so SearchResult
		// round-trips with a manageable citation list when the
		// synthesizer emits no valid refs against non-empty evidence.
		n := len(in)
		if n > fallbackEchoCount {
			n = fallbackEchoCount
		}
		out := make([]research.Evidence, n)
		copy(out, in[:n])
		return out
	}
	want := make(map[string]struct{}, len(keptRefs))
	for _, r := range keptRefs {
		want[r] = struct{}{}
	}
	out := make([]research.Evidence, 0, len(keptRefs))
	for _, e := range in {
		if _, ok := want[e.EntityID]; ok {
			out = append(out, e)
			continue
		}
		if e.ObjectStoreRef != "" {
			if _, ok := want[e.ObjectStoreRef]; ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// buildDecompTrace constructs the DecompTrace from the upstream
// RouteDecision (when present) plus the ExecutionOutput. The trace
// is observability-grade — operator trajectory viewers consume it,
// the chain does not branch on it.
func buildDecompTrace(route *research.RouteDecision, exec *research.ExecutionOutput) *research.DecompTrace {
	trace := &research.DecompTrace{}
	if route != nil {
		trace.RouterAction = route.Action
		switch route.Action {
		case research.ActionWalkSeeds:
			if args, err := route.ParseWalkSeedsArgs(); err == nil {
				trace.SeedEntities = make([]string, 0, len(args.Seeds))
				for _, s := range args.Seeds {
					trace.SeedEntities = append(trace.SeedEntities, s.Ref)
				}
			}
		case research.ActionDecompose:
			if args, err := route.ParseDecomposeArgs(); err == nil {
				// Render the decomposition intent as a single sub-query
				// map so the DecompTrace contract (loose object array)
				// captures it.
				trace.SubQueries = []map[string]any{
					{
						"axes":  args.Axes,
						"focus": args.Focus,
						"scope": args.Scope,
					},
				}
			}
		}
	} else if exec != nil && exec.Action != "" {
		// No RouteDecision — at least record what action the
		// upstream ExecutionOutput claimed it ran.
		trace.RouterAction = exec.Action
	}
	// Surface zero-value trace as nil so callers can omit the field
	// cleanly on synthesize_directly fast-paths.
	if trace.RouterAction == "" && len(trace.SubQueries) == 0 && len(trace.SeedEntities) == 0 {
		return nil
	}
	return trace
}
