package ownership

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidClaim is returned when a claim or waiver is malformed (bad pattern,
// empty predicate set, invalid mode). Registration fails fast — a malformed
// claim is a programming error, not a runtime condition.
var ErrInvalidClaim = errors.New("ownership: invalid claim")

// ErrOwnershipOverlap is returned (wrapped in *OverlapError) when two claims
// select an overlapping (entity-ID pattern, predicate) cell in an owning write
// mode. Registration FAILS — no silent coexistence (ADR-056 Decision 2). Match
// with errors.Is; inspect the named owners/pattern/predicates via
// errors.As(&OverlapError{}).
var ErrOwnershipOverlap = errors.New("ownership: claim overlap")

// OverlapError names both owners, the overlapping pattern, and the overlapping
// predicates of a rejected registration. CrossType is true when the collision
// is Owner×ForeignEdge (an OwnerClaim reconcile would strip a foreign edge)
// rather than OwnerClaim×OwnerClaim.
type OverlapError struct {
	Owner       string   // the registrant whose claim was rejected
	With        string   // the incumbent owner it collides with
	Pattern     string   // the registrant's pattern (the one that intersects)
	WithPattern string   // the incumbent's pattern
	Predicates  []string // the exact predicates in both sets
	CrossType   bool     // true: Owner×ForeignEdge; false: OwnerClaim×OwnerClaim
}

func (e *OverlapError) Error() string {
	kind := "owner/owner"
	if e.CrossType {
		kind = "owner/foreign-edge"
	}
	return fmt.Sprintf(
		"ownership: claim overlap (%s): owner %q pattern %q overlaps owner %q pattern %q on predicate(s) [%s]",
		kind, e.Owner, e.Pattern, e.With, e.WithPattern, strings.Join(e.Predicates, ", "),
	)
}

// Unwrap lets errors.Is(err, ErrOwnershipOverlap) match an *OverlapError.
func (e *OverlapError) Unwrap() error { return ErrOwnershipOverlap }
