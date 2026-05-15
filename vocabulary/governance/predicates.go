// Package governance declares vocabulary predicates emitted by the
// agentic-governance filter chain. Names follow the SemStreams
// three-level dotted notation (domain.category.property) so they are
// addressable as NATS subjects and graph predicates.
//
// Phase 2 of ADR-043 introduces the injection-classifier emission
// surface. The rule-opacity split is intentional:
//
//   - Rule-visible (categorical, not adversary-tunable):
//     governance.injection.signal, governance.injection.tier
//   - Rule-opaque (adversary can optimise toward a threshold, or
//     telemetry-only):
//     governance.injection.score, governance.injection.top_match_id
//
// See docs/adr/043-prompt-injection-defense-detonation-corpus.md and
// project_adr_043_phase_2_plan memory for the decision context.
package governance

// Injection-classifier predicates emitted by the agentic-governance
// embedding classifier filter.
const (
	// InjectionSignal is the signal-bucket label the classifier
	// matched on (e.g., "instruction-override", "network-egress",
	// "benign"). Rule-visible: categorical and not Goodhart-prone.
	InjectionSignal = "governance.injection.signal"

	// InjectionTier records which classifier tier produced the
	// verdict (0=regex/keyword, 1=BM25, 2=neural, 3=LLM).
	// Rule-visible: operators may want to weight regex matches
	// differently from embedding matches.
	InjectionTier = "governance.injection.tier"

	// InjectionScore is the similarity score the classifier
	// produced for the matched example. Rule-opaque: the score is
	// the surface an adversary can tune by crafting an input whose
	// embedding sits just below threshold. Operators may opt rules
	// into matching on it per deployment.
	InjectionScore = "governance.injection.score"

	// InjectionTopMatchID is the corpus record identifier (sha) of
	// the nearest-neighbor example. Rule-opaque: telemetry only,
	// and rules that match on corpus IDs break silently when the
	// corpus is reshuffled (Phase 4 multi-tenancy).
	InjectionTopMatchID = "governance.injection.top_match_id"
)
