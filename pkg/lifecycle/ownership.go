// Package lifecycle — ownership-claim derivation (ADR-056 Decision 5 embed).
//
// The lifecycle Manager is the first framework consumer of the pkg/ownership
// registry. A registered Workflow IS an owner claim of the Decision-1 shape
// (an EntityIDPattern + the predicate groups the Manager authors) — this file
// derives that claim so Manager.Register can register it through the shared
// epoch and have cross-process overlap surfaced (Decision 2).
package lifecycle

import (
	"sort"

	"github.com/c360studio/semstreams/pkg/ownership"
)

// deriveOwnerRegistration builds the ownership.Registration a registered
// Workflow asserts — TWO OwnerClaims over the workflow's EntityIDPattern, split
// by the write discipline each predicate group actually uses (mode is a property
// of the group, not of the single CAS wire-call the Manager batches them into):
//
//   - Claim 1 (cas-transition): {PhasePredicate}. The phase write is conditional
//     on a declared transitions-table edge and carries ExpectedRevision
//     (manager.go:445,:505) — a genuine read-modify-write transition.
//   - Claim 2 (replace-owned): {non-empty audit predicates ∪ writable projected
//     field predicates}. Single-valued remove+add (manager.go:484-506); they
//     ride the same CAS wire-call but their discipline is replace, not
//     transition. Splitting keeps the mode honest for the Decision-3
//     schema-derived reconcile (whose remove-set semantics differ by mode), even
//     though both modes are isOwning() so Decision-2 overlap detection is
//     identical either way.
//
// EXCLUDED from both claims: read-only, reference, and ID predicates. The
// Manager never authors them (projectStructToTriples / diffProjectedTriples skip
// read-only non-phase fields — projection.go:209, manager.go:554), so claiming
// them would falsely collide with their real writer (a foreign-edge producer, an
// upstream processor) rather than protect anything the Manager owns. Child-link
// and reference predicates are read by Manager.Children/References, never
// written — so they are app/foreign-owned, not the Manager's claim.
//
// The owner id is the workflow Name: the stable LOGICAL identity of this writer.
// A redeploy — or a second binary implementing the same workflow type — re-
// registers the same owner idempotently (the workflow TYPE is the owner, not the
// process; presence then reflects "≥1 process of this type is live"). Two
// DIFFERENT workflow owners selecting an overlapping (pattern, predicate) cell is
// the cross-process collision Decision 2 detects.
func deriveOwnerRegistration(w Workflow, meta *structMeta) ownership.Registration {
	claims := make([]ownership.OwnerClaim, 0, 2)

	// Claim 1 — phase, cas-transition.
	claims = append(claims, ownership.OwnerClaim{
		Owner:      w.Name,
		Pattern:    w.EntityIDPattern,
		Predicates: []string{w.PhasePredicate},
		Mode:       ownership.ModeCASTransition,
	})

	// Claim 2 — audit + writable projected fields, replace-owned.
	seen := map[string]struct{}{w.PhasePredicate: {}} // phase belongs to claim 1
	var replace []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		replace = append(replace, p)
	}
	add(w.AuditPredicates.Source)
	add(w.AuditPredicates.At)
	add(w.AuditPredicates.From)
	add(w.AuditPredicates.Note)
	for predicate, fm := range meta.FieldsByPredicate {
		if fm.ReadOnly {
			continue // phase (implicitly read-only) is claim 1; reference/read-only fields the Manager never authors
		}
		add(predicate)
	}
	// Deterministic order so the stored epoch value (and tests) don't flap on
	// Go's randomized map iteration; the overlap check is set-based and
	// order-immaterial, but a stable claim keeps the epoch revision quiet.
	sort.Strings(replace)

	if len(replace) > 0 {
		claims = append(claims, ownership.OwnerClaim{
			Owner:      w.Name,
			Pattern:    w.EntityIDPattern,
			Predicates: replace,
			Mode:       ownership.ModeReplaceOwned,
		})
	}

	return ownership.Registration{Owner: w.Name, Claims: claims}
}
