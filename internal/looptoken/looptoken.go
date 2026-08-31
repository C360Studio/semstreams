// Package looptoken owns the one predicate that decides whether a string is a
// loop instance token — the identity the whole agentic substrate keys on
// (ADR-105, #1192): the AGENT_LOOPS record, the loop-execution graph entity,
// and the run entity's instance segment.
//
// A loop token is minted by the framework as a v4 UUID and carried in canonical
// RFC 4122 text form. Two mint spellings used to truncate that UUID to 8 hex
// characters — 32 bits, where the birthday bound reaches ~1% collision
// probability at ~9,300 loops and 50% at ~77,000, and a dispatch collision was
// SILENT because CreateLoopWithID overwrites the colliding record and context
// manager, merging two conversations. A full canonical UUID carries 122 random
// bits, at which point the collision probability stops being worth a design.
//
// This package is deliberately module-internal. The contract for anyone outside
// this repository is "echo, never author", so there is no exported predicate, no
// configurable strictness, and no injectable generator to relax it.
package looptoken

import "github.com/google/uuid"

// Valid reports whether s is a loop instance token in canonical form: 36 bytes,
// lowercase hexadecimal, hyphenated.
//
// Parsing alone is not the test. uuid.Parse also accepts the uppercase, braced
// ("{...}"), and "urn:uuid:" spellings, which are four different strings for one
// identity — a token stored under one spelling misses its own KV key and its own
// entity ID under another. Requiring the value to equal its own canonical
// re-rendering collapses that to the single form the wire carries.
//
// Form is the whole check: the version bits are not read. Minting is v4, but a
// seam validating what it received has no business asserting how a peer
// deployment's framework minted it.
func Valid(s string) bool {
	if len(s) != 36 {
		return false
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return false
	}
	return parsed.String() == s
}
