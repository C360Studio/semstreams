package graphindex

import (
	"strings"
)

// wildcardPositions returns an exact number of single-token NATS wildcards.
// Fixed-arity graph-index filters use it to make their token count explicit.
func wildcardPositions(count int) string {
	return strings.TrimSuffix(strings.Repeat("*.", count), ".")
}

func nameIndexForwardFilter(name string) string {
	return nameIndexKey(name) + "." + wildcardPositions(7)
}

func incomingIndexTargetFilter(targetID string) string {
	return targetID + "." + wildcardPositions(7)
}
