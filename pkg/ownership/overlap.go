package ownership

import (
	"slices"
	"sort"
)

// checkOverlap rejects a candidate registration that would put two owners on
// the same (entity-ID pattern, predicate) cell in an owning mode (ADR-056
// Decision 2). It runs three checks against the OTHER owners already in the
// epoch (the registrant's own claims never collide with themselves — cs-api's
// isHostedBy is legitimately both an OwnerClaim own→parent edge and a
// ForeignEdgeClaim child→System edge, same owner, exempt):
//
//  1. OwnerClaim × OwnerClaim — two owning-mode claims selecting an overlapping
//     pattern×predicate cell (the load-bearing case).
//  2. registrant OwnerClaim × incumbent ForeignEdgeClaim (different owner) — the
//     owner's reconcile would strip the foreign edge (Decision 2 MEDIUM).
//  3. registrant ForeignEdgeClaim × incumbent OwnerClaim (different owner) — the
//     same collision discovered from the other direction.
//
// Two ForeignEdgeClaims never collide (both legitimately add edges), and
// append-evidence claims are exempt (no single owner). A collision is waived
// only when EVERY overlapping predicate is covered by a CoordinationWaiver
// naming both owners.
//
// `others` must NOT contain the registrant (the caller removes the registrant's
// prior entry before calling, so re-registration is idempotent). Returns the
// first collision in deterministic (sorted-owner) order, or nil.
func checkOverlap(registrant string, cand ownerEntry, others map[string]ownerEntry, waivers []CoordinationWaiver) error {
	for _, incumbent := range sortedOwners(others) {
		if incumbent == registrant {
			continue
		}
		entry := others[incumbent]

		// 1. OwnerClaim × OwnerClaim (owning modes only).
		for _, c := range cand.Claims {
			if !c.Mode.isOwning() {
				continue
			}
			for _, d := range entry.Claims {
				if !d.Mode.isOwning() {
					continue
				}
				if !patternsIntersect(c.Pattern, d.Pattern) {
					continue
				}
				if hit := unwaived(predicatesIntersection(c.Predicates, d.Predicates), registrant, incumbent, waivers); len(hit) > 0 {
					return &OverlapError{
						Owner: registrant, With: incumbent,
						Pattern: c.Pattern, WithPattern: d.Pattern,
						Predicates: hit,
					}
				}
			}
		}

		// 2. registrant OwnerClaim × incumbent ForeignEdgeClaim.
		if err := crossType(registrant, cand.Claims, incumbent, entry.ForeignEdges, waivers); err != nil {
			return err
		}
		// 3. registrant ForeignEdgeClaim × incumbent OwnerClaim (symmetric).
		if err := crossType(incumbent, entry.Claims, registrant, cand.ForeignEdges, waivers); err != nil {
			return err
		}
	}
	return nil
}

// crossType checks owning OwnerClaims (held by ownerA) against ForeignEdgeClaims
// (held by ownerB) for a shared predicate over an overlapping pattern. The
// returned OverlapError always names the registrant first; the caller passes
// the pair in whichever order matches the direction being checked, and we
// normalise by reporting ownerA as Owner (it holds the OwnerClaim whose
// reconcile would strip the edge).
func crossType(ownerA string, claims []OwnerClaim, ownerB string, edges []ForeignEdgeClaim, waivers []CoordinationWaiver) error {
	if ownerA == ownerB {
		return nil // same owner: legitimate dual use (isHostedBy own + foreign)
	}
	for _, c := range claims {
		if !c.Mode.isOwning() {
			continue
		}
		for _, f := range edges {
			target := f.TargetPattern
			if target == "" {
				target = "*" // match-any: conservative default
			}
			if !patternsIntersect(c.Pattern, target) {
				continue
			}
			if !slices.Contains(c.Predicates, f.Predicate) {
				continue
			}
			if hit := unwaived([]string{f.Predicate}, ownerA, ownerB, waivers); len(hit) > 0 {
				return &OverlapError{
					Owner: ownerA, With: ownerB,
					Pattern: c.Pattern, WithPattern: target,
					Predicates: hit, CrossType: true,
				}
			}
		}
	}
	return nil
}

// unwaived returns the predicates in `preds` NOT exempted by a mutual-consent
// waiver between a and b. An empty result means the whole collision is waived.
func unwaived(preds []string, a, b string, waivers []CoordinationWaiver) []string {
	if len(preds) == 0 {
		return nil
	}
	var out []string
	for _, p := range preds {
		if !waived(a, b, p, waivers) {
			out = append(out, p)
		}
	}
	return out
}

// waived reports whether the (a,b) collision on predicate p is exempted by
// MUTUAL CONSENT: BOTH owners must have declared a matching waiver (ADR-056
// 056:759 — "both overlapping owners carry a matching waiver"). Neither owner
// can unilaterally waive itself into the other's owned cell — a one-sided
// waiver is no waiver.
func waived(a, b, p string, waivers []CoordinationWaiver) bool {
	return hasWaiver(waivers, a, b, p) && hasWaiver(waivers, b, a, p)
}

func hasWaiver(waivers []CoordinationWaiver, owner, with, p string) bool {
	for _, w := range waivers {
		if w.Owner == owner && w.With == with && slices.Contains(w.Predicates, p) {
			return true
		}
	}
	return false
}

func sortedOwners(m map[string]ownerEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
