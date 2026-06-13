package ownership

import "strings"

// validPattern reports whether p is a 6-part dotted entity-ID glob. Bare "*"
// (match-any) is intentionally NOT a valid OwnerClaim pattern — an owner
// claiming every entity is almost always a bug; patternsIntersect still
// handles "*" for the ForeignEdgeClaim match-any default.
func validPattern(p string) bool {
	return len(strings.Split(p, ".")) == idArity
}

// validOwnerID reports whether an owner id is usable directly as a NATS KV key
// segment (alphanumerics + `-` `_` `=` `.`, no `*`/`>`). Owner identity is
// compared as the canonical string everywhere — presence keys, liveness, the
// write-time lease handle — so it must be subject-safe with NO hashing: a hash
// would put a (tiny but silent) birthday-collision on the OWNERSHIP correctness
// path (two owners colliding ⇒ one masks the other's liveness or forges its
// lease). Component/gateway/owner ids are already short identifiers in this
// shape; enforcing it here keeps identity exact.
func validOwnerID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '=', r == '.':
		default:
			return false
		}
	}
	return true
}

// patternMatches reports whether a concrete 6-part entity id matches a glob
// pattern. Mirrors pkg/lifecycle.matchPattern (manager_query.go:428) so the
// registry's OwnerOf lookup classifies an entity the same way the lifecycle
// query layer does.
func patternMatches(pattern, id string) bool {
	if pattern == "" {
		return false
	}
	if pattern == id || pattern == "*" {
		return true
	}
	pParts := strings.Split(pattern, ".")
	iParts := strings.Split(id, ".")
	if len(pParts) != len(iParts) {
		return false
	}
	for i := range pParts {
		if pParts[i] == "*" {
			continue
		}
		if pParts[i] != iParts[i] {
			return false
		}
	}
	return true
}

// patternsIntersect reports whether two globs can match the same concrete
// entity id — the core of the Decision-2 overlap check. Two patterns intersect
// iff, segment-by-segment, every position is compatible: either side is "*" or
// the literals are equal. Differing arity cannot share an id (unless one side
// is the bare "*" match-any). This is consistent with patternMatches: if id m
// matches both A and B, then A and B intersect.
func patternsIntersect(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == "*" || b == "*" || a == b {
		return true
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	if len(aParts) != len(bParts) {
		return false
	}
	for i := range aParts {
		if aParts[i] == "*" || bParts[i] == "*" || aParts[i] == bParts[i] {
			continue
		}
		return false
	}
	return true
}

// predicatesIntersection returns the sorted set of exact predicate strings
// present in both sets. Empty result means no overlap. Used both to detect a
// collision and to name the offending predicates in the error.
func predicatesIntersection(a, b []string) []string {
	set := make(map[string]struct{}, len(a))
	for _, p := range a {
		set[p] = struct{}{}
	}
	var out []string
	seen := make(map[string]struct{})
	for _, p := range b {
		if _, ok := set[p]; !ok {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny insertion sort to keep error messages deterministic
// without pulling sort into a hot, allocation-light path (overlap sets are
// small — a handful of predicates).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
