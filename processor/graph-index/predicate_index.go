package graphindex

import "strings"

// predicateIndexMarker is the fixed, content-free value stored at every
// PREDICATE_INDEX composite key. Empty by construction, not a timestamp:
// the worker pool that drives indexing has no cross-entity completion
// order, so a timestamp would invite a future reader to treat it as
// "most recent," which this design does not guarantee (ADR-065).
var predicateIndexMarker = []byte{}

// predicateIndexKey builds the PREDICATE_INDEX composite key for one
// (predicate, entity) membership pair: predicate3.entity6.
func predicateIndexKey(predicate, entityID string) string {
	return predicate + "." + entityID
}

// predicateIndexForwardFilters returns the exact, domain.category, and domain
// fixed-nine-token filters for a canonical three-token predicate.
func predicateIndexForwardFilters(predicate string) []string {
	parts := strings.Split(predicate, ".")
	if len(parts) != 3 {
		return nil
	}
	entityPositions := "." + wildcardPositions(6)
	return []string{
		predicate + entityPositions,
		parts[0] + "." + parts[1] + ".*" + entityPositions,
		parts[0] + ".*.*" + entityPositions,
	}
}

func predicateIndexForwardFilter(predicate string) string {
	filters := predicateIndexForwardFilters(predicate)
	if len(filters) == 0 {
		return ""
	}
	return filters[0]
}

// predicateIndexEntityFilter enumerates the complete predicate membership set
// owned by one entity under the predicate3.entity6 layout.
func predicateIndexEntityFilter(entityID string) string {
	return wildcardPositions(3) + "." + entityID
}
