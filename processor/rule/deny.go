// Package rule - DenyVerdict typed error for the deny action.
//
// DenyVerdict is the sentinel error that flows out of executeDeny through the
// action executor and into runActions / dispatchAndRecord. It carries the
// originating rule ID and the human-readable reason so short-circuit handlers
// can record the denial without a side-channel lookup.
//
// Two check patterns are supported:
//
//   - errors.Is(err, ErrDenyVerdict)    — "is this any deny verdict?"
//   - errors.As(err, &dv)              — "give me the RuleID and Reason"
//
// Neither pattern requires a type assertion on the error interface directly.
package rule

import "fmt"

// DenyVerdict is the typed error a deny action returns. The reason
// travels with the error so short-circuit handlers can record it
// without a side-channel lookup. Wrapped via errors.Is/errors.As:
//
//	if errors.Is(err, ErrDenyVerdict) {
//	    // any deny verdict, payload not needed
//	}
//	var dv *DenyVerdict
//	if errors.As(err, &dv) {
//	    // dv.RuleID and dv.Reason available
//	}
type DenyVerdict struct {
	RuleID string
	Reason string
}

func (d *DenyVerdict) Error() string {
	return fmt.Sprintf("rule %s denied: %s", d.RuleID, d.Reason)
}

// ErrDenyVerdict is the sentinel target for errors.Is checks.
// Any *DenyVerdict matches via the Is method below.
var ErrDenyVerdict = &DenyVerdict{}

// Is reports whether target is any *DenyVerdict, regardless of payload.
// Implements the errors.Is contract so errors.Is(err, ErrDenyVerdict)
// returns true for any *DenyVerdict — not just the sentinel instance.
func (d *DenyVerdict) Is(target error) bool {
	_, ok := target.(*DenyVerdict)
	return ok
}
