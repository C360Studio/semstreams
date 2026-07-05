package graph

import "strings"

// MatchesAnyIDPrefix reports whether a dot-delimited entity ID falls under any
// of the given ID prefixes (OR-matched). An empty/nil prefixes slice means "no
// filter" and matches every ID — the convention already proven by
// graphrag.filterEntityIDsByType and the []string type-filter surfaces (ADR-071).
//
// Matching is on a dot boundary: id matches prefix p iff id == p or id has the
// literal prefix p+"." So prefix "c360.semspec.source.doc" matches
// "c360.semspec.source.doc" and "c360.semspec.source.doc.readme" but NOT
// "c360.semspec.source.docker.compose" — a prefix must end on a segment
// boundary, never mid-segment. This mirrors the dot-prefix convention on
// PrefixQueryRequest (the server appends the trailing dot when it filters), so
// the deterministic prefix query and the NL scope filter share ONE matcher and
// their semantics cannot drift.
//
// An empty string element inside a non-empty slice is treated as an explicit
// match-all (consistent with empty=no-filter), not as "matches only the empty
// ID".
func MatchesAnyIDPrefix(id string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if p == "" || id == p || strings.HasPrefix(id, p+".") {
			return true
		}
	}
	return false
}
