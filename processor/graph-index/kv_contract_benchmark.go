package graphindex

import "strings"

// wildcardPositions returns an exact number of single-token NATS wildcards.
// It is intentionally limited to the inactive graph-index contract spike.
func wildcardPositions(count int) string {
	return strings.TrimSuffix(strings.Repeat("*.", count), ".")
}

func predicateIndexForwardFilter(predicate string) string {
	return predicateHashHex(predicate) + "." + wildcardPositions(6)
}

func nameIndexForwardFilter(name string) string {
	return nameIndexKey(name) + "." + wildcardPositions(7)
}

func incomingIndexTargetFilter(targetID string) string {
	return targetID + "." + wildcardPositions(7)
}

// The raw predicate helpers describe only the benchmark candidate. No
// production reader, writer, configuration, or lifecycle path calls them.
func rawPredicateCandidateKey(predicate, entityID string) string {
	return predicate + "." + entityID
}

func rawPredicateCandidateForwardFilters(predicate string) []string {
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

func rawPredicateCandidateOwnerFilter(entityID string) string {
	return wildcardPositions(3) + "." + entityID
}
