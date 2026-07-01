package fusion

// RankSignals supplies the framework ranking signals the lens engine folds into
// rankEntities on top of resolve-order + lexical: ontology specificity and
// predicate salience (ADR-062 increment 5, gh#396). Injected through an
// interface so pkg/fusion stays a leaf (no vocabulary / bfo / cco imports);
// production wires the vocabulary registry + BFO/CCO subclass helper (see
// pkg/fusion/fusionvocab). A nil RankSignals leaves ranking at resolve-order +
// lexical — the increments 1–4 behavior — so the signals are strictly additive.
type RankSignals interface {
	// ClassSpecificity scores how specific an ontology class IRI is: more
	// specific (deeper in the BFO/CCO subclass tree) = higher; 0 for an
	// unknown or unset class. A deeper class carries more information, so within
	// the same resolve/lexical tier a precisely-typed entity reorders ahead of a
	// vaguely-typed peer.
	ClassSpecificity(classIRI string) float64

	// PredicateSalience returns a predicate's stored salience weight (0 =
	// neutral). Production reads vocabulary PredicateMetadata.Weight so an
	// entity carrying more salient facts reorders ahead of a sparse peer.
	PredicateSalience(predicate string) float64
}
