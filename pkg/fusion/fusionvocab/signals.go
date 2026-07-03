// Package fusionvocab is the production implementation of fusion.RankSignals
// (ADR-062 increment 5, gh#396): it wires the lens engine's ranking signals onto
// the vocabulary registry (predicate salience) and the BFO/CCO subclass helper
// (ontology specificity).
//
// It is a SUBPACKAGE of pkg/fusion deliberately: pkg/fusion is a near-leaf
// (message only) so the engine stays deterministic and unit-testable without the
// vocabulary/ontology packages. This is where the ranker's framework signals are
// resolved against those packages — the same split as pkg/fusion/fusionnats for
// the retrieval surface.
package fusionvocab

import (
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

// Signals implements fusion.RankSignals over the framework's registries: ontology
// specificity from the BFO/CCO subclass tree, predicate salience from the
// vocabulary registry. Stateless — safe to share and pass by value.
type Signals struct{}

// Compile-time assertion that Signals satisfies the engine's ranking surface.
var _ fusion.RankSignals = Signals{}

// New returns a production RankSignals. (A trivial constructor for symmetry with
// the other fusion wiring and to keep the seam explicit at call sites.)
func New() Signals { return Signals{} }

// ClassSpecificity scores an ontology class IRI by its subclass depth — the
// number of ancestors up to the root — so a more specific class ranks higher.
// cco.Parents resolves BOTH CCO and pure-BFO IRIs (it continues up the BFO tree
// once it crosses the ontology boundary), so it is the single depth function for
// either namespace. An empty or unknown IRI has depth 0.
func (Signals) ClassSpecificity(classIRI string) float64 {
	if classIRI == "" {
		return 0
	}
	return float64(len(cco.Parents(classIRI)))
}

// PredicateSalience returns a predicate's stored salience weight from the
// vocabulary registry (0 when unregistered or unweighted). Signed: a negative
// weight down-ranks (gh#441) — a faithful pass-through of PredicateMetadata.Weight.
func (Signals) PredicateSalience(predicate string) float64 {
	if meta := vocabulary.GetPredicateMetadata(predicate); meta != nil {
		return meta.Weight
	}
	return 0
}
