package ownership

import "fmt"

// InverseResolver reports whether a predicate has a registered inverse predicate
// (the vocabulary registry's GetInversePredicate returns non-empty). It is
// INJECTED so this package stays free of the vocabulary dependency and the
// inverse-gate is unit-testable with a fake. graph-ingest boot passes an adapter
// over vocabulary.GetInversePredicate.
type InverseResolver func(predicate string) (inverse string, ok bool)

// CheckInverseGate enforces ADR-056 Decision 4's inverse-gate: a ForeignEdgeClaim
// whose mode reconstructs the inverse edge after a birth race
// (Conditional defers-and-applies, Backfill re-derives) REQUIRES the predicate to
// have a registered inverse — otherwise the inverse edge is unrecoverable and the
// graph silently loses a traversal edge (BLOCKING-B). Such a claim must instead
// be declared Strict (drop-if-absent) or NoBirthStub (materialise a stub). Strict
// and NoBirthStub modes need no inverse and are never gated.
//
// Intended to run at boot, the same way payload registration is validated, over
// every registered ForeignEdgeClaim (graph-ingest wires it with a
// vocabulary-backed resolver). Returns the FIRST offending claim's error.
func CheckInverseGate(resolve InverseResolver, claims ...ForeignEdgeClaim) error {
	if resolve == nil {
		return fmt.Errorf("%w: CheckInverseGate needs a non-nil InverseResolver", ErrInvalidClaim)
	}
	for _, c := range claims {
		if !c.Mode.requiresInverse() {
			continue
		}
		if _, ok := resolve(c.Predicate); !ok {
			return fmt.Errorf("%w: foreign-edge predicate %q (mode %q, producer %q) has no registered inverse — "+
				"declare it Strict/NoBirthStub or register an inverse (Decision 4 inverse-gate)",
				ErrInvalidClaim, c.Predicate, c.Mode, c.Producer)
		}
	}
	return nil
}
