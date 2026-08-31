package types

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Property-based contract tests for the entity-ID grammar (rapid spike).
// Each property encodes a stated spec invariant, cited by requirement, and its
// generator is written from the spec grammar, never from the implementation.
//
// The division of labor across the three suites over this grammar — none of
// them subsumes another, and deleting any one opens a hole:
//
//   - These properties own the ACCEPT path. They generate from the spec
//     grammar, so they assert that everything canonical is accepted, parsed
//     into its named position, and re-serialized byte-identically. They say
//     nothing about what must be REJECTED: nothing outside the grammar is
//     ever generated.
//   - The hand-written tables (TestValidateEntityIDContract,
//     TestValidateEntityIDPrefix, TestEntityIDFailurePrecedence) own
//     CLASSIFIED rejection — that a specific malformed input is refused, with
//     a specific code, reason, and precedence. Nothing else in the package
//     asserts that any input is refused at all.
//   - FuzzParseEntityIDRoundTrip owns ORACLE SELF-CONSISTENCY: validation is
//     deterministic, Validate and Parse agree, and an accepted key round-trips
//     through Key(). Read it — it never asserts that anything is rejected, so
//     it cannot see an accept-path widening that stays self-consistent.
//
// Measured 2026-08-31, because the earlier wording here claimed the fuzz
// target "explores rejection densely" and that is false: relaxing the
// first-byte rule to admit a leading '_' widens the accept path while
// preserving the Key() round-trip, and it survives 5.2M fuzz executions over
// 30s AND all three properties. Only the tables catch it. (The narrower
// seven-part arity widening does die in the fuzz target — but only because a
// seven-part accept also breaks round-trip, which is the oracle doing its own
// job, not rejection coverage.) The tables are load-bearing; do not delete
// them on the theory that fuzz or properties cover them.

// entityIDSegment generates one canonical segment from the spec grammar: first
// byte alphanumeric, remaining bytes alphanumeric, '_', or '-'. Length is
// capped at 39 bytes so six segments plus five dots (max 239) stay under the
// 256-byte bound; the bound itself is exercised by TestPropEntityIDByteBound.
// spec: entity-id-contract / Every entity ID has one canonical six-segment ASCII form
var entityIDSegment = rapid.StringMatching(`[a-zA-Z0-9][a-zA-Z0-9_-]{0,38}`)

func drawEntityIDSegments(t *rapid.T) []string {
	segments := make([]string, canonicalEntityIDParts)
	for index := range segments {
		segments[index] = entityIDSegment.Draw(t, "segment")
	}
	return segments
}

// TestPropEntityIDRoundTrip: every grammatically canonical six-segment string
// validates, parses with each segment landing in its named position, and
// re-serializes byte-identically.
// spec: entity-id-contract / Every entity ID has one canonical six-segment ASCII form
// spec: entity-id-contract / Each entity-ID position has one defined meaning and one owner
func TestPropEntityIDRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		segments := drawEntityIDSegments(t)
		id := strings.Join(segments, ".")

		parsed, err := ParseEntityID(id)
		if err != nil {
			t.Fatalf("canonical ID %q rejected: %v", id, err)
		}
		positions := []string{parsed.Org, parsed.Platform, parsed.System, parsed.Domain, parsed.Type, parsed.Instance}
		for index, want := range segments {
			if positions[index] != want {
				t.Fatalf("position %d of %q parsed as %q, want %q", index, id, positions[index], want)
			}
		}
		if parsed.Key() != id {
			t.Fatalf("round trip of %q produced %q", id, parsed.Key())
		}
		if !parsed.IsValid() {
			t.Fatalf("parsed ID %q reports invalid", id)
		}
	})
}

// TestPropEntityIDByteBound: acceptance of an otherwise-canonical ID flips
// exactly at MaxEntityIDBytes — no off-by-one on either side.
// spec: entity-id-contract / Every entity ID has one canonical six-segment ASCII form
func TestPropEntityIDByteBound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Half the draws hug the spec boundary: rapid biases toward the
		// GENERATOR's range endpoints, not domain boundaries it cannot know,
		// so a wide range alone catches an off-by-one at 256 only
		// probabilistically (measured: a `>=` mutation survived 100 cases).
		total := rapid.OneOf(
			rapid.IntRange(11, MaxEntityIDBytes+64),
			rapid.IntRange(MaxEntityIDBytes-1, MaxEntityIDBytes+1),
		).Draw(t, "totalBytes")
		id := "a.a.a.a.a." + strings.Repeat("x", total-10)
		err := ValidateEntityID(id)
		if total <= MaxEntityIDBytes && err != nil {
			t.Fatalf("%d-byte canonical ID rejected: %v", total, err)
		}
		if total > MaxEntityIDBytes && err == nil {
			t.Fatalf("%d-byte ID accepted past the %d-byte bound", total, MaxEntityIDBytes)
		}
	})
}

// TestPropEntityIDPatternMatch: a pattern derived from an ID by wildcarding
// any subset of positions matches that ID, and substituting a different
// literal at any non-wildcard position stops the match without an error.
// spec: entity-id-contract / Entity-ID patterns are separate exact-arity wildcard values
func TestPropEntityIDPatternMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		segments := drawEntityIDSegments(t)
		id := strings.Join(segments, ".")

		pattern := make([]string, len(segments))
		literalPositions := make([]int, 0, len(segments))
		for index, segment := range segments {
			if rapid.Bool().Draw(t, "wildcard") {
				pattern[index] = "*"
			} else {
				pattern[index] = segment
				literalPositions = append(literalPositions, index)
			}
		}

		matched, err := MatchEntityIDPattern(strings.Join(pattern, "."), id)
		if err != nil {
			t.Fatalf("derived pattern %q errored against %q: %v", strings.Join(pattern, "."), id, err)
		}
		if !matched {
			t.Fatalf("derived pattern %q did not match its source %q", strings.Join(pattern, "."), id)
		}

		if len(literalPositions) == 0 {
			return
		}
		position := rapid.SampledFrom(literalPositions).Draw(t, "substitutedPosition")
		// Half the draws are NEAR misses. An independent draw from the ~62^n
		// segment space is a foreign literal by brute distance, so it cannot
		// distinguish "compares bytes" from "compares case-insensitively" —
		// and the spec forbids case-folding by name (entity-id-contract: MUST
		// NOT trim, case-fold, Unicode-normalize, escape, encode, replace).
		// Measured: an EqualFold mutation of MatchEntityIDPattern survived the
		// independent draw at the default budget on six fresh seeds, and needed
		// 285 tests at 200x the budget to die by luck.
		substitute := rapid.OneOf(
			entityIDSegment,
			rapid.Just(strings.ToUpper(segments[position])),
			rapid.Just(strings.ToLower(segments[position])),
		).Draw(t, "substitute")
		if substitute == segments[position] {
			return
		}
		pattern[position] = substitute
		matched, err = MatchEntityIDPattern(strings.Join(pattern, "."), id)
		if err != nil {
			t.Fatalf("substituted pattern %q errored against %q: %v", strings.Join(pattern, "."), id, err)
		}
		if matched {
			t.Fatalf("pattern %q with foreign literal at %d still matched %q", strings.Join(pattern, "."), position, id)
		}
	})
}
