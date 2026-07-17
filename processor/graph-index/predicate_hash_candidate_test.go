package graphindex

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash predicate helpers preserve the retired candidate exclusively for the
// representation decision tests. Production never compiles, reads, or writes it.
func hashPredicateCandidateHex(predicate string) string {
	sum := sha256.Sum256([]byte(predicate))
	return hex.EncodeToString(sum[:])
}

func hashPredicateCandidateKey(predicate, entityID string) string {
	return hashPredicateCandidateHex(predicate) + "." + entityID
}

func hashPredicateCandidateForwardFilter(predicate string) string {
	return hashPredicateCandidateHex(predicate) + "." + wildcardPositions(6)
}

func hashPredicateCandidateOwnerFilter(entityID string) string {
	return "*." + entityID
}
