package fusion

// RankSignals supplies the framework ranking signals the lens engine folds into
// rankEntities on top of resolve-order + lexical: ontology specificity and
// predicate salience (ADR-062 increment 5, gh#396). Injected through an
// interface so pkg/fusion stays a leaf (no vocabulary / bfo / cco imports);
// production wires the vocabulary registry + BFO/CCO subclass helper (see
// pkg/fusion/fusionvocab). A nil RankSignals leaves ranking at resolve-order +
// lexical — the increments 1–4 behavior — so attaching signals is purely
// additive to the pipeline (nil → unchanged). The salience signal itself is
// DIRECTIONAL: a predicate's weight is signed, so it can down-rank as well as
// boost (gh#441).
type RankSignals interface {
	// ClassSpecificity scores how specific an ontology class IRI is: more
	// specific (deeper in the BFO/CCO subclass tree) = higher; 0 for an
	// unknown or unset class. A deeper class carries more information, so within
	// the same resolve/lexical tier a precisely-typed entity reorders ahead of a
	// vaguely-typed peer.
	ClassSpecificity(classIRI string) float64

	// PredicateSalience returns a predicate's stored salience weight, SIGNED:
	// positive boosts (an entity carrying the fact reorders ahead), negative
	// demotes (it reorders behind), 0 is neutral. Production reads vocabulary
	// PredicateMetadata.Weight. A negative weight is how a consumer down-ranks
	// structurally-identifiable noise (tests, generated code, mocks) that carries
	// the same boosted predicates as the real thing — additive boosting alone
	// cannot separate them (gh#441). The engine folds an entity's strongest boost
	// and strongest demotion together (see entitySalience), so a demotion is a
	// bounded secondary reordering, never an exclusion.
	PredicateSalience(predicate string) float64
}
