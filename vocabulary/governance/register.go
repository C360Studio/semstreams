package governance

import "github.com/c360studio/semstreams/vocabulary"

// Register registers governance predicates with the global vocabulary
// registry. Idempotent (the registry overwrites on re-register).
//
// Callers: processor/agentic-governance/init() invokes this so the
// rule validator's vocabulary.IsRuleOpaque check sees the metadata
// whenever the governance package is imported. Tests may call
// directly.
func Register() {
	registerInjectionPredicates()
}

// registerInjectionPredicates registers the four predicates emitted
// by the embedding classifier filter. Score and TopMatchID are
// rule-opaque per the ADR-043 Phase 2 decision; signal and tier stay
// rule-matchable.
func registerInjectionPredicates() {
	vocabulary.Register(InjectionSignal,
		vocabulary.WithDescription("Signal-bucket label the injection classifier matched on (e.g., instruction-override, network-egress, benign); categorical, rule-matchable"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(InjectionTier,
		vocabulary.WithDescription("Classifier tier that produced the verdict (0=regex, 1=BM25, 2=neural, 3=LLM); operational filter, rule-matchable"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(InjectionScore,
		vocabulary.WithDescription("Similarity score of the nearest-neighbor match; rule-opaque to prevent adversaries tuning embedding distance toward a rule threshold"),
		vocabulary.WithDataType("float64"),
		vocabulary.WithRange("0-1"),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(InjectionTopMatchID,
		vocabulary.WithDescription("Corpus record identifier (sha) of the nearest-neighbor example; rule-opaque telemetry — rules matching on corpus IDs break when the corpus is reshuffled"),
		vocabulary.WithDataType("string"),
		vocabulary.WithRuleOpaque(true))
}
